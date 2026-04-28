package team

// skill_scanner.go implements the LLM-gated wiki scanner that emits skill
// proposals from team/**/*.md articles. Per the Eng Review Stage A reframe
// (2026-04-28) this is NOT a heuristic — every candidate article is sent to
// an LLM provider with the skill-creator.md system prompt and the LLM decides
// whether the article is a reusable, agent-callable skill.

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultSkillCreatorPromptEmbedded is the fallback skill-creator system prompt
// used when team/skills/.system/skill-creator.md is missing from the wiki. It
// ships with the binary so a fresh wiki can compile skills before the migration
// has had a chance to seed the system prompt.
//
//go:embed prompts/skill_creator_default.md
var defaultSkillCreatorPromptEmbedded string

// scanWalkRoot is the wiki subtree the scanner walks. Per Codex T1 (Eng Review
// Section A, factual bug 1) this is `team/`, NOT `team/wiki/`.
const scanWalkRoot = "team"

// scanSkippedDirs lists prefix paths the scanner must skip. Each entry is a
// wiki-relative path (no leading slash, forward slashes). The scanner short
// circuits a path if it falls under any of these prefixes.
var scanSkippedDirs = []string{
	"team/skills/", // already-compiled skills + .system/ prompt + .rejected.md tombstone
	"team/playbooks/.compiled/",
}

// scanAgentNotebookSegment is the path segment that identifies per-agent
// notebooks (team/agents/*/notebook/). Notebook articles are scratch space —
// scanning them would create noise. Promotion is gated by the notebook→wiki
// review flow, not the scanner.
const scanAgentNotebookSegment = "/notebook/"

// agentsDirPrefix is the prefix under which per-agent notebooks live.
const agentsDirPrefix = "team/agents/"

// llmProvider is the small interface the scanner uses to ask an LLM whether
// an article describes a reusable skill, and if so, what the skill's slug,
// description, and body should be. Defined where it is consumed per the
// "accept interfaces, return structs" idiom.
type llmProvider interface {
	AskIsSkill(ctx context.Context, articlePath, articleContent string) (isSkill bool, fm SkillFrontmatter, body string, err error)
}

// SkillSpec is the canonical in-memory representation the scanner produces
// before handing off to writeSkillProposalLocked. It bundles the parsed
// frontmatter, the body, and the source article path for provenance.
type SkillSpec struct {
	Frontmatter   SkillFrontmatter
	Body          string
	SourceArticle string
}

// ScanError records a single per-article failure during a scan pass. The
// caller decides whether to surface or aggregate; the scanner never panics.
type ScanError struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

// ScanResult is the JSON-serializable summary of a single scan pass. Counts
// are intentionally additive: callers can sum results across passes for
// telemetry.
type ScanResult struct {
	Scanned         int         `json:"scanned"`
	Matched         int         `json:"matched"`
	Proposed        int         `json:"proposed"`
	Deduped         int         `json:"deduped"`
	RejectedByGuard int         `json:"rejected_by_guard"`
	Errors          []ScanError `json:"errors,omitempty"`
	DurationMs      int64       `json:"duration_ms"`
	Trigger         string      `json:"trigger"`
}

// SkillScanner walks the wiki under team/, asks the LLM to classify each
// article, and writes proposals through the broker's funnel.
type SkillScanner struct {
	broker        *Broker
	provider      llmProvider
	budgetPerPass int

	mu         sync.Mutex
	mtimeCache map[string]string // wiki-relative path -> sha256 of content (hex)
}

// NewSkillScanner constructs a scanner. budget is the maximum number of LLM
// calls per pass — guards against runaway spend. Callers may pass 0 to use
// a reasonable default (see defaultSkillCompileBudget).
func NewSkillScanner(b *Broker, provider llmProvider, budget int) *SkillScanner {
	if budget <= 0 {
		budget = defaultSkillCompileBudget
	}
	return &SkillScanner{
		broker:        b,
		provider:      provider,
		budgetPerPass: budget,
		mtimeCache:    make(map[string]string),
	}
}

// defaultSkillCompileBudget caps LLM calls per pass when the caller does not
// supply a budget. 50 covers the design's "Cap WUPHF_SKILL_COMPILE_TICK_BUDGET
// at 50 per tick" decision (Section D, decision 8).
const defaultSkillCompileBudget = 50

// Scan walks the wiki under team/ (or scopePath if non-empty), asks the LLM
// for each candidate, and writes proposals through writeSkillProposalLocked.
// scopePath is wiki-relative (e.g. "team/customers"). Empty scans the full
// team subtree. dryRun=true performs the LLM classification but skips the
// actual proposal write.
func (s *SkillScanner) Scan(ctx context.Context, scopePath string, dryRun bool, trigger string) (ScanResult, error) {
	start := time.Now()
	res := ScanResult{Trigger: trigger}

	if s.broker == nil {
		return res, errors.New("skill_scanner: broker is nil")
	}
	if s.provider == nil {
		return res, errors.New("skill_scanner: llm provider is nil")
	}

	// Resolve the on-disk wiki root via the broker's wiki worker. If the
	// markdown backend is not initialised (no git wiki), there is nothing to
	// scan and we return cleanly.
	wikiRoot, err := s.resolveWikiRoot()
	if err != nil {
		return res, err
	}
	if wikiRoot == "" {
		slog.Info("skill_scanner: wiki worker not initialised, skipping scan")
		res.DurationMs = time.Since(start).Milliseconds()
		return res, nil
	}

	// Load tombstone under b.mu so a concurrent rejection writer can't
	// mutate the slice while we're iterating.
	tombstoneSlugs, tombstoneSources := s.loadTombstone()

	// Walk root: either the full team subtree or the requested scope.
	walkRoot := filepath.Join(wikiRoot, scanWalkRoot)
	if strings.TrimSpace(scopePath) != "" {
		// Sanitize scopePath: must stay under team/.
		clean := filepath.Clean(strings.TrimPrefix(strings.TrimSpace(scopePath), "/"))
		if !strings.HasPrefix(clean, scanWalkRoot) {
			return res, fmt.Errorf("skill_scanner: scope_path %q must be under team/", scopePath)
		}
		walkRoot = filepath.Join(wikiRoot, clean)
	}

	candidates, walkErr := s.collectCandidates(walkRoot, wikiRoot)
	if walkErr != nil {
		// Walk errors are logged but non-fatal: we still process whatever
		// candidates we did collect.
		slog.Warn("skill_scanner: walk encountered errors", "err", walkErr)
		res.Errors = append(res.Errors, ScanError{Slug: "", Reason: "walk: " + walkErr.Error()})
	}

	updatedCache := make(map[string]string, len(candidates))
	llmCalls := 0
	budgetExceeded := false

	for _, c := range candidates {
		if ctx.Err() != nil {
			res.Errors = append(res.Errors, ScanError{Slug: "", Reason: "context: " + ctx.Err().Error()})
			break
		}

		// Tombstone gate: skip if either the slug or source_article matches.
		guess := skillSlugFromPath(c.relPath)
		if tombstoneSlugs[guess] || tombstoneSources[c.relPath] {
			continue
		}

		res.Scanned++

		// SHA cache: skip the LLM call if the article content is unchanged
		// since the last successful classification.
		hash := sha256Hex(c.content)
		updatedCache[c.relPath] = hash

		s.mu.Lock()
		prior, hadPrior := s.mtimeCache[c.relPath]
		s.mu.Unlock()
		if hadPrior && prior == hash {
			continue
		}

		// Budget gate: short-circuit further LLM calls but keep walking so
		// we still update the cache for unchanged articles above.
		if llmCalls >= s.budgetPerPass {
			if !budgetExceeded {
				res.Errors = append(res.Errors, ScanError{
					Slug:   "",
					Reason: fmt.Sprintf("budget_exceeded: %d/%d LLM calls used", llmCalls, s.budgetPerPass),
				})
				budgetExceeded = true
			}
			continue
		}

		llmCalls++

		isSkill, fm, body, err := s.provider.AskIsSkill(ctx, c.relPath, c.content)
		if err != nil {
			res.Errors = append(res.Errors, ScanError{Slug: c.relPath, Reason: "llm: " + err.Error()})
			// Leave the cache entry unset so we retry next pass.
			delete(updatedCache, c.relPath)
			continue
		}

		if !isSkill {
			continue
		}
		res.Matched++

		if dryRun {
			res.Proposed++
			continue
		}

		// Stamp provenance + author identity onto the frontmatter before the
		// write helper takes over.
		fm.Metadata.Wuphf.SourceArticles = appendUnique(fm.Metadata.Wuphf.SourceArticles, c.relPath)
		fm.Metadata.Wuphf.CreatedBy = "archivist"
		spec := specToTeamSkill(fm, body, c.relPath)
		spec.CreatedBy = "archivist"

		s.broker.mu.Lock()
		_, writeErr := s.broker.writeSkillProposalLocked(spec)
		s.broker.mu.Unlock()
		if writeErr != nil {
			// Existing skills come back nil-error, *teamSkill non-nil — counted
			// as a successful no-op above. Real errors land here.
			if isDuplicateSkillError(writeErr) {
				res.Deduped++
				continue
			}
			res.Errors = append(res.Errors, ScanError{Slug: skillSlug(fm.Name), Reason: "write: " + writeErr.Error()})
			delete(updatedCache, c.relPath)
			continue
		}
		res.Proposed++
	}

	// Atomically promote the per-pass cache. We deliberately discard prior
	// entries for paths that disappeared from the walk so the cache stays
	// bounded.
	s.mu.Lock()
	s.mtimeCache = updatedCache
	s.mu.Unlock()

	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

// candidate is a single article queued for classification.
type candidate struct {
	relPath string // wiki-relative, forward-slashed
	content string
}

// collectCandidates walks walkRoot and returns the markdown articles that
// should be sent to the LLM. wikiRoot is the absolute filesystem root used to
// derive wiki-relative paths.
func (s *SkillScanner) collectCandidates(walkRoot, wikiRoot string) ([]candidate, error) {
	var out []candidate
	walkErr := filepath.Walk(walkRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip-and-log via the outer slog.Warn
		}
		// Compute wiki-relative path with forward slashes for matching.
		rel, relErr := filepath.Rel(wikiRoot, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if info.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(rel, ".md") {
			return nil
		}
		// Defence-in-depth: re-check skip prefixes on the file too in case the
		// dir-level check was bypassed (e.g. symlink edge cases).
		if shouldSkipPath(rel) {
			return nil
		}

		raw, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		out = append(out, candidate{relPath: rel, content: string(raw)})
		return nil
	})
	return out, walkErr
}

// shouldSkipDir reports whether the directory at wiki-relative path rel
// should be pruned from the walk.
func shouldSkipDir(rel string) bool {
	// Always skip dot-prefixed dirs (e.g. team/.dlq, team/skills/.system).
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".") && rel != "." {
		return true
	}
	for _, prefix := range scanSkippedDirs {
		// scanSkippedDirs entries have a trailing slash; the directory itself
		// is the prefix without the slash.
		dir := strings.TrimSuffix(prefix, "/")
		if rel == dir || strings.HasPrefix(rel+"/", prefix) {
			return true
		}
	}
	// Per-agent notebooks: team/agents/<slug>/notebook/...
	if strings.HasPrefix(rel, agentsDirPrefix) && strings.Contains(rel, scanAgentNotebookSegment) {
		return true
	}
	// Hide team/agents/<slug>/notebook entirely.
	if strings.HasPrefix(rel, agentsDirPrefix) && strings.HasSuffix(rel, "/notebook") {
		return true
	}
	return false
}

// shouldSkipPath reports whether a file at wiki-relative path rel should be
// excluded from scanning even if its parent dir was walked.
func shouldSkipPath(rel string) bool {
	for _, prefix := range scanSkippedDirs {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	if strings.HasPrefix(rel, agentsDirPrefix) && strings.Contains(rel, scanAgentNotebookSegment) {
		return true
	}
	if strings.HasSuffix(rel, ".executions.jsonl") {
		return true
	}
	return false
}

// resolveWikiRoot returns the on-disk path of the wiki root, or "" if the
// markdown backend is not initialised.
func (s *SkillScanner) resolveWikiRoot() (string, error) {
	s.broker.mu.Lock()
	worker := s.broker.wikiWorker
	s.broker.mu.Unlock()
	if worker == nil {
		return "", nil
	}
	repo := worker.Repo()
	if repo == nil {
		return "", nil
	}
	return repo.Root(), nil
}

// loadTombstone returns the (slugs, sources) sets used to gate scanning. We
// build sets so the per-article check stays O(1).
func (s *SkillScanner) loadTombstone() (slugs, sources map[string]bool) {
	slugs = make(map[string]bool)
	sources = make(map[string]bool)

	s.broker.mu.Lock()
	entries, _ := s.broker.loadSkillTombstoneLocked()
	s.broker.mu.Unlock()

	for _, e := range entries {
		if e.Slug != "" {
			slugs[strings.ToLower(strings.TrimSpace(e.Slug))] = true
		}
		if e.SourceArticle != "" {
			sources[filepath.ToSlash(strings.TrimSpace(e.SourceArticle))] = true
		}
	}
	return slugs, sources
}

// specToTeamSkill folds a SkillFrontmatter + body + source article into the
// teamSkill shape that writeSkillProposalLocked expects. Only the fields the
// frontmatter actually carries get set; the helper fills in defaults.
func specToTeamSkill(fm SkillFrontmatter, body, sourceArticle string) teamSkill {
	wuphf := fm.Metadata.Wuphf
	return teamSkill{
		Name:               fm.Name,
		Title:              wuphf.Title,
		Description:        fm.Description,
		Content:            body,
		CreatedBy:          stringOr(wuphf.CreatedBy, "archivist"),
		Channel:            "general",
		Tags:               append([]string(nil), wuphf.Tags...),
		Trigger:            wuphf.Trigger,
		WorkflowProvider:   wuphf.WorkflowProvider,
		WorkflowKey:        wuphf.WorkflowKey,
		WorkflowDefinition: wuphf.WorkflowDefinition,
		WorkflowSchedule:   wuphf.WorkflowSchedule,
		RelayID:            wuphf.RelayID,
		RelayPlatform:      wuphf.RelayPlatform,
		RelayEventTypes:    append([]string(nil), wuphf.RelayEventTypes...),
		Status:             "proposed",
	}
}

// skillSlugFromPath synthesizes a candidate slug from a wiki path. It is a
// best-effort guess used only for tombstone matching when the LLM hasn't
// classified the article yet — final slugs come from the LLM response.
func skillSlugFromPath(relPath string) string {
	base := strings.TrimSuffix(filepath.Base(relPath), ".md")
	return skillSlug(base)
}

// sha256Hex returns the hex-encoded sha256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// stringOr returns s when non-empty, else fallback.
func stringOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// isDuplicateSkillError reports whether the error returned by
// writeSkillProposalLocked indicates a benign de-dup. Today the helper
// returns the existing skill with a nil error on dedup, so this is a
// forward-compat check for any future error-shaped duplicate signal.
func isDuplicateSkillError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate")
}

// ── default LLM provider ──────────────────────────────────────────────────

// defaultLLMProvider is a stub implementation that always classifies articles
// as not-a-skill. It loads the system prompt from disk on first use so an
// operator can see the prompt in `team/skills/.system/skill-creator.md`, but
// the actual LLM round-trip is intentionally deferred — the scanner plumbing
// ships first; the live model wiring lands in a follow-up.
//
// TODO(skill-compile): wire this to the real broker LLM provider abstraction
// (see internal/team/headless_*.go for patterns) so the scanner actually
// generates proposals from articles. Until then the scanner runs end-to-end
// but produces zero matches in production.
type defaultLLMProvider struct {
	systemPromptPath string

	mu      sync.Mutex
	prompt  string
	loaded  bool
	loadErr error
}

// NewDefaultLLMProvider returns a stub provider that loads the system prompt
// from systemPromptPath (typically <wikiRoot>/team/skills/.system/skill-creator.md)
// and otherwise classifies every article as not-a-skill. If the path is empty
// the embedded default prompt is used.
func NewDefaultLLMProvider(systemPromptPath string) *defaultLLMProvider {
	return &defaultLLMProvider{systemPromptPath: systemPromptPath}
}

// SystemPrompt returns the system prompt that will be sent to the LLM. It
// reads the file from disk on first use, falling back to the embedded
// default if the file is missing or unreadable.
func (p *defaultLLMProvider) SystemPrompt() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		return p.prompt, p.loadErr
	}
	p.loaded = true
	path := strings.TrimSpace(p.systemPromptPath)
	if path == "" {
		p.prompt = defaultSkillCreatorPromptEmbedded
		return p.prompt, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			p.prompt = defaultSkillCreatorPromptEmbedded
			return p.prompt, nil
		}
		p.loadErr = fmt.Errorf("skill-creator.md system prompt: %w", err)
		return "", p.loadErr
	}
	if strings.TrimSpace(string(raw)) == "" {
		p.prompt = defaultSkillCreatorPromptEmbedded
		return p.prompt, nil
	}
	p.prompt = string(raw)
	return p.prompt, nil
}

// AskIsSkill classifies an article as a skill or not. It implements two
// strategies in order:
//
//  1. Explicit-frontmatter fast path: if the article already carries Anthropic
//     Agent Skills frontmatter (top-level `name:` and `description:`), the
//     author has explicitly opted in. We promote without an LLM round-trip.
//     This is the demo-friendly path: seed wiki articles with explicit
//     frontmatter and the scanner picks them up deterministically.
//
//  2. Fallback (today a stub): if no explicit frontmatter is present, the
//     scanner has nothing to act on. The plumbing for a live LLM round-trip
//     is built (system prompt + user prompt assembly), but no provider client
//     is wired today. Returns is_skill=false to match the previous behavior.
//
// The system prompt is loaded eagerly to surface any wiki-config errors at
// the first call rather than silently no-op'ing.
func (p *defaultLLMProvider) AskIsSkill(ctx context.Context, articlePath, articleContent string) (bool, SkillFrontmatter, string, error) {
	if _, err := p.SystemPrompt(); err != nil {
		return false, SkillFrontmatter{}, "", err
	}
	// Fast path: the article already declares itself as a skill via
	// frontmatter. ParseSkillMarkdown only succeeds when both name and
	// description are non-empty, which is the same opt-in contract the
	// Anthropic spec enforces.
	if fm, body, err := ParseSkillMarkdown([]byte(articleContent)); err == nil {
		// Backfill version + license if the author omitted them — keeps the
		// emitted skill markdown hub-publishable without forcing the wiki
		// author to know the spec details.
		if strings.TrimSpace(fm.Version) == "" {
			fm.Version = "1.0.0"
		}
		if strings.TrimSpace(fm.License) == "" {
			fm.License = "MIT"
		}
		return true, fm, body, nil
	}
	// Build the user prompt so it's ready when the live wiring lands. We
	// intentionally throw it away today.
	_ = buildSkillUserPrompt(articlePath, articleContent)
	return false, SkillFrontmatter{}, "", nil
}

// buildSkillUserPrompt assembles the user-message body sent to the LLM.
// Exposed at package scope so tests and the (future) live provider share the
// exact same prompt structure.
func buildSkillUserPrompt(articlePath, articleContent string) string {
	var b strings.Builder
	b.WriteString("ARTICLE PATH: ")
	b.WriteString(articlePath)
	b.WriteString("\n\nARTICLE CONTENT:\n")
	b.WriteString(articleContent)
	b.WriteString("\n\nIs this a reusable skill? If yes, respond with JSON: ")
	b.WriteString(`{"is_skill": true, "name": "kebab-slug", "description": "one line", "body": "markdown body for the skill"}.`)
	b.WriteString(" If no, respond with: ")
	b.WriteString(`{"is_skill": false}.`)
	return b.String()
}

// parseSkillJSON parses an LLM response into the (isSkill, frontmatter, body)
// triple the scanner expects. Exposed so the live provider and tests share
// one decoder.
func parseSkillJSON(raw string) (bool, SkillFrontmatter, string, error) {
	trimmed := strings.TrimSpace(raw)
	// Tolerate ```json fences if a model adds them despite the prompt.
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var parsed struct {
		IsSkill     bool   `json:"is_skill"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return false, SkillFrontmatter{}, "", fmt.Errorf("skill_scanner: parse llm json: %w", err)
	}
	if !parsed.IsSkill {
		return false, SkillFrontmatter{}, "", nil
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return false, SkillFrontmatter{}, "", errors.New("skill_scanner: llm returned is_skill=true but no name")
	}
	if strings.TrimSpace(parsed.Description) == "" {
		return false, SkillFrontmatter{}, "", errors.New("skill_scanner: llm returned is_skill=true but no description")
	}
	fm := SkillFrontmatter{
		Name:        skillSlug(parsed.Name),
		Description: strings.TrimSpace(parsed.Description),
		Version:     "1.0.0",
		License:     "MIT",
	}
	return true, fm, parsed.Body, nil
}
