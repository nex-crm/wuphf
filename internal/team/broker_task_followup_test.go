package team

// broker_task_followup_test.go — post-done message routing (done-integrity
// fix family; ICP-eval v2 [01:48]/[01:58]: "make the tagline punchier" on a
// delivered task got no agent reply and no file edit for 22 minutes):
//
//  1. A HUMAN message in a delivered task's channel stamps the note AND
//     appends the task_followup wake action for the owner. #general and
//     archived tasks are excluded.
//  2. The owner's packet leads with FOLLOW-UP ON DELIVERED TASK and names
//     the reopen path.
//  3. taskNotificationTargets routes task_followup to the OWNER.
//  4. The owner can reopen their own delivered task (the packet's
//     instruction must be executable); other specialists still cannot.

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// newFollowUpTestBroker seeds a broker with a delivered (done) task owned by
// eng living in its own per-task channel, plus the usual roster.
func newFollowUpTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer"},
		{Slug: "fe", Name: "Frontend"},
	}
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng", "fe"}},
		{Slug: "task-done-1", Name: "Ship the landing page", Members: []string{"human", "ceo", "eng"}},
	}
	b.tasks = []teamTask{
		{
			ID:             "task-done-1",
			Channel:        "task-done-1",
			Title:          "Ship the landing page for the beta waitlist",
			Owner:          "eng",
			status:         "done",
			LifecycleState: LifecycleStateApproved,
			Artifact:       "landing/index.html",
		},
	}
	return b
}

func TestTaskFollowUp_HumanPostOnDeliveredTaskMarksAndAppendsAction(t *testing.T) {
	t.Parallel()
	b := newFollowUpTestBroker(t)
	if _, err := b.PostMessage("you", "task-done-1", "Make the tagline punchier — keep everything else.", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}
	marked := b.TaskByID("task-done-1")
	if marked.HumanNotePending == nil || !strings.Contains(marked.HumanNotePending.Body, "tagline punchier") {
		t.Fatalf("delivered task must carry the follow-up note; got %+v", marked.HumanNotePending)
	}
	followed := false
	for _, a := range b.Actions() {
		if a.Kind == taskFollowUpActionKind && a.RelatedID == "task-done-1" &&
			strings.Contains(a.Summary, "human follow-up on delivered task") {
			followed = true
		}
	}
	if !followed {
		t.Errorf("delivered-task follow-up must append the %s action", taskFollowUpActionKind)
	}
}

func TestTaskFollowUp_GeneralAndArchivedAndAgentPostsExcluded(t *testing.T) {
	t.Parallel()
	b := newFollowUpTestBroker(t)
	b.tasks = append(b.tasks,
		teamTask{ID: "task-done-general", Channel: "general", Title: "Old done work", Owner: "eng", status: "done"},
		teamTask{ID: "task-archived-1", Channel: "task-done-1", Title: "Folded legacy chat", Owner: "eng", status: "archived", LifecycleState: LifecycleStateArchived},
	)
	// #general is the office lobby: a human post there must not wake every
	// legacy done task's owner.
	if _, err := b.PostMessage("you", "general", "How is the week looking?", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}
	if task := b.TaskByID("task-done-general"); task.HumanNotePending != nil {
		t.Errorf("done task in #general must not be follow-up marked; got %+v", task.HumanNotePending)
	}
	// Agent posts never mark; archived tasks never mark.
	if _, err := b.PostMessage("ceo", "task-done-1", "Nice work on this one.", nil, ""); err != nil {
		t.Fatalf("agent post: %v", err)
	}
	if task := b.TaskByID("task-done-1"); task.HumanNotePending != nil {
		t.Errorf("agent posts must never mark a follow-up note; got %+v", task.HumanNotePending)
	}
	if _, err := b.PostMessage("you", "task-done-1", "One more tweak please.", nil, ""); err != nil {
		t.Fatalf("human post: %v", err)
	}
	if task := b.TaskByID("task-archived-1"); task.HumanNotePending != nil {
		t.Errorf("archived task must never be follow-up marked; got %+v", task.HumanNotePending)
	}
	for _, a := range b.Actions() {
		if a.Kind == taskFollowUpActionKind && a.RelatedID == "task-archived-1" {
			t.Errorf("archived task must never get a follow-up wake action")
		}
	}
}

func TestTaskFollowUp_PacketLeadsWithFollowUpBlock(t *testing.T) {
	t.Parallel()
	task := teamTask{
		ID:             "task-done-1",
		Channel:        "task-done-1",
		Title:          "Ship the landing page",
		Owner:          "eng",
		status:         "done",
		LifecycleState: LifecycleStateApproved,
		HumanNotePending: &TaskHumanNote{
			From: "human", Body: "Make the tagline punchier.", At: "2026-06-10T00:00:00Z",
		},
	}
	block := humanNotePacketBlock(task, "eng")
	if !strings.HasPrefix(block, "FOLLOW-UP ON DELIVERED TASK") {
		t.Fatalf("done-task note must lead with the follow-up banner; got %q", block)
	}
	if !strings.Contains(block, "Make the tagline punchier.") ||
		!strings.Contains(block, "action=reopen") ||
		!strings.Contains(block, task.ID) {
		t.Errorf("follow-up block must carry the message and the reopen path; got %q", block)
	}
	if got := humanNotePacketBlock(task, "fe"); got != "" {
		t.Errorf("non-owner must not see the note; got %q", got)
	}
	// Running tasks keep the original lead.
	task.status = "in_progress"
	task.LifecycleState = LifecycleStateRunning
	if got := humanNotePacketBlock(task, "eng"); !strings.HasPrefix(got, "HUMAN POSTED WHILE YOU WORKED") {
		t.Errorf("running-task note must keep the original lead; got %q", got)
	}
}

func TestTaskFollowUp_TargetsRouteToOwner(t *testing.T) {
	t.Parallel()
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			},
		},
	}
	task := teamTask{
		ID:             "task-done-1",
		Channel:        "task-done-1",
		Title:          "Ship the landing page",
		Owner:          "eng",
		status:         "done",
		LifecycleState: LifecycleStateApproved,
	}
	immediate, delayed := l.taskNotificationTargets(officeActionLog{
		Kind: taskFollowUpActionKind, Actor: "human", Channel: "task-done-1", RelatedID: "task-done-1",
	}, task)
	if len(immediate) != 1 || immediate[0].Slug != "eng" {
		t.Fatalf("follow-up must wake the owner only; got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("follow-up must not schedule delayed targets; got %+v", delayed)
	}
	// Ownerless delivered task falls back to the lead.
	task.Owner = ""
	immediate, _ = l.taskNotificationTargets(officeActionLog{
		Kind: taskFollowUpActionKind, Actor: "human", Channel: "task-done-1", RelatedID: "task-done-1",
	}, task)
	if len(immediate) != 1 || immediate[0].Slug != "ceo" {
		t.Fatalf("ownerless follow-up must fall back to the lead; got %+v", immediate)
	}
}

func TestTaskFollowUp_OwnerCanReopenOwnDeliveredTask(t *testing.T) {
	t.Parallel()
	b := newFollowUpTestBroker(t)
	if _, err := b.MutateTask(TaskPostRequest{Action: "reopen", ID: "task-done-1", Channel: "task-done-1", CreatedBy: "eng"}); err != nil {
		t.Fatalf("owner reopen of their own delivered task must be allowed; got %v", err)
	}
	reopened := b.TaskByID("task-done-1")
	if reopened.LifecycleState != LifecycleStateRunning || !strings.EqualFold(strings.TrimSpace(reopened.status), "in_progress") {
		t.Errorf("owner reopen must land Running/in_progress; got lifecycle=%s status=%q", reopened.LifecycleState, reopened.status)
	}

	// A non-owner specialist still cannot reopen someone else's task.
	b2 := newFollowUpTestBroker(t)
	_, err := b2.MutateTask(TaskPostRequest{Action: "reopen", ID: "task-done-1", Channel: "task-done-1", CreatedBy: "fe"})
	if err == nil {
		t.Fatalf("non-owner specialist reopen must stay forbidden")
	}
}
