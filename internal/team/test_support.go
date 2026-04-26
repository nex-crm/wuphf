package team

import (
	"context"
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
	allowRealTaskWorktree.Store(false)
	skipBrokerStateLoadOnConstruct = true
	prep := prepareTaskWorktreeFn(func(taskID string) (string, string, error) {
		path, branch := stubTaskWorktreePath(taskID)
		return path, branch, nil
	})
	prepareTaskWorktreeOverride.Store(&prep)
	cleanup := cleanupTaskWorktreeFn(func(string, string) error { return nil })
	cleanupTaskWorktreeOverride.Store(&cleanup)
	// Stub verifyTaskWorktreeWritable too: rejectFalseLocalWorktreeBlock
	// in broker.go calls it with the stub path, which never exists on
	// disk, so the default `os.Stat` check would fail. No tests exercise
	// this path today, but keeping the three worktree vars stubbed
	// together preserves the "real-worktree is off for tests" contract
	// as defense-in-depth for future callers.
	verify := verifyTaskWorktreeWritableFn(func(string) error { return nil })
	verifyTaskWorktreeWritableOverride.Store(&verify)

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

// setHeadlessCodexRunTurnForTest redirects headlessCodexRunTurn(...) to fn
// for the duration of the test, then restores the prior override on cleanup.
//
// Tests previously did `oldFn := headlessCodexRunTurn; headlessCodexRunTurn = ...`
// against a package-level var. That pattern was a data race against the
// queue worker spawned by enqueueHeadlessCodexTurnRecord — the worker could
// outlive the test's deferred restore and observe the swap mid-call. Use
// this helper instead.
//
// CONSTRAINT: tests using any setXForTest helper in this file must NOT call
// t.Parallel(). The setters do an atomic Load → atomic Store pair which is
// non-atomic AS A PAIR; parallel setters can scramble the cleanup chain (T1
// captures prior=A, T2 captures prior=A, T1 stores B, T2 stores C, T1
// cleanup stores A, T2 cleanup stores A — both lose). Single-test
// race-safety against background goroutines is the goal here, not parallel
// test composition.
func setHeadlessCodexRunTurnForTest(t *testing.T, fn func(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error) {
	t.Helper()
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&fn)
	t.Cleanup(func() {
		headlessCodexRunTurnOverride.Store(prior)
	})
}

// setPrepareTaskWorktreeForTest swaps the prepareTaskWorktree dispatcher
// for the duration of the test. Same race motivation as
// setHeadlessCodexRunTurnForTest: the headless queue worker can read the
// dispatcher after the test's deferred restore has already run.
func setPrepareTaskWorktreeForTest(t *testing.T, fn prepareTaskWorktreeFn) {
	t.Helper()
	prior := prepareTaskWorktreeOverride.Load()
	prepareTaskWorktreeOverride.Store(&fn)
	t.Cleanup(func() {
		prepareTaskWorktreeOverride.Store(prior)
	})
}

// setCleanupTaskWorktreeForTest swaps the cleanupTaskWorktree dispatcher
// for the duration of the test.
func setCleanupTaskWorktreeForTest(t *testing.T, fn cleanupTaskWorktreeFn) {
	t.Helper()
	prior := cleanupTaskWorktreeOverride.Load()
	cleanupTaskWorktreeOverride.Store(&fn)
	t.Cleanup(func() {
		cleanupTaskWorktreeOverride.Store(prior)
	})
}

// setTaskWorktreeRootDirForTest swaps the taskWorktreeRootDir dispatcher
// for the duration of the test.
func setTaskWorktreeRootDirForTest(t *testing.T, fn taskWorktreeRootDirFn) {
	t.Helper()
	prior := taskWorktreeRootDirOverride.Load()
	taskWorktreeRootDirOverride.Store(&fn)
	t.Cleanup(func() {
		taskWorktreeRootDirOverride.Store(prior)
	})
}

// setVerifyTaskWorktreeWritableForTest swaps the verifyTaskWorktreeWritable
// dispatcher for the duration of the test.
func setVerifyTaskWorktreeWritableForTest(t *testing.T, fn verifyTaskWorktreeWritableFn) {
	t.Helper()
	prior := verifyTaskWorktreeWritableOverride.Load()
	verifyTaskWorktreeWritableOverride.Store(&fn)
	t.Cleanup(func() {
		verifyTaskWorktreeWritableOverride.Store(prior)
	})
}

// setHeadlessCodexWorkspaceStatusSnapshotForTest swaps the snapshot function
// for the duration of the test. Same race motivation as
// setPrepareTaskWorktreeForTest: the snapshot is read by the headless queue
// worker on a goroutine that can outlive the test's t.Cleanup.
func setHeadlessCodexWorkspaceStatusSnapshotForTest(t *testing.T, fn headlessCodexWorkspaceStatusSnapshotFn) {
	t.Helper()
	prior := headlessCodexWorkspaceStatusSnapshotOverride.Load()
	headlessCodexWorkspaceStatusSnapshotOverride.Store(&fn)
	t.Cleanup(func() {
		headlessCodexWorkspaceStatusSnapshotOverride.Store(prior)
	})
}
