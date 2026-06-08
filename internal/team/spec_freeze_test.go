package team

import "testing"

// spec_freeze_test.go pins the product rule "once a task's spec is approved it
// must not change after that" (specIsFrozen + the guards in MutateTask /
// handleTaskPlan reuse-merge and the MutateTask update branch).

func TestSpecIsFrozen(t *testing.T) {
	frozen := []LifecycleState{
		LifecycleStateRunning, LifecycleStateReview, LifecycleStateDecision,
		LifecycleStateBlockedOnPRMerge, LifecycleStateApproved,
		LifecycleStateRejected, LifecycleStateArchived,
	}
	// Pre-approval states plus ChangesRequested (the request_changes revise
	// loop deliberately re-opens the spec) must stay editable.
	editable := []LifecycleState{
		"", LifecycleStateUnknown, LifecycleStateDrafting, LifecycleStateIntake,
		LifecycleStateReady, LifecycleStatePlanning, LifecycleStateQueuedBehindOwner,
		LifecycleStateChangesRequested,
	}
	for _, s := range frozen {
		if !specIsFrozen(s) {
			t.Errorf("specIsFrozen(%q) = false, want true (approved spec is frozen)", s)
		}
	}
	for _, s := range editable {
		if specIsFrozen(s) {
			t.Errorf("specIsFrozen(%q) = true, want false (still editable)", s)
		}
	}
}

// TestSpecFreeze_CreateReusePreservesApprovedSpec: once a task is approved and
// running, a duplicate team_task `create` (which findReusableTaskLocked folds
// onto the existing task) must NOT rewrite its approved spec. Compare with
// TestMutateTaskReusesExistingTask, where an `open`/unset-state task DOES
// update — proving the freeze is scoped to post-approval states only.
func TestSpecFreeze_CreateReusePreservesApprovedSpec(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
	}
	const approvedSpec = "Approved spec: build X precisely, with these criteria."
	b.tasks = []teamTask{
		{
			ID:             "task-1",
			Channel:        "general",
			Title:          "Write the plan",
			Owner:          "alice",
			status:         "in_progress",
			Details:        approvedSpec,
			LifecycleState: LifecycleStateRunning,
		},
	}

	got, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Write the plan",
		Details:   "Sneaky rewrite of the approved spec",
		Owner:     "alice",
		CreatedBy: "pm",
	})
	if err != nil {
		t.Fatalf("MutateTask create reuse: %v", err)
	}
	if got.Task.ID != "task-1" {
		t.Fatalf("expected the duplicate create to reuse task-1, got %q", got.Task.ID)
	}
	if b.tasks[0].Details != approvedSpec {
		t.Fatalf("approved (Running) task spec must be frozen; Details changed to %q", b.tasks[0].Details)
	}
}
