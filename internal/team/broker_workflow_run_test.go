package team

import (
	"context"
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
