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
)

// newSourcesTestServer wires an httptest server with the three /sources/*
// routes plus a running wiki worker attached to the broker.
func newSourcesTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
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
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/sources/list", b.requireAuth(b.handleSourcesList))
	mux.HandleFunc("/sources/read", b.requireAuth(b.handleSourcesRead))
	mux.HandleFunc("/sources/ingest", b.requireAuth(b.handleSourcesIngest))
	srv := httptest.NewServer(mux)
	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
		<-worker.Done()
	}
}

// ingestSource POSTs to /sources/ingest and returns the decoded response.
func ingestSource(t *testing.T, srv *httptest.Server, token string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	buf, _ := json.Marshal(payload)
	req, err := authReq(http.MethodPost, srv.URL+"/sources/ingest", bytes.NewReader(buf), token)
	if err != nil {
		t.Fatalf("authReq: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	var out map[string]any
	body, _ := io.ReadAll(res.Body)
	if len(bytes.TrimSpace(body)) > 0 {
		_ = json.Unmarshal(body, &out)
	}
	return res.StatusCode, out
}

func TestSourcesIngest_HappyPath(t *testing.T) {
	srv, b, teardown := newSourcesTestServer(t)
	defer teardown()

	status, resp := ingestSource(t, srv, b.Token(), map[string]any{
		"kind":    "note",
		"title":   "Founder insight",
		"origin":  "",
		"content": "Sources are the substrate.",
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, resp = %v", status, resp)
	}
	id, _ := resp["id"].(string)
	path, _ := resp["path"].(string)
	sha, _ := resp["sha"].(string)
	if id == "" || path == "" || sha == "" {
		t.Fatalf("expected id/path/sha, got %v", resp)
	}
	if path != SourceRelPath(SourceKindNote, id) {
		t.Fatalf("path = %q, want %q", path, SourceRelPath(SourceKindNote, id))
	}

	// Read it back through the read endpoint.
	readURL := srv.URL + "/sources/read?kind=note&id=" + id
	req, _ := authReq(http.MethodGet, readURL, nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("read status = %d body=%s", res.StatusCode, body)
	}
	var rec map[string]any
	_ = json.NewDecoder(res.Body).Decode(&rec)
	if rec["content"] != "Sources are the substrate." {
		t.Fatalf("read content = %v", rec["content"])
	}
	if rec["title"] != "Founder insight" {
		t.Fatalf("read title = %v", rec["title"])
	}
}

func TestSourcesIngest_RejectsAutoCaptureKinds(t *testing.T) {
	srv, b, teardown := newSourcesTestServer(t)
	defer teardown()

	for _, kind := range []string{"task", "decision", "chat", "bogus", ""} {
		t.Run("kind="+kind, func(t *testing.T) {
			status, resp := ingestSource(t, srv, b.Token(), map[string]any{
				"kind":    kind,
				"title":   "Should fail",
				"content": "nope",
			})
			if status != http.StatusBadRequest {
				t.Fatalf("kind %q: status = %d (want 400), resp = %v", kind, status, resp)
			}
		})
	}
}

func TestSourcesIngest_RejectsMissingFields(t *testing.T) {
	srv, b, teardown := newSourcesTestServer(t)
	defer teardown()

	// Valid kind but empty content -> NewSourceRecord rejects -> 400.
	status, _ := ingestSource(t, srv, b.Token(), map[string]any{
		"kind":    "doc",
		"title":   "Has title",
		"content": "",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("empty content: status = %d, want 400", status)
	}
}

func TestSourcesIngest_IdempotentByOrigin(t *testing.T) {
	srv, b, teardown := newSourcesTestServer(t)
	defer teardown()

	payload := map[string]any{
		"kind":    "url",
		"title":   "Nex launch",
		"origin":  "https://nex.ai/launch",
		"content": "Launch post body.",
	}
	status1, resp1 := ingestSource(t, srv, b.Token(), payload)
	if status1 != http.StatusOK {
		t.Fatalf("first ingest status = %d", status1)
	}
	// Same origin -> same id -> write-once no-op (still 200, same id).
	status2, resp2 := ingestSource(t, srv, b.Token(), map[string]any{
		"kind":    "url",
		"title":   "Nex launch (edited title)",
		"origin":  "https://nex.ai/launch",
		"content": "Different body that must NOT overwrite.",
	})
	if status2 != http.StatusOK {
		t.Fatalf("second ingest status = %d", status2)
	}
	if resp1["id"] != resp2["id"] {
		t.Fatalf("origin-keyed ingest should be idempotent: id1=%v id2=%v", resp1["id"], resp2["id"])
	}

	// The list shows exactly one record, and the original content survived.
	req, _ := authReq(http.MethodGet, srv.URL+"/sources/list", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer res.Body.Close()
	var listResp struct {
		Sources []map[string]any `json:"sources"`
	}
	_ = json.NewDecoder(res.Body).Decode(&listResp)
	if len(listResp.Sources) != 1 {
		t.Fatalf("expected 1 source after idempotent re-ingest, got %d", len(listResp.Sources))
	}
	// List payload omits content for size.
	if _, hasContent := listResp.Sources[0]["content"]; hasContent {
		t.Fatalf("list payload should omit content, got %v", listResp.Sources[0])
	}

	id, _ := resp1["id"].(string)
	readURL := srv.URL + "/sources/read?kind=url&id=" + id
	rreq, _ := authReq(http.MethodGet, readURL, nil, b.Token())
	rres, err := http.DefaultClient.Do(rreq)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer rres.Body.Close()
	var rec map[string]any
	_ = json.NewDecoder(rres.Body).Decode(&rec)
	if rec["content"] != "Launch post body." {
		t.Fatalf("write-once violated via HTTP: content = %v", rec["content"])
	}
}

func TestSourcesRead_NotFound(t *testing.T) {
	srv, b, teardown := newSourcesTestServer(t)
	defer teardown()

	req, _ := authReq(http.MethodGet, srv.URL+"/sources/read?kind=note&id=note-missing", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
}
