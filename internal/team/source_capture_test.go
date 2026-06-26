package team

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// fakeGBrainCaptureClient is a fake put_page sink. It records every PutPage call
// so tests can assert the page body, slug, source_kind, and ingested_via the
// dispatcher writes. It satisfies both gbrainPutPager (the writer's seam) and
// gbrainMemoryClient (so it can be injected via setSharedGBrainClient to drive
// the full broker -> dispatcher -> gbrain path).
type fakeGBrainCaptureClient struct {
	mu    sync.Mutex
	calls []capturedPut
	err   error // when set, PutPage returns it
}

type capturedPut struct {
	content string
	opts    gbrain.PutOptions
}

func (f *fakeGBrainCaptureClient) Query(ctx context.Context, query string, limit int) ([]gbrain.SearchResult, error) {
	return nil, nil
}

func (f *fakeGBrainCaptureClient) PutPage(ctx context.Context, content string, opts gbrain.PutOptions) (gbrain.PutResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return gbrain.PutResult{}, f.err
	}
	f.calls = append(f.calls, capturedPut{content: content, opts: opts})
	return gbrain.PutResult{Slug: opts.Slug, Status: "ok"}, nil
}

func (f *fakeGBrainCaptureClient) snapshot() []capturedPut {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedPut, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeGBrainCaptureClient) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// distinctSlugs returns the set of slugs PutPage was called with.
func (f *fakeGBrainCaptureClient) distinctSlugs() map[string]struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	slugs := map[string]struct{}{}
	for _, c := range f.calls {
		slugs[c.opts.Slug] = struct{}{}
	}
	return slugs
}

// lastFor returns the most recent captured put for a slug (ok=false if none).
func (f *fakeGBrainCaptureClient) lastFor(slug string) (capturedPut, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].opts.Slug == slug {
			return f.calls[i], true
		}
	}
	return capturedPut{}, false
}

// newFakeCaptureWriter builds a gbrain-backed writer wired to a fake put_page
// sink, returning both so a test can drive the writer and assert the calls.
func newFakeCaptureWriter() (*gbrainSourceWriter, *fakeGBrainCaptureClient) {
	fake := &fakeGBrainCaptureClient{}
	w := &gbrainSourceWriter{resolve: func() gbrainPutPager { return fake }}
	return w, fake
}

// blockingSourceWriter satisfies sourceCaptureWriter but blocks every
// WriteSource call until release is closed. Used to prove that
// SourceCaptureDispatcher.Enqueue never runs WriteSource on the caller's
// goroutine (the calling goroutine would otherwise block here).
type blockingSourceWriter struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (w *blockingSourceWriter) WriteSource(ctx context.Context, job SourceCaptureJob) error {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	select {
	case <-w.release:
	case <-ctx.Done():
	}
	return nil
}

func TestSourceCaptureDispatcher_DrainsAllJobs(t *testing.T) {
	writer, fake := newFakeCaptureWriter()
	disp := NewSourceCaptureDispatcher(writer)
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

	testTickUntil(t, 5*time.Second, func() bool { return fake.count() == n })
	calls := fake.snapshot()
	if len(calls) != n {
		t.Fatalf("drained %d puts, want %d", len(calls), n)
	}
	for i, c := range calls {
		if strings.TrimSpace(c.opts.Slug) == "" {
			t.Errorf("call %d has empty slug", i)
		}
		if c.opts.IngestedVia != gbrainCaptureIngestedVia {
			t.Errorf("call %d ingested_via = %q, want %q", i, c.opts.IngestedVia, gbrainCaptureIngestedVia)
		}
		if c.opts.SourceKind != gbrainCaptureSourceKindPrefix+string(SourceKindNote) {
			t.Errorf("call %d source_kind = %q, want %q", i, c.opts.SourceKind, gbrainCaptureSourceKindPrefix+string(SourceKindNote))
		}
	}
}

func TestSourceCaptureDispatcher_SaturationReturnsFalseWithoutBlocking(t *testing.T) {
	writer := &blockingSourceWriter{release: make(chan struct{})}
	defer close(writer.release)

	disp := NewSourceCaptureDispatcher(writer)
	disp.Start(context.Background())
	defer disp.Stop(2 * time.Second)

	// Enqueue well past the buffer. The drain pulls one job and blocks in the
	// fake writer; the rest fill the buffer; the overflow returns false. The
	// whole loop must stay fast — Enqueue is a non-blocking send, so the
	// calling goroutine never enters WriteSource.
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
	writer, fake := newFakeCaptureWriter()
	disp := NewSourceCaptureDispatcher(writer)
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
	testTickUntil(t, 5*time.Second, func() bool { return fake.count() == 1 })

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

// TestGBrainSourceWriter_RendersFrontmatterAndKinds drives WriteSource directly
// for each source kind and asserts the slug, source_kind, ingested_via, and the
// rendered frontmatter + body that land in gbrain.
func TestGBrainSourceWriter_RendersFrontmatterAndKinds(t *testing.T) {
	writer, fake := newFakeCaptureWriter()

	cases := []struct {
		kind   SourceKind
		origin string
		title  string
		body   string
	}{
		{SourceKindTask, "task-42", "Ship the capture layer", "Spec and session report.\n"},
		{SourceKindChat, "general:2026-06-25", "general digest", "jim: ship it?\npam: green, go\n"},
		{SourceKindNote, "team-learnings", "Team Learnings", "- always drain off-lock\n"},
		{SourceKindDecision, "decision-7", "Pick gbrain", "We chose put_page.\n"},
	}

	for _, tc := range cases {
		job := SourceCaptureJob{
			Kind:       tc.kind,
			ID:         DeriveSourceID(tc.kind, tc.origin, tc.title, tc.body),
			Title:      tc.title,
			Origin:     tc.origin,
			Content:    tc.body,
			CapturedAt: time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC),
		}
		if err := writer.WriteSource(context.Background(), job); err != nil {
			t.Fatalf("WriteSource(%s): %v", tc.kind, err)
		}

		wantSlug := DeriveSourceID(tc.kind, tc.origin, tc.title, tc.body)
		put, ok := fake.lastFor(wantSlug)
		if !ok {
			t.Fatalf("%s: no PutPage for slug %q", tc.kind, wantSlug)
		}
		if put.opts.SourceKind != gbrainCaptureSourceKindPrefix+string(tc.kind) {
			t.Errorf("%s: source_kind = %q, want %q", tc.kind, put.opts.SourceKind, gbrainCaptureSourceKindPrefix+string(tc.kind))
		}
		if put.opts.IngestedVia != gbrainCaptureIngestedVia {
			t.Errorf("%s: ingested_via = %q, want %q", tc.kind, put.opts.IngestedVia, gbrainCaptureIngestedVia)
		}
		for _, want := range []string{
			"---\n",
			"title: " + tc.title,
			"kind: " + string(tc.kind),
			"origin: " + tc.origin,
			"captured_at:",
			"2026-06-25T09:00:00Z",
			"source: " + gbrainCaptureSource,
			"- " + gbrainCaptureTag,
			"- " + string(tc.kind),
			strings.TrimRight(tc.body, "\n"),
		} {
			if !strings.Contains(put.content, want) {
				t.Errorf("%s: page body missing %q\n---\n%s", tc.kind, want, put.content)
			}
		}
	}

	// Exactly one PutPage per case (distinct origins => distinct slugs).
	if got := fake.count(); got != len(cases) {
		t.Fatalf("got %d puts, want %d", got, len(cases))
	}
}

// TestGBrainSourceWriter_IdempotentBySlug proves re-capturing the same office
// event upserts the same gbrain slug rather than duplicating it.
func TestGBrainSourceWriter_IdempotentBySlug(t *testing.T) {
	writer, fake := newFakeCaptureWriter()
	job := SourceCaptureJob{
		Kind:       SourceKindTask,
		ID:         DeriveSourceID(SourceKindTask, "task-99", "title", "body\n"),
		Title:      "title",
		Origin:     "task-99",
		Content:    "body\n",
		CapturedAt: time.Now().UTC(),
	}
	for i := 0; i < 3; i++ {
		if err := writer.WriteSource(context.Background(), job); err != nil {
			t.Fatalf("WriteSource %d: %v", i, err)
		}
	}
	if got := fake.count(); got != 3 {
		t.Fatalf("expected 3 PutPage calls, got %d", got)
	}
	if slugs := fake.distinctSlugs(); len(slugs) != 1 {
		t.Fatalf("expected 1 distinct slug (upsert), got %d: %v", len(slugs), slugs)
	}
}

// TestGBrainSourceWriter_NilClientLogsAndDrops proves an absent gbrain client is
// a graceful no-op: no panic, no error, and no put. Covers both an explicit nil
// resolver and the production newGBrainSourceWriter when no broker has
// registered a shared client.
func TestGBrainSourceWriter_NilClientLogsAndDrops(t *testing.T) {
	job := SourceCaptureJob{
		Kind:    SourceKindNote,
		Title:   "orphan",
		Origin:  "orphan",
		Content: "no backend\n",
	}

	nilResolver := &gbrainSourceWriter{resolve: func() gbrainPutPager { return nil }}
	if err := nilResolver.WriteSource(context.Background(), job); err != nil {
		t.Fatalf("nil-resolver WriteSource returned error: %v", err)
	}

	// Production writer with no shared client registered: must not panic.
	setSharedGBrainClient(nil)
	prod := newGBrainSourceWriter()
	if err := prod.WriteSource(context.Background(), job); err != nil {
		t.Fatalf("prod writer (no shared client) returned error: %v", err)
	}

	// And the whole drain must survive a nil backend without spinning.
	disp := NewSourceCaptureDispatcher(prod)
	disp.Start(context.Background())
	defer disp.Stop(2 * time.Second)
	if ok := disp.Enqueue(job); !ok {
		t.Fatal("Enqueue returned false")
	}
	// Give the drain a moment to process-and-drop; no assertion beyond no-panic.
	time.Sleep(100 * time.Millisecond)
}

// TestSourceCapture_Feeder1_CompletedTask drives a real RecordTaskDecision
// approval on a temp broker with a fake gbrain client injected, and asserts a
// single task page is upserted carrying the spec, session report, reviewer
// grade, feedback, and deliverable path — plus the office-capture frontmatter.
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

	// Inject a fake gbrain client and start the capture dispatcher against it.
	fake := &fakeGBrainCaptureClient{}
	setSharedGBrainClient(fake)
	t.Cleanup(func() { setSharedGBrainClient(nil) })
	b.startSourceCaptureDispatcher()
	t.Cleanup(func() {
		if disp := b.sourceCaptureDispatcher.Load(); disp != nil {
			disp.Stop(2 * time.Second)
		}
	})

	if err := b.RecordTaskDecision(taskID, string(RecordDecisionApprove), "test-human"); err != nil {
		t.Fatalf("RecordTaskDecision: %v", err)
	}

	wantSlug := DeriveSourceID(SourceKindTask, taskID, "Office activity is never snapshotted", "")
	testTickUntil(t, 5*time.Second, func() bool {
		_, ok := fake.lastFor(wantSlug)
		return ok
	})
	put, ok := fake.lastFor(wantSlug)
	if !ok {
		t.Fatalf("no task page upserted for slug %q; slugs=%v", wantSlug, fake.distinctSlugs())
	}
	if put.opts.SourceKind != gbrainCaptureSourceKindPrefix+string(SourceKindTask) {
		t.Errorf("source_kind = %q, want %q", put.opts.SourceKind, gbrainCaptureSourceKindPrefix+string(SourceKindTask))
	}
	if put.opts.IngestedVia != gbrainCaptureIngestedVia {
		t.Errorf("ingested_via = %q, want %q", put.opts.IngestedVia, gbrainCaptureIngestedVia)
	}
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
		"source: " + gbrainCaptureSource,
	} {
		if !strings.Contains(put.content, want) {
			t.Errorf("task page missing %q\n---\n%s", want, put.content)
		}
	}
}
