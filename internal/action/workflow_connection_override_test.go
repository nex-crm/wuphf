package action

import "testing"

func TestWorkflowConnectionOverride(t *testing.T) {
	scope := map[string]any{
		"inputs": map[string]any{
			"connections": map[string]any{
				"gmail": "conn_work",
				"SLACK": "conn_team",
			},
		},
	}
	if got := workflowConnectionOverride(scope, "gmail"); got != "conn_work" {
		t.Fatalf("gmail override = %q, want conn_work", got)
	}
	// Platform match is normalized, so "slack" finds the "SLACK" entry.
	if got := workflowConnectionOverride(scope, "slack"); got != "conn_team" {
		t.Fatalf("slack override = %q, want conn_team", got)
	}
	// A platform with no choice falls back (empty) to auto-resolution.
	if got := workflowConnectionOverride(scope, "github"); got != "" {
		t.Fatalf("github override = %q, want empty", got)
	}
	// No connections input at all → empty (legacy behavior preserved).
	if got := workflowConnectionOverride(map[string]any{}, "gmail"); got != "" {
		t.Fatalf("missing inputs override = %q, want empty", got)
	}
}
