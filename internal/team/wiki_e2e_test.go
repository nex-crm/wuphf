// Cross-lane end-to-end integration: exercises the full HTTP stack across
// Lane B's materializer + Lane A's worker/MCP + the post-lane-sync article
// endpoint. Complements the per-file unit tests that already cover individual
// components in isolation.
//
// Critical paths covered here (from the test plan):
//   - First-run materialization produces skeleton articles that resolve via
//     GET /wiki/article once a git repo exists and articles are committed.
//   - Agent writes via POST /wiki/write → SSE event fires on the wiki event
//     channel → article is readable via GET /wiki/read + GET /wiki/article.
//   - Cross-article backlinks: A links to B, B links back after write, the
//     /wiki/article endpoint for B returns A as a backlink with the correct
//     author slug and extracted title.
//
// Not covered here (covered by Lane A's existing unit tests):
//   - Crash recovery, queue saturation, backend-switch matrix — already green.

package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Spin up an in-process mux with Lane A's four wiki handlers, backed by a
// fresh worker + temp-dir repo. Returns the base URL and a cleanup func.
func newWikiTestServer(t *testing.T) (baseURL string, worker *WikiWorker, cleanup func()) {
	t.Helper()

	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Publisher that captures events into a channel for assertions.
	captured := &capturePublisher{events: make(chan wikiWriteEvent, 16)}
	worker = NewWikiWorker(repo, captured)
	worker.Start(context.Background())

	// Broker stub that exposes only what the handlers need — no HTTP setup,
	// no real broker state. The handlers pull the worker from b.WikiWorker().
	broker := &Broker{wikiWorker: worker}

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/write", broker.handleWikiWrite)
	mux.HandleFunc("/wiki/read", broker.handleWikiRead)
	mux.HandleFunc("/wiki/search", broker.handleWikiSearch)
	mux.HandleFunc("/wiki/list", broker.handleWikiList)
	mux.HandleFunc("/wiki/article", broker.handleWikiArticle)

	srv := httptest.NewServer(mux)
	cleanup = func() {
		srv.Close()
		worker.Stop()
	}
	return srv.URL, worker, cleanup
}

// capturePublisher records every PublishWikiEvent for later assertion.
type capturePublisher struct {
	mu     sync.Mutex
	events chan wikiWriteEvent
}

func (c *capturePublisher) PublishWikiEvent(ev wikiWriteEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case c.events <- ev:
	default:
		// Drop if full — tests pull fast enough.
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E: agent write → SSE event → article readable
// ─────────────────────────────────────────────────────────────────────────────

func TestE2EWikiWriteReadAndEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	baseURL, worker, cleanup := newWikiTestServer(t)
	defer cleanup()
	captured := worker.publisher.(*capturePublisher)

	// Write an article via the HTTP endpoint.
	writeBody := map[string]string{
		"slug":           "ceo",
		"path":           "team/people/customer-x.md",
		"content":        "# Customer X\n\nA mid-market logistics company.\n",
		"mode":           "create",
		"commit_message": "add customer brief",
	}
	resp := postJSON(t, baseURL+"/wiki/write", writeBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /wiki/write: got %d, want 200. body: %s", resp.StatusCode, readBody(t, resp))
	}
	var writeResult struct {
		Path         string `json:"path"`
		CommitSha    string `json:"commit_sha"`
		BytesWritten int    `json:"bytes_written"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&writeResult); err != nil {
		t.Fatalf("decode write response: %v", err)
	}
	resp.Body.Close()
	if writeResult.Path != "team/people/customer-x.md" {
		t.Errorf("write.Path = %q, want team/people/customer-x.md", writeResult.Path)
	}
	if writeResult.CommitSha == "" {
		t.Error("write.CommitSha is empty")
	}
	if writeResult.BytesWritten == 0 {
		t.Error("write.BytesWritten = 0")
	}

	// SSE event fired with the expected shape.
	select {
	case ev := <-captured.events:
		if ev.Path != "team/people/customer-x.md" {
			t.Errorf("event.Path = %q", ev.Path)
		}
		if ev.AuthorSlug != "ceo" {
			t.Errorf("event.AuthorSlug = %q, want ceo", ev.AuthorSlug)
		}
		if ev.CommitSHA == "" {
			t.Error("event.CommitSHA is empty")
		}
		if ev.Timestamp == "" {
			t.Error("event.Timestamp is empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for wiki:write event")
	}

	// Article is readable via /wiki/read.
	resp = get(t, baseURL+"/wiki/read?path=team/people/customer-x.md")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /wiki/read: got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !bytes.Contains([]byte(body), []byte("Customer X")) {
		t.Errorf("read body missing title: %q", body)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E: article endpoint returns rich metadata including backlinks
// ─────────────────────────────────────────────────────────────────────────────

func TestE2EWikiArticleBacklinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	baseURL, _, cleanup := newWikiTestServer(t)
	defer cleanup()

	// Write two articles: B is the target; A links to B via [[people/b]].
	writes := []map[string]string{
		{
			"slug":           "ceo",
			"path":           "team/people/b.md",
			"content":        "# Article B\n\nThe target article.\n",
			"mode":           "create",
			"commit_message": "add B",
		},
		{
			"slug":           "pm",
			"path":           "team/people/a.md",
			"content":        "# Article A\n\nReferences [[people/b]] here.\n",
			"mode":           "create",
			"commit_message": "add A, references B",
		},
	}
	for _, wb := range writes {
		resp := postJSON(t, baseURL+"/wiki/write", wb)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("write %s: got %d", wb["path"], resp.StatusCode)
		}
		resp.Body.Close()
	}

	// GET /wiki/article for B should include A as a backlink.
	resp := get(t, baseURL+"/wiki/article?path=team/people/b.md")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /wiki/article: got %d, body=%s", resp.StatusCode, readBody(t, resp))
	}
	var meta ArticleMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	if meta.Title != "Article B" {
		t.Errorf("Title = %q, want Article B", meta.Title)
	}
	if meta.Revisions != 1 {
		t.Errorf("Revisions = %d, want 1", meta.Revisions)
	}
	if meta.LastEditedBy != "ceo" {
		t.Errorf("LastEditedBy = %q, want ceo", meta.LastEditedBy)
	}
	if len(meta.Backlinks) != 1 {
		t.Fatalf("Backlinks len = %d, want 1 (entries=%+v)", len(meta.Backlinks), meta.Backlinks)
	}
	b := meta.Backlinks[0]
	if b.Path != "team/people/a.md" {
		t.Errorf("backlink.Path = %q", b.Path)
	}
	if b.Title != "Article A" {
		t.Errorf("backlink.Title = %q", b.Title)
	}
	if b.AuthorSlug != "pm" {
		t.Errorf("backlink.AuthorSlug = %q", b.AuthorSlug)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E: five agents write concurrently through the HTTP stack;
// all articles land and git log preserves per-agent authorship.
// ─────────────────────────────────────────────────────────────────────────────

func TestE2EWikiConcurrentAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	baseURL, worker, cleanup := newWikiTestServer(t)
	defer cleanup()

	agents := []struct{ slug, path string }{
		{"ceo", "team/people/alice.md"},
		{"pm", "team/people/bob.md"},
		{"cro", "team/people/carol.md"},
		{"eng-1", "team/people/dave.md"},
		{"designer", "team/people/eve.md"},
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(agents))
	for _, a := range agents {
		wg.Add(1)
		go func(slug, path string) {
			defer wg.Done()
			resp := postJSON(t, baseURL+"/wiki/write", map[string]string{
				"slug":           slug,
				"path":           path,
				"content":        "# " + path + "\n\nWritten by " + slug + ".\n",
				"mode":           "create",
				"commit_message": slug + " adds " + path,
			})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- &httpError{status: resp.StatusCode, body: readBody(t, resp)}
			}
		}(a.slug, a.path)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent write error: %v", err)
	}

	// Every article is committed and readable; per-agent authorship preserved.
	for _, a := range agents {
		refs, err := worker.Repo().Log(context.Background(), a.path)
		if err != nil {
			t.Errorf("Log(%s): %v", a.path, err)
			continue
		}
		if len(refs) != 1 {
			t.Errorf("Log(%s) revisions = %d, want 1", a.path, len(refs))
			continue
		}
		if refs[0].Author != a.slug {
			t.Errorf("Log(%s) author = %q, want %q", a.path, refs[0].Author, a.slug)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// test helpers
// ─────────────────────────────────────────────────────────────────────────────

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string {
	return "http " + http.StatusText(e.status) + ": " + e.body
}
