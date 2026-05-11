package team

// broker_no_self_heal_on_blocker_test.go covers build-time gate #1 of the
// Lane A success criteria: when a task transitions to
// LifecycleStateBlockedOnPRMerge via BlockTask, the broker MUST NOT call
// requestCapabilitySelfHealingLocked. The blocked-on-PR-merge state is a
// typed legitimate condition; treating it as an error and spawning a
// repair task for the agent is the bug this gate prevents.
//
// Per the design doc: "The unit test must observe the call site, not
// just the side effect." We swap the package-level hook so we can count
// invocations directly at the entry point — any caller that bypasses
// the gate fires the counter regardless of whether the downstream
// requestSelfHealingLocked has any side effect to observe.

import (
	"testing"
)

func TestBlockTaskDoesNotTriggerSelfHealOnPRMergeBlocker(t *testing.T) {
	// Acceptance:
	//   - Create a task via the standard EnsurePlannedTask path.
	//   - Block it via BlockTask with a "capability gap"-shaped reason
	//     that pre-Lane-A code WOULD have escalated to self-heal.
	//   - Assert the call site did NOT fire (counter stays 0) AND that
	//     the task landed in LifecycleStateBlockedOnPRMerge with the
	//     blocked bool set.
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})

	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Capability gap probe",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil {
		t.Fatalf("ensure planned task: %v", err)
	}

	// Swap the call-site hook BEFORE the BlockTask call so we observe
	// the entry point, not just the eventual self-heal task creation.
	calls := 0
	prev := requestCapabilitySelfHealingHook
	requestCapabilitySelfHealingHook = func(*teamTask, string, string) { calls++ }
	defer func() { requestCapabilitySelfHealingHook = prev }()

	// "missing skill" matches isCapabilityGapBlocker so under pre-Lane-A
	// behavior this would have called the self-heal path. Lane A's gate
	// short-circuits the call before it reaches the hook.
	got, changed, err := b.BlockTask(task.ID, "builder", "missing skill: provider integration unavailable for this lane")
	if err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	if !changed {
		t.Fatalf("expected BlockTask to change task state, got %+v", got)
	}
	if got.LifecycleState != LifecycleStateBlockedOnPRMerge {
		t.Fatalf("expected LifecycleStateBlockedOnPRMerge, got %q", got.LifecycleState)
	}
	if !got.Blocked() {
		t.Fatalf("expected blocked() to be true, got %+v", got)
	}
	if calls != 0 {
		t.Fatalf("expected requestCapabilitySelfHealingLocked to be skipped for blocked_on_pr_merge tasks, got %d call(s)", calls)
	}
}

func TestBlockTaskStillObservableForLegacyNonHarnessPath(t *testing.T) {
	// Negative control: this test pins down what the hook DOES observe
	// when the gate is intentionally bypassed (e.g. a pre-existing,
	// non-harness mutation_service block path that the BlockTask
	// transition layer does not own). Without this pin, an over-eager
	// future refactor could remove the hook entirely under the false
	// assumption that nothing fires it. We simulate the "raw" pre-Lane-A
	// call by invoking the locked helper directly.
	b := newTestBroker(t)
	calls := 0
	prev := requestCapabilitySelfHealingHook
	requestCapabilitySelfHealingHook = func(*teamTask, string, string) { calls++ }
	defer func() { requestCapabilitySelfHealingHook = prev }()

	b.mu.Lock()
	probe := teamTask{ID: "probe", Title: "raw call probe", Owner: "agent-x"}
	b.requestCapabilitySelfHealingLocked(&probe, "agent-x", "missing skill: needs payments connector")
	b.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected the hook to fire when the call site is exercised directly, got %d", calls)
	}
}
