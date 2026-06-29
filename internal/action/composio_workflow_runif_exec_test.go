package action

import (
	"context"
	"encoding/json"
	"testing"
)

// run_if gates a step deterministically through the real ExecuteWorkflow path:
// the step whose predicate is true runs, the one whose predicate is false is
// skipped (recorded, never executed). Template steps keep this network-free.
func TestExecuteWorkflowRunIfGatesSteps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	client := &ComposioREST{
		APIKey: "cmp_test",
		UserID: "najmuzzaman@nex.ai",
	}

	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"inputs":  map[string]any{"fit": 82},
		"steps": []map[string]any{
			{
				"id":       "alert",
				"type":     "template",
				"run_if":   "inputs.fit >= 80",
				"template": "fire",
			},
			{
				"id":       "nurture",
				"type":     "template",
				"run_if":   "inputs.fit < 80",
				"template": "nurture",
			},
		},
	})

	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{
		Key:        "runif-gate",
		Definition: definition,
	}); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	result, err := client.ExecuteWorkflow(context.Background(), WorkflowExecuteRequest{
		KeyOrPath: "runif-gate",
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}

	var alert map[string]any
	if err := json.Unmarshal(result.Steps["alert"], &alert); err != nil {
		t.Fatalf("decode alert step: %v", err)
	}
	if alert["skipped"] == true {
		t.Fatalf("alert should have run (fit 82 >= 80), got skipped: %#v", alert)
	}
	if alert["result"] != "fire" {
		t.Fatalf("alert should have rendered template, got %#v", alert["result"])
	}

	var nurture map[string]any
	if err := json.Unmarshal(result.Steps["nurture"], &nurture); err != nil {
		t.Fatalf("decode nurture step: %v", err)
	}
	if nurture["skipped"] != true {
		t.Fatalf("nurture should have been skipped (fit 82 not < 80), got %#v", nurture)
	}
}

// A malformed run_if must be rejected at create time, before any run.
func TestCreateWorkflowRejectsInvalidRunIf(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &ComposioREST{APIKey: "cmp_test", UserID: "najmuzzaman@nex.ai"}

	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"steps": []map[string]any{
			{"id": "x", "type": "template", "template": "hi", "run_if": "inputs.fit >="},
		},
	})
	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{
		Key:        "bad-runif",
		Definition: definition,
	}); err == nil {
		t.Fatal("expected create to reject an invalid run_if, got nil error")
	}
}
