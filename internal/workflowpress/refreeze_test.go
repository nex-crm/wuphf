package workflowpress

import (
	"errors"
	"testing"
)

// refreeze_test.go locks down the structural-convergence path: a STRUCTURAL change
// (a new state + transition + action — the kind a leaf Overlay deliberately cannot
// express) goes through Refreeze, yields the SAME workflow id at a higher content
// version (convergence, not proliferation), and still passes Validate + shipcheck.
//
// It is the regression for Phase E's decision: leaf changes -> Overlay; structural
// changes -> Refreeze SAME id. The overlay op set stays leaf-only (no add-state /
// add-event / add-action op), and structural growth converges through a stable id.

// structurallyExtend returns a draft that is the trial-to-ae-routing contract plus
// a brand-new branch the leaf-overlay vocabulary cannot add:
//
//   - a new terminal state "escalated";
//   - a new external event "routing_escalated" (routed -> escalated);
//   - a new external-write action "notify_escalation" fired on that event;
//   - a verification scenario that exercises the new transition (so shipcheck's
//     transition-coverage check still passes for the new edge).
//
// The base content version is preserved on the draft; Refreeze re-stamps it to
// prev+1. The new external-write requires approval (its provenance is inferred), so
// the structural addition is itself a safe state machine.
func structurallyExtend(base *WorkflowSpec) WorkflowSpec {
	draft := *cloneSpec(*base)

	draft.States = append(draft.States, State{
		Name:        "escalated",
		Description: "A routed signup was escalated to the RevOps operator for manual handling.",
		Terminal:    true,
		Provenance:  stated(1.0),
	})
	draft.Events = append(draft.Events, Event{
		Name:       "routing_escalated",
		Trigger:    TriggerExternal,
		From:       "routed",
		To:         "escalated",
		Provenance: inferred(0.6),
	})
	draft.Actions = append(draft.Actions, Action{
		Name:             "notify_escalation",
		Kind:             ActionExternalWrite,
		On:               "routing_escalated",
		Target:           "revops-escalation-channel",
		RequiresApproval: true, // inferred external-write must require approval
		Provenance:       inferred(0.6),
	})
	draft.VerificationScenarios = append(draft.VerificationScenarios, VerificationScenario{
		Name:              "escalation_notifies_revops",
		Given:             map[string]string{"reason": "no_matching_ae"},
		When:              "routing_escalated",
		ExpectTransitions: []Transition{{From: "routed", To: "escalated"}},
		ExpectApproval:    true, // notify_escalation is an external-write gated for approval
	})
	return draft
}

// TestRefreezeStructuralChangeConvergesSameID is the Phase E regression: a draft
// carrying a structural change (new state + transition + action) is Refrozen against
// the previous frozen spec and yields a NEW frozen version of the SAME workflow id
// at version+1, having passed Validate AND a shipcheck replay. Convergence holds via
// the stable id; no new workflow is minted.
func TestRefreezeStructuralChangeConvergesSameID(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")

	// Freeze the base contract first — Refreeze operates on a previously frozen spec.
	prev, err := Freeze(*base, approvalFor(*base, DecisionApprove))
	if err != nil {
		t.Fatalf("Freeze(base): %v", err)
	}

	draft := structurallyExtend(base)
	// The operator's approval is scoped to the resulting candidate id + version
	// (prev.Spec.Version + 1) — Refreeze re-stamps the draft's version to that and
	// the request must authorise exactly it.
	req := FreezeRequest{
		WorkflowID: base.ID,
		Version:    prev.Spec.Version + 1,
		Decision:   DecisionApprove,
		Operator:   "revops",
		Note:       "added an escalation branch for no-matching-AE routing",
	}

	refrozen, report, err := Refreeze(prev, draft, req)
	if err != nil {
		t.Fatalf("Refreeze: %v", err)
	}

	// (1) Same id — convergence, never a new workflow.
	if refrozen.Spec.ID != base.ID {
		t.Errorf("refrozen id = %q, want %q (structural change must converge on the same id)", refrozen.Spec.ID, base.ID)
	}
	// (2) Higher content version — prefer-update, version+1.
	if refrozen.Spec.Version != prev.Spec.Version+1 {
		t.Errorf("refrozen version = %d, want %d", refrozen.Spec.Version, prev.Spec.Version+1)
	}
	// (3) The structural elements are present.
	if !hasState(refrozen.Spec, "escalated") {
		t.Error("refrozen spec is missing the new state 'escalated'")
	}
	if !hasEvent(refrozen.Spec, "routing_escalated") {
		t.Error("refrozen spec is missing the new event 'routing_escalated'")
	}
	if !hasAction(refrozen.Spec, "notify_escalation") {
		t.Error("refrozen spec is missing the new action 'notify_escalation'")
	}
	// (4) Provenance is carried: the new external-write still requires approval and
	// the audit fields record who refroze it and how.
	for _, a := range refrozen.Spec.Actions {
		if a.Name == "notify_escalation" && !a.RequiresApproval {
			t.Error("carried structural external-write lost its approval requirement")
		}
	}
	if refrozen.ApprovedBy != "revops" {
		t.Errorf("refrozen approvedBy = %q, want %q", refrozen.ApprovedBy, "revops")
	}
	if refrozen.Decision != DecisionApprove {
		t.Errorf("refrozen decision = %q, want %q", refrozen.Decision, DecisionApprove)
	}
	if refrozen.Note != req.Note {
		t.Errorf("refrozen note = %q, want %q", refrozen.Note, req.Note)
	}

	// (5) The refrozen spec still Validates AND still passes a full shipcheck replay
	// — the structural change did not break the mechanical proof.
	if err := refrozen.Spec.Validate(); err != nil {
		t.Errorf("refrozen spec does not validate: %v", err)
	}
	if report == nil || !report.Passed {
		if report != nil {
			for _, f := range report.Findings {
				t.Logf("  %s: passed=%v detail=%s", f.Check, f.Passed, f.Detail)
			}
		}
		t.Fatal("refrozen spec failed shipcheck replay; a structural Refreeze must still ship-check")
	}
	if report.Version != prev.Spec.Version+1 {
		t.Errorf("shipcheck report version = %d, want %d", report.Version, prev.Spec.Version+1)
	}
}

// TestRefreezeRejectsDifferentID proves Refreeze can NEVER mint a new workflow id:
// a draft whose id differs from the previous frozen spec is rejected. Structural
// change converges on the SAME id; a different id is a new contract, not a refreeze.
func TestRefreezeRejectsDifferentID(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	prev, err := Freeze(*base, approvalFor(*base, DecisionApprove))
	if err != nil {
		t.Fatalf("Freeze(base): %v", err)
	}

	draft := structurallyExtend(base)
	draft.ID = "trial-to-ae-routing-v2" // a NEW id — forbidden
	req := FreezeRequest{
		WorkflowID: draft.ID,
		Version:    prev.Spec.Version + 1,
		Decision:   DecisionApprove,
		Operator:   "revops",
	}
	if _, _, err := Refreeze(prev, draft, req); !errors.Is(err, ErrRefreezeIDMismatch) {
		t.Fatalf("Refreeze with a different id: err = %v, want ErrRefreezeIDMismatch", err)
	}
}

// TestRefreezeRequiresApproval proves the human gate is not waived for a structural
// change: a non-approving decision leaves the previous spec un-refrozen.
func TestRefreezeRequiresApproval(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	prev, err := Freeze(*base, approvalFor(*base, DecisionApprove))
	if err != nil {
		t.Fatalf("Freeze(base): %v", err)
	}

	draft := structurallyExtend(base)
	req := FreezeRequest{
		WorkflowID: base.ID,
		Version:    prev.Spec.Version + 1,
		Decision:   DecisionReject, // operator declines
		Operator:   "revops",
	}
	if _, _, err := Refreeze(prev, draft, req); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Refreeze(reject): err = %v, want ErrNotApproved", err)
	}
}

// TestRefreezeRejectsBrokenStructuralChange proves the shipcheck gate still applies
// to a structural Refreeze: a structural change whose own verification scenario does
// not reproduce is REJECTED on replay, and no refrozen spec is returned. Here the
// new escalation scenario asserts a transition the runtime cannot produce (it claims
// the run lands in 'routed' instead of the new 'escalated'), so fixture-replay fails.
func TestRefreezeRejectsBrokenStructuralChange(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	prev, err := Freeze(*base, approvalFor(*base, DecisionApprove))
	if err != nil {
		t.Fatalf("Freeze(base): %v", err)
	}

	draft := structurallyExtend(base)
	// Corrupt the new scenario's expected transition so the replay no longer matches
	// what the runtime produces (it lands in 'escalated', not 'routed').
	for i := range draft.VerificationScenarios {
		if draft.VerificationScenarios[i].Name == "escalation_notifies_revops" {
			draft.VerificationScenarios[i].ExpectTransitions = []Transition{{From: "routed", To: "routed"}}
		}
	}
	req := FreezeRequest{
		WorkflowID: base.ID,
		Version:    prev.Spec.Version + 1,
		Decision:   DecisionApprove,
		Operator:   "revops",
	}
	refrozen, report, err := Refreeze(prev, draft, req)
	if err == nil {
		t.Fatal("expected Refreeze to reject a structural change that fails its own shipcheck replay")
	}
	if !errors.Is(err, ErrRefreezeShipcheckFailed) {
		t.Errorf("err = %v, want ErrRefreezeShipcheckFailed", err)
	}
	if refrozen.Spec.ID != "" || refrozen.ApprovedBy != "" {
		t.Errorf("a failing Refreeze must return the zero FrozenSpec, got id=%q approvedBy=%q", refrozen.Spec.ID, refrozen.ApprovedBy)
	}
	if report == nil || report.Passed {
		t.Fatalf("expected a FAILING shipcheck report on a broken structural change, got %+v", report)
	}
}

// TestOverlayVocabularyStaysLeafOnly is the boundary assertion: the overlay op set
// remains LEAF-only and carries NO structural op (no add-state / add-event /
// add-action). Structural growth is Refreeze's job, by the Phase E decision. If
// someone adds a structural overlay op, this test fails and forces a reconsideration
// of the boundary.
func TestOverlayVocabularyStaysLeafOnly(t *testing.T) {
	t.Parallel()
	leafOnly := map[OverlayOpKind]struct{}{
		OpSetGuardExpr:            {},
		OpSetSLAThreshold:         {},
		OpAddException:            {},
		OpAddImprovementSignal:    {},
		OpAddVerificationScenario: {},
	}
	for k := range leafOnly {
		if !k.Valid() {
			t.Errorf("expected leaf overlay op %q to be valid", k)
		}
	}
	// No structural op kind may be Valid — those belong to Refreeze, not Overlay.
	for _, structural := range []OverlayOpKind{"add-state", "add-event", "add-action", "remove-state"} {
		if structural.Valid() {
			t.Errorf("overlay op %q must NOT be a valid leaf op (structural change is Refreeze's job)", structural)
		}
	}
}

// --- small spec-membership helpers (test-local) ---

func hasState(s WorkflowSpec, name string) bool {
	for _, st := range s.States {
		if st.Name == name {
			return true
		}
	}
	return false
}

func hasEvent(s WorkflowSpec, name string) bool {
	for _, ev := range s.Events {
		if ev.Name == name {
			return true
		}
	}
	return false
}

func hasAction(s WorkflowSpec, name string) bool {
	for _, a := range s.Actions {
		if a.Name == name {
			return true
		}
	}
	return false
}
