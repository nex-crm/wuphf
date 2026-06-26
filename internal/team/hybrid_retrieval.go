package team

// hybrid_retrieval.go — the B4 dense layer behind the U2 retrieval spine
// (docs/specs/core-loop.md, Core Loop step 9: "retrieval + deduping").
//
// The lexical paths (relevantLearnings' IDF token-overlap in
// context_assembler.go, WikiIndex BM25 behind searchWiki) stay the floor:
// they are deterministic and work offline on every self-hosted install.
// When an embedding provider IS configured, this file adds a dense ranking
// over the same candidates and fuses the two orderings with Reciprocal Rank
// Fusion (RRF, k=60) — the standard parameter from the original RRF paper,
// also what hybrid-search engines default to.
//
// Hard degradation contract: every entry point here returns the EXACT
// lexical behavior when no provider is configured. No errors, no empty
// results where lexical would hit. "Configured" means a real provider
// (VOYAGE_API_KEY / OPENAI_API_KEY) or an explicit injection through
// setRetrievalEmbedding (the eval harness uses the deterministic stub this
// way); the env-resolved local-stub floor does NOT count, because stub
// vectors are not semantic (same rule as skill_dedup.go).
//
// Embeddings are cached through internal/embedding's content-hash cache:
// the cache key is sha256(text) + provider/model/dimension, so unchanged
// record texts are never re-embedded.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/embedding"
)

const (
	// rrfK is the Reciprocal Rank Fusion constant: score += 1/(k + rank).
	rrfK = 60
	// denseCosineFloor gates dense-only candidates (records with no lexical
	// overlap at all). Without a floor, a dense pass over arbitrary text
	// would spray weakly-similar records into every packet — the exact
	// failure the lexical minOverlap guard exists to prevent.
	denseCosineFloor = float32(0.3)
	// denseEmbedTimeout bounds the embedding pass per retrieval call. The
	// lexical result is already in hand when the dense pass runs, so a slow
	// provider degrades to lexical-only rather than blocking the packet.
	denseEmbedTimeout = 3 * time.Second
	// hybridWikiCandidateFactor widens the BM25 candidate pool before the
	// dense rerank so the fusion has something to reorder.
	hybridWikiCandidateFactor = 4
)

// retrievalEmbedding{Mu,Provider,Cache} hold the explicit injection seam.
// Production resolves from the environment on every call (cheap — NewDefault
// is just env reads); the eval harness and tests inject the deterministic
// stub plus a scratch cache so no network or $HOME state is touched.
var (
	retrievalEmbeddingMu               sync.RWMutex
	retrievalEmbeddingOverrideSet      bool
	retrievalEmbeddingProviderOverride embedding.Provider
	retrievalEmbeddingCacheOverride    *embedding.Cache

	retrievalEmbeddingCacheOnce    sync.Once
	retrievalEmbeddingCacheDefault *embedding.Cache
)

// setRetrievalEmbedding installs an explicit provider + cache for the dense
// retrieval path. Passing a nil provider forces lexical-only retrieval
// regardless of env keys (hermetic eval control). Eval/test seam —
// production never calls this; pair with resetRetrievalEmbedding.
func setRetrievalEmbedding(p embedding.Provider, c *embedding.Cache) {
	retrievalEmbeddingMu.Lock()
	defer retrievalEmbeddingMu.Unlock()
	retrievalEmbeddingOverrideSet = true
	retrievalEmbeddingProviderOverride = p
	retrievalEmbeddingCacheOverride = c
}

// resetRetrievalEmbedding restores env-based provider resolution.
func resetRetrievalEmbedding() {
	retrievalEmbeddingMu.Lock()
	defer retrievalEmbeddingMu.Unlock()
	retrievalEmbeddingOverrideSet = false
	retrievalEmbeddingProviderOverride = nil
	retrievalEmbeddingCacheOverride = nil
}

// retrievalEmbeddingProvider returns the configured dense provider, or nil
// when dense retrieval is unavailable. nil means "lexical only" — every
// caller MUST degrade gracefully to the existing behavior.
func retrievalEmbeddingProvider() embedding.Provider {
	retrievalEmbeddingMu.RLock()
	overrideSet := retrievalEmbeddingOverrideSet
	override := retrievalEmbeddingProviderOverride
	retrievalEmbeddingMu.RUnlock()
	if overrideSet {
		return override
	}
	provider := embedding.NewDefault()
	// NewDefault never returns nil; the local-stub floor is rejected because
	// its hash-bucket vectors are not semantic (same rule as skill_dedup.go).
	if provider.Name() == "local-stub" {
		return nil
	}
	return provider
}

// retrievalEmbeddingCache returns the embedding cache paired with the
// active provider. The default is the shared content-hash JSONL cache at
// $WUPHF_HOME/.wuphf/cache/embeddings.jsonl; an empty path yields a no-op
// cache that always misses (embedding.NewCache handles that).
func retrievalEmbeddingCache() *embedding.Cache {
	retrievalEmbeddingMu.RLock()
	override := retrievalEmbeddingCacheOverride
	retrievalEmbeddingMu.RUnlock()
	if override != nil {
		return override
	}
	retrievalEmbeddingCacheOnce.Do(func() {
		retrievalEmbeddingCacheDefault = embedding.NewCache(embedding.DefaultCachePath())
	})
	return retrievalEmbeddingCacheDefault
}

// embedCached returns the unit vector for text, hitting the content-hash
// cache first so unchanged texts are never re-embedded. Returns nil on any
// failure — callers treat nil as "no dense signal for this record".
func embedCached(ctx context.Context, provider embedding.Provider, cache *embedding.Cache, text string) []float32 {
	text = strings.TrimSpace(text)
	if provider == nil || text == "" {
		return nil
	}
	namespace := embedding.ProviderCacheNamespace(provider)
	if v, ok := cache.GetScoped(text, provider.Name(), namespace, provider.Dimension()); ok {
		return v
	}
	v, err := provider.Embed(ctx, text)
	if err != nil {
		return nil
	}
	// Cache write errors are deliberately dropped: the pipeline must keep
	// working when the cache file is unwritable.
	_ = cache.SetScoped(text, provider.Name(), namespace, v)
	return v
}

// denseRankIndices embeds the query plus every candidate text and returns
// candidate indices ordered by cosine similarity (desc), restricted to
// candidates at or above denseCosineFloor. Deterministic for a deterministic
// provider: ties break on the lower index.
func denseRankIndices(ctx context.Context, provider embedding.Provider, cache *embedding.Cache, query string, texts []string) []int {
	queryVec := embedCached(ctx, provider, cache, query)
	if queryVec == nil {
		return nil
	}
	type densescored struct {
		idx int
		cos float32
	}
	scored := make([]densescored, 0, len(texts))
	for i, text := range texts {
		if ctx.Err() != nil {
			break
		}
		vec := embedCached(ctx, provider, cache, text)
		if vec == nil {
			continue
		}
		cos := embedding.Cosine(queryVec, vec)
		if cos < denseCosineFloor {
			continue
		}
		scored = append(scored, densescored{idx: i, cos: cos})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].cos != scored[j].cos {
			return scored[i].cos > scored[j].cos
		}
		return scored[i].idx < scored[j].idx
	})
	out := make([]int, 0, len(scored))
	for _, s := range scored {
		out = append(out, s.idx)
	}
	return out
}

// rrfFuseIndices fuses any number of index rankings via Reciprocal Rank
// Fusion: fused[idx] = Σ 1/(rrfK + rank + 1). Indices absent from a ranking
// contribute nothing from it.
func rrfFuseIndices(rankings ...[]int) map[int]float64 {
	fused := map[int]float64{}
	for _, ranking := range rankings {
		for rank, idx := range ranking {
			fused[idx] += 1.0 / float64(rrfK+rank+1)
		}
	}
	return fused
}

// hybridWikiSearch is the searchWiki seam (launcher_wiring.go): BM25 alone
// when no provider is configured (byte-identical to the pre-B4 behavior),
// BM25 ∪ dense with RRF fusion when one is.
func hybridWikiSearch(ctx context.Context, idx *WikiIndex, query string, topK int) []SearchHit {
	if idx == nil || topK <= 0 {
		return nil
	}
	provider := retrievalEmbeddingProvider()
	if provider == nil {
		hits, err := idx.Search(ctx, query, topK)
		if err != nil {
			return nil
		}
		return hits
	}
	candidateK := topK * hybridWikiCandidateFactor
	hits, err := idx.Search(ctx, query, candidateK)
	if err != nil {
		return nil
	}
	if len(hits) <= 1 {
		return hits
	}

	embedCtx, cancel := context.WithTimeout(ctx, denseEmbedTimeout)
	defer cancel()
	cache := retrievalEmbeddingCache()
	texts := make([]string, len(hits))
	for i, h := range hits {
		text := strings.TrimSpace(h.Snippet)
		if text == "" {
			text = strings.TrimSpace(h.Entity + " " + h.FactID)
		}
		texts[i] = text
	}
	lexRanking := make([]int, len(hits))
	for i := range hits {
		lexRanking[i] = i
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
		return order[i] < order[j]
	})
	if len(order) > topK {
		order = order[:topK]
	}
	out := make([]SearchHit, 0, len(order))
	for _, idx := range order {
		out = append(out, hits[idx])
	}
	return out
}
