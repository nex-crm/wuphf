package workspaces

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workspace"
)

const (
	liveProbeTimeout = 200 * time.Millisecond
	trashDirName     = ".trash"
)

// Pause escalation timeouts. Declared as vars (not consts) so tests can
// shrink the wall-clock budget; production callers should not mutate these.
var (
	pauseWallClockTimeout = 90 * time.Second
	pauseSIGTERMAt        = 60 * time.Second
	pauseSIGKILLAt        = 75 * time.Second
)

// CreateOptions controls the Create operation.
type CreateOptions struct {
	// Blueprint is the blueprint slug (e.g. "founding-team"). Optional.
	Blueprint string
	// CompanyName is the company name for the new workspace. Optional.
	CompanyName string
	// FromScratch disables inheritance from the current workspace when true.
	FromScratch bool
	// InheritFrom names the source workspace inheritance should pull from.
	// Empty falls back to the orchestrator's default ("cli_current"). Stored
	// on the workspace row so later operations (resume, rebuild) can reproduce
	// the original inheritance choice.
	InheritFrom string
}

// DoctorReport summarises reconciliation findings from Doctor.
type DoctorReport struct {
	OrphanTrees      []string // dirs in ~/.wuphf-spaces/ not in registry
	ZombieRunning    []string // registry says running but port unbound
	PortConflicts    []string // port in use by unknown process
	CorruptRegistry  bool
	SymlinkMissing   bool   // ~/.wuphf symlink absent
	SymlinkWrong     string // symlink points to wrong target
	PartialMigration bool   // regular ~/.wuphf coexists with spaces dir
	Actions          []string // human-readable fixes applied
}

// tmuxKiller is a variable so tests can stub tmux calls.
var tmuxKiller = func(port int) {
	suffix := fmt.Sprintf("%d", port)
	socketName := "wuphf-" + suffix
	sessionName := "wuphf-team-" + suffix
	cmd := exec.Command("tmux", "-L", socketName, "kill-session", "-t", sessionName)
	_ = cmd.Run()
}

// Create allocates a port pair, initialises the workspace runtime directory,
// spawns a broker, and registers the workspace.
func Create(ctx context.Context, name, blueprint string, opts CreateOptions) error {
	if err := ValidateSlug(name); err != nil {
		return err
	}

	lf, err := acquireLock()
	if err != nil {
		return err
	}

	rp, regErr := registryPath()
	if regErr != nil {
		releaseLock(lf)
		return regErr
	}

	reg, err := readFile(rp)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			bak := rp + ".bak"
			reg, err = readFile(bak)
		}
		if err != nil {
			reg = &Registry{Version: Version, CLICurrent: "main"}
		}
	}

	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			releaseLock(lf)
			return fmt.Errorf("workspaces: workspace %q already exists", name)
		}
	}

	brokerPort, webPort, err := AllocatePortPair(reg)
	if err != nil {
		releaseLock(lf)
		return err
	}

	sd, err := spacesDir()
	if err != nil {
		releaseLock(lf)
		return err
	}
	runtimeHome := filepath.Join(sd, name)
	wuphfDir := filepath.Join(runtimeHome, ".wuphf")
	if err := os.MkdirAll(wuphfDir, 0o700); err != nil {
		releaseLock(lf)
		return fmt.Errorf("workspaces: create %q: mkdir: %w", name, err)
	}

	now := time.Now().UTC()
	ws := &Workspace{
		Name:        name,
		RuntimeHome: runtimeHome,
		BrokerPort:  brokerPort,
		WebPort:     webPort,
		State:       StateStarting,
		Blueprint:   blueprint,
		CompanyName: opts.CompanyName,
		CreatedAt:   now,
		LastUsedAt:  now,
	}
	reg.Workspaces = append(reg.Workspaces, ws)

	if err := writeUnderLock(reg); err != nil {
		releaseLock(lf)
		return fmt.Errorf("workspaces: create %q: register: %w", name, err)
	}
	releaseLock(lf)

	if err := Spawn(name, runtimeHome, brokerPort, webPort); err != nil {
		_ = Update(name, func(w *Workspace) error {
			w.State = StateError
			return nil
		})
		return fmt.Errorf("workspaces: create %q: spawn: %w", name, err)
	}

	return Update(name, func(w *Workspace) error {
		w.State = StateRunning
		return nil
	})
}

// Switch updates cli_current to name and returns the workspace's web URL.
// Only wuphf workspace switch updates cli_current; pause/resume do not.
func Switch(ctx context.Context, name string) (string, error) {
	lf, err := acquireLock()
	if err != nil {
		return "", err
	}
	defer releaseLock(lf)

	rp, err := registryPath()
	if err != nil {
		return "", err
	}
	reg, err := readFile(rp)
	if err != nil {
		return "", err
	}

	var target *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			target = ws
			break
		}
	}
	if target == nil {
		return "", ErrWorkspaceNotFound
	}

	reg.CLICurrent = name
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			ws.LastUsedAt = time.Now().UTC()
			break
		}
	}

	if err := writeUnderLock(reg); err != nil {
		return "", err
	}
	return fmt.Sprintf("http://localhost:%d/", target.WebPort), nil
}

// Pause gracefully stops the named workspace's broker.
// Steps: mark stopping → POST /admin/pause → wait exit (90s) → tmux kill →
// mark paused. If the broker doesn't exit cleanly within the wall-clock
// budget, SIGTERM is sent at 60s and SIGKILL at 75s.
//
// Fail-closed semantics: if the broker is still bound to its port after the
// SIGTERM/SIGKILL escalation ladder has run to completion, Pause refuses to
// claim the workspace is paused. The registry transitions to StateError and
// Pause returns the underlying shutdown error so the caller can surface it.
// Transient errors that don't actually leave a live broker (token file
// missing because the broker already exited cleanly, /admin/pause HTTP errors
// followed by a successful exit) fall through to StatePaused as before.
func Pause(ctx context.Context, name string) error {
	reg, err := Read()
	if err != nil {
		return err
	}
	var target *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			target = ws
			break
		}
	}
	if target == nil {
		return ErrWorkspaceNotFound
	}
	if target.State == StatePaused {
		return nil
	}

	// Eagerly persist stopping state so a crash here leaves a reconcilable
	// row (stopping → paused on next doctor run).
	pausedAt := time.Now().UTC()
	if err := Update(name, func(ws *Workspace) error {
		ws.State = StateStopping
		ws.PausedAt = pausedAt
		return nil
	}); err != nil {
		return fmt.Errorf("workspaces: pause %q: mark stopping: %w", name, err)
	}

	// Read the workspace's bearer token and POST /admin/pause.
	// Token file missing is fail-open: if the broker already exited cleanly
	// and removed its token file, /admin/pause without an Authorization
	// header is harmless — the SIGTERM/SIGKILL ladder still covers a live
	// broker. We track the postAdminPause result so we can include it in the
	// fail-closed error annotation if shutdown ultimately fails.
	token, _ := readTokenFile(name)
	pauseHTTPErr := postAdminPause(target.BrokerPort, token)

	// Escalation schedule.
	start := time.Now()
	pid := readPIDFile(target.RuntimeHome)
	brokerExited := false

	// Poll for exit up to pauseWallClockTimeout.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
loop:
	for {
		elapsed := time.Since(start)
		if elapsed >= pauseWallClockTimeout {
			break
		}
		select {
		case <-ticker.C:
			if !probePort(target.BrokerPort) {
				brokerExited = true
				break loop
			}
			if pid > 0 && elapsed >= pauseSIGTERMAt && elapsed < pauseSIGKILLAt {
				sendSIGTERM(pid)
			} else if pid > 0 && elapsed >= pauseSIGKILLAt {
				sendSIGKILL(pid)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	tmuxKiller(target.BrokerPort)

	// Final liveness check after the kill ladder + tmux kill: if the broker
	// is STILL bound, fail closed. Marking the registry "paused" while the
	// process is alive would let a second resume attempt collide with the
	// surviving broker.
	if !brokerExited && probePort(target.BrokerPort) {
		shutdownErr := fmt.Errorf("workspaces: pause %q: broker still alive after SIGTERM/SIGKILL escalation (port %d)", name, target.BrokerPort)
		if pauseHTTPErr != nil {
			shutdownErr = fmt.Errorf("%w; admin/pause: %v", shutdownErr, pauseHTTPErr)
		}
		_ = Update(name, func(ws *Workspace) error {
			ws.State = StateError
			return nil
		})
		return shutdownErr
	}

	return Update(name, func(ws *Workspace) error {
		ws.State = StatePaused
		return nil
	})
}

// Resume spawns the named workspace's broker and waits for it to bind.
func Resume(ctx context.Context, name string) error {
	reg, err := Read()
	if err != nil {
		return err
	}
	var target *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			target = ws
			break
		}
	}
	if target == nil {
		return ErrWorkspaceNotFound
	}

	if err := Update(name, func(ws *Workspace) error {
		ws.State = StateStarting
		return nil
	}); err != nil {
		return err
	}

	if err := Spawn(name, target.RuntimeHome, target.BrokerPort, target.WebPort); err != nil {
		_ = Update(name, func(ws *Workspace) error {
			ws.State = StateError
			return nil
		})
		return fmt.Errorf("workspaces: resume %q: spawn: %w", name, err)
	}

	return Update(name, func(ws *Workspace) error {
		ws.State = StateRunning
		ws.PausedAt = time.Time{}
		ws.LastUsedAt = time.Now().UTC()
		return nil
	})
}

// Shred moves the workspace tree to trash and removes it from the registry.
// If permanent is true the tree is deleted immediately.
func Shred(ctx context.Context, name string, permanent bool) error {
	reg, err := Read()
	if err != nil {
		return err
	}
	var target *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			target = ws
			break
		}
	}
	if target == nil {
		return ErrWorkspaceNotFound
	}

	home := config.RuntimeHomeDir()
	wuphfDir := filepath.Join(target.RuntimeHome, ".wuphf")

	if permanent {
		if _, err := workspace.ShredAt(wuphfDir); err != nil {
			return fmt.Errorf("workspaces: shred %q: %w", name, err)
		}
		_ = os.RemoveAll(target.RuntimeHome)
	} else {
		sd, err := spacesDir()
		if err != nil {
			return err
		}
		trashDir := filepath.Join(sd, trashDirName)
		if err := os.MkdirAll(trashDir, 0o700); err != nil {
			return fmt.Errorf("workspaces: shred %q: mkdir trash: %w", name, err)
		}
		trashEntry := filepath.Join(trashDir,
			fmt.Sprintf("%s-%d", name, time.Now().Unix()))
		if err := os.Rename(target.RuntimeHome, trashEntry); err != nil {
			return fmt.Errorf("workspaces: shred %q: move to trash: %w", name, err)
		}
	}

	// Delete token file.
	_ = os.Remove(tokenFilePath(home, name))

	// Remove ~/.wuphf symlink only if shredding main.
	if name == "main" {
		symlinkPath := filepath.Join(home, ".wuphf")
		if info, err := os.Lstat(symlinkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(symlinkPath)
		}
	}

	return removeFromRegistry(name)
}

// Restore moves a trash entry back to the workspaces directory, assigns a
// fresh port pair, and registers it.
func Restore(ctx context.Context, trashID string) error {
	sd, err := spacesDir()
	if err != nil {
		return err
	}

	trashDir := filepath.Join(sd, trashDirName)
	trashEntry := filepath.Join(trashDir, trashID)

	if info, err := os.Stat(trashEntry); err != nil || !info.IsDir() {
		if err != nil {
			return fmt.Errorf("workspaces: restore: trash entry %q: %w", trashID, err)
		}
		return fmt.Errorf("workspaces: restore: %q is not a directory", trashID)
	}

	originalName := extractOriginalName(trashID)
	if originalName == "" {
		return fmt.Errorf("workspaces: restore: cannot infer workspace name from %q", trashID)
	}

	lf, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lf)

	rp, err := registryPath()
	if err != nil {
		return err
	}
	reg, err := readFile(rp)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		reg = &Registry{Version: Version, CLICurrent: originalName}
	}

	for _, ws := range reg.Workspaces {
		if ws.Name == originalName {
			return fmt.Errorf("workspaces: restore: workspace %q already exists", originalName)
		}
	}

	brokerPort, webPort, err := AllocatePortPair(reg)
	if err != nil {
		return err
	}

	dest := filepath.Join(sd, originalName)
	if err := os.Rename(trashEntry, dest); err != nil {
		return fmt.Errorf("workspaces: restore: move %s → %s: %w", trashEntry, dest, err)
	}

	now := time.Now().UTC()
	reg.Workspaces = append(reg.Workspaces, &Workspace{
		Name:        originalName,
		RuntimeHome: dest,
		BrokerPort:  brokerPort,
		WebPort:     webPort,
		State:       StateNeverStarted,
		CreatedAt:   now,
		LastUsedAt:  now,
	})
	return writeUnderLock(reg)
}

// LiveWorkspace decorates a Workspace with its actual liveness state.
type LiveWorkspace struct {
	*Workspace
	Live bool // true if broker port accepts HTTP HEAD
}

// List reads the registry and decorates each workspace with a parallel
// liveness probe (200ms timeout per probe). Total latency is bounded by
// 200ms regardless of workspace count.
func List(ctx context.Context) ([]*LiveWorkspace, error) {
	reg, err := Read()
	if err != nil {
		return nil, err
	}

	results := make([]*LiveWorkspace, len(reg.Workspaces))
	for i, ws := range reg.Workspaces {
		results[i] = &LiveWorkspace{Workspace: ws}
	}

	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(lws *LiveWorkspace) {
			defer wg.Done()
			lws.Live = probePort(lws.BrokerPort)
		}(results[i])
	}
	wg.Wait()
	return results, nil
}

// Doctor reconciles the registry against the filesystem and running processes.
// It auto-repairs: stopping→paused reconciliation, missing symlink recreation.
func Doctor(ctx context.Context) (DoctorReport, error) {
	var report DoctorReport

	home := config.RuntimeHomeDir()
	if home == "" {
		return report, errors.New("workspaces: doctor: cannot resolve home directory")
	}

	sd, err := spacesDir()
	if err != nil {
		return report, err
	}

	reg, regErr := Read()
	if regErr != nil {
		if !errors.Is(regErr, ErrRegistryNotFound) {
			report.CorruptRegistry = true
		}
		reg = &Registry{}
	}

	known := make(map[string]bool, len(reg.Workspaces))
	for _, ws := range reg.Workspaces {
		known[ws.Name] = true
	}

	// Orphan trees: dirs in spaces/ not in registry.
	if entries, err := os.ReadDir(sd); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			n := e.Name()
			if n == trashDirName || strings.HasPrefix(n, ".") {
				continue
			}
			if !known[n] {
				report.OrphanTrees = append(report.OrphanTrees, filepath.Join(sd, n))
			}
		}
	}

	// Zombie running + stopping→paused reconciliation.
	for _, ws := range reg.Workspaces {
		switch ws.State {
		case StateRunning, StateStopping:
			if !probePort(ws.BrokerPort) {
				report.ZombieRunning = append(report.ZombieRunning,
					fmt.Sprintf("%s (port %d unbound)", ws.Name, ws.BrokerPort))
				if ws.State == StateStopping {
					_ = Update(ws.Name, func(w *Workspace) error {
						w.State = StatePaused
						return nil
					})
					report.Actions = append(report.Actions,
						fmt.Sprintf("reconciled %q: stopping → paused", ws.Name))
				}
			}
		}
	}

	// ~/.wuphf symlink health.
	symlinkPath := filepath.Join(home, ".wuphf")
	expectedTarget := filepath.Join(sd, "main", ".wuphf")

	symlinkInfo, symlinkErr := os.Lstat(symlinkPath)
	switch {
	case os.IsNotExist(symlinkErr):
		if _, err := os.Stat(expectedTarget); err == nil {
			report.SymlinkMissing = true
			if err := os.Symlink(expectedTarget, symlinkPath); err == nil {
				report.Actions = append(report.Actions, "recreated ~/.wuphf symlink")
			}
		}
	case symlinkErr == nil && symlinkInfo.Mode()&os.ModeSymlink == 0:
		if _, err := os.Stat(expectedTarget); err == nil {
			report.PartialMigration = true
		}
	case symlinkErr == nil && symlinkInfo.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(symlinkPath)
		if err == nil && filepath.Clean(target) != filepath.Clean(expectedTarget) {
			report.SymlinkWrong = fmt.Sprintf("points to %s, expected %s", target, expectedTarget)
		}
	}

	return report, nil
}

// ---- internal helpers -------------------------------------------------------

func probePort(port int) bool {
	client := &http.Client{Timeout: liveProbeTimeout}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func postAdminPause(port int, token string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/admin/pause", port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("admin/pause: status %d", resp.StatusCode)
	}
	return nil
}

func readPIDFile(runtimeHome string) int {
	pidPath := filepath.Join(runtimeHome, ".wuphf", "broker.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

func tokenFilePath(home, name string) string {
	return filepath.Join(home, ".wuphf-spaces", "tokens", name+".token")
}

func readTokenFile(name string) (string, error) {
	home := config.RuntimeHomeDir()
	if home == "" {
		return "", errors.New("workspaces: cannot resolve home directory")
	}
	data, err := os.ReadFile(tokenFilePath(home, name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// extractOriginalName recovers the workspace name from a trash ID of the
// form <name>-<unix-timestamp>. It finds the last hyphen followed by a pure
// numeric suffix.
func extractOriginalName(trashID string) string {
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

// removeFromRegistry removes name from registry under flock.
func removeFromRegistry(name string) error {
	lf, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lf)

	rp, err := registryPath()
	if err != nil {
		return err
	}
	reg, err := readFile(rp)
	if err != nil {
		return err
	}

	newList := make([]*Workspace, 0, len(reg.Workspaces))
	for _, ws := range reg.Workspaces {
		if ws.Name != name {
			newList = append(newList, ws)
		}
	}
	reg.Workspaces = newList
	if reg.CLICurrent == name && len(reg.Workspaces) > 0 {
		reg.CLICurrent = reg.Workspaces[0].Name
	}
	return writeUnderLock(reg)
}
