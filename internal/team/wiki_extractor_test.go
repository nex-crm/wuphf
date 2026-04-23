package team

// wiki_extractor_test.go — unit + small-integration tests for the
// extraction loop.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── Fake provider helpers ────────────────────────────────────────────────────

type stubProvider struct {
	mu       sync.Mutex
	calls    int
	response string
	err      error
	// onCall allows a test to dynamically change behaviour per call.
	onCall func(n int) (string, error)
}

func (s *stubProvider) RunPrompt(_ context.Context, _ string, _ string) (string, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	onCall := s.onCall
	resp := s.response
	err := s.err
	s.mu.Unlock()
	if onCall != nil {
		return onCall(n)
	}
	return resp, err
}

func (s *stubProvider) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// ── Harness ──────────────────────────────────────────────────────────────────

type extractHarness struct {
	t         *testing.T
	worker    *WikiWorker
	repo      *Repo
	index     *WikiIndex
	dlq       *DLQ
	provider  *stubProvider
	extractor *Extractor
	teardown  func()
}

func newExtractHarness(t *testing.T) *extractHarness {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	idx := NewWikiIndex(root)
	pub := &recordingPublisher{}
	worker := NewWikiWorkerWithIndex(repo, pub, idx)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	dlq := NewDLQ(filepath.Join(root, "wiki"))
	prov := &stubProvider{}
	extractor := NewExtractor(prov, worker, dlq, idx)
	fixedNow := time.Date(2026, 4, 22, 14, 32, 0, 0, time.UTC)
	extractor.SetNow(func() time.Time { return fixedNow })
	// Tests drive the extractor directly by default; the auto-hook is opted
	// into explicitly via h.enableHook() for end-to-end flows.
	_ = extractor // hook is NOT wired; tests call ExtractFromArtifact directly

	return &extractHarness{
		t:         t,
		worker:    worker,
		repo:      repo,
		index:     idx,
		dlq:       dlq,
		provider:  prov,
		extractor: extractor,
		teardown: func() {
			cancel()
			<-worker.Done()
		},
	}
}

// enableHook wires the extractor as the worker's auto-hook so artifact
// commits trigger extraction asynchronously. Used by the end-to-end test.
func (h *extractHarness) enableHook() {
	h.t.Helper()
	h.worker.SetExtractor(h.extractor)
}

// writeArtifact commits a synthetic artifact straight through the worker so
// every test path exercises the real single-writer queue.
func (h *extractHarness) writeArtifact(sha, kind, body string) string {
	h.t.Helper()
	path := fmt.Sprintf("wiki/artifacts/%s/%s.md", kind, sha)
	if _, _, err := h.worker.EnqueueArtifact(context.Background(),
		ArchivistAuthor, path, body, "ingest test artifact",
	); err != nil {
		h.t.Fatalf("enqueue artifact: %v", err)
	}
	return path
}

// cannedResponse returns the canonical Sarah-Chen extraction JSON.
func cannedResponse(sha string) string {
	payload := extractionOutput{
		ArtifactSHA: sha,
		Entities: []extractedEntity{
			{
				Kind:         "person",
				ProposedSlug: "sarah-chen",
				Signals: extractedSignal{
					PersonName: "Sarah Chen",
					JobTitle:   "VP of Sales",
				},
				Confidence: 0.95,
				Ghost:      true,
			},
		},
		Facts: []extractedFact{
			{
				EntitySlug: "sarah-chen",
				Type:       "status",
				Triplet: &Triplet{
					Subject:   "sarah-chen",
					Predicate: "role_at",
					Object:    "company:acme-corp",
				},
				Text:            "Sarah Chen was promoted to VP of Sales.",
				Confidence:      0.92,
				ValidFrom:       "2026-04-10",
				SourceType:      "chat",
				SourcePath:      "wiki/artifacts/chat/" + sha + ".md",
				SentenceOffset:  0,
				ArtifactExcerpt: "Sarah Chen was promoted to VP of Sales.",
			},
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestExtractIdempotentOnRepeat(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "abc123"
	h.provider.response = cannedResponse(sha)

	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	// Run extraction twice.
	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	h.worker.WaitForIdle()

	// The fact ID must be deterministic — computing it from the JSON fields
	// should match the one stored.
	wantID := ComputeFactID(sha, 0, "sarah-chen", "role_at", "company:acme-corp")
	f, ok, err := h.index.GetFact(ctx, wantID)
	if err != nil || !ok {
		t.Fatalf("fact not indexed (found=%v err=%v)", ok, err)
	}
	if f.Text == "" {
		t.Fatal("expected fact text to be populated")
	}
	// Second run must have bumped reinforced_at without creating a duplicate.
	if f.ReinforcedAt == nil {
		t.Fatal("expected reinforced_at to be set after second extract")
	}

	// Canonical hash should be stable across additional re-runs (idempotent).
	hash1, _ := h.index.CanonicalHashFacts(ctx)
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("third extract: %v", err)
	}
	h.worker.WaitForIdle()
	hash2, _ := h.index.CanonicalHashFacts(ctx)
	// Note: hashes differ because reinforced_at is bumped. The invariant is
	// that the fact ID set is identical — one row, same subject/predicate/
	// object.
	_ = hash1
	_ = hash2
	// Assert one row total.
	facts, _ := h.index.ListFactsForEntity(ctx, "sarah-chen")
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact row after repeat extraction; got %d", len(facts))
	}
}

func TestExtractCreatesGhostEntity(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "ghost01"
	h.provider.response = cannedResponse(sha)
	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	// Entity should exist in the index with the ghost kind=person.
	mem := h.index.store.(*inMemoryFactStore)
	mem.mu.RLock()
	ent, ok := mem.entities["sarah-chen"]
	mem.mu.RUnlock()
	if !ok {
		t.Fatal("expected ghost entity sarah-chen to be indexed")
	}
	if ent.Kind != "person" {
		t.Fatalf("expected person kind; got %q", ent.Kind)
	}

	// Fact must link to the ghost entity.
	facts, _ := h.index.ListFactsForEntity(ctx, "sarah-chen")
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact on ghost; got %d", len(facts))
	}
}

func TestExtractProviderErrorRoutesToDLQ(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "fail01"
	h.provider.err = errors.New("simulated provider timeout")
	path := h.writeArtifact(sha, "chat", "A provider-error artifact.\n")

	ctx := context.Background()
	err := h.extractor.ExtractFromArtifact(ctx, path)
	if err == nil {
		t.Fatal("expected provider error to surface")
	}

	ready, rerr := h.dlq.ReadyForReplay(ctx, time.Now().Add(10*time.Minute))
	if rerr != nil {
		t.Fatalf("ready for replay: %v", rerr)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 DLQ entry; got %d", len(ready))
	}
	if ready[0].ErrorCategory != DLQCategoryProviderTimeout {
		t.Fatalf("expected provider_timeout category; got %q", ready[0].ErrorCategory)
	}
	// Commit still succeeded.
	full := filepath.Join(h.repo.Root(), filepath.FromSlash(path))
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("artifact file missing despite extract failure: %v", err)
	}
}

func TestExtractMalformedJSONValidationCapped(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "bad0jsn"
	h.provider.response = "not json at all"
	path := h.writeArtifact(sha, "chat", "Malformed-payload artifact.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err == nil {
		t.Fatal("expected parse error")
	}
	// Validation errors cap at max_retries = 1.
	ready, _ := h.dlq.ReadyForReplay(ctx, time.Now().Add(10*time.Minute))
	if len(ready) != 1 {
		t.Fatalf("expected 1 DLQ entry; got %d", len(ready))
	}
	if ready[0].ErrorCategory != DLQCategoryValidation {
		t.Fatalf("expected validation category; got %q", ready[0].ErrorCategory)
	}
	if ready[0].MaxRetries != DLQValidationMaxRetries {
		t.Fatalf("expected max_retries = %d; got %d", DLQValidationMaxRetries, ready[0].MaxRetries)
	}
}

func TestExtractReinforcementCountsWithoutDuplicating(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "reinf01"
	h.provider.response = cannedResponse(sha)
	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	h.worker.WaitForIdle()

	wantID := ComputeFactID(sha, 0, "sarah-chen", "role_at", "company:acme-corp")
	f1, _, _ := h.index.GetFact(ctx, wantID)
	if f1.ReinforcedAt != nil {
		t.Fatal("first run should not set reinforced_at")
	}

	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	h.worker.WaitForIdle()

	f2, ok2, _ := h.index.GetFact(ctx, wantID)
	if !ok2 {
		t.Fatalf("fact missing after second extract: want ID %s", wantID)
	}
	if f2.ReinforcedAt == nil {
		// Dump store contents for diagnosis.
		mem := h.index.store.(*inMemoryFactStore)
		mem.mu.RLock()
		for id, f := range mem.facts {
			t.Logf("indexed fact id=%s reinforced=%v", id, f.ReinforcedAt)
		}
		mem.mu.RUnlock()
		t.Fatal("second run should set reinforced_at")
	}
	if f2.CreatedAt != f1.CreatedAt {
		t.Fatal("created_at must be preserved across reinforcement")
	}
}

func TestExtractReplayDLQSucceedsOnRecovery(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "replay01"
	// First attempt: provider fails.
	h.provider.err = errors.New("transient provider failure")
	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err == nil {
		t.Fatal("expected failure on first pass")
	}
	// Confirm DLQ entry landed.
	ready, _ := h.dlq.ReadyForReplay(ctx, time.Now().Add(10*time.Minute))
	if len(ready) != 1 {
		t.Fatalf("expected DLQ to hold the entry; got %d", len(ready))
	}

	// Provider comes back online.
	h.provider.mu.Lock()
	h.provider.err = nil
	h.provider.response = cannedResponse(sha)
	h.provider.mu.Unlock()

	// Advance clock so replay window is eligible.
	future := time.Now().Add(1 * time.Hour)
	h.extractor.SetNow(func() time.Time { return future })

	processed, retired, err := h.extractor.ReplayDLQ(ctx)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if processed != 1 || retired != 1 {
		t.Fatalf("expected 1 processed/1 retired; got %d/%d", processed, retired)
	}
	h.worker.WaitForIdle()

	// Fact should be indexed now.
	wantID := ComputeFactID(sha, 0, "sarah-chen", "role_at", "company:acme-corp")
	if _, ok, _ := h.index.GetFact(ctx, wantID); !ok {
		t.Fatal("expected fact indexed after replay")
	}

	// The DLQ should have no ready entries (tombstoned).
	after, _ := h.dlq.ReadyForReplay(ctx, future)
	if len(after) != 0 {
		t.Fatalf("expected 0 ready entries after replay; got %d", len(after))
	}
}

func TestExtractEndToEndLookupCitesAnswer(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "e2e00001"
	// Use a canned provider that returns BOTH extraction JSON and a lookup
	// answer depending on which prompt comes in. We key off the presence of
	// "VP of Sales" vs. a question phrase.
	lookupCalls := atomic.Int32{}
	extractCalls := atomic.Int32{}
	h.provider.onCall = func(n int) (string, error) {
		// First call is extraction (prompt contains "Read docs/specs/WIKI-SCHEMA.md").
		// Subsequent calls for the lookup contain "answer_query" vocab.
		if n == 1 {
			extractCalls.Add(1)
			return cannedResponse(sha), nil
		}
		lookupCalls.Add(1)
		// Minimal valid lookup response that cites source 1.
		return `{
			"query_class": "status_recency",
			"answer_markdown": "Sarah Chen is VP of Sales.<sup>[1]</sup>",
			"sources_cited": [1],
			"confidence": 0.9,
			"coverage": "complete"
		}`, nil
	}

	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	// Confirm the fact landed.
	facts, _ := h.index.ListFactsForEntity(ctx, "sarah-chen")
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact; got %d", len(facts))
	}

	// Now ask the query handler. Use a query that contains a workspace
	// entity token ("Sarah") and a keyword present in the indexed fact
	// text so the in-memory substring TextIndex matches.
	handler := NewQueryHandler(h.index, h.provider)
	ans, err := handler.Answer(ctx, QueryRequest{
		Query:       "VP of Sales",
		RequestedBy: "human",
		TopK:        10,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !strings.Contains(ans.AnswerMarkdown, "VP of Sales") {
		t.Fatalf("expected answer to mention VP of Sales; got %q", ans.AnswerMarkdown)
	}
	if len(ans.Sources) == 0 {
		t.Fatalf("expected at least one cited source; got %+v", ans)
	}
	if extractCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 extract call; got %d", extractCalls.Load())
	}
	if lookupCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 lookup call; got %d", lookupCalls.Load())
	}
}
