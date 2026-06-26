package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newStartedSourceWorker stands up a real Repo + started WikiWorker rooted in
// t.TempDir(), so EnqueueSource lands real files on disk. Returns the worker,
// its repo root, and a teardown that drains the worker before TempDir cleanup.
func newStartedSourceWorker(t *testing.T) (*WikiWorker, string, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return worker, root, func() {
		worker.Stop()
		<-worker.Done()
		cancel()
	}
}

// blockingSourceWorker satisfies sourceCaptureWorker but blocks every
// EnqueueSource call until release is closed. Used to prove that
// SourceCaptureDispatcher.Enqueue never runs EnqueueSource on the caller's
// goroutine (the calling goroutine would otherwise block here).
type blockingSourceWorker struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (w *blockingSourceWorker) EnqueueSource(ctx context.Context, rec SourceRecord) (string, int, error) {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	select {
	case <-w.release:
	case <-ctx.Done():
	}
	return "sha", len(rec.Content), nil
}

func TestSourceCaptureDispatcher_DrainsAllJobs(t *testing.T) {
	worker, root, teardown := newStartedSourceWorker(t)
	defer teardown()

	disp := NewSourceCaptureDispatcher(worker)
	disp.Start(context.Background())
	defer disp.Stop(2 * time.Second)

	const n = 12
	for i := 0; i < n; i++ {
		ok := disp.Enqueue(SourceCaptureJob{
			Kind:       SourceKindNote,
			Title:      fmt.Sprintf("note-%02d", i),
			Origin:     fmt.Sprintf("origin-%02d", i),
			Content:    fmt.Sprintf("body for note %d\n", i),
			CapturedAt: time.Now().UTC(),
		})
		if !ok {
			t.Fatalf("Enqueue %d returned false unexpectedly", i)
		}
	}

	noteDir := filepath.Join(root, "sources", "note")
	testTickUntil(t, 5*time.Second, func() bool {
		return countMarkdown(t, noteDir) == n
	})
	if got := countMarkdown(t, noteDir); got != n {
		t.Fatalf("drained %d sources, want %d", got, n)
	}
}

func TestSourceCaptureDispatcher_SaturationReturnsFalseWithoutBlocking(t *testing.T) {
	worker := &blockingSourceWorker{release: make(chan struct{})}
	defer close(worker.release)

	disp := NewSourceCaptureDispatcher(worker)
	disp.Start(context.Background())
	defer disp.Stop(2 * time.Second)

	// Enqueue well past the buffer. The drain pulls one job and blocks in the
	// fake worker; the rest fill the buffer; the overflow returns false. The
	// whole loop must stay fast — Enqueue is a non-blocking send, so the
	// calling goroutine never enters EnqueueSource.
	start := time.Now()
	sawFalse := false
	for i := 0; i < SourceCaptureQueue+50; i++ {
		ok := disp.Enqueue(SourceCaptureJob{
			Kind:    SourceKindNote,
			Title:   fmt.Sprintf("n-%d", i),
			Origin:  fmt.Sprintf("o-%d", i),
			Content: "x\n",
		})
		if !ok {
			sawFalse = true
		}
	}
	elapsed := time.Since(start)
	if !sawFalse {
		t.Fatal("expected at least one saturated Enqueue to return false")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Enqueue loop blocked for %s; sends must be non-blocking", elapsed)
	}
}

func TestSourceCaptureDispatcher_StopIsCleanAndIdempotent(t *testing.T) {
	worker, root, teardown := newStartedSourceWorker(t)
	defer teardown()

	disp := NewSourceCaptureDispatcher(worker)
	disp.Start(context.Background())

	if ok := disp.Enqueue(SourceCaptureJob{
		Kind:       SourceKindNote,
		Title:      "before-stop",
		Origin:     "before-stop",
		Content:    "landed before stop\n",
		CapturedAt: time.Now().UTC(),
	}); !ok {
		t.Fatal("Enqueue returned false")
	}
	noteDir := filepath.Join(root, "sources", "note")
	testTickUntil(t, 5*time.Second, func() bool { return countMarkdown(t, noteDir) == 1 })

	done := make(chan struct{})
	go func() {
		disp.Stop(2 * time.Second)
		disp.Stop(2 * time.Second) // idempotent second call
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("Stop did not return promptly")
	}

	// Enqueue after Stop is a no-op that returns false (does not panic).
	if ok := disp.Enqueue(SourceCaptureJob{Kind: SourceKindNote, Title: "after", Origin: "after", Content: "x\n"}); ok {
		t.Fatal("Enqueue after Stop should return false")
	}
}

// TestSourceCapture_Feeder1_CompletedTask drives a real RecordTaskDecision
// approval on a temp broker and asserts a sources/task/{id}.md lands carrying
// the spec, session report, reviewer grade, feedback, and deliverable path.
func TestSourceCapture_Feeder1_CompletedTask(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	b := newTestBroker(t)
	taskID := "task-src"
	seedTaskInState(t, b, taskID, LifecycleStateDecision)

	// Give the task a title + deliverable so the source captures both.
	b.mu.Lock()
	if task := b.findTaskByIDLocked(taskID); task != nil {
		task.Title = "Ship the capture layer"
		task.Artifact = "team/decisions/task-src.md"
	}
	b.mu.Unlock()

	if err := b.SetSpec(taskID, Spec{
		Problem:       "Office activity is never snapshotted",
		Assignment:    "build the source-capture dispatcher",
		TargetOutcome: "completed tasks land as immutable sources",
		AcceptanceCriteria: []ACItem{
			{Statement: "task source lands on approval", Done: true},
		},
		Constraints: []string{"never block under b.mu"},
	}); err != nil {
		t.Fatalf("SetSpec: %v", err)
	}
	if err := b.AppendSessionReport(taskID, SessionReport{
		Highlights: "Built the non-blocking dispatcher and three feeders.",
		TopWins:    []Win{{Delta: "+1 source layer", Description: "auto capture"}},
		DeadEnds:   []DeadEnd{{Tried: "inline commit under lock", Reason: "deadlocks the broker"}},
	}); err != nil {
		t.Fatalf("AppendSessionReport: %v", err)
	}
	if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
		ReviewerSlug: "reviewer-a",
		Severity:     SeverityMinor,
		Suggestion:   "name the drain goroutine",
	}); err != nil {
		t.Fatalf("AppendReviewerGrade: %v", err)
	}

	// Stand up a real wiki worker + capture dispatcher on the broker.
	wikiRoot := filepath.Join(dir, "wiki-repo")
	wikiBackup := filepath.Join(dir, "wiki-backup")
	repo := NewRepoAt(wikiRoot, wikiBackup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	b.startSourceCaptureDispatcher()
	t.Cleanup(func() {
		if disp := b.sourceCaptureDispatcher.Load(); disp != nil {
			disp.Stop(2 * time.Second)
		}
		worker.Stop()
		<-worker.Done()
		cancel()
	})

	if err := b.RecordTaskDecision(taskID, string(RecordDecisionApprove), "test-human"); err != nil {
		t.Fatalf("RecordTaskDecision: %v", err)
	}

	taskDir := filepath.Join(wikiRoot, "sources", "task")
	testTickUntil(t, 5*time.Second, func() bool { return countMarkdown(t, taskDir) >= 1 })

	body := readOnlyMarkdown(t, taskDir)
	for _, want := range []string{
		"Office activity is never snapshotted", // spec problem (title)
		"## Spec",
		"build the source-capture dispatcher", // assignment
		"## Session report",
		"## Reviewer grades",
		"reviewer-a",
		"## Deliverable",
		"team/decisions/task-src.md",
		"kind: task",
		"origin: task-src",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task source missing %q\n---\n%s", want, body)
		}
	}
}

// TestSourceCapture_Feeder4_TeamLearningFiresNoteHook verifies the worker
// invokes the source-capture hook (kind=note) after a successful team-learnings
// commit, carrying the regenerated markdown page as the source body.
func TestSourceCapture_Feeder4_TeamLearningFiresNoteHook(t *testing.T) {
	worker, _, teardown := newStartedSourceWorker(t)
	defer teardown()

	var (
		mu   sync.Mutex
		jobs []SourceCaptureJob
	)
	worker.SetSourceCaptureHook(func(job SourceCaptureJob) {
		mu.Lock()
		jobs = append(jobs, job)
		mu.Unlock()
	})

	page := "# Team Learnings\n\n- always drain off-lock\n"
	if _, _, err := worker.EnqueueTeamLearning(
		context.Background(),
		"librarian",
		TeamLearningsJSONLPath,
		`{"id":"l1","body":"always drain off-lock"}`+"\n",
		page,
		"learning: capture",
	); err != nil {
		t.Fatalf("EnqueueTeamLearning: %v", err)
	}

	testTickUntil(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(jobs) == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 note-capture job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.Kind != SourceKindNote {
		t.Errorf("kind = %q, want note", job.Kind)
	}
	if job.Origin != teamLearningsSourceOrigin {
		t.Errorf("origin = %q, want %q", job.Origin, teamLearningsSourceOrigin)
	}
	if !strings.Contains(job.Content, "always drain off-lock") {
		t.Errorf("note content missing learning body:\n%s", job.Content)
	}
}

// countMarkdown counts *.md files directly under dir (0 if dir is missing).
func countMarkdown(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

// readOnlyMarkdown returns the contents of the single *.md file under dir.
func readOnlyMarkdown(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			return string(data)
		}
	}
	t.Fatalf("no markdown file under %s", dir)
	return ""
}
