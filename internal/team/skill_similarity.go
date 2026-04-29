package team

// skill_similarity.go — semantic similarity gate for the skill-proposal funnel.
//
// Existing skill proposals dedup on slug only. Stage B can still produce
// near-duplicates with different names ("send-invoice-reminder" vs
// "invoice-d7-reminder" vs "ar-reminder-d7") that all describe the same
// workflow. findSimilarActiveSkillLocked compares a candidate spec against
// every active skill and recommends whether the caller should enhance an
// existing skill instead of creating a new one.
//
// PR 7 /review P1: the helper now takes b.mu internally for the snapshot
// phase, releases it for embedding IO, and operates on the snapshot for
// scoring. Callers MUST NOT hold b.mu when calling. The integration in
// writeSkillProposalLocked (Lane A task #5) routes the verdict into the
// enhance-or-create flow.

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// SkillEmbedder is the minimal embedding surface the similarity gate needs.
// Mirrors the Provider shape from internal/embedding (PR #378). Defined here
// (not imported) so the gate can be developed and tested ahead of that
// package landing on this branch, and so the broker has a single hook point
// to swap implementations.
//
// Implementations must return L2-normalised vectors so cosine similarity
// reduces to a dot product.
type SkillEmbedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Similarity thresholds. Method-specific because cosine over a sentence
// embedding and Jaccard over token sets live on different scales for the
// same conceptual "near duplicate" — cosine clusters near-dups around
// 0.88-0.93, while Jaccard on the same skill text typically lands
// 0.30-0.45 because every body word that differs (templates, examples,
// connector verbs) is a full set element. Holding to one universal
// threshold either fires false positives on cosine or never fires at all
// on Jaccard, which is why the two paths use independently calibrated
// bands.
//
// Cosine bands — calibrated against the eval corpus where
// "send-invoice-reminder" / "invoice-d7-reminder" / "ar-reminder-d7"
// cluster ~0.88-0.93, distinct workflows (deploy-canary vs renewal-
// reminder) sit ~0.30-0.55, and ambiguous edges ("draft-monthly-report"
// vs "compile-quarterly-summary") land ~0.72-0.78.
//
// Jaccard bands — calibrated against the same corpus's tokenised bodies
// where exact-content duplicates with renamed slugs land ~0.85-0.95,
// near-dup workflows with overlapping connector vocabulary land
// ~0.35-0.55, and clearly distinct workflows sit below 0.20.
const (
	similarityCosineEnhanceThreshold    = 0.85
	similarityCosineAmbiguousThreshold  = 0.70
	similarityJaccardEnhanceThreshold   = 0.35
	similarityJaccardAmbiguousThreshold = 0.20
)

// SimilarityVerdict is the result of comparing a candidate spec against the
// active skill catalog. All fields are flat scalars snapshotted under
// b.mu so the verdict is safe to propagate after the lock is released.
//
// PR 7 /review P1: replaced the prior `Existing *teamSkill` pointer (which
// was a footgun — only safe under b.mu, easy to misuse). Callers that need
// more than slug/title/description can re-look up via findSkillByNameLocked
// while holding the lock.
type SimilarityVerdict struct {
	// ExistingSlug is the closest active skill's slug, or "" when no skill
	// crossed the ambiguous threshold.
	ExistingSlug string
	// ExistingTitle is the closest skill's display title at snapshot time.
	ExistingTitle string
	// ExistingDescription is the closest skill's one-line description at
	// snapshot time.
	ExistingDescription string
	Score               float64
	Method              string // "embedding-cosine" | "jaccard-tokens"
	Recommendation      string // "create_new" | "enhance_existing" | "ambiguous"
}

// skillSimilarityCache memoises embeddings per (slug, content-sha) so a
// single compile pass that calls findSimilarActiveSkillLocked once per
// candidate doesn't re-embed every existing skill from scratch. The key
// includes the content sha so an in-place edit invalidates the cached
// vector automatically.
type skillSimilarityCache struct {
	mu      sync.Mutex
	entries map[string][]float32
}

func newSkillSimilarityCache() *skillSimilarityCache {
	return &skillSimilarityCache{entries: make(map[string][]float32)}
}

func (c *skillSimilarityCache) get(key string) ([]float32, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *skillSimilarityCache) put(key string, vec []float32) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = vec
}

// findSimilarActiveSkillLocked returns the closest active skill if one
// exceeds the similarity thresholds.
//
// Lock contract (PR 7 /review P1): the function takes b.mu INTERNALLY.
// Callers MUST NOT hold b.mu. The function snapshots the candidate set
// under lock, RELEASES the lock for the embedding IO calls, then walks
// the snapshot to score (no lock needed — pure-data slice). This prevents
// a slow / hung embedder from freezing every other broker operation
// behind a held mutex.
//
// Inputs hashed for comparison: name + description + first 1KB of body.
// Active-only (status == "active"). Skips any skill whose Name matches the
// spec.Name (case-insensitive) so an in-place update doesn't self-match.
//
// When b.skillEmbedder is set, cosine similarity over normalised embeddings
// is used (Method = "embedding-cosine"). When the embedder is unavailable
// or fails — including ctx deadline — the helper falls back to
// token-Jaccard over the same fields (Method = "jaccard-tokens").
//
// The verdict carries flat ExistingSlug / ExistingTitle /
// ExistingDescription strings (no pointer escape).
//
// Name kept as "Locked" suffix for back-compat with PR 7's earlier
// landings that referenced this symbol; the lock contract was rotated in
// the /review P1 fix without renaming to avoid churn at every call site.
func (b *Broker) findSimilarActiveSkillLocked(ctx context.Context, spec teamSkill) SimilarityVerdict {
	specName := strings.ToLower(strings.TrimSpace(spec.Name))
	specText := similarityComparable(spec)

	// Snapshot the eligible candidate set + the embedder reference under
	// lock. The slice is a pure-data copy (slug + comparable text), no
	// pointers back into b.skills, so it stays safe to read after release.
	b.mu.Lock()
	method := similarityMethodFor(b)
	if len(b.skills) == 0 || specText == "" {
		b.mu.Unlock()
		return SimilarityVerdict{Recommendation: "create_new", Method: method}
	}
	candidates := make([]similarityCandidate, 0, len(b.skills))
	for i := range b.skills {
		sk := &b.skills[i]
		if !skillSimilarityEligible(sk, specName) {
			continue
		}
		text := similarityComparable(*sk)
		if text == "" {
			continue
		}
		candidates = append(candidates, similarityCandidate{
			Slug:        skillSlug(sk.Name),
			Title:       strings.TrimSpace(sk.Title),
			Description: strings.TrimSpace(sk.Description),
			Text:        text,
		})
	}
	embedder := b.skillEmbedder
	cache := b.skillSimCache
	if cache == nil && embedder != nil {
		b.skillSimCache = newSkillSimilarityCache()
		cache = b.skillSimCache
	}
	b.mu.Unlock()

	// Try embedding path first when available; fall through to Jaccard on
	// any error (including ctx deadline) so a flaky or hung provider can't
	// block proposal writes.
	if embedder != nil {
		if v, ok := scoreCandidatesByEmbedding(ctx, embedder, cache, specText, candidates); ok {
			return v
		}
	}
	return scoreCandidatesByJaccard(specText, candidates)
}

// similarityCandidate is the per-skill snapshot the embedding path captures
// under b.mu before releasing for IO. Holds only the fields the comparison
// needs — never a pointer back into b.skills.
type similarityCandidate struct {
	Slug        string
	Title       string
	Description string
	Text        string // similarityComparable output
}

func similarityMethodFor(b *Broker) string {
	if b.skillEmbedder != nil {
		return "embedding-cosine"
	}
	return "jaccard-tokens"
}

// scoreCandidatesByEmbedding embeds the spec + each candidate and returns
// the best-scoring verdict. Pure function — operates on the snapshot
// captured under lock by findSimilarActiveSkillLocked, calls the embedder
// without holding b.mu (PR 7 /review P1).
func scoreCandidatesByEmbedding(ctx context.Context, embedder SkillEmbedder, cache *skillSimilarityCache, specText string, candidates []similarityCandidate) (SimilarityVerdict, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	specVec, err := embedTextWithCache(ctx, embedder, cache, "__candidate__", specText)
	if err != nil || len(specVec) == 0 {
		return SimilarityVerdict{}, false
	}

	var (
		bestVal float64
		best    similarityCandidate
	)
	for _, cand := range candidates {
		vec, err := embedTextWithCache(ctx, embedder, cache, cand.Slug, cand.Text)
		if err != nil || len(vec) == 0 {
			// Bail to Jaccard for the whole call rather than silently
			// scoring the candidate against a partial catalog.
			return SimilarityVerdict{}, false
		}
		score := cosineSimilarity(specVec, vec)
		if score > bestVal {
			bestVal = score
			best = cand
		}
	}

	v := SimilarityVerdict{Score: bestVal, Method: "embedding-cosine"}
	if best.Slug != "" && bestVal >= similarityCosineAmbiguousThreshold {
		v.ExistingSlug = best.Slug
		v.ExistingTitle = best.Title
		v.ExistingDescription = best.Description
	}
	v.Recommendation = recommendationFor(bestVal, similarityCosineEnhanceThreshold, similarityCosineAmbiguousThreshold)
	return v, true
}

// scoreCandidatesByJaccard runs token-Jaccard scoring over the snapshot.
// Pure function — no lock needed.
func scoreCandidatesByJaccard(specText string, candidates []similarityCandidate) SimilarityVerdict {
	specTokens := similarityTokenSet(specText)
	if len(specTokens) == 0 {
		return SimilarityVerdict{Recommendation: "create_new", Method: "jaccard-tokens"}
	}

	var (
		bestScore float64
		best      similarityCandidate
	)
	for _, cand := range candidates {
		score := jaccardSets(specTokens, similarityTokenSet(cand.Text))
		if score > bestScore {
			bestScore = score
			best = cand
		}
	}

	v := SimilarityVerdict{Score: bestScore, Method: "jaccard-tokens"}
	if best.Slug != "" && bestScore >= similarityJaccardAmbiguousThreshold {
		v.ExistingSlug = best.Slug
		v.ExistingTitle = best.Title
		v.ExistingDescription = best.Description
	}
	v.Recommendation = recommendationFor(bestScore, similarityJaccardEnhanceThreshold, similarityJaccardAmbiguousThreshold)
	return v
}

func recommendationFor(score, enhance, ambiguous float64) string {
	switch {
	case score >= enhance:
		return "enhance_existing"
	case score >= ambiguous:
		return "ambiguous"
	default:
		return "create_new"
	}
}

// skillSimilarityEligible filters the active-only, not-self set used by
// both the embedding and Jaccard paths.
func skillSimilarityEligible(sk *teamSkill, specName string) bool {
	if sk == nil {
		return false
	}
	if sk.Status != "active" {
		return false
	}
	if specName != "" && strings.ToLower(strings.TrimSpace(sk.Name)) == specName {
		return false
	}
	return true
}

// similarityComparable returns the canonical comparison string for a skill:
// name + description + first 1KB of body. Whitespace is collapsed and the
// result is lowercased so embeddings and Jaccard see the same payload.
func similarityComparable(sk teamSkill) string {
	const bodyCap = 1024
	body := sk.Content
	if len(body) > bodyCap {
		body = body[:bodyCap]
	}
	parts := []string{
		strings.TrimSpace(sk.Name),
		strings.TrimSpace(sk.Description),
		strings.TrimSpace(body),
	}
	joined := strings.ToLower(strings.Join(parts, "\n"))
	return strings.Join(strings.Fields(joined), " ")
}

// embedTextWithCache memoises the embedder output by (slug, content-sha)
// so a single compile pass doesn't re-embed every existing skill from
// scratch. Pure function — caller passes in the embedder and cache so the
// scoring path can run without holding b.mu (PR 7 /review P1).
func embedTextWithCache(ctx context.Context, embedder SkillEmbedder, cache *skillSimilarityCache, slug, text string) ([]float32, error) {
	if embedder == nil {
		return nil, fmt.Errorf("skill embedder not configured")
	}
	key := slug + ":" + sha256Hex(text)
	if cache != nil {
		if v, ok := cache.get(key); ok {
			return v, nil
		}
	}
	vec, err := embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		cache.put(key, vec)
	}
	return vec, nil
}

// cosineSimilarity returns the dot product of two vectors clamped to [0,1].
// Inputs are expected to be L2-normalised; if they aren't, this still
// produces a monotonic ranking, just one that won't match the published
// thresholds. Negative similarity (vectors pointing the wrong way) is
// clamped to 0 so downstream comparisons stay in [0,1].
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	if dot < 0 {
		return 0
	}
	if dot > 1 {
		return 1
	}
	return dot
}

// similarityTokenSet splits s on non-alphanumeric runs, lowercases, drops
// single-char tokens, and returns a set view. Stopwords are not removed:
// skill bodies are short and common-word overlap is part of the signal.
//
// Mirrors the shape consumed by jaccardSets in notebook_signal_scanner.go.
func similarityTokenSet(s string) map[string]bool {
	out := make(map[string]bool)
	var cur []rune
	flush := func() {
		if len(cur) > 1 {
			out[string(cur)] = true
		}
		cur = cur[:0]
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur = append(cur, r)
			continue
		}
		flush()
	}
	flush()
	return out
}
