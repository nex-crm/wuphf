package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/workflow"
)

type stubExtractor struct {
	ret      workflow.Extraction
	gotInput workflow.ExtractInput
	called   bool
}

func (s *stubExtractor) Extract(in workflow.ExtractInput) (workflow.Extraction, error) {
	s.called = true
	s.gotInput = in
	return s.ret, nil
}

// seedTask registers a task so the extractor can read its goal/owner.
func seedTask(b *Broker, id, title, details, owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, teamTask{ID: id, Title: title, Details: details, Owner: owner})
}

// TestExtractWorkflowForTaskEndToEnd drives the broker glue with a stub model:
// real traces -> gated shape -> grounded extraction -> built + shipchecked
// contract. The model's phantom step (not in the trace) must be dropped.
func TestExtractWorkflowForTaskEndToEnd(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	seedTask(b, "OFFICE-9", "Inbox to Slack", "fetch urgent email, alert slack", "outbound")

	persistActionTrace(ActionTrace{TaskID: "OFFICE-9", Seq: 0, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS",
		Args: map[string]any{"data": map[string]any{"query": "is:unread"}}, Result: `{"data":{"messages":[]}}`})
	persistActionTrace(ActionTrace{TaskID: "OFFICE-9", Seq: 1, Platform: "slack", ActionID: "SLACK_CHAT_POST_MESSAGE",
		Args: map[string]any{"data": map[string]any{"channel": "general"}}})

	stub := &stubExtractor{ret: workflow.Extraction{
		IsWorkflow: true, Confidence: 0.9, Name: "Inbox to Slack alert",
		Trigger: workflow.ExtractedTrigger{Kind: "schedule", IntervalMinutes: 1440},
		Steps: []workflow.ExtractedStep{
			{ActionID: "GMAIL_FETCH_EMAILS", Platform: "gmail", Params: map[string]any{"query": "is:unread"}, ResultPath: "data.messages", Expose: []string{"sender"}},
			{ActionID: "STRIPE_CREATE_CHARGE", Platform: "stripe"}, // phantom — must be grounded out
			{ActionID: "SLACK_CHAT_POST_MESSAGE", Platform: "slack", Params: map[string]any{"channel": "general"}},
		},
	}}

	prop, err := b.extractWorkflowForTask("OFFICE-9", stub)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !stub.called {
		t.Fatal("model should have been called (>=2 integration actions)")
	}
	// Gate built the right allow-list shape from the trace.
	if len(stub.gotInput.Shape) != 2 {
		t.Fatalf("gated shape should be 2 tokens, got %v", stub.gotInput.Shape)
	}
	if !prop.IsWorkflow || prop.Spec == nil {
		t.Fatalf("expected a workflow proposal with a spec, got %+v", prop)
	}
	if prop.Trigger.Kind != "schedule" || prop.Trigger.IntervalMinutes != 1440 {
		t.Errorf("trigger not carried through: %+v", prop.Trigger)
	}
	if prop.Shipcheck == nil || !prop.Shipcheck.Passed {
		t.Fatalf("extracted contract must pass shipcheck: %+v", prop.Shipcheck)
	}
	// Phantom step grounded out: spec has exactly the 2 real actions.
	if len(prop.Spec.Actions) != 2 {
		t.Fatalf("phantom step must be dropped, got %d actions: %+v", len(prop.Spec.Actions), prop.Spec.Actions)
	}
	for _, a := range prop.Spec.Actions {
		if a.ID == "stripe_create_charge" {
			t.Fatal("ungrounded phantom action leaked into the contract")
		}
	}
}

// TestExtractWorkflowForTaskGate verifies the cheap gate skips the model for a
// task with fewer than two integration actions.
func TestExtractWorkflowForTaskGate(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	seedTask(b, "OFFICE-SOLO", "one lookup", "just check something", "ceo")
	persistActionTrace(ActionTrace{TaskID: "OFFICE-SOLO", Seq: 0, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"})

	stub := &stubExtractor{ret: workflow.Extraction{IsWorkflow: true}}
	prop, err := b.extractWorkflowForTask("OFFICE-SOLO", stub)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if stub.called {
		t.Error("model must NOT be called when the gate fails (single integration action)")
	}
	if prop.IsWorkflow {
		t.Errorf("single-action task must not be a workflow: %+v", prop)
	}
}
