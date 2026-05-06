package workspaces

// doctor_fix.go implements typed per-issue dispatch for Doctor's findings.
// Doctor itself auto-applies the safe reconcile actions (stopping → paused,
// missing-symlink recreate). FixDoctorIssue is the explicit, idempotent
// remediation entrypoint for the remaining advisory issues that the CLI
// surfaces to the user one at a time.
//
// FixID prefix grammar:
//
//	orphan_tree:<absolute-path>           — register the orphan into the registry
//	orphan_tree:delete:<absolute-path>    — delete the orphan tree (move to trash)
//	zombie:<name> [(port N unbound)]      — reconcile registry state to paused
//	port:<port>                           — manual fix required (returned as error)
//	corrupt:registry                      — force-restore from registry.json.bak
//	symlink:missing                       — recreate ~/.wuphf compatibility symlink
//	symlink:wrong                         — replace mis-pointed compatibility symlink
//	migration:partial                     — manual fix required (returned as error)

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrManualFixRequired is returned by FixDoctorIssue when the issue cannot be
// automatically remediated and the user must intervene. The CLI surfaces this
// to the user verbatim. Callers can use errors.Is to detect it.
var ErrManualFixRequired = errors.New("workspaces: manual fix required")

// ErrUnknownFixID is returned when the FixID prefix does not match any known
// dispatch case.
var ErrUnknownFixID = errors.New("workspaces: unknown fix id")

// FixDoctorIssue applies the typed per-issue fix selected by fixID. The fixID
// values are produced by translateDoctorReport in cmd/wuphf/workspaces_adapter.go
// and consumed here. Each fix is idempotent: calling twice on the same fixID
// either succeeds again or returns a benign "nothing to do" result.
//
// Errors:
//   - ErrManualFixRequired wrapped with a human-readable detail when no
//     automated remediation is possible (port conflict, partial migration).
//   - ErrUnknownFixID when the prefix is not recognised.
//   - The underlying error from the dispatched fix on infrastructure failure.
func FixDoctorIssue(ctx context.Context, fixID string) error {
	if fixID == "" {
		return fmt.Errorf("%w: empty fix id", ErrUnknownFixID)
	}

	prefix, rest := splitFixID(fixID)
	switch prefix {
	case "orphan_tree":
		// orphan_tree:delete:<path> → delete; otherwise register.
		if sub, p, ok := splitOnce(rest, ":"); ok && sub == "delete" {
			return fixOrphanTreeDelete(p)
		}
		return fixOrphanTreeRegister(rest)

	case "zombie":
		return fixZombie(rest)

	case "port":
		return fmt.Errorf("%w: port %s held by another process; stop the conflicting process and re-run doctor",
			ErrManualFixRequired, rest)

	case "corrupt":
		if rest != "registry" {
			return fmt.Errorf("%w: corrupt:%s", ErrUnknownFixID, rest)
		}
		return fixCorruptRegistry()

	case "symlink":
		switch rest {
		case "missing":
			return fixSymlinkMissing()
		case "wrong":
			return fixSymlinkWrong()
		default:
			return fmt.Errorf("%w: symlink:%s", ErrUnknownFixID, rest)
		}

	case "migration":
		if rest == "partial" {
			return fmt.Errorf("%w: ~/.wuphf is a regular directory while ~/.wuphf-spaces/main exists; "+
				"consolidate manually — see docs/multi-workspace.md", ErrManualFixRequired)
		}
		return fmt.Errorf("%w: migration:%s", ErrUnknownFixID, rest)

	default:
		return fmt.Errorf("%w: %q", ErrUnknownFixID, fixID)
	}
}

// splitFixID separates the prefix (everything before the FIRST colon) from
// the rest. If there is no colon, prefix is the whole string and rest is "".
func splitFixID(fixID string) (prefix, rest string) {
	if i := strings.IndexByte(fixID, ':'); i >= 0 {
		return fixID[:i], fixID[i+1:]
	}
	return fixID, ""
}

// splitOnce splits s into (head, tail) at the first occurrence of sep. ok is
// false if sep is not present.
func splitOnce(s, sep string) (head, tail string, ok bool) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// ---- orphan_tree -----------------------------------------------------------

// fixOrphanTreeRegister registers an existing tree under ~/.wuphf-spaces/<name>
// into the registry with a freshly allocated port pair. Idempotent: if the
// directory's basename is already a registered workspace, returns nil.
func fixOrphanTreeRegister(treePath string) error {
	if treePath == "" {
		return fmt.Errorf("orphan_tree: missing path")
	}
	info, err := os.Stat(treePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Already removed — idempotent success.
			return nil
		}
		return fmt.Errorf("orphan_tree: stat %s: %w", treePath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("orphan_tree: %s is not a directory", treePath)
	}

	sd, err := spacesDir()
	if err != nil {
		return err
	}
	// Refuse to register paths outside ~/.wuphf-spaces — this is the only
	// place orphan trees can legitimately live.
	if !pathInsideDir(treePath, sd) {
		return fmt.Errorf("orphan_tree: %s is not inside %s", treePath, sd)
	}
	name := filepath.Base(treePath)
	if err := ValidateSlug(name); err != nil {
		return fmt.Errorf("orphan_tree: invalid workspace name %q derived from path: %w", name, err)
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
		if errors.Is(err, os.ErrNotExist) {
			// Genuine first-run: no registry file exists yet.
			reg = &Registry{Version: Version, CLICurrent: name}
		} else {
			// Primary corrupt — try the backup before giving up.
			bak := rp + ".bak"
			reg, err = readFile(bak)
			if err != nil {
				// Both primary and backup unreadable. Fail closed rather
				// than bootstrapping a fresh registry that would strand
				// every other registered workspace.
				return fmt.Errorf("workspaces: orphan-register %q: registry and backup both unreadable — run 'wuphf workspace doctor --dry-run' for manual recovery: %w", name, err)
			}
		}
	}

	// Idempotency: name already registered → success.
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			return nil
		}
	}

	brokerPort, webPort, err := AllocatePortPair(reg)
	if err != nil {
		return fmt.Errorf("orphan_tree: allocate ports for %q: %w", name, err)
	}

	now := time.Now().UTC()
	reg.Workspaces = append(reg.Workspaces, &Workspace{
		Name:        name,
		RuntimeHome: treePath,
		BrokerPort:  brokerPort,
		WebPort:     webPort,
		State:       StateNeverStarted,
		CreatedAt:   now,
		LastUsedAt:  now,
	})
	if reg.Version == 0 {
		reg.Version = Version
	}
	if reg.CLICurrent == "" {
		reg.CLICurrent = name
	}
	if err := writeUnderLock(reg); err != nil {
		return fmt.Errorf("orphan_tree: register %q: %w", name, err)
	}
	return nil
}

// fixOrphanTreeDelete moves the orphan tree to ~/.wuphf-spaces/.trash/. It
// does NOT touch the registry (the tree was orphaned because it wasn't there
// in the first place). Idempotent: returns nil if the tree is already gone.
func fixOrphanTreeDelete(treePath string) error {
	if treePath == "" {
		return fmt.Errorf("orphan_tree:delete: missing path")
	}
	if _, err := os.Stat(treePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("orphan_tree:delete: stat %s: %w", treePath, err)
	}
	sd, err := spacesDir()
	if err != nil {
		return err
	}
	if !pathInsideDir(treePath, sd) {
		return fmt.Errorf("orphan_tree:delete: %s is not inside %s", treePath, sd)
	}
	trashDir := filepath.Join(sd, trashDirName)
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return fmt.Errorf("orphan_tree:delete: mkdir trash: %w", err)
	}
	dest := filepath.Join(trashDir, fmt.Sprintf("%s-%d", filepath.Base(treePath), time.Now().Unix()))
	if err := os.Rename(treePath, dest); err != nil {
		return fmt.Errorf("orphan_tree:delete: move to trash: %w", err)
	}
	return nil
}

func pathInsideDir(path, dir string) bool {
	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != "."
}

// ---- zombie ----------------------------------------------------------------

// fixZombie reconciles a workspace whose registry state is running/stopping
// but whose broker port is unbound. The orchestrator's Doctor encodes the
// FixID as "zombie:<name> (port N unbound)" — we strip everything from the
// first space onward to recover the name. Idempotent.
func fixZombie(rest string) error {
	name := rest
	if i := strings.IndexByte(name, ' '); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return fmt.Errorf("zombie: missing workspace name")
	}

	reg, err := Read()
	if err != nil {
		return fmt.Errorf("zombie: read registry: %w", err)
	}
	var target *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == name {
			target = ws
			break
		}
	}
	if target == nil {
		return fmt.Errorf("zombie: %w: %s", ErrWorkspaceNotFound, name)
	}

	// If the broker is actually live now, nothing to do.
	if probePort(target.BrokerPort) {
		return nil
	}

	// Already paused → idempotent no-op.
	if target.State == StatePaused {
		return nil
	}

	return Update(name, func(w *Workspace) error {
		w.State = StatePaused
		return nil
	})
}

// ---- corrupt:registry ------------------------------------------------------

// fixCorruptRegistry forces a restore from registry.json.bak. Unlike Read's
// implicit fallback (which only kicks in when the primary fails to parse),
// this helper unconditionally promotes the .bak to the primary slot. Used by
// the user when the primary's contents are wrong-but-parseable.
//
// If no .bak exists, returns ErrManualFixRequired.
func fixCorruptRegistry() error {
	rp, err := registryPath()
	if err != nil {
		return err
	}
	bak := rp + ".bak"

	lf, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lf)

	bakData, err := os.ReadFile(bak)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: registry.json.bak does not exist; recover from a backup tool", ErrManualFixRequired)
		}
		return fmt.Errorf("corrupt:registry: read bak: %w", err)
	}
	// Validate that the bak parses before we promote it.
	if _, err := readFile(bak); err != nil {
		return fmt.Errorf("%w: registry.json.bak unreadable (%w); recover from a backup tool",
			ErrManualFixRequired, err)
	}

	tmp := rp + ".tmp"
	if err := os.WriteFile(tmp, bakData, 0o600); err != nil {
		return fmt.Errorf("corrupt:registry: write temp: %w", err)
	}
	if err := os.Rename(tmp, rp); err != nil {
		return fmt.Errorf("corrupt:registry: promote bak: %w", err)
	}
	return nil
}

// ---- symlink ---------------------------------------------------------------

// fixSymlinkMissing creates the ~/.wuphf → ~/.wuphf-spaces/main/.wuphf
// compatibility symlink. Idempotent: if a correct symlink already exists,
// returns nil.
func fixSymlinkMissing() error {
	_, symlinkPath, expectedTarget, err := symlinkPaths()
	if err != nil {
		return err
	}
	info, lerr := os.Lstat(symlinkPath)
	if lerr == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			// Regular file or directory — partial-migration case, refuse to clobber.
			return fmt.Errorf("%w: ~/.wuphf is a regular path, not a symlink — see docs/multi-workspace.md",
				ErrManualFixRequired)
		}
		// It's a symlink; check whether it already points at the right place.
		if cur, rerr := os.Readlink(symlinkPath); rerr == nil &&
			filepath.Clean(cur) == filepath.Clean(expectedTarget) {
			return nil
		}
	}
	if _, err := os.Stat(expectedTarget); err != nil {
		return fmt.Errorf("symlink:missing: target %s does not exist: %w", expectedTarget, err)
	}
	if err := os.Symlink(expectedTarget, symlinkPath); err != nil {
		return fmt.Errorf("symlink:missing: create %s → %s: %w", symlinkPath, expectedTarget, err)
	}
	return nil
}

// fixSymlinkWrong removes the existing symlink (only if it IS a symlink) and
// recreates it pointing at the expected target. Idempotent: a correct symlink
// is left alone.
func fixSymlinkWrong() error {
	_, symlinkPath, expectedTarget, err := symlinkPaths()
	if err != nil {
		return err
	}
	info, lerr := os.Lstat(symlinkPath)
	if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		cur, rerr := os.Readlink(symlinkPath)
		if rerr == nil && filepath.Clean(cur) == filepath.Clean(expectedTarget) {
			return nil // already correct
		}
		if err := os.Remove(symlinkPath); err != nil {
			return fmt.Errorf("symlink:wrong: remove existing: %w", err)
		}
	} else if lerr == nil && info.Mode()&os.ModeSymlink == 0 {
		// It's a regular file or directory — that's the partial-migration case,
		// not symlink:wrong. Refuse to clobber user data.
		return fmt.Errorf("%w: ~/.wuphf is a regular path, not a symlink — see docs/multi-workspace.md",
			ErrManualFixRequired)
	}
	if _, err := os.Stat(expectedTarget); err != nil {
		return fmt.Errorf("symlink:wrong: target %s does not exist: %w", expectedTarget, err)
	}
	if err := os.Symlink(expectedTarget, symlinkPath); err != nil {
		return fmt.Errorf("symlink:wrong: recreate %s → %s: %w", symlinkPath, expectedTarget, err)
	}
	return nil
}

// symlinkPaths resolves the user's REAL home (NOT WUPHF_RUNTIME_HOME), the
// ~/.wuphf symlink path, and the expected target ~/.wuphf-spaces/main/.wuphf.
//
// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — the compatibility
// symlink lives at the user's real home alongside ~/.wuphf-spaces/, which is
// the shared cross-workspace root. Using RuntimeHomeDir here would point at
// a per-workspace tree and create the symlink in the wrong place when
// WUPHF_RUNTIME_HOME is set (tests, dev isolation, workspace overrides).
func symlinkPaths() (home, symlinkPath, expectedTarget string, err error) {
	home, herr := os.UserHomeDir()
	if herr != nil || home == "" {
		return "", "", "", errors.New("workspaces: cannot resolve user home directory")
	}
	sd, derr := spacesDir()
	if derr != nil {
		return "", "", "", derr
	}
	return home, filepath.Join(home, ".wuphf"), filepath.Join(sd, "main", ".wuphf"), nil
}
