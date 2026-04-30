package workspaces

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// withSpacesDir sets HOME (and WUPHF_RUNTIME_HOME for any callers that hit
// other code paths) to a temp dir so registry operations land in an
// isolated location. spacesDir uses os.UserHomeDir directly because
// ~/.wuphf-spaces is shared cross-workspace and lives at the real user
// HOME, not under any single workspace's WUPHF_RUNTIME_HOME.
func withSpacesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	return filepath.Join(dir, ".wuphf-spaces")
}

func sampleRegistry() *Registry {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	return &Registry{
		Version:    1,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{
				Name:        "main",
				RuntimeHome: "/tmp/spaces/main",
				BrokerPort:  7890,
				WebPort:     7891,
				State:       StateRunning,
				Blueprint:   "founding-team",
				CompanyName: "Nex",
				CreatedAt:   now,
				LastUsedAt:  now,
			},
			{
				Name:        "demo",
				RuntimeHome: "/tmp/spaces/demo",
				BrokerPort:  7910,
				WebPort:     7911,
				State:       StatePaused,
				Blueprint:   "founding-team",
				CompanyName: "Acme",
				CreatedAt:   now,
				LastUsedAt:  now,
				PausedAt:    now,
			},
		},
	}
}

func TestReadWriteHappyPath(t *testing.T) {
	withSpacesDir(t)

	reg := sampleRegistry()
	if err := Write(reg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Version != reg.Version {
		t.Errorf("Version: want %d, got %d", reg.Version, got.Version)
	}
	if got.CLICurrent != reg.CLICurrent {
		t.Errorf("CLICurrent: want %q, got %q", reg.CLICurrent, got.CLICurrent)
	}
	if len(got.Workspaces) != len(reg.Workspaces) {
		t.Errorf("Workspaces len: want %d, got %d", len(reg.Workspaces), len(got.Workspaces))
	}
}

func TestReadFallsBackToBakOnCorruption(t *testing.T) {
	dir := withSpacesDir(t)

	// Write a valid registry so a .bak is created.
	reg := sampleRegistry()
	if err := Write(reg); err != nil {
		t.Fatalf("initial Write: %v", err)
	}
	// Write again to rotate the previous file to .bak.
	if err := Write(reg); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	// Corrupt the primary file.
	rp := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(rp, []byte("{not json}"), 0o600); err != nil {
		t.Fatalf("corrupt write: %v", err)
	}

	got, err := Read()
	if err != nil {
		t.Fatalf("Read after corruption: %v", err)
	}
	if got.CLICurrent != reg.CLICurrent {
		t.Errorf("expected bak fallback data, got CLICurrent=%q", got.CLICurrent)
	}
}

func TestReadReturnsErrNotFoundWhenNoRegistry(t *testing.T) {
	withSpacesDir(t)

	_, err := Read()
	if err != ErrRegistryNotFound {
		t.Errorf("want ErrRegistryNotFound, got %v", err)
	}
}

func TestConcurrentWritersSerialize(t *testing.T) {
	withSpacesDir(t)

	// Seed an initial registry so concurrent writers have something to read.
	seed := sampleRegistry()
	if err := Write(seed); err != nil {
		t.Fatalf("seed Write: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = Update("main", func(ws *Workspace) error {
				ws.CompanyName = "Writer" + string(rune('A'+idx))
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	// Registry must be well-formed after concurrent updates.
	final, err := Read()
	if err != nil {
		t.Fatalf("Read after concurrent writers: %v", err)
	}
	if len(final.Workspaces) != 2 {
		t.Errorf("Workspaces count: want 2, got %d", len(final.Workspaces))
	}
}

func TestUpdatePreservesOtherEntries(t *testing.T) {
	withSpacesDir(t)

	reg := sampleRegistry()
	if err := Write(reg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := Update("main", func(ws *Workspace) error {
		ws.CompanyName = "Updated Nex"
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	var main, demo *Workspace
	for _, ws := range got.Workspaces {
		switch ws.Name {
		case "main":
			main = ws
		case "demo":
			demo = ws
		}
	}
	if main == nil {
		t.Fatal("main workspace missing after Update")
	}
	if main.CompanyName != "Updated Nex" {
		t.Errorf("main.CompanyName: want %q, got %q", "Updated Nex", main.CompanyName)
	}
	if demo == nil {
		t.Fatal("demo workspace missing after Update")
	}
	if demo.CompanyName != "Acme" {
		t.Errorf("demo.CompanyName should be unchanged, got %q", demo.CompanyName)
	}
}

func TestUpdateReturnsErrWorkspaceNotFound(t *testing.T) {
	withSpacesDir(t)

	if err := Write(sampleRegistry()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := Update("nonexistent", func(ws *Workspace) error { return nil })
	if err != ErrWorkspaceNotFound {
		t.Errorf("want ErrWorkspaceNotFound, got %v", err)
	}
}

func TestWriteRotatesBak(t *testing.T) {
	dir := withSpacesDir(t)

	reg := sampleRegistry()
	if err := Write(reg); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	// Modify and write again — should produce .bak of the first write.
	reg.CLICurrent = "demo"
	if err := Write(reg); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	bak := filepath.Join(dir, "registry.json.bak")
	data, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	var bakReg Registry
	if err := json.Unmarshal(data, &bakReg); err != nil {
		t.Fatalf("parse .bak: %v", err)
	}
	if bakReg.CLICurrent != "main" {
		t.Errorf("bak should contain original cli_current %q, got %q", "main", bakReg.CLICurrent)
	}
}

func TestBothFilesAbsentNoBak(t *testing.T) {
	withSpacesDir(t)
	// Neither registry.json nor .bak exist.
	_, err := Read()
	if err != ErrRegistryNotFound {
		t.Errorf("want ErrRegistryNotFound, got %v", err)
	}
}

func TestFileModes(t *testing.T) {
	dir := withSpacesDir(t)

	if err := Write(sampleRegistry()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rp := filepath.Join(dir, "registry.json")
	info, err := os.Stat(rp)
	if err != nil {
		t.Fatalf("stat registry.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("registry.json perm: want 0600, got %o", perm)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat spaces dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("spaces dir perm: want 0700, got %o", perm)
	}
}
