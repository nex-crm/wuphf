package workflow

import (
	"encoding/json"
	"fmt"
	"testing"
)

// --- Test helpers ---

func mustParseSpec(t *testing.T, raw string) WorkflowSpec {
	t.Helper()
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatalf("failed to parse spec: %v", err)
	}
	return spec
}

func mustNewRuntime(t *testing.T, raw string, opts ...RuntimeOption) *Runtime {
	t.Helper()
	spec := mustParseSpec(t, raw)
	rt, err := NewRuntime(spec, opts...)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// --- Tests ---

func TestNewRuntime_ValidSpec(t *testing.T) {
	rt, err := NewRuntime(mustParseSpec(t, deployCheckSpec))
	if err != nil {
		t.Fatalf("expected valid spec to succeed: %v", err)
	}
	if rt.State() != StatePending {
		t.Errorf("expected pending state, got %s", rt.State())
	}
	if rt.CurrentStep() != nil {
		t.Error("expected no current step before Start")
	}
}

func TestNewRuntime_InvalidSpec(t *testing.T) {
	_, err := NewRuntime(WorkflowSpec{})
	if err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestRuntime_Start(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)

	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rt.State() != StateAwaitingInput {
		t.Errorf("expected awaiting_input after start (first step is select), got %s", rt.State())
	}
	step := rt.CurrentStep()
	if step == nil {
		t.Fatal("expected current step after Start")
	}
	if step.ID != "list" {
		t.Errorf("expected first step 'list', got %q", step.ID)
	}

	// Cannot start twice.
	if err := rt.Start(); err == nil {
		t.Error("expected error on double Start")
	}
}

func TestRuntime_Start_RunStep(t *testing.T) {
	// deploy-check starts with a "run" step, so state should be active (not awaiting_input).
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rt.State() != StateActive {
		t.Errorf("expected active for run step, got %s", rt.State())
	}
	step := rt.CurrentStep()
	if step == nil || step.ID != "fetch-status" {
		t.Errorf("expected first step 'fetch-status', got %v", step)
	}
}

func TestRuntime_HandleAction_ValidKey(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Press "a" (Approve) → should transition to "triage".
	transition, execute, err := rt.HandleAction("a")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if transition != "triage" {
		t.Errorf("expected transition 'triage', got %q", transition)
	}
	if execute != nil {
		t.Error("expected no execute for approve action")
	}

	// Should now be on the triage step (run type → active).
	step := rt.CurrentStep()
	if step == nil || step.ID != "triage" {
		t.Errorf("expected current step 'triage', got %v", step)
	}
	if rt.State() != StateActive {
		t.Errorf("expected active state for run step, got %s", rt.State())
	}
}

func TestRuntime_HandleAction_WithExecute(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Navigate to confirm-send step to test an action with execute.
	// list → triage → confirm-send
	if _, _, err := rt.HandleAction("a"); err != nil {
		t.Fatalf("HandleAction(a): %v", err)
	}
	// triage is a run step, simulate entering it via HandleAction
	if _, _, err := rt.HandleAction("Enter"); err != nil {
		t.Fatalf("HandleAction(Enter): %v", err)
	}
	// Now on confirm-send (confirm type → awaiting_input).
	if rt.State() != StateAwaitingInput {
		t.Fatalf("expected awaiting_input on confirm-send, got %s", rt.State())
	}

	// Press "y" (Yes, send) → has execute spec.
	transition, execute, err := rt.HandleAction("y")
	if err != nil {
		t.Fatalf("HandleAction(y): %v", err)
	}
	if transition != "list" {
		t.Errorf("expected transition 'list', got %q", transition)
	}
	if execute == nil {
		t.Fatal("expected execute spec for 'Yes, send' action")
	}
	if execute.Provider != ProviderComposio {
		t.Errorf("expected composio provider, got %q", execute.Provider)
	}
	if execute.Action != "GMAIL_SEND_EMAIL" {
		t.Errorf("expected GMAIL_SEND_EMAIL action, got %q", execute.Action)
	}
	if rt.State() != StateExecutingAction {
		t.Errorf("expected executing_action state, got %s", rt.State())
	}
}

func TestRuntime_HandleAction_InvalidKey(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, _, err := rt.HandleAction("z")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestRuntime_HandleAction_WrongState(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	// Not started yet — should fail.
	_, _, err := rt.HandleAction("a")
	if err == nil {
		t.Fatal("expected error for action in pending state")
	}
}

func TestRuntime_Transition(t *testing.T) {
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Manually transition from fetch-status → review.
	if err := rt.Transition("review"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	step := rt.CurrentStep()
	if step == nil || step.ID != "review" {
		t.Errorf("expected step 'review', got %v", step)
	}
	// review is a confirm step → awaiting_input.
	if rt.State() != StateAwaitingInput {
		t.Errorf("expected awaiting_input, got %s", rt.State())
	}
}

func TestRuntime_Transition_InvalidTarget(t *testing.T) {
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := rt.Transition("nonexistent")
	if err == nil {
		t.Fatal("expected error for invalid transition target")
	}
}

func TestRuntime_Transition_ToDone(t *testing.T) {
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := rt.Transition(TransitionDone); err != nil {
		t.Fatalf("Transition to done: %v", err)
	}
	if rt.State() != StateDone {
		t.Errorf("expected done state, got %s", rt.State())
	}
}

func TestRuntime_Transition_WhenDone(t *testing.T) {
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Transition(TransitionDone); err != nil {
		t.Fatalf("Transition to done: %v", err)
	}

	err := rt.Transition("review")
	if err == nil {
		t.Fatal("expected error when transitioning from done")
	}
}

func TestRuntime_CompleteAction_Success(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Navigate to confirm-send and trigger "y" (execute action).
	rt.HandleAction("a")     // list → triage
	rt.HandleAction("Enter") // triage → confirm-send
	rt.HandleAction("y")     // confirm-send → executing_action

	if rt.State() != StateExecutingAction {
		t.Fatalf("expected executing_action, got %s", rt.State())
	}

	// Complete successfully.
	result := map[string]any{"messageId": "abc123"}
	if err := rt.CompleteAction(result, nil); err != nil {
		t.Fatalf("CompleteAction: %v", err)
	}

	// Should have transitioned to "list" (the action's transition target).
	step := rt.CurrentStep()
	if step == nil || step.ID != "list" {
		t.Errorf("expected step 'list' after complete, got %v", step)
	}

	// Result should be in data store.
	ds := rt.DataStore()
	if ds["messageId"] != "abc123" {
		t.Errorf("expected messageId in data store, got %v", ds["messageId"])
	}
}

func TestRuntime_CompleteAction_Error_Retry(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec, WithMaxRetries(2))
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rt.HandleAction("a")
	rt.HandleAction("Enter")
	rt.HandleAction("y")

	// First failure — retry 1 of 2, should stay in executing_action.
	err := rt.CompleteAction(nil, fmt.Errorf("network error"))
	if err == nil {
		t.Fatal("expected error on failure")
	}
	if rt.State() != StateExecutingAction {
		t.Errorf("expected executing_action after first retry, got %s", rt.State())
	}

	// Second failure — retry 2 of 2, still in executing_action.
	err = rt.CompleteAction(nil, fmt.Errorf("timeout"))
	if err == nil {
		t.Fatal("expected error on second failure")
	}
	if rt.State() != StateExecutingAction {
		t.Errorf("expected executing_action after second retry, got %s", rt.State())
	}

	// Third failure — retries exhausted, should enter error state.
	err = rt.CompleteAction(nil, fmt.Errorf("still broken"))
	if err == nil {
		t.Fatal("expected error on third failure")
	}
	if rt.State() != StateError {
		t.Errorf("expected error state after exhausting retries, got %s", rt.State())
	}
}

func TestRuntime_CompleteAction_Error_Exhausted(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec, WithMaxRetries(1))
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rt.HandleAction("a")
	rt.HandleAction("Enter")
	rt.HandleAction("y")

	// Single failure with maxRetries=1 — first attempt bumps count to 1,
	// which equals maxRetries, so it stays retryable one more time... no:
	// retryCount=1 <= maxRetries=1 means one retry allowed.
	err := rt.CompleteAction(nil, fmt.Errorf("fail"))
	if err == nil {
		t.Fatal("expected error")
	}
	// retryCount is 1, maxRetries is 1 → stays in executing_action for one retry.
	if rt.State() != StateExecutingAction {
		t.Fatalf("expected executing_action for retry, got %s", rt.State())
	}

	// Second failure — retryCount becomes 2 > maxRetries=1 → error state.
	err = rt.CompleteAction(nil, fmt.Errorf("fail again"))
	if err == nil {
		t.Fatal("expected error")
	}
	if rt.State() != StateError {
		t.Errorf("expected error state, got %s", rt.State())
	}
	if rt.LastError() == nil {
		t.Error("expected LastError to be set")
	}
}

func TestRuntime_CompleteAction_WrongState(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Not in executing_action state.
	err := rt.CompleteAction(nil, nil)
	if err == nil {
		t.Fatal("expected error when not in executing_action state")
	}
}

func TestRuntime_Abort(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := rt.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if rt.State() != StateAborted {
		t.Errorf("expected aborted state, got %s", rt.State())
	}

	// Cannot abort twice.
	if err := rt.Abort(); err == nil {
		t.Error("expected error on double abort")
	}
}

func TestRuntime_Abort_WhenDone(t *testing.T) {
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rt.Transition(TransitionDone)

	if err := rt.Abort(); err == nil {
		t.Fatal("expected error when aborting done workflow")
	}
}

func TestRuntime_StepHistory(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Empty history before any actions.
	if len(rt.StepHistory()) != 0 {
		t.Errorf("expected empty history, got %d events", len(rt.StepHistory()))
	}

	// Press "a" (no execute) → should record an event.
	rt.HandleAction("a")
	history := rt.StepHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 event, got %d", len(history))
	}
	if history[0].StepID != "list" {
		t.Errorf("expected step 'list', got %q", history[0].StepID)
	}
	if history[0].Action != "a" {
		t.Errorf("expected action 'a', got %q", history[0].Action)
	}

	// Press Enter on triage step → another event.
	rt.HandleAction("Enter")
	history = rt.StepHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 events, got %d", len(history))
	}
	if history[1].StepID != "triage" {
		t.Errorf("expected step 'triage', got %q", history[1].StepID)
	}
}

func TestRuntime_NeedsAbortConfirmation(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// No history yet — no confirmation needed.
	if rt.NeedsAbortConfirmation() {
		t.Error("expected no confirmation needed before any actions")
	}

	rt.HandleAction("a")

	// History exists — confirmation needed.
	if !rt.NeedsAbortConfirmation() {
		t.Error("expected confirmation needed after action")
	}
}

func TestRuntime_Snapshot(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rt.HandleAction("a")

	snap := rt.Snapshot()
	if snap.WorkflowID != "email-triage" {
		t.Errorf("expected workflow ID 'email-triage', got %q", snap.WorkflowID)
	}
	if snap.CurrentStepID != "triage" {
		t.Errorf("expected current step 'triage', got %q", snap.CurrentStepID)
	}
	if snap.State != StateActive {
		t.Errorf("expected active state, got %s", snap.State)
	}
	if len(snap.StepHistory) != 1 {
		t.Errorf("expected 1 history event, got %d", len(snap.StepHistory))
	}
	if snap.SavedAt.IsZero() {
		t.Error("expected SavedAt to be set")
	}
}

func TestRuntime_WithMaxRetries(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec, WithMaxRetries(5))
	if rt.maxRetries != 5 {
		t.Errorf("expected maxRetries 5, got %d", rt.maxRetries)
	}
}

func TestRuntime_DataStore_IsCopy(t *testing.T) {
	rt := mustNewRuntime(t, emailTriageSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ds := rt.DataStore()
	ds["injected"] = "hacker"

	// Mutation should not affect the runtime's data store.
	ds2 := rt.DataStore()
	if _, found := ds2["injected"]; found {
		t.Error("expected DataStore to return a copy, not a reference")
	}
}

func TestRuntime_HandleAction_TransitionDone(t *testing.T) {
	// Use deploy-check: review step has "a" → Abort → transition "done".
	rt := mustNewRuntime(t, deployCheckSpec)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Transition to review step.
	if err := rt.Transition("review"); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	transition, _, err := rt.HandleAction("a")
	if err != nil {
		t.Fatalf("HandleAction(a): %v", err)
	}
	if transition != TransitionDone {
		t.Errorf("expected transition 'done', got %q", transition)
	}
	if rt.State() != StateDone {
		t.Errorf("expected done state, got %s", rt.State())
	}
}

// --- JSON Pointer helper tests ---

func TestSetPointerPath(t *testing.T) {
	data := map[string]any{}
	setPointerPath(data, "/foo/bar", "hello")
	if resolvePointerPath(data, "/foo/bar") != "hello" {
		t.Errorf("expected 'hello', got %v", resolvePointerPath(data, "/foo/bar"))
	}
}

func TestResolvePointerPath_Empty(t *testing.T) {
	data := map[string]any{"x": 1}
	result := resolvePointerPath(data, "")
	if result == nil {
		t.Error("expected data back for empty pointer")
	}
}

func TestResolvePointerPath_Missing(t *testing.T) {
	data := map[string]any{"x": 1}
	result := resolvePointerPath(data, "/y")
	if result != nil {
		t.Errorf("expected nil for missing key, got %v", result)
	}
}
