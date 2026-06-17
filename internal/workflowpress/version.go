package workflowpress

import (
	"errors"
	"fmt"
)

// version.go is the kernel-coupling policy. It resolves the two-axis coupling
// flagged by triangulation architect #2: a generated tool both imports this
// kernel (the runner runtime, DecodeSpecStrict, the Executor seam) AND embeds a
// frozen spec, and those two were unversioned with respect to each other. A
// kernel change (a runtime contract change, a template change) could silently
// break a committed generated tool with nothing asserting the two still agree.
//
// The fix stamps BOTH axes into every generated tool and asserts compatibility
// at load:
//
//   - the SPEC axis is already covered: the embedded spec carries SchemaVersion,
//     and DecodeSpecStrict fails closed on an unknown/newer schema_version.
//   - the KERNEL axis is covered here: the generator stamps KernelVersion (plus
//     the SchemaVersion it generated against) into each tool as generated
//     constants, and the tool's loadSpec calls RequireKernelCompat against the
//     KernelVersion of the kernel it actually links. A tool generated against an
//     older kernel, linked into a newer one (or vice versa), fails LOUDLY at load
//     instead of running with a runtime that no longer matches its assumptions.
//
// The policy is regenerate-on-bump (NOT pin): there is exactly one supported
// (KernelVersion, SchemaVersionWorkflowSpec) pair at a time, the committed
// generated output is regenerated whenever either bumps, and the
// regenerate-and-compare drift guard (generated_drift_test.go) fails CI if the
// committed output drifts from what the current kernel emits. See
// docs/specs/workflow-press.md "Generated-tool ↔ kernel coupling policy".

// KernelVersion is the version of the workflowpress KERNEL (the runner runtime,
// the strict loader, the Executor seam, the generator templates) — distinct from
// both SchemaVersionWorkflowSpec (the spec WIRE shape) and a spec's content
// Version counter. It bumps on any change that could alter generated output or
// the runtime contract a generated tool depends on: a template edit, a runner
// behaviour change, a new generated file, a guard-evaluation change.
//
// Because the policy is regenerate-on-bump, bumping KernelVersion requires
// regenerating the committed example tools in the same change (the drift guard
// enforces it) and is itself the signal that every generated tool in the wild
// must be regenerated to stay compatible.
const KernelVersion = 1

// ErrKernelIncompatible is returned by RequireKernelCompat when a generated
// tool's stamped KernelVersion does not match the kernel it is linked against. It
// wraps so callers can errors.Is on it; the generated loadSpec surfaces it as a
// loud load-time failure rather than running on a mismatched runtime.
var ErrKernelIncompatible = errors.New("workflowpress: generated tool kernel-version mismatch")

// RequireKernelCompat asserts a generated tool is compatible with THIS kernel.
// The generated tool calls it from loadSpec, passing the KernelVersion and
// SchemaVersionWorkflowSpec constants the generator stamped into it at generation
// time. Both must match the kernel's current constants:
//
//   - toolKernelVersion != KernelVersion means the tool was generated against a
//     different kernel (different runtime/template assumptions) — fail closed,
//     the tool must be regenerated.
//   - toolSchemaVersion != SchemaVersionWorkflowSpec is a belt-and-braces check
//     that the embedded spec's wire shape matches the kernel decoder; the strict
//     loader also asserts the schema_version FIELD, but stamping the generator's
//     view of it lets a mismatch surface even before decode and pins the
//     generated-vs-kernel axis explicitly.
//
// Equality (not >=) is deliberate: the kernel cannot prove forward OR backward
// compatibility across a bump, so any difference fails closed — the safe failure,
// matching DecodeSpecStrict's fail-closed posture on schema_version.
func RequireKernelCompat(toolKernelVersion, toolSchemaVersion int) error {
	if toolKernelVersion != KernelVersion {
		return fmt.Errorf(
			"%w: tool generated against kernel v%d, this kernel is v%d; regenerate the tool",
			ErrKernelIncompatible, toolKernelVersion, KernelVersion,
		)
	}
	if toolSchemaVersion != SchemaVersionWorkflowSpec {
		return fmt.Errorf(
			"%w: tool generated against spec schema v%d, this kernel expects v%d; regenerate the tool",
			ErrKernelIncompatible, toolSchemaVersion, SchemaVersionWorkflowSpec,
		)
	}
	return nil
}
