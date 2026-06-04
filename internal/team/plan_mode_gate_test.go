package team

// plan_mode_gate_test.go verifies the Phase 5 human-approval gate: with
// Plan-first ON, the owner plans and STOPS in LifecycleStatePlanning until a
// HUMAN approves. No agent — not even the CEO — may approve or otherwise
// advance the plan toward execution via team_task. (Live E2E surfaced the CEO
// auto-approving a plan, bypassing the gate the composer promises.)

import (
	"strings"
	"testing"
	"time"
)

func seedPlanningTask(t *testing.T, b *Broker, id, owner string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	task := teamTask{
		ID:        id,
		Channel:   "general",
		Title:     "plan-gated task",
		Owner:     owner,
		PlanFirst: true,
		CreatedBy: "human",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := b.applyLifecycleStateLocked(&task, LifecycleStatePlanning); err != nil {
		t.Fatalf("seed planning task: %v", err)
	}
	b.tasks = append(b.tasks, task)
}

func lifecycleStateOf(t *testing.T, b *Broker, id string) LifecycleState {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if task := b.findTaskByIDLocked(id); task != nil {
		return task.LifecycleState
	}
	return ""
}

func TestPlanMode_CEOCannotApprovePlan(t *testing.T) {
	b := newTestBroker(t)
	seedPlanningTask(t, b, "OFFICE-1", "executor")

	// The CEO is an agent — it passes checkTaskActionAuthLocked (approve is a
	// CEO-allowed scope action) but must be stopped by the Plan-mode gate.
	_, err := b.MutateTask(TaskPostRequest{
		Action:    "approve",
		ID:        "OFFICE-1",
		Channel:   "general",
		CreatedBy: "ceo",
	})
	if err == nil {
		t.Fatalf("CEO approve on a Planning task must be forbidden; got nil error")
	}
	if !strings.Contains(err.Error(), "Plan mode") {
		t.Fatalf("expected a Plan-mode steer in the error, got: %v", err)
	}
	if got := lifecycleStateOf(t, b, "OFFICE-1"); got != LifecycleStatePlanning {
		t.Fatalf("task must stay in Planning after a blocked CEO approve, got %q", got)
	}
}

func TestPlanMode_OtherAgentCannotCompletePlan(t *testing.T) {
	b := newTestBroker(t)
	seedPlanningTask(t, b, "OFFICE-2", "executor")

	// The owner trying to short-circuit the plan into done is also blocked —
	// only the human "Approve plan & Start" leaves Planning.
	_, err := b.MutateTask(TaskPostRequest{
		Action:    "complete",
		ID:        "OFFICE-2",
		Channel:   "general",
		CreatedBy: "executor",
	})
	if err == nil {
		t.Fatalf("agent complete on a Planning task must be forbidden; got nil error")
	}
	if got := lifecycleStateOf(t, b, "OFFICE-2"); got != LifecycleStatePlanning {
		t.Fatalf("task must stay in Planning after a blocked agent complete, got %q", got)
	}
}

func TestPlanMode_HumanApproveNotBlockedByGate(t *testing.T) {
	b := newTestBroker(t)
	seedPlanningTask(t, b, "OFFICE-3", "executor")

	// A human is never blocked by the plan-mode gate. (The real human path is
	// the decision-packet "Approve plan & Start"; here we only assert the gate
	// does not forbid a human actor.)
	_, err := b.MutateTask(TaskPostRequest{
		Action:    "approve",
		ID:        "OFFICE-3",
		Channel:   "general",
		CreatedBy: "human",
	})
	if err != nil && strings.Contains(err.Error(), "Plan mode") {
		t.Fatalf("human approve must not hit the Plan-mode gate, got: %v", err)
	}
	if got := lifecycleStateOf(t, b, "OFFICE-3"); got == LifecycleStatePlanning {
		t.Fatalf("human approve should advance the task out of Planning, still %q", got)
	}
}

func TestPlanMode_GateScopedToPlanningOnly(t *testing.T) {
	// The gate must NOT touch the normal review-approval path: an agent
	// reviewer approving COMPLETED work (Review state) is still allowed.
	b := newTestBroker(t)
	b.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	task := teamTask{
		ID: "OFFICE-4", Channel: "general", Title: "reviewed work",
		Owner: "executor", CreatedBy: "human", CreatedAt: now, UpdatedAt: now,
	}
	if err := b.applyLifecycleStateLocked(&task, LifecycleStateReview); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed review task: %v", err)
	}
	b.tasks = append(b.tasks, task)
	b.mu.Unlock()

	_, err := b.MutateTask(TaskPostRequest{
		Action:    "approve",
		ID:        "OFFICE-4",
		Channel:   "general",
		CreatedBy: "ceo",
	})
	if err != nil && strings.Contains(err.Error(), "Plan mode") {
		t.Fatalf("review-state approve must not hit the Plan-mode gate, got: %v", err)
	}
}
