package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/workflow"
)

func twoStepSpec(a, bID string) *workflow.Spec {
	return &workflow.Spec{Actions: []workflow.Action{{ID: a}, {ID: bID}}}
}

// TestSurfaceExtractedWorkflowsRecurrence verifies the recurrence count: the
// same workflow (same fingerprint) produced by THREE distinct completed tasks
// surfaces once with recurrence 3, ranked above a one-off.
func TestSurfaceExtractedWorkflowsRecurrence(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	path := ProposalSinkPath()

	digest := twoStepSpec("gmail_fetch_emails", "slack_send_message")
	for _, task := range []string{"OFFICE-1", "OFFICE-2", "OFFICE-3"} {
		_ = appendProposal(path, storedProposal{
			Fingerprint: proposalFingerprint(digest), TaskID: task,
			Name: "Inbox to Slack", Spec: digest,
		})
	}
	// A one-off different workflow.
	oneoff := twoStepSpec("gmail_fetch_emails", "gmail_create_email_draft")
	_ = appendProposal(path, storedProposal{
		Fingerprint: proposalFingerprint(oneoff), TaskID: "OFFICE-9",
		Name: "Draft replies", Spec: oneoff,
	})

	items, err := surfaceExtractedWorkflows(path)
	if err != nil {
		t.Fatalf("surface: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 distinct workflows, got %d: %+v", len(items), items)
	}
	// Most-recurrent first.
	if items[0].Name != "Inbox to Slack" || items[0].Recurrence != 3 {
		t.Fatalf("top workflow should be the 3x recurrence, got %+v", items[0])
	}
	if len(items[0].TaskIDs) != 3 {
		t.Errorf("recurrence task set should be 3 distinct tasks, got %v", items[0].TaskIDs)
	}
	if items[1].Recurrence != 1 {
		t.Errorf("one-off should have recurrence 1, got %d", items[1].Recurrence)
	}
}

// TestSurfaceDedupesSameTask verifies re-completing the SAME task does not
// inflate recurrence (distinct-task counting).
func TestSurfaceDedupesSameTask(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	path := ProposalSinkPath()
	spec := twoStepSpec("gmail_fetch_emails", "slack_send_message")
	for i := 0; i < 3; i++ { // same task, persisted 3x (re-completion)
		_ = appendProposal(path, storedProposal{
			Fingerprint: proposalFingerprint(spec), TaskID: "OFFICE-1", Name: "X", Spec: spec,
		})
	}
	items, err := surfaceExtractedWorkflows(path)
	if err != nil {
		t.Fatalf("surface: %v", err)
	}
	if len(items) != 1 || items[0].Recurrence != 1 {
		t.Fatalf("re-completing one task must stay recurrence 1, got %+v", items)
	}
}

// setTaskStatusForTest sets a task's status (in-package test access).
func setTaskStatusForTest(b *Broker, id, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].status = status
			return
		}
	}
	b.tasks = append(b.tasks, teamTask{ID: id, status: status})
}

// TestSweepDetectsNewCompletionsOnly verifies the path-independent sweep: boot
// seeding skips history, a post-boot completion fires once, and reopen →
// re-complete re-fires. (Spawned extractions hit the cheap gate — no traces, no
// model call — so this stays a fast unit test.)
func TestSweepDetectsNewCompletionsOnly(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)

	setTaskStatusForTest(b, "OLD-DONE", "done") // pre-existing history
	b.seedExtractedFromCurrentTasks()
	if got := b.sweepCompletedForExtraction(); len(got) != 0 {
		t.Fatalf("seeded history must not fire, got %v", got)
	}

	setTaskStatusForTest(b, "NEW-1", "done") // completes after boot
	got := b.sweepCompletedForExtraction()
	if len(got) != 1 || got[0] != "NEW-1" {
		t.Fatalf("new completion should fire once, got %v", got)
	}
	if got := b.sweepCompletedForExtraction(); len(got) != 0 {
		t.Fatalf("already-extracted must not re-fire, got %v", got)
	}

	setTaskStatusForTest(b, "NEW-1", "running") // reopen
	b.sweepCompletedForExtraction()             // un-marks
	setTaskStatusForTest(b, "NEW-1", "done")    // re-complete
	if got := b.sweepCompletedForExtraction(); len(got) != 1 || got[0] != "NEW-1" {
		t.Fatalf("re-completion should re-fire, got %v", got)
	}
}
