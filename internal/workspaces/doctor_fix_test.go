package workspaces

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- splitFixID / splitOnce ------------------------------------------------

func TestSplitFixID(t *testing.T) {
	tests := []struct {
		in         string
		wantPrefix string
		wantRest   string
	}{
		{"orphan_tree:/tmp/foo", "orphan_tree", "/tmp/foo"},
		{"orphan_tree:delete:/tmp/foo", "orphan_tree", "delete:/tmp/foo"},
		{"zombie:demo (port 7910 unbound)", "zombie", "demo (port 7910 unbound)"},
		{"corrupt:registry", "corrupt", "registry"},
		{"symlink:missing", "symlink", "missing"},
		{"migration:partial", "migration", "partial"},
		{"unknown", "unknown", ""},
		{"", "", ""},
	}
	for _, tc := range tests {
		gp, gr := splitFixID(tc.in)
		if gp != tc.wantPrefix || gr != tc.wantRest {
			t.Errorf("splitFixID(%q) = (%q, %q); want (%q, %q)",
				tc.in, gp, gr, tc.wantPrefix, tc.wantRest)
		}
	}
}

// ---- FixDoctorIssue: unknown / empty ---------------------------------------

func TestFixDoctorIssueRejectsEmpty(t *testing.T) {
	withOrchestratorHome(t)
	err := FixDoctorIssue(context.Background(), "")
	if !errors.Is(err, ErrUnknownFixID) {
		t.Fatalf("want ErrUnknownFixID, got %v", err)
	}
}

func TestFixDoctorIssueRejectsUnknownPrefix(t *testing.T) {
	withOrchestratorHome(t)
	err := FixDoctorIssue(context.Background(), "nonsense:foo")
	if !errors.Is(err, ErrUnknownFixID) {
		t.Fatalf("want ErrUnknownFixID, got %v", err)
	}
}

func TestFixDoctorIssueRejectsUnknownSubPrefix(t *testing.T) {
	withOrchestratorHome(t)

	for _, fixID := range []string{
		"corrupt:other",
		"symlink:weird",
		"migration:other",
	} {
		err := FixDoctorIssue(context.Background(), fixID)
		if !errors.Is(err, ErrUnknownFixID) {
			t.Errorf("FixDoctorIssue(%q): want ErrUnknownFixID, got %v", fixID, err)
		}
	}
}

// ---- orphan_tree (register) ------------------------------------------------

func TestFixOrphanTreeRegisterAddsToRegistry(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	orphan := filepath.Join(sd, "orphan-ws")
	if err := os.MkdirAll(filepath.Join(orphan, ".wuphf"), 0o700); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := FixDoctorIssue(context.Background(), "orphan_tree:"+orphan); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var found *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == "orphan-ws" {
			found = ws
			break
		}
	}
	if found == nil {
		t.Fatal("orphan-ws not added to registry")
	}
	if found.RuntimeHome != orphan {
		t.Errorf("RuntimeHome: want %q, got %q", orphan, found.RuntimeHome)
	}
	if found.State != StateNeverStarted {
		t.Errorf("state: want never_started, got %s", found.State)
	}
	if found.BrokerPort < PortRangeStart || found.BrokerPort > PortRangeEnd {
		t.Errorf("BrokerPort %d out of range", found.BrokerPort)
	}
}

func TestFixOrphanTreeRegisterIsIdempotent(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	orphan := filepath.Join(sd, "orphan-ws")
	if err := os.MkdirAll(filepath.Join(orphan, ".wuphf"), 0o700); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fixID := "orphan_tree:" + orphan
	if err := FixDoctorIssue(context.Background(), fixID); err != nil {
		t.Fatalf("first FixDoctorIssue: %v", err)
	}
	if err := FixDoctorIssue(context.Background(), fixID); err != nil {
		t.Fatalf("second FixDoctorIssue: %v", err)
	}

	reg, _ := Read()
	count := 0
	for _, ws := range reg.Workspaces {
		if ws.Name == "orphan-ws" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want 1 orphan-ws entry, got %d", count)
	}
}

func TestFixOrphanTreeRegisterRefusesPathOutsideSpacesDir(t *testing.T) {
	withOrchestratorHome(t)
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	outside := t.TempDir() // outside ~/.wuphf-spaces
	err := FixDoctorIssue(context.Background(), "orphan_tree:"+outside)
	if err == nil {
		t.Fatal("expected error for path outside spaces dir")
	}
}

func TestFixOrphanTreeRegisterTreatsMissingPathAsIdempotent(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Path doesn't exist — should not error (idempotent).
	missing := filepath.Join(sd, "already-gone")
	if err := FixDoctorIssue(context.Background(), "orphan_tree:"+missing); err != nil {
		t.Errorf("expected nil for missing path, got %v", err)
	}
}

// ---- orphan_tree:delete ----------------------------------------------------

func TestFixOrphanTreeDeleteMovesToBackups(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	orphan := filepath.Join(sd, "orphan-doomed")
	if err := os.MkdirAll(filepath.Join(orphan, ".wuphf"), 0o700); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := FixDoctorIssue(context.Background(), "orphan_tree:delete:"+orphan); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan should have been moved out of original location")
	}
	backupDir := filepath.Join(sd, backupsDirName)
	entries, _ := os.ReadDir(backupDir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "orphan-doomed-") {
			found = true
		}
	}
	if !found {
		t.Error("orphan should have been moved under .backups/ with a timestamped suffix")
	}
}

func TestFixOrphanTreeDeleteIsIdempotent(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	missing := filepath.Join(sd, "ghost")
	if err := FixDoctorIssue(context.Background(), "orphan_tree:delete:"+missing); err != nil {
		t.Errorf("missing path should be no-op, got %v", err)
	}
}

// ---- zombie ----------------------------------------------------------------

func TestFixZombieReconcilesToPaused(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "zombie-ws", RuntimeHome: "/tmp/zombie",
				BrokerPort: 29980, WebPort: 29981,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Lane B's Doctor builds FixID with a parenthetical detail; FixDoctorIssue
	// must extract just the workspace name.
	fixID := "zombie:zombie-ws (port 29980 unbound)"
	if err := FixDoctorIssue(context.Background(), fixID); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "zombie-ws" && ws.State != StatePaused {
			t.Errorf("state: want paused, got %s", ws.State)
		}
	}
}

func TestFixZombieIsIdempotent(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "zws", RuntimeHome: "/tmp/zws",
				BrokerPort: 29982, WebPort: 29983,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := FixDoctorIssue(context.Background(), "zombie:zws"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

func TestFixZombieErrorsOnUnknownWorkspace(t *testing.T) {
	withOrchestratorHome(t)
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := FixDoctorIssue(context.Background(), "zombie:ghost")
	if err == nil || !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("want ErrWorkspaceNotFound, got %v", err)
	}
}

// ---- port ------------------------------------------------------------------

func TestFixPortConflictReturnsManualFix(t *testing.T) {
	withOrchestratorHome(t)
	err := FixDoctorIssue(context.Background(), "port:7890")
	if !errors.Is(err, ErrManualFixRequired) {
		t.Fatalf("want ErrManualFixRequired, got %v", err)
	}
}

// ---- corrupt:registry ------------------------------------------------------

func TestFixCorruptRegistryRestoresFromBak(t *testing.T) {
	withOrchestratorHome(t)

	// Seed a "good" backup.
	now := time.Now().UTC()
	good := &Registry{
		Version:    Version,
		CLICurrent: "good",
		Workspaces: []*Workspace{
			{Name: "good", RuntimeHome: "/tmp/good",
				BrokerPort: 7910, WebPort: 7911,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}
	if err := Write(good); err != nil {
		t.Fatalf("seed good: %v", err)
	}
	// Force a second write so .bak now contains "good".
	bad := &Registry{
		Version:    Version,
		CLICurrent: "bad",
		Workspaces: []*Workspace{
			{Name: "bad", RuntimeHome: "/tmp/bad",
				BrokerPort: 7912, WebPort: 7913,
				State: StateError, CreatedAt: now, LastUsedAt: now},
		},
	}
	if err := Write(bad); err != nil {
		t.Fatalf("seed bad: %v", err)
	}

	// Sanity: .bak now holds the prior content (with cli_current=good).
	rp, _ := registryPath()
	bakReg, err := readFile(rp + ".bak")
	if err != nil {
		t.Fatalf("read bak: %v", err)
	}
	if bakReg.CLICurrent != "good" {
		t.Fatalf("bak.cli_current: want good, got %s", bakReg.CLICurrent)
	}

	if err := FixDoctorIssue(context.Background(), "corrupt:registry"); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}

	got, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.CLICurrent != "good" {
		t.Errorf("after restore: cli_current want good, got %s", got.CLICurrent)
	}
}

func TestFixCorruptRegistryRequiresManualFixWhenNoBak(t *testing.T) {
	withOrchestratorHome(t)
	// No registry written → no .bak.
	err := FixDoctorIssue(context.Background(), "corrupt:registry")
	if !errors.Is(err, ErrManualFixRequired) {
		t.Fatalf("want ErrManualFixRequired, got %v", err)
	}
}

func TestFixCorruptRegistryIsIdempotent(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: "/tmp/main",
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed1: %v", err)
	}
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: "/tmp/main",
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed2: %v", err)
	}

	if err := FixDoctorIssue(context.Background(), "corrupt:registry"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := FixDoctorIssue(context.Background(), "corrupt:registry"); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

// ---- symlink:missing -------------------------------------------------------

func TestFixSymlinkMissingCreatesSymlink(t *testing.T) {
	home := withOrchestratorHome(t)

	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	symlinkPath := filepath.Join(home, ".wuphf")
	_ = os.Remove(symlinkPath)

	if err := FixDoctorIssue(context.Background(), "symlink:missing"); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}

	info, err := os.Lstat(symlinkPath)
	if err != nil {
		t.Fatalf("Lstat symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink, got regular path")
	}
	tgt, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if filepath.Clean(tgt) != filepath.Clean(mainTarget) {
		t.Errorf("symlink target: want %s, got %s", mainTarget, tgt)
	}
}

func TestFixSymlinkMissingIsIdempotent(t *testing.T) {
	home := withOrchestratorHome(t)
	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	_ = os.Remove(filepath.Join(home, ".wuphf"))

	for i := 0; i < 2; i++ {
		if err := FixDoctorIssue(context.Background(), "symlink:missing"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

func TestFixSymlinkMissingErrorsWhenTargetAbsent(t *testing.T) {
	home := withOrchestratorHome(t)
	_ = os.Remove(filepath.Join(home, ".wuphf"))

	err := FixDoctorIssue(context.Background(), "symlink:missing")
	if err == nil {
		t.Fatal("expected error when target tree missing")
	}
}

// ---- symlink:wrong ---------------------------------------------------------

func TestFixSymlinkWrongReplacesSymlink(t *testing.T) {
	home := withOrchestratorHome(t)
	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	wrongTarget := filepath.Join(home, "wrong-target")
	if err := os.MkdirAll(wrongTarget, 0o700); err != nil {
		t.Fatalf("mkdir wrong: %v", err)
	}
	symlinkPath := filepath.Join(home, ".wuphf")
	if err := os.Symlink(wrongTarget, symlinkPath); err != nil {
		t.Fatalf("seed wrong symlink: %v", err)
	}

	if err := FixDoctorIssue(context.Background(), "symlink:wrong"); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}

	tgt, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if filepath.Clean(tgt) != filepath.Clean(mainTarget) {
		t.Errorf("symlink target after fix: want %s, got %s", mainTarget, tgt)
	}
}

func TestFixSymlinkWrongIsIdempotentOnCorrectLink(t *testing.T) {
	home := withOrchestratorHome(t)
	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	symlinkPath := filepath.Join(home, ".wuphf")
	if err := os.Symlink(mainTarget, symlinkPath); err != nil {
		t.Fatalf("seed correct symlink: %v", err)
	}

	if err := FixDoctorIssue(context.Background(), "symlink:wrong"); err != nil {
		t.Fatalf("FixDoctorIssue: %v", err)
	}
	if err := FixDoctorIssue(context.Background(), "symlink:wrong"); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestFixSymlinkWrongRefusesRegularDirectory(t *testing.T) {
	home := withOrchestratorHome(t)
	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	// Place a regular directory at ~/.wuphf — partial migration shape.
	if err := os.MkdirAll(filepath.Join(home, ".wuphf"), 0o700); err != nil {
		t.Fatalf("mkdir regular: %v", err)
	}

	err := FixDoctorIssue(context.Background(), "symlink:wrong")
	if !errors.Is(err, ErrManualFixRequired) {
		t.Errorf("want ErrManualFixRequired for regular dir at ~/.wuphf, got %v", err)
	}
}

// ---- migration:partial -----------------------------------------------------

func TestFixMigrationPartialReturnsManualFix(t *testing.T) {
	withOrchestratorHome(t)
	err := FixDoctorIssue(context.Background(), "migration:partial")
	if !errors.Is(err, ErrManualFixRequired) {
		t.Fatalf("want ErrManualFixRequired, got %v", err)
	}
}
