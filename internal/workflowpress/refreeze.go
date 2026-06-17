package workflowpress

import (
	"errors"
	"fmt"
	"strings"
)

// refreeze.go is the structural-convergence sibling of the leaf Overlay. It
// reconciles the OVERLAY vocabulary with the press's prefer-update-do-not-proliferate
// principle (triangulation architect #3), drawing the boundary this way:
//
//	LEAF change      -> Overlay   (improvement.go): tune a guard/SLA, add an
//	                              exception/signal/verification scenario. Same id,
//	                              version+1, replayed and accepted. The overlay
//	                              vocabulary is deliberately NARROW and cannot add
//	                              or remove states/events/actions.
//	STRUCTURAL change -> Refreeze (this file): add a state/event/action, restructure
//	                              the machine. A new FROZEN version of the SAME
//	                              workflow id — convergence via a stable id, NOT a
//	                              new workflow.
//
// Why two paths, not one. A leaf Overlay is a small, typed, declarative patch: it
// must produce a sound spec by construction, so its op set is closed to non-
// structural edits. A structural change cannot be a tiny declarative op without the
// overlay vocabulary degenerating into a general-purpose spec rewriter — at which
// point it stops being a reviewable patch. So structural change re-enters through
// the SAME human gate the original contract did (Freeze): the operator reviews the
// whole reworked contract, it is Validated, and it is replayed by shipcheck. The
// ONLY thing Refreeze adds over Freeze is the convergence invariant: the reworked
// draft MUST keep the previous workflow's id, and its content Version is re-stamped
// to prev+1. That is what makes structural growth converge on one id instead of
// fanning out into trial-to-ae-routing, trial-to-ae-routing-v2, trial-to-ae-routing-v3.
//
// Refreeze is DISTINCT from the leaf Overlay machinery and must NEVER mint a new
// workflow id. It does not touch the kernel; like Freeze it is a pure gate:
// (prev frozen spec, reworked draft, operator approval) in, a new frozen spec at
// version+1 (plus its passing shipcheck report) or an error out.

// Refreeze errors. Wrapped with %w so callers classify the failure while still
// reading the offending detail.
var (
	// ErrRefreeze is the umbrella error for refreeze failures.
	ErrRefreeze = errors.New("workflowpress: refreeze")
	// ErrRefreezeIDMismatch is returned when the reworked draft's id differs from
	// the previously frozen spec's id. Refreeze converges on the SAME id; a
	// different id is a brand-new contract, reviewed from scratch via Freeze — never
	// a refreeze. This is the invariant that stops structural change from minting a
	// new workflow.
	ErrRefreezeIDMismatch = errors.New("refreeze draft id does not match the previously frozen workflow id")
	// ErrRefreezeShipcheckFailed is returned when the reworked draft does not pass a
	// shipcheck replay. The mechanical proof gate is not waived for a structural
	// change: a structural rework whose own verification scenarios no longer
	// reproduce is rejected, and the previous frozen spec stands unchanged.
	ErrRefreezeShipcheckFailed = errors.New("refreeze draft failed the shipcheck replay")
)

// Refreeze produces a NEW frozen version of the SAME workflow id from a structurally
// reworked draft. It is the structural-change path the leaf Overlay vocabulary
// cannot express (new states/events/actions). It enforces convergence — the draft
// keeps prev's id and is re-stamped to prev's content Version + 1 — and requires the
// full freeze gate plus a passing shipcheck replay before the new contract is frozen.
//
// The order of the gate is deliberate, fail-closed at each step:
//
//  1. CONVERGENCE — the draft's id must equal prev's id. A different id is rejected
//     (ErrRefreezeIDMismatch); structural change can never mint a new workflow.
//  2. RE-STAMP — the candidate's content Version is set to prev.Spec.Version + 1
//     (prefer-update). The operator's FreezeRequest must authorise exactly that
//     (id, version) pair, so an approval cannot be replayed against a different
//     re-stamping. The human review (approve / approve-with-note / reject) is the
//     same gate the original contract passed — Freeze enforces it, including the
//     structural Validate, so a structurally unsound rework cannot be frozen even
//     with an approval.
//  3. SHIPCHECK REPLAY — the candidate is Generated and run through the mechanical
//     proof gate. Only a passing replay yields a frozen spec; a failing one returns
//     the (failing) report and ErrRefreezeShipcheckFailed, and prev stands.
//
// On success it returns the new FrozenSpec at version+1 (a deep copy, independent of
// both prev and the draft) and the passing ShipcheckReport. Refreeze does not mutate
// prev or the draft.
func Refreeze(prev FrozenSpec, draft WorkflowSpec, req FreezeRequest) (FrozenSpec, *ShipcheckReport, error) {
	// (1) Convergence: the rework must keep the previous workflow's id. This is the
	// load-bearing invariant — Refreeze is prefer-update for structural change, so a
	// different id means "new contract", which is Freeze's job, not Refreeze's.
	if strings.TrimSpace(draft.ID) != prev.Spec.ID {
		return FrozenSpec{}, nil, fmt.Errorf(
			"%w: %w: draft id %q but previous frozen workflow is %q",
			ErrRefreeze, ErrRefreezeIDMismatch, draft.ID, prev.Spec.ID,
		)
	}

	// (2) Re-stamp to prev+1 on a clone (prefer-update; same id, version bumped). The
	// draft the caller passed is never mutated.
	cand := cloneSpec(draft)
	cand.Version = prev.Spec.Version + 1

	// The human freeze gate, scoped to the re-stamped (id, version). Freeze runs the
	// structural Validate too, so a structurally unsound rework is rejected here even
	// with an operator approval. A non-approving decision fails closed (ErrNotApproved).
	frozen, err := Freeze(*cand, req)
	if err != nil {
		return FrozenSpec{}, nil, fmt.Errorf("%w: %w", ErrRefreeze, err)
	}

	// (3) Shipcheck replay: the mechanical proof gate is not waived for a structural
	// change. Generate the candidate tool from the frozen spec and replay the
	// contract's own (now structurally-extended) verification scenarios.
	gen, err := Generate(&frozen.Spec)
	if err != nil {
		return FrozenSpec{}, nil, fmt.Errorf("%w: generating candidate: %w", ErrRefreeze, err)
	}
	report, err := RunShipcheck(&frozen.Spec, gen)
	if err != nil {
		return FrozenSpec{}, nil, fmt.Errorf("%w: replaying candidate: %w", ErrRefreeze, err)
	}
	if !report.Passed {
		// The structural rework does not honour its own contract; the previous frozen
		// spec stands unchanged. Return the failing report so the operator sees which
		// mechanical check the rework broke.
		return FrozenSpec{}, report, fmt.Errorf("%w: %w", ErrRefreeze, ErrRefreezeShipcheckFailed)
	}
	return frozen, report, nil
}
