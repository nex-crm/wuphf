package team

import (
	"context"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// A planning task runs the turn in the provider's native read-only/plan mode;
// every other state runs with full bypass. resolveTurnPosture is the single
// trigger and resolvePermissionFlags maps it onto Claude's flags.
func TestResolveTurnPostureAndPermissionFlags(t *testing.T) {
	binding := provider.ProviderBinding{Kind: "claude-code", Model: "claude-sonnet-4-6"}

	planning := launcherWithActiveTask(t, "eng", binding,
		teamTask{ID: "p1", Title: "plan me", LifecycleState: LifecycleStatePlanning})
	if got := planning.resolveTurnPosture(context.Background(), "eng"); got != posturePlan {
		t.Fatalf("planning task posture = %v, want posturePlan", got)
	}
	if got := planning.resolvePermissionFlags(context.Background(), "eng"); got != "--permission-mode plan" {
		t.Fatalf("planning permission flags = %q, want --permission-mode plan", got)
	}

	running := launcherWithActiveTask(t, "eng", binding,
		teamTask{ID: "r1", Title: "run me", LifecycleState: LifecycleStateRunning})
	if got := running.resolveTurnPosture(context.Background(), "eng"); got != postureExecute {
		t.Fatalf("running task posture = %v, want postureExecute", got)
	}
	flags := running.resolvePermissionFlags(context.Background(), "eng")
	if flags == "--permission-mode plan" {
		t.Fatalf("running turn must not use plan mode, got %q", flags)
	}
	// The bypass posture must never carry plan mode (it would defeat read-only).
	if want := "--permission-mode bypassPermissions --dangerously-skip-permissions"; flags != want {
		t.Fatalf("running permission flags = %q, want %q", flags, want)
	}
}

func TestExtractClaudePlanArtifact(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"valid", `{"plan":"step 1\nstep 2"}`, "step 1\nstep 2"},
		{"trimmed", `  {"plan":"  do x  "}  `, "do x"},
		{"empty plan key", `{"plan":""}`, ""},
		{"wrong shape", `{"notplan":"x"}`, ""},
		{"non-json", `not json at all`, ""},
		{"blank", ``, ""},
	}
	for _, tc := range cases {
		if got := extractClaudePlanArtifact(tc.input); got != tc.want {
			t.Errorf("%s: extractClaudePlanArtifact(%q) = %q, want %q", tc.name, tc.input, got, tc.want)
		}
	}
}

func TestIsExitPlanModeTool(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"ExitPlanMode", true},
		{"mcp__wuphf-office__ExitPlanMode", true},
		{"exitplanmode", true},
		{"team_task", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isExitPlanModeTool(tc.name); got != tc.want {
			t.Errorf("isExitPlanModeTool(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
