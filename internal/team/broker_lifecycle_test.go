package team

// broker_lifecycle_test.go covers build-time gate #3 (forward map) of the
// Lane A success criteria: sweep all eight canonical LifecycleState values
// and assert the derived (pipelineStage, reviewState, status, blocked)
// tuple matches the lifecycleDerivedFields table for each one.
//
// This test guards against silent drift between the documented forward
// map (in the design doc) and the implementation (in
// broker_lifecycle_transition.go). If a contributor edits one but not
// the other, this test fails loudly before the change can ship.

import (
	"testing"
)

func TestLifecycleForwardMapAllStates(t *testing.T) {
	// Acceptance: every canonical LifecycleState produces a deterministic,
	// documented (pipelineStage, reviewState, status, blocked) tuple when
	// applied to a freshly created task. Anything not in the table is a
	// failure surface; the migration shim is tested separately.
	cases := []struct {
		state         LifecycleState
		pipelineStage string
		reviewState   string
		status        string
		blocked       bool
	}{
		{LifecycleStateIntake, "triage", "pending_review", "open", false},
		{LifecycleStateReady, "triage", "pending_review", "open", false},
		{LifecycleStateRunning, "implement", "pending_review", "in_progress", false},
		{LifecycleStateReview, "review", "ready_for_review", "in_progress", false},
		{LifecycleStateDecision, "review", "ready_for_review", "in_progress", false},
		// Documented deviation from the design doc: status="blocked"
		// instead of "in_progress" to preserve the pre-Lane-A contract
		// that ~10 broker code paths read. See lifecycleDerivedFields
		// comment for full rationale.
		{LifecycleStateBlockedOnPRMerge, "review", "ready_for_review", "blocked", true},
		{LifecycleStateChangesRequested, "implement", "pending_review", "in_progress", false},
		{LifecycleStateMerged, "ship", "approved", "done", false},
	}

	// The canonical state list and the forward-map must agree on which
	// states exist; if the forward-map grows we want the test sweep to
	// surface the new state immediately.
	if got, want := len(CanonicalLifecycleStates()), len(cases); got != want {
		t.Fatalf("canonical state count: got %d, want %d (test cases out of sync with implementation)", got, want)
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.state), func(t *testing.T) {
			b := newTestBroker(t)
			b.mu.Lock()
			b.tasks = []teamTask{{ID: "task-fwd", LifecycleState: LifecycleStateIntake}}
			task, err := b.transitionLifecycleLocked("task-fwd", tc.state, "test forward map")
			b.mu.Unlock()
			if err != nil {
				t.Fatalf("transitionLifecycleLocked(%s): %v", tc.state, err)
			}
			if task.LifecycleState != tc.state {
				t.Fatalf("LifecycleState: got %q, want %q", task.LifecycleState, tc.state)
			}
			if task.pipelineStage != tc.pipelineStage {
				t.Fatalf("pipelineStage: got %q, want %q", task.pipelineStage, tc.pipelineStage)
			}
			if task.reviewState != tc.reviewState {
				t.Fatalf("reviewState: got %q, want %q", task.reviewState, tc.reviewState)
			}
			if task.status != tc.status {
				t.Fatalf("status: got %q, want %q", task.status, tc.status)
			}
			if task.blocked != tc.blocked {
				t.Fatalf("blocked: got %v, want %v", task.blocked, tc.blocked)
			}
		})
	}
}

func TestLifecycleTransitionRejectsNonCanonicalState(t *testing.T) {
	// Acceptance: passing a non-canonical state (e.g. "garbage" or the
	// migration fallback "unknown") to the transition layer must error
	// instead of silently writing junk into the inbox index. This is the
	// build-time guarantee that no agent or future event handler can
	// stamp a task into a state that has no forward-map row.
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{{ID: "task-bad", LifecycleState: LifecycleStateRunning}}
	_, err := b.transitionLifecycleLocked("task-bad", LifecycleState("garbage"), "")
	b.mu.Unlock()
	if err == nil {
		t.Fatal("expected error for non-canonical state, got nil")
	}

	b.mu.Lock()
	_, err = b.transitionLifecycleLocked("task-bad", LifecycleStateUnknown, "")
	b.mu.Unlock()
	if err == nil {
		t.Fatal("expected error for LifecycleStateUnknown (it is the migration fallback, not a valid target), got nil")
	}
}
