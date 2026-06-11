package team

// broker_lifecycle_test.go covers build-time gate #3 (forward map) of the
// Lane A success criteria: sweep all canonical LifecycleState values
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
		// Phase 3 — Drafting: pre-Intake mode where agents comment but cannot
		// dispatch. PipelineStage="draft" matches the spec's draft phase name.
		// Status="open" keeps it visible in the open-tasks view. Blocked=false.
		{LifecycleStateDrafting, "draft", "pending_review", "open", false},
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
		{LifecycleStateQueuedBehindOwner, "triage", "pending_review", "open", true},
		{LifecycleStateChangesRequested, "implement", "pending_review", "in_progress", false},
		{LifecycleStateApproved, "ship", "approved", "done", false},
		// Rejected: terminal, blocked=true so unblockDependentsLocked
		// treats the upstream as unresolved; reviewState="rejected"
		// is the durable filter signal in the inbox.
		{LifecycleStateRejected, "review", "rejected", "rejected", true},
		// Archived: terminal, off-board. Blocked=false (not waiting on
		// anything), ReviewState="approved" (clean terminal).
		{LifecycleStateArchived, "archived", "approved", "archived", false},
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

// ── N5: "waiting on you" notice on entering drafting ─────────────────────

// countAwaitingStartNotices returns how many pending "waiting on you"
// notices exist for the given task. Helper for the tests below.
func countAwaitingStartNotices(b *Broker, taskID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for i := range b.requests {
		if b.requests[i].IssueID == taskID && b.requests[i].Kind == "notice" &&
			b.requests[i].Title == awaitingStartNoticeTitle(taskID) {
			n++
		}
	}
	return n
}

// A task entering drafting with an owner can only move forward via the
// human's Approve & Start. The broker must tell the human — once — via a
// non-blocking Inbox notice (ICP eval N5: three silent 10–12 minute stalls).
func TestDraftingEntryRaisesWaitingOnYouNotice(t *testing.T) {
	b := newTestBroker(t)

	b.mu.Lock()
	b.tasks = []teamTask{{ID: "OFFICE-7", TaskType: "issue", Owner: "ceo", Channel: "general"}}
	_, err := b.transitionLifecycleLocked("OFFICE-7", LifecycleStateDrafting, "composer create")
	b.mu.Unlock()
	if err != nil {
		t.Fatalf("transition to drafting: %v", err)
	}

	if got := countAwaitingStartNotices(b, "OFFICE-7"); got != 1 {
		t.Fatalf("expected exactly 1 waiting notice, got %d", got)
	}

	b.mu.Lock()
	var notice *humanInterview
	for i := range b.requests {
		if b.requests[i].IssueID == "OFFICE-7" && b.requests[i].Kind == "notice" {
			notice = &b.requests[i]
			break
		}
	}
	b.mu.Unlock()
	if notice == nil {
		t.Fatal("waiting notice not found")
	}
	if notice.Blocking || notice.Required {
		t.Fatalf("waiting notice must be non-blocking and not required, got blocking=%v required=%v",
			notice.Blocking, notice.Required)
	}
	wantQuestion := "Waiting on you — OFFICE-7 starts when you press Approve & Start."
	if notice.Question != wantQuestion {
		t.Fatalf("question: got %q, want %q", notice.Question, wantQuestion)
	}
	if notice.Status != "pending" {
		t.Fatalf("status: got %q, want pending", notice.Status)
	}
}

// Re-entering drafting (reopen, replayed transition) must not raise a
// second notice for the same task — dedupe is per task, forever. And
// leaving drafting (the human pressed Approve & Start) self-resolves the
// pending notice so it never lingers asking for an acknowledgement.
func TestDraftingReentryDoesNotDuplicateWaitingNotice(t *testing.T) {
	b := newTestBroker(t)

	b.mu.Lock()
	b.tasks = []teamTask{{ID: "OFFICE-9", TaskType: "issue", Owner: "ae", Channel: "general"}}
	if _, err := b.transitionLifecycleLocked("OFFICE-9", LifecycleStateDrafting, "create"); err != nil {
		b.mu.Unlock()
		t.Fatalf("first transition: %v", err)
	}
	if _, err := b.transitionLifecycleLocked("OFFICE-9", LifecycleStateRunning, "approve"); err != nil {
		b.mu.Unlock()
		t.Fatalf("running transition: %v", err)
	}
	b.mu.Unlock()

	// Approve & Start resolved the notice — it is no longer active.
	b.mu.Lock()
	for i := range b.requests {
		if b.requests[i].IssueID == "OFFICE-9" && b.requests[i].Kind == "notice" &&
			b.requests[i].Title == awaitingStartNoticeTitle("OFFICE-9") &&
			requestIsActive(b.requests[i]) {
			b.mu.Unlock()
			t.Fatal("waiting notice must self-resolve when the task leaves drafting")
		}
	}
	b.mu.Unlock()

	b.mu.Lock()
	if _, err := b.transitionLifecycleLocked("OFFICE-9", LifecycleStateDrafting, "reopen"); err != nil {
		b.mu.Unlock()
		t.Fatalf("reopen transition: %v", err)
	}
	b.mu.Unlock()

	if got := countAwaitingStartNotices(b, "OFFICE-9"); got != 1 {
		t.Fatalf("expected 1 deduped waiting notice after reentry, got %d", got)
	}
}

// Ownerless drafts and system/non-issue tasks must not raise the notice:
// the Approve & Start hint would point at a button that can't dispatch
// anyone yet (ownerless), or at internal bookkeeping (system).
func TestDraftingNoticeSkipsOwnerlessSystemAndNonIssueTasks(t *testing.T) {
	b := newTestBroker(t)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "OFFICE-11", TaskType: "issue", Owner: "", Channel: "general"},
		{ID: "OFFICE-12", TaskType: "issue", Owner: "ceo", Channel: "general", System: true},
		{ID: "OFFICE-13", TaskType: "skill_run", Owner: "ceo", Channel: "general"},
	}
	for _, id := range []string{"OFFICE-11", "OFFICE-12", "OFFICE-13"} {
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDrafting, "test"); err != nil {
			b.mu.Unlock()
			t.Fatalf("transition %s: %v", id, err)
		}
	}
	b.mu.Unlock()

	for _, id := range []string{"OFFICE-11", "OFFICE-12", "OFFICE-13"} {
		if got := countAwaitingStartNotices(b, id); got != 0 {
			t.Fatalf("task %s: expected 0 notices, got %d", id, got)
		}
	}
}
