package workflow

import "testing"

// TestGroundExtractionDropsUngroundedSteps is the safety regression: a model
// that invents a step (an action_id not in the gated shape) has it stripped,
// and an all-invented extraction is downgraded to not-a-workflow.
func TestGroundExtractionDropsUngroundedSteps(t *testing.T) {
	e := Extraction{
		IsWorkflow: true,
		Steps: []ExtractedStep{
			{ActionID: "GMAIL_FETCH_EMAILS"},
			{ActionID: "STRIPE_CREATE_CHARGE"}, // not in shape — must be dropped
			{ActionID: "gmail_fetch_emails"},   // dup (case-insensitive) — dropped
			{ActionID: "SLACK_CHAT_POST_MESSAGE"},
		},
	}
	shape := []string{"gmail_fetch_emails", "slack_chat_post_message"}
	g := GroundExtraction(e, shape)
	if len(g.Steps) != 2 {
		t.Fatalf("want 2 grounded steps, got %d: %+v", len(g.Steps), g.Steps)
	}
	if g.Steps[0].ActionID != "GMAIL_FETCH_EMAILS" || g.Steps[1].ActionID != "SLACK_CHAT_POST_MESSAGE" {
		t.Fatalf("wrong grounded steps: %+v", g.Steps)
	}

	allInvented := GroundExtraction(Extraction{IsWorkflow: true,
		Steps: []ExtractedStep{{ActionID: "STRIPE_CREATE_CHARGE"}}}, shape)
	if allInvented.IsWorkflow || len(allInvented.Steps) != 0 {
		t.Fatalf("all-invented extraction must become not-a-workflow, got %+v", allInvented)
	}
}

// TestBuildSpecFromExtractionChain verifies the extractor produces a REAL
// multi-step chain (one hop per step), binds each action, allow-lists reads,
// carries the model's params/expose, and passes shipcheck.
func TestBuildSpecFromExtractionChain(t *testing.T) {
	e := Extraction{
		IsWorkflow: true,
		Name:       "Inbox to Slack alert",
		Steps: []ExtractedStep{
			{ActionID: "GMAIL_FETCH_EMAILS", Platform: "gmail",
				Params:     map[string]any{"query": "is:unread newer_than:1d"},
				ResultPath: "data.messages", Expose: []string{"sender", "subject"}},
			{ActionID: "SLACK_CHAT_POST_MESSAGE", Platform: "slack",
				Params: map[string]any{"channel": "general"}, FeedsFrom: "GMAIL_FETCH_EMAILS"},
		},
	}
	known := map[string]bool{"gmail": true, "slack": true}
	spec, err := BuildSpecFromExtraction("spotted-x", "outbound", e, known)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rep := Shipcheck(&spec); !rep.Passed {
		t.Fatalf("extracted spec must pass shipcheck:\n%s", rep.String())
	}
	// Real chain: 3 states (start -> step_1 -> done), 2 transitions, 2 events.
	if len(spec.States) != 3 || len(spec.Transitions) != 2 || len(spec.Events) != 2 {
		t.Fatalf("want a 2-step chain (3 states/2 transitions/2 events), got %d/%d/%d",
			len(spec.States), len(spec.Transitions), len(spec.Events))
	}
	if spec.Transitions[0].On != "run" {
		t.Errorf("first hop must fire on 'run', got %q", spec.Transitions[0].On)
	}
	if spec.Transitions[1].On != "gmail_fetch_emails_done" {
		t.Errorf("second hop must fire on the prior step's completion, got %q", spec.Transitions[1].On)
	}
	fetch := actionByID(spec, "gmail_fetch_emails")
	if fetch == nil || !fetch.IsIntegrationRead() || fetch.ResultPath != "data.messages" {
		t.Fatalf("fetch must be a bound integration read with the model's result_path, got %+v", fetch)
	}
	if !readAllowed(spec, "gmail", "GMAIL_FETCH_EMAILS") {
		t.Fatalf("bound read must be allow-listed: %+v", spec.AllowedReads)
	}
	post := actionByID(spec, "slack_chat_post_message")
	if post == nil || post.Kind != ActionExternal || post.Platform != "slack" {
		t.Fatalf("post must be a bound external action: %+v", post)
	}
}

// TestParseExtractionToleratesFence ensures a fenced/prose-wrapped model reply
// still decodes.
func TestParseExtractionToleratesFence(t *testing.T) {
	raw := "Sure!\n```json\n{\"is_workflow\":true,\"confidence\":0.9,\"name\":\"X\",\"steps\":[{\"action_id\":\"GMAIL_FETCH_EMAILS\"}]}\n```\nDone."
	e, err := ParseExtraction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !e.IsWorkflow || e.Name != "X" || len(e.Steps) != 1 {
		t.Fatalf("decoded wrong: %+v", e)
	}
}
