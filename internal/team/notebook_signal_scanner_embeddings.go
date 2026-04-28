package team

// notebook_signal_scanner_embeddings.go is the semantic-clustering side of
// the NotebookSignalScanner. It mirrors the Jaccard path in
// notebook_signal_scanner.go but groups entries by cosine similarity over
// real text embeddings instead of token-set overlap.
//
// The contract from the synthesizer's perspective is unchanged: emit
// SkillCandidate values that pass minClusterSize + minDistinctAgents.
// Only the clustering algorithm differs.
//
// Telemetry hooks live on b.skillCompileMetrics:
//
//	EmbeddingCallsTotal       — every Embed call (cache miss or live)
//	EmbeddingCacheHitsTotal   — cache hit count
//	EmbeddingCacheMissesTotal — cache miss count
//	EmbeddingCostUsd          — running approximate USD cost (uint64-bits)

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/nex-crm/wuphf/internal/embedding"
)

// defaultNotebookCosineThreshold is the cosine similarity floor used for
// "same topic" semantic clustering. 0.8 is the documented default; the
// env var WUPHF_STAGE_B_NOTEBOOK_COSINE_THRESHOLD overrides.
const defaultNotebookCosineThreshold = 0.8

// embeddingClusteringEnabled inspects the env to decide whether the
// scanner should use the embedding path. The default is "true if a real
// provider is configured, false otherwise" — i.e. callers running without
// an API key (most CI environments) get the deterministic Jaccard path
// for stable test outputs.
//
// Explicit overrides:
//
//	WUPHF_STAGE_B_USE_EMBEDDINGS=1     → force on
//	WUPHF_STAGE_B_USE_EMBEDDINGS=0     → force off (regression-safe escape)
//	WUPHF_STAGE_B_USE_EMBEDDINGS=auto  → default (treat empty as "auto")
func embeddingClusteringEnabled(provider embedding.Provider) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("WUPHF_STAGE_B_USE_EMBEDDINGS")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	if provider == nil {
		return false
	}
	// Stub provider's vectors are not semantic — keep the deterministic
	// Jaccard path for test runs that haven't configured a real provider.
	return provider.Name() != "local-stub"
}

// clusterNotebookEntriesByEmbedding is the embedding counterpart to
// clusterNotebookEntries (Jaccard). It embeds every entry, clusters by
// cosine similarity, and returns notebookCluster values shaped the same
// way the Jaccard path produces.
//
// Cache misses are logged + counted via b.skillCompileMetrics. Failures
// fall through with the entries that did embed — clustering on a partial
// set is better than emitting no signals.
func (s *NotebookSignalScanner) clusterNotebookEntriesByEmbedding(ctx context.Context, entries []notebookEntry) []notebookCluster {
	if len(entries) == 0 || s == nil || s.embeddingProvider == nil {
		return nil
	}
	threshold := float32(s.embeddingCosineThreshold())

	cacheTexts := make([]string, len(entries))
	for i, e := range entries {
		cacheTexts[i] = e.body
	}

	vectors := s.embedAllWithCache(ctx, cacheTexts)

	clusterEntries := make([]embedding.ClusterEntry, 0, len(entries))
	indexByID := map[string]int{} // ID → entries index
	for i, vec := range vectors {
		if len(vec) == 0 {
			continue
		}
		id := entries[i].relPath
		clusterEntries = append(clusterEntries, embedding.ClusterEntry{
			ID:     id,
			Text:   entries[i].body,
			Vector: vec,
		})
		indexByID[id] = i
	}

	if len(clusterEntries) == 0 {
		return nil
	}

	clusters, _ := embedding.ClusterByCosineWithSkipped(clusterEntries, threshold)

	out := make([]notebookCluster, 0, len(clusters))
	for _, c := range clusters {
		members := make([]notebookEntry, 0, len(c.Entries))
		centroid := map[string]bool{}
		for _, ce := range c.Entries {
			idx, ok := indexByID[ce.ID]
			if !ok {
				continue
			}
			members = append(members, entries[idx])
			for k := range entries[idx].tokenSet {
				centroid[k] = true
			}
		}
		if len(members) == 0 {
			continue
		}
		out = append(out, notebookCluster{
			members:  members,
			centroid: centroid,
		})
	}
	return out
}

// embedAllWithCache returns one vector per input. Misses go through the
// provider; hits come from the on-disk cache. Cost telemetry is updated
// per call.
//
// Errors do not propagate: the provider may rate-limit or hit a network
// blip on a single text. We log + skip and return an empty vector at
// that index so the caller knows the entry is unusable.
func (s *NotebookSignalScanner) embedAllWithCache(ctx context.Context, texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	if s.embeddingProvider == nil {
		return out
	}
	model := s.embeddingProvider.Name()

	type pending struct {
		text  string
		index int
	}
	var misses []pending

	for i, t := range texts {
		if v, ok := s.embeddingCache.Get(t, model); ok {
			out[i] = v
			s.recordCacheHit()
			continue
		}
		misses = append(misses, pending{text: t, index: i})
	}

	if len(misses) == 0 {
		return out
	}

	missTexts := make([]string, len(misses))
	for i, m := range misses {
		missTexts[i] = m.text
	}
	vectors, err := s.embeddingProvider.EmbedBatch(ctx, missTexts)
	s.recordCacheMiss(len(misses))
	if err != nil {
		slog.Warn("notebook_embedding_batch_failed",
			"err", err, "model", model, "texts", len(misses))
		return out
	}
	if len(vectors) != len(misses) {
		slog.Warn("notebook_embedding_batch_mismatch",
			"got", len(vectors), "want", len(misses), "model", model)
		return out
	}
	for i, m := range misses {
		out[m.index] = vectors[i]
		_ = s.embeddingCache.Set(m.text, model, vectors[i])
		s.recordEmbedCall(m.text)
	}
	return out
}

// embeddingCosineThreshold reads
// WUPHF_STAGE_B_NOTEBOOK_COSINE_THRESHOLD or returns the default.
// Clamped to [0,1] so a misconfigured env can't disable clustering.
func (s *NotebookSignalScanner) embeddingCosineThreshold() float64 {
	v := envFloatDefault("WUPHF_STAGE_B_NOTEBOOK_COSINE_THRESHOLD", defaultNotebookCosineThreshold)
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// recordEmbedCall increments EmbeddingCallsTotal and adds an approximate
// cost into EmbeddingCostUsd. Cost is text-embedding-3-small priced
// (~$0.02 / 1M tokens). Tokens ≈ runes / 4. Wrong by a constant factor
// for other models — set WUPHF_EMBEDDING_COST_PER_TOKEN_USD if your
// model is meaningfully more expensive (Voyage voyage-3-large at
// $0.18/1M tokens needs the override).
func (s *NotebookSignalScanner) recordEmbedCall(text string) {
	if s == nil || s.broker == nil {
		return
	}
	atomic.AddInt64(&s.broker.skillCompileMetrics.EmbeddingCallsTotal, 1)
	tokens := approxTokenCount(text)
	cost := float64(tokens) * embeddingCostPerTokenUsd()
	addFloatBits(&s.broker.skillCompileMetrics.EmbeddingCostUsdBits, cost)
}

// recordCacheHit / recordCacheMiss bump the corresponding atomic counters.
func (s *NotebookSignalScanner) recordCacheHit() {
	if s == nil || s.broker == nil {
		return
	}
	atomic.AddInt64(&s.broker.skillCompileMetrics.EmbeddingCacheHitsTotal, 1)
}

func (s *NotebookSignalScanner) recordCacheMiss(n int) {
	if s == nil || s.broker == nil || n <= 0 {
		return
	}
	atomic.AddInt64(&s.broker.skillCompileMetrics.EmbeddingCacheMissesTotal, int64(n))
}

// approxTokenCount returns runes/4, the rule-of-thumb token estimate.
// Real tokenisers diverge; this is intentionally cheap.
func approxTokenCount(text string) int {
	r := len([]rune(text))
	if r == 0 {
		return 0
	}
	t := r / 4
	if t == 0 {
		t = 1
	}
	return t
}

// embeddingCostPerTokenUsd returns the per-token USD cost. Default is
// $0.02 / 1M tokens (text-embedding-3-small). Override with
// WUPHF_EMBEDDING_COST_PER_TOKEN_USD for other models.
func embeddingCostPerTokenUsd() float64 {
	const defaultCost = 0.02 / 1_000_000.0
	v := envFloatDefault("WUPHF_EMBEDDING_COST_PER_TOKEN_USD", defaultCost)
	if v < 0 {
		return defaultCost
	}
	return v
}

// addFloatBits atomically adds delta to the float64 stored in slot using
// math.Float64bits. The compare-and-swap loop handles concurrent
// writers — a slow goroutine retries until its read-modify-write reflects
// the latest value.
func addFloatBits(slot *uint64, delta float64) {
	for {
		oldBits := atomic.LoadUint64(slot)
		oldVal := math.Float64frombits(oldBits)
		newVal := oldVal + delta
		if atomic.CompareAndSwapUint64(slot, oldBits, math.Float64bits(newVal)) {
			return
		}
	}
}

// loadFloatBits atomically reads the float64 in slot.
func loadFloatBits(slot *uint64) float64 {
	return math.Float64frombits(atomic.LoadUint64(slot))
}

// reportClusterCounts logs the cluster shape for telemetry parity with
// the Jaccard path. Lives here so the embedding code-path is fully
// observable without spreading slog calls through the scanner.
func reportClusterCounts(prefix string, clusters []notebookCluster) {
	if len(clusters) == 0 {
		return
	}
	sizes := make([]int, len(clusters))
	for i, c := range clusters {
		sizes[i] = len(c.members)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
	slog.Debug(prefix+"_cluster_sizes",
		"clusters", len(clusters),
		"sizes", fmt.Sprintf("%v", sizes),
	)
}
