package team

import (
	"context"
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

// fakePamRunner is a deterministic PamRunner used in tests. It records the
// prompts it was given so assertions can verify Pam received the article.
type fakePamRunner struct {
	mu          sync.Mutex
	calls       int
	lastSystem  string
	lastUser    string
	responder   func(system, user string) (string, error)
}

func (f *fakePamRunner) Run(_ context.Context, system, user string) (string, error) {
	f.mu.Lock()
	f.calls++
	f.lastSystem = system
	f.lastUser = user
	resp := f.responder
	f.mu.Unlock()
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

func TestPamDispatcher_CoalescesRepeatedEnqueuesPerArticle(t *testing.T) {
	release := make(chan struct{})
	runner := &fakePamRunner{
		responder: func(_, user string) (string, error) {
			<-release
			return user + "\n\ndone", nil
		},
	}
	disp, worker, pub, teardown := newPamFixture(t, runner)
	defer teardown()

	const path = "team/companies/acme.md"
	seedPamArticle(t, worker, path, "# Acme\n\nBody.")

	if _, err := disp.Enqueue(PamActionEnrichArticle, path, "human"); err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	// Give the first job time to transition to in-flight.
	time.Sleep(50 * time.Millisecond)
	id2, err := disp.Enqueue(PamActionEnrichArticle, path, "human")
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	if id2 != 0 {
		t.Fatalf("expected coalesced id=0; got %d", id2)
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
