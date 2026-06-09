package team

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExtractClaudePlanArtifact pins the ExitPlanMode harvest: the plan text is
// pulled from the {"plan": ...} tool input, and non-plan / malformed inputs
// yield "".
func TestExtractClaudePlanArtifact(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plan field", `{"plan":"1. read code\n2. ship"}`, "1. read code\n2. ship"},
		{"trims whitespace", `{"plan":"  do the thing  "}`, "do the thing"},
		{"empty plan", `{"plan":""}`, ""},
		{"no plan key", `{"other":"x"}`, ""},
		{"not json", `ExitPlanMode`, ""},
		{"empty", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractClaudePlanArtifact(tc.input); got != tc.want {
				t.Fatalf("extractClaudePlanArtifact(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestIsExitPlanModeTool tolerates the bare name and MCP-prefixed variants.
func TestIsExitPlanModeTool(t *testing.T) {
	for _, name := range []string{"ExitPlanMode", "exitplanmode", "mcp__x__ExitPlanMode"} {
		if !isExitPlanModeTool(name) {
			t.Fatalf("%q should be ExitPlanMode", name)
		}
	}
	for _, name := range []string{"", "Edit", "team_task", "Plan"} {
		if isExitPlanModeTool(name) {
			t.Fatalf("%q should NOT be ExitPlanMode", name)
		}
	}
}

// TestEmitHeadlessPlanWireShape pins the plan-card wire shape and that empty
// plans are dropped.
func TestEmitHeadlessPlanWireShape(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}

	emitHeadlessPlan(stream, "turn-9", HeadlessProviderClaude, "ceo", "task-3", "   ")
	if got := stream.recentTask("task-3"); len(got) != 0 {
		t.Fatalf("empty plan must be dropped, got %d", len(got))
	}

	emitHeadlessPlan(stream, "turn-9", HeadlessProviderClaude, "ceo", "task-3", "Goal: X\nSteps: 1,2,3")
	lines := stream.recentTask("task-3")
	if len(lines) != 1 {
		t.Fatalf("expected 1 plan event, got %d: %v", len(lines), lines)
	}
	var event HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if event.Type != HeadlessEventTypePlan {
		t.Fatalf("type: want plan, got %q", event.Type)
	}
	if event.TurnID != "turn-9" || event.TaskID != "task-3" || event.Agent != "ceo" {
		t.Fatalf("correlation fields wrong: %+v", event)
	}
	if !strings.Contains(event.Text, "Goal: X") {
		t.Fatalf("plan text missing: %+v", event)
	}
}
