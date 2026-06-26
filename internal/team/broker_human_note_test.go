package team

// broker_human_note_test.go — the stop-order backstop (anti-fabrication
// fix family #2; ICP-eval v2 [00:50]: the human typed "Stop — do not build
// a placeholder…" while the CEO worked, the message was never seen, and a
// fabricated one-pager shipped over it):
//
//  1. A HUMAN message posted into a running task's channel stamps
//     teamTask.HumanNotePending (fresh struct, additive wire field).
//     Agent posts, non-running tasks, and system tasks never get stamped.
//  2. A note whose message LEADS with stop/wait/hold sets Halt: agent
//     submit_for_review/complete are refused naming the stop order; a
//     human performing the action clears the note instead.
//  3. ConsumeTaskHumanNote (the packet builder's seam) clears the note,
//     releasing the gate — the next complete succeeds.

import (
	"errors"
	"strings"
	"testing"
)

// newHumanNoteTestBroker seeds a broker with a general channel, a ceo+eng
// roster, and one running task owned by eng in #general.
func newHumanNoteTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer"},
	}
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}},
	}
	b.tasks = []teamTask{
		{
			ID:      "task-note-1",
			Channel: "general",
			Title:   "Draft the Acme Corp QBR one-pager",
			Owner:   "eng",
			status:  "in_progress",
		},
	}
	return b
}

func TestHumanNote_LeadingHaltTokens(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"Stop — do not build a placeholder":      true,
		"stop everything":                        true,
		"WAIT, the numbers are wrong":            true,
		"Hold on, read the wiki first":           true,
		"Please stop doing that":                 false, // stop is not the leading token
		"FYI the seat count changed":             false,
		"The brief looks good, ship it":          false,
		"":                                       false,
		"   wait!! the escalations are missing!": true,
	}
	for content, want := range cases {
		if got := humanNoteLeadsWithHalt(content); got != want {
			t.Errorf("humanNoteLeadsWithHalt(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestHumanNote_HumanPostMarksRunningTaskOnly(t *testing.T) {
	t.Parallel()
	b := newHumanNoteTestBroker(t)
	// Add a non-running sibling and a system task in the same channel. The
	// done sibling lives in #general, so the post-done follow-up marking
	// (broker_task_followup_test.go) does not apply either — #general is
	// the lobby and never wakes legacy done tasks.
	b.tasks = append(b.tasks,
		teamTask{ID: "task-note-2", Channel: "general", Title: "Done work", Owner: "eng", status: "done"},
		teamTask{ID: "task-note-3", Channel: "general", Title: "Backup & Migration", Owner: "ceo", status: "in_progress", System: true},
	)

	if _, err := b.PostMessage("you", "general", "Stop — do not build a placeholder. Dana Whitfield is the owner on record.", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}

	marked := b.TaskByID("task-note-1")
	if marked == nil || marked.HumanNotePending == nil {
		t.Fatalf("running task must carry the human note; got %+v", marked)
	}
	if !marked.HumanNotePending.Halt {
		t.Errorf("leading 'Stop' must set Halt; got %+v", marked.HumanNotePending)
	}
	if !strings.Contains(marked.HumanNotePending.Body, "Dana Whitfield") {
		t.Errorf("note body must carry the message verbatim; got %q", marked.HumanNotePending.Body)
	}
	if doneTask := b.TaskByID("task-note-2"); doneTask.HumanNotePending != nil {
		t.Errorf("non-running task must not be stamped; got %+v", doneTask.HumanNotePending)
	}
	if sys := b.TaskByID("task-note-3"); sys.HumanNotePending != nil {
		t.Errorf("system task must not be stamped; got %+v", sys.HumanNotePending)
	}
}

func TestHumanNote_AgentPostDoesNotMark(t *testing.T) {
	t.Parallel()
	b := newHumanNoteTestBroker(t)
	if _, err := b.PostMessage("ceo", "general", "stop and think about the sequencing here", nil, ""); err != nil {
		t.Fatalf("agent post: %v", err)
	}
	if task := b.TaskByID("task-note-1"); task.HumanNotePending != nil {
		t.Errorf("agent posts must never stamp a human note; got %+v", task.HumanNotePending)
	}
}

func TestHumanNote_HaltBlocksAgentCompleteUntilConsumed(t *testing.T) {
	t.Parallel()
	b := newHumanNoteTestBroker(t)
	if _, err := b.PostMessage("you", "general", "Stop — wrong numbers, read team/accounts/acme-corp.md first.", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}

	for _, action := range []string{"complete", "submit_for_review"} {
		_, err := b.MutateTask(TaskPostRequest{Action: action, ID: "task-note-1", Channel: "general", CreatedBy: "eng"})
		var mutationErr *TaskMutationError
		if !errors.As(err, &mutationErr) || mutationErr.Kind != TaskMutationForbidden {
			t.Fatalf("agent %s must be forbidden while the halt note is pending; got %v", action, err)
		}
		if !strings.Contains(mutationErr.Message, "stop order") {
			t.Errorf("%s error must name the stop order; got %q", action, mutationErr.Message)
		}
	}

	// Consumption (the packet builder's seam) releases the gate.
	b.ConsumeTaskHumanNote("task-note-1")
	if task := b.TaskByID("task-note-1"); task.HumanNotePending != nil {
		t.Fatalf("ConsumeTaskHumanNote must clear the note; got %+v", task.HumanNotePending)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: "task-note-1", Channel: "general", CreatedBy: "eng"}); err != nil {
		t.Errorf("complete must succeed once the note is consumed; got %v", err)
	}
}

func TestHumanNote_NonHaltNeverBlocks_HumanCompleteClears(t *testing.T) {
	t.Parallel()
	b := newHumanNoteTestBroker(t)
	if _, err := b.PostMessage("you", "general", "FYI the seat count changed to 240 this morning.", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}
	task := b.TaskByID("task-note-1")
	if task.HumanNotePending == nil || task.HumanNotePending.Halt {
		t.Fatalf("FYI note must be pending and non-halt; got %+v", task.HumanNotePending)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: "task-note-1", Channel: "general", CreatedBy: "eng"}); err != nil {
		t.Fatalf("non-halt note must never block agent complete; got %v", err)
	}

	// Halt note + HUMAN complete: the human knows what they said — the
	// action proceeds and clears the note.
	b2 := newHumanNoteTestBroker(t)
	if _, err := b2.PostMessage("you", "general", "stop — actually I'll close this out myself", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}
	if _, err := b2.MutateTask(TaskPostRequest{Action: "complete", ID: "task-note-1", Channel: "general", CreatedBy: "human"}); err != nil {
		t.Fatalf("human complete must pass the halt gate; got %v", err)
	}
	if after := b2.TaskByID("task-note-1"); after.HumanNotePending != nil {
		t.Errorf("human complete must clear the note; got %+v", after.HumanNotePending)
	}
}
