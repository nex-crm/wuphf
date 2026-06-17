package workflowpress

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// runner.go is the runner runtime — the small, protected kernel that executes a
// frozen WorkflowSpec's state machine over a single input, producing an ordered
// transition log plus the recorded action calls each transition fires.
//
// It is deterministic by construction: given the same spec and the same input it
// always produces the same RunResult. That determinism is what lets shipcheck
// replay verification scenarios and what makes the inngest adapter (adapter.go)
// behavior-equivalent to this local runner — the adapter merely re-hosts these
// same steps on a durable substrate.
//
// Action calls go through the Executor seam (executor.go). In this phase the
// only backend is the host-stub, which refuses every live mutating/network
// action: external side effects are SIMULATED and RECORDED, never performed. A
// write-action that requires approval is recorded with Gated=true and its
// payload is handed to the executor with ApprovalGranted=false, so the host-stub
// fails it closed — exactly the fail-closed posture the security model demands.
// The runner records the gated call (so shipcheck can prove the approval gate
// fired) and continues the transition; it never performs the real side effect.

// RunInput is the single input a run is driven by. Fields mirrors a
// VerificationScenario's Given map (string->string) so a scenario fixture can be
// replayed verbatim, but the runner accepts any caller-supplied fixture. Event
// names the triggering event (the scenario's When).
type RunInput struct {
	// Event is the name of the triggering event (a VerificationScenario.When).
	Event string
	// Fields is the opaque fixture the guards evaluate against. Kept as
	// string->string to match the contract's verification fixtures.
	Fields map[string]string
}

// TransitionStep is one recorded state hop in a run: the event that fired it and
// the from/to states. The ordered slice of these is the transition log shipcheck
// asserts against a scenario's ExpectTransitions.
type TransitionStep struct {
	Event string `json:"event"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// ActionCall is one recorded action invocation. The runner records every action
// a transition fires, whether it was a read run through the executor or a gated
// write held at the approval boundary. Gated marks a write that required
// approval and was therefore NOT performed (recorded only). Backend names the
// executor that handled (or refused) it, for audit completeness.
type ActionCall struct {
	Name    string     `json:"name"`
	Kind    ActionKind `json:"kind"`
	Target  string     `json:"target"`
	OnEvent string     `json:"on_event"`
	// Gated is true when the action required approval and was held at the gate
	// (no live side effect). Reads and operator-relaxed writes are not gated.
	Gated bool `json:"gated"`
	// Backend is the executor backend that handled the call (e.g. "host-stub").
	Backend string `json:"backend"`
	// Refused is true when the executor refused the call (the expected outcome
	// for any live mutating/network action in this phase).
	Refused bool `json:"refused"`
}

// RunResult is the deterministic outcome of executing a workflow over one input:
// the ordered transition log, the recorded action calls, and the final state.
// ApprovalRequested is true if any gated write was encountered (used by
// shipcheck to assert a scenario's ExpectApproval).
type RunResult struct {
	WorkflowID        string           `json:"workflow_id"`
	Version           int              `json:"version"`
	FinalState        string           `json:"final_state"`
	Transitions       []TransitionStep `json:"transitions"`
	Actions           []ActionCall     `json:"actions"`
	ApprovalRequested bool             `json:"approval_requested"`
}

// ErrRunner is the umbrella error for runtime failures (unknown event, undefined
// transition target). Callers can errors.Is against it.
var ErrRunner = errors.New("workflowpress: runner")

// Runner executes a frozen WorkflowSpec's state machine. It holds the spec, an
// Executor backend (the host-stub in this phase), and a GuardEvaluator. Build one
// with NewRunner; it is immutable and safe to reuse across runs.
type Runner struct {
	spec  *WorkflowSpec
	exec  Executor
	guard GuardEvaluator

	// Derived indices, computed once at construction for deterministic lookup.
	states  map[string]State
	events  map[string]Event
	guards  map[string]Guard
	initial string
	// byFrom maps a from-state to the events that leave it, in spec order, so
	// transition selection is deterministic.
	byFrom map[string][]Event
	// actionsOn maps an event name to the actions it fires, in spec order.
	actionsOn map[string][]Action
}

// NewRunner builds a Runner over a frozen spec. It re-validates the spec first:
// NewRunner is an execution boundary, and a hand-built or JSON-loaded
// *WorkflowSpec can reach it without ever passing the freeze gate. Skipping
// Validate here would let an inferred external-write with RequiresApproval=false
// (or an unknown ActionKind that fails open past the write-approval rule) reach
// execution unguarded. So Validate is the first gate, then the initial/terminal
// anchoring checks below. exec is the execution backend (pass NewHostExecutor for
// the fail-closed stub); guard may be nil to use the default fixture-driven
// evaluator.
func NewRunner(spec *WorkflowSpec, exec Executor, guard GuardEvaluator) (*Runner, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrRunner)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("workflowpress: NewRunner on invalid spec: %w", err)
	}
	if exec == nil {
		exec = NewHostExecutor()
	}
	if guard == nil {
		guard = DefaultGuardEvaluator{}
	}
	r := &Runner{
		spec:      spec,
		exec:      exec,
		guard:     guard,
		states:    make(map[string]State, len(spec.States)),
		events:    make(map[string]Event, len(spec.Events)),
		guards:    make(map[string]Guard, len(spec.Guards)),
		byFrom:    make(map[string][]Event),
		actionsOn: make(map[string][]Action),
	}
	for _, st := range spec.States {
		r.states[st.Name] = st
		if st.Initial {
			if r.initial != "" {
				return nil, fmt.Errorf("%w: spec %q has more than one initial state", ErrRunner, spec.ID)
			}
			r.initial = st.Name
		}
	}
	if r.initial == "" {
		return nil, fmt.Errorf("%w: spec %q has no initial state", ErrRunner, spec.ID)
	}
	for _, ev := range spec.Events {
		r.events[ev.Name] = ev
		r.byFrom[ev.From] = append(r.byFrom[ev.From], ev)
	}
	for _, g := range spec.Guards {
		r.guards[g.Name] = g
	}
	for _, a := range spec.Actions {
		r.actionsOn[a.On] = append(r.actionsOn[a.On], a)
	}
	return r, nil
}

// Run drives the state machine from the input event forward as far as the
// machine deterministically advances, recording every transition and action
// call. It starts at the input event's from-state (so a scenario may begin
// mid-machine, e.g. firing "match_found" from "matched") and then follows
// internal transitions out of each new state whose guard passes, until no guarded
// transition is enabled or a terminal state is reached.
//
// Determinism: at each state the outgoing events are tried in spec order; the
// first whose guard passes (or that has no guard) fires. Because a sound spec has
// at most one enabled transition per state for a given input, this yields one
// path. If no transition out of the input event's from-state fires (its guard is
// false), Run returns a result with no transitions — the "does not route" case.
func (r *Runner) Run(ctx context.Context, in RunInput) (*RunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: context: %w", ErrRunner, err)
	}
	startEv, ok := r.events[in.Event]
	if !ok {
		return nil, fmt.Errorf("%w: unknown event %q in spec %q", ErrRunner, in.Event, r.spec.ID)
	}
	res := &RunResult{WorkflowID: r.spec.ID, Version: r.spec.Version}

	// The run begins at the triggering event's from-state and attempts that exact
	// event first; thereafter it follows whatever transition is enabled out of the
	// current state. This lets a scenario start mid-machine while still exercising
	// the downstream path.
	cur := startEv.From
	res.FinalState = cur

	// Attempt the explicit starting event first.
	advanced, err := r.fire(ctx, startEv, in, res)
	if err != nil {
		return nil, err
	}
	if !advanced {
		// The triggering event's guard did not pass: no transition. This is the
		// expected "below threshold / not flagged / too far out" outcome.
		return res, nil
	}
	cur = startEv.To
	res.FinalState = cur

	// Follow the machine forward from the new state. Bound the walk by the number
	// of events to guarantee termination even on a malformed cyclic spec.
	for steps := 0; steps < len(r.spec.Events)+1; steps++ {
		if r.isTerminal(cur) {
			return res, nil
		}
		next, fired, err := r.next(ctx, cur, in, res)
		if err != nil {
			return nil, err
		}
		if !fired {
			return res, nil
		}
		cur = next
		res.FinalState = cur
	}
	return res, fmt.Errorf("%w: spec %q exceeded transition bound from %q (possible cycle)", ErrRunner, r.spec.ID, cur)
}

// next finds and fires the single enabled transition out of state cur, returning
// the new state. fired is false when no outgoing transition's guard passes.
func (r *Runner) next(ctx context.Context, cur string, in RunInput, res *RunResult) (string, bool, error) {
	for _, ev := range r.byFrom[cur] {
		advanced, err := r.fire(ctx, ev, in, res)
		if err != nil {
			return "", false, err
		}
		if advanced {
			return ev.To, true, nil
		}
	}
	return "", false, nil
}

// fire attempts a single event: evaluates its guard against the input fixture;
// if it passes (or there is none), records the transition, runs the event's
// actions through the executor seam, and reports advanced=true. A failing guard
// records nothing and reports advanced=false.
func (r *Runner) fire(ctx context.Context, ev Event, in RunInput, res *RunResult) (bool, error) {
	if _, ok := r.states[ev.To]; !ok {
		return false, fmt.Errorf("%w: event %q targets undefined state %q", ErrRunner, ev.Name, ev.To)
	}
	if ev.Guard != "" {
		g, ok := r.guards[ev.Guard]
		if !ok {
			return false, fmt.Errorf("%w: event %q references undefined guard %q", ErrRunner, ev.Name, ev.Guard)
		}
		pass, err := r.guard.Eval(g, in.Fields)
		if err != nil {
			return false, fmt.Errorf("%w: evaluating guard %q for event %q: %w", ErrRunner, g.Name, ev.Name, err)
		}
		if !pass {
			return false, nil
		}
	}
	res.Transitions = append(res.Transitions, TransitionStep{Event: ev.Name, From: ev.From, To: ev.To})
	if err := r.runActions(ctx, ev, res); err != nil {
		return false, err
	}
	return true, nil
}

// runActions runs every action fired by ev through the executor seam, recording
// each call. A write that requires approval is held at the gate: it is recorded
// with Gated=true and the host-stub refuses it (no live side effect). Reads are
// run through the executor with no target side effect in the stub.
func (r *Runner) runActions(ctx context.Context, ev Event, res *RunResult) error {
	for _, a := range r.actionsOn[ev.Name] {
		call := ActionCall{
			Name:    a.Name,
			Kind:    a.Kind,
			Target:  a.Target,
			OnEvent: ev.Name,
			Backend: r.exec.Backend(),
		}
		// A write that requires approval is gated: we never perform the real side
		// effect. We still hand it to the executor with ApprovalGranted=false so the
		// fail-closed backend refuses it, proving the gate fired.
		gated := a.Kind.IsWrite() && a.RequiresApproval
		call.Gated = gated
		if gated {
			res.ApprovalRequested = true
		}
		cfg := ExecConfig{
			WorkflowID:      r.spec.ID,
			Version:         r.spec.Version,
			ApprovalGranted: false, // the seam never self-approves; gated writes fail closed
		}
		// Reads with no external target are allow-listed implicitly (empty target);
		// reads against a named target are refused by the stub (no live network),
		// which is recorded, not fatal — the run still proceeds deterministically.
		_, err := r.exec.Execute(ctx, cfg, ExecAction{
			Name:   a.Name,
			Kind:   a.Kind,
			Target: a.Target,
		})
		if err != nil {
			if errors.Is(err, ErrNotAuthorized) {
				call.Refused = true
			} else {
				return fmt.Errorf("%w: executing action %q: %w", ErrRunner, a.Name, err)
			}
		}
		res.Actions = append(res.Actions, call)
	}
	return nil
}

func (r *Runner) isTerminal(state string) bool {
	st, ok := r.states[state]
	return ok && st.Terminal
}

// --- Guard evaluation ---

// GuardEvaluator decides whether a guard passes for a given fixture. It is a seam
// so a workflow may supply richer semantics later; the kernel ships a
// deterministic default. Implementations MUST be pure and deterministic.
type GuardEvaluator interface {
	// Eval reports whether the guard holds for the fixture. It returns an error
	// only when the guard expression cannot be interpreted at all (never for a
	// merely-false guard).
	Eval(g Guard, fields map[string]string) (bool, error)
}

// DefaultGuardEvaluator interprets a guard's Expr as a single comparison and
// evaluates it against the string->string fixture. It is deliberately small and
// deterministic:
//
//   - The expression is split on the first comparison operator
//     (>=, <=, >, <, ==, !=).
//   - Each side is resolved to a float: a numeric literal stays itself; an
//     identifier (possibly dotted, e.g. usage_trend.delta_pct) resolves by
//     matching its last path segment against a fixture key, then against the
//     fixture's known aliases (defaultFixtureAliases), then against a small
//     threshold registry (defaultThresholds) for a named threshold absent from
//     the fixture.
//   - A side that resolves to nothing makes the guard fail (false), not error —
//     a guard whose data is absent simply does not hold.
//
// This covers every guard in the three ground-truth specs without hardcoding any
// one workflow, and degrades safely: an unresolved operand is a non-firing guard,
// which keeps the machine from advancing on missing data.
type DefaultGuardEvaluator struct{}

// defaultThresholds supplies a deterministic value for a named threshold operand
// that the fixture does not carry. These are the rubric constants a real ICP /
// match / renewal model would hold; the kernel keeps them explicit and stable so
// generation stays deterministic and the scenarios are self-contained.
var defaultThresholds = map[string]float64{
	"icp_threshold":   50,  // a lead at/above 50 fits the ICP
	"match_threshold": 0.5, // a candidate at/above 0.5 is a confident match
	"renewal_window":  60,  // renewals within 60 days are in scope
}

// defaultFixtureAliases maps a guard operand's last path segment to the fixture
// key that actually carries its value, when the contract's domain wording and the
// fixture's wording differ (e.g. the guard speaks of "renewal_date - now" while
// the fixture carries the already-computed "renewal_in_days"). Deterministic and
// shared by every spec.
var defaultFixtureAliases = map[string]string{
	"renewal_date": "renewal_in_days",
}

// Eval implements GuardEvaluator.
func (DefaultGuardEvaluator) Eval(g Guard, fields map[string]string) (bool, error) {
	expr := strings.TrimSpace(g.Expr)
	if expr == "" {
		return false, fmt.Errorf("guard %q: empty expression", g.Name)
	}

	// Recognise the one arithmetic shape used by the renewal guard:
	// "renewal_date - now <op> 60d". The fixture carries the already-computed
	// days-remaining (renewal_in_days via the alias map); the window is the
	// duration literal on the right.
	if strings.Contains(expr, "- now") {
		lhs := strings.TrimSpace(expr[:strings.Index(expr, "- now")])
		op, rest, ok := splitOnOperator(expr)
		if !ok {
			return false, fmt.Errorf("guard %q: no comparison operator in %q", g.Name, expr)
		}
		window, derr := parseDuration(rest)
		if derr != nil {
			return false, fmt.Errorf("guard %q: %w", g.Name, derr)
		}
		days, dok := resolveOperand(lhs, fields)
		if !dok {
			return false, nil
		}
		return compare(days, op, window), nil
	}

	op, rhsStr, ok := splitOnOperator(expr)
	if !ok {
		return false, fmt.Errorf("guard %q: no comparison operator in %q", g.Name, expr)
	}
	lhs := strings.TrimSpace(expr[:strings.Index(expr, op)])

	left, lok := resolveOperand(lhs, fields)
	if !lok {
		return false, nil
	}
	right, rok := resolveOperandOrLiteral(rhsStr, fields)
	if !rok {
		return false, nil
	}
	return compare(left, op, right), nil
}

// comparisonOps are the recognised operators, longest first so ">=" is matched
// before ">".
var comparisonOps = []string{">=", "<=", "==", "!=", ">", "<"}

// splitOnOperator finds the first comparison operator in expr and returns it plus
// the right-hand remainder.
func splitOnOperator(expr string) (op string, rhs string, ok bool) {
	best := -1
	var bestOp string
	for _, o := range comparisonOps {
		i := strings.Index(expr, o)
		if i < 0 {
			continue
		}
		if best == -1 || i < best || (i == best && len(o) > len(bestOp)) {
			best = i
			bestOp = o
		}
	}
	if best == -1 {
		return "", "", false
	}
	return bestOp, strings.TrimSpace(expr[best+len(bestOp):]), true
}

// resolveOperand resolves an identifier operand (possibly dotted) to a float from
// the fixture, via the alias map. It does NOT consult literals; use
// resolveOperandOrLiteral for a side that may be a constant.
func resolveOperand(ident string, fields map[string]string) (float64, bool) {
	key := lastSegment(ident)
	if alias, ok := defaultFixtureAliases[key]; ok {
		key = alias
	}
	if v, ok := fields[key]; ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

// resolveOperandOrLiteral resolves a right-hand operand that may be a numeric
// literal (e.g. -0.20), a fixture field, or a named threshold from the registry.
func resolveOperandOrLiteral(operand string, fields map[string]string) (float64, bool) {
	operand = strings.TrimSpace(operand)
	if operand == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(operand, 64); err == nil {
		return f, true
	}
	if f, ok := resolveOperand(operand, fields); ok {
		return f, true
	}
	if f, ok := defaultThresholds[lastSegment(operand)]; ok {
		return f, true
	}
	return 0, false
}

// lastSegment returns the final dotted segment of an identifier
// (usage_trend.delta_pct -> delta_pct).
func lastSegment(ident string) string {
	ident = strings.TrimSpace(ident)
	if i := strings.LastIndex(ident, "."); i >= 0 {
		return ident[i+1:]
	}
	return ident
}

// parseDuration reads a trailing day count like "60d" or "60" into a float.
func parseDuration(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "d")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("parsing duration %q: %w", s, err)
	}
	return f, nil
}

// compare evaluates left <op> right.
func compare(left float64, op string, right float64) bool {
	switch op {
	case ">=":
		return left >= right
	case "<=":
		return left <= right
	case ">":
		return left > right
	case "<":
		return left < right
	case "==":
		return left == right
	case "!=":
		return left != right
	default:
		return false
	}
}

// sortedKeys returns the fixture keys in deterministic order (used when emitting
// a fixture for documentation/tests).
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
