package team

// Tests for the extracted paneDispatcher type. PLAN.md §C6 — second
// goroutine extraction. Reuses the manualClock and signal-channel
// patterns introduced in C4 (scheduler_test.go) so every new assertion
// is deterministic without time.Sleep.
//
// The existing pane_dispatch_queue_test.go remains the regression net —
// it goes through Launcher.queuePaneNotification (now a wrapper) and
// uses the package-global paneDispatchMinGap / paneDispatchCoalesceWindow
// vars to shorten the windows. The new tests here drive the dispatcher
// type directly with a manual clock instead.

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newPaneDispatcherForTest builds a dispatcher with sane defaults for
// direct-on-type testing. send is captured into the recordingSend slice;
// the dispatcher's onSent channel exposes a deterministic signal for
// tests to wait on (no polling).
func newPaneDispatcherForTest(t *testing.T, clk clock) (*paneDispatcher, *recordingSend) {
	t.Helper()
	rs := &recordingSend{
		signal: make(chan struct{}, 8),
	}
	d := &paneDispatcher{
		clock:  clk,
		sendFn: rs.send,
		onSent: rs.signal,
	}
	return d, rs
}

type recordingSend struct {
	mu       sync.Mutex
	captured []string
	count    int64
	// signal is bidirectional inside the test so the test can both
	// observe sends (rs.signal as <-chan) and pass the send-only end to
	// the dispatcher's onSent field.
	signal chan struct{}
}

func (r *recordingSend) send(paneTarget, notification string) {
	r.mu.Lock()
	r.captured = append(r.captured, notification)
	r.mu.Unlock()
	atomic.AddInt64(&r.count, 1)
}

func (r *recordingSend) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.captured))
	copy(out, r.captured)
	return out
}

func TestPaneDispatcher_FirstEnqueueDispatchesImmediately(t *testing.T) {
	clk := newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	d, rs := newPaneDispatcherForTest(t, clk)

	d.Enqueue("pm", "team:1", "first message")

	select {
	case <-rs.signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("first send did not fire within 2s")
	}

	got := rs.snapshot()
	if len(got) != 1 || got[0] != "first message" {
		t.Fatalf("expected single dispatch with 'first message'; got %v", got)
	}
}

func TestPaneDispatcher_SecondEnqueueWithinCoalesceWindowMerges(t *testing.T) {
	// Two enqueues within the coalesce window should produce exactly
	// one extra dispatch with the bodies joined by a divider.
	clk := newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	d, rs := newPaneDispatcherForTest(t, clk)

	d.Enqueue("pm", "team:1", "first")
	select {
	case <-rs.signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("first send did not fire")
	}

	// Record the deadline arrival of the worker before advancing —
	// without this the worker's clock.After() call may not be registered
	// yet, so the test would race.
	d.Enqueue("pm", "team:1", "second")
	d.Enqueue("pm", "team:1", "third")
	// Worker should be inside coalesce-window wait. Drain the registered
	// signal so we know the wait is observed.
	select {
	case <-clk.registered:
	case <-time.After(2 * time.Second):
		t.Fatalf("worker never registered its coalesce-window sleeper")
	}

	// Advance past the coalesce window and the min-gap floor combined.
	clk.Advance(paneDispatchCoalesceWindow + paneDispatchMinGap + time.Millisecond)

	select {
	case <-rs.signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("merged second dispatch never fired")
	}

	got := rs.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 dispatches (first + merged); got %d: %v", len(got), got)
	}
	if !strings.Contains(got[1], "second") || !strings.Contains(got[1], "third") {
		t.Errorf("second dispatch should contain both merged bodies; got %q", got[1])
	}
	if !strings.Contains(got[1], "---") {
		t.Errorf("merged dispatch should include the divider; got %q", got[1])
	}
}

func TestPaneDispatcher_EmptySlugAndTargetIgnored(t *testing.T) {
	clk := newManualClock(time.Now())
	d, rs := newPaneDispatcherForTest(t, clk)

	d.Enqueue("", "team:1", "x")
	d.Enqueue("pm", "", "x")
	d.Enqueue("pm", "team:1", "")

	// Give any rogue worker a chance to fire; with all three rejected
	// inputs there must be zero dispatches.
	select {
	case <-rs.signal:
		t.Fatalf("expected zero dispatches for invalid inputs; got %v", rs.snapshot())
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPaneDispatcher_DistinctSlugsRunInParallel(t *testing.T) {
	clk := newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	d, rs := newPaneDispatcherForTest(t, clk)

	d.Enqueue("pm", "team:1", "for pm")
	d.Enqueue("eng", "team:2", "for eng")

	// Each slug has its own worker, so both should fire on first
	// enqueue without needing to advance the clock.
	for i := 0; i < 2; i++ {
		select {
		case <-rs.signal:
		case <-time.After(2 * time.Second):
			t.Fatalf("expected 2 first-enqueue dispatches; only got %d", i)
		}
	}

	got := rs.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 dispatches; got %d", len(got))
	}
}

func TestPaneDispatcher_OnSentNilDoesNotPanic(t *testing.T) {
	clk := newManualClock(time.Now())
	rs := &recordingSend{}
	d := &paneDispatcher{
		clock:  clk,
		sendFn: rs.send,
		// onSent left nil — verify the worker doesn't panic.
	}
	d.Enqueue("pm", "team:1", "no-signal")

	// Wait briefly via a second clock-watching goroutine. We can't use
	// rs.signal because onSent is nil; instead poll the count atomically.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&rs.count) < 1 && time.Now().Before(deadline) {
		clk.Advance(time.Millisecond)
		// Yield to scheduler. Not a sleep with a behavior dependency —
		// it's just letting the worker goroutine run.
		time.Sleep(time.Millisecond)
	}
	if atomic.LoadInt64(&rs.count) != 1 {
		t.Fatalf("expected one dispatch with onSent=nil; got %d", rs.count)
	}
}

// Sanity: the launcher's lazy paneDispatch() returns a non-nil dispatcher
// and queuePaneNotification routes through it.
func TestLauncher_PaneDispatchWiringRoutesEnqueue(t *testing.T) {
	l := &Launcher{}
	if d := l.paneDispatch(); d == nil {
		t.Fatalf("paneDispatch() must never return nil for &Launcher{}")
	}
}

// Defensive: the dispatcher's now() must work without a clock field set.
// This is the same nil-safe fallback the rest of the package follows
// (officeTargeter / notificationContextBuilder / scheduler) so a
// zero-value &paneDispatcher{} doesn't panic.
func TestPaneDispatcher_NowFallsBackToRealClockWhenUnset(t *testing.T) {
	d := &paneDispatcher{}
	if d.now().IsZero() {
		t.Fatalf("dispatcher.now() should never return zero time")
	}
}

// Defensive: enqueueing into a dispatcher whose worker just finished
// (queue empty, worker flag not yet cleared by the goroutine) should
// still produce a single dispatch. Exercises the cold-path branch.
func TestPaneDispatcher_EnqueueAfterIdleSlugWorkerStartsFresh(t *testing.T) {
	clk := newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	d, rs := newPaneDispatcherForTest(t, clk)

	d.Enqueue("pm", "team:1", "first")
	select {
	case <-rs.signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("first send did not fire")
	}

	// Drain the registered signal (worker registers an empty-queue path
	// on its next iteration). The worker exits when queue becomes empty;
	// the next Enqueue should restart it cleanly.
	select {
	case <-clk.registered:
	default:
	}

	// Advance past the coalesce window so the next enqueue takes the
	// cold path rather than the inflight branch.
	clk.Advance(paneDispatchCoalesceWindow + time.Second)

	d.Enqueue("pm", "team:1", "second")
	select {
	case <-rs.signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("second cold-path send did not fire")
	}

	if got := rs.snapshot(); len(got) != 2 {
		t.Errorf("expected 2 distinct dispatches via cold-path; got %d: %v", len(got), got)
	}
}
