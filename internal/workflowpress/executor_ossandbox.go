package workflowpress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// executor_ossandbox.go is the Phase 0 OS-NATIVE sandbox backend for the
// Executor seam. It is modeled on what the pi coding agent uses
// (@anthropic-ai/sandbox-runtime: sandbox-exec/Seatbelt on macOS, bubblewrap on
// Linux) but implemented NATIVELY in Go, with NO Node dependency.
//
// ============================ SECURITY ============================
//
// This backend does REAL execution, so it is OFF BY DEFAULT. The package default
// stays the host-stub (NewHostExecutor); a caller must explicitly opt in via
// NewOSSandboxExecutor. Enabling live execution requires security-reviewer
// sign-off (see docs/specs/workflow-press.md, Phase 0).
//
// It keeps EVERY gate the host-stub enforces, in the same order, BEFORE it ever
// spawns a process:
//
//   - unknown ActionKind is refused (allow-list, fail closed) — IsWrite/Mutates
//     fail OPEN for an unrecognised kind, so Valid() is checked first;
//   - a mutating action is refused unless cfg.ApprovalGranted is true (the seam
//     never self-approves; gated writes route through ExternalActionApprovalCard
//     upstream);
//   - a read whose target is not in the FS allow-list is refused (deny by
//     default; empty allow-list == deny all);
//   - an unsupported platform (not darwin/linux) is refused with ErrNotAuthorized
//     — it NEVER silently runs unsandboxed, because a backend that runs without a
//     boundary is a FALSE boundary, which is worse than no backend at all.
//
// THREAT MODEL (honest): an OS sandbox is a FILESYSTEM + NETWORK boundary, not
// kernel isolation. Seatbelt and bubblewrap both run the payload on the host
// kernel; a kernel-level escape (a 0-day in a syscall the profile still allows)
// is out of scope for this tier. The stronger opt-in tiers behind the SAME seam
// are a container and a micro-VM. Domain-level egress allow-listing is deferred:
// this backend supports DENY-ALL-NETWORK robustly and treats any Net allow-list
// entry as REQUIRING APPROVAL plus a proxy follow-up — it never silently allows
// all network (see ErrNetworkAllowlistUnsupported).
//
// =================================================================

// ErrSandboxUnavailable is returned when the platform sandbox tool the backend
// needs (sandbox-exec on darwin, bwrap on linux) is not installed. The backend
// FAILS CLOSED: it refuses the action rather than running it unsandboxed.
var ErrSandboxUnavailable = errors.New("workflowpress: os sandbox unavailable")

// ErrNetworkAllowlistUnsupported is returned when cfg.Net is non-empty. Domain-
// level egress allow-listing needs a filtering proxy this backend does not yet
// ship, so rather than silently widen the boundary to allow-all network, the
// backend refuses. Network egress in this tier is DENY-ALL only; a Net entry is
// a documented follow-up (a proxy), not a capability this backend grants.
var ErrNetworkAllowlistUnsupported = errors.New("workflowpress: network allow-list requires a proxy (deferred); only deny-all-network is supported")

// ErrEmptyArgv is returned when an action's Payload does not decode to a non-empty
// argv. The OS-sandbox backend runs a real process, so it needs an explicit
// command vector; it never guesses one.
var ErrEmptyArgv = errors.New("workflowpress: os sandbox action carries no argv")

const (
	// sandboxExecPath is the macOS Seatbelt wrapper. Apple-deprecated but still
	// functional and what @anthropic-ai/sandbox-runtime drives on darwin.
	sandboxExecPath = "/usr/bin/sandbox-exec"
	// bwrapBinary is the Linux bubblewrap wrapper. Looked up on PATH.
	bwrapBinary = "bwrap"
	// defaultWallMillis bounds a single action when cfg.MaxWallMillis is unset.
	// Zero must never mean "unlimited" — a runaway action is a denial-of-service.
	defaultWallMillis = 30_000
)

// commandRunner abstracts process spawning so tests can assert the EXACT wrapped
// argv (sandbox-exec/bwrap + the inner command) without spawning, and so the
// live adversarial tests can run the real thing. The default is execRunner.
type commandRunner interface {
	// run executes argv[0] with argv[1:] under ctx and returns combined output
	// and the process exit code. A non-nil error with exitCode set to the
	// process code means the process ran and failed; a non-nil error with a
	// negative exitCode means it never started.
	run(ctx context.Context, argv []string) (output []byte, exitCode int, err error)
}

// osSandboxExecutor runs an action's argv wrapped by the host OS sandbox. It is
// the floor of the REAL-execution ladder (os-sandbox -> container -> micro-VM)
// and is OFF BY DEFAULT; the package default stays the host-stub.
type osSandboxExecutor struct {
	// goos is the target platform; defaults to runtime.GOOS. Injectable so the
	// profile/args generators are unit-testable on any host.
	goos string
	// runner spawns the wrapped process; defaults to execRunner.
	runner commandRunner
	// lookPath resolves a binary on PATH; defaults to exec.LookPath. Injectable so
	// the fail-closed "tool missing" path is testable without mutating PATH.
	lookPath func(string) (string, error)
}

// NewOSSandboxExecutor returns the OS-native sandbox backend. It does REAL
// execution and is the explicit opt-in for live runs; the package default
// (NewHostExecutor) never executes. Enabling this backend in a real workflow run
// requires security-reviewer sign-off — see the SECURITY block above and
// docs/specs/workflow-press.md.
func NewOSSandboxExecutor() Executor {
	return &osSandboxExecutor{
		goos:     goruntime.GOOS,
		runner:   execRunner{},
		lookPath: exec.LookPath,
	}
}

// Backend identifies this backend in audit trails.
func (*osSandboxExecutor) Backend() string { return "os-sandbox" }

// Execute applies every host-stub gate, then runs the action's argv wrapped by
// the platform sandbox. It is fail-closed by construction: an unknown kind, an
// un-approved mutation, an unlisted target, a non-empty Net allow-list, an
// unsupported platform, or a missing sandbox tool all refuse BEFORE any process
// is spawned.
func (e *osSandboxExecutor) Execute(ctx context.Context, cfg ExecConfig, action ExecAction) (*ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("workflowpress: executor context: %w", err)
	}

	// Gate 1: fail closed on an UNKNOWN kind. Mutates() fails OPEN for an
	// unrecognised kind (classifies it as a read), so reject anything that is not
	// a known kind BEFORE Mutates is consulted — the same allow-list guard the
	// host-stub applies.
	if !action.Kind.Valid() {
		return nil, fmt.Errorf(
			"%w: backend %q refuses action %q: unknown action kind %q",
			ErrNotAuthorized, e.Backend(), action.Name, action.Kind,
		)
	}

	// Gate 2: a state-changing action requires approval. The seam never grants
	// approval itself; an inferred/observed write must clear the
	// ExternalActionApprovalCard upstream and arrive with ApprovalGranted=true.
	if action.Mutates() && !cfg.ApprovalGranted {
		return nil, fmt.Errorf(
			"%w: backend %q refuses mutating action %q (kind %s): ExternalActionApprovalCard approval not granted",
			ErrNotAuthorized, e.Backend(), action.Name, action.Kind,
		)
	}

	// Gate 3: deny-by-default target. A read (or an approved write) whose target
	// is not covered by the FS allow-list is refused. Empty allow-list == deny all.
	if !targetAllowed(cfg, action.Target) {
		return nil, fmt.Errorf(
			"%w: backend %q refuses action %q: target %q not in allow-list",
			ErrNotAuthorized, e.Backend(), action.Name, action.Target,
		)
	}

	// Gate 4: network egress. Domain-level allow-listing needs a proxy this
	// backend does not ship, so a non-empty Net is refused rather than silently
	// widened to allow-all. Egress is DENY-ALL in this tier.
	if len(cfg.Net) > 0 {
		return nil, fmt.Errorf(
			"%w: backend %q refuses action %q: %d network allow-list entr(y/ies) present",
			ErrNetworkAllowlistUnsupported, e.Backend(), action.Name, len(cfg.Net),
		)
	}

	// Gate 5: unsupported platform. NEVER run unsandboxed — a backend without a
	// boundary is a FALSE boundary. Only darwin/linux have a sandbox wrapper here.
	switch e.goos {
	case "darwin", "linux":
	default:
		return nil, fmt.Errorf(
			"%w: backend %q refuses action %q: platform %q has no OS sandbox (will not run unsandboxed)",
			ErrNotAuthorized, e.Backend(), action.Name, e.goos,
		)
	}

	argv, err := decodeArgv(action.Payload)
	if err != nil {
		return nil, fmt.Errorf("backend %q action %q: %w", e.Backend(), action.Name, err)
	}

	wrapped, err := e.wrap(cfg, argv)
	if err != nil {
		return nil, fmt.Errorf("backend %q action %q: %w", e.Backend(), action.Name, err)
	}

	// Resource cap: bound wallclock with a context timeout. Zero is replaced by a
	// bounded default, never treated as unlimited.
	wallMillis := cfg.MaxWallMillis
	if wallMillis <= 0 {
		wallMillis = defaultWallMillis
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(wallMillis)*time.Millisecond)
	defer cancel()

	out, code, runErr := e.runner.run(runCtx, wrapped)
	if runErr != nil {
		// A timeout or a never-started process surfaces as an error; a process that
		// ran and exited non-zero is reported via ExitCode, not as a Go error.
		if code < 0 {
			return nil, fmt.Errorf("backend %q action %q: %w", e.Backend(), action.Name, runErr)
		}
	}
	return &ExecResult{Output: out, ExitCode: code}, nil
}

// wrap builds the full argv that runs the inner command under the platform
// sandbox. It refuses (fails closed) when the platform's sandbox tool is not
// installed — it never falls back to running unsandboxed.
func (e *osSandboxExecutor) wrap(cfg ExecConfig, argv []string) ([]string, error) {
	switch e.goos {
	case "darwin":
		if _, err := e.lookPath(sandboxExecPath); err != nil {
			return nil, fmt.Errorf("%w: sandbox-exec not found at %s", ErrSandboxUnavailable, sandboxExecPath)
		}
		profile := seatbeltProfile(cfg)
		// sandbox-exec -p <profile> <argv...>
		return append([]string{sandboxExecPath, "-p", profile}, argv...), nil
	case "linux":
		bwrapPath, err := e.lookPath(bwrapBinary)
		if err != nil {
			return nil, fmt.Errorf("%w: bwrap (bubblewrap) not found on PATH", ErrSandboxUnavailable)
		}
		return append([]string{bwrapPath}, append(bwrapArgs(cfg), argv...)...), nil
	default:
		// Unreachable: Execute gates platform before calling wrap. Fail closed.
		return nil, fmt.Errorf("%w: platform %q has no OS sandbox", ErrNotAuthorized, e.goos)
	}
}

// decodeArgv extracts the command vector an OS-sandbox action runs. Payload is a
// JSON-encoded []string (argv). It is non-empty by contract — the backend never
// guesses a command.
func decodeArgv(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, ErrEmptyArgv
	}
	var argv []string
	if err := json.Unmarshal(payload, &argv); err != nil {
		return nil, fmt.Errorf("%w: payload is not a JSON argv: %w", ErrEmptyArgv, err)
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return nil, ErrEmptyArgv
	}
	return argv, nil
}

// execRunner is the production commandRunner: it spawns the wrapped process for
// real. Wallclock is enforced by the ctx passed in (Execute wraps it in a
// timeout); a context cancellation kills the process group.
type execRunner struct{}

func (execRunner) run(ctx context.Context, argv []string) ([]byte, int, error) {
	if len(argv) == 0 {
		return nil, -1, ErrEmptyArgv
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, cmd.ProcessState.ExitCode(), nil
	}
	// A context timeout/cancellation is reported as a start/run error, not an
	// exit code, so callers can distinguish "killed by the cap" from "ran and
	// exited non-zero".
	if ctxErr := ctx.Err(); ctxErr != nil {
		return out, -1, fmt.Errorf("sandboxed process killed by resource cap: %w", ctxErr)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The process ran and exited non-zero. Surface the code, not a Go error.
		return out, exitErr.ExitCode(), nil
	}
	// The process never started (e.g. binary missing inside the sandbox).
	return out, -1, fmt.Errorf("sandboxed process failed to start: %w", err)
}

// canonicalPath resolves a path for a sandbox profile. macOS Seatbelt and Linux
// bwrap both match REAL paths, so a symlinked allow-list entry (the classic
// /tmp -> /private/tmp on macOS) must be canonicalized or the boundary silently
// fails open (a write "allowed" at /tmp is denied) or, worse, mismatches. It
// returns the cleaned path even when the file does not exist yet (a write target
// may be created), and resolves the existing parent's symlinks.
func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	// The path may not exist yet (a write target). Resolve the deepest existing
	// ancestor and re-attach the remaining tail, so /tmp/new -> /private/tmp/new.
	dir, file := filepath.Split(filepath.Clean(p))
	dir = filepath.Clean(dir)
	if dir == "" || dir == p {
		return filepath.Clean(p)
	}
	return filepath.Join(canonicalPath(dir), file)
}
