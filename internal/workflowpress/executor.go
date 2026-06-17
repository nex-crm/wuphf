package workflowpress

import (
	"context"
	"errors"
	"fmt"
)

// executor.go is the Phase 0 execution seam: the isolation boundary the
// Workflow Press does not yet have at the process level (the existing iframe
// sandbox covers UI Apps only).
//
// ============================ SECURITY ============================
//
// Generated runners and authored overlay code are HOSTILE BY ASSUMPTION. Treat
// every action they ask to run as adversarial input.
//
//   - NO LIVE EXECUTION ships in this phase. The only backend here, hostExecutor,
//     is a STUB that refuses every mutating or network action with a clear "not
//     authorized for live execution" error. It exists to prove the seam, not to
//     run anything.
//   - Live execution is gated behind security-reviewer review AND triangulation
//     (multiple orthogonal-lens sub-agents) before any generated or authored
//     code runs in a real backend (host -> container -> micro-VM).
//   - Mutating and network actions MUST route through the office's
//     ExternalActionApprovalCard (a human approval gate) before they reach a
//     backend. The Executor is downstream of approval, never a bypass for it.
//   - Trust tier drives caution: inferred/observed write-actions require human
//     approval; operator-stated actions may be looser, but the ApprovalRequired
//     decision lives with the caller's policy, not inside a backend.
//   - ExecConfig carries filesystem and network ALLOW-LISTS plus resource caps.
//     A backend must deny by default: anything not explicitly allow-listed is
//     refused. An empty allow-list means "deny all", never "allow all".
//
// =================================================================

// ExecAction is a single action a generated runner asks the Executor to perform.
// It is the unit the approval gate, the allow-lists and the resource caps all
// reason about.
type ExecAction struct {
	// Name is the spec action name (audit anchor).
	Name string
	// Kind classifies the effect; Mutates/Network are derived from it.
	Kind ActionKind
	// Target is the system/host/path the action touches; checked against the
	// allow-lists.
	Target string
	// Payload is the opaque action body. Treated as hostile; never executed in
	// this phase.
	Payload []byte
}

// Mutates reports whether the action changes state and therefore requires
// approval before any backend may run it.
func (a ExecAction) Mutates() bool { return a.Kind.IsWrite() }

// NetCap names a single network egress the workflow is permitted to reach. Host
// is matched exactly (no wildcards in this phase — explicit is safer).
type NetCap struct {
	Host  string
	Ports []int
}

// FSCap names a single filesystem path the workflow may touch and whether writes
// are permitted there.
type FSCap struct {
	Path  string
	Write bool
}

// ExecConfig bounds what a backend may do. Allow-lists are deny-by-default: an
// action whose target is not covered by FS/Net is refused. ResourceCaps bound
// CPU/memory/wallclock. WorkflowID/Version anchor every decision to a frozen
// spec for audit.
type ExecConfig struct {
	WorkflowID string
	Version    int
	// FS is the filesystem allow-list. Empty == deny all filesystem access.
	FS []FSCap
	// Net is the network allow-list. Empty == deny all network access.
	//
	// !!! DECLARED BUT NOT YET ENFORCED !!!
	// The host-stub backend refuses ALL network actions outright in this phase, so
	// Net is never consulted: targetAllowed() below checks ONLY the FS allow-list.
	// Populating Net therefore does NOT grant any egress today — it is a forward-
	// declared capability shape, not an enforcement point. The productization
	// backend (container/micro-VM) that actually performs network I/O MUST wire Net
	// enforcement (match host+port against this list, deny by default) before any
	// live egress, OR this field must be removed. Do not assume setting Net opens a
	// hole; assume the opposite until a reviewed backend enforces it.
	Net []NetCap
	// Resource caps. Zero means "use the backend's safe default", which must
	// itself be bounded — never unlimited.
	MaxCPUMillis  int
	MaxMemoryMB   int
	MaxWallMillis int
	// ApprovalGranted records that the action already cleared the
	// ExternalActionApprovalCard. A backend MUST refuse a mutating/network
	// action when this is false. The seam never grants approval itself.
	ApprovalGranted bool
}

// ExecResult is the outcome of an action a backend actually ran. In this phase
// no backend runs anything, so this type exists for the seam's shape only.
type ExecResult struct {
	Output   []byte
	ExitCode int
}

// Executor is the relocatable execution seam: host -> container -> micro-VM
// backends share this interface so the runner runtime is backend-agnostic.
// Backends are deny-by-default and downstream of the approval gate. INSIDE the
// kernel (the seam); the backends themselves are reviewed per Phase 0.
type Executor interface {
	// Backend names the backend ("host-stub", "container", "microvm") for audit.
	Backend() string
	// Execute runs one action under cfg, or refuses it. A backend MUST refuse
	// any mutating/network action when cfg.ApprovalGranted is false or the
	// target is not allow-listed.
	Execute(ctx context.Context, cfg ExecConfig, action ExecAction) (*ExecResult, error)
}

// ErrNotAuthorized is returned by hostExecutor (and any backend) when an action
// is refused: mutating/network without approval, target outside the allow-list,
// or — in this phase — any live mutating/network action at all.
var ErrNotAuthorized = errors.New("workflowpress: not authorized for live execution")

// hostExecutor is the ONLY backend shipped in this phase, and it is a STUB. It
// runs nothing. Any mutating or network action is refused with ErrNotAuthorized;
// a pure read with no network/filesystem target returns an empty result so the
// seam is exercisable in tests without authorizing real I/O.
//
// hostExecutor deliberately holds no state and reaches no real host resource. It
// is the floor of the host -> container -> micro-VM ladder, present only to
// prove the seam and to fail closed until a reviewed backend replaces it.
type hostExecutor struct{}

// NewHostExecutor returns the host-stub backend. It never performs live
// execution; see the SECURITY block above.
func NewHostExecutor() Executor { return hostExecutor{} }

// Backend identifies this stub in audit trails.
func (hostExecutor) Backend() string { return "host-stub" }

// Execute refuses every mutating or network action, and refuses any action
// whose approval has not been granted. It is fail-closed by construction: the
// only non-error path is a pure read that touches no network and whose
// filesystem target (if any) is allow-listed — and even then it performs no real
// I/O in this phase.
func (hostExecutor) Execute(ctx context.Context, cfg ExecConfig, action ExecAction) (*ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("workflowpress: executor context: %w", err)
	}

	// Fail closed on an UNKNOWN kind. ActionKind.IsWrite/Mutates fail OPEN for an
	// unrecognised kind (they return false, classifying it as a read), so a write
	// smuggled in under a garbage kind string would otherwise slip to the success
	// path. Allow-list, never deny-list: reject anything that is not a known kind
	// before Mutates is ever consulted. This mirrors the guard Validate applies to
	// the contract.
	if !action.Kind.Valid() {
		return nil, fmt.Errorf(
			"%w: backend %q refuses action %q: unknown action kind %q",
			ErrNotAuthorized, hostExecutor{}.Backend(), action.Name, action.Kind,
		)
	}

	// Fail closed: any state-changing action is refused outright in this phase.
	// Read ApprovalGranted explicitly so the documented invariant — a backend MUST
	// refuse a mutating action when approval is absent — is enforced in code and a
	// future reviewed backend inherits it. Even with approval, no live backend
	// ships this phase, so the action is still refused.
	if action.Mutates() {
		if !cfg.ApprovalGranted {
			return nil, fmt.Errorf(
				"%w: backend %q refuses mutating action %q (kind %s): ExternalActionApprovalCard approval not granted",
				ErrNotAuthorized, hostExecutor{}.Backend(), action.Name, action.Kind,
			)
		}
		return nil, fmt.Errorf(
			"%w: backend %q refuses mutating action %q (kind %s) even with approval; no live backend ships this phase — route through a reviewed backend",
			ErrNotAuthorized, hostExecutor{}.Backend(), action.Name, action.Kind,
		)
	}

	// A network read is still a network action; deny it without an explicit
	// allow-list entry. In this stub we have no real network, so any non-empty
	// target that is not filesystem-allow-listed is refused.
	if !targetAllowed(cfg, action.Target) {
		return nil, fmt.Errorf(
			"%w: backend %q refuses action %q: target %q not in allow-list",
			ErrNotAuthorized, hostExecutor{}.Backend(), action.Name, action.Target,
		)
	}

	// Pure, allow-listed read: return an empty result. The stub performs no real
	// I/O — it only proves the seam accepts a safe shape.
	return &ExecResult{ExitCode: 0}, nil
}

// targetAllowed reports whether a read action's target is covered by the
// filesystem allow-list. Deny-by-default: an empty allow-list, or a target not
// listed, is refused. An empty target (no external resource touched) is allowed.
//
// NOTE: this checks ONLY cfg.FS. The network allow-list (cfg.Net) is DECLARED BUT
// NOT ENFORCED here — see the warning on ExecConfig.Net. The host-stub refuses all
// network actions upstream, so a network target never reaches a Net check at all.
// A productization backend that performs live egress MUST add Net enforcement here
// (or wherever it routes network actions) before trusting cfg.Net to bound it.
func targetAllowed(cfg ExecConfig, target string) bool {
	if target == "" {
		return true
	}
	for _, fs := range cfg.FS {
		if fs.Path == target {
			return true
		}
	}
	return false
}
