// Package workspace wipes WUPHF's on-disk state for two distinct blast radii:
//
//   - Reset: narrow. Clears broker runtime state so a stuck office can restart
//     clean. Preserves task worktrees, team, company, office history, and
//     workflows. Equivalent to what `wuphf shred` did before the verb swap.
//
//   - Shred: full. Everything Reset does, plus deletes the team roster, company
//     identity, the office's task receipts, saved workflows, logs, sessions,
//     provider state, calendar, and local markdown memory. The next load shows
//     the onboarding wizard.
//
// Preserved in both cases: office.pid, task-worktrees/, openclaw/, config.json.
// In-flight work remains on disk so branches and local changes inside task
// worktrees survive, and credentials/preferences stay available for the next
// launch.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/onboarding"
)

// Result reports which paths the operation actually removed and collects any
// non-fatal errors. A path is "removed" only if it existed before the call.
type Result struct {
	Removed []string `json:"removed"`
	Errors  []string `json:"errors,omitempty"`
}

// ClearRuntime performs a narrow reset: deletes the broker state file and the
// last-good snapshot. Safe to call when no broker is running. The live broker
// may keep using the same office.pid and team directory; callers that want to
// clear in-memory runtime should do so separately.
func ClearRuntime() (Result, error) {
	var res Result
	statePath, snapshotPath, err := brokerStatePaths()
	if err != nil {
		return Result{}, err
	}
	res.removeIfPresent(statePath)
	res.removeIfPresent(snapshotPath)
	return res, nil
}

// Shred performs a full workspace wipe. Runs ClearRuntime first, then removes
// onboarding state, company identity, office task receipts, workflows, logs,
// provider session state, and local markdown memory.
func Shred() (Result, error) {
	home, err := wuphfHome()
	if err != nil {
		return Result{}, err
	}
	res, err := ClearRuntime()
	if err != nil {
		return res, err
	}
	res.removeIfPresent(onboarding.StatePath())
	res.removeIfPresent(company.ManifestPath())
	res.removeIfPresent(filepath.Join(home, "office"))
	res.removeIfPresent(filepath.Join(home, "workflows"))
	res.removeIfPresent(filepath.Join(home, "logs"))
	res.removeIfPresent(filepath.Join(home, "sessions"))
	res.removeIfPresent(filepath.Join(home, "providers"))
	res.removeIfPresent(filepath.Join(home, "codex-headless"))
	res.removeIfPresent(filepath.Join(home, "wiki"))
	res.removeIfPresent(filepath.Join(home, "wiki.bak"))
	res.removeIfPresent(filepath.Join(home, "calendar.json"))
	return res, nil
}

// wuphfHome returns the absolute path to ~/.wuphf, honoring WUPHF_RUNTIME_HOME
// so tests and sandboxed runs stay isolated from the real user directory.
func wuphfHome() (string, error) {
	home := config.RuntimeHomeDir()
	if home == "" {
		return "", errors.New("workspace: could not resolve home directory")
	}
	return filepath.Join(home, ".wuphf"), nil
}

func brokerStatePaths() (string, string, error) {
	if p := strings.TrimSpace(os.Getenv("WUPHF_BROKER_STATE_PATH")); p != "" {
		return p, p + ".last-good", nil
	}
	home, err := wuphfHome()
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(home, "team", "broker-state.json")
	return path, path + ".last-good", nil
}

func (r *Result) removeIfPresent(path string) {
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("stat %s: %v", path, err))
		return
	}
	var rmErr error
	if info.IsDir() {
		rmErr = os.RemoveAll(path)
	} else {
		rmErr = os.Remove(path)
	}
	if rmErr != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("remove %s: %v", path, rmErr))
		return
	}
	r.Removed = append(r.Removed, path)
}
