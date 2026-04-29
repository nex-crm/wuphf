package main

// workspaces_adapter.go bridges Lane B's package-level workspaces API to
// Lane C's broker-side `team.workspaceOrchestrator` interface and Lane D's
// CLI-side `workspaceOrchestrator` interface. Lane B's public API is
// package functions (workspaces.Create, workspaces.Pause, etc.); these
// adapters wrap them in the method-shaped contracts the consumers expect.
//
// We adapt at the consumer rather than modifying Lane B so the package
// stays a tight functional surface usable from anywhere (including the
// spawned child broker).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/workspaces"
)

// cliOrchestratorAdapter implements the CLI's workspaceOrchestrator interface
// against the package-level workspaces functions.
type cliOrchestratorAdapter struct{}

func (cliOrchestratorAdapter) List(ctx context.Context, opts ListOpts) (ListResult, error) {
	live, err := workspaces.List(ctx)
	if err != nil {
		if errors.Is(err, workspaces.ErrRegistryNotFound) {
			return ListResult{Workspaces: []Workspace{}, Trash: []TrashEntry{}}, nil
		}
		return ListResult{}, err
	}
	cliCurrent := readCLICurrent()

	out := ListResult{Workspaces: make([]Workspace, 0, len(live))}
	for _, lw := range live {
		ws := workspaceFromRegistry(lw.Workspace)
		ws.IsActive = lw.Live
		ws.IsCLICurrent = lw.Workspace.Name == cliCurrent
		out.Workspaces = append(out.Workspaces, ws)
	}

	if opts.IncludeTrash {
		entries, terr := listTrashEntries()
		if terr == nil {
			out.Trash = entries
		}
	}
	return out, nil
}

func (cliOrchestratorAdapter) Create(ctx context.Context, req CreateRequest) (Workspace, error) {
	opts := workspaces.CreateOptions{
		Blueprint:   req.Blueprint,
		FromScratch: req.FromScratch,
	}
	if err := workspaces.Create(ctx, req.Name, req.Blueprint, opts); err != nil {
		return Workspace{}, err
	}
	return resolveWorkspace(req.Name)
}

func (cliOrchestratorAdapter) Switch(ctx context.Context, name string, openBrowser bool) (Workspace, error) {
	url, err := workspaces.Switch(ctx, name)
	if err != nil {
		return Workspace{}, err
	}
	ws, rerr := resolveWorkspace(name)
	if rerr != nil {
		return Workspace{}, rerr
	}
	if openBrowser && url != "" {
		openWorkspaceURL(url)
	}
	return ws, nil
}

func (cliOrchestratorAdapter) Pause(ctx context.Context, name string, force bool) error {
	// Lane B's Pause already escalates SIGTERM/SIGKILL on its 60s/75s
	// schedule. The CLI's --force flag is currently treated as advisory:
	// the same Pause path is taken but we shorten the caller's context so
	// the wall-clock budget is tighter. Lane B can refine later by accepting
	// a Force option.
	if force {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	return workspaces.Pause(ctx, name)
}

func (cliOrchestratorAdapter) Resume(ctx context.Context, name string) (Workspace, error) {
	if err := workspaces.Resume(ctx, name); err != nil {
		return Workspace{}, err
	}
	return resolveWorkspace(name)
}

func (cliOrchestratorAdapter) Shred(ctx context.Context, name string, permanent bool) error {
	return workspaces.Shred(ctx, name, permanent)
}

func (cliOrchestratorAdapter) Restore(ctx context.Context, trashID string) (Workspace, error) {
	if err := workspaces.Restore(ctx, trashID); err != nil {
		return Workspace{}, err
	}
	name := trashIDOriginalName(trashID)
	if name == "" {
		return Workspace{}, fmt.Errorf("restore %q: cannot infer workspace name", trashID)
	}
	return resolveWorkspace(name)
}

func (cliOrchestratorAdapter) Doctor(ctx context.Context) (DoctorReport, error) {
	rep, err := workspaces.Doctor(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	return translateDoctorReport(rep), nil
}

func (cliOrchestratorAdapter) FixDoctorIssue(ctx context.Context, fixID string) error {
	// Lane B owns typed dispatch per FixID prefix
	// (orphan_tree, zombie, port, corrupt, symlink, migration). Doctor's
	// auto-applied fixes (stopping → paused, missing-symlink recreate) still
	// run on every Doctor call — FixDoctorIssue is the explicit, idempotent
	// path for the leftover advisory issues that were surfaced to the user.
	return workspaces.FixDoctorIssue(ctx, fixID)
}

func (cliOrchestratorAdapter) Resolve(ctx context.Context, name string) (Workspace, error) {
	return resolveWorkspace(name)
}

// brokerOrchestratorAdapter implements the broker-side
// team.workspaceOrchestrator (the unexported interface in
// internal/team/broker_workspaces.go). The interface is unexported but Go's
// structural typing lets cmd/wuphf supply a value that satisfies it as long
// as every method shape matches.
type brokerOrchestratorAdapter struct{}

func (brokerOrchestratorAdapter) List(ctx context.Context) ([]team.Workspace, error) {
	live, err := workspaces.List(ctx)
	if err != nil {
		if errors.Is(err, workspaces.ErrRegistryNotFound) {
			return []team.Workspace{}, nil
		}
		return nil, err
	}
	out := make([]team.Workspace, 0, len(live))
	for _, lw := range live {
		out = append(out, brokerWorkspace(lw.Workspace))
	}
	return out, nil
}

func (brokerOrchestratorAdapter) Create(ctx context.Context, req team.CreateRequest) (team.Workspace, error) {
	opts := workspaces.CreateOptions{
		Blueprint:   req.Blueprint,
		CompanyName: req.CompanyName,
		FromScratch: req.FromScratch,
	}
	if err := workspaces.Create(ctx, req.Name, req.Blueprint, opts); err != nil {
		return team.Workspace{}, err
	}
	ws, err := lookupRegistry(req.Name)
	if err != nil {
		return team.Workspace{}, err
	}
	return brokerWorkspace(ws), nil
}

func (brokerOrchestratorAdapter) Switch(ctx context.Context, name string) error {
	_, err := workspaces.Switch(ctx, name)
	return err
}

func (brokerOrchestratorAdapter) Pause(ctx context.Context, name string) error {
	return workspaces.Pause(ctx, name)
}

func (brokerOrchestratorAdapter) Resume(ctx context.Context, name string) error {
	return workspaces.Resume(ctx, name)
}

func (brokerOrchestratorAdapter) Shred(ctx context.Context, name string, permanent bool) error {
	return workspaces.Shred(ctx, name, permanent)
}

func (brokerOrchestratorAdapter) Restore(ctx context.Context, trashID string) (team.Workspace, error) {
	if err := workspaces.Restore(ctx, trashID); err != nil {
		return team.Workspace{}, err
	}
	name := trashIDOriginalName(trashID)
	if name == "" {
		return team.Workspace{}, fmt.Errorf("restore %q: cannot infer workspace name", trashID)
	}
	ws, err := lookupRegistry(name)
	if err != nil {
		return team.Workspace{}, err
	}
	return brokerWorkspace(ws), nil
}

// --- shared helpers ----------------------------------------------------------

func workspaceFromRegistry(ws *workspaces.Workspace) Workspace {
	out := Workspace{
		Name:        ws.Name,
		RuntimeHome: ws.RuntimeHome,
		BrokerPort:  ws.BrokerPort,
		WebPort:     ws.WebPort,
		State:       WorkspaceState(ws.State),
		Blueprint:   ws.Blueprint,
		CompanyName: ws.CompanyName,
		CreatedAt:   ws.CreatedAt,
		LastUsedAt:  ws.LastUsedAt,
	}
	if !ws.PausedAt.IsZero() {
		t := ws.PausedAt
		out.PausedAt = &t
	}
	return out
}

func brokerWorkspace(ws *workspaces.Workspace) team.Workspace {
	out := team.Workspace{
		Name:        ws.Name,
		RuntimeHome: ws.RuntimeHome,
		BrokerPort:  ws.BrokerPort,
		WebPort:     ws.WebPort,
		State:       string(ws.State),
		Blueprint:   ws.Blueprint,
		CompanyName: ws.CompanyName,
		CreatedAt:   ws.CreatedAt.Format(time.RFC3339),
		LastUsedAt:  ws.LastUsedAt.Format(time.RFC3339),
	}
	if !ws.PausedAt.IsZero() {
		s := ws.PausedAt.Format(time.RFC3339)
		out.PausedAt = &s
	}
	return out
}

// resolveWorkspace reads the registry and returns the workspace shape used
// by the CLI. Returns ErrWorkspaceNotFound if the name is absent.
func resolveWorkspace(name string) (Workspace, error) {
	ws, err := lookupRegistry(name)
	if err != nil {
		return Workspace{}, err
	}
	return workspaceFromRegistry(ws), nil
}

func lookupRegistry(name string) (*workspaces.Workspace, error) {
	reg, err := workspaces.Read()
	if err != nil {
		return nil, err
	}
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			return ws, nil
		}
	}
	return nil, workspaces.ErrWorkspaceNotFound
}

func readCLICurrent() string {
	reg, err := workspaces.Read()
	if err != nil {
		return ""
	}
	return reg.CLICurrent
}

// targetBrokerURL maps a workspace name to the http://127.0.0.1:<port>/
// base URL the cross-broker pause proxy hits. Returns "" if the workspace
// is unknown — broker_workspaces.go translates that into a 503.
func targetBrokerURL(name string) string {
	ws, err := lookupRegistry(name)
	if err != nil || ws == nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", ws.BrokerPort)
}

// translateDoctorReport converts Lane B's reconciliation findings into
// Lane D's typed DoctorReport. Lane B already auto-applies the safe fixes
// (stopping → paused, symlink recreate); we surface the leftover findings
// as advisory issues so the user can re-run doctor or shell-debug.
func translateDoctorReport(rep workspaces.DoctorReport) DoctorReport {
	out := DoctorReport{}
	for _, p := range rep.OrphanTrees {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssueOrphanTree,
			Subject:   p,
			Detail:    "directory exists in ~/.wuphf-spaces but is not registered",
			FixAction: "remove the directory or `wuphf workspace restore` if it is recoverable",
			FixID:     fmt.Sprintf("orphan_tree:%s", p),
		})
	}
	for _, z := range rep.ZombieRunning {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssueZombieState,
			Subject:   z,
			Detail:    "registry says running but the broker port is unbound",
			FixAction: "re-run doctor to reconcile state",
			FixID:     fmt.Sprintf("zombie:%s", z),
		})
	}
	for _, c := range rep.PortConflicts {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssuePortConflict,
			Subject:   c,
			Detail:    "port held by an unknown process",
			FixAction: "stop the conflicting process and re-run doctor",
			FixID:     fmt.Sprintf("port:%s", c),
		})
	}
	if rep.CorruptRegistry {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssueCorruptRegistry,
			Subject:   "registry.json",
			Detail:    "registry parse failed; backup may be in use",
			FixAction: "manual intervention required — see ~/.wuphf-spaces/",
			FixID:     "corrupt:registry",
		})
	}
	if rep.SymlinkMissing {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssueMissingSymlink,
			Subject:   "~/.wuphf",
			Detail:    "compatibility symlink missing",
			FixAction: "re-run doctor to recreate the symlink",
			FixID:     "symlink:missing",
		})
	}
	if rep.SymlinkWrong != "" {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssueOrphanedSymlink,
			Subject:   "~/.wuphf",
			Detail:    rep.SymlinkWrong,
			FixAction: "remove ~/.wuphf and re-run doctor",
			FixID:     "symlink:wrong",
		})
	}
	if rep.PartialMigration {
		out.Issues = append(out.Issues, DoctorIssue{
			Kind:      DoctorIssuePartialMigration,
			Subject:   "~/.wuphf",
			Detail:    "regular ~/.wuphf coexists with ~/.wuphf-spaces/main",
			FixAction: "consolidate manually — see docs/multi-workspace.md",
			FixID:     "migration:partial",
		})
	}
	return out
}

// listTrashEntries scans ~/.wuphf-spaces/.trash/ and returns one entry per
// directory. Lane B doesn't expose this listing yet; we reach into the
// well-known path directly.
//
// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — ~/.wuphf-spaces
// is the cross-workspace registry root and lives at the user's real home,
// not any single workspace's runtime tree.
func listTrashEntries() ([]TrashEntry, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	trashDir := filepath.Join(home, ".wuphf-spaces", ".trash")
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []TrashEntry{}, nil
		}
		return nil, err
	}
	out := make([]TrashEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		name := trashIDOriginalName(id)
		if name == "" {
			continue
		}
		shreddedAt := time.Time{}
		if ts := trashIDTimestamp(id); ts > 0 {
			shreddedAt = time.Unix(ts, 0).UTC()
		}
		full := filepath.Join(trashDir, id)
		size, _ := dirSizeBytes(full)
		out = append(out, TrashEntry{
			TrashID:    id,
			Name:       name,
			ShreddedAt: shreddedAt,
			SizeBytes:  size,
		})
	}
	return out, nil
}

func dirSizeBytes(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// trashIDOriginalName mirrors Lane B's package-private extractOriginalName:
// finds the last hyphen followed by a numeric suffix and returns the
// preceding portion.
func trashIDOriginalName(trashID string) string {
	for i := len(trashID) - 1; i >= 0; i-- {
		if trashID[i] == '-' {
			suffix := trashID[i+1:]
			if _, err := strconv.ParseInt(suffix, 10, 64); err == nil {
				return trashID[:i]
			}
		}
	}
	return ""
}

func trashIDTimestamp(trashID string) int64 {
	for i := len(trashID) - 1; i >= 0; i-- {
		if trashID[i] == '-' {
			suffix := trashID[i+1:]
			if v, err := strconv.ParseInt(suffix, 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}

// openWorkspaceURL opens a browser at the workspace's web URL when the user
// passed --open. Reuses the package-level openBrowserURL helper from the
// rest of the CLI.
func openWorkspaceURL(url string) {
	_ = openBrowserURL(url)
}
