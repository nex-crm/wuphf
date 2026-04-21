package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newReviewTestServer wires a full httptest server + broker + review log.
func newReviewTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	b := NewBroker()
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	b.SetReviewerResolver(func(string) string { return "ceo" })
	b.ensureReviewLog()

	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", b.requireAuth(b.handleNotebookWrite))
	mux.HandleFunc("/notebook/promote", b.requireAuth(b.handleNotebookPromote))
	mux.HandleFunc("/review/list", b.requireAuth(b.handleReviewList))
	mux.HandleFunc("/review/", b.requireAuth(b.handleReviewSubpath))
	srv := httptest.NewServer(mux)
	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
	}
}

func seedNotebookViaHTTP(t *testing.T, srv *httptest.Server, token, slug, path, body string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"slug": slug, "path": path, "content": body, "mode": "create", "commit_message": "seed",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(payload), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("seed write: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("seed write status=%d body=%s", res.StatusCode, string(b))
	}
}

func submitPromotion(t *testing.T, srv *httptest.Server, token string, body map[string]any) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/promote", bytes.NewReader(payload), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return res
}

func TestPromotionHandlers_EndToEnd(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	token := b.Token()
	seedNotebookViaHTTP(t, srv, token, "pm", "agents/pm/notebook/retro.md", "# Retro\n\nbody\n")

	// SSE subscriber — verifies events are fanned out.
	events, unsub := b.SubscribeReviewEvents(16)
	defer unsub()

	res := submitPromotion(t, srv, token, map[string]any{
		"my_slug":          "pm",
		"source_path":      "agents/pm/notebook/retro.md",
		"target_wiki_path": "team/playbooks/retro.md",
		"rationale":        "canonical retro",
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("submit status=%d body=%s", res.StatusCode, string(body))
	}
	var submitRes struct {
		PromotionID  string `json:"promotion_id"`
		ReviewerSlug string `json:"reviewer_slug"`
		State        string `json:"state"`
	}
	_ = json.NewDecoder(res.Body).Decode(&submitRes)
	if submitRes.PromotionID == "" {
		t.Fatalf("no promotion ID: %+v", submitRes)
	}
	if submitRes.State != "pending" {
		t.Fatalf("state=%s", submitRes.State)
	}

	// Verify an SSE event was emitted.
	select {
	case evt := <-events:
		if evt.ID != submitRes.PromotionID {
			t.Fatalf("event id mismatch")
		}
	case <-time.After(time.Second):
		t.Fatal("expected SSE event within 1s")
	}

	// GET /review/{id}
	req, _ := authReq(http.MethodGet, srv.URL+"/review/"+submitRes.PromotionID, nil, token)
	getRes, _ := http.DefaultClient.Do(req)
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", getRes.StatusCode)
	}
	getRes.Body.Close()

	// GET /review/list
	req, _ = authReq(http.MethodGet, srv.URL+"/review/list?scope=all", nil, token)
	listRes, _ := http.DefaultClient.Do(req)
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", listRes.StatusCode)
	}
	var listBody struct {
		Reviews []map[string]any `json:"reviews"`
	}
	_ = json.NewDecoder(listRes.Body).Decode(&listBody)
	listRes.Body.Close()
	if len(listBody.Reviews) != 1 {
		t.Fatalf("list returned %d reviews", len(listBody.Reviews))
	}

	// Approve triggers the atomic promote commit. Drain any buffered SSE
	// events first so the approve event lands in a fresh read.
	drainEvents(events)
	approveBody, _ := json.Marshal(map[string]any{"actor_slug": "ceo", "rationale": "LGTM"})
	req, _ = authReq(http.MethodPost, srv.URL+"/review/"+submitRes.PromotionID+"/approve", bytes.NewReader(approveBody), token)
	approveRes, _ := http.DefaultClient.Do(req)
	if approveRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(approveRes.Body)
		t.Fatalf("approve status=%d body=%s", approveRes.StatusCode, string(body))
	}
	approveRes.Body.Close()

	// Drain events until we see the approved transition (auto-pickup may
	// arrive first).
	var sawApproved bool
	for i := 0; i < 4 && !sawApproved; i++ {
		select {
		case evt := <-events:
			if evt.NewState == PromotionApproved {
				sawApproved = true
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for approved event")
		}
	}
	if !sawApproved {
		t.Fatal("never saw approved event")
	}

	// Target wiki article should exist on disk.
	target := filepath.Join(b.wikiWorker.Repo().Root(), "team/playbooks/retro.md")
	if _, err := readArticle(b.wikiWorker.Repo(), "team/playbooks/retro.md"); err != nil {
		t.Fatalf("target missing: %v (path=%s)", err, target)
	}
}

func TestPromotionHandlers_AuthRequired(t *testing.T) {
	srv, _, teardown := newReviewTestServer(t)
	defer teardown()
	res, err := http.Post(srv.URL+"/notebook/promote", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", res.StatusCode)
	}
}

func TestPromotionHandlers_MethodNotAllowed(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	token := b.Token()
	// GET /notebook/promote is not allowed.
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/promote", nil, token)
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestReviewSubpath_UnknownIDReturns404(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	token := b.Token()
	req, _ := authReq(http.MethodGet, srv.URL+"/review/rvw-nope", nil, token)
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestReviewIllegalTransition_Returns409(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	token := b.Token()
	seedNotebookViaHTTP(t, srv, token, "pm", "agents/pm/notebook/retro.md", "# Retro\n\nbody\n")
	res := submitPromotion(t, srv, token, map[string]any{
		"my_slug":          "pm",
		"source_path":      "agents/pm/notebook/retro.md",
		"target_wiki_path": "team/playbooks/retro.md",
		"rationale":        "r",
	})
	var submitRes struct {
		PromotionID string `json:"promotion_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&submitRes)
	res.Body.Close()

	// Approve once — succeeds.
	approveBody, _ := json.Marshal(map[string]any{"actor_slug": "ceo"})
	req, _ := authReq(http.MethodPost, srv.URL+"/review/"+submitRes.PromotionID+"/approve", bytes.NewReader(approveBody), token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve 1: %v", err)
	}
	resp.Body.Close()

	// Approve again — 409 conflict (already approved).
	req, _ = authReq(http.MethodPost, srv.URL+"/review/"+submitRes.PromotionID+"/approve", bytes.NewReader(approveBody), token)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve 2: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 on already-approved, got %d", resp2.StatusCode)
	}
}

func TestReviewTargetExists_BouncesToChangesRequested(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	token := b.Token()
	seedNotebookViaHTTP(t, srv, token, "pm", "agents/pm/notebook/retro.md", "# Retro\n\nbody\n")

	// Seed the target wiki path before submitting.
	if _, _, err := b.wikiWorker.Repo().Commit(context.Background(), "ceo", "team/playbooks/retro.md",
		"# Existing\n\nbody\n", "create", "seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := submitPromotion(t, srv, token, map[string]any{
		"my_slug":          "pm",
		"source_path":      "agents/pm/notebook/retro.md",
		"target_wiki_path": "team/playbooks/retro.md",
		"rationale":        "r",
	})
	defer res.Body.Close()
	// Submit handler rejects early with 409 because target already exists.
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 at submit, got %d", res.StatusCode)
	}
}

func drainEvents(ch <-chan ReviewStateChangeEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
