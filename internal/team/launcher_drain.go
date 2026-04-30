package team

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// drainingFlag reports whether Drain has been initiated. Pane dispatch reads
// this via IsDraining to skip enqueueing new notifications.
var drainingFlag atomic.Bool

// IsDraining reports whether any Launcher is currently draining. Pane
// dispatch and notify loops consult this to short-circuit new work after
// /admin/pause has been accepted.
func IsDraining() bool { return drainingFlag.Load() }

// Drain cleanly stops every Launcher dispatch path so the broker can exit
// without leaving in-flight work mid-turn. Called by the broker's
// /admin/pause handler with a 60-second deadline.
//
// Order matters:
//
//  1. Flip the package-level draining flag so new pane-dispatch enqueues
//     short-circuit before queueing more `/clear` cycles.
//  2. Cancel the headless dispatch context so headless workers exit at
//     their next outer-loop tick.
//  3. Stop the watchdog scheduler so its goroutine drains.
//  4. Wait for the headless WaitGroup with the caller's deadline. If the
//     deadline expires, return a descriptive error so the caller can log
//     the timeout — the process will still exit via the admin-pause hook.
//
// Drain does NOT stop the broker, close the HTTP listener, or kill tmux
// panes. That happens after the caller invokes the admin-pause exit hook
// (os.Exit(0) in production), which bypasses tmux teardown intentionally:
// the orchestrator on the other side is the one issuing tmux-kill via
// tmuxKiller after the broker exits.
func (l *Launcher) Drain(ctx context.Context) error {
	if l == nil {
		return nil
	}

	drainingFlag.Store(true)

	l.headless.mu.Lock()
	cancel := l.headless.cancel
	l.headless.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	if l.schedulerWorker != nil {
		l.schedulerWorker.Stop()
	}

	done := make(chan struct{})
	go func() {
		l.headless.workerWg.Wait()
		close(done)
	}()

	deadline, hasDeadline := ctx.Deadline()
	var timeout time.Duration
	if hasDeadline {
		timeout = time.Until(deadline)
	} else {
		timeout = 60 * time.Second
	}
	if timeout <= 0 {
		timeout = time.Second
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("launcher drain: context cancelled before workers finished: %w", ctx.Err())
	case <-time.After(timeout):
		return fmt.Errorf("launcher drain: headless workers did not finish within %s", timeout)
	}
}
