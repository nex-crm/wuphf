package workflowpress

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// idSlug constrains a WorkflowSpec.ID to a safe slug. The id is used verbatim as a
// path component when the Generator builds the emitted file-map keys (dir + "/" +
// file), so it must not carry path separators, dots, or traversal sequences.
// Lowercase alphanumerics and internal hyphens only, 1..63 runes, must start with
// an alphanumeric: "trial-to-ae-routing" passes; "../../etc", "a/b", "a.b" do not.
var idSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// validate.go enforces the contract invariants that keep a WorkflowSpec a sound
// state machine and a safe one. These are deterministic, kernel-side checks run
// before a spec is frozen, generated from, or has an overlay accepted. They are
// the structural half of the freeze gate; the human review is the other half.
//
// The invariants:
//
//   - every transition (Event.From/To, plus VerificationScenario transitions)
//     references a defined state;
//   - every action carries Provenance (a valid trust tier + confidence in
//     0..1);
//   - every event carries a known trigger and every action a known kind (enum
//     checks — an unknown kind would otherwise fail open past the next rule
//     because ActionKind.IsWrite treats anything unrecognised as a read);
//   - verification scenarios reference real states and a real event;
//   - inferred/observed write-actions have RequiresApproval == true (the
//     trust-tier security rule — only operator-stated writes may relax it).
//
// All returned errors wrap a sentinel with %w so callers can match on the class
// of failure while still seeing the offending element.

// Sentinel errors for the validation classes. Wrapped with %w so callers can
// errors.Is against the class and still read the detail.
var (
	// ErrInvalidSpec is the umbrella error: every Validate failure wraps it.
	ErrInvalidSpec = errors.New("workflowpress: invalid workflow spec")
	// ErrUndefinedState is returned when a transition references a state that is
	// not defined in States.
	ErrUndefinedState = errors.New("transition references undefined state")
	// ErrMissingProvenance is returned when an action (or other inferable
	// element) lacks valid provenance.
	ErrMissingProvenance = errors.New("element missing valid provenance")
	// ErrBadScenarioRef is returned when a verification scenario references a
	// state or event that does not exist.
	ErrBadScenarioRef = errors.New("verification scenario references undefined state or event")
	// ErrWriteNeedsApproval is returned when an inferred/observed write-action
	// does not require approval.
	ErrWriteNeedsApproval = errors.New("inferred/observed write-action must require approval")
	// ErrEmptyField is returned when a structurally required field is empty.
	ErrEmptyField = errors.New("required field is empty")
	// ErrInvalidEnum is returned when an enum-typed field (action kind, event
	// trigger) carries a value outside its defined set. This is security-relevant
	// for action kind: an unknown kind would fail open past the write-approval
	// rule, so it must be rejected here.
	ErrInvalidEnum = errors.New("enum field has unknown value")
	// ErrNoBlueprint is returned by Synthesize when no synthesis blueprint is
	// registered for a workflow id. Synthesis needs the state-machine skeleton a
	// generic inference pass cannot read out of evidence; without it there is no
	// draft to produce.
	ErrNoBlueprint = errors.New("no synthesis blueprint registered for workflow")
	// ErrNotApproved is returned by Freeze when the operator has not approved the
	// draft. Freezing is the human gate: an unapproved draft never becomes a
	// frozen contract.
	ErrNotApproved = errors.New("freeze requires operator approval")
	// ErrApprovalMismatch is returned by Freeze when the approval does not match
	// the draft it is meant to authorise (wrong workflow id or wrong version). An
	// approval is scoped to exactly the draft the operator reviewed.
	ErrApprovalMismatch = errors.New("approval does not match the draft under review")
	// ErrInvalidID is returned when a spec ID is not a safe slug. The id is used
	// verbatim as a path component during generation, so a value with slashes,
	// dots, or traversal sequences is a path-traversal risk and is rejected here.
	ErrInvalidID = errors.New("workflow id is not a safe slug")
	// ErrUnsupportedSchemaVersion is returned when an artifact's schema_version is
	// not the current supported wire-format version. Validate fails CLOSED on any
	// unknown/newer version: a producer on a wire format this kernel does not
	// recognise must be rejected, never decoded best-effort (which would risk
	// silently zero-valuing a guard or a RequiresApproval flag).
	ErrUnsupportedSchemaVersion = errors.New("unsupported schema_version")
)

// Validate enforces the contract invariants on a WorkflowSpec. It returns the
// first violation found, wrapping ErrInvalidSpec plus the specific sentinel so
// callers can both classify (errors.Is) and report (error string) the failure.
func (s *WorkflowSpec) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: %w: spec is nil", ErrInvalidSpec, ErrEmptyField)
	}
	// Wire-format gate FIRST, and fail closed: a spec whose schema_version is not
	// the current supported version speaks a wire shape this kernel cannot be sure
	// it understands. Reject it before any field-level check so an unknown/newer
	// version cannot reach code that would interpret it best-effort.
	if s.SchemaVersion != SchemaVersionWorkflowSpec {
		return fmt.Errorf(
			"%w: %w: spec schema_version is %d, want %d",
			ErrInvalidSpec, ErrUnsupportedSchemaVersion, s.SchemaVersion, SchemaVersionWorkflowSpec,
		)
	}
	if s.ID == "" {
		return fmt.Errorf("%w: %w: id", ErrInvalidSpec, ErrEmptyField)
	}
	// The id is used verbatim as a path component when the Generator builds its
	// file-map keys, so it must be a safe slug — no slashes, dots, or traversal.
	if !idSlug.MatchString(s.ID) {
		return fmt.Errorf("%w: %w: id %q must match %s", ErrInvalidSpec, ErrInvalidID, s.ID, idSlug.String())
	}
	if s.Goal == "" {
		return fmt.Errorf("%w: %w: goal", ErrInvalidSpec, ErrEmptyField)
	}
	if s.Operator == "" {
		return fmt.Errorf("%w: %w: operator", ErrInvalidSpec, ErrEmptyField)
	}
	if len(s.States) == 0 {
		return fmt.Errorf("%w: %w: states", ErrInvalidSpec, ErrEmptyField)
	}

	// Index defined states and events for reference checks, and count the
	// state-machine anchors (initial/terminal) while we are iterating.
	stateSet := make(map[string]struct{}, len(s.States))
	var initialStates, terminalStates int
	for _, st := range s.States {
		if st.Name == "" {
			return fmt.Errorf("%w: %w: state name", ErrInvalidSpec, ErrEmptyField)
		}
		if err := validateProvenance(st.Provenance, "state "+st.Name); err != nil {
			return err
		}
		if st.Initial {
			initialStates++
		}
		if st.Terminal {
			terminalStates++
		}
		stateSet[st.Name] = struct{}{}
	}

	// State-machine soundness: Validate is the complete freeze gate, so it — not
	// just NewRunner — must require exactly one initial state (a single entry to
	// anchor a run) and at least one terminal state (a defined exit). A spec with
	// zero or two initial states, or no terminal state, is not a sound machine and
	// must never be frozen, generated from, or accepted via an overlay.
	if initialStates != 1 {
		return fmt.Errorf("%w: %w: states must have exactly one initial state, found %d", ErrInvalidSpec, ErrEmptyField, initialStates)
	}
	if terminalStates == 0 {
		return fmt.Errorf("%w: %w: states must have at least one terminal state", ErrInvalidSpec, ErrEmptyField)
	}

	eventSet := make(map[string]struct{}, len(s.Events))
	for _, ev := range s.Events {
		if ev.Name == "" {
			return fmt.Errorf("%w: %w: event name", ErrInvalidSpec, ErrEmptyField)
		}
		if err := validateProvenance(ev.Provenance, "event "+ev.Name); err != nil {
			return err
		}
		if !ev.Trigger.Valid() {
			return fmt.Errorf("%w: %w: event %q has unknown trigger %q", ErrInvalidSpec, ErrInvalidEnum, ev.Name, ev.Trigger)
		}
		// Every transition references a defined state.
		if _, ok := stateSet[ev.From]; !ok {
			return fmt.Errorf("%w: %w: event %q from-state %q", ErrInvalidSpec, ErrUndefinedState, ev.Name, ev.From)
		}
		if _, ok := stateSet[ev.To]; !ok {
			return fmt.Errorf("%w: %w: event %q to-state %q", ErrInvalidSpec, ErrUndefinedState, ev.Name, ev.To)
		}
		eventSet[ev.Name] = struct{}{}
	}

	for _, en := range s.Entities {
		if err := validateProvenance(en.Provenance, "entity "+en.Name); err != nil {
			return err
		}
	}

	// Every action has provenance; inferred/observed write-actions require
	// approval.
	for _, a := range s.Actions {
		if a.Name == "" {
			return fmt.Errorf("%w: %w: action name", ErrInvalidSpec, ErrEmptyField)
		}
		if err := validateProvenance(a.Provenance, "action "+a.Name); err != nil {
			return err
		}
		// Reject unknown kinds BEFORE the write-approval rule below. IsWrite fails
		// open (an unrecognised kind reads as a non-write), so without this guard a
		// write smuggled in under an unknown kind string would skip approval.
		if !a.Kind.Valid() {
			return fmt.Errorf("%w: %w: action %q has unknown kind %q", ErrInvalidSpec, ErrInvalidEnum, a.Name, a.Kind)
		}
		if a.Kind.IsWrite() && !a.Provenance.IsOperatorStated() && !a.RequiresApproval {
			return fmt.Errorf(
				"%w: %w: action %q (%s, %s)",
				ErrInvalidSpec, ErrWriteNeedsApproval, a.Name, a.Kind, a.Provenance.TrustTier,
			)
		}
	}

	for _, ex := range s.Exceptions {
		if err := validateProvenance(ex.Provenance, "exception "+ex.Name); err != nil {
			return err
		}
	}
	for _, sla := range s.SLAs {
		if err := validateProvenance(sla.Provenance, "sla "+sla.Name); err != nil {
			return err
		}
	}
	for _, g := range s.Guards {
		if err := validateProvenance(g.Provenance, "guard "+g.Name); err != nil {
			return err
		}
	}

	// Verification scenarios reference real states and a real event.
	for _, sc := range s.VerificationScenarios {
		if sc.Name == "" {
			return fmt.Errorf("%w: %w: verification scenario name", ErrInvalidSpec, ErrEmptyField)
		}
		if _, ok := eventSet[sc.When]; !ok {
			return fmt.Errorf("%w: %w: scenario %q when-event %q", ErrInvalidSpec, ErrBadScenarioRef, sc.Name, sc.When)
		}
		for _, tr := range sc.ExpectTransitions {
			if _, ok := stateSet[tr.From]; !ok {
				return fmt.Errorf("%w: %w: scenario %q transition from %q", ErrInvalidSpec, ErrBadScenarioRef, sc.Name, tr.From)
			}
			if _, ok := stateSet[tr.To]; !ok {
				return fmt.Errorf("%w: %w: scenario %q transition to %q", ErrInvalidSpec, ErrBadScenarioRef, sc.Name, tr.To)
			}
		}
	}

	return nil
}

// ErrStrictDecode is returned when a spec fails strict JSON decoding: an unknown
// (removed/renamed) field, trailing data, or malformed JSON. It is the loud
// failure the generated tool's loadSpec relies on — a removed or renamed field
// that a lenient json.Unmarshal would silently zero-value (dropping a guard or a
// RequiresApproval flag) instead surfaces here.
var ErrStrictDecode = errors.New("strict spec decode failed")

// DecodeSpecStrict decodes a WorkflowSpec from JSON with a STRICT decoder
// (DisallowUnknownFields), asserts its schema_version is the current supported
// version, and runs the full state-machine Validate. It is the kernel's loud
// loader: a removed/renamed field, a schema_version mismatch, or any contract
// violation fails here rather than silently zero-valuing a guard or a
// RequiresApproval flag in the decoded spec.
//
// The generated tool's loadSpec delegates to this so the security-load-bearing
// strict-decode + version assertion lives in the reviewed kernel, not in
// per-workflow generated code.
func DecodeSpecStrict(data []byte) (*WorkflowSpec, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var spec WorkflowSpec
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("%w: %w: %w", ErrInvalidSpec, ErrStrictDecode, err)
	}
	// Reject trailing data after the JSON value so a concatenated second document
	// (which the first Decode would ignore) cannot slip past.
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: %w: unexpected trailing data after spec", ErrInvalidSpec, ErrStrictDecode)
		}
		return nil, fmt.Errorf("%w: %w: %w", ErrInvalidSpec, ErrStrictDecode, err)
	}
	// Validate runs the schema_version gate (fail-closed) plus the full
	// state-machine invariants.
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}

// validateProvenance enforces that an inferable element carries a valid trust
// tier and a confidence in [0,1]. The label names the offending element so the
// wrapped error is actionable.
func validateProvenance(p Provenance, label string) error {
	if !p.TrustTier.Valid() {
		return fmt.Errorf("%w: %w: %s has invalid trust tier %q", ErrInvalidSpec, ErrMissingProvenance, label, p.TrustTier)
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("%w: %w: %s has confidence %v outside [0,1]", ErrInvalidSpec, ErrMissingProvenance, label, p.Confidence)
	}
	return nil
}
