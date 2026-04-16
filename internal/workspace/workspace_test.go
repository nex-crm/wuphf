package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// seedWorkspace builds a realistic ~/.wuphf tree under dir and returns the
// map of human-readable labels to absolute paths so tests can assert on
// specific entries without recomputing paths.
func seedWorkspace(t *testing.T, dir string) map[string]string {
	t.Helper()
	base := filepath.Join(dir, ".wuphf")
	paths := map[string]string{
		"onboarded":    filepath.Join(base, "onboarded.json"),
		"company":      filepath.Join(base, "company.json"),
		"brokerState":  filepath.Join(base, "team", "broker-state.json"),
		"officePID":    filepath.Join(base, "team", "office.pid"),
		"officeTasks":  filepath.Join(base, "office", "tasks", "t-1.json"),
		"workflow":     filepath.Join(base, "workflows", "wf-1.json"),
		"logs":         filepath.Join(base, "logs", "channel-stderr.log"),
		"session":      filepath.Join(base, "sessions", "s-1.json"),
		"worktree":     filepath.Join(base, "task-worktrees", "wt-1", "file"),
		"codex":        filepath.Join(base, "codex-headless", "cache"),
		"providers":    filepath.Join(base, "providers", "claude-sessions.json"),
		"openclaw":     filepath.Join(base, "openclaw", "identity.json"),
		"config":       filepath.Join(base, "config.json"),
		"calendar":     filepath.Join(base, "calendar.json"),
	}
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return paths
}

// withRuntimeHome isolates Shred/ClearRuntime from the real home directory by
// pointing WUPHF_RUNTIME_HOME at a t.TempDir().
func withRuntimeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	return dir
}

func assertGone(t *testing.T, label, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s (%s) removed, got err=%v", label, path, err)
	}
}

func assertStays(t *testing.T, label, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s (%s) preserved, got err=%v", label, path, err)
	}
}

func TestClearRuntimeRemovesBrokerStateOnly(t *testing.T) {
	dir := withRuntimeHome(t)
	paths := seedWorkspace(t, dir)

	res, err := ClearRuntime()
	if err != nil {
		t.Fatalf("ClearRuntime: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	assertGone(t, "brokerState", paths["brokerState"])
	assertGone(t, "officePID", paths["officePID"])

	// Everything else survives a narrow reset.
	for _, label := range []string{
		"onboarded", "company", "officeTasks", "workflow",
		"logs", "session", "worktree", "codex", "providers",
		"openclaw", "config", "calendar",
	} {
		assertStays(t, label, paths[label])
	}
}

func TestShredRemovesWorkspaceButPreservesHistory(t *testing.T) {
	dir := withRuntimeHome(t)
	paths := seedWorkspace(t, dir)

	res, err := Shred()
	if err != nil {
		t.Fatalf("Shred: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	// Wiped by shred.
	for _, label := range []string{
		"onboarded", "company", "brokerState", "officePID",
		"officeTasks", "workflow",
	} {
		assertGone(t, label, paths[label])
	}

	// Preserved: history, in-flight work, auth caches, user prefs.
	for _, label := range []string{
		"logs", "session", "worktree", "codex", "providers",
		"openclaw", "config", "calendar",
	} {
		assertStays(t, label, paths[label])
	}
}

func TestShredIsIdempotent(t *testing.T) {
	withRuntimeHome(t)
	// No seed — directory is empty. Shred must not error on missing paths.
	res, err := Shred()
	if err != nil {
		t.Fatalf("first Shred on empty home: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("expected no removals on empty home, got %v", res.Removed)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	// Second call is still fine.
	if _, err := Shred(); err != nil {
		t.Fatalf("second Shred: %v", err)
	}
}

func TestClearRuntimeWithNoTeamDirIsNoOp(t *testing.T) {
	withRuntimeHome(t)
	res, err := ClearRuntime()
	if err != nil {
		t.Fatalf("ClearRuntime: %v", err)
	}
	if len(res.Removed) != 0 || len(res.Errors) != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
}
