package workspaces

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

const (
	migrationLockName   = ".wuphf-migration.lock"
	mainBrokerProbePort = 7890
	migrationProbeTO    = 2 * time.Second
)

// brokerRunningFn is a var so tests can inject a controlled probe result
// without spinning up a real HTTP server on the legacy port 7890.
var brokerRunningFn = isBrokerRunning

// MigrateToSymmetric migrates the legacy single-workspace layout
// (~/.wuphf/) to the symmetric multi-workspace layout
// (~/.wuphf-spaces/main/.wuphf/) on first workspace-aware launch.
//
// Steps:
//  1. Acquire migration lock at <home>/.wuphf-migration.lock.
//  2. If registry.json already exists, no-op (idempotent).
//  3. Probe broker port 7890; abort if a broker is running.
//  4. Atomic rename ~/.wuphf → ~/.wuphf-spaces/main/.wuphf.
//  5. Create compatibility symlink ~/.wuphf → ~/.wuphf-spaces/main/.wuphf.
//  6. Initialize registry.json with the single "main" entry.
//  7. Release lock.
//
// Recovery from a partial rename (power loss / SIGKILL mid-step) is handled
// by wuphf workspace doctor, not re-entrant migration.
func MigrateToSymmetric() error {
	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — the migration
	// operates on the legacy ~/.wuphf at the user's REAL home, and the lock
	// lives next to it. Using RuntimeHomeDir would point at a per-workspace
	// path that has no legacy tree to migrate.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fall back to RuntimeHomeDir for tests that explicitly clear HOME.
		home = config.RuntimeHomeDir()
	}
	if home == "" {
		return errors.New("workspaces: migrate: cannot resolve home directory")
	}

	// Ensure home exists so lock open doesn't fail on fresh / test setups.
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("workspaces: migrate: mkdir %s: %w", home, err)
	}

	lockPath := filepath.Join(home, migrationLockName)
	lf, err := openMigrationLock(lockPath)
	if err != nil {
		return fmt.Errorf("workspaces: migrate: acquire lock: %w", err)
	}
	defer releaseMigrationLock(lf)

	// Idempotency: if registry already exists, nothing to do.
	rp, err := registryPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(rp); err == nil {
		return nil // already migrated
	}

	// Guard: refuse to migrate if the old broker is running.
	if brokerRunningFn(mainBrokerProbePort) {
		return fmt.Errorf("workspaces: migrate: broker is running on port %d; "+
			"stop WUPHF before upgrading, then restart with `npx wuphf`",
			mainBrokerProbePort)
	}

	oldPath := filepath.Join(home, ".wuphf")
	spacesD, err := spacesDir()
	if err != nil {
		return err
	}
	mainRuntimeHome := filepath.Join(spacesD, "main")
	newPath := filepath.Join(mainRuntimeHome, ".wuphf")

	// Only rename if the old directory exists and is NOT already a symlink.
	if info, err := os.Lstat(oldPath); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.MkdirAll(mainRuntimeHome, 0o700); err != nil {
			return fmt.Errorf("workspaces: migrate: mkdir %s: %w", mainRuntimeHome, err)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("workspaces: migrate: rename %s → %s: %w", oldPath, newPath, err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// Fresh install — just create the dir.
		if err := os.MkdirAll(newPath, 0o700); err != nil {
			return fmt.Errorf("workspaces: migrate: mkdir %s: %w", newPath, err)
		}
	}

	// Compatibility symlink: ~/.wuphf → ~/.wuphf-spaces/main/.wuphf
	if _, err := os.Lstat(oldPath); err == nil {
		_ = os.Remove(oldPath)
	}
	if err := os.Symlink(newPath, oldPath); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("workspaces: migrate: symlink %s → %s: %w", oldPath, newPath, err)
	}

	// Initialize registry.
	now := time.Now().UTC()
	reg := &Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{
				Name:        "main",
				RuntimeHome: mainRuntimeHome,
				BrokerPort:  MainBrokerPort,
				WebPort:     MainWebPort,
				State:       StateNeverStarted,
				CreatedAt:   now,
				LastUsedAt:  now,
			},
		},
	}
	return writeUnderLock(reg)
}

// isBrokerRunning probes port via HTTP HEAD with a short timeout.
func isBrokerRunning(port int) bool {
	client := &http.Client{Timeout: migrationProbeTO}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func openMigrationLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFileExclusiveNonBlocking(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another migration is in progress (lock %s): %w", path, err)
	}
	return f, nil
}

func releaseMigrationLock(f *os.File) {
	if f == nil {
		return
	}
	_ = unlockFile(f)
	_ = f.Close()
}
