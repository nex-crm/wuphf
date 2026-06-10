package team

// context_assembler.go — U2.1/U2.2 task-scoped knowledge injection
// (docs/specs/sota-uplift.md).
//
// Replaces the "global top-8 learnings for everyone" model with retrieval
// scoped to the work at hand: when a work packet is built for a task, the
// assembler queries the learning log and the wiki index with the task's own
// text and injects only what scores as relevant. Knowledge arrives WITH the
// work instead of waiting behind a discouraged pull tool.
//
// Scoring is deterministic token-overlap with corpus IDF weighting — it
// works offline with no embedding provider, which keeps the eval harness
// and self-hosted installs honest. Dense rerank over internal/embedding can
// slot in behind relevantLearnings without changing any caller (the seam is
// the function, not the call sites).

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// taskKnowledgeLearningLimit / WikiLimit cap the injected block.
	taskKnowledgeLearningLimit = 5
	taskKnowledgeWikiLimit     = 3
	// taskKnowledgeMinOverlap is the minimum count of distinct meaningful
	// query tokens a learning must share before it is considered relevant
	// at all — guards against single-token coincidences spraying unrelated
	// knowledge into every packet (the eval's cold control).
	taskKnowledgeMinOverlap = 2
)

var contextTokenStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {},
	"from": {}, "into": {}, "are": {}, "was": {}, "will": {}, "should": {},
	"task": {}, "work": {}, "new": {}, "use": {}, "all": {}, "its": {},
	"has": {}, "have": {}, "not": {}, "you": {}, "your": {}, "our": {},
}

// contextTokens lowercases and splits s into meaningful tokens.
func contextTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 3 {
			continue
		}
		if _, stop := contextTokenStopwords[f]; stop {
			continue
		}
		out = append(out, f)
	}
	return out
}

func contextTokenSet(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, tok := range contextTokens(s) {
		set[tok] = struct{}{}
	}
	return set
}

// learningSearchText is the haystack a learning is matched against.
func learningSearchText(rec LearningRecord) string {
	parts := []string{rec.Key, rec.Insight, rec.Scope, rec.PlaybookSlug}
	parts = append(parts, rec.Files...)
	parts = append(parts, rec.Entities...)
	return strings.Join(parts, " ")
}

// relevantLearnings scores every learning against the query by IDF-weighted
// distinct-token overlap and returns the top `limit` above the overlap
// floor, ordered by score then effective confidence. Deterministic and
// offline. When an embedding provider is configured (hybrid_retrieval.go),
// a dense cosine ranking over the same corpus is fused with the lexical
// ranking via RRF — dense-only candidates can surface even with zero token
// overlap, gated by the dense cosine floor. With no provider the behavior
// is byte-identical to the lexical-only path.
func relevantLearnings(log *LearningLog, query string, limit int) []LearningSearchResult {
	if log == nil || limit <= 0 {
		return nil
	}
	provider := retrievalEmbeddingProvider()
	queryTokens := contextTokenSet(query)
	if len(queryTokens) == 0 && provider == nil {
		return nil
	}
	// Pull the deduped corpus through the existing search path (no query →
	// no substring filtering), bounded well above the injection cap so IDF
	// sees the whole store.
	corpus, err := log.Search(LearningSearchFilters{Limit: MaxLearningLimit})
	if err != nil || len(corpus) == 0 {
		return nil
	}

	// Document frequency per token across the corpus.
	df := map[string]int{}
	docTokens := make([]map[string]struct{}, len(corpus))
	for i, rec := range corpus {
		set := contextTokenSet(learningSearchText(rec.LearningRecord))
		docTokens[i] = set
		for tok := range set {
			df[tok]++
		}
	}
	n := float64(len(corpus))

	type scored struct {
		idx     int
		score   float64
		overlap int
	}
	var hits []scored
	for i := range corpus {
		score := 0.0
		overlap := 0
		for tok := range queryTokens {
			if _, ok := docTokens[i][tok]; !ok {
				continue
			}
			overlap++
			score += math.Log(1 + n/float64(df[tok]))
		}
		if overlap < taskKnowledgeMinOverlap {
			continue
		}
		hits = append(hits, scored{idx: i, score: score, overlap: overlap})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return corpus[hits[i].idx].EffectiveConfidence > corpus[hits[j].idx].EffectiveConfidence
	})

	if provider == nil {
		// Lexical only — exactly the pre-B4 behavior.
		if len(hits) > limit {
			hits = hits[:limit]
		}
		out := make([]LearningSearchResult, 0, len(hits))
		for _, h := range hits {
			out = append(out, corpus[h.idx])
		}
		return out
	}

	// Hybrid: dense cosine ranking over the same corpus, fused with the
	// lexical ranking via RRF (hybrid_retrieval.go).
	embedCtx, cancel := context.WithTimeout(context.Background(), denseEmbedTimeout)
	defer cancel()
	cache := retrievalEmbeddingCache()
	texts := make([]string, len(corpus))
	for i, rec := range corpus {
		texts[i] = learningSearchText(rec.LearningRecord)
	}
	lexRanking := make([]int, 0, len(hits))
	for _, h := range hits {
		lexRanking = append(lexRanking, h.idx)
	}
	denseRanking := denseRankIndices(embedCtx, provider, cache, query, texts)
	fused := rrfFuseIndices(lexRanking, denseRanking)

	order := make([]int, 0, len(fused))
	for idx := range fused {
		order = append(order, idx)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if fused[order[i]] != fused[order[j]] {
			return fused[order[i]] > fused[order[j]]
		}
		if corpus[order[i]].EffectiveConfidence != corpus[order[j]].EffectiveConfidence {
			return corpus[order[i]].EffectiveConfidence > corpus[order[j]].EffectiveConfidence
		}
		return order[i] < order[j]
	})
	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]LearningSearchResult, 0, len(order))
	for _, idx := range order {
		out = append(out, corpus[idx])
	}
	return out
}

// taskKnowledgeContext builds the RELEVANT TEAM KNOWLEDGE block for a task
// packet: top-scored learnings plus wiki hits for the task's own text.
// Returns ("", nil) when nothing clears the relevance floor — an empty
// block is worse than no block. The second return is the context manifest:
// the ids of every injected item ("learning:<id>", "wiki:<ref>"), recorded
// on the turn's ledger entry so the human can see exactly what context the
// agent was handed (B4 transparency).
func (b *notificationContextBuilder) taskKnowledgeContext(task teamTask) (string, []string) {
	query := strings.TrimSpace(task.Title + " " + task.Details)
	if query == "" {
		return "", nil
	}
	var lines []string
	var manifest []string

	if b.searchLearnings != nil {
		for _, rec := range b.searchLearnings(query, taskKnowledgeLearningLimit) {
			line := fmt.Sprintf("- [learning:%s confidence=%d] %s", rec.ID, rec.EffectiveConfidence, strings.TrimSpace(rec.Insight))
			if rec.Key != "" {
				line += " (key: " + rec.Key + ")"
			}
			lines = append(lines, line)
			manifest = append(manifest, "learning:"+rec.ID)
		}
	}
	if b.searchWiki != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		hits := b.searchWiki(ctx, query, taskKnowledgeWikiLimit)
		cancel()
		for _, h := range hits {
			snippet := strings.TrimSpace(h.Snippet)
			if snippet == "" {
				continue
			}
			ref := h.Entity
			if ref == "" {
				ref = h.FactID
			}
			lines = append(lines, fmt.Sprintf("- [wiki:%s] %s", ref, truncate(snippet, 400)))
			manifest = append(manifest, "wiki:"+ref)
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	header := "RELEVANT TEAM KNOWLEDGE (matched to this task — apply it; cite the learning/wiki id when you do):"
	footer := "When you use retrieved context, cite its id in your messages."
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer, manifest
}

// tailClip keeps the LAST max bytes of s — task outcomes accrete at the end
// of Details, so the tail is where findings live.
func tailClip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}

// upstreamOutcomesContext renders the outcomes of completed upstream
// dependencies into a dependent task's packet (U3.2): dependency edges
// carry data, not just scheduling. Without this, agent B starts a task
// that depends on agent A's finished work without A's findings in
// context — side-by-side work, not collaboration. The second return is
// the manifest of injected upstream task ids ("upstream:<id>") for the
// turn's context-used record (B4 transparency).
func (b *notificationContextBuilder) upstreamOutcomesContext(task teamTask) (string, []string) {
	if b == nil || b.taskByID == nil {
		return "", nil
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(task.DependsOn)+len(task.BlockedOn))
	for _, id := range append(append([]string(nil), task.DependsOn...), task.BlockedOn...) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	var lines []string
	var manifest []string
	for _, id := range ids {
		up := b.taskByID(id)
		if up == nil {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(up.status))
		if status != "done" && status != "review" {
			continue
		}
		line := fmt.Sprintf("- #%s %s (%s)", up.ID, truncate(strings.TrimSpace(up.Title), taskListTitleClipChars), status)
		if details := strings.TrimSpace(up.Details); details != "" {
			line += "\n  Outcome: " + tailClip(details, 1500)
		}
		if res := up.VerificationResult; res != nil && res.Pass && strings.TrimSpace(res.Detail) != "" {
			line += "\n  Verified (" + res.Kind + "): " + truncate(strings.TrimSpace(res.Detail), 400)
		}
		lines = append(lines, line)
		manifest = append(manifest, "upstream:"+up.ID)
	}
	if len(lines) == 0 {
		return "", nil
	}
	return "UPSTREAM RESULTS (work this task depends on — build on it, do not redo it):\n" + strings.Join(lines, "\n"), manifest
}
