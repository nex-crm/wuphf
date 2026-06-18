package team

import (
	"strings"
	"testing"
)

// startPlanning simulates a human approving a task's structured plan: it moves a
// task from Planning to Running so tests that exercise execution / completion
// can proceed past the new plan-first default. No-op when the task is not in
// Planning, so it is safe to call unconditionally after a create.
func startPlanning(t *testing.T, b *Broker, id string) {
	t.Helper()
	cur := b.TaskByID(id)
	if cur == nil || cur.LifecycleState != LifecycleStatePlanning {
		return
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: id, CreatedBy: "human"}); err != nil {
		t.Fatalf("startPlanning approve %s: %v", id, err)
	}
}

func newPlanApprovalBroker(t *testing.T) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}},
	}
	b.mu.Unlock()
	return b
}

// A top-level work Issue with a real owner now lands in Planning (structured
// planning) instead of Running — the owner plans + the human approves before
// execution.
func TestNewIssueLandsInPlanning(t *testing.T) {
	b := newPlanApprovalBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the onboarding revamp",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Task.LifecycleState != LifecycleStatePlanning {
		t.Fatalf("new owner-set issue should land Planning, got %q", created.Task.LifecycleState)
	}
}

// Approving the plan (human) transitions Planning → Running.
func TestApprovePlanStartsExecution(t *testing.T) {
	b := newPlanApprovalBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the onboarding revamp",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: created.Task.ID, CreatedBy: "human"}); err != nil {
		t.Fatalf("approve plan: %v", err)
	}
	if task := b.TaskByID(created.Task.ID); task == nil || task.LifecycleState != LifecycleStateRunning {
		t.Fatalf("approved plan should run, got %v", task)
	}
}

// While a parent is in Planning, sub-issue creation is refused — premature
// decomposition is exactly what the planning phase exists to stop.
func TestPlanGateBlocksSubtaskCreation(t *testing.T) {
	b := newPlanApprovalBroker(t)
	parent, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Build the billing system",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	_, err = b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Wire Stripe webhooks",
		Owner: "eng", CreatedBy: "ceo", ParentIssueID: parent.Task.ID,
	})
	if err == nil {
		t.Fatal("expected sub-issue creation under a planning parent to be refused")
	}
	if !strings.Contains(err.Error(), "planning") {
		t.Fatalf("error should explain the plan gate, got %v", err)
	}
	// After approval, sub-issue creation is allowed.
	startPlanning(t, b, parent.Task.ID)
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Wire Stripe webhooks",
		Owner: "eng", CreatedBy: "ceo", ParentIssueID: parent.Task.ID,
	}); err != nil {
		t.Fatalf("sub-issue after approval should succeed: %v", err)
	}
}

// A sub-issue that merely restates its parent is rejected.
func TestShallowSubtaskRestatingParentRejected(t *testing.T) {
	b := newPlanApprovalBroker(t)
	parent, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the MVP",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	startPlanning(t, b, parent.Task.ID)
	_, err = b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the MVP!",
		Owner: "eng", CreatedBy: "ceo", ParentIssueID: parent.Task.ID,
	})
	if err == nil {
		t.Fatal("expected a sub-issue restating the parent to be rejected")
	}
}

// RaisePlanApproval is idempotent: a second call for the same planning task
// reuses the existing interview instead of stacking a duplicate.
func TestRaisePlanApprovalIdempotent(t *testing.T) {
	b := newPlanApprovalBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Build the billing system",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id1 := b.RaisePlanApproval(created.Task.ID, "eng", "the plan")
	id2 := b.RaisePlanApproval(created.Task.ID, "eng", "the plan again")
	if id1 == "" || id1 != id2 {
		t.Fatalf("expected idempotent reuse, got id1=%q id2=%q", id1, id2)
	}
	b.mu.Lock()
	n := 0
	for i := range b.requests {
		if requestIsPlanApproval(b.requests[i]) && b.requests[i].IssueID == created.Task.ID {
			n++
		}
	}
	b.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected exactly one plan-approval interview, got %d", n)
	}
}

// Answering the plan-approval interview with anything other than approve leaves
// the task in Planning so the owner can revise.
func TestRejectPlanLeavesTaskInPlanning(t *testing.T) {
	b := newPlanApprovalBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the redesign",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	reqID := b.RaisePlanApproval(created.Task.ID, "eng", "the plan")
	if reqID == "" {
		t.Fatal("expected a plan-approval interview")
	}
	if code, msg := b.answerRequestFromActor("human", reqID, "reject", "", ""); code != 200 {
		t.Fatalf("answer reject: code=%d msg=%q", code, msg)
	}
	if task := b.TaskByID(created.Task.ID); task == nil || task.LifecycleState != LifecycleStatePlanning {
		t.Fatalf("rejected plan must leave task in Planning, got %v", task)
	}
}

// Completing a task that is still in Planning is refused — there is no delivered
// work, only a plan awaiting approval.
func TestCompleteFromPlanningRefused(t *testing.T) {
	b := newPlanApprovalBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Write the launch post",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Give the owner access to the task's minted channel so the complete reaches
	// the planning gate rather than the channel-access guard.
	ensureTestMemberAccess(b, created.Task.Channel, "eng", "Eng")
	_, err = b.MutateTask(TaskPostRequest{Action: "complete", ID: created.Task.ID, Channel: created.Task.Channel, CreatedBy: "eng"})
	if err == nil {
		t.Fatal("expected complete on a planning task to be refused")
	}
	if !strings.Contains(err.Error(), "planning") {
		t.Fatalf("error should explain the planning gate, got %v", err)
	}
}

// A sub-issue duplicating an existing sibling is rejected.
func TestDuplicateSiblingSubtaskRejected(t *testing.T) {
	b := newPlanApprovalBroker(t)
	parent, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Build the billing system",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	startPlanning(t, b, parent.Task.ID)
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Wire Stripe webhooks",
		Owner: "eng", CreatedBy: "ceo", ParentIssueID: parent.Task.ID,
	}); err != nil {
		t.Fatalf("first sibling: %v", err)
	}
	_, err = b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "wire the stripe webhook",
		Owner: "eng", CreatedBy: "ceo", ParentIssueID: parent.Task.ID,
	})
	if err == nil {
		t.Fatal("expected a sub-issue duplicating a sibling to be rejected")
	}
}
