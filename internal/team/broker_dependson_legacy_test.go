package team

// broker_dependson_legacy_test.go covers build-time gate #8 (IRON RULE
// — required without ask) of the Lane A success criteria: tasks that
// existed before Lane A and use the legacy DependsOn-based block /
// unblock pattern MUST keep working identically post-rename. Pre-Lane-A
// brokers serialised many such tasks to disk; if this regression
// landed, every legacy unblock would either silently fail or move the
// task into the wrong state.
//
// The test exercises the cascade explicitly: a parent task in `done`,
// a child task with `DependsOn: [parent]`, blocked = true, no
// LifecycleState set (the migration shim leaves nothing on tasks that
// already have a state, but legacy in-memory tasks created before Lane
// A's deploy may still arrive with empty LifecycleState during the
// transition window). After unblockDependentsLocked(parent), the child
// must:
//   - have blocked = false
//   - have status = "in_progress" (legacy-owner case) or "open" (no-owner)
//   - have LifecycleState and the lifecycle index reconciled to the legacy
//     status outcome
//   - have the "task_unblocked" action appended

import (
	"testing"
	"time"
)

func TestUnblockDependentsLegacyDependsOnPath(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{
			ID:        "parent-done",
			Channel:   "client-loop",
			Title:     "Done parent",
			Owner:     "builder",
			status:    "done",
			CreatedBy: "operator",
			TaskType:  "feature",
			CreatedAt: now,
			UpdatedAt: now,
			// LifecycleState intentionally empty — simulates a pre-Lane-A
			// task that has not yet been touched by the migration shim.
		},
		{
			ID:        "child-with-owner",
			Channel:   "client-loop",
			Title:     "Owned dependent",
			Owner:     "builder",
			status:    "blocked",
			blocked:   true,
			DependsOn: []string{"parent-done"},
			CreatedBy: "operator",
			TaskType:  "feature",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "child-without-owner",
			Channel:   "client-loop",
			Title:     "Unowned dependent",
			status:    "blocked",
			blocked:   true,
			DependsOn: []string{"parent-done"},
			CreatedBy: "operator",
			TaskType:  "feature",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	priorActions := len(b.actions)
	b.unblockDependentsLocked("parent-done")

	getByID := func(id string) *teamTask {
		for i := range b.tasks {
			if b.tasks[i].ID == id {
				return &b.tasks[i]
			}
		}
		return nil
	}

	owned := getByID("child-with-owner")
	if owned == nil {
		b.mu.Unlock()
		t.Fatal("child-with-owner missing")
	}
	if owned.blocked {
		t.Errorf("child-with-owner: expected blocked=false post-unblock, got true")
	}
	if owned.status != "in_progress" {
		t.Errorf("child-with-owner: expected status=in_progress, got %q", owned.status)
	}
	if owned.LifecycleState != LifecycleStateRunning {
		t.Errorf("child-with-owner: expected LifecycleState=%q, got %q", LifecycleStateRunning, owned.LifecycleState)
	}

	unowned := getByID("child-without-owner")
	if unowned == nil {
		b.mu.Unlock()
		t.Fatal("child-without-owner missing")
	}
	if unowned.blocked {
		t.Errorf("child-without-owner: expected blocked=false post-unblock, got true")
	}
	if unowned.status != "open" {
		t.Errorf("child-without-owner: expected status=open (no owner), got %q", unowned.status)
	}
	if unowned.LifecycleState != LifecycleStateReady {
		t.Errorf("child-without-owner: expected LifecycleState=%q, got %q", LifecycleStateReady, unowned.LifecycleState)
	}
	index := b.lifecycleIndexSnapshotLocked()
	if got := index[LifecycleStateRunning]; !containsString(got, "child-with-owner") {
		t.Errorf("expected lifecycle index %q to include child-with-owner, got %v", LifecycleStateRunning, got)
	}
	if got := index[LifecycleStateReady]; !containsString(got, "child-without-owner") {
		t.Errorf("expected lifecycle index %q to include child-without-owner, got %v", LifecycleStateReady, got)
	}

	// Two task_unblocked actions should have been appended.
	unblockedActions := 0
	for _, action := range b.actions[priorActions:] {
		if action.Kind == "task_unblocked" {
			unblockedActions++
		}
	}
	b.mu.Unlock()
	if unblockedActions != 2 {
		t.Errorf("expected 2 task_unblocked actions, got %d", unblockedActions)
	}
}

func TestUnblockDependentsHarnessBlockedOnPRMergePath(t *testing.T) {
	// Acceptance: a task in LifecycleStateBlockedOnPRMerge with a typed
	// BlockedOn entry that resolves must transition to
	// LifecycleStateReview (not "in_progress"), per the design doc:
	// "blocked_on_pr_merge → after removing the resolved entry from
	// BlockedOn, if list empty, transition to review."
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{
			ID:        "blocker-task",
			Channel:   "client-loop",
			Title:     "Blocker that merged",
			Owner:     "builder",
			status:    "done",
			CreatedBy: "operator",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:             "harness-blocked",
			Channel:        "client-loop",
			Title:          "Waiting on PR merge",
			Owner:          "builder",
			status:         "blocked",
			blocked:        true,
			BlockedOn:      []string{"blocker-task"},
			LifecycleState: LifecycleStateBlockedOnPRMerge,
			pipelineStage:  "review",
			reviewState:    "ready_for_review",
			CreatedBy:      "operator",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	// Index the seed state so the transition layer's index update has
	// something to subtract from.
	b.indexLifecycleLocked("harness-blocked", "", LifecycleStateBlockedOnPRMerge)

	b.unblockDependentsLocked("blocker-task")

	var resolved *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == "harness-blocked" {
			resolved = &b.tasks[i]
		}
	}
	b.mu.Unlock()
	if resolved == nil {
		t.Fatal("harness-blocked task vanished")
	}
	if resolved.LifecycleState != LifecycleStateReview {
		t.Errorf("harness path: expected LifecycleStateReview after unblock, got %q", resolved.LifecycleState)
	}
	if len(resolved.BlockedOn) != 0 {
		t.Errorf("harness path: expected BlockedOn to be drained, got %+v", resolved.BlockedOn)
	}
	if resolved.blocked {
		t.Errorf("harness path: expected blocked=false derived from review state, got true")
	}
}

func TestOnDecisionRecordedExtendsUnblockListener(t *testing.T) {
	// Acceptance: the registered decision.recorded handler (Lane A
	// startup wiring) calls into unblockDependentsLocked under b.mu and
	// persists. Lane B / C / D event emitters can rely on this entry
	// point surviving broker restarts.
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "blocker", Channel: "client-loop", Title: "Done blocker", Owner: "builder", status: "done", CreatedBy: "operator", CreatedAt: now, UpdatedAt: now},
		{ID: "child", Channel: "client-loop", Title: "Was blocked", Owner: "builder", status: "blocked", blocked: true, DependsOn: []string{"blocker"}, CreatedBy: "operator", CreatedAt: now, UpdatedAt: now},
	}
	b.mu.Unlock()

	b.OnDecisionRecorded("blocker")

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == "child" && b.tasks[i].blocked {
			t.Fatalf("expected child to unblock after OnDecisionRecorded, got blocked=true")
		}
	}
}
