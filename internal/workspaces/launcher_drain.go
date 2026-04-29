package workspaces

import "context"

// Drainer is the interface the broker's Launcher must implement to participate
// in the pause lifecycle. Lane B defines this interface; the broker (Lane C)
// and Launcher (internal/team/launcher.go) wire it up.
//
// Drain cleanly stops every Launcher dispatch path — headless dispatch, pane
// dispatch, scheduler loop, watchdog loop, and notify poll loop — by
// cancelling the shared run context and joining all subsystem goroutines.
// The provided ctx carries the 60-second drain deadline set by the pause
// orchestrator; Drain should respect ctx.Done() and return an error if it
// cannot complete in time.
//
// Contract for implementors:
//   - Cancel the headless dispatch context; give in-flight headless invocations
//     up to their per-agent timeout to finish.
//   - Set the pane-dispatch draining flag to prevent new sends; let in-flight
//     pane sends finish via the existing per-slug worker drain.
//   - Cancel the scheduler, watchdog, and notify-poll contexts.
//   - WaitGroup-join all subsystem goroutines with a deadline derived from ctx.
//   - Return nil on clean exit or a descriptive error on timeout/failure.
type Drainer interface {
	Drain(ctx context.Context) error
}
