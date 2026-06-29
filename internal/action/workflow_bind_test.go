package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeResolver binds each plan step from a fixed table keyed by step id.
type fakeResolver struct {
	byID map[string]BoundStep
}

func (f fakeResolver) Resolve(_ context.Context, _ Plan, step PlanStep) (BoundStep, error) {
	return f.byID[step.ID], nil
}

func TestBindWorkflowPlanStructure(t *testing.T) {
	plan := Plan{
		Name:   "High-fit demo alert",
		ToolID: "inbound-routing",
		Steps: []PlanStep{
			{ID: "trigger", Kind: "trigger", Title: "Demo booked"},
			{ID: "score", Kind: "ai", Title: "Score fit"},
			{ID: "alert", Kind: "action", Title: "Post to Slack", Integration: "Slack", Gated: true},
		},
	}
	resolver := fakeResolver{byID: map[string]BoundStep{
		"trigger": {Skip: true}, // pure UI marker, no runnable counterpart
		"score":   {Type: "template", Template: "{{ inputs.fit }}"},
		"alert": {
			Type:     "action",
			Platform: "slack",
			ActionID: "SLACK_SENDS_A_MESSAGE",
			Params:   map[string]any{"channel": "#sales"},
			RunIf:    "inputs.fit >= 80",
		},
	}}

	def, err := BindWorkflowPlan(context.Background(), plan, resolver)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if def.Version != composioWorkflowVersion {
		t.Fatalf("version = %q, want %q", def.Version, composioWorkflowVersion)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("expected 2 steps (trigger skipped), got %d", len(def.Steps))
	}
	alert := def.Steps[1]
	if alert.Type != "action" || alert.Platform != "slack" || alert.ActionID != "SLACK_SENDS_A_MESSAGE" {
		t.Fatalf("alert not bound to the Composio action: %#v", alert)
	}
	if alert.RunIf != "inputs.fit >= 80" {
		t.Fatalf("run_if did not ride through binding, got %q", alert.RunIf)
	}
}

func TestBindWorkflowPlanRejectsBadBinding(t *testing.T) {
	plan := Plan{Name: "x", Steps: []PlanStep{{ID: "a", Kind: "action"}}}
	// Action step bound without an action_id must be rejected at bind time.
	resolver := fakeResolver{byID: map[string]BoundStep{
		"a": {Type: "action", Platform: "slack"},
	}}
	if _, err := BindWorkflowPlan(context.Background(), plan, resolver); err == nil {
		t.Fatal("expected bind to reject an action step missing action_id")
	}
}

// The bound definition runs through the real executor and run_if gates the step.
func TestBindWorkflowPlanRunIfGatesAtRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &ComposioREST{APIKey: "cmp_test", UserID: "najmuzzaman@nex.ai"}

	plan := Plan{
		Name:   "gate demo",
		Inputs: map[string]any{"fit": 70},
		Steps: []PlanStep{
			{ID: "score", Kind: "ai", Title: "Score"},
			{ID: "alert", Kind: "action", Title: "Alert", Gated: true},
		},
	}
	// Keep network out of the test: bind the gated step to a template carrying the
	// run_if, so we exercise bind -> definition -> executor skip deterministically.
	resolver := fakeResolver{byID: map[string]BoundStep{
		"score": {Type: "template", Template: "noted"},
		"alert": {Type: "template", Template: "fire", RunIf: "inputs.fit >= 80"},
	}}

	def, err := BindWorkflowPlan(context.Background(), plan, resolver)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	raw, _ := json.Marshal(def)
	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{Key: "bind-gate", Definition: raw}); err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := client.ExecuteWorkflow(context.Background(), WorkflowExecuteRequest{KeyOrPath: "bind-gate"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var alert map[string]any
	_ = json.Unmarshal(result.Steps["alert"], &alert)
	if alert["skipped"] != true {
		t.Fatalf("alert should be skipped (fit 70 < 80), got %#v", alert)
	}
}

// The stub resolver binds a realistic plan into a valid, runnable definition
// (network-free) so the build->run loop can be wired before the real resolver.
func TestStubWorkflowResolverProducesRunnableDefinition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &ComposioREST{APIKey: "cmp_test", UserID: "najmuzzaman@nex.ai"}

	plan := Plan{
		Name:   "Inbound demo routing",
		ToolID: "inbound-routing",
		Steps: []PlanStep{
			{ID: "t", Kind: "trigger", Title: "Demo booked"},
			{ID: "score", Kind: "ai", Title: "Score the fit"},
			{ID: "alert", Kind: "action", Title: "Post to Slack", Integration: "Slack", Gated: true},
		},
	}
	def, err := BindWorkflowPlan(context.Background(), plan, NewStubWorkflowResolver())
	if err != nil {
		t.Fatalf("bind with stub: %v", err)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("expected trigger dropped, 2 runnable steps, got %d", len(def.Steps))
	}
	// The stub narrates external steps as templates so the dry run stays offline.
	if def.Steps[1].Type != "template" || !strings.Contains(def.Steps[1].Template, "Slack") {
		t.Fatalf("Slack step should narrate via a template, got %#v", def.Steps[1])
	}

	raw, _ := json.Marshal(def)
	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{Key: "stub-run", Definition: raw}); err != nil {
		t.Fatalf("create: %v", err)
	}
	dry := true
	result, err := client.ExecuteWorkflow(context.Background(), WorkflowExecuteRequest{KeyOrPath: "stub-run", DryRun: dry})
	if err != nil {
		t.Fatalf("dry-run execute: %v", err)
	}
	if result.Status != "planned" {
		t.Fatalf("dry run status = %q, want planned", result.Status)
	}
}
