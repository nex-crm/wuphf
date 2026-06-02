package team

// broker_lifecycle_dispatch_test.go — Phase 4 acceptance gate.
//
// TestBrokerRefusesDispatchForNonExecutableLifecycle is the parametric test
// required by spec section "## Eng review decisions → Tests (load-bearing)":
//
//   Approval gate parametric test as Phase 4 acceptance gate. New file
//   broker_lifecycle_dispatch_test.go: parametric over every dispatch entry
//   point. Negative cases (Drafting, Intake, Review, ChangesRequested) all
//   reject. Positive cases (Running, Approved) allow. Comment path allowed
//   in all states.
//
// The dispatch gate lives in sendTaskUpdate (notifier_delivery.go), the
// single chokepoint all task-bound execution notifications flow through.
// Every task_created / task_updated / task_unblocked action routes to
// sendTaskUpdate, which calls isExecutableTeamTaskStatus before enqueuing
// work. Comments use MutateTask(action="comment") which does NOT call
// sendTaskUpdate — it only appends a FeedbackItem.
//
// Deferred dispatch entry points (not exercised here because they create
// brand-new tasks whose lifecycle state is set by the caller at creation):
//   - TODO(phase4-followup): guard self-heal requestSelfHealingLocked path
//     explicitly — self-heal tasks are created in Running state today
//     (applyLifecycleStateLocked Running at line ~103 of self_healing.go),
//     so they already pass the gate. No change needed in v1.
//   - TODO(phase4-followup): guard headless codex dispatch for tasks created
//     by external_trigger — external triggers create tasks in Ready or Running;
//     the lifecycle gate in sendTaskUpdate covers them.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestIsExecutableTeamTaskStatus validates the gate function itself.
func TestIsExecutableTeamTaskStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state LifecycleState
		want  bool
	}{
		// Executable: dispatch is permitted.
		{LifecycleStateRunning, true},
		{LifecycleStateApproved, true},
		// Non-executable: dispatch MUST be refused.
		{LifecycleStateDrafting, false},
		{LifecycleStateIntake, false},
		{LifecycleStateReady, false},
		{LifecycleStateReview, false},
		{LifecycleStateDecision, false},
		{LifecycleStateChangesRequested, false},
		{LifecycleStateBlockedOnPRMerge, false},
		{LifecycleStateQueuedBehindOwner, false},
		{LifecycleStateRejected, false},
		{LifecycleStateArchived, false},
		{LifecycleStateUnknown, false},
		// Empty string (unset): not executable.
		{"", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.state), func(t *testing.T) {
			t.Parallel()
			got := isExecutableTeamTaskStatus(tc.state)
			if got != tc.want {
				t.Errorf("isExecutableTeamTaskStatus(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// TestBrokerRefusesDispatchForNonExecutableLifecycle is the load-bearing
// Phase 4 acceptance gate. It verifies the gate logic by exercising
// sendTaskUpdate directly and observing whether pane dispatch is called.
//
// NOTE: cannot use t.Parallel() because setLauncherSendNotificationToPaneForTest
// and setHeadlessCodexRunTurnForTest mutate package-level state that background
// goroutines also read. Sequential sub-tests share the package-global safely.
//
// Race-safety: paneCalled and headlessCalled use int32 atomics because
// paneDispatcher.Enqueue spawns a background goroutine to call the pane hook;
// a plain bool would race between the goroutine write and the main-goroutine
// read. The pane dispatch gap is pinned to near-zero so the goroutine fires
// within the poll window below.
func TestBrokerRefusesDispatchForNonExecutableLifecycle(t *testing.T) {
	// Pin pane timing to near-zero so the background dispatch goroutine
	// fires immediately in positive-case sub-tests. Restore on cleanup.
	oldGap := paneDispatchMinGap
	oldWin := paneDispatchCoalesceWindow
	paneDispatchMinGap = 1 * time.Millisecond
	paneDispatchCoalesceWindow = 5 * time.Millisecond
	t.Cleanup(func() {
		paneDispatchMinGap = oldGap
		paneDispatchCoalesceWindow = oldWin
	})

	cases := []struct {
		state          LifecycleState
		wantDispatched bool
	}{
		// Positive cases — dispatch MUST succeed (pane send called).
		{LifecycleStateRunning, true},
		{LifecycleStateApproved, true},
		// Negative cases — dispatch MUST be refused (pane send NOT called).
		{LifecycleStateDrafting, false},
		{LifecycleStateIntake, false},
		{LifecycleStateReady, false},
		{LifecycleStateReview, false},
		{LifecycleStateDecision, false},
		{LifecycleStateChangesRequested, false},
		{LifecycleStateBlockedOnPRMerge, false},
		{LifecycleStateQueuedBehindOwner, false},
		{LifecycleStateRejected, false},
		{LifecycleStateArchived, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("sendTaskUpdate/"+string(tc.state), func(t *testing.T) {
			// Use atomics to avoid races: paneDispatcher.Enqueue fires a
			// background goroutine that writes the hook; a plain bool races
			// with the main-goroutine read in the assertion below.
			var paneCalled int32
			setLauncherSendNotificationToPaneForTest(t, func(_ *Launcher, _, _ string) {
				atomic.StoreInt32(&paneCalled, 1)
			})
			// Intercept headless codex so the gate test doesn't spin up
			// real processes. setHeadlessCodexRunTurnForTest is NOT safe
			// with t.Parallel() — registered here, inherits cleanup.
			var headlessCalled int32
			setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, _ context.Context, _, _ string, _ ...string) error {
				atomic.StoreInt32(&headlessCalled, 1)
				return nil
			})

			b := newTestBroker(t)
			b.mu.Lock()
			b.tasks = append(b.tasks, teamTask{
				ID:      "task-gate-test",
				Channel: "general",
				Title:   "Gate test task",
				Owner:   "eng",
			})
			task := &b.tasks[len(b.tasks)-1]
			if err := b.applyLifecycleStateLocked(task, tc.state); err != nil {
				b.mu.Unlock()
				t.Fatalf("applyLifecycleStateLocked(%s): %v", tc.state, err)
			}
			taskCopy := *task
			b.mu.Unlock()

			l := &Launcher{
				broker:           b,
				provider:         "claude-code",
				paneBackedAgents: true, // force pane path so paneCalled is observable
			}

			action := officeActionLog{
				ID:        "action-1",
				Kind:      "task_updated",
				Actor:     "system",
				Channel:   "general",
				RelatedID: "task-gate-test",
			}
			target := notificationTarget{Slug: "eng", PaneTarget: "wuphf:eng"}

			l.sendTaskUpdate(target, action, taskCopy, "do the work")

			// For positive cases, wait briefly for the background pane goroutine
			// to fire (paneDispatchMinGap is pinned to 1ms above, so this is fast).
			// For negative cases, no wait needed: gate returns before Enqueue is called.
			//
			// Uses a ticker rather than time.Sleep per CONTRIBUTING.md (no
			// time.Sleep in tests). Ticker channel select lets the runtime
			// schedule the background goroutine without busy-spinning.
			if tc.wantDispatched {
				deadline := time.Now().Add(200 * time.Millisecond)
				ticker := time.NewTicker(2 * time.Millisecond)
				for atomic.LoadInt32(&paneCalled) == 0 && atomic.LoadInt32(&headlessCalled) == 0 && time.Now().Before(deadline) {
					<-ticker.C
				}
				ticker.Stop()
			}

			dispatched := atomic.LoadInt32(&paneCalled) == 1 || atomic.LoadInt32(&headlessCalled) == 1
			if dispatched != tc.wantDispatched {
				t.Errorf("state %q: dispatched=%v want=%v", tc.state, dispatched, tc.wantDispatched)
			}
		})
	}
}

// TestCommentPathAllowedInAllLifecycleStates verifies that the comment path
// (MutateTask action="comment") does NOT go through sendTaskUpdate and is
// therefore available in every lifecycle state, including non-executable ones.
func TestCommentPathAllowedInAllLifecycleStates(t *testing.T) {
	t.Parallel()
	states := CanonicalLifecycleStates()
	for _, state := range states {
		state := state
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			b.mu.Lock()
			b.channels = []teamChannel{
				{Slug: "general", Name: "general", Members: []string{"human", "eng"}},
			}
			b.tasks = append(b.tasks, teamTask{
				ID:      "task-comment-state",
				Channel: "general",
				Title:   "Comment path test",
				Owner:   "eng",
			})
			task := &b.tasks[len(b.tasks)-1]
			if err := b.applyLifecycleStateLocked(task, state); err != nil {
				b.mu.Unlock()
				t.Fatalf("applyLifecycleStateLocked(%s): %v", state, err)
			}
			b.mu.Unlock()

			// Comments must succeed (not error out) regardless of lifecycle state.
			// The spec requires: "Comment path allowed in all states" — meaning
			// MutateTask(action="comment") must not return an error for any
			// lifecycle state. We do NOT assert that the lifecycle state is
			// unchanged because MutateTask internally calls reindexTaskLifecycleFromLegacyLocked
			// which re-derives state from legacy fields; that is pre-existing
			// behavior orthogonal to the Phase 4 dispatch gate.
			_, err := b.MutateTask(TaskPostRequest{
				Action:    "comment",
				ID:        "task-comment-state",
				Channel:   "general",
				Details:   "drive-by comment from state " + string(state),
				CreatedBy: "human",
			})
			if err != nil {
				t.Errorf("state %q: MutateTask(comment) failed: %v (comments must succeed in all states)", state, err)
			}
		})
	}
}

// TestErrIssueNotApprovedSentinel validates that ErrIssueNotApproved is
// defined and carries the correct message.
func TestErrIssueNotApprovedSentinel(t *testing.T) {
	t.Parallel()
	if ErrIssueNotApproved == nil {
		t.Fatal("ErrIssueNotApproved must not be nil")
	}
	if !strings.Contains(ErrIssueNotApproved.Error(), "not approved") {
		t.Errorf("ErrIssueNotApproved.Error() = %q, want substring %q", ErrIssueNotApproved.Error(), "not approved")
	}
}
