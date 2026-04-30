// Package workspaces manages the set of WUPHF workspaces — the registry,
// orchestrator, and lifecycle. Operations within a single workspace
// (Reset/Shred of state) live in internal/workspace (singular).
package workspaces

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// Version is the current registry schema version.
const Version = 1

// State describes the lifecycle state of a workspace.
type State string

const (
	StateRunning      State = "running"
	StatePaused       State = "paused"
	StateStarting     State = "starting"
	StateStopping     State = "stopping"
	StateNeverStarted State = "never_started"
	StateError        State = "error"
)

// Registry is the top-level structure stored in ~/.wuphf-spaces/registry.json.
type Registry struct {
	Version    int          `json:"version"`
	CLICurrent string       `json:"cli_current"`
	Workspaces []*Workspace `json:"workspaces"`
}

// Workspace is one entry in the registry, representing a fully-isolated
// WUPHF instance with its own runtime home, ports, and lifecycle state.
type Workspace struct {
	Name        string    `json:"name"`
	RuntimeHome string    `json:"runtime_home"`
	BrokerPort  int       `json:"broker_port"`
	WebPort     int       `json:"web_port"`
	State       State     `json:"state"`
	Blueprint   string    `json:"blueprint"`
	CompanyName string    `json:"company_name"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	PausedAt    time.Time `json:"paused_at,omitempty"`
}

// spacesDir returns ~/.wuphf-spaces at the user's REAL home dir. This is the
// shared cross-workspace registry directory: every workspace's broker
// reads/writes the same registry.json and the same tokens/. If we used
// config.RuntimeHomeDir() here, a broker running with WUPHF_RUNTIME_HOME set
// would resolve spacesDir under its OWN runtime tree and never see siblings.
//
// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — the spaces
// directory is the cross-workspace orchestration root, not per-workspace state.
func spacesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fall back to RuntimeHomeDir's resolution if HOME is unset (e.g. in
		// tests that explicitly clear it).
		home = config.RuntimeHomeDir()
	}
	if home == "" {
		return "", errors.New("workspaces: cannot resolve home directory")
	}
	return filepath.Join(home, ".wuphf-spaces"), nil
}

// registryPath returns the canonical path to registry.json.
func registryPath() (string, error) {
	dir, err := spacesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.json"), nil
}

// lockPath returns the path to the advisory flock file.
func lockPath() (string, error) {
	dir, err := spacesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.lock"), nil
}

// acquireLock opens (or creates) the lock file and acquires an exclusive
// POSIX flock. Callers must defer releaseLock.
func acquireLock() (*os.File, error) {
	lp, err := lockPath()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(lp)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("workspaces: mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("workspaces: open lock %s: %w", lp, err)
	}
	if err := lockFileExclusive(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("workspaces: flock %s: %w", lp, err)
	}
	return f, nil
}

// releaseLock releases the flock and closes the file.
func releaseLock(f *os.File) {
	if f == nil {
		return
	}
	_ = unlockFile(f)
	_ = f.Close()
}

// contentHash returns the hex-encoded SHA-256 of data.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Read loads the registry from disk. If registry.json fails to parse it
// falls back to registry.json.bak. Returns ErrRegistryNotFound if neither
// file exists (first-run case).
func Read() (*Registry, error) {
	rp, err := registryPath()
	if err != nil {
		return nil, err
	}
	reg, err := readFile(rp)
	if err == nil {
		return reg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		// Primary file missing. Try the backup first: an interrupted
		// write-temp-rename (process killed between the two renames) can
		// leave the only valid copy in registry.json.bak.
		bak := rp + ".bak"
		bakReg, bakErr := readFile(bak)
		if bakErr == nil {
			return bakReg, nil
		}
		return nil, ErrRegistryNotFound
	}
	// Main file parse failure — try backup.
	bak := rp + ".bak"
	reg, bakErr := readFile(bak)
	if bakErr != nil {
		// Return original parse error as the primary.
		return nil, fmt.Errorf("workspaces: registry parse failed (%w); backup unreadable (%w)", err, bakErr)
	}
	return reg, nil
}

// ErrRegistryNotFound is returned when no registry exists yet (fresh install).
var ErrRegistryNotFound = errors.New("workspaces: registry not found")

func readFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &reg, nil
}

// Write serializes reg to disk using write-temp-then-rename under an
// exclusive flock. It rotates the previous registry.json to registry.json.bak
// on success and verifies the written content via a hash check.
func Write(reg *Registry) error {
	lf, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lf)
	return writeUnderLock(reg)
}

// writeUnderLock performs the actual write; callers must hold the flock.
func writeUnderLock(reg *Registry) error {
	rp, err := registryPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(rp)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workspaces: mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspaces: marshal registry: %w", err)
	}
	wantHash := contentHash(data)

	tmp := rp + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("workspaces: write temp %s: %w", tmp, err)
	}

	// Rotate existing registry to .bak before rename.
	if _, err := os.Stat(rp); err == nil {
		if err := os.Rename(rp, rp+".bak"); err != nil {
			return fmt.Errorf("workspaces: rotate bak: %w", err)
		}
	}

	if err := os.Rename(tmp, rp); err != nil {
		return fmt.Errorf("workspaces: rename temp→registry: %w", err)
	}

	// Read-after-write hash verification.
	written, err := os.ReadFile(rp)
	if err != nil {
		return fmt.Errorf("workspaces: read-after-write verify: %w", err)
	}
	if gotHash := contentHash(written); gotHash != wantHash {
		return fmt.Errorf("workspaces: read-after-write hash mismatch (want %s, got %s)", wantHash, gotHash)
	}
	return nil
}

// Update reads the registry, calls fn on the workspace named name, then
// writes back — all under a single flock. Returns ErrWorkspaceNotFound if
// name is absent.
func Update(name string, fn func(*Workspace) error) error {
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
			return ErrRegistryNotFound
		}
		// Try backup under the same lock.
		bak := rp + ".bak"
		reg, err = readFile(bak)
		if err != nil {
			return fmt.Errorf("workspaces: read for update: %w", err)
		}
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
	if err := fn(target); err != nil {
		return err
	}
	return writeUnderLock(reg)
}

// ErrWorkspaceNotFound is returned when the named workspace is absent from
// the registry.
var ErrWorkspaceNotFound = errors.New("workspaces: workspace not found")
