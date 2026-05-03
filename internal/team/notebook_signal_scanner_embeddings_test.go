package team

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nex-crm/wuphf/internal/embedding"
)

// stableEmbeddingProvider is a tiny test stub that returns hand-crafted
// vectors keyed off the input text's first token. Lets us pin clusters
// to specific assertions without depending on the FNV-bucket arithmetic
// inside the production stub provider.
type stableEmbeddingProvider struct {
	dim         int
	vectors     map[string][]float32
	batchCalls  int
	batchInputs int
}

func (p *stableEmbeddingProvider) Name() string   { return "test-stable" }
func (p *stableEmbeddingProvider) Dimension() int { return p.dim }

func (p *stableEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := p.vectors[text]; ok {
		return embedding.L2Normalise(v), nil
	}
	// Default: random non-clustering vector keyed off length so distinct
	// inputs land in different buckets but never close to each other.
	v := make([]float32, p.dim)
	v[len(text)%p.dim] = 1
	return embedding.L2Normalise(v), nil
}

func (p *stableEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	p.batchCalls++
	p.batchInputs += len(texts)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := p.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// newStableEmbeddingProvider builds a provider whose vectors are dense,
// not sparse — sparse vectors over a 4-dim space alias too easily.
func newStableEmbeddingProvider(t *testing.T, vecs map[string][]float32) *stableEmbeddingProvider {
	t.Helper()
	return &stableEmbeddingProvider{dim: 4, vectors: vecs}
}

func TestNotebookSignalScanner_EmbeddingPath_ClustersByCosine(t *testing.T) {
	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "1")

	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	bodyA := "deploy prod pipeline smoke tests"
	bodyB := "shipping prod release with smoke tests today"
	bodyC := "rolling deploy to prod cluster after smoke tests passed"

	writeNotebookEntry(t, root, "alice", "2026-04-22", bodyA)
	writeNotebookEntry(t, root, "bob", "2026-04-23", bodyB)
	writeNotebookEntry(t, root, "carol", "2026-04-24", bodyC)

	// Wire all three bodies to nearly-identical vectors so cosine
	// similarity passes the 0.8 default threshold even though their
	// token sets only partially overlap (the Jaccard path would not
	// merge them as cleanly).
	provider := newStableEmbeddingProvider(t, map[string][]float32{
		bodyA: {1, 0.05, 0.05, 0.02},
		bodyB: {0.95, 0.1, 0.07, 0.05},
		bodyC: {0.97, 0.02, 0.08, 0.04},
	})

	scanner := NewNotebookSignalScanner(b)
	scanner.SetEmbeddingProvider(provider)
	scanner.SetEmbeddingCache(embedding.NewCache(filepath.Join(t.TempDir(), "cache.jsonl")))

	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d (%+v)", len(cands), cands)
	}
	if cands[0].SignalCount != 3 {
		t.Errorf("signal count: got %d want 3", cands[0].SignalCount)
	}
	if got := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCallsTotal); got != 3 {
		t.Errorf("embedding calls: got %d want 3", got)
	}
	if got := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCacheMissesTotal); got != 3 {
		t.Errorf("cache misses: got %d want 3", got)
	}
	if cost := loadFloatBits(&b.skillCompileMetrics.EmbeddingCostUsdBits); cost <= 0 {
		t.Errorf("cost: got %v want > 0", cost)
	}
}

func TestNotebookSignalScanner_EmbeddingPath_RejectsSingleton(t *testing.T) {
	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "1")

	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	writeNotebookEntry(t, root, "alice", "2026-04-22", "lonely entry not enough to cluster")

	provider := newStableEmbeddingProvider(t, map[string][]float32{
		"lonely entry not enough to cluster": {1, 0, 0, 0},
	})
	scanner := NewNotebookSignalScanner(b)
	scanner.SetEmbeddingProvider(provider)
	scanner.SetEmbeddingCache(embedding.NewCache(filepath.Join(t.TempDir(), "cache.jsonl")))

	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(cands))
	}
}

func TestNotebookSignalScanner_EmbeddingPath_CacheHitsNoOp(t *testing.T) {
	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "1")

	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	bodyA := "embedding cache test entry alpha"
	bodyB := "embedding cache test entry beta"
	bodyC := "embedding cache test entry gamma"

	writeNotebookEntry(t, root, "alice", "2026-04-22", bodyA)
	writeNotebookEntry(t, root, "bob", "2026-04-23", bodyB)
	writeNotebookEntry(t, root, "carol", "2026-04-24", bodyC)

	provider := newStableEmbeddingProvider(t, map[string][]float32{
		bodyA: {1, 0.05, 0.05, 0.02},
		bodyB: {0.95, 0.1, 0.07, 0.05},
		bodyC: {0.97, 0.02, 0.08, 0.04},
	})
	cache := embedding.NewCache(filepath.Join(t.TempDir(), "cache.jsonl"))

	scanner := NewNotebookSignalScanner(b)
	scanner.SetEmbeddingProvider(provider)
	scanner.SetEmbeddingCache(cache)

	if _, err := scanner.Scan(context.Background()); err != nil {
		t.Fatalf("first scan: %v", err)
	}

	missesAfterFirst := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCacheMissesTotal)
	if missesAfterFirst != 3 {
		t.Fatalf("first scan misses: got %d want 3", missesAfterFirst)
	}

	// Second scan: every text should hit the cache, so cache misses do
	// not increase. Cache hits go up by 3.
	if _, err := scanner.Scan(context.Background()); err != nil {
		t.Fatalf("second scan: %v", err)
	}

	missesAfterSecond := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCacheMissesTotal)
	hitsAfterSecond := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCacheHitsTotal)
	if missesAfterSecond != missesAfterFirst {
		t.Errorf("second scan should not miss: got %d want %d", missesAfterSecond, missesAfterFirst)
	}
	if hitsAfterSecond < 3 {
		t.Errorf("hits: got %d want >=3", hitsAfterSecond)
	}
}

func TestNotebookSignalScanner_EmbeddingPath_DedupesBatchMisses(t *testing.T) {
	b, _, teardown := newNotebookScannerHarness(t)
	defer teardown()

	bodyA := "embedding cache duplicate alpha"
	bodyB := "embedding cache duplicate beta"
	provider := newStableEmbeddingProvider(t, map[string][]float32{
		bodyA: {1, 0.05, 0.05, 0.02},
		bodyB: {0.95, 0.1, 0.07, 0.05},
	})

	scanner := NewNotebookSignalScanner(b)
	scanner.SetEmbeddingProvider(provider)
	scanner.SetEmbeddingCache(embedding.NewCache(filepath.Join(t.TempDir(), "cache.jsonl")))

	got := scanner.embedAllWithCache(context.Background(), []string{bodyA, bodyA, bodyB})
	if len(got) != 3 {
		t.Fatalf("vectors: got %d want 3", len(got))
	}
	if provider.batchCalls != 1 {
		t.Fatalf("batch calls: got %d want 1", provider.batchCalls)
	}
	if provider.batchInputs != 2 {
		t.Fatalf("batch inputs: got %d want 2 unique texts", provider.batchInputs)
	}
	if calls := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCallsTotal); calls != 2 {
		t.Fatalf("embedding calls: got %d want 2 unique texts", calls)
	}
	if misses := atomic.LoadInt64(&b.skillCompileMetrics.EmbeddingCacheMissesTotal); misses != 2 {
		t.Fatalf("cache misses: got %d want 2 unique texts", misses)
	}
}

func TestEmbeddingClusteringEnabled_RespectsEnv(t *testing.T) {
	provider := embedding.NewStubProvider()

	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "")
	if embeddingClusteringEnabled(provider) {
		t.Error("auto + stub provider: should not be enabled")
	}

	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "1")
	if !embeddingClusteringEnabled(provider) {
		t.Error("force-on: should override stub-provider default")
	}

	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "0")
	if embeddingClusteringEnabled(provider) {
		t.Error("force-off: should disable even with real provider")
	}

	t.Setenv("WUPHF_STAGE_B_USE_EMBEDDINGS", "")
	if embeddingClusteringEnabled(nil) {
		t.Error("nil provider: should not be enabled")
	}
}

func TestApproxTokenCount(t *testing.T) {
	if got := approxTokenCount(""); got != 0 {
		t.Errorf("empty: got %d want 0", got)
	}
	if got := approxTokenCount("abcd"); got != 1 {
		t.Errorf("4 runes: got %d want 1", got)
	}
	if got := approxTokenCount("a"); got != 1 {
		t.Errorf("single rune: got %d want 1", got)
	}
}

func TestAddFloatBits_AccumulatesAtomically(t *testing.T) {
	var slot uint64
	for i := 0; i < 5; i++ {
		addFloatBits(&slot, 0.1)
	}
	got := loadFloatBits(&slot)
	if got < 0.49 || got > 0.51 {
		t.Errorf("got %v want ~0.5", got)
	}
}
