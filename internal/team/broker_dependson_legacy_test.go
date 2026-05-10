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
//   - NOT change LifecycleState (legacy path does not touch it)
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
	if owned.LifecycleState != "" {
		t.Errorf("child-with-owner: legacy path must NOT set LifecycleState, got %q", owned.LifecycleState)
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
	if unowned.LifecycleState != "" {
		t.Errorf("child-without-owner: legacy path must NOT set LifecycleState, got %q", unowned.LifecycleState)
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

// TestBlockTaskWritesBlockedOnAndCascadeUnblocks asserts that the public
// BlockTask entry point (used by the CLI / HTTP surface) records the
// typed blocker reference into task.BlockedOn. The cascade is the
// load-bearing acceptance: after the blocker resolves via
// OnDecisionRecorded, the dependent task must transition out of
// LifecycleStateBlockedOnPRMerge into review without any test-only
// helper (SetTaskBlockedOnForTest) propping up the BlockedOn field.
//
// Pre-fix: BlockTask routed through transitionLifecycleLocked but never
// wrote BlockedOn, so unblockDependentsLocked's BlockedOn sweep saw an
// empty list and the cascade silently no-op'd. Tutorial 3 in the ICP
// suite worked around the gap by calling SetTaskBlockedOnForTest
// directly.
func TestBlockTaskWritesBlockedOnAndCascadeUnblocks(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	blocker, _, err := b.EnsureTask("client-loop", "Blocker work", "Land the API change", "builder", "operator", "")
	if err != nil {
		t.Fatalf("ensure blocker: %v", err)
	}
	dependent, _, err := b.EnsureTask("client-loop", "Dependent work", "Wire UI to the new API", "builder", "operator", "")
	if err != nil {
		t.Fatalf("ensure dependent: %v", err)
	}

	got, changed, err := b.BlockTask(dependent.ID, "operator", "waiting on blocker work to merge", blocker.ID)
	if err != nil || !changed {
		t.Fatalf("BlockTask: err=%v changed=%v", err, changed)
	}
	if len(got.BlockedOn) != 1 || got.BlockedOn[0] != blocker.ID {
		t.Fatalf("expected BlockedOn=[%s], got %+v", blocker.ID, got.BlockedOn)
	}
	if got.LifecycleState != LifecycleStateBlockedOnPRMerge {
		t.Fatalf("expected LifecycleStateBlockedOnPRMerge after BlockTask, got %q", got.LifecycleState)
	}

	// Idempotency: a second BlockTask call with the same blockerID must
	// not duplicate the entry.
	if _, _, err := b.BlockTask(dependent.ID, "operator", "still waiting", blocker.ID); err != nil {
		t.Fatalf("BlockTask idempotent re-call: %v", err)
	}
	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID != dependent.ID {
			continue
		}
		if len(b.tasks[i].BlockedOn) != 1 {
			t.Fatalf("expected BlockedOn to stay length 1 after re-block, got %+v", b.tasks[i].BlockedOn)
		}
	}
	b.mu.Unlock()

	// Resolve the blocker. Mark blocker done so hasUnresolvedDepsLocked
	// treats it as resolved, then fire OnDecisionRecorded which is the
	// public entry the harness uses on a Decision Packet merge.
	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID == blocker.ID {
			b.tasks[i].status = "done"
		}
	}
	b.mu.Unlock()

	b.OnDecisionRecorded(blocker.ID)

	b.mu.Lock()
	defer b.mu.Unlock()
	var resolved *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == dependent.ID {
			resolved = &b.tasks[i]
		}
	}
	if resolved == nil {
		t.Fatal("dependent task vanished")
	}
	if resolved.LifecycleState != LifecycleStateReview {
		t.Fatalf("expected dependent to land in LifecycleStateReview after cascade, got %q", resolved.LifecycleState)
	}
	if len(resolved.BlockedOn) != 0 {
		t.Fatalf("expected BlockedOn drained after cascade, got %+v", resolved.BlockedOn)
	}
	if resolved.blocked {
		t.Fatal("expected dependent.blocked=false after cascade")
	}
}
