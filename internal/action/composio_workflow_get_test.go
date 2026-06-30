package action

import (
	"context"
	"encoding/json"
	"testing"
)

// GetWorkflow is the read side of compile-and-freeze: a definition saved by
// CreateWorkflow reads back as decoded steps, and a missing key reports
// Exists:false rather than erroring (so callers can show a "compile it" state).
func TestGetWorkflowRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &ComposioREST{APIKey: "cmp_test", UserID: "tester@example.com"}

	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"title":   "Daily digest",
		"steps": []map[string]any{
			{"id": "read", "type": "template", "template": "read email"},
			{
				"id":        "send",
				"type":      "action",
				"platform":  "slack",
				"action_id": "SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL",
			},
		},
	})
	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{
		Key:        "digest",
		Definition: definition,
	}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	got, err := client.GetWorkflow(context.Background(), "digest")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if !got.Exists {
		t.Fatal("expected Exists=true for a saved workflow")
	}
	if got.Title != "Daily digest" {
		t.Fatalf("title = %q, want %q", got.Title, "Daily digest")
	}
	if len(got.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(got.Steps))
	}
	// The send step is a mutating action, so it reads back gated.
	send := got.Steps[1]
	if send.ActionID != "SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL" || !send.Gated {
		t.Fatalf("send step not gated as expected: %+v", send)
	}
	// The read step is a non-mutating template, so it is not gated.
	if got.Steps[0].Gated {
		t.Fatalf("read step should not be gated: %+v", got.Steps[0])
	}
}

func TestGetWorkflowMissingIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &ComposioREST{APIKey: "cmp_test", UserID: "tester@example.com"}

	got, err := client.GetWorkflow(context.Background(), "never-compiled")
	if err != nil {
		t.Fatalf("GetWorkflow on missing key should not error: %v", err)
	}
	if got.Exists {
		t.Fatal("expected Exists=false for a key that was never compiled")
	}
}
