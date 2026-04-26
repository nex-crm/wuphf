package team

import (
	"fmt"
	"os"
	"testing"
)

// The guard flips + stub installs are intentionally in a *_test.go init()
// so the `testing` import (and the stub functions) stay out of the
// production build. Production defaults in worktree.go remain:
//   - allowRealTaskWorktree = true
//   - unscopedWikiRootAllowed = true
//   - prepareTaskWorktreeOverride = nil  (falls through to defaultPrepareTaskWorktree)
//   - cleanupTaskWorktreeOverride = nil  (falls through to defaultCleanupTaskWorktree)
//
// Under `go test`, init() below replaces all four with safe defaults:
//   - The two guards are disabled so any test that reaches the real
//     codepath without opting in panics / errors loudly.
//   - The two override pointers are populated with stubs so indirect
//     callers (e.g. EnsureTask → syncTaskWorktreeLocked →
//     prepareTaskWorktree on a coding-agent task) get a deterministic
//     fake path + branch instead of registering a worktree against the
//     developer's wuphf repo.
//
// Tests that legitimately need the real prepare/cleanup codepath (the
// three cases in worktree_test.go that build a tempdir-scoped repo and
// chdir into it) must opt in via allowRealTaskWorktreeForTest(t) AND
// call defaultPrepareTaskWorktree directly. Tests that want custom
// stub behavior call setPrepareTaskWorktreeForTest(t, fn); the
// per-test t.Cleanup restores the package-init stub.
func init() {
	allowRealTaskWorktree.Store(false)
	unscopedWikiRootAllowed = false
	prep := prepareTaskWorktreeFn(stubPrepareTaskWorktree)
	prepareTaskWorktreeOverride.Store(&prep)
	cleanup := cleanupTaskWorktreeFn(stubCleanupTaskWorktree)
	cleanupTaskWorktreeOverride.Store(&cleanup)
	skipBrokerStateLoadOnConstruct = true

	// Pin WUPHF_RUNTIME_HOME into a process-lifetime leaked tempdir so
	// any test that constructs a Broker without its own isolation setup
	// falls back to /tmp instead of the developer's real ~/.wuphf.
	// defaultBrokerStatePath() consults this env var, so broker state
	// files created by unisolated tests land under a leaked temp dir.
	// Leaked (not t.TempDir) so late writes from goroutines a test
	// failed to stop don't race on a directory being deleted.
	runtimeHome, err := os.MkdirTemp("", "wuphf-test-runtime-home-*")
	if err != nil {
		panic(fmt.Sprintf("worktree_guard_test init: mktemp runtime home: %v", err))
	}
	if err := os.Setenv("WUPHF_RUNTIME_HOME", runtimeHome); err != nil {
		panic(fmt.Sprintf("worktree_guard_test init: setenv WUPHF_RUNTIME_HOME: %v", err))
	}
}

func stubPrepareTaskWorktree(taskID string) (string, string, error) {
	// Share stubTaskWorktreePath with DisableRealTaskWorktreeForTests so
	// both stubs emit the same `<root>/.wuphf/task-worktrees/<repoToken>/wuphf-task-<id>`
	// shape — downstream assertions on the path format stay consistent.
	path, branch := stubTaskWorktreePath(taskID)
	return path, branch, nil
}

func stubCleanupTaskWorktree(string, string) error { return nil }

// allowRealTaskWorktreeForTest opts the current test into the real
// defaultPrepareTaskWorktree / defaultCleanupTaskWorktree codepath.
//
// Each individual Store/Load on the underlying atomic primitives is
// race-safe, but the THREE stores together (allow + prepare override +
// cleanup override) are not coherent as a tuple. A concurrent caller
// observing the post-flip state may see allow=true while the override
// pointers are still mid-update or vice versa. The current callers in
// worktree_test.go avoid this by:
//   - never running t.Parallel() in the opt-in test, AND
//   - chdiring into a tempdir-scoped repo (so even if they did parallel,
//     each would be in its own working directory).
//
// Future callers who need t.Parallel() must serialize the opt-in
// (e.g. wrap the triple in a sync.Mutex) — atomic alone won't help.
func allowRealTaskWorktreeForTest(t *testing.T) {
	t.Helper()
	prevAllow := allowRealTaskWorktree.Load()
	prevPrepare := prepareTaskWorktreeOverride.Load()
	prevCleanup := cleanupTaskWorktreeOverride.Load()
	allowRealTaskWorktree.Store(true)
	prepareTaskWorktreeOverride.Store(nil)
	cleanupTaskWorktreeOverride.Store(nil)
	t.Cleanup(func() {
		allowRealTaskWorktree.Store(prevAllow)
		prepareTaskWorktreeOverride.Store(prevPrepare)
		cleanupTaskWorktreeOverride.Store(prevCleanup)
	})
}
