package team

// playbook_synthesizer.go is the broker-level LLM synthesis worker for v1.3
// playbook compounding-intelligence. Mirrors entity_synthesizer.go closely:
// same debounce + coalesce + threshold pattern. The only meaningful
// differences are:
//
//  1. The "authoritative" document is a playbook body (team/playbooks/{slug}.md)
//     rather than an entity brief. We preserve the author's main body verbatim
//     and only append / update a trailing "## What we've learned" section.
//  2. The input signal is the append-only execution log at
//     team/playbooks/{slug}.executions.jsonl, not entity facts.
//  3. Threshold default is 3 — playbooks run more often than entity facts
//     accumulate, and early wisdom is still useful.
//
// The write path is a plain wikiWriteRequest for the source playbook article;
// the existing auto-recompile hook in wiki_worker.go then regenerates the
// SKILL.md under team/playbooks/.compiled/{slug}/SKILL.md.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// DefaultPlaybookSynthesisThreshold is the number of new executions that
// must accumulate before an automatic synthesis is triggered. Configurable
// per deployment via WUPHF_PLAYBOOK_SYNTHESIS_THRESHOLD.
const DefaultPlaybookSynthesisThreshold = 3

// DefaultPlaybookSynthesisTimeout bounds a single LLM shell-out.
// Configurable via WUPHF_PLAYBOOK_SYNTHESIS_TIMEOUT (seconds).
const DefaultPlaybookSynthesisTimeout = 45 * time.Second

// MaxPlaybookSynthQueue is the buffered channel size for pending jobs.
const MaxPlaybookSynthQueue = 32

// MaxPlaybookBodySize caps the LLM output bytes we are willing to commit.
// Playbooks can be longer than entity briefs (more structured steps), so
// the ceiling is doubled from MaxBriefSize.
const MaxPlaybookBodySize = 64 * 1024

// MaxExecutionsForPrompt is the hard cap on how many recent execution
// entries we feed into a single synthesis prompt. Keeps the prompt
// bounded even when a playbook has hundreds of runs.
const MaxExecutionsForPrompt = 20

// PlaybookSynthesisPromptSystem is the system prompt sent on every call.
// Locked here so the behavior is reviewable — do not edit casually.
const PlaybookSynthesisPromptSystem = `You maintain playbooks in a team wiki. Each playbook has an author who specified the canonical steps. Your job is to integrate lessons from recent executions WITHOUT rewriting the author's body.

Rules you MUST follow:
1. Preserve the frontmatter (the leading --- block) exactly as given. Do not add, remove, or reorder keys.
2. Preserve the author's main body verbatim. Do not rewrite steps, even if executions suggest changes. The author's intent is authoritative; execution learnings are advisory.
3. At the bottom of the body, maintain a section titled exactly "## What we've learned". If it already exists, update the bullets. If it doesn't, append it. Everything else below that heading is owned by you.
4. In "What we've learned", surface concrete lessons drawn from the provided executions: common failure modes, helpful prerequisites, shortcuts that worked, when to skip or add extra steps.
5. When two executions disagree (e.g., skipping step X succeeded once but failed another time), record the disagreement as a "**Contradiction:**" inline callout under "What we've learned" and DO NOT resolve it. The next human editor will decide.
6. Do not invent lessons. Every bullet must be traceable to at least one execution entry.
7. Output ONLY the full updated markdown of the playbook file. No commentary, no code fences.`

// WhatWeveLearnedHeading is the exact heading the synthesizer maintains.
// Changing this invalidates prior-synthesis section replacement — in-flight
// playbooks would grow a duplicate heading.
const WhatWeveLearnedHeading = "## What we've learned"

// ErrPlaybookSynthQueueSaturated is returned when the buffered channel is full.
var ErrPlaybookSynthQueueSaturated = errors.New("playbook synth: queue saturated")

// ErrPlaybookSynthesizerStopped is returned when Enqueue is called after Stop.
var ErrPlaybookSynthesizerStopped = errors.New("playbook synth: not running")

// ErrPlaybookSynthNoNewExecutions is surfaced for observability when a job
// runs with zero un-synthesized executions. Not a hard failure — skips.
var ErrPlaybookSynthNoNewExecutions = errors.New("playbook synth: no new executions since last synthesis")

// ErrPlaybookSourceMissing is surfaced when the source article no longer
// exists. Treat as an idempotent skip — deletion of the authored body makes
// learnings moot.
var ErrPlaybookSourceMissing = errors.New("playbook synth: source playbook does not exist")

// PlaybookSynthesisJob is one pending synthesis request for a specific slug.
type PlaybookSynthesisJob struct {
	Slug       string
	RequestBy  string
	EnqueuedAt time.Time
	// ID is a monotonic counter so callers can correlate responses.
	ID uint64
}

// PlaybookSynthesizedEvent is the SSE payload broadcast after every
// successful synthesis commit. Kept distinct from the SKILL.md compile
// path — the UI cares about the learnings landing, not the recompile.
type PlaybookSynthesizedEvent struct {
	Slug            string `json:"slug"`
	CommitSHA       string `json:"commit_sha"`
	ExecutionCount  int    `json:"execution_count"`
	SynthesizedTS   string `json:"synthesized_ts"`
	SourcePath      string `json:"source_path"`
	TriggeredByUser bool   `json:"triggered_by_user"`
}

// PlaybookSynthesizerConfig tunes the worker. Zero values -> defaults.
type PlaybookSynthesizerConfig struct {
	Threshold int
	Timeout   time.Duration

	// LLMCall is the pluggable shell-out used by tests. Production leaves
	// this nil and the worker falls back to defaultLLMCall from
	// entity_synthesizer.go (provider.RunConfiguredOneShot).
	LLMCall func(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// playbookSynthEventPublisher is the subset of Broker the synthesizer needs.
type playbookSynthEventPublisher interface {
	PublishPlaybookSynthesized(evt PlaybookSynthesizedEvent)
}

// PlaybookSynthesizer is the broker-level synthesis worker for playbook
// learnings. Single-writer via a drain goroutine so only one archivist
// commit is in flight at a time across the whole wiki.
type PlaybookSynthesizer struct {
	worker    *WikiWorker
	execLog   *ExecutionLog
	publisher playbookSynthEventPublisher
	cfg       PlaybookSynthesizerConfig

	mu       sync.Mutex
	jobs     chan PlaybookSynthesisJob
	inflight map[string]bool // slug -> at most 1 running per slug
	queued   map[string]bool // slug -> at most 1 pending per slug
	running  bool
	nextID   uint64
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewPlaybookSynthesizer wires a synthesizer against the given worker +
// execution log. Config may be zero; defaults are filled in here.
func NewPlaybookSynthesizer(worker *WikiWorker, execLog *ExecutionLog, publisher playbookSynthEventPublisher, cfg PlaybookSynthesizerConfig) *PlaybookSynthesizer {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultPlaybookSynthesisThreshold
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultPlaybookSynthesisTimeout
	}
	return &PlaybookSynthesizer{
		worker:    worker,
		execLog:   execLog,
		publisher: publisher,
		cfg:       cfg,
		jobs:      make(chan PlaybookSynthesisJob, MaxPlaybookSynthQueue),
		inflight:  make(map[string]bool),
		queued:    make(map[string]bool),
	}
}

// Start launches the synthesis loop. Returns immediately. Stop via ctx or Stop().
func (s *PlaybookSynthesizer) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.drain(ctx)
}

// Stop signals the worker to exit. Pending jobs are dropped.
func (s *PlaybookSynthesizer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()
	s.wg.Wait()
}

// Threshold returns the current synthesis threshold.
func (s *PlaybookSynthesizer) Threshold() int {
	return s.cfg.Threshold
}

// OnExecutionRecorded is the hook the broker calls after every append to
// the execution log. It fetches the current execution count, compares
// against the last-synthesized count in the playbook frontmatter, and
// enqueues a synthesis if the delta meets the threshold.
//
// Non-blocking: all work happens inline but returns immediately; errors
// are logged and swallowed so the caller's /playbook/execution handler
// stays fast.
func (s *PlaybookSynthesizer) OnExecutionRecorded(slug string) {
	if s == nil {
		return
	}
	// Count all executions. Compare against fact_count_at_synthesis in the
	// playbook frontmatter — same mechanism entity briefs use.
	all, err := s.execLog.List(slug)
	if err != nil {
		log.Printf("playbook synth: list executions for %s: %v", slug, err)
		return
	}
	total := len(all)
	_, _, lastCount := s.readSynthesisCounters(slug)
	since := total - lastCount
	if since < 0 {
		since = 0
	}
	if since < s.cfg.Threshold {
		return
	}
	if _, err := s.EnqueueSynthesis(slug, ArchivistAuthor, false); err != nil && !errors.Is(err, ErrPlaybookSynthQueueSaturated) {
		// Coalesced returns (0, nil); saturation is a soft error. Everything
		// else surfaces a real bug — log so ops can see it.
		log.Printf("playbook synth: enqueue after threshold for %s: %v", slug, err)
	}
}

// SynthesizeNow runs a synthesis for the given slug synchronously. Used by
// the POST /playbook/synthesize endpoint and the MCP tool for on-demand
// refresh. Returns once the commit is in the wiki queue (not when it is
// written — the caller can read-back to confirm).
func (s *PlaybookSynthesizer) SynthesizeNow(ctx context.Context, slug, actor string) (uint64, error) {
	return s.EnqueueSynthesis(slug, strings.TrimSpace(actor), true)
}

// EnqueueSynthesis adds a synthesis job if none is already in-flight or
// queued for the same slug. Returns the assigned job ID (or 0 when the
// request was coalesced).
func (s *PlaybookSynthesizer) EnqueueSynthesis(slug, requestBy string, triggeredByUser bool) (uint64, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return 0, ErrPlaybookSynthesizerStopped
	}
	if s.queued[slug] {
		s.mu.Unlock()
		return 0, nil
	}
	if s.inflight[slug] {
		s.queued[slug] = true
		s.mu.Unlock()
		return 0, nil
	}
	s.nextID++
	id := s.nextID
	job := PlaybookSynthesisJob{
		Slug:       slug,
		RequestBy:  strings.TrimSpace(requestBy),
		EnqueuedAt: time.Now().UTC(),
		ID:         id,
	}
	s.queued[slug] = true
	s.mu.Unlock()

	select {
	case s.jobs <- job:
		// Record whether this was an on-demand request so the SSE event
		// can surface it. Stored on a side map to avoid plumbing another
		// field through the channel type.
		s.markTriggerSource(slug, triggeredByUser)
		return id, nil
	default:
		s.mu.Lock()
		delete(s.queued, slug)
		s.mu.Unlock()
		return 0, ErrPlaybookSynthQueueSaturated
	}
}

// triggerSources tracks whether the currently-queued job for each slug was
// triggered by a user (true) vs. auto-threshold (false). Read once when the
// job pops off the queue; deleted after the job finishes. No lock of its
// own — we reuse s.mu to keep the hot paths aligned.
var playbookTriggerSources = struct {
	mu  sync.Mutex
	src map[string]bool
}{src: map[string]bool{}}

func (s *PlaybookSynthesizer) markTriggerSource(slug string, user bool) {
	playbookTriggerSources.mu.Lock()
	defer playbookTriggerSources.mu.Unlock()
	// OR-reduce: if any pending enqueue was user-triggered, treat the
	// coalesced synthesis as user-triggered.
	playbookTriggerSources.src[slug] = playbookTriggerSources.src[slug] || user
}

func (s *PlaybookSynthesizer) consumeTriggerSource(slug string) bool {
	playbookTriggerSources.mu.Lock()
	defer playbookTriggerSources.mu.Unlock()
	v := playbookTriggerSources.src[slug]
	delete(playbookTriggerSources.src, slug)
	return v
}

// drain is the single synthesis worker goroutine.
func (s *PlaybookSynthesizer) drain(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case job := <-s.jobs:
			s.runJob(ctx, job)
		}
	}
}

// runJob is the per-slug serialized synthesis. Mirrors
// EntitySynthesizer.runJob — mark inflight, run, schedule any coalesced
// follow-up after we finish.
func (s *PlaybookSynthesizer) runJob(ctx context.Context, job PlaybookSynthesisJob) {
	s.mu.Lock()
	s.inflight[job.Slug] = true
	delete(s.queued, job.Slug)
	s.mu.Unlock()

	userTriggered := s.consumeTriggerSource(job.Slug)

	defer func() {
		s.mu.Lock()
		delete(s.inflight, job.Slug)
		needsFollowup := s.queued[job.Slug]
		// Drop the queued flag BEFORE the follow-up goroutine re-enqueues;
		// otherwise it sees queued=true and silently coalesces its own call.
		delete(s.queued, job.Slug)
		running := s.running
		s.mu.Unlock()
		if needsFollowup && running {
			// Track on s.wg so Stop() waits; otherwise the orphan goroutine
			// can outlive the test's TempDir and race filesystem cleanup.
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				select {
				case <-time.After(10 * time.Millisecond):
				case <-s.stopCh:
					return
				}
				_, _ = s.EnqueueSynthesis(job.Slug, ArchivistAuthor, false)
			}()
		}
	}()

	if err := s.synthesize(ctx, job, userTriggered); err != nil {
		if errors.Is(err, ErrPlaybookSynthNoNewExecutions) || errors.Is(err, ErrPlaybookSourceMissing) {
			return
		}
		log.Printf("playbook synth: %s failed: %v", job.Slug, err)
	}
}

// synthesize runs the full pipeline for one job.
func (s *PlaybookSynthesizer) synthesize(ctx context.Context, job PlaybookSynthesisJob, userTriggered bool) error {
	sourceRel := playbookSourceRel(job.Slug)
	source, hadSource := s.readArticle(sourceRel)
	if !hadSource {
		return ErrPlaybookSourceMissing
	}

	all, err := s.execLog.List(job.Slug)
	if err != nil {
		return fmt.Errorf("list executions: %w", err)
	}
	total := len(all)
	_, _, lastCount := parseSynthesisFrontmatter(source)
	since := total - lastCount
	if since < 0 {
		since = 0
	}
	if since == 0 && total > 0 {
		return ErrPlaybookSynthNoNewExecutions
	}
	if total == 0 {
		return ErrPlaybookSynthNoNewExecutions
	}

	// Slice the most recent MaxExecutionsForPrompt entries. List returns
	// newest-first, so a simple head-slice is the right window.
	window := all
	if len(window) > MaxExecutionsForPrompt {
		window = window[:MaxExecutionsForPrompt]
	}

	userPrompt := buildPlaybookSynthUserPrompt(source, window)

	callCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	llm := s.cfg.LLMCall
	if llm == nil {
		llm = defaultLLMCall
	}
	output, llmErr := llm(callCtx, PlaybookSynthesisPromptSystem, userPrompt)
	if llmErr != nil {
		return fmt.Errorf("llm: %w", llmErr)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("llm output is empty")
	}
	if len(output) > MaxPlaybookBodySize {
		return fmt.Errorf("llm output exceeds %d bytes (got %d)", MaxPlaybookBodySize, len(output))
	}
	// Weak tamper check — drop obvious prompt-echo failures.
	if strings.Contains(output, "# Existing playbook") && strings.Contains(output, "# Recent executions") {
		return fmt.Errorf("llm output appears to contain the prompt verbatim")
	}

	// Safety net: enforce author-body preservation + frontmatter preservation.
	// If the LLM rewrote the main body, reconstruct the file ourselves by
	// replacing only the "What we've learned" trailing section from the LLM's
	// draft and splicing it onto the original author body. This is defense in
	// depth — the prompt already instructs the model not to rewrite — but we
	// do NOT want silent body-rewrites slipping through.
	safeBody, ok := mergePreservingAuthorBody(source, output)
	if !ok {
		return fmt.Errorf("llm output missing required %q heading", WhatWeveLearnedHeading)
	}

	// Stamp frontmatter counters so the next run knows what is new.
	headSHA, headErr := s.headSHA(ctx)
	if headErr != nil {
		log.Printf("playbook synth: resolve HEAD failed for %s: %v", job.Slug, headErr)
	}
	now := time.Now().UTC()
	stamped := applySynthesisFrontmatter(safeBody, headSHA, now, total, source)

	// Commit via the standard wiki queue. The post-commit auto-recompile hook
	// (wiki_worker.go) detects team/playbooks/{slug}.md and regenerates the
	// SKILL.md automatically — we deliberately do not call CompilePlaybook.
	commitMsg := fmt.Sprintf("archivist: synthesize learnings into playbook %s (%d executions)", job.Slug, total)
	sha, _, werr := s.worker.Enqueue(ctx, ArchivistAuthor, sourceRel, stamped, "replace", commitMsg)
	if werr != nil {
		return fmt.Errorf("commit playbook: %w", werr)
	}

	if s.publisher != nil {
		s.publisher.PublishPlaybookSynthesized(PlaybookSynthesizedEvent{
			Slug:            job.Slug,
			CommitSHA:       sha,
			ExecutionCount:  total,
			SynthesizedTS:   now.Format(time.RFC3339),
			SourcePath:      sourceRel,
			TriggeredByUser: userTriggered,
		})
	}
	return nil
}

// readArticle returns the article bytes + whether the file exists.
func (s *PlaybookSynthesizer) readArticle(relPath string) (string, bool) {
	bytes, err := readArticle(s.worker.Repo(), relPath)
	if err != nil {
		return "", false
	}
	return string(bytes), true
}

// readSynthesisCounters returns the sha/ts/fact-count from the source
// playbook frontmatter. Reuses the entity_frontmatter.go helpers — the
// schema (last_synthesized_sha/ts + fact_count_at_synthesis) is identical.
func (s *PlaybookSynthesizer) readSynthesisCounters(slug string) (string, time.Time, int) {
	body, ok := s.readArticle(playbookSourceRel(slug))
	if !ok {
		return "", time.Time{}, 0
	}
	return parseSynthesisFrontmatter(body)
}

// headSHA returns the current repo HEAD short SHA. Same shape as
// EntitySynthesizer.headSHA.
func (s *PlaybookSynthesizer) headSHA(ctx context.Context) (string, error) {
	repo := s.worker.Repo()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out, err := repo.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// buildPlaybookSynthUserPrompt formats the LLM user message. Keeps the
// bounds explicit and the section headers stable so the response can be
// parsed reliably downstream.
func buildPlaybookSynthUserPrompt(source string, execs []Execution) string {
	var b strings.Builder
	b.WriteString("# Existing playbook\n\n")
	b.WriteString(strings.TrimSpace(source))
	b.WriteString("\n\n# Recent executions (newest first, max ")
	fmt.Fprintf(&b, "%d", MaxExecutionsForPrompt)
	b.WriteString(")\n\n")
	if len(execs) == 0 {
		b.WriteString("_No executions provided._\n")
	} else {
		for i, e := range execs {
			ts := e.CreatedAt.UTC().Format(time.RFC3339)
			fmt.Fprintf(&b, "## Execution %d — %s\n", i+1, e.Outcome)
			fmt.Fprintf(&b, "- recorded_by: %s\n", e.RecordedBy)
			fmt.Fprintf(&b, "- timestamp: %s\n", ts)
			fmt.Fprintf(&b, "- summary: %s\n", oneLine(e.Summary))
			if strings.TrimSpace(e.Notes) != "" {
				fmt.Fprintf(&b, "- notes: %s\n", oneLine(e.Notes))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("# Your task\n\n")
	b.WriteString("Produce the FULL updated playbook markdown now, preserving the frontmatter and the author's body verbatim, and appending or updating the trailing \"")
	b.WriteString(WhatWeveLearnedHeading)
	b.WriteString("\" section with lessons drawn from the executions above.")
	return b.String()
}

func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}

// mergePreservingAuthorBody takes the original source and the LLM draft and
// returns a merged markdown file where:
//   - frontmatter is taken from the ORIGINAL source
//   - body above WhatWeveLearnedHeading is taken from the ORIGINAL source
//   - WhatWeveLearnedHeading + everything below is taken from the LLM DRAFT
//
// Returns (result, true) on success. Returns ("", false) when the LLM draft
// does not contain the required heading — the caller treats that as a
// hard synthesis failure (we refuse to commit an output that didn't produce
// learnings).
//
// Byte-identical input produces byte-identical output, so re-synthesizing a
// stable playbook is a no-op commit.
func mergePreservingAuthorBody(source, draft string) (string, bool) {
	heading := WhatWeveLearnedHeading
	// Split source into (frontmatter-and-body-above, existing-learnings).
	// The author body above the heading is authoritative; existing learnings
	// are replaceable.
	origAbove := splitAboveLearnings(source)

	// The LLM draft might include a frontmatter we want to throw away, plus
	// a body that should have the heading. Strip any frontmatter first so
	// we don't search inside it for the heading.
	draftBody := stripFrontmatter(draft)
	idx := strings.Index(draftBody, heading)
	if idx < 0 {
		return "", false
	}
	newLearnings := strings.TrimLeft(draftBody[idx:], " \t")

	// Rejoin: frontmatter+body-above (with trailing newline separation) + new learnings.
	above := strings.TrimRight(origAbove, "\n")
	return above + "\n\n" + strings.TrimRight(newLearnings, " \t\n") + "\n", true
}

// splitAboveLearnings returns everything in source strictly above the
// WhatWeveLearnedHeading, including the frontmatter. When the heading is
// absent the whole source is considered "above".
func splitAboveLearnings(source string) string {
	// Consider only the body for heading detection — searching the raw
	// source would match a heading that appeared inside frontmatter yaml
	// comments (rare but possible).
	fm, body := splitFrontmatterAndBody(source)
	idx := strings.Index(body, WhatWeveLearnedHeading)
	if idx < 0 {
		return source
	}
	above := body[:idx]
	if fm == "" {
		return above
	}
	return fm + above
}

// splitFrontmatterAndBody returns (frontmatter-including-delimiters, body).
// When there is no frontmatter, ("", source) is returned.
func splitFrontmatterAndBody(source string) (string, string) {
	if !strings.HasPrefix(source, "---\n") {
		return "", source
	}
	rest := source[len("---\n"):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", source
	}
	// idx is start of "\n---"; closing line may end with "\n" or be the final line.
	closing := idx + len("\n---")
	tail := rest[closing:]
	// Skip the immediate newline after the closing --- so the body starts on
	// a clean line, but preserve leading blank lines beyond that.
	tail = strings.TrimPrefix(tail, "\n")
	fm := "---\n" + rest[:closing] + "\n"
	return fm, tail
}
