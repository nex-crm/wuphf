package workspaces

import (
	"os"
	"path/filepath"
	"testing"
)

func withMigrationHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// spacesDir uses real HOME because ~/.wuphf-spaces is shared cross-workspace.
	// Override HOME alongside WUPHF_RUNTIME_HOME so the migration writes inside
	// the tempdir instead of leaking into the developer's real ~/.wuphf-spaces.
	t.Setenv("HOME", dir)
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	// Inject a no-op broker probe so tests don't fail when a real broker
	// is running on port 7890 in the developer's environment.
	orig := brokerRunningFn
	brokerRunningFn = func(port int) bool { return false }
	t.Cleanup(func() { brokerRunningFn = orig })
	return dir
}

func TestMigrateToSymmetricHappyPath(t *testing.T) {
	home := withMigrationHome(t)

	// Create a legacy ~/.wuphf directory with some content.
	oldWuphf := filepath.Join(home, ".wuphf")
	if err := os.MkdirAll(filepath.Join(oldWuphf, "team"), 0o700); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldWuphf, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := MigrateToSymmetric(); err != nil {
		t.Fatalf("MigrateToSymmetric: %v", err)
	}

	// New path should exist.
	newPath := filepath.Join(home, ".wuphf-spaces", "main", ".wuphf")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new path %s: %v", newPath, err)
	}

	// Content should be present at new path.
	if _, err := os.Stat(filepath.Join(newPath, "config.json")); err != nil {
		t.Errorf("config.json missing at new path: %v", err)
	}

	// ~/.wuphf must be a compatibility symlink.
	oldPath := filepath.Join(home, ".wuphf")
	info, err := os.Lstat(oldPath)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected ~/.wuphf to be a symlink, got mode %v", info.Mode())
	}

	// Registry should be initialized with one "main" workspace.
	reg, err := Read()
	if err != nil {
		t.Fatalf("Read registry: %v", err)
	}
	if len(reg.Workspaces) != 1 || reg.Workspaces[0].Name != "main" {
		t.Errorf("unexpected registry: %+v", reg)
	}
	if reg.Workspaces[0].BrokerPort != MainBrokerPort {
		t.Errorf("main BrokerPort: want %d, got %d", MainBrokerPort, reg.Workspaces[0].BrokerPort)
	}
}

func TestMigrateToSymmetricIsIdempotent(t *testing.T) {
	home := withMigrationHome(t)

	oldWuphf := filepath.Join(home, ".wuphf")
	if err := os.MkdirAll(oldWuphf, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := MigrateToSymmetric(); err != nil {
		t.Fatalf("first MigrateToSymmetric: %v", err)
	}
	if err := MigrateToSymmetric(); err != nil {
		t.Fatalf("second MigrateToSymmetric: %v", err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reg.Workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(reg.Workspaces))
	}
}

func TestMigrateToSymmetricFreshInstall(t *testing.T) {
	home := withMigrationHome(t)

	// No legacy ~/.wuphf directory.
	oldWuphf := filepath.Join(home, ".wuphf")
	if _, err := os.Stat(oldWuphf); !os.IsNotExist(err) {
		t.Fatalf("expected no legacy dir, got err=%v", err)
	}

	if err := MigrateToSymmetric(); err != nil {
		t.Fatalf("MigrateToSymmetric on fresh install: %v", err)
	}

	newPath := filepath.Join(home, ".wuphf-spaces", "main", ".wuphf")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new path %s: %v", newPath, err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reg.CLICurrent != "main" {
		t.Errorf("CLICurrent: want main, got %q", reg.CLICurrent)
	}
}

func TestMigrateToSymmetricAbortsWhenBrokerRunning(t *testing.T) {
	home := withMigrationHome(t)

	// Override the probe to simulate a running broker.
	orig := brokerRunningFn
	brokerRunningFn = func(port int) bool { return true }
	defer func() { brokerRunningFn = orig }()

	oldWuphf := filepath.Join(home, ".wuphf")
	_ = os.MkdirAll(oldWuphf, 0o700)

	err := MigrateToSymmetric()
	if err == nil {
		t.Fatal("expected error when broker is running")
	}
}

func TestMigrateDoesNotRenameSymlink(t *testing.T) {
	home := withMigrationHome(t)

	oldWuphf := filepath.Join(home, ".wuphf")
	if err := os.MkdirAll(oldWuphf, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := MigrateToSymmetric(); err != nil {
		t.Fatalf("first migration: %v", err)
	}

	info, err := os.Lstat(filepath.Join(home, ".wuphf"))
	if err != nil {
		t.Fatalf("lstat after first migration: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink after first migration")
	}

	// Second migration must be a no-op (registry exists).
	if err := MigrateToSymmetric(); err != nil {
		t.Fatalf("second migration: %v", err)
	}

	// Symlink should still be a symlink.
	info2, err := os.Lstat(filepath.Join(home, ".wuphf"))
	if err != nil {
		t.Fatalf("lstat after second migration: %v", err)
	}
	if info2.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink still present after second migration")
	}
}

func TestIsBrokerRunning(t *testing.T) {
	// Start a real HTTP server to test the positive path.
	port, shutdown := startFakeServer(t)
	defer shutdown()

	if !isBrokerRunning(port) {
		t.Errorf("isBrokerRunning(%d) = false; want true", port)
	}

	// Port 0 should always be unreachable in this form.
	if isBrokerRunning(19997) {
		t.Error("isBrokerRunning(19997) = true on an unbound port; want false")
	}
}
