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
//   - the coupling policy — KernelVersion and RequireKernelCompat (version.go).
//     A generated tool is coupled to the kernel on TWO axes (it imports the
//     runtime AND embeds a spec); each is stamped into the tool and asserted at
//     load. The policy is regenerate-on-bump: one supported (KernelVersion,
//     SchemaVersionWorkflowSpec) pair, the committed generated golden regenerated
//     whenever either bumps, and a byte-level drift guard
//     (TestGeneratedOutputMatchesCommitted) failing CI on un-regenerated drift;
//   - validation of the contract — WorkflowSpec.Validate (validate.go);
//   - the Generator — deterministic emission of the local workflow from the
//     spec (interface here; implementation in a later phase);
//   - the Shipcheck — the mechanical proof gate (interface here; later phase);
//   - the synthesis MACHINERY — Synthesize plus the carry/degrade logic
//     (synthesize.go), which fuses evidence with an INJECTED per-workflow
//     blueprint. The kernel holds the machinery and the BlueprintRegistry seam,
//     NOT the per-workflow blueprints;
//   - the runner runtime — executes the generated workflow's actions through the
//     Executor seam (executor.go; a host-stub backend only, no live execution).
//     Its DefaultGuardEvaluator carries NO per-workflow constants: it resolves
//     named thresholds and fixture aliases from the spec's GuardConfig, wired in
//     by NewRunner;
//   - the OverlayStore — overlay apply/replay/accept machinery (interface here;
//     later phase).
//
// OUTSIDE the kernel (mutable or per-workflow, persisted/registered elsewhere,
// never hardcoded in the kernel):
//
//   - the research store — append-only raw WorkflowResearch evidence;
//   - live run observations and failures;
//   - operator edits;
//   - proposed overlays awaiting review;
//   - the PER-WORKFLOW domain knowledge — the synthesis blueprints (the
//     state-machine skeletons) and the guard thresholds/aliases each carries.
//     These are injected through the BlueprintRegistry seam (an implementation
//     such as RevOpsRegistry, defined in revops_workflows.go, holds the data) and
//     ride onto each contract's GuardConfig. Adding a new workflow registers a
//     blueprint with a registry and edits NO kernel file.
//
// Keep the kernel SMALL — and FROZEN per workflow. Per-workflow data lives in an
// injected registry and on the spec, so the kernel does not grow as workflows are
// added. New mutable state belongs outside it; new behaviour belongs behind one of
// the kernel's interfaces, exercised against the contract, not bolted into a
// growing engine.
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
