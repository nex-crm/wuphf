package team

import (
	"context"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/workflow"
)

func TestWorkflowActionExecRouting(t *testing.T) {
	b := &Broker{}
	called := false
	var gotPlatform, gotAction string
	fakeGate := func(_ context.Context, _, platform, actionID string, _ map[string]any) workflow.ActionOutcome {
		called = true
		gotPlatform, gotAction = platform, actionID
		return workflow.ActionOutcome{OK: false, Output: map[string]any{"needs_approval": true}}
	}
	exec := b.makeWorkflowActionExecWithGate(context.Background(), "revops", fakeGate, &workflow.Spec{})

	// Deterministic action: completes inline, gate not involved.
	if o := exec(workflow.Action{ID: "track", Kind: workflow.ActionDeterministic}, nil); !o.OK {
		t.Fatalf("deterministic action should be OK: %+v", o)
	}

	// LLM action: produces a real draft string.
	o := exec(workflow.Action{ID: "slack_draft", Kind: workflow.ActionLLM}, map[string]any{"owner": "ada"})
	if !o.OK || o.Output["draft"] == nil {
		t.Fatalf("llm action should produce a draft: %+v", o)
	}

	// External action WITH an integration target: routes through the gate, which
	// here requires approval, so the send does not auto-fire.
	o = exec(workflow.Action{ID: "slack_send", Kind: workflow.ActionExternal, Platform: "slack", ActionID: "SLACK_SEND_MESSAGE"}, nil)
	if !called || o.OK {
		t.Fatalf("targeted external should route to the gate and not auto-send: called=%v ok=%v", called, o.OK)
	}
	if gotPlatform != "slack" || gotAction != "SLACK_SEND_MESSAGE" {
		t.Fatalf("gate received wrong target: %s/%s", gotPlatform, gotAction)
	}

	// External action WITHOUT a target: records intent only, gate not called.
	called = false
	o = exec(workflow.Action{ID: "slack_send", Kind: workflow.ActionExternal}, nil)
	if called || !o.OK {
		t.Fatalf("targetless external should record intent without the gate: called=%v ok=%v", called, o.OK)
	}
}

// TestWorkflowRunWiresLLMOutputIntoSend proves the execution fix end-to-end
// through the real action dispatch: a read's output flows into an llm step,
// whose produced text is substituted into the SEND's body — so the send carries
// real content, not the "{{...}}" token or a placeholder. Uses a fake gate so
// no live integration is needed; the llm step runs the real (here unconfigured →
// baseline) provider, which still exercises the output-threading + templating.
func TestWorkflowRunWiresLLMOutputIntoSend(t *testing.T) {
	b := newTestBroker(t)

	var sentBody map[string]any
	fakeGate := func(_ context.Context, _, platform, actionID string, data map[string]any) workflow.ActionOutcome {
		sentBody = data // what the runtime would hand Composio as the send body
		return workflow.ActionOutcome{OK: true, Output: map[string]any{"sent": true}}
	}
	exec := b.makeWorkflowActionExecWithGate(context.Background(), "outbound", fakeGate, &workflow.Spec{})

	// Simulate the runner threading data across steps.
	data := map[string]any{}

	// 1) read already produced the projected emails (skip the live Composio call).
	data["gmail_fetch_emails"] = []any{map[string]any{"subject": "Acme renewal due Friday"}}

	// 2) llm synthesis step — real provider call (baseline when unconfigured).
	llm := workflow.Action{ID: "summarize_urgent_emails", Kind: workflow.ActionLLM,
		Description: "Summarize the urgent emails into a short Slack alert."}
	for k, v := range exec(llm, data).Output {
		data[k] = v
	}
	summary, _ := data["summarize_urgent_emails"].(string)
	if strings.TrimSpace(summary) == "" {
		t.Fatalf("llm step must produce text addressable by its id, got %+v", data)
	}

	// 3) send step references the llm output token; the gate must receive the
	//    REAL produced text, not the token.
	send := workflow.Action{ID: "slack_send_message", Kind: workflow.ActionExternal,
		Platform: "slack", ActionID: "SLACK_SEND_MESSAGE",
		Params: map[string]any{"data": map[string]any{
			"channel": "#general", "markdown_text": "{{summarize_urgent_emails}}",
		}}}
	exec(send, data)

	inner, _ := sentBody["data"].(map[string]any)
	if inner == nil {
		t.Fatalf("send body missing data envelope: %+v", sentBody)
	}
	got, _ := inner["markdown_text"].(string)
	if strings.Contains(got, "{{") {
		t.Fatalf("send still carries an unresolved token: %q", got)
	}
	if got != summary {
		t.Fatalf("send body must be the llm output %q, got %q", summary, got)
	}
}

// TestEnsureWorkflowApprovalRequestMintsCard verifies a workflow's external send
// that needs approval gets a real approval card (kind=approval) carrying the
// action payload — so the human has something to approve and the workflow can
// resume. (The resolver builds the preview but never created the request.)
func TestEnsureWorkflowApprovalRequestMintsCard(t *testing.T) {
	b := newTestBroker(t)
	resp := integrationResolveResponse{
		Decision: "approve", Platform: "slack", ActionID: "SLACK_SEND_MESSAGE", Name: "Slack",
		Account: &integrationResolveAccount{Name: "acme", Key: "secret-key"},
		RawEnvelope: &integrationResolveEnvelope{
			Method: "POST", URL: "https://slack/api",
			Data: map[string]any{"channel": "general", "markdown_text": "real summary"},
		},
	}
	id := b.ensureWorkflowApprovalRequest("ceo", resp)
	if id == "" {
		t.Fatal("expected a minted approval request id")
	}
	var got *humanInterview
	for i := range b.requests {
		if b.requests[i].ID == id {
			got = &b.requests[i]
		}
	}
	if got == nil || got.Kind != "approval" || got.Action == nil {
		t.Fatalf("minted request must be an approval with an action payload, got %+v", got)
	}
	if got.Action.ActionID != "SLACK_SEND_MESSAGE" || got.Action.Platform != "slack" {
		t.Errorf("action payload wrong: %+v", got.Action)
	}
	// The internal connection key must never reach the card.
	if got.Action.Account != nil && got.Action.Account.Key != "" {
		t.Errorf("connection key leaked onto the approval card: %q", got.Action.Account.Key)
	}
}
