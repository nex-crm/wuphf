package workflowpress

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// shipcheck.go is the mechanical-proof gate — the deterministic check run before
// a generated workflow ships OR an overlay is accepted. It proves the generated
// tool honours its frozen contract, by REPLAY, not by inspection: it drives the
// kernel Runner (and the durable LocalAdapter) over the spec's own verification
// fixtures and asserts the observable behaviour the contract promises.
//
// It is INSIDE the kernel. It is pure and deterministic: the same (spec, gen)
// always yields the same Report. It never executes a live side effect — every
// run goes through the fail-closed Executor seam (writes are gated and refused),
// so the proof is about the state machine's behaviour, not real I/O.
//
// The seven checks (the mechanical-proof list from docs/specs/workflow-press.md):
//
//   - fixture-replay      — every VerificationScenario replays and produces its
//                           expected transitions and approval expectation.
//   - transition-coverage — every declared transition (spec Event) is exercised
//                           by at least one scenario replay.
//   - idempotency         — re-running an action does NOT double-apply: an
//                           idempotent action fires the same number of times on a
//                           re-run of the identical input.
//   - duplicate-handling  — a duplicate/redelivered event is absorbed: the re-run
//                           lands in the same final state with the same idempotent
//                           action count.
//   - stale-handling      — a stale / out-of-window event is rejected per the SLA:
//                           a non-firing-guard scenario produces no transition.
//   - audit-completeness   — every action a run fires is recorded with an audit
//                           anchor (a backend), so nothing executes unlogged.
//   - adapter-parity      — the durable LocalAdapter produces a RunResult equal to
//                           the local Runner's for every scenario (no drift between
//                           the two execution paths the generator emits).
//
// Each check returns a ShipcheckFinding{Check, Passed, Detail}. The Report passes
// iff every finding passes. A failing finding BLOCKS the ship (or the overlay
// acceptance) — this is the gate.

// shipcheckCheckOrder is the fixed, deterministic order findings are emitted in,
// so a Report's Findings slice is stable across runs (the gate's output must be
// reproducible).
var shipcheckCheckOrder = []string{
	checkFixtureReplay,
	checkTransitionCoverage,
	checkIdempotency,
	checkDuplicateHandling,
	checkStaleHandling,
	checkAuditCompleteness,
	checkAdapterParity,
}

// Check names — stable identifiers used in ShipcheckFinding.Check and asserted by
// tests, so they are constants rather than inline strings.
const (
	checkFixtureReplay      = "fixture-replay"
	checkTransitionCoverage = "transition-coverage"
	checkIdempotency        = "idempotency"
	checkDuplicateHandling  = "duplicate-handling"
	checkStaleHandling      = "stale-handling"
	checkAuditCompleteness  = "audit-completeness"
	checkAdapterParity      = "adapter-parity"
)

// runnerFactory builds the runtime a check replays a scenario through. It is a
// seam so a test can inject a DEFECTIVE runtime (e.g. one whose idempotency is
// broken) and prove shipcheck CATCHES it; production always uses the real kernel
// Runner via defaultRunnerFactory. The factory must be deterministic.
type runnerFactory func(spec *WorkflowSpec) (runtime, error)

// runtime is the minimal behaviour shipcheck needs from whatever executes the
// state machine: run one input, deterministically. Both the kernel Runner and a
// defective test double satisfy it. Accepting this interface (not *Runner) is what
// lets the negative test seed a defect without touching the kernel.
type runtime interface {
	Run(ctx context.Context, in RunInput) (*RunResult, error)
}

// defaultRunnerFactory builds the real kernel Runner over the fail-closed host
// executor and the default guard evaluator — the exact runtime the generated tool
// ships with, so shipcheck proves the real thing.
func defaultRunnerFactory(spec *WorkflowSpec) (runtime, error) {
	return NewRunner(spec, NewHostExecutor(), DefaultGuardEvaluator{})
}

// ShipcheckOptions configures a Shipcheck run. The zero value is the production
// configuration (real kernel Runner). RunnerFactory is exported only so a test
// can inject a defective runtime to prove the negative path; production code
// leaves it nil.
type ShipcheckOptions struct {
	// RunnerFactory overrides how the replay runtime is built. Nil uses the real
	// kernel Runner (defaultRunnerFactory). A test sets this to inject a defect.
	RunnerFactory runnerFactory
}

// compile-time assertion: MechanicalShipcheck satisfies the kernel's Shipcheck
// seam, so a caller can hold the interface while the gate stays a struct.
var _ Shipcheck = MechanicalShipcheck{}

// MechanicalShipcheck implements the kernel Shipcheck interface as the
// mechanical-proof gate. It holds only the (deterministic) runner factory; it is
// immutable and safe to reuse.
type MechanicalShipcheck struct {
	factory runnerFactory
}

// NewShipcheck returns the mechanical-proof Shipcheck with the production
// configuration: it replays through the real kernel Runner.
func NewShipcheck() Shipcheck {
	return MechanicalShipcheck{factory: defaultRunnerFactory}
}

// newShipcheckWithOptions builds a Shipcheck with test-supplied options. It is
// unexported: production callers use NewShipcheck; only tests reach for the
// defect-injection seam.
func newShipcheckWithOptions(opts ShipcheckOptions) MechanicalShipcheck {
	f := opts.RunnerFactory
	if f == nil {
		f = defaultRunnerFactory
	}
	return MechanicalShipcheck{factory: f}
}

// Check satisfies the kernel Shipcheck interface, running the mechanical proof
// over the (spec, gen) pair. It honours context cancellation, then delegates to
// the pure Shipcheck function.
func (m MechanicalShipcheck) Check(ctx context.Context, spec *WorkflowSpec, gen *GeneratedWorkflow) (*ShipcheckReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("workflowpress: shipcheck context: %w", err)
	}
	return shipcheck(spec, gen, m.factory)
}

// RunShipcheck runs the mechanical-proof gate over a frozen spec and the workflow
// generated from it, with the production runtime (the real kernel Runner). It is
// the package's public function entrypoint for the gate (the kernel's Shipcheck
// is the interface; this is its pure implementation). Callers that want the
// kernel seam use NewShipcheck.
//
// The gate proves the generated tool honours the contract by replaying the
// contract's own fixtures. It returns a Report whose Passed is true iff every one
// of the seven checks passes; a single failing check blocks the ship.
func RunShipcheck(spec *WorkflowSpec, gen *GeneratedWorkflow) (*ShipcheckReport, error) {
	return shipcheck(spec, gen, defaultRunnerFactory)
}

// shipcheck is the deterministic core: validate the inputs, build the replay
// runtime via the factory, run each check, and assemble the Report in fixed check
// order. Every check is pure with respect to (spec, gen).
func shipcheck(spec *WorkflowSpec, gen *GeneratedWorkflow, factory runnerFactory) (*ShipcheckReport, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: %w: spec is nil", ErrInvalidSpec, ErrEmptyField)
	}
	if gen == nil {
		return nil, fmt.Errorf("workflowpress: shipcheck: generated workflow is nil")
	}
	// A workflow generated from a different spec cannot be proven against this
	// contract; the gate must refuse the mismatch rather than silently pass.
	if gen.WorkflowID != spec.ID || gen.Version != spec.Version {
		return nil, fmt.Errorf(
			"workflowpress: shipcheck: generated %s/%d does not match spec %s/%d",
			gen.WorkflowID, gen.Version, spec.ID, spec.Version,
		)
	}
	// The contract must itself be a sound, safe state machine before we can prove a
	// tool honours it. Validate is the structural half of the freeze gate; shipcheck
	// re-checks defensively so an unvalidated spec never reaches replay.
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("workflowpress: shipcheck: spec is invalid: %w", err)
	}

	if factory == nil {
		factory = defaultRunnerFactory
	}
	rt, err := factory(spec)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: shipcheck: building runtime: %w", err)
	}

	checks := map[string]ShipcheckFinding{
		checkFixtureReplay:      checkFixtureReplayFn(spec, rt),
		checkTransitionCoverage: checkTransitionCoverageFn(spec, rt),
		checkIdempotency:        checkIdempotencyFn(spec, rt),
		checkDuplicateHandling:  checkDuplicateHandlingFn(spec, rt),
		checkStaleHandling:      checkStaleHandlingFn(spec, rt),
		checkAuditCompleteness:  checkAuditCompletenessFn(spec, rt),
		checkAdapterParity:      checkAdapterParityFn(spec, rt),
	}

	report := &ShipcheckReport{WorkflowID: spec.ID, Version: spec.Version, Passed: true}
	for _, name := range shipcheckCheckOrder {
		f := checks[name]
		f.Check = name // pin the canonical name regardless of how the fn set it
		report.Findings = append(report.Findings, f)
		if !f.Passed {
			report.Passed = false
		}
	}
	return report, nil
}

// pass / fail build a ShipcheckFinding. The check name is overwritten by the
// caller from the fixed order, so these only carry Passed + Detail.
func pass(detail string) ShipcheckFinding { return ShipcheckFinding{Passed: true, Detail: detail} }
func fail(detail string) ShipcheckFinding { return ShipcheckFinding{Passed: false, Detail: detail} }

// runOne is the deterministic single-replay helper every check shares. It runs the
// runtime over one input and returns the result, surfacing a runtime error as a
// failing finding's detail (a runtime that errors on a contract fixture is a
// defect the gate must catch, not a panic).
func runOne(rt runtime, when string, given map[string]string) (*RunResult, error) {
	return rt.Run(context.Background(), RunInput{Event: when, Fields: given})
}

// --- 1. fixture-replay ---

// checkFixtureReplayFn replays every verification scenario and asserts the runtime
// reproduces its expected transitions and approval expectation. This is the
// contract carrying its own tests: a scenario that does not reproduce is a tool
// that does not honour the contract.
func checkFixtureReplayFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	if len(spec.VerificationScenarios) == 0 {
		return fail("spec carries no verification scenarios to replay")
	}
	for _, sc := range spec.VerificationScenarios {
		res, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: replay errored: %v", sc.Name, err))
		}
		if d, ok := transitionsMatch(res.Transitions, sc.ExpectTransitions); !ok {
			return fail(fmt.Sprintf("scenario %q: %s", sc.Name, d))
		}
		if res.ApprovalRequested != sc.ExpectApproval {
			return fail(fmt.Sprintf(
				"scenario %q: approval requested = %v, want %v",
				sc.Name, res.ApprovalRequested, sc.ExpectApproval,
			))
		}
	}
	return pass(fmt.Sprintf("%d verification scenarios replayed and matched", len(spec.VerificationScenarios)))
}

// transitionsMatch reports whether a recorded transition log equals the expected
// one exactly (order and endpoints), returning a human-readable diff on mismatch.
func transitionsMatch(got []TransitionStep, want []Transition) (string, bool) {
	if len(got) != len(want) {
		return fmt.Sprintf("got %d transitions, want %d", len(got), len(want)), false
	}
	for i, w := range want {
		if got[i].From != w.From || got[i].To != w.To {
			return fmt.Sprintf("transition %d = %s->%s, want %s->%s", i, got[i].From, got[i].To, w.From, w.To), false
		}
	}
	return "", true
}

// --- 2. transition-coverage ---

// checkTransitionCoverageFn proves every declared transition (each spec Event) is
// EXERCISED by at least one scenario replay. An event no fixture ever drives is an
// untested edge of the state machine; the gate refuses to ship an uncovered
// transition.
func checkTransitionCoverageFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	// The set of transitions the contract declares, keyed by event+from+to.
	declared := make(map[string]struct{}, len(spec.Events))
	for _, ev := range spec.Events {
		declared[transitionKey(ev.Name, ev.From, ev.To)] = struct{}{}
	}
	covered := make(map[string]struct{}, len(declared))
	for _, sc := range spec.VerificationScenarios {
		res, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: replay errored: %v", sc.Name, err))
		}
		for _, tr := range res.Transitions {
			covered[transitionKey(tr.Event, tr.From, tr.To)] = struct{}{}
		}
	}
	var missing []string
	for _, ev := range spec.Events {
		if _, ok := covered[transitionKey(ev.Name, ev.From, ev.To)]; !ok {
			missing = append(missing, fmt.Sprintf("%s(%s->%s)", ev.Name, ev.From, ev.To))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fail(fmt.Sprintf("%d/%d transitions uncovered by any scenario: %v", len(missing), len(declared), missing))
	}
	return pass(fmt.Sprintf("all %d declared transitions exercised by scenarios", len(declared)))
}

func transitionKey(event, from, to string) string { return event + "|" + from + "|" + to }

// --- 3. idempotency ---

// checkIdempotencyFn proves re-running an action does NOT double-apply. It asserts
// TWO properties of every idempotent action, each of which a real double-apply
// defect violates:
//
//  1. WITHIN a run, an idempotent action fires AT MOST ONCE. A merge that appends a
//     duplicate apply (the classic broken-merge defect) fires twice in a single
//     run; this catches it regardless of how the runtime behaves across runs — a
//     runner that ALWAYS double-applies (internally consistent across re-runs, so a
//     first-vs-second comparison would miss it) is caught here.
//  2. ACROSS runs, a re-delivery of the same input fires the action the same number
//     of times — the run is stable, so a duplicate delivery cannot accumulate.
//
// The check requires at least one scenario to actually exercise an idempotent
// action, so a declared-but-unexercised idempotent action does not pass vacuously.
func checkIdempotencyFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	idempotent := idempotentActionNames(spec)
	if len(idempotent) == 0 {
		return pass("spec declares no idempotent actions; nothing to prove")
	}
	exercised := 0
	for _, sc := range spec.VerificationScenarios {
		first, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: first replay errored: %v", sc.Name, err))
		}
		// (1) Within-run: each idempotent action fires at most once.
		fires := false
		for name := range idempotent {
			n := countActionCalls(first, name)
			if n > 1 {
				return fail(fmt.Sprintf(
					"scenario %q: idempotent action %q fired %d times in ONE run (double-apply)",
					sc.Name, name, n,
				))
			}
			if n == 1 {
				fires = true
			}
		}
		if !fires {
			continue // no idempotent action fired; this scenario does not constrain the check
		}
		exercised++
		// (2) Across-run stability: a re-delivery fires each action the same number
		// of times (so a duplicate delivery cannot accumulate).
		second, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: re-run errored: %v", sc.Name, err))
		}
		for name := range idempotent {
			a := countActionCalls(first, name)
			b := countActionCalls(second, name)
			if a != b {
				return fail(fmt.Sprintf(
					"scenario %q: idempotent action %q fired %d times then %d on re-run (unstable / double-apply)",
					sc.Name, name, a, b,
				))
			}
		}
	}
	if exercised == 0 {
		return fail(fmt.Sprintf(
			"spec declares %d idempotent action(s) but no scenario exercises one; idempotency is unproven",
			len(idempotent),
		))
	}
	return pass(fmt.Sprintf("%d idempotent action(s) applied at most once and stable across re-runs (%d scenario(s))", len(idempotent), exercised))
}

// idempotentActionNames returns the set of action names the spec marks idempotent.
func idempotentActionNames(spec *WorkflowSpec) map[string]struct{} {
	out := make(map[string]struct{})
	for _, a := range spec.Actions {
		if a.Idempotent {
			out[a.Name] = struct{}{}
		}
	}
	return out
}

// countActionCalls counts recorded calls to the named action in a run.
func countActionCalls(res *RunResult, name string) int {
	n := 0
	for _, a := range res.Actions {
		if a.Name == name {
			n++
		}
	}
	return n
}

// --- 4. duplicate-handling ---

// checkDuplicateHandlingFn proves a duplicate (re-delivered) event is ABSORBED:
// replaying the identical input twice lands in the same final state and fires each
// idempotent action the same number of times, so a duplicate delivery does not
// advance the machine twice or double-create. It is the event-level companion to
// the action-level idempotency check.
func checkDuplicateHandlingFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	if len(spec.VerificationScenarios) == 0 {
		return fail("spec carries no scenarios to test duplicate handling against")
	}
	idempotent := idempotentActionNames(spec)
	for _, sc := range spec.VerificationScenarios {
		first, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: first delivery errored: %v", sc.Name, err))
		}
		dup, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: duplicate delivery errored: %v", sc.Name, err))
		}
		if first.FinalState != dup.FinalState {
			return fail(fmt.Sprintf(
				"scenario %q: duplicate delivery changed final state %q -> %q (not absorbed)",
				sc.Name, first.FinalState, dup.FinalState,
			))
		}
		if d, ok := transitionsMatch(dup.Transitions, transitionStepsToExpected(first.Transitions)); !ok {
			return fail(fmt.Sprintf("scenario %q: duplicate delivery diverged: %s", sc.Name, d))
		}
		for name := range idempotent {
			if countActionCalls(first, name) != countActionCalls(dup, name) {
				return fail(fmt.Sprintf(
					"scenario %q: duplicate delivery re-applied idempotent action %q", sc.Name, name,
				))
			}
		}
	}
	return pass(fmt.Sprintf("duplicate delivery absorbed across %d scenario(s)", len(spec.VerificationScenarios)))
}

// transitionStepsToExpected projects a recorded transition log to the Transition
// shape transitionsMatch compares against (dropping the event name, which the
// determinism of replay already pins).
func transitionStepsToExpected(steps []TransitionStep) []Transition {
	out := make([]Transition, len(steps))
	for i, s := range steps {
		out[i] = Transition{From: s.From, To: s.To}
	}
	return out
}

// --- 5. stale-handling ---

// checkStaleHandlingFn proves a stale / out-of-window event is REJECTED: a
// scenario whose guard does not fire (an expected NO-transition fixture) must
// produce no transition. These are the contract's own "below threshold / too far
// out / stable usage" fixtures — the freshness/window guards a stale record trips.
// A runtime that ADVANCES on such a fixture (a missed guard) fails here.
//
// Not every SLA implies a stale-rejection fixture: a pure latency/completion SLA
// (e.g. "merge within 2m") is about how fast a fired path runs, not about
// rejecting an out-of-window event. So the check proves every negative fixture the
// contract DOES carry is rejected, and additionally requires a negative fixture
// only when the spec declares a FRESHNESS/WINDOW SLA — one whose freshness it can
// only honour by rejecting stale input. A freshness SLA with no negative fixture
// is an unproven rejection promise.
func checkStaleHandlingFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	var negatives []VerificationScenario
	for _, sc := range spec.VerificationScenarios {
		if len(sc.ExpectTransitions) == 0 {
			negatives = append(negatives, sc)
		}
	}
	freshness := countFreshnessSLAs(spec)
	if freshness > 0 && len(negatives) == 0 {
		return fail(fmt.Sprintf(
			"spec declares %d freshness/window SLA(s) but no negative fixture proves a stale/out-of-window event is rejected",
			freshness,
		))
	}
	if len(negatives) == 0 {
		return pass("spec declares no freshness/window SLA and no negative fixture; nothing to prove")
	}
	for _, sc := range negatives {
		res, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("stale scenario %q: replay errored: %v", sc.Name, err))
		}
		if len(res.Transitions) != 0 {
			return fail(fmt.Sprintf(
				"stale scenario %q advanced %d transition(s); a stale/out-of-window event must be rejected",
				sc.Name, len(res.Transitions),
			))
		}
	}
	return pass(fmt.Sprintf("%d stale/out-of-window fixture(s) rejected", len(negatives)))
}

// countFreshnessSLAs counts the SLAs that express a FRESHNESS or WINDOW promise
// (the kind a stale record violates), as distinct from a pure latency/completion
// SLA. Deterministic: it keys off freshness-signalling words in the SLA's metric.
// A freshness SLA can only be honoured by rejecting out-of-window input, so it is
// the SLA class that obliges a negative fixture.
func countFreshnessSLAs(spec *WorkflowSpec) int {
	n := 0
	for _, sla := range spec.SLAs {
		if slaIsFreshness(sla) {
			n++
		}
	}
	return n
}

// freshnessSLAWords are the metric substrings that mark an SLA as a freshness or
// window promise (the data must be recent, the record in-window) rather than a
// latency/completion bound. Lower-cased substring match keeps it deterministic.
var freshnessSLAWords = []string{"fresh", "stale", "age", "older", "window", "within"}

// slaIsFreshness reports whether an SLA expresses a freshness/window promise.
func slaIsFreshness(sla SLA) bool {
	metric := strings.ToLower(sla.Name + " " + sla.Metric)
	for _, w := range freshnessSLAWords {
		if strings.Contains(metric, w) {
			return true
		}
	}
	return false
}

// --- 6. audit-completeness ---

// checkAuditCompletenessFn proves every action a run fires leaves an audit trail:
// each recorded ActionCall carries a backend (the executor that handled or refused
// it) and a gated write is recorded as gated. An action that runs without an audit
// anchor is an unlogged side effect — the gate refuses it. It also asserts that
// across the scenarios at least one action was actually recorded, so the check is
// not vacuously satisfied by a corpus that fires nothing.
func checkAuditCompletenessFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	recorded := 0
	for _, sc := range spec.VerificationScenarios {
		res, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: replay errored: %v", sc.Name, err))
		}
		for _, call := range res.Actions {
			recorded++
			if call.Name == "" {
				return fail(fmt.Sprintf("scenario %q: an action call was recorded with no name", sc.Name))
			}
			if call.Backend == "" {
				return fail(fmt.Sprintf(
					"scenario %q: action %q recorded with no backend (no audit anchor)", sc.Name, call.Name,
				))
			}
			// A write that requires approval must be recorded as gated, so the audit
			// trail proves the approval boundary fired rather than being bypassed.
			if a, ok := actionByName(spec, call.Name); ok && a.Kind.IsWrite() && a.RequiresApproval && !call.Gated {
				return fail(fmt.Sprintf(
					"scenario %q: write action %q requires approval but was recorded un-gated", sc.Name, call.Name,
				))
			}
		}
	}
	if recorded == 0 {
		return fail("no action calls were recorded across any scenario; audit trail is empty")
	}
	return pass(fmt.Sprintf("%d action call(s) recorded, each with an audit anchor", recorded))
}

// actionByName looks up an action in the spec by name.
func actionByName(spec *WorkflowSpec, name string) (Action, bool) {
	for _, a := range spec.Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

// --- 7. adapter-parity ---

// checkAdapterParityFn proves the durable adapter path does not drift from the
// local runner path: the LocalAdapter (the parity reference the inngest adapter
// must match) must produce a RunResult equal to the runtime's for every scenario.
// If the two execution paths the generator emits ever diverge, a workflow could
// behave one way in a fast in-process replay and another on the durable substrate;
// the gate forbids that drift.
//
// Note: the adapter always uses the real kernel Runner (NewLocalAdapter), so when
// a test injects a DEFECTIVE runtime via the factory, this check compares the
// real adapter against the defect — which is exactly the parity violation it
// should report. The idempotency check is the one designed to localise the defect;
// parity corroborates that the defect makes the two paths disagree.
func checkAdapterParityFn(spec *WorkflowSpec, rt runtime) ShipcheckFinding {
	adapter, err := NewLocalAdapter(spec)
	if err != nil {
		return fail(fmt.Sprintf("building local adapter: %v", err))
	}
	for _, sc := range spec.VerificationScenarios {
		viaRuntime, err := runOne(rt, sc.When, sc.Given)
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: runtime replay errored: %v", sc.Name, err))
		}
		viaAdapter, err := adapter.Run(context.Background(), RunInput{Event: sc.When, Fields: sc.Given})
		if err != nil {
			return fail(fmt.Sprintf("scenario %q: adapter replay errored: %v", sc.Name, err))
		}
		if d, ok := resultsEqual(viaRuntime, viaAdapter); !ok {
			return fail(fmt.Sprintf("scenario %q: %s", sc.Name, d))
		}
	}
	return pass(fmt.Sprintf("durable adapter matched the runner across %d scenario(s)", len(spec.VerificationScenarios)))
}

// resultsEqual reports whether two RunResults are behaviorally identical (final
// state, transition log, approval flag, and recorded action calls). It returns a
// diff string on the first divergence. Used by the parity check to assert the two
// execution paths agree.
func resultsEqual(a, b *RunResult) (string, bool) {
	if a.FinalState != b.FinalState {
		return fmt.Sprintf("final state %q != %q", a.FinalState, b.FinalState), false
	}
	if a.ApprovalRequested != b.ApprovalRequested {
		return fmt.Sprintf("approval %v != %v", a.ApprovalRequested, b.ApprovalRequested), false
	}
	if d, ok := transitionsMatch(a.Transitions, transitionStepsToExpected(b.Transitions)); !ok {
		return d, false
	}
	if len(a.Actions) != len(b.Actions) {
		return fmt.Sprintf("recorded %d action calls != %d", len(a.Actions), len(b.Actions)), false
	}
	for i := range a.Actions {
		if a.Actions[i] != b.Actions[i] {
			return fmt.Sprintf("action call %d differs: %+v != %+v", i, a.Actions[i], b.Actions[i]), false
		}
	}
	return "", true
}
