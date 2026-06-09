package team

import (
	"strings"
	"testing"
)

// TestIsPlanningLifecycleState pins the single trigger for native plan mode:
// only LifecycleStatePlanning is a planning turn. Drafting is pre-execution but
// NOT dispatched, so it is not a planning turn.
func TestIsPlanningLifecycleState(t *testing.T) {
	if !isPlanningLifecycleState(LifecycleStatePlanning) {
		t.Fatalf("planning state must be a planning turn")
	}
	for _, s := range []LifecycleState{
		LifecycleStateDrafting, LifecycleStateRunning, LifecycleStateApproved,
		LifecycleStateReview, LifecycleStateReady, LifecycleStateIntake,
		LifecycleStateDecision, LifecycleStateArchived, "",
	} {
		if isPlanningLifecycleState(s) {
			t.Fatalf("%q must not be a planning turn", s)
		}
	}
}

// TestResolvePermissionFlagsPlanPosture is the security-critical assertion: a
// planning turn maps to Claude's native plan mode and NEVER carries
// --dangerously-skip-permissions (which would defeat the read-only gate), while
// every other turn keeps full bypass.
func TestResolvePermissionFlagsPlanPosture(t *testing.T) {
	b := newTestBroker(t)
	seedTaskInState(t, b, "task-plan", LifecycleStatePlanning)
	seedTaskInState(t, b, "task-run", LifecycleStateRunning)
	seedTaskInState(t, b, "task-draft", LifecycleStateDrafting)
	l := &Launcher{broker: b}

	planFlags := l.resolvePermissionFlags(withHeadlessTurnTaskID(t.Context(), "task-plan"), "ceo")
	if planFlags != "--permission-mode plan" {
		t.Fatalf("planning turn flags = %q, want %q", planFlags, "--permission-mode plan")
	}
	if strings.Contains(planFlags, "dangerously-skip-permissions") {
		t.Fatalf("planning turn must not skip permissions, got %q", planFlags)
	}

	for _, tc := range []struct {
		name   string
		taskID string
	}{
		{"running", "task-run"},
		{"drafting", "task-draft"},
		{"no-task", ""},
	} {
		ctx := t.Context()
		if tc.taskID != "" {
			ctx = withHeadlessTurnTaskID(ctx, tc.taskID)
		}
		flags := l.resolvePermissionFlags(ctx, "ceo")
		if !strings.Contains(flags, "bypassPermissions") || !strings.Contains(flags, "dangerously-skip-permissions") {
			t.Fatalf("%s turn flags = %q, want bypass + skip-permissions", tc.name, flags)
		}
	}
}

func TestResolveTurnPostureNilLauncher(t *testing.T) {
	var l *Launcher
	if l.resolveTurnPosture(t.Context(), "ceo") != postureExecute {
		t.Fatal("nil launcher must default to execute posture")
	}
}
