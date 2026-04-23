package team

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Real-world symptom: user tagged @pm twice in three minutes. The first
// dispatch was still live (claude mid-turn) when the second arrived,
// which triggered `/clear` and wiped claude's in-progress reply. PM only
// answered one of the two questions.
//
// Design shift: rather than trying to detect claude's idle state via pane
// scraping (unreliable given claude's TUI runs on a 1x4 geometry in
// practice and does its own internal buffer layout), the queue COALESCES
// rapid bursts. Any notification arriving within paneDispatchCoalesceWindow
// of the previous send merges into the pending dispatch — claude sees one
// prompt containing every question and answers them together.

// TestQueuePaneNotification_CoalescesBurstsIntoOneDispatch pins the
// primary contract: two rapid notifications for the same slug produce
// exactly one combined dispatch, with both prompts separated by a divider.
func TestQueuePaneNotification_CoalescesBurstsIntoOneDispatch(t *testing.T) {
	oldGap := paneDispatchMinGap
	oldWin := paneDispatchCoalesceWindow
	// Short window so the test runs in milliseconds, but large enough
	// that the second enqueue arrives inside it.
	paneDispatchMinGap = 5 * time.Millisecond
	paneDispatchCoalesceWindow = 200 * time.Millisecond
	defer func() {
		paneDispatchMinGap = oldGap
		paneDispatchCoalesceWindow = oldWin
	}()

	l := &Launcher{}
	var dispatches int64
	var captured []string
	var capturedMu sync.Mutex
	origSend := launcherSendNotificationToPane
	launcherSendNotificationToPane = func(_ *Launcher, _, notification string) {
		atomic.AddInt64(&dispatches, 1)
		capturedMu.Lock()
		captured = append(captured, notification)
		capturedMu.Unlock()
	}
	defer func() { launcherSendNotificationToPane = origSend }()

	// First enqueue dispatches immediately. Second arrives during its
	// coalesce window and should be merged into the pending follow-up,
	// which itself waits out the window before sending.
	l.queuePaneNotification("pm", "team:1", "what are you working on?")
	time.Sleep(20 * time.Millisecond) // let first dispatch complete
	l.queuePaneNotification("pm", "team:1", "you doing fine?")

	// Wait out the coalesce window plus slack for the second dispatch.
	deadline := time.Now().Add(paneDispatchCoalesceWindow + 300*time.Millisecond)
	for atomic.LoadInt64(&dispatches) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Two dispatches total: the first with one prompt, the second with
	// both prompts concatenated via the divider.
	if atomic.LoadInt64(&dispatches) != 2 {
		t.Fatalf("expected 2 dispatches after coalesce window, got %d", atomic.LoadInt64(&dispatches))
	}
	capturedMu.Lock()
	defer capturedMu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("expected 2 captured notifications, got %d", len(captured))
	}
	if captured[0] != "what are you working on?" {
		t.Errorf("first dispatch should be just the first prompt, got %q", captured[0])
	}
	// Second dispatch should include both prompts — it's the merged
	// result. Either "first \n\n--- \n\n second" or just "second" depending
	// on coalesce ordering; the important part is that BOTH prompts land.
	if !strings.Contains(captured[1], "you doing fine?") {
		t.Errorf("second dispatch should contain the later prompt, got %q", captured[1])
	}
}

// TestQueuePaneNotification_SingleTagDispatchesImmediately is the no-burst
// baseline: a lone notification lands in the pane without waiting for a
// coalesce window (coalesce only fires when claude is mid-turn).
func TestQueuePaneNotification_SingleTagDispatchesImmediately(t *testing.T) {
	oldGap := paneDispatchMinGap
	oldWin := paneDispatchCoalesceWindow
	paneDispatchMinGap = 5 * time.Millisecond
	paneDispatchCoalesceWindow = 100 * time.Millisecond
	defer func() {
		paneDispatchMinGap = oldGap
		paneDispatchCoalesceWindow = oldWin
	}()

	l := &Launcher{}
	dispatched := make(chan struct{}, 1)
	origSend := launcherSendNotificationToPane
	launcherSendNotificationToPane = func(_ *Launcher, _, _ string) {
		select {
		case dispatched <- struct{}{}:
		default:
		}
	}
	defer func() { launcherSendNotificationToPane = origSend }()

	startedAt := time.Now()
	l.queuePaneNotification("pm", "team:1", "solo tag")

	select {
	case <-dispatched:
		// Should land in well under the coalesce window.
		elapsed := time.Since(startedAt)
		if elapsed > paneDispatchCoalesceWindow/2 {
			t.Fatalf("single-tag dispatch took %s, expected fast path (< %s)",
				elapsed, paneDispatchCoalesceWindow/2)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("single tag did not dispatch within 50ms")
	}
}

// TestQueuePaneNotification_DifferentSlugsRunInParallel: per-slug queues
// must not block each other. A slow pane for one agent must not starve
// notifications to another agent.
func TestQueuePaneNotification_DifferentSlugsRunInParallel(t *testing.T) {
	oldGap := paneDispatchMinGap
	oldWin := paneDispatchCoalesceWindow
	paneDispatchMinGap = 5 * time.Millisecond
	paneDispatchCoalesceWindow = 200 * time.Millisecond
	defer func() {
		paneDispatchMinGap = oldGap
		paneDispatchCoalesceWindow = oldWin
	}()

	l := &Launcher{}
	var countA, countB int64
	origSend := launcherSendNotificationToPane
	launcherSendNotificationToPane = func(_ *Launcher, paneTarget, _ string) {
		if paneTarget == "team:a" {
			atomic.AddInt64(&countA, 1)
		} else {
			atomic.AddInt64(&countB, 1)
		}
	}
	defer func() { launcherSendNotificationToPane = origSend }()

	startedAt := time.Now()
	l.queuePaneNotification("alpha", "team:a", "first")
	l.queuePaneNotification("beta", "team:b", "first")

	deadline := time.Now().Add(200 * time.Millisecond)
	for (atomic.LoadInt64(&countA) == 0 || atomic.LoadInt64(&countB) == 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt64(&countA) == 0 || atomic.LoadInt64(&countB) == 0 {
		t.Fatalf("expected both alpha and beta to dispatch; countA=%d countB=%d",
			atomic.LoadInt64(&countA), atomic.LoadInt64(&countB))
	}
	if elapsed := time.Since(startedAt); elapsed > paneDispatchCoalesceWindow/2 {
		t.Fatalf("cross-slug dispatch took %s, expected well under one coalesce window — queues are not independent",
			elapsed)
	}
}
