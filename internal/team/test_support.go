package team

import (
	"os"
	"path/filepath"
)

// DisableRealTaskWorktreeForTests replaces the package-level
// prepare/cleanup task worktree funcs with no-op stubs that return a
// deterministic fake path+branch. It also flips allowRealTaskWorktree to
// false so any stray defaultPrepareTaskWorktree caller errors loudly.
//
// Intended for TestMain in packages that depend on team and exercise the
// local_worktree dispatch path via integration tests (e.g.
// internal/teammcp's handleTeamTask). Without this, those tests reach
// `git worktree add` against the developer's wuphf repo and leave stale
// `wuphf-<hash>-task-N` branches plus interleaving HEAD ref-lock
// failures on the invoking worktree's `git push`.
//
// Production MUST NOT call this — the team package's own *_test.go init
// uses the same stubs for in-package tests. See worktree_guard_test.go.
func DisableRealTaskWorktreeForTests() {
	allowRealTaskWorktree = false
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		id := sanitizeWorktreeToken(taskID)
		// Mirror the production path shape (.wuphf/task-worktrees/...) so
		// callers that assert on the worktree path format (e.g. the
		// runtime-state snapshot tests) see a realistic value.
		path := filepath.Join(os.TempDir(), ".wuphf", "task-worktrees", "wuphf-task-stub-"+id)
		return path, "wuphf-" + id, nil
	}
	cleanupTaskWorktree = func(string, string) error { return nil }
}
