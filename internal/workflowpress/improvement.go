package workflowpress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// improvement.go is the improvement loop — the fifth and final press artifact. An
// OVERLAY is a proposed patch to a frozen spec. It arrives from OUTSIDE the kernel
// (an operator edit, a recurring exception, an SLA miss), is REPLAYED against the
// contract's fixtures by Shipcheck, and is ACCEPTED — folded into the spec, the
// version bumped — ONLY if the replay passes.
//
// Two architectural invariants this file enforces:
//
//   - Overlays NEVER mutate the kernel. They patch the per-workflow spec only.
//     Apply produces a CANDIDATE next-version spec without touching the live one;
//     Accept replaces the live spec only after Shipcheck has passed the candidate.
//     The kernel's code (this package's behaviours) is never a patch target.
//   - PREFER-UPDATE over proliferation. Accepting an overlay UPDATES the existing
//     spec in place (same id, version+1). It never mints a new workflow. The press
//     converges a workflow toward correctness; it does not spawn variants.
//
// The patch encoding is a small, typed, declarative set of operations
// (OverlayPatch) carried as JSON in Overlay.Patch. It is deliberately narrow:
// overlays may tune the contract (guards, SLAs, exceptions, improvement signals,
// thresholds) and add verification scenarios, but may NOT restructure the state
// machine wholesale — a structural rewrite is a new contract, reviewed from
// scratch, not an overlay. Apply validates and re-Validates the candidate; an
// overlay that produces an unsound state machine is rejected before it ever
// reaches replay.

// Overlay errors. Wrapped with %w so callers classify the failure class while
// still reading the offending detail.
var (
	// ErrOverlay is the umbrella error for overlay machinery failures.
	ErrOverlay = errors.New("workflowpress: overlay")
	// ErrOverlayBaseMismatch is returned when an overlay's BaseVersion does not
	// match the live spec it patches — an overlay is scoped to exactly the version
	// the proposer reviewed, so a stale overlay cannot apply to a moved-on spec.
	ErrOverlayBaseMismatch = errors.New("overlay base version does not match the live spec")
	// ErrOverlayUnknownWorkflow is returned when an overlay names a workflow the
	// store does not hold.
	ErrOverlayUnknownWorkflow = errors.New("overlay names an unknown workflow")
	// ErrOverlayBadPatch is returned when an overlay's patch does not decode, names
	// an element that does not exist, or would produce an unsound spec.
	ErrOverlayBadPatch = errors.New("overlay patch is malformed or unapplicable")
	// ErrOverlayNotReplayed is returned by Accept when the overlay has not passed a
	// Shipcheck replay — acceptance without proof is forbidden.
	ErrOverlayNotReplayed = errors.New("overlay was not accepted: it has not passed a shipcheck replay")
)

// OverlayOpKind classifies one declarative operation within an OverlayPatch. The
// set is intentionally small: tune the contract's gating/handling/signals, adjust
// a named threshold, or add a verification scenario. It cannot add or remove
// states/events — a structural change is a new contract, not an overlay.
type OverlayOpKind string

const (
	// OpSetGuardExpr rewrites the expression of an existing named guard (e.g.
	// tightening an ICP threshold comparison). The guard must already exist.
	OpSetGuardExpr OverlayOpKind = "set-guard-expr"
	// OpSetSLAThreshold rewrites the threshold of an existing named SLA.
	OpSetSLAThreshold OverlayOpKind = "set-sla-threshold"
	// OpAddException appends a new known exception (a recurring exception promoted
	// into the contract's handling).
	OpAddException OverlayOpKind = "add-exception"
	// OpAddImprovementSignal appends a new improvement signal to watch.
	OpAddImprovementSignal OverlayOpKind = "add-improvement-signal"
	// OpAddVerificationScenario appends a new verification fixture (e.g. codifying a
	// regression an operator hit, so shipcheck guards it forever after).
	OpAddVerificationScenario OverlayOpKind = "add-verification-scenario"
)

// Valid reports whether the op kind is one of the known operations.
func (k OverlayOpKind) Valid() bool {
	switch k {
	case OpSetGuardExpr, OpSetSLAThreshold, OpAddException,
		OpAddImprovementSignal, OpAddVerificationScenario:
		return true
	default:
		return false
	}
}

// OverlayOp is a single declarative change. Only the fields relevant to Kind are
// read; the rest are ignored. Target names the element (a guard/SLA name) the op
// addresses where applicable.
type OverlayOp struct {
	Kind   OverlayOpKind `json:"kind"`
	Target string        `json:"target,omitempty"`
	// Value is the new scalar value for a set-* op (a guard expr, an SLA
	// threshold).
	Value string `json:"value,omitempty"`
	// Exception / Signal / Scenario carry the appended element for the add-* ops.
	Exception *Exception            `json:"exception,omitempty"`
	Signal    *ImprovementSignal    `json:"signal,omitempty"`
	Scenario  *VerificationScenario `json:"scenario,omitempty"`
	// Provenance is the trust tier of the change (an operator edit is
	// operator-stated; a recurring-exception promotion is inferred). Applied to the
	// element the op touches so the contract keeps its evidence trail.
	Provenance Provenance `json:"provenance"`
}

// OverlayPatch is the declarative body of an Overlay: an ordered list of ops
// applied in sequence to a copy of the live spec. It is the encoding carried as
// JSON in Overlay.Patch.
type OverlayPatch struct {
	Ops []OverlayOp `json:"ops"`
}

// EncodeOverlayPatch marshals a patch to the JSON bytes an Overlay carries. It is
// the canonical encoder so a proposer does not hand-roll the wire shape.
func EncodeOverlayPatch(p OverlayPatch) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding patch: %w", ErrOverlay, err)
	}
	return b, nil
}

// decodeOverlayPatch parses an overlay's patch bytes into a typed patch, rejecting
// a malformed encoding or an unknown op kind up front (so a bad op never reaches
// the spec).
func decodeOverlayPatch(raw []byte) (OverlayPatch, error) {
	var p OverlayPatch
	if len(raw) == 0 {
		return p, fmt.Errorf("%w: %w: empty patch", ErrOverlay, ErrOverlayBadPatch)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("%w: %w: decoding patch: %w", ErrOverlay, ErrOverlayBadPatch, err)
	}
	if len(p.Ops) == 0 {
		return p, fmt.Errorf("%w: %w: patch has no ops", ErrOverlay, ErrOverlayBadPatch)
	}
	for i, op := range p.Ops {
		if !op.Kind.Valid() {
			return p, fmt.Errorf("%w: %w: op %d has unknown kind %q", ErrOverlay, ErrOverlayBadPatch, i, op.Kind)
		}
	}
	return p, nil
}

// applyPatch folds a decoded patch into a CLONE of base, returning the candidate
// next-version spec WITHOUT mutating base. The version is bumped here so the
// candidate is already at its target version when Shipcheck replays it. The result
// is re-Validated by the caller (Apply); applyPatch itself only performs the
// structural edits and surfaces an op that targets a missing element.
func applyPatch(base *WorkflowSpec, patch OverlayPatch) (*WorkflowSpec, error) {
	cand := cloneSpec(*base)
	cand.Version = base.Version + 1 // prefer-update: same id, version+1

	for i, op := range patch.Ops {
		if err := applyOp(cand, op); err != nil {
			return nil, fmt.Errorf("%w: %w: op %d (%s): %w", ErrOverlay, ErrOverlayBadPatch, i, op.Kind, err)
		}
	}
	return cand, nil
}

// applyOp applies one op to the candidate spec in place. It returns an error if the
// op targets an element that does not exist (a set-* on an unknown name) — an
// overlay must address real contract elements.
func applyOp(cand *WorkflowSpec, op OverlayOp) error {
	switch op.Kind {
	case OpSetGuardExpr:
		for i := range cand.Guards {
			if cand.Guards[i].Name == op.Target {
				cand.Guards[i].Expr = op.Value
				cand.Guards[i].Provenance = op.Provenance
				return nil
			}
		}
		return fmt.Errorf("no guard named %q", op.Target)
	case OpSetSLAThreshold:
		for i := range cand.SLAs {
			if cand.SLAs[i].Name == op.Target {
				cand.SLAs[i].Threshold = op.Value
				cand.SLAs[i].Provenance = op.Provenance
				return nil
			}
		}
		return fmt.Errorf("no sla named %q", op.Target)
	case OpAddException:
		if op.Exception == nil {
			return errors.New("add-exception op carries no exception")
		}
		ex := *op.Exception
		ex.Provenance = op.Provenance
		cand.Exceptions = append(cand.Exceptions, ex)
		return nil
	case OpAddImprovementSignal:
		if op.Signal == nil {
			return errors.New("add-improvement-signal op carries no signal")
		}
		cand.ImprovementSignals = append(cand.ImprovementSignals, *op.Signal)
		return nil
	case OpAddVerificationScenario:
		if op.Scenario == nil {
			return errors.New("add-verification-scenario op carries no scenario")
		}
		cand.VerificationScenarios = append(cand.VerificationScenarios, *op.Scenario)
		return nil
	default:
		// decodeOverlayPatch already rejects unknown kinds; this is defence in depth.
		return fmt.Errorf("unknown op kind %q", op.Kind)
	}
}

// --- the OverlayStore: propose / apply / replay / accept ---

// compile-time assertion: the in-memory store satisfies the kernel's OverlayStore
// seam, so callers can hold the interface (accept interfaces) while the concrete
// store returns structs.
var _ OverlayStore = (*MemoryOverlayStore)(nil)

// MemoryOverlayStore is the overlay apply/replay/accept machinery backed by
// in-memory state. The MACHINERY is inside the kernel (it is the only sanctioned
// path by which a spec changes after freeze); the PROPOSED overlays it holds are
// outside-the-kernel mutable state, persisted here for the lifetime of the store.
//
// It holds the live frozen specs (keyed by id), the proposed overlays awaiting
// review, and the Shipcheck gate every candidate must clear before Accept. It is
// safe for concurrent use.
type MemoryOverlayStore struct {
	mu        sync.Mutex
	specs     map[string]*WorkflowSpec
	proposed  map[string][]Overlay
	replayed  map[string]*WorkflowSpec // candidate keyed by overlayKey, set once replay passes
	shipcheck Shipcheck
	generator Generator
}

// NewOverlayStore builds an overlay store seeded with the live specs it governs.
// It uses the production Shipcheck and Generator; pass overrides via
// newOverlayStoreWith for tests. The seeded specs are deep-copied so the store
// owns its state and a caller cannot mutate a live spec behind its back.
func NewOverlayStore(specs ...*WorkflowSpec) *MemoryOverlayStore {
	return newOverlayStoreWith(NewShipcheck(), NewGenerator(), specs...)
}

// newOverlayStoreWith builds a store with explicit kernel seams (for tests that
// inject a defect-catching or stub shipcheck/generator).
func newOverlayStoreWith(sc Shipcheck, gen Generator, specs ...*WorkflowSpec) *MemoryOverlayStore {
	s := &MemoryOverlayStore{
		specs:     make(map[string]*WorkflowSpec, len(specs)),
		proposed:  make(map[string][]Overlay),
		replayed:  make(map[string]*WorkflowSpec),
		shipcheck: sc,
		generator: gen,
	}
	for _, sp := range specs {
		if sp == nil {
			continue
		}
		s.specs[sp.ID] = cloneSpec(*sp)
	}
	return s
}

// Spec returns a deep copy of the live spec for id, so a caller can read it
// without reaching into the store's state. ok is false when the workflow is
// unknown.
func (s *MemoryOverlayStore) Spec(id string) (*WorkflowSpec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.specs[id]
	if !ok {
		return nil, false
	}
	return cloneSpec(*sp), true
}

// Propose records a proposed overlay for review. It is append-only and never
// applies; it validates the overlay's shape (decodable patch, matching workflow)
// so a malformed overlay is rejected at the door rather than at accept time. The
// base-version check is deferred to Apply, which is where the candidate is built —
// a proposal may legitimately sit in the queue while the operator reviews it.
func (s *MemoryOverlayStore) Propose(ctx context.Context, ov Overlay) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: propose context: %w", ErrOverlay, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.specs[ov.WorkflowID]; !ok {
		return fmt.Errorf("%w: %w: %q", ErrOverlay, ErrOverlayUnknownWorkflow, ov.WorkflowID)
	}
	if _, err := decodeOverlayPatch(ov.Patch); err != nil {
		return err
	}
	s.proposed[ov.WorkflowID] = append(s.proposed[ov.WorkflowID], ov)
	return nil
}

// Apply produces a CANDIDATE next-version spec from the live spec + overlay,
// WITHOUT accepting it. It is the replay input: a caller (or AcceptIfProven) hands
// the candidate to Shipcheck against the candidate's own fixtures before any
// acceptance. Apply does not mutate the live spec.
//
// It enforces base-version scoping (a stale overlay against a moved-on spec is
// rejected), decodes and folds the patch, and re-Validates the candidate so an
// overlay that produces an unsound state machine fails here, before replay.
func (s *MemoryOverlayStore) Apply(ctx context.Context, base *WorkflowSpec, ov Overlay) (*WorkflowSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: apply context: %w", ErrOverlay, err)
	}
	if base == nil {
		return nil, fmt.Errorf("%w: %w: base spec is nil", ErrOverlay, ErrEmptyField)
	}
	if ov.WorkflowID != base.ID {
		return nil, fmt.Errorf("%w: %w: overlay names %q but base is %q", ErrOverlay, ErrOverlayUnknownWorkflow, ov.WorkflowID, base.ID)
	}
	if ov.BaseVersion != base.Version {
		return nil, fmt.Errorf(
			"%w: %w: overlay targets version %d but live spec is version %d",
			ErrOverlay, ErrOverlayBaseMismatch, ov.BaseVersion, base.Version,
		)
	}
	patch, err := decodeOverlayPatch(ov.Patch)
	if err != nil {
		return nil, err
	}
	cand, err := applyPatch(base, patch)
	if err != nil {
		return nil, err
	}
	// An overlay that produces an unsound or unsafe state machine is rejected before
	// it can ever be replayed or accepted — the structural half of the gate still
	// applies to a patched contract.
	if err := cand.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w: candidate spec is invalid: %w", ErrOverlay, ErrOverlayBadPatch, err)
	}
	return cand, nil
}

// Accept folds a replayed-and-passed overlay into the live spec and bumps the
// version. It MUST be called only after a Shipcheck replay of the candidate has
// passed; it refuses an overlay it has not seen pass a replay (ErrOverlayNot
// Replayed). On success it UPDATES the existing spec in place (prefer-update) and
// returns a copy of the new live spec.
//
// Accept is the only mutation point. It re-checks base-version scoping against the
// CURRENT live spec (a concurrent accept may have moved it on) so two overlays
// racing on the same version cannot both land.
//
// The replayed-candidate entry is CONSUMED on every path past the not-replayed
// guard: a race-loser whose base version no longer matches (or whose workflow
// vanished) had its candidate recorded by AcceptIfProven, and that candidate is now
// stale — leaving it in the replayed map would leak it forever and could let a
// later, unrelated Accept on the same key reuse a stale candidate. A deferred
// delete keyed on the looked-up entry removes it whether Accept succeeds or fails
// the version recheck, while ErrOverlayNotReplayed still fires (without consuming
// anything) when no entry exists.
func (s *MemoryOverlayStore) Accept(ctx context.Context, ov Overlay) (*WorkflowSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: accept context: %w", ErrOverlay, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := overlayKey(ov)
	cand, ok := s.replayed[key]
	if !ok {
		// No proven candidate: nothing to consume, and acceptance without proof is
		// forbidden. Return before any deletion so the not-replayed guard is pure.
		return nil, fmt.Errorf("%w: %w", ErrOverlay, ErrOverlayNotReplayed)
	}
	// The candidate is consumed from here on, whatever the outcome of the version
	// recheck below — so it can never be left as a stale entry on a race-loser path.
	defer delete(s.replayed, key)

	live, ok := s.specs[ov.WorkflowID]
	if !ok {
		return nil, fmt.Errorf("%w: %w: %q", ErrOverlay, ErrOverlayUnknownWorkflow, ov.WorkflowID)
	}
	if ov.BaseVersion != live.Version {
		return nil, fmt.Errorf(
			"%w: %w: overlay targets version %d but live spec is now version %d",
			ErrOverlay, ErrOverlayBaseMismatch, ov.BaseVersion, live.Version,
		)
	}
	// Prefer-update: replace the live spec in place at the bumped version. Same id,
	// no new workflow.
	s.specs[ov.WorkflowID] = cloneSpec(*cand)
	s.removeProposed(ov)
	return cloneSpec(*cand), nil
}

// AcceptIfProven is the end-to-end overlay loop: apply the overlay to the live
// spec, GENERATE the candidate tool, REPLAY it through Shipcheck, and accept it
// ONLY if the replay passes. It is the sanctioned one-call path the press uses; a
// caller that wants the steps individually uses Apply + Shipcheck + Accept.
//
// On a failing replay it returns the (failing) ShipcheckReport and a nil spec, and
// the live spec is UNCHANGED — the kernel never accepts an unproven overlay. On a
// passing replay it accepts and returns the new live spec with the passing report.
func (s *MemoryOverlayStore) AcceptIfProven(ctx context.Context, ov Overlay) (*WorkflowSpec, *ShipcheckReport, error) {
	live, ok := s.Spec(ov.WorkflowID)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %w: %q", ErrOverlay, ErrOverlayUnknownWorkflow, ov.WorkflowID)
	}
	cand, err := s.Apply(ctx, live, ov)
	if err != nil {
		return nil, nil, err
	}
	gen, err := s.generator.Generate(ctx, cand)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: generating candidate: %w", ErrOverlay, err)
	}
	report, err := s.shipcheck.Check(ctx, cand, gen)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: replaying candidate: %w", ErrOverlay, err)
	}
	if !report.Passed {
		// Rejected on replay: the live spec is untouched and the candidate is NOT
		// recorded as replayed, so a later Accept cannot sneak it in.
		return nil, report, nil
	}
	// Replay passed: record the candidate and accept it.
	s.mu.Lock()
	s.replayed[overlayKey(ov)] = cand
	s.mu.Unlock()

	newSpec, err := s.Accept(ctx, ov)
	if err != nil {
		return nil, report, err
	}
	return newSpec, report, nil
}

// removeProposed drops a now-accepted overlay from the proposed queue. Caller holds
// the lock. Matching is by overlay key (workflow + base version + origin + patch
// bytes) so an identical re-proposal is not silently consumed.
func (s *MemoryOverlayStore) removeProposed(ov Overlay) {
	queue := s.proposed[ov.WorkflowID]
	key := overlayKey(ov)
	kept := queue[:0]
	for _, q := range queue {
		if overlayKey(q) != key {
			kept = append(kept, q)
		}
	}
	if len(kept) == 0 {
		delete(s.proposed, ov.WorkflowID)
		return
	}
	s.proposed[ov.WorkflowID] = kept
}

// Proposed returns a copy of the proposed-overlay queue for a workflow, for an
// operator review surface. The slice is a copy; the overlays themselves are value
// copies of small structs (the Patch byte slice is shared read-only).
func (s *MemoryOverlayStore) Proposed(id string) []Overlay {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.proposed[id]
	out := make([]Overlay, len(queue))
	copy(out, queue)
	return out
}

// overlayKey is the stable identity of an overlay (workflow + base version +
// origin + patch bytes). Two overlays with the same key are the same proposal.
//
// It hashes the canonical JSON encoding of the identity fields rather than
// concatenating them with separators. A naive separator scheme ("@","/","#")
// collides: an unescaped separator in one field can be absorbed into an adjacent
// field so two distinct overlays produce the same key (e.g. Origin "x#y" with an
// empty patch vs Origin "x" with patch "#y", or a "/" in WorkflowID vs Origin).
// JSON-encoding each field length-delimits and escapes it, so the hash is
// collision-resistant: distinct overlays cannot map to the same key.
func overlayKey(ov Overlay) string {
	identity := struct {
		WorkflowID  string `json:"workflow_id"`
		BaseVersion int    `json:"base_version"`
		Origin      string `json:"origin"`
		Patch       []byte `json:"patch"`
	}{
		WorkflowID:  ov.WorkflowID,
		BaseVersion: ov.BaseVersion,
		Origin:      ov.Origin,
		Patch:       ov.Patch,
	}
	// json.Marshal of this fixed-field struct cannot fail (all fields are
	// marshallable scalars / []byte), so the error is unreachable; fall back to a
	// best-effort key if it ever does rather than panicking in the kernel.
	b, err := json.Marshal(identity)
	if err != nil {
		return ov.WorkflowID + "@" + fmt.Sprint(ov.BaseVersion) + "/" + ov.Origin
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
