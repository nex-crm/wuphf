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
//
// For managing the set of workspaces (list, create, switch, pause, resume),
// see internal/workspaces (plural).
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

// ClearRuntime performs a narrow reset on the user-default workspace tree at
// config.RuntimeHomeDir()/.wuphf. It deletes the broker state file and the
// last-good snapshot. Safe to call when no broker is running. The live broker
// may keep using the same office.pid and team directory; callers that want to
// clear in-memory runtime should do so separately.
//
// Equivalent to ResetAt(<default wuphf home>). Both honor
// WUPHF_BROKER_STATE_PATH when set.
func ClearRuntime() (Result, error) {
	home, err := wuphfHome()
	if err != nil {
		return Result{}, err
	}
	return ResetAt(home)
}

// Shred performs a full workspace wipe on the user-default workspace tree at
// config.RuntimeHomeDir()/.wuphf. Runs the same wipe set as ShredAt and, for
// parity with the previous implementation, additionally removes any env-
// overridden onboarding state path (onboarding.StatePath) and company manifest
// path (company.ManifestPath, which may resolve to WUPHF_COMPANY_FILE,
// NEX_COMPANY_FILE, or a CWD-local wuphf.company.json) when those resolve
// outside the wuphfHome tree.
func Shred() (Result, error) {
	home, err := wuphfHome()
	if err != nil {
		return Result{}, err
	}
	res, err := ShredAt(home)
	if err != nil {
		return res, err
	}
	// Cover env-overridden locations that ShredAt cannot see because it is
	// scoped to a wuphfHome tree. removeIfPresent is a no-op when the path
	// has already been removed via ShredAt or does not exist.
	res.removeIfPresent(onboarding.StatePath())
	res.removeIfPresent(company.ManifestPath())
	return res, nil
}

// ResetAt performs a narrow reset on an explicit workspace tree rooted at
// wuphfHome (the .wuphf subdirectory of a workspace's runtime home). It
// deletes the broker state file and the last-good snapshot. Honors
// WUPHF_BROKER_STATE_PATH when set, in which case the override path replaces
// the in-tree default.
//
// ClearRuntime delegates here using config.RuntimeHomeDir()/.wuphf as the
// canonical wipe set.
func ResetAt(wuphfHome string) (Result, error) {
	var res Result
	statePath := filepath.Join(wuphfHome, "team", "broker-state.json")
	snapshotPath := statePath + ".last-good"
	if p := strings.TrimSpace(os.Getenv("WUPHF_BROKER_STATE_PATH")); p != "" {
		statePath = p
		snapshotPath = p + ".last-good"
	}
	res.removeIfPresent(statePath)
	res.removeIfPresent(snapshotPath)
	return res, nil
}

// ShredAt performs a full workspace wipe on an explicit workspace tree rooted
// at wuphfHome (the .wuphf subdirectory of a workspace's runtime home). It
// runs ResetAt first, then removes onboarded.json, company.json, and the
// directories holding office task receipts, workflows, logs, sessions,
// provider session state, codex-headless cache, wiki, wiki.bak, and the
// calendar JSON file.
//
// Shred delegates here using config.RuntimeHomeDir()/.wuphf and additionally
// covers env-overridden onboarding/company paths that may resolve outside the
// wuphfHome tree.
func ShredAt(wuphfHome string) (Result, error) {
	res, err := ResetAt(wuphfHome)
	if err != nil {
		return res, err
	}
	res.removeIfPresent(filepath.Join(wuphfHome, "onboarded.json"))
	res.removeIfPresent(filepath.Join(wuphfHome, "company.json"))
	res.removeIfPresent(filepath.Join(wuphfHome, "office"))
	res.removeIfPresent(filepath.Join(wuphfHome, "workflows"))
	res.removeIfPresent(filepath.Join(wuphfHome, "logs"))
	res.removeIfPresent(filepath.Join(wuphfHome, "sessions"))
	res.removeIfPresent(filepath.Join(wuphfHome, "providers"))
	res.removeIfPresent(filepath.Join(wuphfHome, "codex-headless"))
	res.removeIfPresent(filepath.Join(wuphfHome, "wiki"))
	res.removeIfPresent(filepath.Join(wuphfHome, "wiki.bak"))
	res.removeIfPresent(filepath.Join(wuphfHome, "calendar.json"))
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
