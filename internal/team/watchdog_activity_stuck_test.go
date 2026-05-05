package team

import (
	"testing"
	"time"
)

// drainActivityKind reads from ch until it sees a snapshot for slug with
// Kind == wantKind or the deadline passes. Returns true on match.
func drainActivityKind(ch <-chan agentActivitySnapshot, slug, wantKind string, deadline time.Duration) bool {
	limit := time.After(deadline)
	for {
		select {
		case snap, ok := <-ch:
			if !ok {
				return false
			}
			if snap.Slug == slug && snap.Kind == wantKind {
				return true
			}
		case <-limit:
			return false
		}
	}
}

// activityKindNow reads the in-memory activity map under the broker lock.
// The mutation helpers (markAgentStuckFromWatchdogLocked etc.) write to
// b.activity synchronously before returning, so a post-call lock read is
// the authoritative check without channel timing.
func activityKindNow(b *Broker, slug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.activity[slug].Kind
}

// TestWatchdogActivityStuck_SingleAlertFlipsKindToStuck verifies that
// CreateWatchdogAlert stamps the owner's activity snapshot with Kind="stuck"
// immediately and that the subscription channel receives the event.
func TestWatchdogActivityStuck_SingleAlertFlipsKindToStuck(t *testing.T) {
	b := newTestBroker(t)

	actCh, unsub := b.SubscribeActivity(16)
	defer unsub()

	const owner = "eng"
	_, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", "task-1", owner, "Waiting for unblock")
	if err != nil {
		t.Fatalf("CreateWatchdogAlert: %v", err)
	}

	// In-memory map must be updated synchronously.
	if got := activityKindNow(b, owner); got != "stuck" {
		t.Errorf("activity Kind after alert = %q, want stuck", got)
	}

	// The publish must arrive on the subscription channel.
	if !drainActivityKind(actCh, owner, "stuck", 2*time.Second) {
		t.Error("did not receive Kind=stuck on activity channel within 2s")
	}
}

// TestWatchdogActivityStuck_ResolveLastAlertFlipsKindToRoutine verifies that
// resolving the only active watchdog on an owner restores Kind="routine".
func TestWatchdogActivityStuck_ResolveLastAlertFlipsKindToRoutine(t *testing.T) {
	b := newTestBroker(t)

	actCh, unsub := b.SubscribeActivity(16)
	defer unsub()

	const owner = "fe"
	_, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", "task-2", owner, "Waiting")
	if err != nil {
		t.Fatalf("CreateWatchdogAlert: %v", err)
	}

	// Drain the "stuck" event so the channel is fresh for the "routine" one.
	if !drainActivityKind(actCh, owner, "stuck", 2*time.Second) {
		t.Fatal("did not receive Kind=stuck before resolving")
	}

	// Resolve the only alert — must flip back to routine.
	b.mu.Lock()
	b.resolveWatchdogAlertsLocked("task", "task-2", "general")
	b.mu.Unlock()

	if got := activityKindNow(b, owner); got != "routine" {
		t.Errorf("activity Kind after resolve = %q, want routine", got)
	}

	if !drainActivityKind(actCh, owner, "routine", 2*time.Second) {
		t.Error("did not receive Kind=routine on activity channel within 2s")
	}
}

// TestWatchdogActivityStuck_TwoAlertsResolveOneKindStaysStuck pins the
// concurrent-guard fix: if two watchdogs are active on the same owner and
// only one is resolved, Kind must remain "stuck" because the second alert
// is still active. This is the CodeRabbit fix shipped in PR #664.
func TestWatchdogActivityStuck_TwoAlertsResolveOneKindStaysStuck(t *testing.T) {
	b := newTestBroker(t)

	actCh, unsub := b.SubscribeActivity(32)
	defer unsub()

	const owner = "be"

	// First alert: task-3
	_, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", "task-3", owner, "Waiting on task-3")
	if err != nil {
		t.Fatalf("CreateWatchdogAlert task-3: %v", err)
	}
	// Second alert: task-4 (different targetID, same owner)
	_, _, err = b.CreateWatchdogAlert("task_stalled", "general", "task", "task-4", owner, "Waiting on task-4")
	if err != nil {
		t.Fatalf("CreateWatchdogAlert task-4: %v", err)
	}

	// Both alerts are up; Kind must be stuck.
	if got := activityKindNow(b, owner); got != "stuck" {
		t.Fatalf("expected stuck before resolve, got %q", got)
	}

	// Drain any queued events so the channel is settled.
	drainActivityKind(actCh, owner, "stuck", 500*time.Millisecond)

	// Resolve only the first alert. The second is still active.
	b.mu.Lock()
	b.resolveWatchdogAlertsLocked("task", "task-3", "general")
	b.mu.Unlock()

	// Kind must remain "stuck" because task-4's alert is still active.
	if got := activityKindNow(b, owner); got != "stuck" {
		t.Errorf("activity Kind after partial resolve = %q, want still stuck", got)
	}

	// Sanity: the second watchdog is genuinely still active.
	dogs := b.Watchdogs()
	activeCount := 0
	for _, w := range dogs {
		if w.Owner == owner && w.Status != "resolved" {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("expected 1 active watchdog remaining after partial resolve, got %d", activeCount)
	}

	// Now resolve the second alert — only now should Kind drop to routine.
	b.mu.Lock()
	b.resolveWatchdogAlertsLocked("task", "task-4", "general")
	b.mu.Unlock()

	if got := activityKindNow(b, owner); got != "routine" {
		t.Errorf("activity Kind after full resolve = %q, want routine", got)
	}

	if !drainActivityKind(actCh, owner, "routine", 2*time.Second) {
		t.Error("did not receive Kind=routine after resolving all alerts")
	}
}

// TestWatchdogActivityStuck_NoCollisionWithStalenessReaper confirms the
// test-path isolation requirement from the PR spec: the watchdog mutation
// path must not interfere with the stale-while-active reaper in
// broker_streams.go. We verify this by checking that a directly-created
// watchdog alert (no running broker HTTP server, no reaper goroutine) still
// sets Kind="stuck" without waiting for the 90s stale timeout.
func TestWatchdogActivityStuck_NoCollisionWithStalenessReaper(t *testing.T) {
	// newTestBroker does NOT start the broker's background goroutines
	// (that requires StartOnPort), so no reaper is running. Any Kind="stuck"
	// result must come exclusively from the watchdog path.
	b := newTestBroker(t)

	const owner = "data"
	_, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", "task-5", owner, "Direct watchdog test")
	if err != nil {
		t.Fatalf("CreateWatchdogAlert: %v", err)
	}

	if got := activityKindNow(b, owner); got != "stuck" {
		t.Errorf("expected stuck from watchdog path only (no reaper), got %q", got)
	}
}
