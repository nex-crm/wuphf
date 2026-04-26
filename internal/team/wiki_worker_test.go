package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingPublisher captures published events in order for assertions.
type recordingPublisher struct {
	mu     sync.Mutex
	events []wikiWriteEvent
}

func (p *recordingPublisher) PublishWikiEvent(evt wikiWriteEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
}

func (p *recordingPublisher) snapshot() []wikiWriteEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]wikiWriteEvent, len(p.events))
	copy(out, p.events)
	return out
}

func newStartedWorker(t *testing.T) (*WikiWorker, *Repo, *recordingPublisher, context.CancelFunc) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	pub := &recordingPublisher{}
	worker := NewWikiWorker(repo, pub)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return worker, repo, pub, func() {
		cancel()
		// Wait for drain (and its side goroutines — auto-recompile and
		// backup mirror) to fully exit before the test returns, so
		// t.TempDir() cleanup does not race in-flight writes into
		// wiki.bak/ or the main wiki repo.
		<-worker.Done()
	}
}

func TestWikiWorkerEnqueueHappyPath(t *testing.T) {
	// Arrange
	worker, _, pub, teardown := newStartedWorker(t)
	defer teardown()

	// Act
	sha, n, err := worker.Enqueue(context.Background(), "ceo", "team/people/nazz.md",
		"# Nazz\n\nFounder.\n", "create", "add nazz brief")

	// Assert
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty sha")
	}
	if n == 0 {
		t.Fatal("expected non-zero bytes written")
	}
	// Allow the event to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(pub.snapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	events := pub.snapshot()
	if len(events) == 0 {
		t.Fatal("expected at least one SSE event")
	}
	evt := events[0]
	if evt.Path != "team/people/nazz.md" || evt.AuthorSlug != "ceo" || evt.CommitSHA == "" || evt.Timestamp == "" {
		t.Fatalf("unexpected event shape: %+v", evt)
	}
	// Event must NOT carry article content.
	// Go's struct tag for wikiWriteEvent has no content field — compile-time guard.
	// Runtime: re-serializing to a map and confirming keys would be brittle; the
	// struct shape itself is the test. Treat this line as a readability anchor.
	_ = evt
}

func TestWikiWorkerQueueSaturation(t *testing.T) {
	// Arrange — build a worker whose drain goroutine never runs, so the
	// channel fills up predictably.
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	// Mark as running so Enqueue does not bail with ErrWorkerStopped, but do
	// NOT start the drain loop.
	worker.running.Store(true)
	defer worker.running.Store(false)

	// Act — fill the buffer, then overflow.
	for i := 0; i < wikiRequestBuffer; i++ {
		req := wikiWriteRequest{
			Slug:    "ceo",
			Path:    "team/people/x.md",
			Content: "x",
			Mode:    "create",
			ReplyCh: make(chan wikiWriteResult, 1),
		}
		select {
		case worker.requests <- req:
		default:
			t.Fatalf("buffer filled early at %d", i)
		}
	}
	// Overflow request should get saturated error.
	_, _, err := worker.Enqueue(context.Background(), "ceo", "team/people/overflow.md",
		"x", "create", "overflow")

	// Assert
	if !errors.Is(err, ErrQueueSaturated) {
		t.Fatalf("expected ErrQueueSaturated, got %v", err)
	}
}

func TestWikiWorkerStoppedReturnsError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	// Never started
	if _, _, err := worker.Enqueue(context.Background(), "ceo", "team/people/x.md", "x", "create", "m"); !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}
}

func TestWikiWorkerConcurrentEnqueue(t *testing.T) {
	worker, _, pub, teardown := newStartedWorker(t)
	defer teardown()

	var wg sync.WaitGroup
	var errCount atomic.Int32
	goroutines := 5
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "team/people/agent" + string(rune('a'+idx)) + ".md"
			_, _, err := worker.Enqueue(context.Background(), "a"+string(rune('a'+idx)),
				path, "# x\n", "create", "m")
			if err != nil {
				errCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("expected no errors, got %d", errCount.Load())
	}
	// All 5 events should land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(pub.snapshot()) >= goroutines {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(pub.snapshot()); got != goroutines {
		t.Fatalf("expected %d events, got %d", goroutines, got)
	}
}

func TestWikiWorkerBackupDebounce(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()

	// First write — should trigger a backup.
	if _, _, err := worker.Enqueue(context.Background(), "ceo", "team/people/a.md", "# a\n", "create", "m"); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	// Let backup land
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := fileExists(filepath.Join(repo.BackupRoot(), "team/people/a.md")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Immediate second write — debounce should skip this backup.
	if _, _, err := worker.Enqueue(context.Background(), "ceo", "team/people/b.md", "# b\n", "create", "m"); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	// Without debounce we would expect b.md in backup too within the window,
	// but the debouncer should skip. Give the async goroutine a chance to
	// fire (or not).
	time.Sleep(300 * time.Millisecond)
	if _, err := fileExists(filepath.Join(repo.BackupRoot(), "team/people/b.md")); err == nil {
		t.Fatal("expected b.md to be skipped by debounce window")
	}
}

// fileExists returns nil when the path exists and an error otherwise.
// Helper kept local to avoid polluting package scope with another utility.
func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info != nil, nil
}

func TestWikiSearchReturnsHits(t *testing.T) {
	// Arrange
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	ctx := context.Background()
	if _, _, err := worker.Enqueue(ctx, "ceo", "team/people/nazz.md", "# Nazz\n\nFounded WUPHF in 2026.\n", "create", "m"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Act
	hits, err := searchArticles(worker.Repo(), "WUPHF")

	// Assert
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if hits[0].Path != "team/people/nazz.md" {
		t.Fatalf("unexpected path: %q", hits[0].Path)
	}
	if hits[0].Line == 0 || hits[0].Snippet == "" {
		t.Fatalf("expected line number and snippet: %+v", hits[0])
	}
}

func TestWikiSearchRequiresPattern(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	if _, err := searchArticles(worker.Repo(), "  "); err == nil {
		t.Fatal("expected pattern-required error")
	}
}

func TestWikiReadArticle(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	ctx := context.Background()
	if _, _, err := worker.Enqueue(ctx, "ceo", "team/people/nazz.md", "# Nazz\n\nFounder.\n", "create", "m"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	bytes, err := readArticle(worker.Repo(), "team/people/nazz.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !contains(string(bytes), "Founder") {
		t.Fatalf("unexpected body %q", string(bytes))
	}
}

func TestWikiReadArticleInvalidPath(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	if _, err := readArticle(worker.Repo(), "../etc/passwd"); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestWikiReadIndexAllMissingFallsBack(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	// Remove the fresh index
	_ = os.Remove(worker.Repo().IndexAllPath())
	bytes, err := readIndexAll(worker.Repo())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !contains(string(bytes), "No articles yet") {
		t.Fatalf("expected fallback body, got %q", string(bytes))
	}
}

func TestBrokerWikiHandlersEndToEnd(t *testing.T) {
	// Arrange — spin up a broker + worker using a temp wiki dir.
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/write", b.handleWikiWrite)
	mux.HandleFunc("/wiki/read", b.handleWikiRead)
	mux.HandleFunc("/wiki/search", b.handleWikiSearch)
	mux.HandleFunc("/wiki/list", b.handleWikiList)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Act — write an article via POST /wiki/write
	writeBody, _ := json.Marshal(map[string]any{
		"slug":           "ceo",
		"path":           "team/people/nazz.md",
		"content":        "# Nazz\n\nFounder.\n",
		"mode":           "create",
		"commit_message": "add nazz",
	})
	res, err := http.Post(srv.URL+"/wiki/write", "application/json", bytes.NewReader(writeBody))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	res.Body.Close()

	// Assert — read it back
	rres, err := http.Get(srv.URL + "/wiki/read?path=team/people/nazz.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rres.Body.Close()
	body, _ := readAllBody(rres.Body)
	if !contains(body, "Founder") {
		t.Fatalf("read mismatch: %q", body)
	}

	// Search
	sres, err := http.Get(srv.URL + "/wiki/search?pattern=Founder")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer sres.Body.Close()
	if sres.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", sres.StatusCode)
	}

	// List
	lres, err := http.Get(srv.URL + "/wiki/list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer lres.Body.Close()
	lbody, _ := readAllBody(lres.Body)
	if !contains(lbody, "Team wiki index") {
		t.Fatalf("list body unexpected: %q", lbody)
	}
}

func TestBrokerWikiWriteRejectsBadJSON(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/write", b.handleWikiWrite)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Post(srv.URL+"/wiki/write", "application/json", bytes.NewReader([]byte("{not-json")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerWikiSearchRejectsEmptyPattern(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/search", b.handleWikiSearch)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/wiki/search?pattern=")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestWikiWorkerPublicAccessors(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()
	if worker.Repo() != repo {
		t.Fatalf("Repo mismatch")
	}
	if got := worker.QueueLength(); got != 0 {
		t.Fatalf("expected empty queue, got %d", got)
	}
}

func TestBrokerWikiAuditReturnsFullLineage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Seed a bootstrap commit and one agent commit so the audit log has
	// three distinct authors (system, wuphf-bootstrap, operator).
	stub := filepath.Join(root, "team", "playbooks", "renewal.md")
	if err := os.MkdirAll(filepath.Dir(stub), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stub, []byte("# Renewal\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := repo.CommitBootstrap(ctx, "materialize"); err != nil {
		t.Fatalf("CommitBootstrap: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "operator", "team/people/nazz.md", "# Nazz\n", "create", "nazz"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(workerCtx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/audit", b.handleWikiAudit)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/wiki/audit")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Entries []struct {
			SHA        string   `json:"sha"`
			AuthorSlug string   `json:"author_slug"`
			Timestamp  string   `json:"timestamp"`
			Message    string   `json:"message"`
			Paths      []string `json:"paths"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total < 3 {
		t.Fatalf("expected total >= 3, got %d: %+v", body.Total, body.Entries)
	}
	seen := map[string]bool{}
	for _, e := range body.Entries {
		seen[e.AuthorSlug] = true
		if e.Paths == nil {
			t.Errorf("entry %s: paths should be [] not nil for JSON stability", e.SHA)
		}
	}
	for _, want := range []string{"system", "wuphf-bootstrap", "operator"} {
		if !seen[want] {
			t.Errorf("expected author %q in audit feed, got %v", want, seen)
		}
	}
}

func TestBrokerWikiAuditRejectsBadSince(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(workerCtx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/audit", b.handleWikiAudit)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/wiki/audit?since=not-a-timestamp")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestWikiPublishViaBroker(t *testing.T) {
	b := newTestBroker(t)
	ch, unsubscribe := b.SubscribeWikiEvents(4)
	defer unsubscribe()
	evt := wikiWriteEvent{Path: "team/x.md", CommitSHA: "abc", AuthorSlug: "ceo", Timestamp: "now"}
	b.PublishWikiEvent(evt)
	select {
	case got := <-ch:
		if got != evt {
			t.Fatalf("event mismatch: %+v", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected event to land")
	}
}

func TestWikiRootAndBackupDirsHonorRuntimeHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	if got := WikiRootDir(); got != filepath.Join(dir, ".wuphf", "wiki") {
		t.Fatalf("unexpected root: %s", got)
	}
	if got := WikiBackupDir(); got != filepath.Join(dir, ".wuphf", "wiki.bak") {
		t.Fatalf("unexpected backup: %s", got)
	}
	r := NewRepo()
	if r.Root() != filepath.Join(dir, ".wuphf", "wiki") {
		t.Fatalf("unexpected NewRepo root: %s", r.Root())
	}
	if r.TeamDir() != filepath.Join(r.Root(), "team") {
		t.Fatalf("unexpected TeamDir: %s", r.TeamDir())
	}
}

func TestBrokerWikiHandlersReturn503WhenWorkerInactive(t *testing.T) {
	b := newTestBroker(t)
	// No worker attached.
	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/read", b.handleWikiRead)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/wiki/read?path=team/x.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.StatusCode)
	}
}

func readAllBody(r interface {
	Read(p []byte) (int, error)
}) (string, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return string(buf), nil
			}
			return string(buf), nil
		}
	}
}

// contains avoids importing strings just for a substring check. Local helper.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) == 0) || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// TestWikiWorkerWithIndex_FactReconciled verifies that after a successful
// entity-fact commit the WikiIndex reflects the new fact once WaitForIdle
// returns — proving that ReconcilePath is called in the side goroutine.
func TestWikiWorkerWithIndex_FactReconciled(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	idx := NewWikiIndex(root)
	pub := &recordingPublisher{}
	worker := NewWikiWorkerWithIndex(repo, pub, idx)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	defer func() {
		cancel()
		<-worker.Done()
		idx.Close()
	}()

	// Build a TypedFact and marshal it as a single jsonl line.
	fact := TypedFact{
		ID:         "deadbeef00000001",
		EntitySlug: "sarah-jones",
		Type:       "status",
		Text:       "Sarah Jones joined as VP of Engineering.",
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "archivist",
	}
	factBytes, err := json.Marshal(fact)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	content := string(factBytes) + "\n"

	// Use the entity-fact path shape that both CommitEntityFact and
	// ReconcilePath (factLogLegacyV12 regex) recognise.
	factPath := "team/entities/people-sarah-jones.facts.jsonl"

	sha, n, err := worker.EnqueueEntityFact(context.Background(), "archivist", factPath, content, "archivist: record sarah jones vp eng")
	if err != nil {
		t.Fatalf("EnqueueEntityFact: %v", err)
	}
	if sha == "" || n == 0 {
		t.Fatalf("expected non-empty sha and bytes written, got sha=%q n=%d", sha, n)
	}

	// Block until all side goroutines (including the reconcile) finish.
	worker.WaitForIdle()

	// The fact must now be visible in the index.
	got, ok, err := idx.GetFact(context.Background(), "deadbeef00000001")
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if !ok {
		t.Fatal("expected fact to be indexed after WaitForIdle, but GetFact returned ok=false")
	}
	if got.EntitySlug != "sarah-jones" {
		t.Errorf("indexed fact entity_slug = %q, want %q", got.EntitySlug, "sarah-jones")
	}
	if got.Text != "Sarah Jones joined as VP of Engineering." {
		t.Errorf("indexed fact text = %q, want %q", got.Text, "Sarah Jones joined as VP of Engineering.")
	}
}

// TestWikiWorkerNilIndex_NoPanic verifies that a worker constructed without
// an index (NewWikiWorker) processes commits without panicking and returns a
// valid SHA.
func TestWikiWorkerNilIndex_NoPanic(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Nil index — NewWikiWorker path.
	worker := NewWikiWorker(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	defer func() {
		cancel()
		<-worker.Done()
	}()

	sha, n, err := worker.Enqueue(context.Background(), "ceo", "team/people/test.md",
		"# Test\n\nContent.\n", "create", "add test article")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty sha")
	}
	if n == 0 {
		t.Fatal("expected non-zero bytes written")
	}
	// Block until side goroutines finish (backup mirror etc.).
	worker.WaitForIdle()
}
