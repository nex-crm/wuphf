package action

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestBindAndRunBrowserStepE2E is the engine e2e for the browser-step foundation
// (slices 1-2): a plan with a no-integration step binds into a real `browser`
// step in the frozen workflow, that step carries its goal, and running it is
// tolerated (a marker, never an error). Actual cua execution is slice 3.
func TestBindAndRunBrowserStepE2E(t *testing.T) {
	plan := Plan{
		Name:   "Vendor Portal Poster",
		ToolID: "app_x",
		Steps: []PlanStep{
			{ID: "t", Kind: "trigger", Title: "On a schedule"},
			{ID: "portal", Kind: "browser", Title: "Submit in the vendor portal", Detail: "Open the vendor portal and submit the refund"},
		},
	}
	// A browser step needs no search/LLM to bind.
	def, err := BindWorkflowPlan(context.Background(), plan, NewComposioActionResolver(nil, nil))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	var browser *workflowStep
	for i := range def.Steps {
		if def.Steps[i].Type == "browser" {
			browser = &def.Steps[i]
			break
		}
	}
	if browser == nil {
		t.Fatal("expected a browser step in the frozen definition")
	}
	if !strings.Contains(browser.Template, "submit the refund") {
		t.Fatalf("browser step lost its goal: %q", browser.Template)
	}
	// A frozen run tolerates the browser step (marker, no error).
	out, err := executeWorkflowBrowserStep(context.Background(), *browser, nil, false)
	if err != nil {
		t.Fatalf("run browser step: %v", err)
	}
	if out["runs_in_browser"] != true || out["goal"] == "" {
		t.Fatalf("browser step run output = %#v", out)
	}
}

func slackCandidates() []Action {
	return []Action{
		{ActionID: "SLACK_SENDS_A_MESSAGE", Title: "Send a message"},
		{ActionID: "SLACK_LIST_CHANNELS", Title: "List channels"},
	}
}

func resolverWith(search ActionSearchFunc, llm LLMCompleteFunc) *ComposioActionResolver {
	return NewComposioActionResolver(search, llm)
}

func TestResolverBindsActionFromCandidate(t *testing.T) {
	search := func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil }
	llm := func(_ context.Context, _, _ string) (string, error) {
		return "```json\n{\"action_id\":\"SLACK_SENDS_A_MESSAGE\",\"params\":{\"channel\":\"#sales\"},\"run_if\":\"steps.score.result.fit >= 80\"}\n```", nil
	}
	r := resolverWith(search, llm)

	step := PlanStep{ID: "alert", Kind: "action", Title: "Post to Slack", Integration: "Slack", Gated: true}
	bound, err := r.Resolve(context.Background(), Plan{Name: "x", Steps: []PlanStep{step}}, step)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bound.Type != "action" || bound.Platform != "slack" || bound.ActionID != "SLACK_SENDS_A_MESSAGE" {
		t.Fatalf("unexpected bound action: %#v", bound)
	}
	if bound.Params["channel"] != "#sales" {
		t.Fatalf("params not mapped: %#v", bound.Params)
	}
	if bound.RunIf != "steps.score.result.fit >= 80" {
		t.Fatalf("run_if not authored: %q", bound.RunIf)
	}
}

func TestResolverBrowserStep(t *testing.T) {
	// A browser step (no integration) binds to a "browser" step carrying its goal
	// — no Composio action, no search/LLM needed.
	r := resolverWith(nil, nil)
	step := PlanStep{
		ID:     "portal",
		Kind:   "browser",
		Title:  "Submit in the vendor portal",
		Detail: "Open the vendor portal and submit the refund",
	}
	bound, err := r.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bound.Type != "browser" {
		t.Fatalf("browser step should bind to type browser, got %#v", bound)
	}
	if bound.Template != "Open the vendor portal and submit the refund" {
		t.Fatalf("browser goal not carried in template: %q", bound.Template)
	}
}

func TestExecuteWorkflowBrowserStepEmitsGoal(t *testing.T) {
	out, err := executeWorkflowBrowserStep(context.Background(), workflowStep{Type: "browser", Template: "do the thing"}, nil, false)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out["type"] != "browser" || out["goal"] != "do the thing" || out["runs_in_browser"] != true {
		t.Fatalf("browser step output = %#v", out)
	}
}

// TestExecuteWorkflowBrowserStepRendersTemplatedGoal proves a browser step's
// templated goal is resolved against the workflow scope (like every other step
// type) BEFORE cua sees it, so a placeholder never reaches the browser raw.
func TestExecuteWorkflowBrowserStepRendersTemplatedGoal(t *testing.T) {
	prev := BrowserStepRunner
	defer func() { BrowserStepRunner = prev }()

	var gotGoal string
	BrowserStepRunner = func(_ context.Context, goal string) (map[string]any, error) {
		gotGoal = goal
		return map[string]any{"result": "ok"}, nil
	}

	scope := map[string]any{"inputs": map[string]any{"vendor": "Acme"}}
	// The step-normalized template form ({{ .inputs.* }}); the raw workflow shorthand
	// {{ inputs.vendor }} is rewritten to this during decode.
	step := workflowStep{Type: "browser", Template: "Submit the refund in the {{ .inputs.vendor }} portal"}
	out, err := executeWorkflowBrowserStep(context.Background(), step, scope, false)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotGoal != "Submit the refund in the Acme portal" {
		t.Fatalf("templated goal not rendered before driving: %q", gotGoal)
	}
	if out["goal"] != "Submit the refund in the Acme portal" {
		t.Fatalf("rendered goal not surfaced in output: %#v", out)
	}

	// Description is the fallback goal and is rendered the same way.
	gotGoal = ""
	descStep := workflowStep{Type: "browser", Description: "Email {{ .inputs.vendor }}"}
	if _, err := executeWorkflowBrowserStep(context.Background(), descStep, scope, false); err != nil {
		t.Fatalf("execute (description fallback): %v", err)
	}
	if gotGoal != "Email Acme" {
		t.Fatalf("description fallback not rendered: %q", gotGoal)
	}
}

func TestExecuteWorkflowBrowserStepDrivesViaRunner(t *testing.T) {
	prev := BrowserStepRunner
	defer func() { BrowserStepRunner = prev }()

	var gotGoal string
	BrowserStepRunner = func(_ context.Context, goal string) (map[string]any, error) {
		gotGoal = goal
		return map[string]any{"actions_count": 2, "result": "did it"}, nil
	}
	// A REAL (non-dry) run drives the runner and merges its outcome.
	out, err := executeWorkflowBrowserStep(context.Background(), workflowStep{Type: "browser", Template: "email the digest"}, nil, false)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotGoal != "email the digest" {
		t.Fatalf("runner got goal %q", gotGoal)
	}
	if out["type"] != "browser" || out["goal"] != "email the digest" || out["result"] != "did it" || out["actions_count"] != 2 {
		t.Fatalf("browser step output = %#v", out)
	}

	// A DRY run NEVER drives the browser (preview only).
	gotGoal = ""
	dryOut, _ := executeWorkflowBrowserStep(context.Background(), workflowStep{Type: "browser", Template: "x"}, nil, true)
	if gotGoal != "" {
		t.Fatal("dry run must not drive the browser")
	}
	if dryOut["dry_run"] != true {
		t.Fatalf("dry marker = %#v", dryOut)
	}
}

func TestResolverRejectsInventedActionAndFallsBack(t *testing.T) {
	search := func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil }
	// The model picks an action id that is NOT a candidate — must not be trusted.
	llm := func(_ context.Context, _, _ string) (string, error) {
		return `{"action_id":"SLACK_DELETE_WORKSPACE","params":{}}`, nil
	}
	r := resolverWith(search, llm)
	step := PlanStep{ID: "a", Kind: "action", Title: "Post", Integration: "Slack"}
	bound, _ := r.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step)
	if bound.Type != "template" {
		t.Fatalf("invented action should fall back to a template, got %#v", bound)
	}
}

func TestResolverDropsMalformedRunIfButKeepsAction(t *testing.T) {
	search := func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil }
	llm := func(_ context.Context, _, _ string) (string, error) {
		return `{"action_id":"SLACK_SENDS_A_MESSAGE","params":{},"run_if":"not a comparison"}`, nil
	}
	r := resolverWith(search, llm)
	step := PlanStep{ID: "a", Kind: "action", Title: "Post", Integration: "Slack"}
	bound, _ := r.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step)
	if bound.Type != "action" || bound.RunIf != "" {
		t.Fatalf("malformed run_if should be dropped, action kept; got %#v", bound)
	}
}

func TestResolverFallsBackWhenNoCandidatesOrSearchFails(t *testing.T) {
	llm := func(_ context.Context, _, _ string) (string, error) { return `{}`, nil }
	step := PlanStep{ID: "a", Kind: "action", Title: "Post", Integration: "Acme"}

	none := resolverWith(func(_ context.Context, _, _ string) ([]Action, error) { return nil, nil }, llm)
	if b, _ := none.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step); b.Type != "template" {
		t.Fatalf("no candidates should fall back to template, got %#v", b)
	}
	boom := resolverWith(func(_ context.Context, _, _ string) ([]Action, error) { return nil, errors.New("down") }, llm)
	if b, _ := boom.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step); b.Type != "template" {
		t.Fatalf("search error should fall back to template, got %#v", b)
	}
}

func TestResolverMapsNonIntegrationSteps(t *testing.T) {
	r := resolverWith(
		func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil },
		func(_ context.Context, _, _ string) (string, error) { return "{}", nil },
	)
	ctx := context.Background()
	plan := Plan{}

	if b, _ := r.Resolve(ctx, plan, PlanStep{ID: "t", Kind: "trigger"}); !b.Skip {
		t.Fatal("trigger should be skipped")
	}
	if b, _ := r.Resolve(ctx, plan, PlanStep{ID: "s", Kind: "ai", Title: "Score the fit"}); b.Type != "nex_ask" || b.QueryTemplate == "" {
		t.Fatalf("ai step should become nex_ask, got %#v", b)
	}
}
