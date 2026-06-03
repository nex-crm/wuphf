package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPlanFirstEnabled(t *testing.T) {
	tru, fls := true, false
	if (TaskPlanInput{}).PlanFirstEnabled() {
		t.Errorf("absent PlanFirst should resolve OFF")
	}
	if !(TaskPlanInput{PlanFirst: &tru}).PlanFirstEnabled() {
		t.Errorf("explicit true should resolve ON")
	}
	if (TaskPlanInput{PlanFirst: &fls}).PlanFirstEnabled() {
		t.Errorf("explicit false should resolve OFF")
	}
}

func TestTaskIsPreExecution(t *testing.T) {
	for _, s := range []LifecycleState{"", LifecycleStateUnknown, LifecycleStateDrafting, LifecycleStateIntake, LifecycleStateReady, LifecycleStatePlanning, LifecycleStateQueuedBehindOwner} {
		if !taskIsPreExecution(s) {
			t.Errorf("%q should be pre-execution", s)
		}
	}
	for _, s := range []LifecycleState{LifecycleStateRunning, LifecycleStateReview, LifecycleStateDecision, LifecycleStateApproved, LifecycleStateRejected, LifecycleStateArchived} {
		if taskIsPreExecution(s) {
			t.Errorf("%q should NOT be pre-execution", s)
		}
	}
}

func TestPlanningStateIsExecutableAndInProgressStage(t *testing.T) {
	if !isExecutableTeamTaskStatus(LifecycleStatePlanning) {
		t.Errorf("Planning must be executable so the owner is dispatched to plan")
	}
	if got := lifecycleStageFor(LifecycleStatePlanning); got != StageInProgress {
		t.Errorf("Planning stage = %q, want in_progress", got)
	}
	row, ok := derivedFieldsFor(LifecycleStatePlanning)
	if !ok || row.Status != "in_progress" || row.PipelineStage != "plan" {
		t.Errorf("Planning derived fields = %+v ok=%v, want status=in_progress stage=plan", row, ok)
	}
}

func TestPlanModeDirectiveTellsOwnerToPlanOnly(t *testing.T) {
	d := planModeDirective(teamTask{ID: "OFFICE-7"})
	for _, want := range []string{"PLAN MODE", "notebook_write", "do NOT change the repo", "STOP", "Approve & Start", "OFFICE-7"} {
		if !strings.Contains(d, want) {
			t.Errorf("plan directive missing %q\n%s", want, d)
		}
	}
}

// postTaskPlanForTest POSTs a single-task plan and returns the created tasks.
func postTaskPlanForTest(t *testing.T, b *Broker, task map[string]any) []teamTask {
	t.Helper()
	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "human",
		"tasks":      []map[string]any{task},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task-plan request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("task-plan status %d: %s", resp.StatusCode, raw)
	}
	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task-plan: %v", err)
	}
	return result.Tasks
}

func TestBrokerTaskPlanFirstStartsInPlanning(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	tasks := postTaskPlanForTest(t, b, map[string]any{
		"title":      "Build the onboarding flow",
		"assignee":   "builder",
		"task_type":  "feature",
		"plan_first": true,
	})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	if got.LifecycleState != LifecycleStatePlanning {
		t.Fatalf("plan-first start-now task should be in Planning, got %q (%+v)", got.LifecycleState, got)
	}
	if !got.PlanFirst {
		t.Errorf("expected PlanFirst persisted true")
	}
}

func TestBrokerTaskPlanFirstOffRunsImmediately(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	tasks := postTaskPlanForTest(t, b, map[string]any{
		"title":      "Quick fix the typo",
		"assignee":   "builder",
		"task_type":  "feature",
		"plan_first": false,
	})
	got := tasks[0]
	if got.LifecycleState == LifecycleStatePlanning {
		t.Fatalf("plan-first OFF task must NOT be in Planning, got %+v", got)
	}
	if got.Status() != "in_progress" {
		t.Fatalf("plan-first OFF start-now task should run immediately (in_progress), got %q", got.Status())
	}
}

func TestBrokerApprovePlanTransitionsPlanningToRunning(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	tasks := postTaskPlanForTest(t, b, map[string]any{
		"title":      "Build the onboarding flow",
		"assignee":   "builder",
		"task_type":  "feature",
		"plan_first": true,
	})
	taskID := tasks[0].ID
	if tasks[0].LifecycleState != LifecycleStatePlanning {
		t.Fatalf("precondition: task should be Planning, got %q", tasks[0].LifecycleState)
	}

	// Approve the plan → execution starts.
	if err := b.RecordTaskDecision(taskID, "approve", "human"); err != nil {
		t.Fatalf("approve plan: %v", err)
	}
	updated := b.TaskByID(taskID)
	if updated == nil {
		t.Fatalf("task %s not found after approve", taskID)
	}
	if updated.LifecycleState != LifecycleStateRunning {
		t.Fatalf("approving the plan should move Planning→Running, got %q", updated.LifecycleState)
	}
}
