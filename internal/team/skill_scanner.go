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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
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
	// AskIsSkill classifies an article. existingSkillsSummary is injected
	// into the user prompt for deduplication (may be empty). enhanceSlug is
	// non-empty when the LLM returns an "enhance" directive.
	AskIsSkill(ctx context.Context, articlePath, articleContent, existingSkillsSummary string) (isSkill bool, fm SkillFrontmatter, body, enhanceSlug string, err error)
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
		if strings.HasPrefix(clean, "..") || (clean != scanWalkRoot && !strings.HasPrefix(clean, scanWalkRoot+"/")) {
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

	// Build existing-skills summary once per pass (request-scoped).
	s.broker.mu.Lock()
	existingSkillsSummary := buildExistingSkillsSummary(s.broker.skills, 2048)
	s.broker.mu.Unlock()

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

		isSkill, fm, body, enhanceSlug, err := s.provider.AskIsSkill(ctx, c.relPath, c.content, existingSkillsSummary)
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

		// Enhancement path: merge new details into the existing skill.
		if enhanceSlug != "" {
			s.broker.mu.Lock()
			enhanced, enhErr := s.broker.enhanceSkillLocked(enhanceSlug, body, fm.Description, skillSlug(fm.Name))
			s.broker.mu.Unlock()
			if enhErr != nil {
				slog.Warn("skill_scanner: enhance failed, falling through to proposal",
					"enhance_slug", enhanceSlug, "err", enhErr)
				// Fall through to normal proposal path below.
			} else if enhanced != nil {
				slog.Info("skill_scanner: enhanced existing skill",
					"slug", enhanceSlug, "source", c.relPath)
				res.Proposed++
				continue
			}
		}

		// Stamp provenance + author identity onto the frontmatter before the
		// write helper takes over.
		fm.Metadata.Wuphf.SourceArticles = appendUnique(fm.Metadata.Wuphf.SourceArticles, c.relPath)
		fm.Metadata.Wuphf.CreatedBy = "archivist"
		spec := specToTeamSkill(fm, body, c.relPath)
		spec.CreatedBy = "archivist"
		// Source-article frontmatter can opt into skill creation, but it is not
		// authoritative for lifecycle state. Every scanner write enters the
		// approval workflow as a proposal.
		spec.Status = "proposed"
		spec.DisabledFromStatus = ""

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
			slog.Warn("skill_scanner: skipping path due to walk error", "path", p, "err", err)
			return nil
		}
		// Compute wiki-relative path with forward slashes for matching.
		rel, relErr := filepath.Rel(wikiRoot, p)
		if relErr != nil {
			slog.Warn("skill_scanner: skipping path with unresolvable relative", "path", p, "wiki_root", wikiRoot, "err", relErr)
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
			slog.Warn("skill_scanner: skipping unreadable file", "path", p, "err", readErr)
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
//
// sourceArticle threads the wiki-relative path of the article that drove the
// proposal so the wiki provenance chain (notebook → wiki → skill) survives
// onto the in-memory record and the rendered SKILL.md frontmatter. Per the
// "archivist is a commit-author name, not an agent" decision (see
// project_entity_briefs_v1_2.md) we leave CreatedBy = archivist on the
// Stage A path. Drift detection, the UI source link, and read-based
// staleness all key off SourceArticle.
func specToTeamSkill(fm SkillFrontmatter, body, sourceArticle string) teamSkill {
	wuphf := fm.Metadata.Wuphf
	// Prefer the explicit sourceArticle argument; fall back to the first
	// frontmatter source_article entry so existing callers that route
	// provenance through the frontmatter still work.
	src := strings.TrimSpace(sourceArticle)
	if src == "" && len(wuphf.SourceArticles) > 0 {
		src = strings.TrimSpace(wuphf.SourceArticles[0])
	}
	status := strings.TrimSpace(wuphf.Status)
	if status == "" {
		status = "proposed"
	}
	return teamSkill{
		Name:               fm.Name,
		Title:              wuphf.Title,
		Description:        fm.Description,
		Content:            body,
		CreatedBy:          stringOr(wuphf.CreatedBy, "archivist"),
		SourceArticle:      src,
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
		Status:             status,
		DisabledFromStatus: strings.TrimSpace(wuphf.DisabledFromStatus),
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

// defaultSkillLLMTimeout is the default per-call deadline for the skill LLM
// classification. Override via WUPHF_SKILL_LLM_TIMEOUT (seconds).
const defaultSkillLLMTimeout = 30 * time.Second

// skillLLMTimeout resolves the per-call LLM timeout from the environment.
func skillLLMTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("WUPHF_SKILL_LLM_TIMEOUT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultSkillLLMTimeout
}

// defaultLLMProvider classifies wiki articles using the configured LLM
// provider (via provider.RunConfiguredOneShot). It implements two strategies
// in order:
//
//  1. Explicit-frontmatter fast path: articles already carrying valid Anthropic
//     Agent Skills frontmatter are promoted immediately without an LLM call.
//     This is the demo-friendly path and is never removed.
//
//  2. Live LLM round-trip: for articles without explicit frontmatter the
//     provider calls provider.RunConfiguredOneShot with the skill-creator.md
//     system prompt and a structured JSON request. If no API key is available
//     or the call fails the article is silently skipped (is_skill=false with
//     no error propagation) so the scan degrades gracefully rather than
//     aborting.
type defaultLLMProvider struct {
	systemPromptPath string

	mu      sync.Mutex
	prompt  string
	loaded  bool
	loadErr error
}

// NewDefaultLLMProvider returns a provider that classifies articles via the
// configured LLM CLI. systemPromptPath is the on-disk path of the
// skill-creator.md system prompt (typically
// <wikiRoot>/team/skills/.system/skill-creator.md). When empty or missing the
// embedded default prompt is used.
func NewDefaultLLMProvider(systemPromptPath string) *defaultLLMProvider {
	return &defaultLLMProvider{systemPromptPath: systemPromptPath}
}

// SystemPrompt returns the system prompt sent to the LLM. Reads from disk on
// first use, falling back to the embedded default when the file is absent or
// empty.
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

// AskIsSkill classifies an article as a skill or not.
//
//  1. Explicit-frontmatter fast path: if the article already carries valid
//     Anthropic Agent Skills frontmatter (name + description), the author has
//     opted in explicitly. We promote without an LLM call.
//
//  2. Live LLM round-trip: for articles without explicit frontmatter we call
//     provider.RunConfiguredOneShot with the skill-creator.md system prompt.
//     If the API key is missing or the call fails we log the reason and return
//     is_skill=false so the scan continues uninterrupted.
func (p *defaultLLMProvider) AskIsSkill(ctx context.Context, articlePath, articleContent, existingSkillsSummary string) (bool, SkillFrontmatter, string, string, error) {
	sysPrompt, err := p.SystemPrompt()
	if err != nil {
		return false, SkillFrontmatter{}, "", "", err
	}

	// Fast path: explicit frontmatter opt-in.
	if fm, body, parseErr := ParseSkillMarkdown([]byte(articleContent)); parseErr == nil {
		if strings.TrimSpace(fm.Version) == "" {
			fm.Version = "1.0.0"
		}
		if strings.TrimSpace(fm.License) == "" {
			fm.License = "MIT"
		}
		return true, fm, body, "", nil
	}

	// LLM path: wrap the caller's context with a per-call deadline so a slow
	// provider doesn't block the whole scan pass.
	timeout := skillLLMTimeout()
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	userPrompt := buildSkillUserPrompt(articlePath, articleContent, existingSkillsSummary)
	out, callErr := provider.RunConfiguredOneShotCtx(callCtx, sysPrompt, userPrompt, "")
	if callErr != nil {
		if callCtx.Err() != nil {
			slog.Warn("skill_scanner: LLM call timed out", "path", articlePath, "timeout", timeout)
			return false, SkillFrontmatter{}, "", "", nil
		}
		slog.Warn("skill_scanner: LLM call failed, skipping article",
			"path", articlePath, "err", callErr)
		return false, SkillFrontmatter{}, "", "", nil
	}
	if callCtx.Err() != nil {
		slog.Warn("skill_scanner: LLM call timed out", "path", articlePath, "timeout", timeout)
		return false, SkillFrontmatter{}, "", "", nil
	}
	result := parseSkillJSONFull(out)
	if result.Err != nil {
		slog.Warn("skill_scanner: LLM JSON parse failed, treating as not-a-skill",
			"path", articlePath, "err", result.Err)
		return false, SkillFrontmatter{}, "", "", nil
	}
	return result.IsSkill, result.FM, result.Body, result.Enhance, nil
}

// buildSkillUserPrompt assembles the user-message body sent to the LLM.
// Exposed at package scope so tests and the (future) live provider share the
// exact same prompt structure.
//
// existingSkillsSummary is an optional block listing current skills for
// deduplication. When non-empty it is appended so the LLM can avoid
// proposing duplicates.
func buildSkillUserPrompt(articlePath, articleContent, existingSkillsSummary string) string {
	var b strings.Builder
	b.WriteString("ARTICLE PATH: ")
	b.WriteString(articlePath)
	b.WriteString("\n\nARTICLE CONTENT:\n")
	b.WriteString(articleContent)
	if strings.TrimSpace(existingSkillsSummary) != "" {
		b.WriteString("\n\n")
		b.WriteString(existingSkillsSummary)
	}
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
	res := parseSkillJSONFull(raw)
	return res.IsSkill, res.FM, res.Body, res.Err
}

// parseSkillJSONFull is the enhanced parser that also extracts the "enhance"
// field. The legacy parseSkillJSON wrapper above preserves the existing
// call-site contract; new callers use parseSkillJSONFull directly.
func parseSkillJSONFull(raw string) skillJSONParseResult {
	trimmed := strings.TrimSpace(raw)
	// Tolerate ```json fences if a model adds them despite the prompt.
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var parsed struct {
		IsSkill     bool   `json:"is_skill"`
		Enhance     string `json:"enhance,omitempty"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return skillJSONParseResult{Err: fmt.Errorf("skill_scanner: parse llm json: %w", err)}
	}
	if !parsed.IsSkill {
		return skillJSONParseResult{}
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return skillJSONParseResult{Err: errors.New("skill_scanner: llm returned is_skill=true but no name")}
	}
	if strings.TrimSpace(parsed.Description) == "" {
		return skillJSONParseResult{Err: errors.New("skill_scanner: llm returned is_skill=true but no description")}
	}
	fm := SkillFrontmatter{
		Name:        skillSlug(parsed.Name),
		Description: strings.TrimSpace(parsed.Description),
		Version:     "1.0.0",
		License:     "MIT",
	}
	return skillJSONParseResult{
		IsSkill: true,
		Enhance: strings.TrimSpace(parsed.Enhance),
		FM:      fm,
		Body:    parsed.Body,
	}
}

// skillJSONParseResult bundles all outputs from parseSkillJSONFull.
type skillJSONParseResult struct {
	IsSkill bool
	Enhance string
	FM      SkillFrontmatter
	Body    string
	Err     error
}
