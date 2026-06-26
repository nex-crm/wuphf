package team

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRepoInit_InsideParentGitRepo pins the fresh-install breaker found in
// the v4 smoke run: a wiki root nested inside a LARGER git repo (runtime
// home under a checkout) made isGitRepoLocked answer true for the parent,
// Init skipped git init, and fsck failed on the missing .git forever.
func TestRepoInit_InsideParentGitRepo(t *testing.T) {
	parent := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main", parent}} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	root := filepath.Join(parent, "home", ".wuphf", "wiki")
	// Pre-create content the way the notebook shelf init does on boot.
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := NewRepoAt(root, filepath.Join(parent, "wiki-backup"))
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("wiki must own its OWN git repo even inside a parent checkout: %v", err)
	}
	if err := repo.Fsck(context.Background()); err != nil {
		t.Fatalf("fsck after init: %v", err)
	}
}
