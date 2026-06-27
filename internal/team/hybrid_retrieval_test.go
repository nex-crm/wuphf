package team

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/embedding"
)

// newTestLearningLog spins a real wiki worker + learning log for retrieval
// tests. Cleans up via t.Cleanup.
func newTestLearningLog(t *testing.T) *LearningLog {
	t.Helper()
	worker, cancel := newStartedWikiWorkerForTest(t)
	t.Cleanup(cancel)
	return NewLearningLog(worker)
}

func seedLearning(t *testing.T, log *LearningLog, key, insight string) LearningRecord {
	t.Helper()
	rec, err := log.Append(context.Background(), LearningRecord{
		Type: "operational", Key: key, Insight: insight,
		Confidence: 8, Source: "execution", Trusted: true, Scope: "team",
		CreatedBy: "eng", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed learning %q: %v", key, err)
	}
	return rec
}

func resultIDs(results []LearningSearchResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.ID)
	}
	return out
}

func containsID(results []LearningSearchResult, id string) bool {
	for _, r := range results {
		if r.ID == id {
			return true
		}
	}
	return false
}

// TestRelevantLearningsLexicalOnlyWithoutProvider pins the degradation
// contract: with no embedding provider, a record sharing zero lexical
// tokens with the query is NOT retrievable and the token-overlap path is
// byte-identical to the pre-B4 behavior.
func TestRelevantLearningsLexicalOnlyWithoutProvider(t *testing.T) {
	setRetrievalEmbedding(nil, nil) // force lexical regardless of host env keys
	t.Cleanup(resetRetrievalEmbedding)
	log := newTestLearningLog(t)
	adjacent := seedLearning(t, log, "q3-ai-ux", "q3 ai ux: cap nav depth at two levels")
	lexical := seedLearning(t, log, "acme-renewal-email", "Acme renewals: always CC the CSM and lead with the usage-growth chart.")

	if got := relevantLearnings(log, "Q3 AI UX pass", 5); containsID(got, adjacent.ID) {
		t.Fatalf("lexical-only retrieval returned the zero-token-overlap record: %v", resultIDs(got))
	}
	got := relevantLearnings(log, "Draft the Acme renewal email", 5)
	if !containsID(got, lexical.ID) {
		t.Fatalf("lexical retrieval lost the token-overlap record: %v", resultIDs(got))
	}
}

// TestRelevantLearningsHybridRRF pins the dense path: with the
// deterministic stub configured, a record adjacent only through dense
// vectors ranks in the top-k, lexical hits survive the fusion, and record
// texts embed exactly once through the content-hash cache.
func TestRelevantLearningsHybridRRF(t *testing.T) {
	cache := embedding.NewCache(filepath.Join(t.TempDir(), "embeddings.jsonl"))
	setRetrievalEmbedding(embedding.NewStubProvider(), cache)
	t.Cleanup(resetRetrievalEmbedding)
	log := newTestLearningLog(t)
	adjacent := seedLearning(t, log, "q3-ai-ux", "q3 ai ux: cap nav depth at two levels")
	lexical := seedLearning(t, log, "acme-renewal-email", "Acme renewals: always CC the CSM and lead with the usage-growth chart.")
	seedLearning(t, log, "webhook-signing", "Billing webhooks: rotate signing keys quarterly.")

	if got := relevantLearnings(log, "Q3 AI UX pass", 5); !containsID(got, adjacent.ID) {
		t.Fatalf("hybrid retrieval missed the dense-adjacent record: %v", resultIDs(got))
	}
	if got := relevantLearnings(log, "Draft the Acme renewal email", 5); !containsID(got, lexical.ID) {
		t.Fatalf("hybrid retrieval lost the lexical hit: %v", resultIDs(got))
	}

	first := cache.Stats().Entries
	if first == 0 {
		t.Fatal("expected embeddings to land in the content-hash cache")
	}
	_ = relevantLearnings(log, "Q3 AI UX pass", 5)
	if second := cache.Stats().Entries; second != first {
		t.Fatalf("unchanged texts were re-embedded: cache entries %d -> %d", first, second)
	}
}

// TestRRFFuseIndices pins the fusion arithmetic: an index present in both
// rankings outscores indices present in only one.
func TestRRFFuseIndices(t *testing.T) {
	fused := rrfFuseIndices([]int{0, 1}, []int{1, 2})
	if fused[1] <= fused[0] || fused[1] <= fused[2] {
		t.Fatalf("index in both rankings must win: %v", fused)
	}
	if len(fused) != 3 {
		t.Fatalf("expected 3 fused candidates, got %v", fused)
	}
}

// TestDenseRankIndicesFloor pins the cosine floor: unrelated texts never
// enter the dense ranking, related ones rank by similarity.
func TestDenseRankIndicesFloor(t *testing.T) {
	provider := embedding.NewStubProvider()
	cache := embedding.NewCache("") // no-op cache
	ctx := context.Background()
	texts := []string{
		"q3 ai ux cap nav depth",             // shares q3/ai/ux with the query
		"completely unrelated zebra granite", // shares nothing
	}
	ranked := denseRankIndices(ctx, provider, cache, "q3 ai ux pass", texts)
	if len(ranked) == 0 || ranked[0] != 0 {
		t.Fatalf("expected the token-sharing text to rank first, got %v", ranked)
	}
	for _, idx := range ranked {
		if idx == 1 {
			t.Fatalf("unrelated text cleared the cosine floor: %v", ranked)
		}
	}
}

// TestHybridWikiSearchNilSafe pins the degradation edges of the wiki seam.
func TestHybridWikiSearchNilSafe(t *testing.T) {
	setRetrievalEmbedding(nil, nil)
	t.Cleanup(resetRetrievalEmbedding)
	if got := hybridWikiSearch(context.Background(), nil, "query", 3); got != nil {
		t.Fatalf("nil index must return nil, got %v", got)
	}
	if got := hybridWikiSearch(context.Background(), nil, "query", 0); got != nil {
		t.Fatalf("topK<=0 must return nil, got %v", got)
	}
}
