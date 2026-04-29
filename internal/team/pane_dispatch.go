package team

// pane_dispatch.go owns the per-slug pane-dispatch worker that serializes
// notifications into live tmux Claude panes (PLAN.md §C6). Second
// goroutine extraction in the launcher decomposition; reuses the clock
// interface introduced in C4 (scheduler.go) so the two timing gates
// (paneDispatchMinGap, paneDispatchCoalesceWindow) are deterministic
// in tests.
//
// PLAN.md trap §5.3: the launcherSendNotificationToPaneOverride
// atomic.Pointer is package-global and stays in launcher.go. Existing
// tests (pane_dispatch_queue_test.go, resume_test.go) read that override
// directly; moving it would break them. The dispatcher takes its send
// function as a constructor arg, and the Launcher wires it to a closure
// that consults the override on every call.
//
// PLAN.md §3 trap on coalesce-window vars: paneDispatchMinGap and
// paneDispatchCoalesceWindow remain package-globals (not fields) because
// existing tests mutate them at the package level. Reading per-call
// rather than caching at construction lets those tests keep working
// while still allowing per-instance overrides via direct field set in
// the future.

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// paneDispatchTurn is one queued notification to type into a tmux pane.
// Held in the per-slug queue, consumed by the dispatcher worker.
type paneDispatchTurn struct {
	PaneTarget   string
	Notification string
	EnqueuedAt   time.Time
}

// paneDispatchMinGap caps how often the dispatcher will type into the
// same pane. Two messages arriving in quick succession get coalesced
// into one /clear + send so claude doesn't see truncated input. The
// default is 3s — generous enough to absorb the typical "agent
// responded then human posted" double-tag without losing either.
var paneDispatchMinGap = 3 * time.Second

// paneDispatchCoalesceWindow: if a new notification arrives this soon
// after the previous, the queued sends merge into a single Enter+text
// burst. 60s lets a multi-step claude turn finish before the next
// /clear fires for a fresh prompt — without it, the dispatcher would
// step on a still-running turn and produce truncated tool output.
//
// Both knobs are package-level vars (not constants) so tests can
// override them in-process; see pane_dispatch_queue_test.go for the
// pattern.
var paneDispatchCoalesceWindow = 60 * time.Second

// launcherSendNotificationToPaneFn is the test seam type swapped via
// setLauncherSendNotificationToPaneForTest. Production calls
// launcherSendNotificationToPane directly; tests intercept by
// installing a fake closure that records or no-ops the send. Kept as
// a named type so the atomic.Pointer below stays readable.
type launcherSendNotificationToPaneFn func(l *Launcher, paneTarget, notification string)

// launcherSendNotificationToPaneOverride is read by the pane-dispatch
// and pane-priming code paths. Tests must never assign directly; use
// setLauncherSendNotificationToPaneForTest in test_support.go which
// nests t.Cleanup correctly so concurrent tests don't corrupt each
// other's overrides.
var launcherSendNotificationToPaneOverride atomic.Pointer[launcherSendNotificationToPaneFn]

// launcherSendNotificationToPane is the default production send path.
// The dispatcher's sendFn closure consults the override on every call
// so tests can intercept without owning the dispatcher. Production
// delegates to (l *Launcher).sendNotificationToPane (defined in
// launcher.go) so the actual /clear + send-keys sequence stays
// alongside the rest of the Launcher's tmux helpers.
func launcherSendNotificationToPane(l *Launcher, paneTarget, notification string) {
	if p := launcherSendNotificationToPaneOverride.Load(); p != nil {
		(*p)(l, paneTarget, notification)
		return
	}
	l.sendNotificationToPane(paneTarget, notification)
}

// paneDispatcher serializes notifications per agent slug into tmux Claude
// panes. One goroutine per slug drains its queue with a min-gap floor
// against tmux input bursts plus a coalesce window that lets Claude's
// in-flight turn finish before /clear fires for the next prompt. The
// goroutine exits when its queue is empty; a fresh enqueue spawns a new
// one (atomic handoff inside the per-dispatcher mutex).
type paneDispatcher struct {
	clock clock

	mu       sync.Mutex
	queues   map[string][]paneDispatchTurn
	workers  map[string]bool
	lastSent map[string]time.Time

	// sendFn is the actual /clear + type + Enter sequence. The Launcher
	// wires this to a closure that consults launcherSendNotificationToPaneOverride
	// on every call (preserving the existing test seam without moving it).
	sendFn func(paneTarget, notification string)

	// onSent, when non-nil, receives one struct after every successful
	// send. Tests use it to wait deterministically. Production leaves
	// it nil so the worker has zero overhead.
	onSent chan<- struct{}
}

// Enqueue queues a notification for pane-backed agent `slug` and ensures
// there is one worker draining its queue. Rapid successive tags for the
// same slug coalesce into a single dispatch when they arrive inside
// paneDispatchCoalesceWindow — the defence against mid-turn /clear
// wiping Claude's in-progress output.
func (d *paneDispatcher) Enqueue(slug, paneTarget, notification string) {
	slug = strings.TrimSpace(slug)
	paneTarget = strings.TrimSpace(paneTarget)
	if slug == "" || paneTarget == "" || notification == "" {
		return
	}
	d.mu.Lock()
	if d.queues == nil {
		d.queues = make(map[string][]paneDispatchTurn)
	}
	if d.workers == nil {
		d.workers = make(map[string]bool)
	}
	if d.lastSent == nil {
		d.lastSent = make(map[string]time.Time)
	}
	now := d.now()
	inflight := false
	if last, ok := d.lastSent[slug]; ok && now.Sub(last) < paneDispatchCoalesceWindow {
		inflight = true
	}
	queue := d.queues[slug]
	if (inflight || len(queue) > 0) && len(queue) > 0 {
		// Combine with the pending turn. Claude will see both prompts
		// separated by a visible divider and typically answers both.
		last := &d.queues[slug][len(queue)-1]
		last.Notification = last.Notification + "\n\n---\n\n" + notification
		last.EnqueuedAt = now
		d.mu.Unlock()
		return
	}
	if inflight && len(queue) == 0 {
		// No pending turn yet but Claude is mid-flight from a recent
		// send. Create a single pending turn that absorbs further bursts
		// through the branch above.
		d.queues[slug] = []paneDispatchTurn{{
			PaneTarget:   paneTarget,
			Notification: notification,
			EnqueuedAt:   now,
		}}
		startWorker := !d.workers[slug]
		if startWorker {
			d.workers[slug] = true
		}
		d.mu.Unlock()
		if startWorker {
			go d.runQueue(slug)
		}
		return
	}
	// Cold path: no recent activity, no queue. Dispatch immediately.
	d.queues[slug] = append(d.queues[slug], paneDispatchTurn{
		PaneTarget:   paneTarget,
		Notification: notification,
		EnqueuedAt:   now,
	})
	startWorker := !d.workers[slug]
	if startWorker {
		d.workers[slug] = true
	}
	d.mu.Unlock()
	if startWorker {
		go d.runQueue(slug)
	}
}

// runQueue is the per-slug worker. Drains the queue serially with a min-
// gap floor against sub-second bursts plus a coalesce window that lets
// Claude's current turn land before /clear fires. Exits when the queue
// is empty (atomic handoff via worker flag clear under mu).
func (d *paneDispatcher) runQueue(slug string) {
	var lastSentAt time.Time
	for {
		// Step 1: peek (not pop).
		d.mu.Lock()
		queue := d.queues[slug]
		if len(queue) == 0 {
			delete(d.workers, slug)
			delete(d.queues, slug)
			d.mu.Unlock()
			return
		}
		globalLastSentAt := d.lastSent[slug]
		d.mu.Unlock()

		// Step 2a: min-gap floor against sub-second bursts.
		if !lastSentAt.IsZero() {
			wait := paneDispatchMinGap - d.now().Sub(lastSentAt)
			if wait > 0 {
				<-d.clock.After(wait)
			}
		}
		// Step 2b: coalesce window — let Claude's in-flight turn land
		// before /clear fires. Concurrent Enqueue calls may merge new
		// content into the head during this wait.
		if !globalLastSentAt.IsZero() {
			wait := paneDispatchCoalesceWindow - d.now().Sub(globalLastSentAt)
			if wait > 0 {
				<-d.clock.After(wait)
			}
		}

		// Step 3: pop (re-read head to pick up any merged content).
		d.mu.Lock()
		queue = d.queues[slug]
		if len(queue) == 0 {
			d.mu.Unlock()
			continue
		}
		turn := queue[0]
		if len(queue) == 1 {
			delete(d.queues, slug)
		} else {
			d.queues[slug] = queue[1:]
		}
		d.mu.Unlock()

		if d.sendFn != nil {
			d.sendFn(turn.PaneTarget, turn.Notification)
		}

		// Step 4: record send time for the next enqueue's coalesce check.
		lastSentAt = d.now()
		d.mu.Lock()
		d.lastSent[slug] = lastSentAt
		d.mu.Unlock()

		if d.onSent != nil {
			select {
			case d.onSent <- struct{}{}:
			default:
			}
		}
	}
}

// now returns the current time via the dispatcher's clock, falling back
// to the real clock when no clock is wired (defensive, mirrors the rest
// of the package).
func (d *paneDispatcher) now() time.Time {
	if d.clock == nil {
		return time.Now()
	}
	return d.clock.Now()
}
