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
// offline; the dense-rerank seam lives here.
func relevantLearnings(log *LearningLog, query string, limit int) []LearningSearchResult {
	if log == nil || limit <= 0 {
		return nil
	}
	queryTokens := contextTokenSet(query)
	if len(queryTokens) == 0 {
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
		res     LearningSearchResult
		score   float64
		overlap int
	}
	var hits []scored
	for i, rec := range corpus {
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
		hits = append(hits, scored{res: rec, score: score, overlap: overlap})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].res.EffectiveConfidence > hits[j].res.EffectiveConfidence
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]LearningSearchResult, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.res)
	}
	return out
}

// taskKnowledgeContext builds the RELEVANT TEAM KNOWLEDGE block for a task
// packet: top-scored learnings plus wiki hits for the task's own text.
// Returns "" when nothing clears the relevance floor — an empty block is
// worse than no block.
func (b *notificationContextBuilder) taskKnowledgeContext(task teamTask) string {
	query := strings.TrimSpace(task.Title + " " + task.Details)
	if query == "" {
		return ""
	}
	var lines []string

	if b.searchLearnings != nil {
		for _, rec := range b.searchLearnings(query, taskKnowledgeLearningLimit) {
			line := fmt.Sprintf("- [learning:%s confidence=%d] %s", rec.ID, rec.EffectiveConfidence, strings.TrimSpace(rec.Insight))
			if rec.Key != "" {
				line += " (key: " + rec.Key + ")"
			}
			lines = append(lines, line)
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
		}
	}
	if len(lines) == 0 {
		return ""
	}
	header := "RELEVANT TEAM KNOWLEDGE (matched to this task — apply it; cite the learning/wiki id when you do):"
	return header + "\n" + strings.Join(lines, "\n")
}
