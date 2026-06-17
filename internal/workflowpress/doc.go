// Package workflowpress is the keystone of the Workflow Press: the small,
// protected kernel that turns messy repeated RevOps work into a proven,
// improvable internal tool with a reviewed contract behind it.
//
// The architecture source of truth is docs/specs/workflow-press.md. The two
// load-bearing principles it encodes:
//
//  1. Discovery does not become code. It becomes an evidence-backed IR first.
//     Raw observation (WorkflowResearch) is messy and lossy, so it is distilled
//     into a narrow structured contract (WorkflowSpec) with its evidence
//     attached, and only that contract drives generation, verification, scoring
//     and improvement — deterministically. The model's raw observation is never
//     wired straight into a tool.
//
//  2. Self-improvement lives outside a small, protected kernel. The kernel (the
//     contract schema, the generator, the shipcheck, the runner runtime, the
//     overlay apply/replay/accept machinery) is frozen, reviewed and versioned.
//     Everything mutable (research, observations, operator edits, durable
//     playbooks, proposed overlays) is persisted outside it. Improvements arrive
//     as overlays → reviewed → replayed against fixtures → accepted, never as
//     direct mutations of the kernel.
//
// # The kernel boundary
//
// INSIDE the kernel (this package, frozen + reviewed + versioned):
//
//   - the contract schema — WorkflowResearch, WorkflowSpec and their JSON
//     Schema (contract.go, schema.go). Both artifacts carry an asserted
//     wire-format SchemaVersion (distinct from the content Version counter);
//     Validate fails closed on an unknown/newer version and DecodeSpecStrict is
//     the loud strict loader the generated tool uses;
//   - validation of the contract — WorkflowSpec.Validate (validate.go);
//   - the Generator — deterministic emission of the local workflow from the
//     spec (interface here; implementation in a later phase);
//   - the Shipcheck — the mechanical proof gate (interface here; later phase);
//   - the runner runtime — executes the generated workflow's actions through the
//     Executor seam (executor.go; a host-stub backend only, no live execution);
//   - the OverlayStore — overlay apply/replay/accept machinery (interface here;
//     later phase).
//
// OUTSIDE the kernel (mutable, persisted elsewhere, never the source of truth
// for generation):
//
//   - the research store — append-only raw WorkflowResearch evidence;
//   - live run observations and failures;
//   - operator edits;
//   - proposed overlays awaiting review.
//
// Keep the kernel SMALL. New mutable state belongs outside it; new behaviour
// belongs behind one of the kernel's interfaces, exercised against the
// contract, not bolted into a growing engine.
//
// # Security posture
//
// Generated runners and authored overlay code are HOSTILE BY ASSUMPTION. Live
// execution is gated behind Phase 0's relocatable sandbox (the Executor seam in
// executor.go), security-reviewer review and triangulation. Mutating and
// network actions must route through the office's ExternalActionApprovalCard.
// Trust tier drives caution: inferred/observed write-actions require human
// approval; operator-stated actions may be looser. Nothing downstream ships
// until the execution boundary is proven; this phase ships only the seam and a
// host-stub that refuses every live mutating/network action.
package workflowpress
