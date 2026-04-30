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
	"context"
	"fmt"
	"os"
	"os/exec"
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
	// Attempts tracks how many times the dispatcher has tried to
	// send this turn. Bumped on each failed sendFn; the worker
	// keeps the turn at the head of the queue and retries until
	// paneDispatchMaxAttempts, then drops with a stderr log so
	// the user has a trail even after a permanently-wedged pane.
	Attempts int
}

// paneDispatchMaxAttempts caps per-turn retries against transient
// tmux failures. Below this floor a permanently-wedged pane (kill -9
// of the claude process, dialog stuck open, terminal allocation
// contention) would loop forever and burn CPU; above it we'd hold a
// stale notification in front of fresher work. Three attempts at
// paneDispatchMinGap apart (≈9s total) covers a brief tmux server
// hiccup while still draining within a reasonable window.
const paneDispatchMaxAttempts = 3

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
//
// The legacy void shape is preserved here so existing tests keep
// compiling — fakes that record calls don't need to invent an error
// to return. The dispatcher wraps it into the error-returning
// dispatchSendFn at construction time, treating any test-installed
// override as a successful send.
type launcherSendNotificationToPaneFn func(l *Launcher, paneTarget, notification string)

// launcherSendNotificationToPaneOverride is read by the pane-dispatch
// and pane-priming code paths. Tests must never assign directly; use
// setLauncherSendNotificationToPaneForTest in test_support.go which
// nests t.Cleanup correctly so concurrent tests don't corrupt each
// other's overrides.
var launcherSendNotificationToPaneOverride atomic.Pointer[launcherSendNotificationToPaneFn]

// launcherSendNotificationToPane is the default production send path.
// The dispatcher's sendFn closure consults the override on every call
// so tests can intercept without owning the dispatcher. Returns the
// real send error in production; tests that install an override get a
// nil error back (the override fakes are recording-only and have no
// failure to signal).
func launcherSendNotificationToPane(l *Launcher, paneTarget, notification string) error {
	if p := launcherSendNotificationToPaneOverride.Load(); p != nil {
		(*p)(l, paneTarget, notification)
		return nil
	}
	return l.sendNotificationToPane(paneTarget, notification)
}

// sendNotificationToPane delivers a notification to a persistent
// interactive Claude session in a tmux pane. It sends /clear first so
// each turn starts with a fresh context window — the work packet
// carries all required context, so accumulated history is not needed
// and only causes drift over time. --append-system-prompt is a CLI
// flag and survives /clear intact.
//
// Returns the first non-nil send error (if any). Callers should prefer
// paneDispatch().Enqueue — this function runs /clear + type + Enter
// unconditionally, so rapid direct calls will race each other. The
// dispatcher serializes per-slug, inserts the minimum gap, and uses
// the returned error to suppress the post-send coalesce window when
// the send didn't actually land.
func (l *Launcher) sendNotificationToPane(paneTarget, notification string) error {
	if err := tmuxSendKeys(paneTarget, "/clear", "Enter"); err != nil {
		return err
	}
	if err := tmuxSendKeysLiteral(paneTarget, notification); err != nil {
		return err
	}
	return tmuxSendKeys(paneTarget, "Enter")
}

// tmuxSendKeysTimeout caps any single send-keys invocation. tmux
// itself replies in ~ms, but a stalled pty (claude pane wedged on
// a tool prompt, terminal allocation contention) can hang the
// dispatcher worker indefinitely if the runtime context isn't
// bounded. 3s is well past the p99 of a real send and short enough
// that a stuck pane doesn't queue up a backlog.
const tmuxSendKeysTimeout = 3 * time.Second

// Both helpers log to stderr on failure (the next reader of wuphf
// logs needs the trail — a stale pane target, a dead tmux server, and
// a context-deadline timeout all look identical otherwise) AND return
// the error so the dispatcher can decide whether to update its
// coalesce-window timestamp. Pre-fix the dispatcher always advanced
// d.lastSent regardless of send outcome, gating the next enqueue
// behind a 60s coalesce window for a notification that never landed.

func tmuxSendKeys(paneTarget string, keys ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxSendKeysTimeout)
	defer cancel()
	args := append([]string{"-L", tmuxSocketName, "send-keys", "-t", paneTarget}, keys...)
	if err := exec.CommandContext(ctx, "tmux", args...).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tmux send-keys to %s failed: %v\n", paneTarget, err)
		return err
	}
	return nil
}

func tmuxSendKeysLiteral(paneTarget, payload string) error {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxSendKeysTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "-l", payload,
	).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tmux send-keys -l to %s failed (%d bytes): %v\n", paneTarget, len(payload), err)
		return err
	}
	return nil
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
	// Returns nil on a successful send and the underlying tmux error
	// otherwise; runQueue uses the error to decide whether to update
	// the per-slug coalesce-window timestamp.
	sendFn func(paneTarget, notification string) error

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
	// Short-circuit during /admin/pause drain so new pane /clear cycles
	// are not enqueued after Launcher.Drain has started.
	if IsDraining() {
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
		// Also refresh PaneTarget — if the pane was respawned between
		// the original enqueue and this one, the old target is stale
		// and the merged notification would land in a defunct pane.
		last := &d.queues[slug][len(queue)-1]
		last.PaneTarget = paneTarget
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
				<-d.after(wait)
			}
		}
		// Step 2b: coalesce window — let Claude's in-flight turn land
		// before /clear fires. Concurrent Enqueue calls may merge new
		// content into the head during this wait.
		if !globalLastSentAt.IsZero() {
			wait := paneDispatchCoalesceWindow - d.now().Sub(globalLastSentAt)
			if wait > 0 {
				<-d.after(wait)
			}
		}

		// Step 3: peek (don't pop yet — head stays in place so a
		// failed send can be retried without losing the
		// notification, and concurrent Enqueue calls keep merging
		// fresh content into queue[0] right up until the success
		// branch dequeues). Re-read after the wait windows to pick
		// up that merged content.
		d.mu.Lock()
		queue = d.queues[slug]
		if len(queue) == 0 {
			d.mu.Unlock()
			continue
		}
		turn := queue[0]
		d.mu.Unlock()

		var sendErr error
		if d.sendFn != nil {
			sendErr = d.sendFn(turn.PaneTarget, turn.Notification)
		}

		// Step 4: advance the per-worker rate-limit floor either
		// way so a runaway sendFn that always errors can't burn
		// CPU faster than paneDispatchMinGap.
		lastSentAt = d.now()

		if sendErr == nil {
			// Success path: dequeue the head, advance the global
			// coalesce-window timestamp, and signal onSent. Pre-fix
			// the dispatcher updated d.lastSent regardless of
			// outcome, gating the next enqueue behind a 60s
			// coalesce window for a notification that never
			// reached the pane — a transient tmux glitch became a
			// minute of silent stalling. Onsent is success-only so
			// the test seam doesn't flicker on a retry burst.
			d.mu.Lock()
			queue = d.queues[slug]
			if len(queue) == 1 {
				delete(d.queues, slug)
			} else if len(queue) > 1 {
				d.queues[slug] = queue[1:]
			}
			d.lastSent[slug] = lastSentAt
			d.mu.Unlock()
			if d.onSent != nil {
				select {
				case d.onSent <- struct{}{}:
				default:
				}
			}
			continue
		}

		// Failure path: bump the in-place attempts counter and
		// either retry on the next loop iteration (the head stays
		// in queue, so it's the natural target of the next pop) or
		// drop the turn after exhausting paneDispatchMaxAttempts.
		// The head was never popped, so concurrent Enqueue calls
		// continue to merge fresh content into queue[0] which the
		// retry will pick up. d.lastSent is NOT advanced — a fresh
		// enqueue should not be gated by a 60s coalesce window for
		// a notification that didn't actually land.
		d.mu.Lock()
		queue = d.queues[slug]
		if len(queue) > 0 {
			queue[0].Attempts++
			if queue[0].Attempts >= paneDispatchMaxAttempts {
				attempts := queue[0].Attempts
				if len(queue) == 1 {
					delete(d.queues, slug)
				} else {
					d.queues[slug] = queue[1:]
				}
				d.mu.Unlock()
				fmt.Fprintf(os.Stderr, "pane dispatch: dropping %s notification to %s after %d failed attempts: %v\n",
					slug, turn.PaneTarget, attempts, sendErr)
				continue
			}
		}
		d.mu.Unlock()
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

// after returns a channel that fires after wait, falling back to the
// real clock when no clock is wired. Mirrors now()'s nil-safety so a
// zero-value paneDispatcher (e.g. l == nil receiver) doesn't panic in
// the runQueue sleep path.
func (d *paneDispatcher) after(wait time.Duration) <-chan time.Time {
	if d.clock == nil {
		return time.After(wait)
	}
	return d.clock.After(wait)
}
