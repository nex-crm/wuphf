package workflowpress

import "context"

// kernel.go declares the protected-kernel interfaces. The boundary itself is
// documented in doc.go; this file makes it executable. The kernel is the set of
// behaviours frozen, reviewed and versioned together:
//
//	{ contract schema, Generator, Shipcheck, runner runtime, OverlayStore }
//
// Everything mutable lives outside it. These interfaces are the only seams new
// behaviour may enter through. Each is a stub in this phase; the implementing
// phase is named in each TODO. Accepting interfaces / returning structs keeps
// callers swappable without growing the kernel.

// GeneratedWorkflow is the deterministic output of the Generator: the local
// internal tool emitted from a frozen WorkflowSpec (runner, types, exceptions,
// state, inngest adapter, fixtures, docs, tests). Files maps a relative path to
// its generated contents; nothing here is executed until it clears the Executor
// boundary.
type GeneratedWorkflow struct {
	// WorkflowID and Version pin the output to the exact spec it was generated
	// from, so a shipcheck or overlay replay can prove provenance.
	WorkflowID string
	Version    int
	// Files is the emitted tree (relative path -> contents). Deterministic for a
	// given spec: the same contract must produce byte-identical files.
	Files map[string][]byte
}

// Generator deterministically emits a GeneratedWorkflow from a frozen
// WorkflowSpec. Templated, not LLM-freeform: the same spec must produce
// byte-identical output. INSIDE the kernel.
type Generator interface {
	// Generate emits the local workflow from spec. It must be pure and
	// deterministic with respect to spec.
	Generate(ctx context.Context, spec *WorkflowSpec) (*GeneratedWorkflow, error)
}

// ShipcheckReport is the result of the mechanical proof gate. Passed gates ship;
// a failure blocks the workflow (or an overlay acceptance). Findings carry the
// per-check detail.
type ShipcheckReport struct {
	WorkflowID string
	Version    int
	Passed     bool
	Findings   []ShipcheckFinding
}

// ShipcheckFinding is one check's outcome within a ShipcheckReport. Check names
// the mechanical proof (fixture-replay, transition-coverage, idempotency,
// duplicate-handling, stale-handling, audit-completeness, adapter-parity).
type ShipcheckFinding struct {
	Check  string
	Passed bool
	Detail string
}

// Shipcheck runs the deterministic mechanical proof that a GeneratedWorkflow
// honours its WorkflowSpec before it ships or an overlay is accepted. INSIDE the
// kernel.
type Shipcheck interface {
	// Check proves the generated workflow honours the contract: fixture replay,
	// transition coverage, idempotency, duplicate/stale handling, audit
	// completeness and adapter parity.
	Check(ctx context.Context, spec *WorkflowSpec, gen *GeneratedWorkflow) (*ShipcheckReport, error)
}

// Overlay is a proposed patch to a per-workflow spec (never to the kernel). It
// arrives from outside the kernel — an operator edit or a recurring exception —
// is reviewed, replayed against fixtures by Shipcheck, then accepted (folded in,
// version-bumped). WUPHF prefers updating the existing workflow over creating a
// new one.
type Overlay struct {
	// WorkflowID and BaseVersion name the spec this overlay patches. Apply
	// rejects an overlay whose BaseVersion does not match the live spec.
	WorkflowID  string
	BaseVersion int
	// Origin is the improvement signal that proposed it (operator-edit,
	// recurring-exception, sla-miss), for audit.
	Origin string
	// Patch is the proposed change. Kept opaque here; the implementing phase
	// fixes the patch encoding (e.g. JSON Patch over the spec).
	Patch []byte
}

// OverlayStore is the overlay apply/replay/accept machinery: the only path by
// which a spec changes after freeze. The kernel never mutates; only the
// per-workflow spec + overlays do. INSIDE the kernel (the machinery), while the
// proposed overlays it holds are persisted OUTSIDE it.
type OverlayStore interface {
	// Propose records a proposed overlay for review. Append-only; never applies.
	Propose(ctx context.Context, ov Overlay) error
	// Apply produces a candidate next-version spec from base + overlay WITHOUT
	// accepting it, so Shipcheck can replay the candidate against fixtures.
	Apply(ctx context.Context, base *WorkflowSpec, ov Overlay) (*WorkflowSpec, error)
	// Accept folds a replayed-and-passed overlay into the live spec and bumps
	// the version. Must be called only after Shipcheck passes on the candidate.
	Accept(ctx context.Context, ov Overlay) (*WorkflowSpec, error)
}

// Implementations of these seams:
//
//   - Generator   — generate.go (TemplateGenerator / NewGenerator): deterministic
//     templated emission of the local workflow from the spec.
//   - Shipcheck   — shipcheck.go (MechanicalShipcheck / NewShipcheck): the
//     mechanical-proof gate (fixture replay, transition coverage, idempotency,
//     duplicate/stale handling, audit completeness, adapter parity).
//   - OverlayStore — improvement.go (MemoryOverlayStore / NewOverlayStore): the
//     overlay propose/apply/replay/accept machinery, replaying every candidate
//     through Shipcheck before Accept and preferring update over a new workflow.
