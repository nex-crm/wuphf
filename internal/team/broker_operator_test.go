package team

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The operator build->run loop: a semantic plan posts to /operator/run-plan,
// gets bound (stub resolver) and dry-run through the Composio executor, and
// comes back "planned". Network-free: the stub binds steps as templates.
func TestHandleOperatorRunPlanDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("COMPOSIO_API_KEY", "test-key")
	t.Setenv("COMPOSIO_USER_ID", "tester@example.com")

	b := newTestBroker(t)

	body := map[string]any{
		"schema_version": 1,
		"plan": map[string]any{
			"name":    "Inbound demo routing",
			"tool_id": "inbound-routing",
			"steps": []map[string]any{
				{"id": "t", "kind": "trigger", "title": "Demo booked"},
				{"id": "score", "kind": "ai", "title": "Score the fit"},
				{"id": "alert", "kind": "action", "title": "Post to Slack", "integration": "Slack", "gated": true},
			},
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/operator/run-plan", bytes.NewReader(raw))
	rec := httptest.NewRecorder()

	b.handleOperatorRunPlan(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK          bool   `json:"ok"`
		WorkflowKey string `json:"workflow_key"`
		DryRun      bool   `json:"dry_run"`
		Status      string `json:"status"`
		RunID       string `json:"run_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK || !resp.DryRun || resp.Status != "planned" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.WorkflowKey != "operator-inbound-routing" {
		t.Fatalf("workflow key = %q, want operator-inbound-routing", resp.WorkflowKey)
	}
	if resp.RunID == "" {
		t.Fatal("expected a run id")
	}
}

func TestHandleOperatorRunPlanRejectsEmpty(t *testing.T) {
	b := newTestBroker(t)
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "plan": map[string]any{"name": "x"}})
	req := httptest.NewRequest(http.MethodPost, "/operator/run-plan", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	b.handleOperatorRunPlan(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty plan, got %d", rec.Code)
	}
}
