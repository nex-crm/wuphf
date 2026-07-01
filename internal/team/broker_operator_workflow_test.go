package team

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

func TestOperatorAppWorkflowKey(t *testing.T) {
	if got := operatorAppWorkflowKey("app_5a3379594ca35d86"); got != "operator-app-5a3379594ca35d86" {
		t.Fatalf("key = %q, want operator-app-5a3379594ca35d86", got)
	}
	// Two different app ids never collide on the same key.
	if operatorAppWorkflowKey("app_one") == operatorAppWorkflowKey("app_two") {
		t.Fatal("distinct app ids must produce distinct keys")
	}
}

func TestHumanizeAction(t *testing.T) {
	cases := map[string]string{
		"slack|SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL": "Slack: sends a message to a slack channel",
		"gmail|GMAIL_FETCH_EMAILS":                       "Gmail: fetch emails",
		"|GENERIC_ACTION":                                "Generic action",
	}
	for in, want := range cases {
		platform, actionID, _ := strings.Cut(in, "|")
		if got := humanizeAction(platform, actionID); got != want {
			t.Fatalf("humanizeAction(%q,%q) = %q, want %q", platform, actionID, got, want)
		}
	}
}

// planFromAppCapabilities is deterministic: the same capabilities always produce
// the same ordered plan — trigger, reads, the AI step, then gated writes.
func TestPlanFromAppCapabilities(t *testing.T) {
	app := CustomApp{ID: "app_abc", Name: "Daily Digest"}
	caps := AppCapabilities{
		BridgeAPIs: []string{"getEmails", "ai"},
		Integrations: []AppIntegrationUsage{
			{Platform: "gmail", Actions: []string{"GMAIL_FETCH_EMAILS"}},
			{Platform: "slack", Actions: []string{"SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL"}},
		},
		OfficeWrites: []string{"createTask"},
	}
	plan := planFromAppCapabilities(app, caps)

	if plan.Name != "Daily Digest" || plan.ToolID != "app_abc" {
		t.Fatalf("plan meta = %+v", plan)
	}
	kinds := make([]string, len(plan.Steps))
	gated := make([]bool, len(plan.Steps))
	for i, s := range plan.Steps {
		kinds[i] = s.Kind
		gated[i] = s.Gated
	}
	// trigger, email read, ai, slack send (gated), create task (gated).
	// The gmail fetch is NOT duplicated because getEmails already covered it.
	want := []string{"trigger", "enrich", "ai", "action", "action"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("step %d kind = %q, want %q (all=%v)", i, kinds[i], want[i], kinds)
		}
	}
	if !gated[3] || !gated[4] {
		t.Fatalf("expected the two write steps gated, got %v", gated)
	}
	if gated[0] || gated[1] || gated[2] {
		t.Fatalf("expected trigger/read/ai not gated, got %v", gated)
	}
}

// The authored-plan parser clamps a hallucinated integration to a narration
// step (so a system the app does not use can never reach a run), normalizes
// kinds, and guarantees a leading trigger.
func TestParseAuthoredWorkflowPlan(t *testing.T) {
	app := CustomApp{ID: "app_x", Name: "Refund Approver"}
	caps := AppCapabilities{
		Integrations: []AppIntegrationUsage{{Platform: "slack"}},
	}
	raw := "```json\n" + `{"steps":[
      {"id":"t","kind":"trigger","title":"On a schedule"},
      {"id":"read","kind":"fetch","title":"Read refund requests","integration":"gmail"},
      {"id":"post","kind":"send","title":"Post to Slack","integration":"slack","gated":true}
    ]}` + "\n```"
	plan, ok := parseAuthoredWorkflowPlan(raw, app, caps)
	if !ok {
		t.Fatal("expected a valid authored plan")
	}
	if plan.Name != "Refund Approver" || plan.ToolID != "app_x" {
		t.Fatalf("plan meta = %+v", plan)
	}
	if plan.Steps[0].Kind != "trigger" {
		t.Fatalf("first step must be trigger, got %q", plan.Steps[0].Kind)
	}
	// "fetch" normalizes to enrich; gmail is NOT in caps, so it is clamped away.
	if plan.Steps[1].Kind != "enrich" || plan.Steps[1].Integration != "" {
		t.Fatalf("read step = %+v (gmail should be clamped, fetch->enrich)", plan.Steps[1])
	}
	// "send" normalizes to action; slack IS in caps, so it stays gated.
	if plan.Steps[2].Kind != "action" || plan.Steps[2].Integration != "slack" || !plan.Steps[2].Gated {
		t.Fatalf("post step = %+v", plan.Steps[2])
	}
}

func TestParseAuthoredWorkflowPlanBrowserStep(t *testing.T) {
	app := CustomApp{ID: "app_b", Name: "Portal Poster"}
	caps := AppCapabilities{Integrations: []AppIntegrationUsage{{Platform: "slack"}}}
	// A send step to a system the app has NO integration for → browser step
	// ("no integration available → browser step"). An explicit kind:"browser"
	// is kept as-is.
	raw := `{"steps":[
	  {"id":"t","kind":"trigger","title":"On a schedule"},
	  {"id":"post","kind":"action","title":"Submit in the vendor portal","detail":"Open the vendor portal and submit the refund","integration":"vendorportal","gated":true},
	  {"id":"nav","kind":"browser","title":"Update the tracker","detail":"Mark it done in the web tracker"}
	]}`
	plan, ok := parseAuthoredWorkflowPlan(raw, app, caps)
	if !ok {
		t.Fatal("expected a valid plan")
	}
	// Unavailable integration on a gated send → browser, integration cleared,
	// gated preserved, goal kept in detail.
	post := plan.Steps[1]
	if post.Kind != "browser" || post.Integration != "" || !post.Gated {
		t.Fatalf("post step should be a gated browser step, got %+v", post)
	}
	if post.Detail != "Open the vendor portal and submit the refund" {
		t.Fatalf("browser step must preserve the authored goal, got %q", post.Detail)
	}
	// An explicitly-authored browser step is preserved.
	if plan.Steps[2].Kind != "browser" {
		t.Fatalf("explicit browser step = %+v", plan.Steps[2])
	}
}

func TestParseAuthoredWorkflowPlanRejectsJunk(t *testing.T) {
	if _, ok := parseAuthoredWorkflowPlan("not json at all", CustomApp{}, AppCapabilities{}); ok {
		t.Fatal("expected non-JSON to be rejected")
	}
	if _, ok := parseAuthoredWorkflowPlan(`{"steps":[]}`, CustomApp{}, AppCapabilities{}); ok {
		t.Fatal("expected an empty step list to be rejected")
	}
}

func TestPlanFromAppCapabilitiesEmpty(t *testing.T) {
	plan := planFromAppCapabilities(CustomApp{ID: "app_x", Name: "Static"}, AppCapabilities{})
	if len(plan.Steps) != 1 || plan.Steps[0].Kind != "trigger" {
		t.Fatalf("expected only a trigger step, got %+v", plan.Steps)
	}
}

// An app that reads and writes nothing has no workflow to compile, so the
// compile endpoint refuses with a clear 400 rather than binding an empty plan.
// An HTML-only app (no source) introspects to empty capabilities.
func TestOperatorAppWorkflowCompileRejectsEmptyApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("COMPOSIO_API_KEY", "test-key")
	t.Setenv("COMPOSIO_USER_ID", "tester@example.com")

	b := newTestBroker(t)
	app, err := b.appStore().Save(CustomAppWriteRequest{
		Name:  "Static Page",
		HTML:  "<html><body>static</body></html>",
		Actor: "app-builder",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/operator/apps/"+app.ID+"/workflow/compile", nil)
	rec := httptest.NewRecorder()
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an app with no workflow, got %d: %s", rec.Code, rec.Body.String())
	}
}

// End-to-end determinism through the real engine + HTTP handlers: a frozen plan
// reads back identically (GET), and running it twice executes the SAME saved
// definition each time — the compile-and-freeze promise. Binding uses the stub
// resolver, so this is network-free.
func TestOperatorAppWorkflowGetRunDeterministic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("COMPOSIO_API_KEY", "test-key")
	t.Setenv("COMPOSIO_USER_ID", "tester@example.com")

	b := newTestBroker(t)
	appID := "app_digest123"
	caps := AppCapabilities{
		BridgeAPIs:   []string{"getEmails", "ai"},
		Integrations: []AppIntegrationUsage{{Platform: "slack", Actions: []string{"SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL"}}},
	}
	plan := planFromAppCapabilities(CustomApp{ID: appID, Name: "Digest"}, caps)

	// Freeze the plan exactly as the compile handler would (bind once + persist).
	prov := &action.ComposioREST{APIKey: "test-key", UserID: "tester@example.com"}
	def, err := action.BindWorkflowPlan(context.Background(), plan, action.NewStubWorkflowResolver())
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	raw, _ := json.Marshal(def)
	key := operatorAppWorkflowKey(appID)
	if _, err := prov.CreateWorkflow(context.Background(), action.WorkflowCreateRequest{Key: key, Definition: raw}); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	// GET returns the frozen steps.
	getReq := httptest.NewRequest(http.MethodGet, "/operator/apps/"+appID+"/workflow", nil)
	getRec := httptest.NewRecorder()
	b.handleOperatorAppWorkflow(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: status %d: %s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		Compiled bool                      `json:"compiled"`
		Steps    []action.WorkflowStepView `json:"steps"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("get decode: %v", err)
	}
	if !got.Compiled || len(got.Steps) == 0 {
		t.Fatalf("expected frozen steps, got %+v", got)
	}

	// Run twice — both load and execute the same saved definition.
	run := func() []string {
		body, _ := json.Marshal(map[string]any{"dry_run": true})
		req := httptest.NewRequest(http.MethodPost, "/operator/apps/"+appID+"/workflow/run", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		b.handleOperatorAppWorkflow(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("run: status %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			OK     bool                       `json:"ok"`
			DryRun bool                       `json:"dry_run"`
			Status string                     `json:"status"`
			Steps  map[string]json.RawMessage `json:"steps"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("run decode: %v", err)
		}
		if !resp.OK || !resp.DryRun || resp.Status != "planned" {
			t.Fatalf("unexpected run response: %+v", resp)
		}
		ids := make([]string, 0, len(resp.Steps))
		for id := range resp.Steps {
			ids = append(ids, id)
		}
		return ids
	}
	first := run()
	second := run()
	if len(first) != len(second) || len(first) == 0 {
		t.Fatalf("runs produced different step counts: %v vs %v", first, second)
	}
}

// Running before compiling is a clear 409, not an opaque load failure.
func TestOperatorAppWorkflowRunBeforeCompile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("COMPOSIO_API_KEY", "test-key")
	t.Setenv("COMPOSIO_USER_ID", "tester@example.com")

	b := newTestBroker(t)
	req := httptest.NewRequest(http.MethodPost, "/operator/apps/app_never/workflow/run", nil)
	rec := httptest.NewRecorder()
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 before compile, got %d: %s", rec.Code, rec.Body.String())
	}
}
