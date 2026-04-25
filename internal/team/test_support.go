package team

import (
	"os"
	"path/filepath"
	"testing"
)

// stubTaskWorktreePath returns the canonical stub path + branch shape used
// by both the in-package worktree guard (worktree_guard_test.go) and the
// cross-package helper below. Shape mirrors production
// (<root>/.wuphf/task-worktrees/<repoToken>/wuphf-task-<id>) so test
// assertions on the worktree path format stay consistent with real
// behavior. Having two different stub shapes in the past let tests drift
// apart — one stub passing "contains .wuphf/task-worktrees/" while the
// other didn't — so this is the single source of truth.
func stubTaskWorktreePath(taskID string) (string, string) {
	id := sanitizeWorktreeToken(taskID)
	root := defaultTaskWorktreeRootDir("stub")
	return filepath.Join(root, "wuphf-task-"+id), "wuphf-" + id
}

// DisableRealTaskWorktreeForTests replaces the package-level
// prepare/cleanup task worktree funcs with no-op stubs and flips the
// broker-state-load + real-worktree guards so that tests which exercise
// the local_worktree dispatch path (handleTeamTask etc.) cannot reach
// `git worktree add` against the developer's wuphf repo, nor load stale
// state from the user's real ~/.wuphf/.
//
// Intended for TestMain in packages that depend on team and exercise
// this codepath via integration tests. Currently only
// internal/teammcp/testmain_test.go. Grep for `ExecutionMode:
// "local_worktree"` in internal/*/\*_test.go to find additional
// candidates.
//
// Guarded by testing.Testing() so a production caller panics
// immediately instead of silently corrupting the real task dispatcher
// for the lifetime of the process. The in-package tests inside the team
// package get equivalent guards from worktree_guard_test.go's init.
func DisableRealTaskWorktreeForTests() {
	if !testing.Testing() {
		panic("team: DisableRealTaskWorktreeForTests must only be called from tests " +
			"(it mutates package-level worktree dispatch vars with no restore path)")
	}
	allowRealTaskWorktree = false
	skipBrokerStateLoadOnConstruct = true
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		path, branch := stubTaskWorktreePath(taskID)
		return path, branch, nil
	}
	cleanupTaskWorktree = func(string, string) error { return nil }
	// Stub verifyTaskWorktreeWritable too: rejectFalseLocalWorktreeBlock
	// in broker.go calls it with the stub path, which never exists on
	// disk, so the default `os.Stat` check would fail. No tests exercise
	// this path today, but keeping the three worktree vars stubbed
	// together preserves the "real-worktree is off for tests" contract
	// as defense-in-depth for future callers.
	verifyTaskWorktreeWritable = func(string) error { return nil }

	// If the caller's test package hasn't set WUPHF_RUNTIME_HOME, point
	// it at a process-lifetime leaked tempdir so any implicit
	// ~/.wuphf/... write falls through to /tmp instead of the user's
	// real home. Matches worktree_guard_test.go's init for the team
	// package's own tests. Tests that override with t.Setenv take
	// precedence and their restore lands on this safe default.
	if os.Getenv("WUPHF_RUNTIME_HOME") == "" {
		if dir, err := os.MkdirTemp("", "wuphf-disable-real-worktree-home-*"); err == nil {
			_ = os.Setenv("WUPHF_RUNTIME_HOME", dir)
		}
	}
}
