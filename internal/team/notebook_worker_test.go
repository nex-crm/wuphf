package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingNotebookPublisher captures both wiki and notebook events.
// Having both on one publisher mirrors the real Broker, which implements
// both interfaces.
type recordingNotebookPublisher struct {
	mu        sync.Mutex
	wikiEvs   []wikiWriteEvent
	notebookE []notebookWriteEvent
}

func (p *recordingNotebookPublisher) PublishWikiEvent(evt wikiWriteEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wikiEvs = append(p.wikiEvs, evt)
}

func (p *recordingNotebookPublisher) PublishNotebookEvent(evt notebookWriteEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notebookE = append(p.notebookE, evt)
}

func (p *recordingNotebookPublisher) notebookSnapshot() []notebookWriteEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]notebookWriteEvent, len(p.notebookE))
	copy(out, p.notebookE)
	return out
}

func newStartedNotebookWorker(t *testing.T) (*WikiWorker, *Repo, *recordingNotebookPublisher, context.CancelFunc) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	pub := &recordingNotebookPublisher{}
	worker := NewWikiWorker(repo, pub)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return worker, repo, pub, func() {
		cancel()
		worker.Stop()
	}
}

// TestNotebookWriteHappyPath covers the canonical create → commit → SSE event
// flow for a single-agent write.
func TestNotebookWriteHappyPath(t *testing.T) {
	worker, _, pub, teardown := newStartedNotebookWorker(t)
	defer teardown()
	path := "agents/pm/notebook/2026-04-20-retro.md"

	sha, n, err := worker.NotebookWrite(context.Background(), "pm", path,
		"# Retro\n\nDraft thoughts.\n", "create", "draft retro")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if sha == "" || n == 0 {
		t.Fatalf("expected non-empty sha and bytes, got %q / %d", sha, n)
	}

	// Event should land on the notebook channel, not the wiki channel.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(pub.notebookSnapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	evts := pub.notebookSnapshot()
	if len(evts) != 1 {
		t.Fatalf("expected 1 notebook event, got %d", len(evts))
	}
	if evts[0].Slug != "pm" || evts[0].Path != path || evts[0].CommitSHA == "" {
		t.Fatalf("unexpected event shape: %+v", evts[0])
	}
	// Wiki publisher MUST NOT have received an event for a notebook write.
	pub.mu.Lock()
	wikiCount := len(pub.wikiEvs)
	pub.mu.Unlock()
	if wikiCount != 0 {
		t.Fatalf("expected 0 wiki events, got %d", wikiCount)
	}
}

// TestNotebookWriteSlugMismatch ensures an agent cannot write to another
// agent's notebook directory.
func TestNotebookWriteSlugMismatch(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	_, _, err := worker.NotebookWrite(context.Background(), "pm",
		"agents/ceo/notebook/something.md", "# hi\n", "create", "m")
	if err == nil {
		t.Fatal("expected slug-mismatch rejection")
	}
	if !errors.Is(err, ErrNotebookPathNotAuthorOwned) {
		t.Fatalf("expected ErrNotebookPathNotAuthorOwned, got %v", err)
	}
}

// TestNotebookWritePathTraversal rejects .. segments and absolute paths.
func TestNotebookWritePathTraversal(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	cases := []struct {
		name string
		path string
	}{
		{"dot-dot-escape", "agents/pm/notebook/../../etc/passwd.md"},
		{"absolute", "/etc/passwd.md"},
		{"outside-agents", "team/people/other.md"},
		{"nested-subdir", "agents/pm/notebook/sub/x.md"},
		{"not-markdown", "agents/pm/notebook/x.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := worker.NotebookWrite(context.Background(), "pm", tc.path, "content\n", "create", "m")
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.path)
			}
		})
	}
}

// TestNotebookWriteModes covers each write mode's edge cases.
func TestNotebookWriteModes(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	path := "agents/pm/notebook/entry.md"

	// create on missing → OK
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, "# v1\n", "create", "m"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// create on existing → error
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, "# v2\n", "create", "m"); err == nil {
		t.Fatal("expected error when creating over existing file")
	}
	// replace on existing → OK
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, "# v2\n", "replace", "m"); err != nil {
		t.Fatalf("replace existing: %v", err)
	}
	// replace on missing → error
	if _, _, err := worker.NotebookWrite(ctx, "pm", "agents/pm/notebook/other.md", "# x\n", "replace", "m"); err == nil {
		t.Fatal("expected error when replacing missing file")
	}
	// append_section on existing → content appended
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, "## Section B\n", "append_section", "m"); err != nil {
		t.Fatalf("append: %v", err)
	}
	b, err := worker.NotebookRead(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, "# v2") || !strings.Contains(body, "## Section B") {
		t.Fatalf("append body missing pieces: %q", body)
	}
	// append_section on missing → creates fresh
	freshPath := "agents/pm/notebook/fresh.md"
	if _, _, err := worker.NotebookWrite(ctx, "pm", freshPath, "# Fresh\n", "append_section", "m"); err != nil {
		t.Fatalf("append on missing: %v", err)
	}
	// unknown mode → error
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, "x", "banana", "m"); err == nil {
		t.Fatal("expected error for bogus mode")
	}
}

// TestNotebookWriteQueueSaturation fills the shared queue without a drain
// running and confirms the saturated error surfaces.
func TestNotebookWriteQueueSaturation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	worker.running.Store(true)
	defer worker.running.Store(false)

	for i := 0; i < wikiRequestBuffer; i++ {
		req := wikiWriteRequest{
			Slug:       "pm",
			Path:       "agents/pm/notebook/x.md",
			Content:    "x",
			Mode:       "create",
			IsNotebook: true,
			ReplyCh:    make(chan wikiWriteResult, 1),
		}
		select {
		case worker.requests <- req:
		default:
			t.Fatalf("buffer filled early at %d", i)
		}
	}
	_, _, err := worker.NotebookWrite(context.Background(), "pm",
		"agents/pm/notebook/overflow.md", "x", "create", "m")
	if !errors.Is(err, ErrQueueSaturated) {
		t.Fatalf("expected ErrQueueSaturated, got %v", err)
	}
}

// TestNotebookWriteWorkerStopped returns ErrWorkerStopped before enqueue.
func TestNotebookWriteWorkerStopped(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	_, _, err := worker.NotebookWrite(context.Background(), "pm",
		"agents/pm/notebook/x.md", "x", "create", "m")
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}
}

// TestNotebookWriteRequiresSlug rejects missing slug.
func TestNotebookWriteRequiresSlug(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	_, _, err := worker.NotebookWrite(context.Background(), "",
		"agents/pm/notebook/x.md", "x", "create", "m")
	if err == nil {
		t.Fatal("expected slug-required error")
	}
}

// TestNotebookWriteInvalidSlug rejects dangerous slug characters.
func TestNotebookWriteInvalidSlug(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	_, _, err := worker.NotebookWrite(context.Background(), "pm/../other",
		"agents/pm/../other/notebook/x.md", "x", "create", "m")
	if err == nil {
		t.Fatal("expected rejection for dangerous slug")
	}
}

// TestNotebookListEmpty returns empty slice, not error, when no entries.
func TestNotebookListEmpty(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	entries, err := worker.NotebookList("pm")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// TestNotebookListReverseChronological seeds several dated entries and
// verifies the list order.
func TestNotebookListReverseChronological(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	names := []string{
		"2026-04-18-a.md",
		"2026-04-20-c.md",
		"2026-04-19-b.md",
	}
	for _, n := range names {
		if _, _, err := worker.NotebookWrite(ctx, "pm", "agents/pm/notebook/"+n,
			"# "+n+"\n", "create", "m"); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	entries, err := worker.NotebookList("pm")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Filename-sort-desc: -20, -19, -18
	if !strings.Contains(entries[0].Path, "2026-04-20") ||
		!strings.Contains(entries[1].Path, "2026-04-19") ||
		!strings.Contains(entries[2].Path, "2026-04-18") {
		t.Fatalf("order wrong: %+v", entries)
	}
}

// TestNotebookListCrossAgent verifies an agent can list another agent's
// notebook directory.
func TestNotebookListCrossAgent(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	if _, _, err := worker.NotebookWrite(ctx, "ceo",
		"agents/ceo/notebook/2026-04-20-decisions.md", "# d\n", "create", "m"); err != nil {
		t.Fatalf("ceo write: %v", err)
	}
	// PM lists CEO's notebook
	entries, err := worker.NotebookList("ceo")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

// TestNotebookListSkipsDotfiles ensures .gitkeep and non-.md files are
// filtered out.
func TestNotebookListSkipsDotfiles(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	if _, _, err := worker.NotebookWrite(ctx, "pm",
		"agents/pm/notebook/note.md", "# n\n", "create", "m"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Drop a .gitkeep and a .txt directly in the dir
	dir := filepath.Join(worker.repo.Root(), "agents/pm/notebook")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o600); err != nil {
		t.Fatalf("gitkeep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("raw"), 0o600); err != nil {
		t.Fatalf("txt: %v", err)
	}
	entries, err := worker.NotebookList("pm")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (note.md), got %d", len(entries))
	}
}

// TestNotebookListBadSlug rejects garbage.
func TestNotebookListBadSlug(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	if _, err := worker.NotebookList(""); err == nil {
		t.Fatal("expected empty-slug rejection")
	}
	if _, err := worker.NotebookList("bad/slug"); err == nil {
		t.Fatal("expected invalid-character rejection")
	}
}

// TestNotebookReadHappy and not-found and path-traversal.
func TestNotebookRead(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	path := "agents/pm/notebook/note.md"
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, "# hi\n\nbody\n", "create", "m"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// happy
	b, err := worker.NotebookRead(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "body") {
		t.Fatalf("unexpected body %q", string(b))
	}
	// not-found → error (os.ReadFile error, not validation)
	if _, err := worker.NotebookRead("agents/pm/notebook/missing.md"); err == nil {
		t.Fatal("expected not-found error")
	}
	// path-traversal
	if _, err := worker.NotebookRead("agents/pm/notebook/../../etc/passwd.md"); err == nil {
		t.Fatal("expected path-traversal rejection")
	}
	// outside agents/
	if _, err := worker.NotebookRead("team/people/nazz.md"); err == nil {
		t.Fatal("expected outside-agents rejection")
	}
	// wrong shape
	if _, err := worker.NotebookRead("agents/pm/other/note.md"); err == nil {
		t.Fatal("expected wrong-subdir rejection")
	}
}

// TestNotebookSearch covers happy / no-match / cross-agent / special-chars.
func TestNotebookSearch(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	if _, _, err := worker.NotebookWrite(ctx, "pm",
		"agents/pm/notebook/retro.md", "# Retro\n\nShipped $(whoami) last week.\n", "create", "m"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := worker.NotebookWrite(ctx, "ceo",
		"agents/ceo/notebook/decisions.md", "# Decisions\n\nShip PM's retro.\n", "create", "m"); err != nil {
		t.Fatalf("ceo write: %v", err)
	}

	// happy: search scoped to pm finds only pm's hit
	hits, err := worker.NotebookSearch("pm", "Shipped")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Path, "pm/notebook") {
		t.Fatalf("unexpected hits: %+v", hits)
	}

	// cross-agent: PM searches CEO's notebook
	hits, err = worker.NotebookSearch("ceo", "Decisions")
	if err != nil {
		t.Fatalf("ceo search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected ceo hit")
	}

	// no-match
	hits, err = worker.NotebookSearch("pm", "nonexistent-phrase-xyz")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(hits))
	}

	// special chars — literal match, no regex surface
	hits, err = worker.NotebookSearch("pm", "$(whoami)")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for special-chars substring, got %d", len(hits))
	}

	// empty pattern → error
	if _, err := worker.NotebookSearch("pm", "   "); err == nil {
		t.Fatal("expected empty-pattern rejection")
	}

	// bad slug → error
	if _, err := worker.NotebookSearch("bad/slug", "x"); err == nil {
		t.Fatal("expected bad-slug rejection")
	}

	// empty slug → error
	if _, err := worker.NotebookSearch("", "x"); err == nil {
		t.Fatal("expected empty-slug rejection")
	}

	// missing dir for valid slug returns empty slice, not error
	hits, err = worker.NotebookSearch("ghost", "x")
	if err != nil {
		t.Fatalf("search on missing dir: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits on missing dir, got %d", len(hits))
	}
}

// TestNotebookConcurrentWrites fires 10 parallel writes with distinct author
// slugs and distinct paths. All must succeed with correct author attribution
// and no git index corruption.
func TestNotebookConcurrentWrites(t *testing.T) {
	worker, repo, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	goroutines := 10

	var wg sync.WaitGroup
	var errCount atomic.Int32
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		slug := "agent" + string(rune('a'+i))
		path := "agents/" + slug + "/notebook/entry.md"
		go func(slug, path string) {
			defer wg.Done()
			if _, _, err := worker.NotebookWrite(context.Background(), slug, path,
				"# "+slug+"\n\nbody\n", "create", "m"); err != nil {
				errCount.Add(1)
				t.Logf("write %s err: %v", slug, err)
			}
		}(slug, path)
	}
	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("expected all writes to succeed, got %d errors", errCount.Load())
	}

	// Sanity-check the git log: every author slug MUST appear at least once.
	logOut, err := repo.runGitLocked(context.Background(), "system",
		"log", "--format=%an", "-n", "20")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	for i := 0; i < goroutines; i++ {
		slug := "agent" + string(rune('a'+i))
		if !strings.Contains(logOut, slug) {
			t.Fatalf("expected slug %q in git log, got %q", slug, logOut)
		}
	}
}

// TestNotebookWriteIdempotentReplay covers the "byte-identical re-write"
// branch in commitNotebookLocked — it must return the current HEAD sha
// without creating a new commit.
func TestNotebookWriteIdempotentReplay(t *testing.T) {
	worker, repo, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	path := "agents/pm/notebook/note.md"
	content := "# n\n\nbody\n"
	if _, _, err := worker.NotebookWrite(ctx, "pm", path, content, "create", "m1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	sha1, err := repo.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	// Replay with identical bytes via replace mode — nothing staged, no new commit.
	sha, _, err := worker.NotebookWrite(ctx, "pm", path, content, "replace", "m2")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if strings.TrimSpace(sha) != strings.TrimSpace(sha1) {
		t.Fatalf("expected same HEAD sha, got %q vs %q", sha, sha1)
	}
}

// TestNotebookContentRequired rejects empty/whitespace content.
func TestNotebookContentRequired(t *testing.T) {
	worker, _, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	_, _, err := worker.NotebookWrite(context.Background(), "pm",
		"agents/pm/notebook/x.md", "   ", "create", "m")
	if err == nil {
		t.Fatal("expected content-required rejection")
	}
}

// TestNotebookWriteDoesNotRegenWikiIndex ensures notebook writes do NOT touch
// index/all.md — that file is wiki-scoped and should remain unchanged.
func TestNotebookWriteDoesNotRegenWikiIndex(t *testing.T) {
	worker, repo, _, teardown := newStartedNotebookWorker(t)
	defer teardown()
	ctx := context.Background()
	indexPath := repo.IndexAllPath()
	before, _ := os.ReadFile(indexPath)

	if _, _, err := worker.NotebookWrite(ctx, "pm",
		"agents/pm/notebook/x.md", "# x\n", "create", "m"); err != nil {
		t.Fatalf("write: %v", err)
	}
	after, _ := os.ReadFile(indexPath)
	if string(before) != string(after) {
		t.Fatalf("notebook write unexpectedly changed index/all.md: before=%q after=%q", string(before), string(after))
	}
}
