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
	"sync"
	"testing"
	"time"
)

// pamPublisherStub captures Pam SSE events for assertions.
type pamPublisherStub struct {
	mu      sync.Mutex
	started []PamActionStartedEvent
	done    []PamActionDoneEvent
	failed  []PamActionFailedEvent
}

func (p *pamPublisherStub) PublishPamActionStarted(evt PamActionStartedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = append(p.started, evt)
}

func (p *pamPublisherStub) PublishPamActionDone(evt PamActionDoneEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = append(p.done, evt)
}

func (p *pamPublisherStub) PublishPamActionFailed(evt PamActionFailedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failed = append(p.failed, evt)
}

func (p *pamPublisherStub) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.started), len(p.done), len(p.failed)
}

func (p *pamPublisherStub) lastFailedError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.failed) == 0 {
		return ""
	}
	return p.failed[len(p.failed)-1].Error
}

// fakePamRunner is a deterministic PamRunner used in tests. It records the
// prompts it was given so assertions can verify Pam received the article.
// If entered is non-nil the runner signals it once per Run, right before
// invoking the responder — callers use this to guarantee the first job is
// inflight before enqueuing a coalesced follow-up.
type fakePamRunner struct {
	mu         sync.Mutex
	calls      int
	lastSystem string
	lastUser   string
	responder  func(system, user string) (string, error)
	entered    chan struct{}
}

func (f *fakePamRunner) Run(_ context.Context, system, user string) (string, error) {
	f.mu.Lock()
	f.calls++
	f.lastSystem = system
	f.lastUser = user
	resp := f.responder
	entered := f.entered
	f.mu.Unlock()
	if entered != nil {
		// Non-blocking: fire a single signal when the first call enters.
		// Coalescing tests only need to observe the transition to inflight
		// once; subsequent calls don't block on the channel.
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if resp == nil {
		return user, nil
	}
	return resp(system, user)
}

func (f *fakePamRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newPamFixture(t *testing.T, runner PamRunner) (*PamDispatcher, *WikiWorker, *pamPublisherStub, func()) {
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

	pub := &pamPublisherStub{}
	disp := NewPamDispatcher(worker, pub, PamDispatcherConfig{
		Timeout: 2 * time.Second,
		Runner:  runner,
	})
	disp.Start(context.Background())

	teardown := func() {
		disp.Stop()
		cancel()
		<-worker.Done()
	}
	return disp, worker, pub, teardown
}

func seedPamArticle(t *testing.T, worker *WikiWorker, path, body string) {
	t.Helper()
	if _, _, err := worker.Enqueue(context.Background(), "human", path, body, "replace", "seed: "+path); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func waitPamCounts(t *testing.T, pub *pamPublisherStub, started, done, failed int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, d, f := pub.counts()
		if s >= started && d >= done && f >= failed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, d, f := pub.counts()
	t.Fatalf("timed out waiting for started>=%d done>=%d failed>=%d; got %d/%d/%d", started, done, failed, s, d, f)
}

// ─── PamAction registry tests ──

func TestPamActions_RegistryHasEnrichArticle(t *testing.T) {
	a, err := LookupPamAction(PamActionEnrichArticle)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if a.Label == "" {
		t.Fatalf("expected label")
	}
	if !strings.Contains(a.UserPromptTmpl, "%s") {
		t.Fatalf("user prompt template must accept article body via %%s")
	}
	msg := a.renderCommitMsg("team/companies/acme.md")
	if !strings.Contains(msg, "archivist:") || !strings.Contains(msg, "team/companies/acme.md") {
		t.Fatalf("commit msg malformed: %q", msg)
	}
}

func TestPamActions_UnknownAction(t *testing.T) {
	_, err := LookupPamAction(PamActionID("does-not-exist"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestPamActions_MenuIsCopy(t *testing.T) {
	a := PamActions()
	b := PamActions()
	if &a[0] == &b[0] {
		t.Fatalf("PamActions must return a defensive copy")
	}
}

// ─── PamDispatcher happy path ──

func TestPamDispatcher_EnrichArticleCommitsAsArchivist(t *testing.T) {
	runner := &fakePamRunner{
		responder: func(_, user string) (string, error) {
			// Append a bogus line to prove the runner's output is what commits.
			return user + "\n\nPam was here.", nil
		},
	}
	disp, worker, pub, teardown := newPamFixture(t, runner)
	defer teardown()

	const path = "team/companies/acme.md"
	seedPamArticle(t, worker, path, "# Acme\n\nOld body.")

	id, err := disp.Enqueue(PamActionEnrichArticle, path, "human")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero job id")
	}

	waitPamCounts(t, pub, 1, 1, 0, 3*time.Second)

	if runner.callCount() != 1 {
		t.Fatalf("runner calls: want 1 got %d", runner.callCount())
	}
	if !strings.Contains(runner.lastUser, "Old body.") {
		t.Fatalf("runner never saw article body; got %q", runner.lastUser)
	}

	// The commit author must remain ArchivistAuthor — Pam is the archivist,
	// her rebrand must not change the git identity.
	repo := worker.Repo()
	out, err := runGitOutput(repo.Root(), "log", "-1", "--format=%an", "--", path)
	if err != nil {
		t.Fatalf("git log: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), ArchivistAuthor) {
		t.Fatalf("expected author %q on last commit of %s; got %q", ArchivistAuthor, path, string(out))
	}
}

// ─── PamDispatcher coalescing ──

// TestPamDispatcher_CoalescesRepeatedEnqueuesPerArticle uses an `entered`
// signal from the runner instead of a sleep to deterministically wait
// until the first job is inflight before enqueuing the follow-up.
//
// Post-Agent-1 contract: the second Enqueue for the same (action, path)
// returns the *existing* job id rather than zero.
func TestPamDispatcher_CoalescesRepeatedEnqueuesPerArticle(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	runner := &fakePamRunner{
		entered: entered,
		responder: func(_, user string) (string, error) {
			<-release
			return user + "\n\ndone", nil
		},
	}
	disp, worker, pub, teardown := newPamFixture(t, runner)
	defer teardown()

	const path = "team/companies/acme.md"
	seedPamArticle(t, worker, path, "# Acme\n\nBody.")

	id1, err := disp.Enqueue(PamActionEnrichArticle, path, "human")
	if err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	if id1 == 0 {
		t.Fatalf("expected non-zero id1")
	}
	// Wait for the first job to actually enter the runner — this is
	// the moment it transitions from queued to inflight.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("first job never entered runner")
	}
	id2, err := disp.Enqueue(PamActionEnrichArticle, path, "human")
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("expected coalesced id2=%d to match id1; got %d", id1, id2)
	}
	close(release)

	// Expect exactly 2 runner calls (original + 1 coalesced follow-up), and
	// two done events. A third enqueue during the window must not multiply.
	waitPamCounts(t, pub, 2, 2, 0, 4*time.Second)
	if runner.callCount() != 2 {
		t.Fatalf("want 2 runner calls; got %d", runner.callCount())
	}
}

// ─── PamDispatcher error paths ──

func TestPamDispatcher_UnknownActionRejected(t *testing.T) {
	disp, _, _, teardown := newPamFixture(t, &fakePamRunner{})
	defer teardown()
	_, err := disp.Enqueue(PamActionID("bogus"), "team/x.md", "human")
	if err == nil {
		t.Fatalf("expected ErrUnknownPamAction")
	}
}

func TestPamDispatcher_MissingArticlePublishesFailed(t *testing.T) {
	disp, _, pub, teardown := newPamFixture(t, &fakePamRunner{})
	defer teardown()
	if _, err := disp.Enqueue(PamActionEnrichArticle, "team/nope/does-not-exist.md", "human"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitPamCounts(t, pub, 0, 0, 1, 3*time.Second)
}

// TestPamDispatcher_RunnerErrorPublishesFailedNotDone covers the
// runner-returns-error branch: we must publish action_failed and never
// publish action_done for the same job.
func TestPamDispatcher_RunnerErrorPublishesFailedNotDone(t *testing.T) {
	runner := &fakePamRunner{
		responder: func(_, _ string) (string, error) {
			return "", errPamRunnerTest
		},
	}
	disp, worker, pub, teardown := newPamFixture(t, runner)
	defer teardown()

	const path = "team/companies/acme.md"
	seedPamArticle(t, worker, path, "# Acme\n\nBody.")

	if _, err := disp.Enqueue(PamActionEnrichArticle, path, "human"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitPamCounts(t, pub, 1, 0, 1, 3*time.Second)

	s, d, _ := pub.counts()
	if s != 1 {
		t.Fatalf("expected 1 started, got %d", s)
	}
	if d != 0 {
		t.Fatalf("expected 0 done, got %d", d)
	}
}

// errPamRunnerTest is a sentinel error used by the runner-error test.
var errPamRunnerTest = testPamError("pam test: runner failure")

type testPamError string

func (e testPamError) Error() string { return string(e) }

// TestPamDispatcher_EmptyOutputFails asserts that a runner returning an
// empty string publishes action_failed and does not commit anything.
func TestPamDispatcher_EmptyOutputFails(t *testing.T) {
	runner := &fakePamRunner{
		responder: func(_, _ string) (string, error) {
			return "", nil
		},
	}
	disp, worker, pub, teardown := newPamFixture(t, runner)
	defer teardown()

	const path = "team/companies/acme.md"
	seedPamArticle(t, worker, path, "# Acme\n\nBody.")

	if _, err := disp.Enqueue(PamActionEnrichArticle, path, "human"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitPamCounts(t, pub, 1, 0, 1, 3*time.Second)

	// No Pam commit should exist — only the seed commit.
	repo := worker.Repo()
	out, err := runGitOutput(repo.Root(), "log", "--format=%an", "--", path)
	if err != nil {
		t.Fatalf("git log: %v (%s)", err, out)
	}
	if strings.Count(string(out), ArchivistAuthor) != 0 {
		t.Fatalf("expected 0 archivist commits; got %q", string(out))
	}
}

// TestPamDispatcher_OverlargeOutputFails asserts that a runner returning
// more than MaxPamOutputSize bytes publishes action_failed and does not
// commit anything.
func TestPamDispatcher_OverlargeOutputFails(t *testing.T) {
	huge := strings.Repeat("x", MaxPamOutputSize+1)
	runner := &fakePamRunner{
		responder: func(_, _ string) (string, error) {
			return huge, nil
		},
	}
	disp, worker, pub, teardown := newPamFixture(t, runner)
	defer teardown()

	const path = "team/companies/acme.md"
	seedPamArticle(t, worker, path, "# Acme\n\nBody.")

	if _, err := disp.Enqueue(PamActionEnrichArticle, path, "human"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitPamCounts(t, pub, 1, 0, 1, 3*time.Second)

	errMsg := pub.lastFailedError()
	if !strings.Contains(errMsg, "exceeds") {
		t.Fatalf("expected size-limit error in failed event; got %q", errMsg)
	}

	repo := worker.Repo()
	out, err := runGitOutput(repo.Root(), "log", "--format=%an", "--", path)
	if err != nil {
		t.Fatalf("git log: %v (%s)", err, out)
	}
	if strings.Count(string(out), ArchivistAuthor) != 0 {
		t.Fatalf("expected 0 archivist commits; got %q", string(out))
	}
}

// ─── HTTP handler tests ──

// newPamHTTPFixture wires a broker + mux + httptest.Server with the Pam
// routes registered, matching the pattern from broker_notebook_test.go.
func newPamHTTPFixture(t *testing.T) (*httptest.Server, *Broker, func()) {
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

	// Install a fake runner so the dispatcher does not fork a real CLI.
	// We do this by priming the dispatcher ourselves and stashing it on
	// the broker before the first request arrives.
	fake := &fakePamRunner{
		responder: func(_, user string) (string, error) { return user + "\n\npam!", nil },
	}
	disp := NewPamDispatcher(worker, b, PamDispatcherConfig{
		Timeout: 2 * time.Second,
		Runner:  fake,
	})
	disp.Start(context.Background())
	b.mu.Lock()
	b.pamDispatcher = disp
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/pam/actions", b.requireAuth(b.handlePamActions))
	mux.HandleFunc("/pam/action", b.requireAuth(b.handlePamAction))
	srv := httptest.NewServer(mux)

	return srv, b, func() {
		srv.Close()
		disp.Stop()
		cancel()
		worker.Stop()
	}
}

func pamAuthReq(method, url string, body io.Reader, token string) (*http.Request, error) {
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

func TestHandlePamAction_MalformedJSON(t *testing.T) {
	srv, b, teardown := newPamHTTPFixture(t)
	defer teardown()

	req, err := pamAuthReq(http.MethodPost, srv.URL+"/pam/action", strings.NewReader("{"), b.Token())
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestHandlePamAction_MissingPath(t *testing.T) {
	srv, b, teardown := newPamHTTPFixture(t)
	defer teardown()

	body, _ := json.Marshal(map[string]any{
		"action":     "enrich_article",
		"path":       "",
		"actor_slug": "human",
	})
	req, _ := pamAuthReq(http.MethodPost, srv.URL+"/pam/action", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestHandlePamAction_UnknownAction(t *testing.T) {
	srv, b, teardown := newPamHTTPFixture(t)
	defer teardown()

	body, _ := json.Marshal(map[string]any{
		"action":     "not-a-real-action",
		"path":       "team/companies/acme.md",
		"actor_slug": "human",
	})
	req, _ := pamAuthReq(http.MethodPost, srv.URL+"/pam/action", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestHandlePamAction_InvalidActorSlug(t *testing.T) {
	srv, b, teardown := newPamHTTPFixture(t)
	defer teardown()

	body, _ := json.Marshal(map[string]any{
		"action":     "enrich_article",
		"path":       "team/companies/acme.md",
		"actor_slug": "evil;rm -rf /",
	})
	req, _ := pamAuthReq(http.MethodPost, srv.URL+"/pam/action", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	var parsed map[string]string
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if parsed["error"] != "invalid actor_slug" {
		t.Fatalf("expected invalid actor_slug error, got %q", parsed["error"])
	}
}

func TestHandlePamAction_BodyTooLarge(t *testing.T) {
	srv, b, teardown := newPamHTTPFixture(t)
	defer teardown()

	// 128 KiB of JSON payload — well over the 64 KiB cap.
	huge := strings.Repeat("a", 128*1024)
	body, _ := json.Marshal(map[string]any{
		"action":     "enrich_article",
		"path":       "team/companies/acme.md",
		"actor_slug": "human",
		"pad":        huge,
	})
	req, _ := pamAuthReq(http.MethodPost, srv.URL+"/pam/action", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge && res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 413 or 400 for oversized body, got %d", res.StatusCode)
	}
}

func TestHandlePamActions_WrongMethod(t *testing.T) {
	srv, b, teardown := newPamHTTPFixture(t)
	defer teardown()

	req, _ := pamAuthReq(http.MethodPost, srv.URL+"/pam/actions", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.StatusCode)
	}
}
