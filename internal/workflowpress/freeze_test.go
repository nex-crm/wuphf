package workflowpress

import (
	"encoding/json"
	"errors"
	"testing"
)

// freeze_test.go is the second half of the Phase 3 proof: Freeze is the human
// gate. It proves a draft becomes a frozen contract ONLY when the operator
// approves AND the draft passes Validate, and that an unapproved draft never
// becomes frozen.

// approvalFor builds an approving FreezeRequest scoped to a draft.
func approvalFor(draft WorkflowSpec, decision FreezeDecision) FreezeRequest {
	return FreezeRequest{
		WorkflowID: draft.ID,
		Version:    draft.Version,
		Decision:   decision,
		Operator:   "revops",
	}
}

// TestFreezeApprovedDraftBecomesFrozen proves the happy path: for each
// ground-truth example, an approved-and-valid draft freezes into a contract that
// is a faithful copy of the reviewed draft and records the approval.
func TestFreezeApprovedDraftBecomesFrozen(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			draft := synthDraft(t, name)

			frozen, err := Freeze(draft, approvalFor(draft, DecisionApprove))
			if err != nil {
				t.Fatalf("Freeze(approved) error: %v", err)
			}
			if frozen.Spec.ID != draft.ID {
				t.Errorf("frozen id = %q, want %q", frozen.Spec.ID, draft.ID)
			}
			if frozen.Decision != DecisionApprove {
				t.Errorf("frozen decision = %q, want %q", frozen.Decision, DecisionApprove)
			}
			if frozen.ApprovedBy != "revops" {
				t.Errorf("frozen approvedBy = %q, want %q", frozen.ApprovedBy, "revops")
			}
			// The frozen spec is itself a sound, safe state machine.
			if err := frozen.Spec.Validate(); err != nil {
				t.Errorf("frozen spec does not validate: %v", err)
			}
		})
	}
}

// TestFreezeApproveWithNoteFreezes proves the "add context and approve" path
// also green-lights a freeze and carries the operator note for audit.
func TestFreezeApproveWithNoteFreezes(t *testing.T) {
	t.Parallel()
	draft := synthDraft(t, "trial-to-ae-routing")
	req := approvalFor(draft, DecisionApproveWithNote)
	req.Note = "looks right; route_to_ae target should be crm-v2"

	frozen, err := Freeze(draft, req)
	if err != nil {
		t.Fatalf("Freeze(approve_with_note) error: %v", err)
	}
	if frozen.Note != req.Note {
		t.Errorf("frozen note = %q, want %q", frozen.Note, req.Note)
	}
}

// TestFreezeRejectedDraftNeverFreezes is the core gate assertion: a rejected (or
// otherwise non-approving) decision never produces a frozen spec. This is the
// "an unapproved spec never becomes frozen" requirement, table-driven over every
// non-approving decision including the empty and unknown ones (fail closed).
func TestFreezeRejectedDraftNeverFreezes(t *testing.T) {
	t.Parallel()
	draft := synthDraft(t, "trial-to-ae-routing")

	nonApproving := []FreezeDecision{
		DecisionReject,
		FreezeDecision(""),
		FreezeDecision("maybe"),
		FreezeDecision("APPROVE"), // case-sensitive; not the canonical value
	}
	for _, decision := range nonApproving {
		decision := decision
		t.Run(string(decision), func(t *testing.T) {
			t.Parallel()
			frozen, err := Freeze(draft, approvalFor(draft, decision))
			if !errors.Is(err, ErrNotApproved) {
				t.Fatalf("Freeze(%q) error = %v, want ErrNotApproved", decision, err)
			}
			// A rejected freeze must yield the zero FrozenSpec: no contract, no
			// recorded approval. (FrozenSpec contains slices so it is not == comparable;
			// assert the load-bearing fields directly.)
			if frozen.Spec.ID != "" || frozen.ApprovedBy != "" || frozen.Decision != "" {
				t.Fatalf("Freeze(%q) returned a non-zero FrozenSpec on rejection: %+v", decision, frozen)
			}
		})
	}
}

// TestFreezeRejectsApprovalForWrongDraft proves the approval is scoped: an
// approval whose workflow id or version does not match the draft is rejected,
// so an approval cannot be replayed against a different or re-synthesised draft.
func TestFreezeRejectsApprovalForWrongDraft(t *testing.T) {
	t.Parallel()
	draft := synthDraft(t, "trial-to-ae-routing")

	t.Run("wrong workflow id", func(t *testing.T) {
		t.Parallel()
		req := approvalFor(draft, DecisionApprove)
		req.WorkflowID = "renewal-risk-sweep"
		if _, err := Freeze(draft, req); !errors.Is(err, ErrApprovalMismatch) {
			t.Fatalf("Freeze(wrong id) = %v, want ErrApprovalMismatch", err)
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		t.Parallel()
		req := approvalFor(draft, DecisionApprove)
		req.Version = draft.Version + 1
		if _, err := Freeze(draft, req); !errors.Is(err, ErrApprovalMismatch) {
			t.Fatalf("Freeze(wrong version) = %v, want ErrApprovalMismatch", err)
		}
	})
}

// TestFreezeRequiresValidation proves the human review does not waive the
// structural invariants: an approved draft that does not Validate still cannot
// be frozen. Here the draft has an inferred external-write with approval
// stripped — exactly the unsafe shape Validate rejects.
func TestFreezeRequiresValidation(t *testing.T) {
	t.Parallel()
	draft := synthDraft(t, "trial-to-ae-routing")

	// Break the draft: strip approval off an inferred external-write. Validate
	// must reject it, and Freeze must therefore refuse even with an approval.
	var broke bool
	for i := range draft.Actions {
		if draft.Actions[i].Kind == ActionExternalWrite {
			draft.Actions[i].RequiresApproval = false
			draft.Actions[i].Provenance = Provenance{TrustTier: TrustInferred, Confidence: 0.5}
			broke = true
			break
		}
	}
	if !broke {
		t.Fatal("test setup: no external-write action to break")
	}

	_, err := Freeze(draft, approvalFor(draft, DecisionApprove))
	if !errors.Is(err, ErrWriteNeedsApproval) {
		t.Fatalf("Freeze(invalid draft) = %v, want ErrWriteNeedsApproval", err)
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("Freeze(invalid draft) = %v, want wrapping ErrInvalidSpec", err)
	}
}

// TestFreezeDoesNotMutateDraft proves Freeze treats its draft as immutable: the
// caller's draft reads identically before and after, and the frozen spec does
// not alias it (mutating the frozen copy does not reach back into the draft).
func TestFreezeDoesNotMutateDraft(t *testing.T) {
	t.Parallel()
	draft := synthDraft(t, "inbound-lead-dedupe-merge")
	before, err := json.Marshal(draft)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	frozen, err := Freeze(draft, approvalFor(draft, DecisionApprove))
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	after, err := json.Marshal(draft)
	if err != nil {
		t.Fatalf("snapshot after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("Freeze mutated its draft\nbefore: %s\nafter:  %s", before, after)
	}

	// Mutating the frozen copy must not reach the draft.
	if len(frozen.Spec.Actions) > 0 {
		frozen.Spec.Actions[0].Name = "MUTATED"
		if draft.Actions[0].Name == "MUTATED" {
			t.Fatal("frozen spec aliases the draft's actions")
		}
	}
}

// TestFreezeDecisionApproves spot-checks the decision predicate's fail-closed
// behaviour directly.
func TestFreezeDecisionApproves(t *testing.T) {
	t.Parallel()
	approving := []FreezeDecision{DecisionApprove, DecisionApproveWithNote}
	for _, d := range approving {
		if !d.Approves() {
			t.Errorf("%q.Approves() = false, want true", d)
		}
	}
	rejecting := []FreezeDecision{DecisionReject, "", "approve ", "nope"}
	for _, d := range rejecting {
		if d.Approves() {
			t.Errorf("%q.Approves() = true, want false", d)
		}
	}
}

// TestFreezeDefaultsApprovedByToOperator proves that when the request omits the
// reviewing operator, the frozen record falls back to the spec's operator rather
// than recording an empty approver.
func TestFreezeDefaultsApprovedByToOperator(t *testing.T) {
	t.Parallel()
	draft := synthDraft(t, "renewal-risk-sweep")
	req := approvalFor(draft, DecisionApprove)
	req.Operator = ""

	frozen, err := Freeze(draft, req)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if frozen.ApprovedBy != draft.Operator {
		t.Errorf("approvedBy = %q, want fallback to spec operator %q", frozen.ApprovedBy, draft.Operator)
	}
}

// TestFreezeRoundTripFromSynthesis is the end-to-end Phase-3 proof: distil ->
// synthesise -> the draft is NOT yet a frozen contract -> operator approval ->
// frozen contract that validates and schema-validates.
func TestFreezeRoundTripFromSynthesis(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			draft := synthDraft(t, name)

			// A draft is not a contract: without approval it does not freeze.
			if _, err := Freeze(draft, FreezeRequest{WorkflowID: name, Version: draft.Version, Decision: DecisionReject}); !errors.Is(err, ErrNotApproved) {
				t.Fatalf("draft froze without approval: %v", err)
			}

			frozen, err := Freeze(draft, approvalFor(draft, DecisionApprove))
			if err != nil {
				t.Fatalf("Freeze(approved): %v", err)
			}
			if err := frozen.Spec.Validate(); err != nil {
				t.Fatalf("frozen spec failed semantic validation: %v", err)
			}
			if _, err := roundTripValidate(&frozen.Spec, ValidateSpecJSON); err != nil {
				t.Fatalf("frozen spec failed JSON Schema validation: %v", err)
			}
		})
	}
}
