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
)

// newNotebookTestServer wires a full httptest server with the auth-gated
// notebook routes + a running worker against a temp repo.
func newNotebookTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := NewBroker()
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", b.requireAuth(b.handleNotebookWrite))
	mux.HandleFunc("/notebook/read", b.requireAuth(b.handleNotebookRead))
	mux.HandleFunc("/notebook/list", b.requireAuth(b.handleNotebookList))
	mux.HandleFunc("/notebook/search", b.requireAuth(b.handleNotebookSearch))
	srv := httptest.NewServer(mux)

	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
	}
}

func authReq(method, url string, body io.Reader, token string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func TestBrokerNotebookHandlersEndToEnd(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()

	// Write
	writeBody, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/2026-04-20-retro.md",
		"content":        "# Retro\n\nDraft.\n",
		"mode":           "create",
		"commit_message": "draft retro",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(writeBody), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(body))
	}
	var writeRes map[string]any
	_ = json.NewDecoder(res.Body).Decode(&writeRes)
	res.Body.Close()
	if writeRes["commit_sha"] == "" {
		t.Fatalf("expected commit_sha in response: %+v", writeRes)
	}

	// Read
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/read?slug=pm&path=agents/pm/notebook/2026-04-20-retro.md", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read status %d: %s", res.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "Draft") {
		t.Fatalf("unexpected body: %q", string(body))
	}

	// List
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/list?slug=pm", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listRes struct {
		Entries []map[string]any `json:"entries"`
	}
	_ = json.NewDecoder(res.Body).Decode(&listRes)
	res.Body.Close()
	if len(listRes.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(listRes.Entries))
	}

	// Search
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/search?slug=pm&q=Draft", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searchRes struct {
		Hits []map[string]any `json:"hits"`
	}
	_ = json.NewDecoder(res.Body).Decode(&searchRes)
	res.Body.Close()
	if len(searchRes.Hits) == 0 {
		t.Fatal("expected at least one search hit")
	}
}

func TestBrokerNotebookWriteAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	body, _ := json.Marshal(map[string]any{"slug": "pm", "path": "agents/pm/notebook/x.md"})
	res, err := http.Post(srv.URL+"/notebook/write", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/read?path=agents/pm/notebook/x.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookListAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/list?slug=pm")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookSearchAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/search?slug=pm&q=x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookWriteRejectsBadJSON(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader([]byte("{not-json")), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookWriteMethodNotAllowed(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/write", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadMethodNotAllowed(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/read", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookListRequiresSlug(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/list", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookListEmptyReturnsEmptyArray(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/list?slug=nobody", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var parsed struct {
		Entries []map[string]any `json:"entries"`
	}
	_ = json.NewDecoder(res.Body).Decode(&parsed)
	if parsed.Entries == nil {
		t.Fatal("expected [] not null")
	}
	if len(parsed.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(parsed.Entries))
	}
}

func TestBrokerNotebookWriteSlugMismatchReturns403(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	body, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/ceo/notebook/x.md",
		"content":        "# x\n",
		"mode":           "create",
		"commit_message": "m",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 403, got %d: %s", res.StatusCode, string(b))
	}
}

func TestBrokerNotebookWriteValidationReturns400(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	body, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/x.txt", // not markdown
		"content":        "x",
		"mode":           "create",
		"commit_message": "m",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadBadSlugHint(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	// slug hint != path owner — should 400
	req, _ := authReq(http.MethodGet,
		srv.URL+"/notebook/read?slug=pm&path=agents/ceo/notebook/x.md", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadMissingPath(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/read", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookSearchRequiresSlugAndPattern(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	// missing slug
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/search?q=x", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing slug, got %d", res.StatusCode)
	}
	// missing q
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/search?slug=pm", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing q, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookServiceUnavailable(t *testing.T) {
	// No worker attached — every handler should 503.
	b := NewBroker()
	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", b.handleNotebookWrite)
	mux.HandleFunc("/notebook/read", b.handleNotebookRead)
	mux.HandleFunc("/notebook/list", b.handleNotebookList)
	mux.HandleFunc("/notebook/search", b.handleNotebookSearch)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		method string
		url    string
	}{
		{http.MethodPost, "/notebook/write"},
		{http.MethodGet, "/notebook/read?path=agents/pm/notebook/x.md"},
		{http.MethodGet, "/notebook/list?slug=pm"},
		{http.MethodGet, "/notebook/search?slug=pm&q=x"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.url, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.url, err)
		}
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s %s expected 503, got %d", tc.method, tc.url, res.StatusCode)
		}
		res.Body.Close()
	}
}

// TestBrokerNotebookSSEEvent subscribes via SubscribeNotebookEvents and
// confirms a write publishes on the right channel (not wiki).
func TestBrokerNotebookSSEEvent(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	nbCh, unsub := b.SubscribeNotebookEvents(16)
	defer unsub()
	wikiCh, unsubWiki := b.SubscribeWikiEvents(16)
	defer unsubWiki()

	body, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/x.md",
		"content":        "# x\n",
		"mode":           "create",
		"commit_message": "m",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()

	select {
	case evt := <-nbCh:
		if evt.Slug != "pm" || evt.Path != "agents/pm/notebook/x.md" || evt.CommitSHA == "" {
			t.Fatalf("unexpected event: %+v", evt)
		}
	case <-context.Background().Done():
	}
	// Drain the wiki channel; it should NOT have fired.
	select {
	case evt := <-wikiCh:
		t.Fatalf("wiki channel fired unexpectedly: %+v", evt)
	default:
	}
}
