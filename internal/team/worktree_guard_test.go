package team

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The guard flips + stub installs are intentionally in a *_test.go init()
// so the `testing` import (and the stub functions) stay out of the
// production build. Production defaults in worktree.go remain:
//   - allowRealTaskWorktree = true
//   - unscopedWikiRootAllowed = true
//   - prepareTaskWorktree = defaultPrepareTaskWorktree
//   - cleanupTaskWorktree = defaultCleanupTaskWorktree
//
// Under `go test`, init() below replaces all four with safe defaults:
//   - The two guards are disabled so any test that reaches the real
//     codepath without opting in panics / errors loudly.
//   - The two vars are stubbed so indirect callers (e.g. EnsureTask →
//     syncTaskWorktreeLocked → prepareTaskWorktree on a coding-agent
//     task) get a deterministic fake path + branch instead of
//     registering a worktree against the developer's wuphf repo.
//
// Tests that legitimately need the real prepare/cleanup codepath (the
// three cases in worktree_test.go that build a tempdir-scoped repo and
// chdir into it) must opt in via allowRealTaskWorktreeForTest(t) AND
// call defaultPrepareTaskWorktree directly. Tests that want custom
// stub behavior continue to monkey-patch prepareTaskWorktree; their
// defer-restore lands on the stub below, not the real codepath.
func init() {
	allowRealTaskWorktree = false
	unscopedWikiRootAllowed = false
	prepareTaskWorktree = stubPrepareTaskWorktree
	cleanupTaskWorktree = stubCleanupTaskWorktree
	skipBrokerStateLoadOnConstruct = true

	// Pin WUPHF_RUNTIME_HOME and the default brokerStatePath into
	// process-lifetime leaked tempdirs so any test that constructs a
	// Broker without its own isolation setup falls back to /tmp instead
	// of the developer's real ~/.wuphf. Tests that override either via
	// t.Setenv / brokerStatePath = ... take precedence locally; their
	// deferred restore lands on these safe defaults. Leaked (not
	// t.TempDir) so late writes from goroutines a test failed to stop
	// don't race on a directory being deleted.
	runtimeHome, err := os.MkdirTemp("", "wuphf-test-runtime-home-*")
	if err != nil {
		panic(fmt.Sprintf("worktree_guard_test init: mktemp runtime home: %v", err))
	}
	if err := os.Setenv("WUPHF_RUNTIME_HOME", runtimeHome); err != nil {
		panic(fmt.Sprintf("worktree_guard_test init: setenv WUPHF_RUNTIME_HOME: %v", err))
	}

	stateDir, err := os.MkdirTemp("", "wuphf-test-broker-state-*")
	if err != nil {
		panic(fmt.Sprintf("worktree_guard_test init: mktemp broker state: %v", err))
	}
	defaultTestStatePath := filepath.Join(stateDir, "broker-state.json")
	defaultFn := func() string { return defaultTestStatePath }
	brokerStatePathOverride.Store(&defaultFn)
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
// defaultPrepareTaskWorktree / defaultCleanupTaskWorktree codepath. It
// mutates three package-level globals (allowRealTaskWorktree,
// prepareTaskWorktree, cleanupTaskWorktree) without synchronization and
// restores them via t.Cleanup. Call sites MUST NOT call t.Parallel() in
// the same test, and the test MUST NOT spawn background brokers/workers
// that read those function pointers concurrently — both conditions hold
// for the three current callers in worktree_test.go (no t.Parallel, no
// Broker goroutines). If a future caller needs either, convert this to
// a mutex-guarded swap like setHeadlessWakeLeadFn in broker_test.go.
func allowRealTaskWorktreeForTest(t *testing.T) {
	t.Helper()
	prevAllow := allowRealTaskWorktree
	prevPrepare := prepareTaskWorktree
	prevCleanup := cleanupTaskWorktree
	allowRealTaskWorktree = true
	prepareTaskWorktree = defaultPrepareTaskWorktree
	cleanupTaskWorktree = defaultCleanupTaskWorktree
	t.Cleanup(func() {
		allowRealTaskWorktree = prevAllow
		prepareTaskWorktree = prevPrepare
		cleanupTaskWorktree = prevCleanup
	})
}
