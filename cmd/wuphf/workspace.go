package main

// This file owns the `wuphf workspace ...` CLI subcommand tree. The tree is a
// flat dispatcher: `workspace` is the top-level verb, every action below it
// (list, create, switch, pause, resume, shred, restore, doctor) lives in its
// own file (workspace_<action>.go) so each subcommand has a focused unit of
// code + tests.
//
// All subcommands talk to a small `workspaceOrchestrator` interface defined
// here. The real implementation lives in `internal/workspaces` (Lane B);
// tests use `fakeWorkspaceOrchestrator` from workspace_test.go. The CLI never
// pokes at registry.json directly — every path goes through the orchestrator
// so that workspace-aware HTTP handlers and the CLI share the same contract.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// WorkspaceState mirrors the registry's `state` enum. Strings rather than ints
// so JSON output is human-readable and stable across versions.
type WorkspaceState string

const (
	WorkspaceStateRunning      WorkspaceState = "running"
	WorkspaceStatePaused       WorkspaceState = "paused"
	WorkspaceStateStarting     WorkspaceState = "starting"
	WorkspaceStateStopping     WorkspaceState = "stopping"
	WorkspaceStateNeverStarted WorkspaceState = "never_started"
	WorkspaceStateError        WorkspaceState = "error"
)

// Workspace is the registry-shaped record the CLI cares about. Mirrors the
// registry.json schema in the design doc. Lane B's package returns a richer
// type; the CLI only uses these fields, so the interface keeps the surface
// narrow and stable.
type Workspace struct {
	Name         string         `json:"name"`
	RuntimeHome  string         `json:"runtime_home"`
	BrokerPort   int            `json:"broker_port"`
	WebPort      int            `json:"web_port"`
	State        WorkspaceState `json:"state"`
	Blueprint    string         `json:"blueprint,omitempty"`
	CompanyName  string         `json:"company_name,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	LastUsedAt   time.Time      `json:"last_used_at"`
	PausedAt     *time.Time     `json:"paused_at,omitempty"`
	IsActive     bool           `json:"is_active,omitempty"`
	IsCLICurrent bool           `json:"is_cli_current,omitempty"`
	// CostUSD is per-workspace cumulative cost for the current billing window
	// (cached by the orchestrator from each broker's /usage endpoint). Zero
	// means "no data yet" rather than "$0.00 spent".
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// TrashEntry represents a shredded workspace held in ~/.wuphf-spaces/.trash/.
// `TrashID` is the directory name (`<name>-<unix-timestamp>`) that `restore`
// takes as its argument.
type TrashEntry struct {
	TrashID     string    `json:"trash_id"`
	Name        string    `json:"name"`
	ShreddedAt  time.Time `json:"shredded_at"`
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	CompanyName string    `json:"company_name,omitempty"`
}

// ListOpts gates the orchestrator's listing behavior. Trash and active are
// independent — callers can ask for either or both.
type ListOpts struct {
	IncludeTrash bool
}

// ListResult is what the orchestrator hands back. Two slices rather than a
// merged shape so JSON output stays unambiguous.
type ListResult struct {
	Workspaces []Workspace  `json:"workspaces"`
	Trash      []TrashEntry `json:"trash,omitempty"`
}

// CreateRequest is the payload for `workspace create`. The orchestrator owns
// validation; the CLI only collects flags and forwards.
type CreateRequest struct {
	Name        string
	Blueprint   string
	FromScratch bool
	// InheritFrom defaults to "main" inside the orchestrator when empty.
	// Surfaced so test cases can assert pass-through.
	InheritFrom string
}

// DoctorIssueKind tags every reconcile finding. Strings rather than ints so
// future categories can be added without renumbering and so JSON output is
// self-documenting if we ever add a `--json` flag to doctor.
type DoctorIssueKind string

const (
	DoctorIssueOrphanTree         DoctorIssueKind = "orphan_tree"
	DoctorIssueZombieState        DoctorIssueKind = "zombie_state"
	DoctorIssuePortConflict       DoctorIssueKind = "port_conflict"
	DoctorIssueCorruptRegistry    DoctorIssueKind = "corrupt_registry"
	DoctorIssueOrphanedSymlink    DoctorIssueKind = "orphaned_symlink"
	DoctorIssueMissingSymlink     DoctorIssueKind = "missing_symlink"
	DoctorIssuePartialMigration   DoctorIssueKind = "partial_migration"
	DoctorIssueStoppingReconcile  DoctorIssueKind = "stopping_reconcile"
	DoctorIssueExpiredTrashSweep  DoctorIssueKind = "expired_trash_sweep"
	DoctorIssueTokenFilePerm      DoctorIssueKind = "token_file_perm"
	DoctorIssueOpencodeRaceConfig DoctorIssueKind = "opencode_race_config"
)

// DoctorIssue is a single finding from a reconcile pass. The CLI iterates
// these and prompts the user for each fix.
type DoctorIssue struct {
	Kind      DoctorIssueKind `json:"kind"`
	Subject   string          `json:"subject"` // workspace name, path, port — depends on Kind
	Detail    string          `json:"detail"`
	FixAction string          `json:"fix_action"` // human-readable description of what `Fix` will do
	// FixID is the opaque token the orchestrator hands the CLI to invoke the
	// remediation. Tests rely on this being deterministic per (Kind, Subject).
	FixID string `json:"fix_id"`
}

// DoctorReport is what `Doctor()` returns: zero or more issues. An empty
// Issues slice means "all clean".
type DoctorReport struct {
	Issues []DoctorIssue `json:"issues"`
}

// workspaceOrchestrator is the contract Lane B must honor at merge time. The
// CLI only depends on this interface; `internal/workspaces.New(...)` will be
// wired in cmd/wuphf/main.go once Lane B lands. Method shapes:
//
//   - Every method takes a context for cancellation/timeouts. The CLI passes
//     a short-deadline context for orchestrator calls so a hung filesystem
//     can't wedge the user's terminal.
//   - List returns both running + paused; --trash adds trashed entries.
//   - Create blocks until the new broker is bound or returns a typed error.
//   - Switch updates registry.cli_current AND optionally opens the browser.
//   - Pause's `force` skips the 90s graceful drain → SIGKILL after 5s.
//   - Shred's `permanent` skips trash entirely (irreversible).
//   - Doctor reports issues. The CLI prompts per-issue; each `FixID` is
//     re-submitted via FixDoctorIssue to actually apply the fix.
type workspaceOrchestrator interface {
	List(ctx context.Context, opts ListOpts) (ListResult, error)
	Create(ctx context.Context, req CreateRequest) (Workspace, error)
	Switch(ctx context.Context, name string, openBrowser bool) (Workspace, error)
	Pause(ctx context.Context, name string, force bool) error
	Resume(ctx context.Context, name string) (Workspace, error)
	Shred(ctx context.Context, name string, permanent bool) error
	Restore(ctx context.Context, trashID string) (Workspace, error)
	Doctor(ctx context.Context) (DoctorReport, error)
	FixDoctorIssue(ctx context.Context, fixID string) error
	// Resolve returns the registry record for name without spawning or
	// touching ports. Used by --workspace=<name> override + by the doctor
	// loop. Must be idempotent and side-effect-free.
	Resolve(ctx context.Context, name string) (Workspace, error)
}

// orchestratorFactory is the global indirection point so tests can swap a
// fake implementation without touching every subcommand. Production wires
// this to `internal/workspaces.New(...)` in main.go.
var orchestratorFactory = func() (workspaceOrchestrator, error) {
	return nil, fmt.Errorf("workspace orchestrator not wired (Lane B integration pending)")
}

// runWorkspace is the entry point for `wuphf workspace ...`. Routes args[0]
// to the matching subcommand handler. Mirrors the dispatch shape used by
// `runUpgradeCheck` / `runImport` / `runLogCmd` for consistency.
func runWorkspace(args []string) {
	if len(args) == 0 {
		printWorkspaceHelp()
		os.Exit(1)
	}
	if subcommandWantsHelp(args) {
		printWorkspaceHelp()
		return
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list", "ls":
		runWorkspaceList(rest)
	case "create", "new":
		runWorkspaceCreate(rest)
	case "switch", "use":
		runWorkspaceSwitch(rest)
	case "pause":
		runWorkspacePause(rest)
	case "resume", "start":
		runWorkspaceResume(rest)
	case "shred":
		runWorkspaceShred(rest)
	case "restore":
		runWorkspaceRestore(rest)
	case "doctor":
		runWorkspaceDoctor(rest)
	default:
		fmt.Fprintf(os.Stderr, "wuphf workspace: unknown subcommand %q\n", sub)
		fmt.Fprintln(os.Stderr, "")
		printWorkspaceHelp()
		os.Exit(1)
	}
}

// printWorkspaceHelp owns the `wuphf workspace --help` copy. Kept here rather
// than in printSubcommandHelp() so adding a new subcommand only touches one
// file.
func printWorkspaceHelp() {
	fmt.Fprintln(os.Stderr, "wuphf workspace — manage WUPHF workspaces (the offices upstairs)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Each workspace is one fully isolated WUPHF instance — its own team, wiki,")
	fmt.Fprintln(os.Stderr, "office tasks, broker process, and per-workspace token bill. Like having")
	fmt.Fprintln(os.Stderr, "Scranton and Stamford in separate buildings, except both are localhost.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  wuphf workspace list                       List all workspaces")
	fmt.Fprintln(os.Stderr, "  wuphf workspace list --json                Stable JSON for scripting")
	fmt.Fprintln(os.Stderr, "  wuphf workspace list --trash               Show trashed workspaces with restore IDs")
	fmt.Fprintln(os.Stderr, "  wuphf workspace create <name>              Create a new workspace")
	fmt.Fprintln(os.Stderr, "      [--blueprint=X]                          Blueprint slug (defaults to current's)")
	fmt.Fprintln(os.Stderr, "      [--from-scratch]                         Skip blueprint inheritance — start blank")
	fmt.Fprintln(os.Stderr, "      [--inherit-from=Y]                       Source workspace for inherited fields")
	fmt.Fprintln(os.Stderr, "  wuphf workspace switch <name> [--open]     Switch CLI default workspace")
	fmt.Fprintln(os.Stderr, "  wuphf workspace pause <name> [--force]     Pause (graceful 90s, --force kills hard)")
	fmt.Fprintln(os.Stderr, "  wuphf workspace resume <name>              Resume a paused workspace")
	fmt.Fprintln(os.Stderr, "  wuphf workspace shred <name> [--permanent] Shred (to trash; --permanent skips trash)")
	fmt.Fprintln(os.Stderr, "  wuphf workspace restore <trash-id>         Restore from trash with a fresh port pair")
	fmt.Fprintln(os.Stderr, "  wuphf workspace doctor                     Reconcile registry; interactive fixes")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Per-command help: wuphf workspace <subcommand> --help")
}

// resolveOrchestrator builds the orchestrator instance via the factory and
// surfaces a friendly error if Lane B's package isn't wired yet. Centralized
// so every subcommand handles the not-wired case the same way.
func resolveOrchestrator() workspaceOrchestrator {
	orch, err := orchestratorFactory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return orch
}

// workspaceCtx returns a short-deadline context shared across orchestrator
// calls. Keeps the CLI responsive when the broker / filesystem hangs.
// Doctor and create take longer (broker spawn) — see workspaceCtxLong.
func workspaceCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// workspaceCtxLong covers create/resume/restore where the orchestrator must
// wait for a broker to bind a port. 60s is the eng-review-budgeted "ready
// from create click" target plus headroom.
func workspaceCtxLong() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// printError writes a one-line user-facing error and exits 1. Centralized so
// every subcommand has the same exit posture.
func printError(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+strings.TrimRight(format, "\n")+"\n", a...)
	os.Exit(1)
}

// applyWorkspaceOverride resolves a --workspace=<name> override into a
// runtime_home and exports it as WUPHF_RUNTIME_HOME for the duration of this
// process. Called from main() BEFORE subcommand dispatch so that workspace
// state-path resolvers (config.RuntimeHomeDir) see the correct tree from the
// first lookup onward.
//
// On orchestrator-not-wired we degrade gracefully: WUPHF_RUNTIME_HOME stays
// at whatever the caller already set so the existing dev/prod isolation
// pattern keeps working. We exit non-zero only if the orchestrator IS wired
// and the workspace doesn't exist — a typo in --workspace=demo should fail
// loud, not silently target main.
func applyWorkspaceOverride(name string) {
	orch, err := orchestratorFactory()
	if err != nil {
		// Orchestrator not yet wired (Lane B integration pending). Surface a
		// friendly note to stderr and let the caller fall through. This
		// keeps existing single-workspace and dev/prod flows working
		// unchanged until Lane B lands.
		fmt.Fprintf(os.Stderr, "warning: --workspace=%q ignored: %v\n", name, err)
		return
	}
	ctx, cancel := workspaceCtx()
	defer cancel()
	ws, err := orch.Resolve(ctx, name)
	if err != nil {
		printError("--workspace=%q: %v", name, err)
	}
	if ws.RuntimeHome == "" {
		printError("--workspace=%q: workspace has no runtime_home in registry (run `wuphf workspace doctor`)", name)
	}
	_ = os.Setenv("WUPHF_RUNTIME_HOME", ws.RuntimeHome)
	if ws.BrokerPort > 0 {
		_ = os.Setenv("WUPHF_BROKER_PORT", fmt.Sprintf("%d", ws.BrokerPort))
	}
}
