package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newCompressFixture wires repo + worker + compressor with an injected LLM
// stub. The teardown closure stops the compressor and worker so test
// goroutines don't outlive the TempDir.
func newCompressFixture(t *testing.T, llmStub func(ctx context.Context, sys, user string) (string, error)) (
	*WikiCompressor, *WikiWorker, func(),
) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	cmp := NewWikiCompressor(worker, CompressorConfig{
		Timeout: 5 * time.Second,
		LLMCall: llmStub,
	})
	cmp.Start(context.Background())
	return cmp, worker, func() {
		cmp.Stop()
		cancel()
		<-worker.Done()
	}
}

// commitArticle seeds an initial article on the repo so the compressor has
// something to read.
func commitArticle(t *testing.T, worker *WikiWorker, relPath, body string) {
	t.Helper()
	if _, _, err := worker.Enqueue(context.Background(), "test-author", relPath, body, "replace", "test: seed "+relPath); err != nil {
		t.Fatalf("seed %s: %v", relPath, err)
	}
}

// waitForBody polls readArticle until the body satisfies pred or the timeout
// fires. Useful for the compress async commit.
func waitForBody(t *testing.T, worker *WikiWorker, relPath string, pred func(string) bool, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		bytes, err := readArticle(worker.Repo(), relPath)
		if err == nil {
			body := string(bytes)
			if pred(body) {
				return body
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	bytes, _ := readArticle(worker.Repo(), relPath)
	t.Fatalf("waitForBody timed out for %s; last body:\n%s", relPath, string(bytes))
	return ""
}

// TestWikiCompressor_ICP1_Compress is Alex's flow: long brief, click compress,
// brief gets shorter under archivist identity.
func TestWikiCompressor_ICP1_Compress(t *testing.T) {
	const original = "# Contoso\n\n" +
		"Long verbose paragraph about the renewal contact Jordan Ramirez, " +
		"with redundant filler words and repeated facts and lots of prose " +
		"that exists only because successive synthesis passes accumulated " +
		"slightly different phrasings of the same underlying truth. The " +
		"renewal date is 2026-09-01. Jordan Ramirez is the renewal contact. " +
		"There are duplicated paragraphs about the same renewal contact.\n"
	stub := func(ctx context.Context, sys, user string) (string, error) {
		// Emit roughly half-length output preserving the named facts.
		return "# Contoso\n\nRenewal contact: Jordan Ramirez. Renewal date 2026-09-01.\n", nil
	}
	cmp, worker, teardown := newCompressFixture(t, stub)
	defer teardown()

	relPath := "team/customers/contoso.md"
	commitArticle(t, worker, relPath, original)

	queued, err := cmp.EnqueueCompress(relPath, "alex")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if !queued {
		t.Fatalf("expected queued=true on first call")
	}

	body := waitForBody(t, worker, relPath, func(b string) bool {
		return strings.Contains(b, "Renewal contact: Jordan Ramirez")
	}, 3*time.Second)
	if len(body) >= len(original) {
		t.Fatalf("compressed body not shorter:\noriginal=%d\nresult=%d\nbody:\n%s", len(original), len(body), body)
	}
	if !strings.Contains(body, "Jordan Ramirez") {
		t.Errorf("compressed body lost named fact: %s", body)
	}

	// Verify archivist authorship via git log.
	commits, err := worker.Repo().Log(context.Background(), relPath)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(commits) == 0 {
		t.Fatalf("no commits for %s", relPath)
	}
	top := commits[0]
	if !strings.Contains(top.Message, "archivist: compress "+relPath) {
		t.Errorf("expected archivist compress commit; got %q", top.Message)
	}
}

// TestWikiCompressor_ICP2_Debounce is Jordan's flow: second click while a job
// is in-flight returns (false, nil) and IsInflight reports true.
func TestWikiCompressor_ICP2_Debounce(t *testing.T) {
	// Block the LLM call so the first job stays in-flight while we issue
	// the second. Release on test cleanup so the goroutine exits cleanly.
	release := make(chan struct{})
	var calls atomic.Int32
	stub := func(ctx context.Context, sys, user string) (string, error) {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "# short\n\nshort body.\n", nil
	}
	cmp, worker, teardown := newCompressFixture(t, stub)
	defer teardown()
	defer close(release)

	relPath := "team/customers/globex.md"
	commitArticle(t, worker, relPath, "# Globex\n\nlong body that needs trimming.\n")

	queued1, err := cmp.EnqueueCompress(relPath, "jordan")
	if err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	if !queued1 {
		t.Fatalf("expected queued=true on first call")
	}

	// Wait until the first job is actually in-flight (LLM stub started).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatalf("first job never reached LLM stub")
	}
	if !cmp.IsInflight(relPath) {
		t.Fatalf("first job not flagged in-flight")
	}

	queued2, err := cmp.EnqueueCompress(relPath, "jordan")
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	if queued2 {
		t.Errorf("expected queued=false on debounced second call")
	}
	if !cmp.IsInflight(relPath) {
		t.Errorf("expected IsInflight=true while job runs")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 LLM call; got %d", got)
	}
}

// TestWikiCompressor_ICP3_MCPPath is Marcus's flow: POST /wiki/compress
// returns {queued: true} JSON with 200 for a valid path.
func TestWikiCompressor_ICP3_MCPPath(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Old Contact\n\nshort body.\n", nil
	}

	// Build a Broker with the wiki worker + compressor wired manually so we
	// avoid needing the full Start() pipeline. The handler reaches into the
	// broker via its accessors.
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	cmp := NewWikiCompressor(worker, CompressorConfig{
		Timeout: 5 * time.Second,
		LLMCall: stub,
	})
	cmp.Start(context.Background())
	t.Cleanup(func() {
		cmp.Stop()
		cancel()
		<-worker.Done()
	})

	relPath := "team/people/old-contact.md"
	commitArticle(t, worker, relPath, "# Old contact\n\noriginal long body.\n")

	b := &Broker{}
	b.wikiWorker = worker
	b.wikiCompressor = cmp

	req := httptest.NewRequest(http.MethodPost, "/wiki/compress?path="+relPath, nil)
	rec := httptest.NewRecorder()
	b.handleWikiCompress(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Queued   bool   `json:"queued"`
		InFlight bool   `json:"in_flight"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if !resp.Queued {
		t.Errorf("expected queued=true; got %+v", resp)
	}
	if resp.Path != relPath {
		t.Errorf("path mismatch: got %q want %q", resp.Path, relPath)
	}
}

// TestWikiCompressor_HandlerRejectsNonPost guards against accidental browser
// GET on the compress endpoint.
func TestWikiCompressor_HandlerRejectsNonPost(t *testing.T) {
	b := &Broker{}
	req := httptest.NewRequest(http.MethodGet, "/wiki/compress?path=team/people/x.md", nil)
	rec := httptest.NewRecorder()
	b.handleWikiCompress(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET; got %d", rec.Code)
	}
}

// TestWikiCompressor_HandlerRejectsBadPath guards against a missing or
// escaping path parameter.
func TestWikiCompressor_HandlerRejectsBadPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	cmp := NewWikiCompressor(worker, CompressorConfig{Timeout: time.Second})
	cmp.Start(context.Background())
	t.Cleanup(func() {
		cmp.Stop()
		cancel()
		<-worker.Done()
	})

	b := &Broker{}
	b.wikiWorker = worker
	b.wikiCompressor = cmp

	req := httptest.NewRequest(http.MethodPost, "/wiki/compress", nil)
	rec := httptest.NewRecorder()
	b.handleWikiCompress(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing path; got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestWikiCompressor_PreservesFrontmatter guards the round-trip: original
// frontmatter must survive a compress. The LLM stub returns plain markdown
// with no frontmatter; the compressor re-prepends the original block.
func TestWikiCompressor_PreservesFrontmatter(t *testing.T) {
	original := "---\nghost: true\nkind: people\n---\n\n# Long\n\nverbose body.\n"
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Short\n\ntight body.\n", nil
	}
	cmp, worker, teardown := newCompressFixture(t, stub)
	defer teardown()

	relPath := "team/people/example.md"
	commitArticle(t, worker, relPath, original)

	if _, err := cmp.EnqueueCompress(relPath, "test"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	body := waitForBody(t, worker, relPath, func(b string) bool {
		return strings.Contains(b, "tight body")
	}, 3*time.Second)
	if !strings.HasPrefix(body, "---\n") {
		t.Errorf("frontmatter missing after compress: %s", body)
	}
	if !strings.Contains(body, "ghost: true") {
		t.Errorf("ghost frontmatter lost: %s", body)
	}
	if !strings.Contains(body, "kind: people") {
		t.Errorf("kind frontmatter lost: %s", body)
	}
}
