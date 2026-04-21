package team

// entity_synthesizer.go is the broker-level LLM synthesis worker for v1.2
// entity briefs.
//
// Design summary (see project_entity_briefs_v1_2.md):
//   - Synthesis is NOT an agent turn. It runs inside the broker as a
//     dedicated goroutine consuming a buffered SynthesisJob channel.
//   - The worker shells out to the user's configured CLI (claude-code,
//     codex, openclaw, ...) through provider.RunConfiguredOneShot so we
//     never carry an LLM SDK in the broker binary.
//   - Output is committed via the WikiWorker queue under the synthetic
//     "archivist" git identity — preserving the single-writer invariant
//     and attribution semantics that rest of the wiki uses.
//   - The worker coalesces re-synth requests per-entity. If a fact lands
//     while a synthesis is running for the same entity, exactly one
//     follow-up synthesis is queued — not one per new fact.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// defaultLLMCall shells out to the user's configured LLM CLI.
// Extracted so tests can replace it via SynthesizerConfig.LLMCall.
func defaultLLMCall(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	// RunConfiguredOneShot does not take a context; we rely on the OS's
	// process group cleanup when our ctx deadline fires. The synthesize()
	// layer owns the deadline via context.WithTimeout.
	_ = ctx
	return provider.RunConfiguredOneShot(systemPrompt, userPrompt, "")
}

// ArchivistAuthor is the synthetic commit author for every brief update.
// Not a roster member — pure git identity.
const ArchivistAuthor = "archivist"

// DefaultSynthesisThreshold is the number of new facts that must accumulate
// before an automatic synthesis is triggered. Configurable per deployment
// via WUPHF_ENTITY_BRIEF_THRESHOLD.
const DefaultSynthesisThreshold = 5

// DefaultSynthesisTimeout bounds a single LLM shell-out. Configurable via
// WUPHF_ENTITY_BRIEF_TIMEOUT (seconds).
const DefaultSynthesisTimeout = 30 * time.Second

// MaxSynthesisQueue is the buffered channel size for pending jobs. Overflow
// surfaces ErrSynthesisQueueSaturated.
const MaxSynthesisQueue = 32

// MaxBriefSize caps the LLM output bytes we are willing to commit. Any
// larger response is treated as a malformed synthesis and dropped.
const MaxBriefSize = 32 * 1024

// SynthesisPromptSystem is the exact system prompt the worker sends. Wording
// locked by project_entity_briefs_v1_2.md — do not edit without updating
// the memo.
const SynthesisPromptSystem = `You maintain entity briefs in a team wiki. Given an existing brief and new facts, produce an updated markdown brief that incorporates the facts. Never invent facts. Preserve the canonical structure (sections, ordering). Mark contradictions explicitly with **Contradiction:** inline callouts rather than resolving them. Output ONLY the updated markdown, no explanation.`

// ErrSynthesisQueueSaturated is returned by EnqueueSynthesis when the
// buffered channel is full. Callers surface this as a retry-later.
var ErrSynthesisQueueSaturated = errors.New("entity synth: queue saturated")

// ErrSynthesizerStopped is returned when EnqueueSynthesis is called after
// the worker has been stopped.
var ErrSynthesizerStopped = errors.New("entity synth: not running")

// ErrSynthesisNoNewFacts is surfaced for observability when a job runs with
// zero un-synthesized facts. Not a hard failure — the job simply skips.
var ErrSynthesisNoNewFacts = errors.New("entity synth: no new facts since last synthesis")

// SynthesisJob is one pending synthesis request for a specific entity.
type SynthesisJob struct {
	Kind       EntityKind
	Slug       string
	RequestBy  string
	EnqueuedAt time.Time
	// ID is a monotonic counter so callers can correlate responses.
	ID uint64
}

// EntityBriefSynthesizedEvent is the SSE payload broadcast after every
// successful synthesis commit.
type EntityBriefSynthesizedEvent struct {
	Kind           EntityKind `json:"kind"`
	Slug           EntitySlug `json:"slug"`
	CommitSHA      string     `json:"commit_sha"`
	FactCount      int        `json:"fact_count"`
	SynthesizedTS  string     `json:"synthesized_ts"`
}

// EntitySlug is a typed alias. Helps readers of the SSE JSON schema; string
// at the wire level.
type EntitySlug = string

// EntityFactRecordedEvent is the SSE payload broadcast when a fact lands.
type EntityFactRecordedEvent struct {
	Kind             EntityKind `json:"kind"`
	Slug             string     `json:"slug"`
	FactID           string     `json:"fact_id"`
	RecordedBy       string     `json:"recorded_by"`
	FactCount        int        `json:"fact_count"`
	ThresholdCrossed bool       `json:"threshold_crossed"`
	Timestamp        string     `json:"timestamp"`
}

// SynthesizerConfig is the tunable knobs for the worker. All fields are
// optional; defaults match constants above.
type SynthesizerConfig struct {
	Provider  string
	Threshold int
	Timeout   time.Duration

	// LLMCall is the pluggable shell-out used by tests. Production code
	// leaves this nil and the worker falls back to provider.RunConfiguredOneShot.
	LLMCall func(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// EntitySynthesizer is the broker-level synthesis worker.
type EntitySynthesizer struct {
	worker    *WikiWorker
	factLog   *FactLog
	publisher entityEventPublisher
	cfg       SynthesizerConfig

	mu       sync.Mutex
	jobs     chan SynthesisJob
	inflight map[string]bool // key=kind/slug — at most 1 running per entity
	queued   map[string]bool // key=kind/slug — at most 1 pending per entity
	running  bool
	nextID   uint64
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// entityEventPublisher is the subset of Broker the synthesizer needs to
// fan out entity-scoped SSE events.
type entityEventPublisher interface {
	PublishEntityBriefSynthesized(evt EntityBriefSynthesizedEvent)
	PublishEntityFactRecorded(evt EntityFactRecordedEvent)
}

// NewEntitySynthesizer wires a synthesizer against the given worker + fact
// log. Config may be the zero value; defaults are filled in here.
func NewEntitySynthesizer(worker *WikiWorker, factLog *FactLog, publisher entityEventPublisher, cfg SynthesizerConfig) *EntitySynthesizer {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultSynthesisThreshold
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultSynthesisTimeout
	}
	return &EntitySynthesizer{
		worker:    worker,
		factLog:   factLog,
		publisher: publisher,
		cfg:       cfg,
		jobs:      make(chan SynthesisJob, MaxSynthesisQueue),
		inflight:  make(map[string]bool),
		queued:    make(map[string]bool),
	}
}

// Start launches the synthesis loop. Returns immediately. Stop via the ctx
// or via Stop().
func (s *EntitySynthesizer) Start(ctx context.Context) {
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

// Stop signals the worker to exit. Pending jobs in the buffered channel are
// discarded — caller is responsible for only calling this at shutdown.
func (s *EntitySynthesizer) Stop() {
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
func (s *EntitySynthesizer) Threshold() int {
	return s.cfg.Threshold
}

// entityKey is the coalesce key used by inflight/queued maps.
func entityKey(kind EntityKind, slug string) string {
	return string(kind) + "/" + slug
}

// EnqueueSynthesis adds a synthesis job if none is already in-flight or
// queued for the same entity. Returns the assigned job ID (or 0 when the
// request was coalesced into an existing queue entry).
func (s *EntitySynthesizer) EnqueueSynthesis(kind EntityKind, slug, requestBy string) (uint64, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return 0, ErrSynthesizerStopped
	}
	key := entityKey(kind, slug)
	// If another job is queued for the same entity, coalesce silently.
	if s.queued[key] {
		s.mu.Unlock()
		return 0, nil
	}
	// If a job is in-flight for the same entity, mark a single follow-up
	// as queued — the drain loop will schedule it after the current run.
	if s.inflight[key] {
		s.queued[key] = true
		s.mu.Unlock()
		return 0, nil
	}
	s.nextID++
	id := s.nextID
	job := SynthesisJob{
		Kind:       kind,
		Slug:       slug,
		RequestBy:  strings.TrimSpace(requestBy),
		EnqueuedAt: time.Now().UTC(),
		ID:         id,
	}
	s.queued[key] = true
	s.mu.Unlock()

	select {
	case s.jobs <- job:
		return id, nil
	default:
		// Queue saturated — undo the reservation so future calls can retry.
		s.mu.Lock()
		delete(s.queued, key)
		s.mu.Unlock()
		return 0, ErrSynthesisQueueSaturated
	}
}

// drain is the single synthesis worker goroutine. Runs exactly one job at
// a time so the WikiWorker queue never has two archivist writes racing.
func (s *EntitySynthesizer) drain(ctx context.Context) {
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

// runJob is the per-entity serialized synthesis. Marks the entity as
// in-flight, transitions queued->idle->inflight, and after completion
// schedules any coalesced follow-up.
func (s *EntitySynthesizer) runJob(ctx context.Context, job SynthesisJob) {
	key := entityKey(job.Kind, job.Slug)

	s.mu.Lock()
	s.inflight[key] = true
	delete(s.queued, key)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		needsFollowup := s.queued[key]
		running := s.running
		s.mu.Unlock()
		// A fact landed during this run. Enqueue a follow-up IF the
		// synthesizer is still running — Stop() may have raced with the
		// append, and we don't want to block on a closed channel.
		if needsFollowup && running {
			// Use a background context for the re-schedule — the caller's
			// context has already returned. The follow-up will run on the
			// next drain iteration.
			go func() {
				// Small delay so bursts of facts get coalesced further
				// instead of tail-biting the follow-up immediately.
				time.Sleep(10 * time.Millisecond)
				_, _ = s.EnqueueSynthesis(job.Kind, job.Slug, ArchivistAuthor)
			}()
		}
	}()

	if err := s.synthesize(ctx, job); err != nil {
		if errors.Is(err, ErrSynthesisNoNewFacts) {
			// Idempotent skip — not a failure.
			return
		}
		log.Printf("entity synth: %s/%s failed: %v", job.Kind, job.Slug, err)
	}
}

// synthesize runs the full pipeline for one job. Errors here are logged by
// runJob; we return them for tests.
func (s *EntitySynthesizer) synthesize(ctx context.Context, job SynthesisJob) error {
	relBrief := briefPath(job.Kind, job.Slug)
	existingBrief, hadBrief := s.readBrief(relBrief)
	lastSHA, _, _ := parseSynthesisFrontmatter(existingBrief)

	facts, err := s.factLog.List(job.Kind, job.Slug)
	if err != nil {
		return fmt.Errorf("list facts: %w", err)
	}
	// Facts since last synthesis.
	newFacts := facts
	if lastSHA != "" {
		if ts, tsErr := s.factLog.commitTimestamp(ctx, lastSHA); tsErr == nil {
			newFacts = facts[:0:0]
			for _, f := range facts {
				if f.CreatedAt.After(ts) {
					newFacts = append(newFacts, f)
				}
			}
		}
	}
	if len(newFacts) == 0 && hadBrief {
		return ErrSynthesisNoNewFacts
	}
	if len(facts) == 0 && !hadBrief {
		return ErrSynthesisNoNewFacts
	}

	// Build prompt.
	factListBody := renderFactsForPrompt(newFacts)
	userPrompt := fmt.Sprintf(
		"# Existing brief\n\n%s\n\n# New facts since last synthesis\n\n%s\n\n# Your task\nProduce the full updated brief markdown now.",
		strings.TrimSpace(stripFrontmatter(existingBrief)),
		factListBody,
	)

	// Shell out with a bounded timeout.
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	llm := s.cfg.LLMCall
	if llm == nil {
		llm = defaultLLMCall
	}
	output, llmErr := llm(callCtx, SynthesisPromptSystem, userPrompt)
	if llmErr != nil {
		return fmt.Errorf("llm: %w", llmErr)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("llm output is empty")
	}
	if len(output) > MaxBriefSize {
		return fmt.Errorf("llm output exceeds %d bytes (got %d)", MaxBriefSize, len(output))
	}
	// Very weak tamper check — prompt leakage means the LLM echoed us
	// back. Drop the result rather than commit garbage.
	if strings.Contains(output, "# Your task") && strings.Contains(output, "# New facts since last synthesis") {
		return fmt.Errorf("llm output appears to contain the prompt verbatim")
	}

	// Resolve current HEAD sha so we stamp the frontmatter with the commit
	// that existed BEFORE we add the synthesis commit. This lets the next
	// run know exactly which facts it hasn't seen yet.
	headSHA, headErr := s.headSHA(ctx)
	if headErr != nil {
		// Non-fatal — the brief will just re-count every fact next time.
		log.Printf("entity synth: resolve HEAD failed: %v", headErr)
	}

	now := time.Now().UTC()
	factCount := len(facts)
	newBody := applySynthesisFrontmatter(output, headSHA, now, factCount, existingBrief)

	// Commit via the wiki queue under the archivist identity. We can't
	// call CommitBootstrap because that's tree-wide + picks its own slug;
	// we need an explicit slug commit. Enqueue does exactly that.
	commitMsg := fmt.Sprintf("archivist: update %s/%s brief (%d facts)", job.Kind, job.Slug, factCount)
	sha, _, werr := s.worker.Enqueue(ctx, ArchivistAuthor, relBrief, newBody, "replace", commitMsg)
	if werr != nil {
		return fmt.Errorf("commit brief: %w", werr)
	}

	if s.publisher != nil {
		s.publisher.PublishEntityBriefSynthesized(EntityBriefSynthesizedEvent{
			Kind:          job.Kind,
			Slug:          job.Slug,
			CommitSHA:     sha,
			FactCount:     factCount,
			SynthesizedTS: now.Format(time.RFC3339),
		})
	}
	return nil
}

// readBrief returns the existing brief bytes (string) and whether a file
// was present.
func (s *EntitySynthesizer) readBrief(relPath string) (string, bool) {
	repo := s.worker.Repo()
	bytes, err := readArticle(repo, relPath)
	if err != nil {
		return "", false
	}
	return string(bytes), true
}

// headSHA returns the current repo HEAD short SHA.
func (s *EntitySynthesizer) headSHA(ctx context.Context) (string, error) {
	repo := s.worker.Repo()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out, err := repo.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// briefPath resolves the canonical wiki path for an entity brief.
func briefPath(kind EntityKind, slug string) string {
	return filepath.ToSlash(filepath.Join("team", string(kind), slug+".md"))
}

// renderFactsForPrompt formats the new facts as a bulleted list the LLM
// can read without ambiguity.
func renderFactsForPrompt(facts []Fact) string {
	if len(facts) == 0 {
		return "_No new facts._"
	}
	var b strings.Builder
	for _, f := range facts {
		ts := f.CreatedAt.UTC().Format(time.RFC3339)
		line := fmt.Sprintf("- [%s] recorded by %s", ts, f.RecordedBy)
		if f.SourcePath != "" {
			line += fmt.Sprintf(" (source: %s)", f.SourcePath)
		}
		line += ": " + strings.ReplaceAll(f.Text, "\n", " ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// --- frontmatter helpers ---------------------------------------------------

// synthesisFrontmatterPattern captures the 3 keys we own. Other keys
// (title:, tags:, ...) are preserved untouched.
var (
	lastSHAKey = "last_synthesized_sha"
	lastTSKey  = "last_synthesized_ts"
	factCntKey = "fact_count_at_synthesis"
)

var frontmatterKeyLine = regexp.MustCompile(`(?m)^([a-zA-Z0-9_]+):\s*(.*)$`)

// parseSynthesisFrontmatter extracts the synthesis keys from the existing
// brief. Missing keys yield zero values.
func parseSynthesisFrontmatter(brief string) (sha string, ts time.Time, factCount int) {
	if !strings.HasPrefix(brief, "---\n") {
		return "", time.Time{}, 0
	}
	rest := brief[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", time.Time{}, 0
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		val := strings.TrimSpace(m[2])
		switch key {
		case lastSHAKey:
			sha = val
		case lastTSKey:
			if parsed, err := time.Parse(time.RFC3339, val); err == nil {
				ts = parsed
			}
		case factCntKey:
			factCount = parseInt(val)
		}
	}
	return sha, ts, factCount
}

// applySynthesisFrontmatter merges the three synthesis keys onto the LLM
// output. When the output already has a frontmatter block we update those
// keys in-place; otherwise we prepend a fresh block that also preserves
// any non-synthesis keys from the prior brief.
func applySynthesisFrontmatter(body, headSHA string, ts time.Time, factCount int, prior string) string {
	tsStr := ts.UTC().Format(time.RFC3339)
	ours := map[string]string{
		lastSHAKey: headSHA,
		lastTSKey:  tsStr,
		factCntKey: fmt.Sprintf("%d", factCount),
	}

	preserved := preservedFrontmatterKeys(prior)

	hasFrontmatter := strings.HasPrefix(body, "---\n")
	if !hasFrontmatter {
		var b strings.Builder
		b.WriteString("---\n")
		for _, key := range orderedFrontmatterKeys() {
			if v, ok := ours[key]; ok {
				b.WriteString(key)
				b.WriteString(": ")
				b.WriteString(v)
				b.WriteString("\n")
			}
		}
		for _, kv := range preserved {
			// Skip keys we already wrote ourselves.
			if _, mine := ours[kv.key]; mine {
				continue
			}
			b.WriteString(kv.key)
			b.WriteString(": ")
			b.WriteString(kv.val)
			b.WriteString("\n")
		}
		b.WriteString("---\n\n")
		b.WriteString(body)
		return b.String()
	}

	// Body HAS frontmatter. Rewrite only our keys + append missing ones.
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		// Malformed — fall back to prepend path.
		return applySynthesisFrontmatter(stripFrontmatter(body), headSHA, ts, factCount, prior)
	}
	block := rest[:end]
	tail := rest[end+len("\n---"):]
	lines := strings.Split(block, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		if v, ok := ours[key]; ok {
			lines[i] = key + ": " + v
			seen[key] = true
		}
	}
	for _, key := range orderedFrontmatterKeys() {
		if !seen[key] {
			lines = append(lines, key+": "+ours[key])
		}
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n---")
	b.WriteString(tail)
	return b.String()
}

// orderedFrontmatterKeys returns the synthesis keys in a deterministic
// order so commits don't churn on reorderings.
func orderedFrontmatterKeys() []string {
	return []string{lastSHAKey, lastTSKey, factCntKey}
}

type frontmatterKV struct {
	key string
	val string
}

// preservedFrontmatterKeys lifts non-synthesis keys from a prior brief so
// we don't lose custom frontmatter when we rewrite from scratch.
func preservedFrontmatterKeys(prior string) []frontmatterKV {
	if !strings.HasPrefix(prior, "---\n") {
		return nil
	}
	rest := prior[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil
	}
	block := rest[:end]
	var out []frontmatterKV
	for _, line := range strings.Split(block, "\n") {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		switch key {
		case lastSHAKey, lastTSKey, factCntKey:
			continue
		}
		out = append(out, frontmatterKV{key: key, val: strings.TrimSpace(m[2])})
	}
	return out
}

func parseInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
