package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// newPromotionDemandTestServer wires the same notebook surface as
// newNotebookTestServer but additionally instantiates a NotebookDemandIndex
// against a temp JSONL log. The broker's wiki worker is wired so
// AutoEscalateDemandCandidates can verify entry existence.
func newPromotionDemandTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	demandPath := filepath.Join(t.TempDir(), "events.jsonl")
	idx, err := NewNotebookDemandIndex(demandPath)
	if err != nil {
		t.Fatalf("NewNotebookDemandIndex: %v", err)
	}

	b.mu.Lock()
	b.wikiWorker = worker
	b.demandIndex = idx
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", b.requireAuth(b.handleNotebookWrite))
	mux.HandleFunc("/notebook/search", b.requireAuth(b.handleNotebookSearch))
	srv := httptest.NewServer(mux)

	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
	}
}

func searchAs(t *testing.T, srv *httptest.Server, token, agent, ownerSlug, query string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/notebook/search?slug="+ownerSlug+"&q="+query, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if agent != "" {
		req.Header.Set("X-WUPHF-Agent", agent)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("search status %d: %s", res.StatusCode, string(body))
	}
}

func writeNotebookEntryHTTP(t *testing.T, srv *httptest.Server, token, slug, path, content string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"slug":           slug,
		"path":           path,
		"content":        content,
		"mode":           "create",
		"commit_message": "test entry",
	})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("write status %d: %s", res.StatusCode, string(raw))
	}
}

func waitForDemandScore(t *testing.T, idx *NotebookDemandIndex, path string, want float64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := idx.WaitForCondition(ctx, func() bool {
		return idx.Score(path) >= want
	}); err != nil {
		t.Fatalf("demand score for %q never reached %v (got %v): %v", path, want, idx.Score(path), err)
	}
}

// TestBrokerPromotionDemand_CrossAgentSearchAccrues exercises the end-to-end
// hook: agent A and agent C searching agent B's notebook each accrue a demand
// event, score reaches 2.0 (cross-agent search weight 1.0 each).
func TestBrokerPromotionDemand_CrossAgentSearchAccrues(t *testing.T) {
	srv, b, teardown := newPromotionDemandTestServer(t)
	defer teardown()
	token := b.Token()

	notebookPath := "agents/pm/notebook/2026-05-06-onboarding.md"
	writeNotebookEntryHTTP(t, srv, token, "pm", notebookPath,
		"# onboarding gotchas\n\nremember to set the bun version on first clone.\n")

	// Agent A (eng) searches PM's shelf.
	searchAs(t, srv, token, "eng", "pm", "onboarding")
	// Agent C (design) searches PM's shelf for the same content.
	searchAs(t, srv, token, "design", "pm", "onboarding")

	idx := b.demandIndex
	if idx == nil {
		t.Fatalf("demand index not wired")
	}
	waitForDemandScore(t, idx, notebookPath, 2.0)

	// Self-search by PM should NOT accrue.
	searchAs(t, srv, token, "pm", "pm", "onboarding")
	if got := idx.Score(notebookPath); got != 2.0 {
		t.Fatalf("after self-search score = %v, want 2.0", got)
	}

	// Same searcher, same day → still 2.0 (deduped).
	searchAs(t, srv, token, "eng", "pm", "onboarding")
	if got := idx.Score(notebookPath); got != 2.0 {
		t.Fatalf("after dup-day search score = %v, want 2.0", got)
	}
}

// TestBrokerPromotionDemand_AutoEscalation drives the full pipeline to
// threshold and asserts a promotion lands in ReviewLog.
func TestBrokerPromotionDemand_AutoEscalation(t *testing.T) {
	srv, b, teardown := newPromotionDemandTestServer(t)
	defer teardown()
	token := b.Token()

	notebookPath := "agents/pm/notebook/2026-05-06-icp.md"
	writeNotebookEntryHTTP(t, srv, token, "pm", notebookPath,
		"# our ICP\n\nfounders running 3+ AI agents.\n")

	// Two distinct agents searching → 2.0 (below 3.0 threshold).
	searchAs(t, srv, token, "eng", "pm", "ICP")
	searchAs(t, srv, token, "design", "pm", "ICP")

	idx := b.demandIndex
	waitForDemandScore(t, idx, notebookPath, 2.0)

	// Add a CEO review flag (PR 4 will be doing this via an MCP tool; here
	// we exercise the same Record API).
	now := time.Now().UTC()
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    notebookPath,
		OwnerSlug:    "pm",
		SearcherSlug: "ceo",
		Signal:       DemandSignalCEOReviewFlag,
		RecordedAt:   now,
	}); err != nil {
		t.Fatalf("record CEO flag: %v", err)
	}
	// 2 cross-agent (1.0 each) + 1 CEO flag (1.5) = 3.5 → above default 3.0.
	if got := idx.Score(notebookPath); got < 3.0 {
		t.Fatalf("post-CEO score = %v, want ≥ 3.0", got)
	}

	// Wire a ReviewLog backed by the wiki repo so SubmitPromotion's path
	// validators work the same way they would in production.
	worker := b.WikiWorker()
	if worker == nil {
		t.Fatalf("wiki worker missing")
	}
	rl, err := NewReviewLog(ReviewLogPath(worker.Repo().Root()), nil, nil)
	if err != nil {
		t.Fatalf("review log: %v", err)
	}
	if err := idx.AutoEscalateDemandCandidates(context.Background(), rl, worker); err != nil {
		t.Fatalf("escalate: %v", err)
	}

	reviews := rl.List("all")
	if len(reviews) != 1 {
		t.Fatalf("expected 1 escalated review, got %d", len(reviews))
	}
	if reviews[0].SourcePath != notebookPath {
		t.Fatalf("review source_path = %q, want %q", reviews[0].SourcePath, notebookPath)
	}

	// Idempotency: re-running escalation does NOT duplicate.
	if err := idx.AutoEscalateDemandCandidates(context.Background(), rl, worker); err != nil {
		t.Fatalf("re-escalate: %v", err)
	}
	if got := len(rl.List("all")); got != 1 {
		t.Fatalf("idempotent escalation produced %d reviews, want 1", got)
	}
}

// TestBrokerPromotionDemand_NoIndexNoOp asserts that handleNotebookSearch
// works (and returns 200) when the demand index is not wired. The PR must be
// safe to revert by clearing b.demandIndex without breaking search.
func TestBrokerPromotionDemand_NoIndexNoOp(t *testing.T) {
	srv, b, teardown := newPromotionDemandTestServer(t)
	defer teardown()
	token := b.Token()

	notebookPath := "agents/pm/notebook/entry.md"
	writeNotebookEntryHTTP(t, srv, token, "pm", notebookPath, "# entry\n\nbody.\n")

	// Clear the index — search must still succeed.
	b.mu.Lock()
	b.demandIndex = nil
	b.mu.Unlock()

	searchAs(t, srv, token, "eng", "pm", "entry")
	// No assertion on score; we're testing absence of panic / 500.
}
