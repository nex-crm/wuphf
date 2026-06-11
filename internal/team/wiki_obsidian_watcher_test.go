package team

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newObsidianWatcherFixture mirrors newGraphFixture but returns a real
// fsnotify-backed ObsidianWatcher wired to a tempdir Repo + WikiWorker.
// The returned debounce is short by default (50ms) so trailing-edge fires
// finish well inside test timeouts.
func newObsidianWatcherFixture(t *testing.T) (*Repo, *WikiWorker, *ObsidianWatcher, context.CancelFunc) {
	t.Helper()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	watcher := NewObsidianWatcher(repo, worker)
	watcher.SetDebounceForTest(50 * time.Millisecond)
	watcher.SetIdentity(func() (string, bool) { return "sarah", true })

	if err := watcher.Start(ctx); err != nil {
		cancel()
		worker.Stop()
		t.Fatalf("start watcher: %v", err)
	}

	teardown := func() {
		_ = watcher.Stop()
		cancel()
		worker.Stop()
		<-worker.Done()
	}
	return repo, worker, watcher, teardown
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// waitForHEADChange polls until the short HEAD SHA differs from `prev`,
// returning the new SHA. Fails the test on timeout. Uses select/time.After
// rather than Sleep so the deadline is structural; fsnotify + git commit
// are OS-driven so a software clock can't replace real time here.
func waitForHEADChange(t *testing.T, repo *Repo, prev string, timeout time.Duration) string {
	t.Helper()
	deadlineCh := time.After(timeout)
	for {
		sha, err := repo.HeadSHA(context.Background())
		if err == nil && sha != "" && sha != prev {
			return sha
		}
		select {
		case <-deadlineCh:
			t.Fatalf("HEAD did not advance from %q within %s", prev, timeout)
			return ""
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// requireHEADStable asserts that HEAD does not advance within window.
func requireHEADStable(t *testing.T, repo *Repo, prev string, window time.Duration) {
	t.Helper()
	deadlineCh := time.After(window)
	for {
		sha, err := repo.HeadSHA(context.Background())
		if err == nil && sha != prev {
			t.Fatalf("HEAD advanced unexpectedly: %s → %s", prev, sha)
		}
		select {
		case <-deadlineCh:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestObsidianWatcher_StartStop(t *testing.T) {
	_, _, _, teardown := newObsidianWatcherFixture(t)
	defer teardown()
}

func TestObsidianWatcher_CreateMarkdownCommits(t *testing.T) {
	repo, _, _, teardown := newObsidianWatcherFixture(t)
	defer teardown()

	prev, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	rel := "team/people/test.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	writeFile(t, full, "# Test person\n\nNotes.\n")

	newSHA := waitForHEADChange(t, repo, prev, 3*time.Second)
	if newSHA == prev {
		t.Fatalf("expected commit; head unchanged")
	}
}

func TestObsidianWatcher_ModifyExistingFileReplaces(t *testing.T) {
	repo, _, _, teardown := newObsidianWatcherFixture(t)
	defer teardown()

	rel := "team/people/sarah.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	writeFile(t, full, "# Sarah\n\nv1\n")
	first := waitForHEADChange(t, repo, "", 3*time.Second)

	writeFile(t, full, "# Sarah\n\nv2 with updated notes\n")
	second := waitForHEADChange(t, repo, first, 3*time.Second)

	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read after commit: %v", err)
	}
	if !strings.Contains(string(got), "v2 with updated notes") {
		t.Fatalf("file does not contain v2 content: %q", got)
	}
	if second == first {
		t.Fatalf("HEAD did not advance for replace commit")
	}
}

func TestObsidianWatcher_NotifyWriteFiltersOwnEvents(t *testing.T) {
	repo, _, watcher, teardown := newObsidianWatcherFixture(t)
	defer teardown()

	// First commit so HEAD is at a known SHA.
	rel := "team/people/sarah.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	writeFile(t, full, "# Sarah\n\ninitial\n")
	first := waitForHEADChange(t, repo, "", 3*time.Second)

	// Pretend the worker is about to write to the same path. NotifyWrite
	// records the path within the 5s TTL; the subsequent fsnotify event
	// should be dropped.
	watcher.NotifyWrite(rel)
	writeFile(t, full, "# Sarah\n\nworker-originated content\n")

	requireHEADStable(t, repo, first, 500*time.Millisecond)
}

// B3/B4 knowledge-integrity regression: a worker (agent-authored) commit
// makes the file identical to HEAD, so the watcher's fsnotify echo must NOT
// produce a follow-up "wiki: external edit" commit attributed to the human
// identity. Production never calls NotifyWrite for worker commits — the v3
// run's git history was all "human · wiki: external edit" because every
// agent commit was echoed with a fresh sentinel stamp, and that sentinel
// commit re-triggered the watcher into a commit storm ("173 revisions").
func TestObsidianWatcher_WorkerCommitEchoKeepsAgentAuthor(t *testing.T) {
	repo, worker, _, teardown := newObsidianWatcherFixture(t)
	defer teardown()

	rel := "team/people/agent-authored.md"
	if _, _, err := worker.Enqueue(context.Background(), "eng", rel,
		"# Agent authored\n\nWritten through the worker.\n", "create", "agent: brief"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	worker.WaitForIdle()
	head, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	// The fsnotify echo of the worker's own write must be dropped by the
	// dirty-vs-HEAD guard (fixture debounce is 50ms; give it 10x).
	requireHEADStable(t, repo, head, 500*time.Millisecond)

	refs, err := repo.Log(context.Background(), rel)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected exactly 1 commit for %s; got %d: %#v", rel, len(refs), refs)
	}
	if refs[0].Author != "eng" {
		t.Fatalf("agent write misattributed: author=%q want %q", refs[0].Author, "eng")
	}
	if strings.Contains(refs[0].Message, "external edit") {
		t.Fatalf("worker commit must not be an external-edit echo: %q", refs[0].Message)
	}
}

func TestObsidianWatcher_DebounceCoalescesRapidWrites(t *testing.T) {
	repo, _, watcher, teardown := newObsidianWatcherFixture(t)
	defer teardown()
	// Widen the debounce so multiple writes land inside a single window.
	watcher.SetDebounceForTest(300 * time.Millisecond)

	prev, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	rel := "team/people/burst.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	for i := 0; i < 5; i++ {
		writeFile(t, full, "# Burst\n\nversion "+string(rune('a'+i))+"\n")
		<-time.After(40 * time.Millisecond)
	}

	newSHA := waitForHEADChange(t, repo, prev, 3*time.Second)

	// Confirm content is the final version (latest write wins).
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "version e") {
		t.Fatalf("expected final content `version e`; got %q", got)
	}

	// Verify only one commit landed for that path: the most recent commit
	// touching rel should be the one we just observed; the one before it
	// should pre-date our writes (an init / unrelated commit).
	refs, err := repo.Log(context.Background(), rel)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected exactly 1 commit for %s; got %d: %#v", rel, len(refs), refs)
	}
	if refs[0].SHA != newSHA {
		t.Fatalf("log head sha mismatch: log=%s head=%s", refs[0].SHA, newSHA)
	}
}

func TestObsidianWatcher_IdentityUnavailableSkipsCommit(t *testing.T) {
	repo, _, watcher, teardown := newObsidianWatcherFixture(t)
	defer teardown()
	watcher.SetIdentity(func() (string, bool) { return "", false })

	var buf bytes.Buffer
	var bufMu sync.Mutex
	watcher.SetLogger(log.New(syncWriter{w: &buf, mu: &bufMu}, "", 0))

	prev, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	rel := "team/people/test.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	writeFile(t, full, "# Test\n\nbody\n")

	requireHEADStable(t, repo, prev, 500*time.Millisecond)

	bufMu.Lock()
	logged := buf.String()
	bufMu.Unlock()
	if !strings.Contains(logged, "identity unavailable") {
		t.Fatalf("expected identity-unavailable log; got %q", logged)
	}
}

func TestObsidianWatcher_IgnoresCompiledPlaybookDir(t *testing.T) {
	repo, _, _, teardown := newObsidianWatcherFixture(t)
	defer teardown()

	prev, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	rel := "team/playbooks/.compiled/some-skill/SKILL.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	writeFile(t, full, "# Compiled\n\nworker-owned output\n")

	requireHEADStable(t, repo, prev, 500*time.Millisecond)
}

func TestObsidianWatcher_StopDuringPendingDebounce(t *testing.T) {
	repo, _, watcher, teardown := newObsidianWatcherFixture(t)
	// Wide debounce so the timer is still pending when we Stop.
	watcher.SetDebounceForTest(1500 * time.Millisecond)

	prev, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	rel := "team/people/pending.md"
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	writeFile(t, full, "# Pending\n\nbody\n")

	// Give fsnotify time to deliver the event and arm the timer, then
	// stop the watcher before the trailing edge.
	<-time.After(150 * time.Millisecond)
	if err := watcher.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Trailing edge would have fired around 1.5s from the write; wait
	// long enough to be sure it did not.
	requireHEADStable(t, repo, prev, 2*time.Second)

	teardown()
}

// syncWriter is a tiny thread-safe io.Writer wrapper around bytes.Buffer.
// The logger and the goroutine running Stop's drain may both touch the
// underlying buffer; the lock keeps `go test -race` quiet.
type syncWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (s syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
