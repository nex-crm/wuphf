// Package gitexec runs `git` subprocesses with a sanitized environment so
// wuphf never silently retargets the outer repository when invoked from
// inside a git hook or a nested git operation. Every call-site that needs
// to shell out to git MUST go through the helpers in this package (Run,
// RunOK) or, at a minimum, build cmd.Env from CleanEnv(). Do not use
// exec.Command("git", ...) directly — inheriting GIT_DIR / GIT_WORK_TREE /
// GIT_CONFIG_PARAMETERS from the parent process is how we produced the
// runaway "wuphf: init wiki" commits clobbering real branches, and is the
// class of bug PR #277 discovered in commitCountForPath. Centralizing the
// env strip here makes the guarantee a package-level invariant instead of
// a convention callers can forget.
package gitexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CleanEnv returns os.Environ() minus the GIT_* variables that pin git to a
// specific repo, index, or worktree; the GIT_CONFIG_* family that can inject
// `-c` overrides (including GIT_CONFIG_GLOBAL/_SYSTEM which redirect config
// discovery); GIT_ALTERNATE_OBJECT_DIRECTORIES which lets the subprocess
// resolve objects from the outer repo; and GIT_ATTR_SOURCE which overrides
// attributes lookup. Callers set cmd.Dir explicitly, so inheriting GIT_DIR
// from a parent (e.g. when wuphf runs inside a `git push` hook or a nested
// git operation) would silently redirect every subprocess to the outer repo —
// that's what produced the runaway "wuphf: init wiki" commits clobbering
// real branches.
//
// Callers that legitimately want to pin config to /dev/null (e.g. the wiki
// path in wiki_git.go) should append GIT_CONFIG_GLOBAL=/dev/null AFTER this
// — os/exec dedupes env keys last-wins, so stripping here + appending the
// override is the clean pattern and does not rely on unspecified ordering.
func CleanEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "GIT_DIR="),
			strings.HasPrefix(kv, "GIT_WORK_TREE="),
			strings.HasPrefix(kv, "GIT_INDEX_FILE="),
			strings.HasPrefix(kv, "GIT_OBJECT_DIRECTORY="),
			strings.HasPrefix(kv, "GIT_ALTERNATE_OBJECT_DIRECTORIES="),
			strings.HasPrefix(kv, "GIT_COMMON_DIR="),
			strings.HasPrefix(kv, "GIT_NAMESPACE="),
			strings.HasPrefix(kv, "GIT_ATTR_SOURCE="),
			strings.HasPrefix(kv, "GIT_CONFIG="),
			strings.HasPrefix(kv, "GIT_CONFIG_GLOBAL="),
			strings.HasPrefix(kv, "GIT_CONFIG_SYSTEM="),
			strings.HasPrefix(kv, "GIT_CONFIG_COUNT="),
			strings.HasPrefix(kv, "GIT_CONFIG_KEY_"),
			strings.HasPrefix(kv, "GIT_CONFIG_VALUE_"),
			strings.HasPrefix(kv, "GIT_CONFIG_PARAMETERS="):
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

// Run executes `git <args...>` in dir with a cleaned environment and returns
// the trimmed stdout. On failure the returned error wraps the git exit code
// plus the captured stderr.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = CleanEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunOK executes `git <args...>` in dir with a cleaned environment and
// discards stdout. Use when you only care whether the command succeeded.
func RunOK(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = CleanEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
