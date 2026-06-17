package workflowpress

import (
	"fmt"
	"strings"
)

// freeze.go is the second half of Phase 3: the review gate that turns a DRAFT
// WorkflowSpec into a frozen, operator-reviewed contract. Freezing the spec IS
// the human gate of the press — the operator reviews the synthesised draft and
// accepts it. Only an approved-AND-valid draft becomes a frozen contract;
// otherwise it stays a draft and an error explains why.
//
// The approval shape mirrors the office's propose_app non-blocking approval: a
// proposal is raised for review, the human can approve, "add context and
// approve", or reject, and only an approve green-lights the next step. Here the
// FreezeRequest carries that decision, scoped to the exact draft the operator
// reviewed (workflow id + version), so an approval cannot be replayed against a
// different or re-synthesised draft.
//
// Freeze does NOT mutate the kernel and does NOT execute anything. It is a pure
// gate: (approval, draft) in, frozen-or-error out. The frozen spec is the same
// contract the operator reviewed, marked frozen so downstream generation and
// shipcheck can rely on it having cleared both halves of the gate — the human
// review and the structural Validate.

// FreezeDecision is the operator's verdict on a synthesised draft, mirroring the
// propose_app approval choices. Only Approve and ApproveWithNote green-light a
// freeze; Reject (and any unknown value) leaves the draft a draft.
type FreezeDecision string

const (
	// DecisionApprove accepts the draft as-is.
	DecisionApprove FreezeDecision = "approve"
	// DecisionApproveWithNote accepts the draft and records an operator note
	// (the "add context and approve" path). It green-lights the freeze just like
	// DecisionApprove.
	DecisionApproveWithNote FreezeDecision = "approve_with_note"
	// DecisionReject declines the draft. The draft stays a draft.
	DecisionReject FreezeDecision = "reject"
)

// Approves reports whether the decision green-lights a freeze. Approve and
// ApproveWithNote both proceed; everything else (Reject, the empty string, any
// unknown value) does not. It fails CLOSED: an unrecognised decision never
// freezes a spec.
func (d FreezeDecision) Approves() bool {
	switch d {
	case DecisionApprove, DecisionApproveWithNote:
		return true
	default:
		return false
	}
}

// FreezeRequest represents the operator's review verdict on a specific draft. It
// is the human gate made explicit: the operator reviewed the draft identified by
// WorkflowID at Version and rendered Decision. Scoping the approval to that exact
// (id, version) pair stops an approval from being replayed against a different
// draft or a re-synthesised one.
type FreezeRequest struct {
	// WorkflowID and Version name the exact draft this verdict authorises. Freeze
	// rejects the request if they do not match the draft handed to it.
	WorkflowID string
	Version    int
	// Decision is the operator's verdict. Only an approving decision freezes.
	Decision FreezeDecision
	// Operator is who reviewed the draft, recorded for audit.
	Operator string
	// Note is the operator's optional context, set on the "add context and
	// approve" path. Carried for audit; it does not change the contract.
	Note string
}

// FrozenSpec is a WorkflowSpec that has cleared BOTH halves of the freeze gate:
// the operator's approving review and the structural Validate. It wraps the spec
// rather than mutating it so the type system, not a bool field, marks a contract
// as frozen — a function that requires a frozen contract can take a FrozenSpec
// and cannot be handed a bare draft by mistake. The embedded approval records
// who froze it and how, for audit.
type FrozenSpec struct {
	// Spec is the frozen contract. It is a copy of the reviewed draft; callers
	// cannot reach back through it into the draft they passed to Freeze.
	Spec WorkflowSpec
	// ApprovedBy and Decision record the operator and the verdict that froze it.
	ApprovedBy string
	Decision   FreezeDecision
	// Note carries the operator's optional approval context for audit.
	Note string
}

// Freeze is the review gate. It returns a FrozenSpec ONLY when the request both
// approves the draft AND the draft passes Validate; otherwise it returns an
// error and the draft stays a draft. The order of checks is deliberate:
//
//  1. the request must be scoped to this exact draft (id + version) — an
//     approval for a different or re-synthesised draft is rejected;
//  2. the decision must approve — Reject (or any non-approving value) fails
//     closed with ErrNotApproved;
//  3. the draft must Validate — a structurally unsound state machine cannot be
//     frozen even with an approval (the human review does not waive the
//     mechanical invariants).
//
// Because the frozen spec is a deep copy of the draft, the returned contract is
// independent of the draft the caller passed; later mutation of either does not
// affect the other.
func Freeze(draft WorkflowSpec, req FreezeRequest) (FrozenSpec, error) {
	// (1) The approval must be scoped to exactly the draft under review. An
	// operator approves a specific contract they read; an id/version mismatch
	// means this verdict authorises a different draft and must not freeze this
	// one.
	if strings.TrimSpace(req.WorkflowID) != draft.ID {
		return FrozenSpec{}, fmt.Errorf(
			"workflowpress: freeze: %w: approval for %q but draft is %q",
			ErrApprovalMismatch, req.WorkflowID, draft.ID,
		)
	}
	if req.Version != draft.Version {
		return FrozenSpec{}, fmt.Errorf(
			"workflowpress: freeze: %w: approval for version %d but draft is version %d",
			ErrApprovalMismatch, req.Version, draft.Version,
		)
	}

	// (2) The decision must green-light the freeze. Fails closed: Reject, the
	// empty decision, and any unknown value all leave the draft a draft.
	if !req.Decision.Approves() {
		return FrozenSpec{}, fmt.Errorf(
			"workflowpress: freeze: %w: decision %q for %q",
			ErrNotApproved, req.Decision, draft.ID,
		)
	}

	// (3) The structural invariants are not waived by the human review. A draft
	// that is not a sound, safe state machine cannot be frozen even with an
	// approval.
	frozen := cloneSpec(draft)
	if err := frozen.Validate(); err != nil {
		return FrozenSpec{}, fmt.Errorf("workflowpress: freeze: %w", err)
	}

	approvedBy := strings.TrimSpace(req.Operator)
	if approvedBy == "" {
		approvedBy = frozen.Operator
	}

	return FrozenSpec{
		Spec:       *frozen,
		ApprovedBy: approvedBy,
		Decision:   req.Decision,
		Note:       req.Note,
	}, nil
}

// cloneSpec returns a deep copy of a spec so a FrozenSpec never aliases the draft
// it was frozen from. Every slice and map the spec carries is copied; the frozen
// contract is therefore independent of later mutation of the draft (and vice
// versa). Validate is called on the clone, not the original, so a caller's draft
// is never touched by the freeze attempt.
func cloneSpec(s WorkflowSpec) *WorkflowSpec {
	out := s // copy scalars (ID, Version, Goal, Operator)
	out.Entities = cloneEntities(s.Entities)
	out.States = cloneStates(s.States)
	out.Events = cloneEvents(s.Events)
	out.Guards = cloneGuards(s.Guards)
	out.Actions = cloneActions(s.Actions)
	out.Exceptions = cloneExceptionsList(s.Exceptions)
	out.SLAs = cloneSLAs(s.SLAs)
	out.VerificationScenarios = cloneScenarios(s.VerificationScenarios)
	out.ImprovementSignals = cloneSignals(s.ImprovementSignals)
	return &out
}

func cloneEntities(in []Entity) []Entity {
	if len(in) == 0 {
		return nil
	}
	out := make([]Entity, len(in))
	for i, e := range in {
		e.Fields = cloneStrings(e.Fields)
		e.Provenance.Evidence = cloneStrings(e.Provenance.Evidence)
		out[i] = e
	}
	return out
}

func cloneActions(in []Action) []Action {
	if len(in) == 0 {
		return nil
	}
	out := make([]Action, len(in))
	for i, a := range in {
		a.Provenance.Evidence = cloneStrings(a.Provenance.Evidence)
		out[i] = a
	}
	return out
}

func cloneExceptionsList(in []Exception) []Exception {
	if len(in) == 0 {
		return nil
	}
	out := make([]Exception, len(in))
	for i, ex := range in {
		ex.Provenance.Evidence = cloneStrings(ex.Provenance.Evidence)
		out[i] = ex
	}
	return out
}

func cloneSignals(in []ImprovementSignal) []ImprovementSignal {
	if len(in) == 0 {
		return nil
	}
	out := make([]ImprovementSignal, len(in))
	copy(out, in)
	return out
}
