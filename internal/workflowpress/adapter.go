package workflowpress

import (
	"context"
	"fmt"
)

// adapter.go is the durable-execution adapter seam. A generated workflow has two
// execution paths that MUST stay behavior-equivalent:
//
//   - the local runner runtime (runner.go), used by shipcheck for fast,
//     in-process fixture replay; and
//   - a durable adapter, which re-hosts the same state machine on a durable
//     workflow engine so a real run survives restarts and retries.
//
// inngest is the intended durable target (durable steps, retries, scheduled
// triggers map cleanly onto a RevOps workflow's external/scheduled events). The
// inngest Go SDK is NOT a dependency of this repo, so this phase ships:
//
//   - Adapter — the interface a real inngest adapter implements; and
//   - LocalAdapter — a behavior-equivalent local runtime that drives the SAME
//     Runner. It is the reference the next phase's "adapter parity" shipcheck
//     asserts the inngest path against.
//
// When the inngest dependency is added, an InngestAdapter implements Adapter by
// registering each spec event as an inngest function/step and delegating the
// per-step transition logic to the same Runner semantics; because both paths run
// the identical deterministic Runner over the identical input, parity is
// structural, not coincidental.

// Adapter re-hosts a generated workflow on a durable execution engine. The
// contract is deliberately narrow and identical to a single Runner.Run so the
// durable path cannot drift from the local path: Run over one input MUST produce
// a RunResult equal to the local Runner's for the same spec and input.
type Adapter interface {
	// Name identifies the adapter backend ("local", "inngest") for audit.
	Name() string
	// Run executes the workflow over one input and returns the same RunResult the
	// local Runner would. Implementations re-host the state machine but MUST NOT
	// change its observable behavior.
	Run(ctx context.Context, in RunInput) (*RunResult, error)
}

// LocalAdapter is the behavior-equivalent local runtime. It wraps a Runner and
// satisfies the Adapter interface by delegating to it directly. It is the parity
// reference: any durable adapter (inngest) is correct iff it produces the same
// RunResult LocalAdapter does for every input. Because LocalAdapter IS the Runner,
// parity for the local path is definitional; the next phase proves the inngest
// path matches it.
type LocalAdapter struct {
	runner *Runner
}

// NewLocalAdapter builds the local durable-runtime stand-in over a spec. It uses
// the fail-closed host executor and the default guard evaluator — the same
// defaults the local Runner uses — so the two paths are identical by construction.
func NewLocalAdapter(spec *WorkflowSpec) (*LocalAdapter, error) {
	r, err := NewRunner(spec, NewHostExecutor(), DefaultGuardEvaluator{})
	if err != nil {
		return nil, fmt.Errorf("workflowpress: building local adapter: %w", err)
	}
	return &LocalAdapter{runner: r}, nil
}

// Name identifies the local adapter.
func (*LocalAdapter) Name() string { return "local" }

// Run delegates to the wrapped Runner; the local path's behavior is the Runner's
// behavior verbatim.
func (a *LocalAdapter) Run(ctx context.Context, in RunInput) (*RunResult, error) {
	return a.runner.Run(ctx, in)
}

// InngestStep is the shape a single durable step takes when the inngest adapter
// is wired up: one event-driven transition, addressable by event name, idempotent
// on a step id derived from the workflow + input. It is declared here (without the
// inngest dependency) so the generated inngest adapter has a stable target shape
// and the parity shipcheck has something concrete to assert against.
//
// The mapping is intentionally 1:1 with the Runner's transition loop: each spec
// Event becomes one durable step; the step's guard/action semantics are exactly
// what Runner.fire does. Re-hosting therefore cannot change behavior — it only
// changes where the steps run and how failures are retried.
type InngestStep struct {
	// FunctionID is the durable function id the step registers under
	// (workflow id + event name), unique and stable per spec.
	FunctionID string
	// Event is the triggering event name the step reacts to.
	Event string
	// From and To are the transition this step performs.
	From string
	To   string
	// Trigger carries the event's trigger kind so the inngest registration can
	// pick the right primitive (a scheduled trigger becomes an inngest cron).
	Trigger EventTrigger
	// Schedule is the cron expression for a scheduled step (empty otherwise).
	Schedule string
}

// PlanInngestSteps derives the durable step plan from a spec. It is pure and
// deterministic: one step per event, in spec order. A real InngestAdapter would
// register these (the scheduled ones as crons, the external/internal ones as
// event functions) and execute each via the same Runner semantics. Shipping the
// planner now lets the generator emit an inngest adapter file whose step plan
// provably matches the local runner's transition set.
func PlanInngestSteps(spec *WorkflowSpec) []InngestStep {
	steps := make([]InngestStep, 0, len(spec.Events))
	for _, ev := range spec.Events {
		steps = append(steps, InngestStep{
			FunctionID: spec.ID + "." + ev.Name,
			Event:      ev.Name,
			From:       ev.From,
			To:         ev.To,
			Trigger:    ev.Trigger,
			Schedule:   ev.Schedule,
		})
	}
	return steps
}
