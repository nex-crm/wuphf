package team

// notebook_signal_scanner.go is a Stage B signal source. It walks every
// per-agent notebook under team/agents/<slug>/notebook/, builds an in-memory
// inverted index over the markdown bodies, clusters entries by token-set
// Jaccard similarity, and emits SkillCandidate values for clusters that
// represent a multi-agent convergence on the same topic.
//
// Heuristics here are intentionally cheap: tokenise → drop stopwords →
// Jaccard cluster. The synthesizer (PR 2-B) is the LLM-gated step that
// decides whether a candidate is worth materialising into a proposed skill.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// NotebookSignalScanner walks team/agents/*/notebook/**/*.md and clusters
// cross-agent entries by token-overlap similarity. Each qualifying cluster
// becomes a SkillCandidate.
type NotebookSignalScanner struct {
	broker *Broker

	minClusterSize       int
	minDistinctAgents    int
	similarityThreshold  float64
	maxCandidatesPerPass int
}

// NewNotebookSignalScanner constructs a scanner with defaults pulled from
// env (or the documented fallbacks):
//
//	WUPHF_STAGE_B_NOTEBOOK_MIN_CLUSTER  → minClusterSize       (default 2)
//	WUPHF_STAGE_B_NOTEBOOK_MIN_AGENTS   → minDistinctAgents    (default 2)
//	WUPHF_STAGE_B_NOTEBOOK_SIMILARITY   → similarityThreshold  (default 0.6)
//	WUPHF_STAGE_B_NOTEBOOK_MAX_PER_PASS → maxCandidatesPerPass (default 10)
func NewNotebookSignalScanner(b *Broker) *NotebookSignalScanner {
	return &NotebookSignalScanner{
		broker:               b,
		minClusterSize:       envIntDefault("WUPHF_STAGE_B_NOTEBOOK_MIN_CLUSTER", 2),
		minDistinctAgents:    envIntDefault("WUPHF_STAGE_B_NOTEBOOK_MIN_AGENTS", 2),
		similarityThreshold:  envFloatDefault("WUPHF_STAGE_B_NOTEBOOK_SIMILARITY", 0.6),
		maxCandidatesPerPass: envIntDefault("WUPHF_STAGE_B_NOTEBOOK_MAX_PER_PASS", 10),
	}
}

// Scan walks team/agents/*/notebook/**/*.md, tokenises each entry, clusters
// them by Jaccard similarity, and emits one SkillCandidate per cluster that
// passes minClusterSize + minDistinctAgents. Returns up to
// maxCandidatesPerPass candidates ordered by SignalCount desc.
func (s *NotebookSignalScanner) Scan(ctx context.Context) ([]SkillCandidate, error) {
	if s == nil || s.broker == nil {
		return nil, nil
	}

	wikiRoot := s.resolveWikiRoot()
	if wikiRoot == "" {
		// No wiki worker yet — the signal source degrades gracefully.
		slog.Info("notebook_signal_scanner_skipped", "reason", "wiki worker not initialised")
		return nil, nil
	}

	entries, walkErr := s.collectNotebookEntries(wikiRoot)
	if walkErr != nil {
		// Walk errors are non-fatal: we still cluster whatever we did read.
		slog.Warn("notebook_signal_scanner_walk_errors", "err", walkErr)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("notebook_signal_scanner: ctx cancelled: %w", err)
	}

	clusters := clusterNotebookEntries(entries, s.similarityThreshold)
	candidates := s.candidatesFromClusters(ctx, clusters)

	// Stable order: highest SignalCount first; ties break on suggested name
	// so output is deterministic across runs.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].SignalCount != candidates[j].SignalCount {
			return candidates[i].SignalCount > candidates[j].SignalCount
		}
		return candidates[i].SuggestedName < candidates[j].SuggestedName
	})

	if s.maxCandidatesPerPass > 0 && len(candidates) > s.maxCandidatesPerPass {
		candidates = candidates[:s.maxCandidatesPerPass]
	}

	for _, c := range candidates {
		slog.Info("notebook_cluster_emitted",
			"name", c.SuggestedName,
			"signal_count", c.SignalCount,
			"distinct_agents", distinctAuthors(c.Excerpts),
		)
	}
	return candidates, nil
}

// notebookEntry is a single per-agent notebook article queued for clustering.
type notebookEntry struct {
	relPath   string // wiki-relative, forward-slashed
	author    string // agent slug parsed from the path
	tokens    map[string]int
	tokenSet  map[string]bool
	body      string
	createdAt time.Time
}

// collectNotebookEntries walks team/agents/*/notebook/**/*.md under wikiRoot
// and returns one notebookEntry per readable file. Dot-prefixed dirs and
// files are skipped.
func (s *NotebookSignalScanner) collectNotebookEntries(wikiRoot string) ([]notebookEntry, error) {
	root := filepath.Join(wikiRoot, agentsDirPrefix[:len(agentsDirPrefix)-1])
	if info, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	} else if !info.IsDir() {
		return nil, nil
	}
	var out []notebookEntry
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			slog.Warn("notebook_signal_scanner: skipping path due to walk error", "path", p, "err", err)
			return nil
		}
		rel, relErr := filepath.Rel(wikiRoot, p)
		if relErr != nil {
			slog.Warn("notebook_signal_scanner: skipping path with unresolvable relative", "path", p, "wiki_root", wikiRoot, "err", relErr)
			return nil
		}
		rel = filepath.ToSlash(rel)

		if info.IsDir() {
			base := filepath.Base(rel)
			if strings.HasPrefix(base, ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(rel, ".md") {
			return nil
		}
		base := filepath.Base(rel)
		if strings.HasPrefix(base, ".") {
			return nil
		}
		// Only files under team/agents/<slug>/notebook/ qualify.
		if !strings.HasPrefix(rel, agentsDirPrefix) || !strings.Contains(rel, scanAgentNotebookSegment) {
			return nil
		}

		raw, readErr := os.ReadFile(p)
		if readErr != nil {
			slog.Warn("notebook_signal_scanner: skipping unreadable notebook file", "path", p, "err", readErr)
			return nil
		}

		body, createdAt := splitFrontmatterForNotebook(string(raw))
		if createdAt.IsZero() {
			createdAt = info.ModTime().UTC()
		}
		tokens := tokenizeForCluster(body)
		if len(tokens) == 0 {
			return nil
		}
		entry := notebookEntry{
			relPath:   rel,
			author:    notebookAuthorFromPath(rel),
			tokens:    tokens,
			tokenSet:  tokenSet(tokens),
			body:      body,
			createdAt: createdAt,
		}
		out = append(out, entry)
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return out, walkErr
	}
	return out, nil
}

// notebookCluster is a working set used during clustering. The centroid is
// the union of every member's tokenSet so growth is monotonic — adding an
// entry can only widen the centroid.
type notebookCluster struct {
	members  []notebookEntry
	centroid map[string]bool
}

// clusterNotebookEntries greedily groups entries by Jaccard similarity over
// the centroid. For each entry we find the existing cluster whose centroid
// has Jaccard >= threshold; if none, we start a new cluster. The greedy
// choice is good enough for v1 — the synthesizer is the LLM gate that
// catches false-positive clusters.
func clusterNotebookEntries(entries []notebookEntry, threshold float64) []notebookCluster {
	var clusters []notebookCluster
	for _, e := range entries {
		bestIdx := -1
		bestScore := 0.0
		for i := range clusters {
			score := jaccardSets(clusters[i].centroid, e.tokenSet)
			if score >= threshold && score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			centroid := make(map[string]bool, len(e.tokenSet))
			for k := range e.tokenSet {
				centroid[k] = true
			}
			clusters = append(clusters, notebookCluster{
				members:  []notebookEntry{e},
				centroid: centroid,
			})
			continue
		}
		clusters[bestIdx].members = append(clusters[bestIdx].members, e)
		for k := range e.tokenSet {
			clusters[bestIdx].centroid[k] = true
		}
	}
	return clusters
}

// candidatesFromClusters folds clusters that pass minClusterSize +
// minDistinctAgents into SkillCandidate values. Excerpts are the top three
// entries scored by token-overlap with the cluster centroid.
func (s *NotebookSignalScanner) candidatesFromClusters(ctx context.Context, clusters []notebookCluster) []SkillCandidate {
	var out []SkillCandidate
	for _, c := range clusters {
		if len(c.members) < s.minClusterSize {
			continue
		}
		authors := map[string]bool{}
		for _, m := range c.members {
			if m.author != "" {
				authors[m.author] = true
			}
		}
		if len(authors) < s.minDistinctAgents {
			continue
		}

		first, last := timeWindow(c.members)
		excerpts := topExcerpts(c.members, c.centroid, 3)
		name := suggestedNameFromCluster(c.members, c.centroid)
		desc := suggestedDescriptionFromCluster(c.members)
		related := s.relatedWikiPaths(ctx, c.centroid)

		candidate := SkillCandidate{
			Source:               SourceNotebookCluster,
			SuggestedName:        name,
			SuggestedDescription: desc,
			Excerpts:             excerpts,
			RelatedWikiPaths:     related,
			SignalCount:          len(c.members),
			FirstSeenAt:          first,
			LastSeenAt:           last,
		}
		out = append(out, candidate)
	}
	return out
}

// relatedWikiPaths consults the broker's wiki index using the most-frequent
// tokens from the cluster centroid. If the index is unavailable we return
// an empty slice — the synthesizer can still proceed with the excerpts.
func (s *NotebookSignalScanner) relatedWikiPaths(ctx context.Context, centroid map[string]bool) []string {
	idx := s.broker.WikiIndex()
	if idx == nil {
		// TODO(stage-b): once the wiki index is unavailable we degrade to
		// no related-paths context. The synthesizer must tolerate empty.
		return nil
	}
	query := centroidQuery(centroid, 5)
	if query == "" {
		return nil
	}
	hits, err := idx.Search(ctx, query, 5)
	if err != nil {
		slog.Warn("notebook_signal_scanner_search_failed", "err", err, "query", query)
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	for _, h := range hits {
		key := h.Entity
		if key == "" {
			key = h.FactID
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		paths = append(paths, key)
	}
	return paths
}

// resolveWikiRoot returns the on-disk path of the wiki root, or "" if the
// markdown backend is not initialised.
func (s *NotebookSignalScanner) resolveWikiRoot() string {
	worker := s.broker.WikiWorker()
	if worker == nil {
		return ""
	}
	repo := worker.Repo()
	if repo == nil {
		return ""
	}
	return repo.Root()
}

// ── helpers ───────────────────────────────────────────────────────────────

// notebookStopwords is the conservative 50-token English stop-word list used
// for clustering. We keep it small intentionally — over-aggressive stopword
// removal pushes Jaccard scores up and creates false clusters.
var notebookStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "but": true, "by": true, "can": true, "do": true, "does": true,
	"for": true, "from": true, "had": true, "has": true, "have": true,
	"he": true, "her": true, "here": true, "his": true, "how": true,
	"i": true, "if": true, "in": true, "into": true, "is": true, "it": true,
	"its": true, "of": true, "on": true, "or": true, "our": true, "out": true,
	"so": true, "than": true, "that": true, "the": true, "their": true,
	"them": true, "then": true, "there": true, "these": true, "they": true,
	"this": true, "to": true, "was": true, "we": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "who": true,
	"why": true, "will": true, "with": true, "you": true, "your": true,
}

// tokenizeForCluster lowercases s, splits on non-alphanumeric runes, drops
// stopwords + 1-char tokens, and returns a token-frequency map.
func tokenizeForCluster(s string) map[string]int {
	tokens := map[string]int{}
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tok := current.String()
		current.Reset()
		if len(tok) <= 1 {
			return
		}
		if notebookStopwords[tok] {
			return
		}
		tokens[tok]++
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// tokenSet returns the unique-token view of a frequency map.
func tokenSet(tokens map[string]int) map[string]bool {
	out := make(map[string]bool, len(tokens))
	for k := range tokens {
		out[k] = true
	}
	return out
}

// jaccardSets returns |A∩B| / |A∪B|. Returns 0 for two empty sets.
func jaccardSets(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// notebookAuthorFromPath extracts the agent slug from a wiki-relative path
// of shape team/agents/<slug>/notebook/... — returns "" if the path does
// not match the expected layout.
func notebookAuthorFromPath(rel string) string {
	if !strings.HasPrefix(rel, agentsDirPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(rel, agentsDirPrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// distinctAuthors returns the count of unique excerpt authors (case-sensitive,
// trimmed). Returns 0 when excerpts is empty. Lives here because it is the
// canonical helper shared between the notebook scanner that produces
// excerpts and the synthesizer (PR 2-B) that summarises them.
func distinctAuthors(excerpts []SkillCandidateExcerpt) int {
	seen := make(map[string]struct{}, len(excerpts))
	for _, e := range excerpts {
		a := strings.TrimSpace(e.Author)
		if a == "" {
			continue
		}
		seen[a] = struct{}{}
	}
	return len(seen)
}

// timeWindow returns (oldest, newest) createdAt across cluster members.
func timeWindow(entries []notebookEntry) (time.Time, time.Time) {
	if len(entries) == 0 {
		return time.Time{}, time.Time{}
	}
	first := entries[0].createdAt
	last := entries[0].createdAt
	for _, e := range entries[1:] {
		if e.createdAt.Before(first) {
			first = e.createdAt
		}
		if e.createdAt.After(last) {
			last = e.createdAt
		}
	}
	return first, last
}

// topExcerpts returns up to n excerpts ordered by token overlap with the
// centroid. Snippets are bounded to ~600 chars so the synthesizer's prompt
// stays predictable.
func topExcerpts(entries []notebookEntry, centroid map[string]bool, n int) []SkillCandidateExcerpt {
	scored := make([]struct {
		entry notebookEntry
		score int
	}, len(entries))
	for i, e := range entries {
		count := 0
		for k := range e.tokenSet {
			if centroid[k] {
				count++
			}
		}
		scored[i].entry = e
		scored[i].score = count
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].entry.relPath < scored[j].entry.relPath
	})
	if len(scored) > n {
		scored = scored[:n]
	}
	out := make([]SkillCandidateExcerpt, 0, len(scored))
	for _, s := range scored {
		out = append(out, SkillCandidateExcerpt{
			Path:      s.entry.relPath,
			Snippet:   truncateSnippet(s.entry.body, 600),
			Author:    s.entry.author,
			CreatedAt: s.entry.createdAt,
		})
	}
	return out
}

// truncateSnippet caps s at maxRunes runes (not bytes) so we never slice a
// multi-byte UTF-8 glyph in half.
func truncateSnippet(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

// suggestedNameFromCluster picks the most-frequent meaningful token across
// the cluster, slugified. Falls back to "untitled-cluster-{N}" if no token
// dominates. The synthesizer may override.
func suggestedNameFromCluster(entries []notebookEntry, centroid map[string]bool) string {
	freq := map[string]int{}
	for _, e := range entries {
		for tok, n := range e.tokens {
			if !centroid[tok] {
				continue
			}
			freq[tok] += n
		}
	}
	if len(freq) == 0 {
		return fmt.Sprintf("untitled-cluster-%d", len(entries))
	}
	type kv struct {
		tok string
		n   int
	}
	pairs := make([]kv, 0, len(freq))
	for tok, n := range freq {
		pairs = append(pairs, kv{tok: tok, n: n})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].tok < pairs[j].tok
	})
	primary := pairs[0].tok
	if len(pairs) >= 2 {
		// "deploy" + "prod" → "deploy-prod-workflow" feels less generic than a
		// single token by itself. Two-token slug only when both terms are
		// frequent enough to clearly belong to the cluster.
		if pairs[1].n >= pairs[0].n/2 {
			return skillSlug(primary + "-" + pairs[1].tok + "-workflow")
		}
	}
	return skillSlug(primary + "-workflow")
}

// suggestedDescriptionFromCluster picks the most-shared sentence (>5 tokens)
// across cluster members. We look for sentences whose content tokens (post
// stopword) overlap heavily with the centroid. Returns "" if no candidate.
func suggestedDescriptionFromCluster(entries []notebookEntry) string {
	type sentenceHit struct {
		sentence string
		count    int
	}
	hits := map[string]*sentenceHit{}
	for _, e := range entries {
		for _, raw := range splitSentences(e.body) {
			normalised := strings.ToLower(strings.TrimSpace(raw))
			if normalised == "" {
				continue
			}
			toks := tokenizeForCluster(normalised)
			if len(toks) < 3 {
				continue
			}
			key := normalised
			h, ok := hits[key]
			if !ok {
				h = &sentenceHit{sentence: strings.TrimSpace(raw)}
				hits[key] = h
			}
			h.count++
		}
	}
	var best *sentenceHit
	for _, h := range hits {
		if h.count < 2 {
			continue
		}
		if best == nil || h.count > best.count || (h.count == best.count && h.sentence < best.sentence) {
			best = h
		}
	}
	if best == nil {
		return ""
	}
	return truncateSnippet(best.sentence, 200)
}

// splitSentences performs a coarse sentence split on s. We don't ship a
// real NLP tokenizer — period / exclamation / question marks as terminators
// is good enough for matching duplicate guidance across notebooks.
func splitSentences(s string) []string {
	var out []string
	var current strings.Builder
	flush := func() {
		val := strings.TrimSpace(current.String())
		current.Reset()
		if val != "" {
			out = append(out, val)
		}
	}
	for _, r := range s {
		current.WriteRune(r)
		switch r {
		case '.', '!', '?', '\n':
			flush()
		}
	}
	flush()
	return out
}

// centroidQuery returns the top-n centroid tokens joined by spaces, used as
// the BM25 query against the wiki index.
func centroidQuery(centroid map[string]bool, n int) string {
	if len(centroid) == 0 {
		return ""
	}
	tokens := make([]string, 0, len(centroid))
	for k := range centroid {
		tokens = append(tokens, k)
	}
	sort.Strings(tokens)
	if len(tokens) > n {
		tokens = tokens[:n]
	}
	return strings.Join(tokens, " ")
}

// splitFrontmatterForNotebook returns (body, createdAt). If the entry has a
// "---" YAML frontmatter block we strip it; if it carries a created_at field
// we parse it. Best-effort — failure returns the raw text and zero time.
func splitFrontmatterForNotebook(content string) (string, time.Time) {
	if !strings.HasPrefix(content, "---\n") {
		return content, time.Time{}
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return content, time.Time{}
	}
	yamlBlock := rest[:end]
	body := strings.TrimSpace(rest[end+len("\n---"):])

	var createdAt time.Time
	for _, line := range strings.Split(yamlBlock, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "created_at:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "created_at:"))
		val = strings.Trim(val, "\"'")
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			createdAt = t.UTC()
		}
		break
	}
	return body, createdAt
}

// envIntDefault reads an int from env, falling back to fallback on missing
// or unparseable values.
func envIntDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return fallback
	}
	return n
}

// envFloatDefault reads a float from env, falling back to fallback on
// missing or unparseable values.
func envFloatDefault(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return fallback
	}
	return f
}
