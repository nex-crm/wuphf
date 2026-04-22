package team

import (
	"sync/atomic"
	"testing"
	"time"
)

// Real-world symptom: user tagged @pm twice in three minutes. claude's
// TUI received both `/clear` + type sequences, but the second clear
// arrived before the first turn had returned prompt-ready, so one
// notification was wiped and PM only answered one of the two questions.
//
// Regression shape: rapid queuePaneNotification calls for the same slug
// must NOT race into the tmux pane — the queue drains serially with a
// minimum gap between `/clear` cycles.

func TestQueuePaneNotification_SerializesPerSlug(t *testing.T) {
	// Stub tmux: every exec.Command("tmux", ...) is a no-op that simply
	// records call time. We only care about the ORDER and spacing, not
	// that tmux actually ran.
	//
	// exec.Command itself is too low-level to mock cheanly; instead
	// shorten the dispatch gap and observe that the second turn's
	// sendNotificationToPane starts AFTER the first's. Since sends are
	// synchronous inside the worker, sequential-queue ordering is
	// enough to prove the serialization.
	oldGap := paneDispatchMinGap
	paneDispatchMinGap = 20 * time.Millisecond
	defer func() { paneDispatchMinGap = oldGap }()

	l := &Launcher{}
	var order int64
	firstAt := int64(0)
	secondAt := int64(0)

	// Replace sendNotificationToPane via a test hook. Simplest: intercept
	// by substituting a recorder that bumps a counter and captures the
	// nanosecond timestamp for each call.
	origSend := launcherSendNotificationToPane
	launcherSendNotificationToPane = func(_ *Launcher, paneTarget, _ string) {
		n := atomic.AddInt64(&order, 1)
		ts := time.Now().UnixNano()
		switch n {
		case 1:
			atomic.StoreInt64(&firstAt, ts)
		case 2:
			atomic.StoreInt64(&secondAt, ts)
		}
		_ = paneTarget
	}
	defer func() { launcherSendNotificationToPane = origSend }()

	// Enqueue two notifications nearly simultaneously for the same slug.
	l.queuePaneNotification("pm", "team:1", "first prompt")
	l.queuePaneNotification("pm", "team:1", "second prompt")

	// Wait up to 500ms for both to be processed.
	deadline := time.Now().Add(500 * time.Millisecond)
	for atomic.LoadInt64(&order) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt64(&order) != 2 {
		t.Fatalf("expected 2 dispatches, got %d", atomic.LoadInt64(&order))
	}
	elapsed := atomic.LoadInt64(&secondAt) - atomic.LoadInt64(&firstAt)
	minNs := int64(paneDispatchMinGap) - int64(2*time.Millisecond) // 2ms slack
	if elapsed < minNs {
		t.Fatalf("expected second dispatch at least %s after first, got %s",
			paneDispatchMinGap, time.Duration(elapsed))
	}
}

func TestQueuePaneNotification_DifferentSlugsRunInParallel(t *testing.T) {
	// Per-slug queues: two different agents should NOT block each other
	// on the minimum-gap clock. Otherwise a slow pane for one agent
	// would starve notifications to others.
	oldGap := paneDispatchMinGap
	paneDispatchMinGap = 100 * time.Millisecond
	defer func() { paneDispatchMinGap = oldGap }()

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

	// Wait for both first dispatches.
	deadline := time.Now().Add(250 * time.Millisecond)
	for (atomic.LoadInt64(&countA) == 0 || atomic.LoadInt64(&countB) == 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt64(&countA) == 0 || atomic.LoadInt64(&countB) == 0 {
		t.Fatalf("expected both alpha and beta to dispatch; countA=%d countB=%d",
			atomic.LoadInt64(&countA), atomic.LoadInt64(&countB))
	}
	// Both should land well before the min-gap that applies within a
	// single slug — that gap is not a cross-slug fence.
	if elapsed := time.Since(startedAt); elapsed > paneDispatchMinGap {
		t.Fatalf("cross-slug dispatch took %s, expected <%s — queues are not independent",
			elapsed, paneDispatchMinGap)
	}
}
