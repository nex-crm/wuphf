package team

// broker_lifecycle_stage_test.go covers the LifecycleStage derived concept
// and the archive action.
//
// Tests:
//   - TestLifecycleStageForAllCanonicalStates: sweeps CanonicalLifecycleStates
//     + LifecycleStateArchived and asserts lifecycleStageFor returns the
//     exact expected stage for every value.
//   - TestArchiveActionTransitionsToArchivedState: asserts that the archive
//     action transitions a task to LifecycleStateArchived.
//   - TestArchivedTaskIsTerminalAndFilteredFromDefault: asserts that an
//     archived task is excluded from default active listings and included
//     with include_done=true.

import (
	"testing"
)

func TestLifecycleStageForAllCanonicalStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state LifecycleState
		want  LifecycleStage
	}{
		// backlog ← drafting, intake, ready, unknown
		{LifecycleStateDrafting, StageBacklog},
		{LifecycleStateIntake, StageBacklog},
		{LifecycleStateReady, StageBacklog},
		// in_progress ← planning, running, review, changes_requested
		{LifecycleStatePlanning, StageInProgress},
		{LifecycleStateRunning, StageInProgress},
		{LifecycleStateReview, StageInProgress},
		{LifecycleStateChangesRequested, StageInProgress},
		// blocked ← blocked, queued_behind_owner
		{LifecycleStateBlocked, StageBlocked},
		{LifecycleStateQueuedBehindOwner, StageBlocked},
		// needs_human ← decision
		{LifecycleStateDecision, StageNeedsHuman},
		// done ← approved
		{LifecycleStateApproved, StageDone},
		// archive ← rejected, archived
		{LifecycleStateRejected, StageArchive},
		{LifecycleStateArchived, StageArchive},
		// unknown defaults to backlog
		{LifecycleStateUnknown, StageBacklog},
	}

	// Ensure every state in CanonicalLifecycleStates is covered by the
	// table above (except LifecycleStateUnknown which is the migration
	// fallback, not in CanonicalLifecycleStates). This prevents silent
	// drift when new canonical states are added.
	canonical := CanonicalLifecycleStates()
	covered := make(map[LifecycleState]bool, len(cases))
	for _, tc := range cases {
		covered[tc.state] = true
	}
	for _, state := range canonical {
		if !covered[state] {
			t.Errorf("CanonicalLifecycleState %q is not covered by the stage-mapping test table", state)
		}
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.state), func(t *testing.T) {
			t.Parallel()
			got := lifecycleStageFor(tc.state)
			if got != tc.want {
				t.Errorf("lifecycleStageFor(%q) = %q, want %q", tc.state, got, tc.want)
			}
		})
	}
}

func TestLifecycleStageScheduledIsNeverReturnedByLifecycleStageFor(t *testing.T) {
	// StageScheduled comes from a scheduling primitive, not from a
	// LifecycleState. lifecycleStageFor must never return StageScheduled
	// for any input.
	t.Parallel()
	allStates := append(CanonicalLifecycleStates(), LifecycleStateUnknown, LifecycleState(""), LifecycleState("garbage"))
	for _, s := range allStates {
		got := lifecycleStageFor(s)
		if got == StageScheduled {
			t.Errorf("lifecycleStageFor(%q) returned StageScheduled, which must never happen", s)
		}
	}
}

func TestArchiveActionTransitionsToArchivedState(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human"}},
	}
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:             "task-archive",
		Channel:        "general",
		Title:          "Work to archive",
		LifecycleState: LifecycleStateApproved,
		status:         "done",
	}}
	b.indexLifecycleLocked("task-archive", "", LifecycleStateApproved)
	b.mu.Unlock()

	got, err := b.MutateTask(TaskPostRequest{
		Action:    "archive",
		ID:        "task-archive",
		Channel:   "general",
		Details:   "moving off the board",
		CreatedBy: "human",
	})
	if err != nil {
		t.Fatalf("MutateTask archive: %v", err)
	}
	if got.Task.LifecycleState != LifecycleStateArchived {
		t.Errorf("LifecycleState: got %q, want %q", got.Task.LifecycleState, LifecycleStateArchived)
	}
	if got.Task.Status() != "archived" {
		t.Errorf("status: got %q, want %q", got.Task.Status(), "archived")
	}
	if got.Task.Blocked() {
		t.Errorf("archived task should not be blocked, got blocked=true")
	}
}

func TestArchivedTaskIsTerminalStatus(t *testing.T) {
	t.Parallel()
	if !isTerminalTeamTaskStatus("archived") {
		t.Error("isTerminalTeamTaskStatus(\"archived\") = false, want true")
	}
	// Verify the terminal check does not accidentally catch non-archived statuses.
	for _, notTerminal := range []string{"open", "in_progress", "review", "rejected", "blocked", "draft"} {
		if isTerminalTeamTaskStatus(notTerminal) {
			t.Errorf("isTerminalTeamTaskStatus(%q) = true, want false", notTerminal)
		}
	}
}

func TestArchivedTaskExcludedFromDefaultListingIncludedWithIncludeDone(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
	}
	b.tasks = []teamTask{
		{ID: "active-task", Channel: "general", Title: "Active", status: "open"},
		{ID: "done-task", Channel: "general", Title: "Done", status: "done"},
		{ID: "archived-task", Channel: "general", Title: "Archived", status: "archived", LifecycleState: LifecycleStateArchived},
	}

	// Default listing (include_done=false): archived tasks must be excluded.
	got, err := b.ListTasks(TaskListRequest{Channel: "general", ViewerSlug: "pm"})
	if err != nil {
		t.Fatalf("ListTasks default: %v", err)
	}
	for _, task := range got.Tasks {
		if task.ID == "archived-task" {
			t.Errorf("archived-task should not appear in default active listing, got %+v", got.Tasks)
		}
		if task.ID == "done-task" {
			t.Errorf("done-task should not appear in default active listing, got %+v", got.Tasks)
		}
	}
	assertTaskIDs(t, got.Tasks, []string{"active-task"})

	// With include_done=true: both done and archived tasks must appear.
	gotAll, err := b.ListTasks(TaskListRequest{Channel: "general", ViewerSlug: "pm", IncludeDone: true})
	if err != nil {
		t.Fatalf("ListTasks include_done: %v", err)
	}
	assertTaskIDs(t, gotAll.Tasks, []string{"active-task", "done-task", "archived-task"})
}
