package gitexec_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/gitexec"
)

// TestRunIgnoresInheritedGitDir is the regression test for the class of bug
// PR #277 fixed in commitCountForPath: if a parent process exports GIT_DIR
// (e.g. wuphf invoked from a git hook), every child `git` call would silently
// retarget the outer repo. gitexec.Run must strip that env so cmd.Dir wins.
func TestRunIgnoresInheritedGitDir(t *testing.T) {
	tmpRepo := t.TempDir()
	initCmd := exec.Command("git",
		"-c", "init.defaultBranch=main",
		"-c", "commit.gpgsign=false",
		"init", "-q",
	)
	initCmd.Dir = tmpRepo
	initCmd.Env = gitexec.CleanEnv()
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init tmp repo: %v: %s", err, out)
	}

	// Point GIT_DIR at a bogus path that does NOT exist. If gitexec.Run
	// inherits this, `git rev-parse --show-toplevel` will fail or return
	// the wrong value.
	bogus := filepath.Join(t.TempDir(), "bogus.git")
	t.Setenv("GIT_DIR", bogus)

	ctx := context.Background()
	got, err := gitexec.Run(ctx, tmpRepo, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("gitexec.Run rev-parse: %v", err)
	}

	// On macOS /tmp is a symlink to /private/tmp, so compare resolved paths.
	want, err := filepath.EvalSymlinks(tmpRepo)
	if err != nil {
		t.Fatalf("eval symlinks tmpRepo: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(strings.TrimSpace(got))
	if err != nil {
		t.Fatalf("eval symlinks got: %v", err)
	}
	if gotResolved != want {
		t.Fatalf("rev-parse --show-toplevel = %q, want %q (inherited GIT_DIR=%q leaked)",
			gotResolved, want, bogus)
	}
}

// TestCleanEnvStripsGitConfigFamily verifies the prefix-match strip covers the
// numbered GIT_CONFIG_KEY_<n> / GIT_CONFIG_VALUE_<n> family along with the
// single-key GIT_* vars.
func TestCleanEnvStripsGitConfigFamily(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/bogus")
	t.Setenv("GIT_WORK_TREE", "/tmp/bogus-wt")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.autocrlf=false'")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.autocrlf")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", "/tmp/alt")
	// Canary — must still pass through.
	t.Setenv("GITEXEC_CANARY", "ok")

	env := gitexec.CleanEnv()
	stripped := map[string]bool{
		"GIT_DIR=":                          true,
		"GIT_WORK_TREE=":                    true,
		"GIT_CONFIG_PARAMETERS=":            true,
		"GIT_CONFIG_COUNT=":                 true,
		"GIT_CONFIG_KEY_":                   true,
		"GIT_CONFIG_VALUE_":                 true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=": true,
	}
	for _, kv := range env {
		for prefix := range stripped {
			if strings.HasPrefix(kv, prefix) {
				t.Errorf("CleanEnv leaked %q (prefix %q)", kv, prefix)
			}
		}
	}
	canaryFound := false
	for _, kv := range env {
		if kv == "GITEXEC_CANARY=ok" {
			canaryFound = true
		}
	}
	if !canaryFound {
		t.Errorf("CleanEnv stripped the canary GITEXEC_CANARY var")
	}
	// Sanity: os.Environ still has the original vars.
	if os.Getenv("GIT_DIR") != "/tmp/bogus" {
		t.Errorf("t.Setenv did not take effect for GIT_DIR")
	}
}
