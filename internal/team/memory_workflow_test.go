package team

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryWorkflowJSONRoundTrip(t *testing.T) {
	score := 0.91
	stale := false
	task := teamTask{
		ID:        "task-1",
		Title:     "Research prior context for onboarding",
		Status:    "in_progress",
		CreatedBy: "ceo",
		CreatedAt: "2026-04-30T10:00:00Z",
		UpdatedAt: "2026-04-30T10:00:00Z",
		MemoryWorkflow: &MemoryWorkflow{
			Required:          true,
			Status:            MemoryWorkflowStatusPending,
			RequirementReason: "research task asks for prior organizational context",
			RequiredSteps:     []MemoryWorkflowStep{MemoryWorkflowStepLookup, MemoryWorkflowStepCapture, MemoryWorkflowStepPromote},
			Lookup: MemoryWorkflowStepState{
				Required:    true,
				Status:      MemoryWorkflowStepStatusSatisfied,
				Actor:       "pm",
				Query:       "prior onboarding context",
				CompletedAt: "2026-04-30T10:01:00Z",
			},
			Citations: []ContextCitation{
				{
					Backend:     "markdown",
					Source:      "notebook",
					Path:        "agents/pm/notebook/onboarding.md",
					ChunkID:     "line-4",
					Title:       "Onboarding",
					Snippet:     "Prior onboarding work",
					Score:       &score,
					Stale:       &stale,
					RetrievedAt: "2026-04-30T10:01:00Z",
				},
			},
		},
	}

	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"memory_workflow"`) {
		t.Fatalf("expected memory_workflow in JSON: %s", raw)
	}

	var decoded teamTask
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.MemoryWorkflow == nil || !decoded.MemoryWorkflow.Required {
		t.Fatalf("workflow did not round trip: %+v", decoded.MemoryWorkflow)
	}
	got := decoded.MemoryWorkflow.Citations[0]
	if got.Backend != "markdown" || got.Source != "notebook" || got.Path != "agents/pm/notebook/onboarding.md" {
		t.Fatalf("citation did not round trip: %+v", got)
	}
	if got.Score == nil || *got.Score != score || got.Stale == nil || *got.Stale != stale {
		t.Fatalf("score/stale did not round trip: %+v", got)
	}
}

func TestMemoryWorkflowRequirementPolicy(t *testing.T) {
	cases := []struct {
		name string
		task teamTask
		want bool
	}{
		{
			name: "process research task requires workflow",
			task: teamTask{TaskType: "process-research", Title: "Map support escalation memory"},
			want: true,
		},
		{
			name: "research task with prior context requires workflow",
			task: teamTask{TaskType: "research", Title: "Research prior context for renewal playbook"},
			want: true,
		},
		{
			name: "plain research task does not accidentally block",
			task: teamTask{TaskType: "research", Title: "Compare pricing pages"},
			want: false,
		},
		{
			name: "feature task does not accidentally block",
			task: teamTask{TaskType: "feature", Title: "Implement task drawer"},
			want: false,
		},
		{
			name: "launch task does not accidentally block",
			task: teamTask{TaskType: "launch", Title: "Launch customer webinar"},
			want: false,
		},
		{
			name: "follow up task does not accidentally block",
			task: teamTask{TaskType: "follow_up", Title: "Follow up with Sarah"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := memoryWorkflowRequirementForTask(&tc.task)
			if got.Required != tc.want {
				t.Fatalf("required=%v want %v (%+v)", got.Required, tc.want, got)
			}
		})
	}
}

func TestMemoryWorkflowTransitionsAreIdempotent(t *testing.T) {
	task := &teamTask{
		ID:        "task-1",
		TaskType:  "research",
		Title:     "Research prior context for onboarding",
		CreatedAt: "2026-04-30T10:00:00Z",
		UpdatedAt: "2026-04-30T10:00:00Z",
	}
	syncTaskMemoryWorkflow(task, "2026-04-30T10:00:00Z")
	if task.MemoryWorkflow == nil || task.MemoryWorkflow.Status != MemoryWorkflowStatusPending {
		t.Fatalf("expected pending required workflow, got %+v", task.MemoryWorkflow)
	}

	citation := ContextCitation{Backend: "markdown", Source: "notebook", Path: "agents/pm/notebook/onboarding.md", Snippet: "prior work"}
	if !recordMemoryWorkflowLookup(task, "pm", "prior onboarding", []ContextCitation{citation}, "2026-04-30T10:01:00Z") {
		t.Fatal("expected lookup to change workflow")
	}
	if recordMemoryWorkflowLookup(task, "pm", "prior onboarding", []ContextCitation{citation}, "2026-04-30T10:01:00Z") {
		t.Fatal("duplicate lookup should be idempotent")
	}
	if got := len(task.MemoryWorkflow.Citations); got != 1 {
		t.Fatalf("expected one citation, got %d", got)
	}

	capture := MemoryWorkflowArtifact{Backend: "markdown", Source: "notebook", Path: "agents/pm/notebook/onboarding.md", Title: "Onboarding"}
	if !recordMemoryWorkflowCapture(task, "pm", capture, "2026-04-30T10:02:00Z") {
		t.Fatal("expected capture to change workflow")
	}
	if recordMemoryWorkflowCapture(task, "pm", capture, "2026-04-30T10:02:00Z") {
		t.Fatal("duplicate capture should be idempotent")
	}
	promotion := MemoryWorkflowArtifact{Backend: "markdown", Source: "promotion", Path: "team/process/onboarding.md", PromotionID: "rvw-1"}
	if !recordMemoryWorkflowPromotion(task, "pm", promotion, "2026-04-30T10:03:00Z") {
		t.Fatal("expected promotion to change workflow")
	}
	if recordMemoryWorkflowPromotion(task, "pm", promotion, "2026-04-30T10:03:00Z") {
		t.Fatal("duplicate promotion should be idempotent")
	}
	if task.MemoryWorkflow.Status != MemoryWorkflowStatusSatisfied {
		t.Fatalf("expected satisfied workflow, got %+v", task.MemoryWorkflow)
	}
	if got := len(task.MemoryWorkflow.Captures); got != 1 {
		t.Fatalf("expected one capture, got %d", got)
	}
	if got := len(task.MemoryWorkflow.Promotions); got != 1 {
		t.Fatalf("expected one promotion, got %d", got)
	}
}

func TestMemoryWorkflowCompletionGateAndOverride(t *testing.T) {
	task := &teamTask{
		ID:        "task-1",
		TaskType:  "research",
		Title:     "Research prior context for onboarding",
		Status:    "in_progress",
		CreatedAt: "2026-04-30T10:00:00Z",
		UpdatedAt: "2026-04-30T10:00:00Z",
	}
	syncTaskMemoryWorkflow(task, "2026-04-30T10:00:00Z")

	err := applyMemoryWorkflowCompletionGate(task, "ceo", "", false, "2026-04-30T10:04:00Z")
	if err == nil || !strings.Contains(err.Error(), "memory workflow incomplete") {
		t.Fatalf("expected incomplete workflow error, got %v", err)
	}

	if err := applyMemoryWorkflowCompletionGate(task, "ceo", "Human accepted missing memory evidence", true, "2026-04-30T10:05:00Z"); err != nil {
		t.Fatalf("override should allow completion: %v", err)
	}
	if task.MemoryWorkflow.Status != MemoryWorkflowStatusOverridden || task.MemoryWorkflow.Override == nil {
		t.Fatalf("expected override state, got %+v", task.MemoryWorkflow)
	}
	if task.MemoryWorkflow.Override.Actor != "ceo" || task.MemoryWorkflow.Override.Reason == "" || task.MemoryWorkflow.Override.Timestamp != "2026-04-30T10:05:00Z" {
		t.Fatalf("override metadata missing: %+v", task.MemoryWorkflow.Override)
	}
}
