package team

// broker_human_objection_test.go — the human-sovereignty contract
// (core-loop grader fix family #1; ICP-eval v2 observations [00:55],
// [01:04], [01:06]):
//
//  1. Request-changes feedback TEXT must reach the agent: the latest
//     verdict is stamped on the task (teamTask.ChangesRequested) and
//     rendered verbatim in the owner's next execution packet and wake
//     notification — not buried in the Decision Packet feedback log,
//     which BuildTaskExecutionPacket never reads.
//  2. An open HUMAN objection (teamTask.HumanObjection) hard-blocks
//     terminal transitions: approve/complete by ANY agent — the
//     lead/CEO included, on both the team_task and the
//     /tasks/{id}/decision paths — fails naming the objection; only a
//     human approve/complete clears it; a human request_changes
//     refreshes it.
//  3. The wire shape is additive: changes_requested / human_objection
//     round-trip the teamTaskWire shadow.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// newObjectionTestBroker seeds a broker with a general channel, a
// ceo+eng roster (ceo resolves as lead), and one in-review task owned
// by eng with reviewer on the reviewer list.
func newObjectionTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer"},
	}
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng", "reviewer"}},
	}
	b.tasks = []teamTask{
		{
			ID:        "task-obj-1",
			Channel:   "general",
			Title:     "Draft the renewal one-pager",
			Owner:     "eng",
			status:    "review",
			Reviewers: []string{"reviewer"},
		},
	}
	return b
}

const objectionFeedback = "Use Dana as the champion, not a fabricated contact, and rebuild the Corti sequence escalation-first."

func humanRequestChanges(t *testing.T, b *Broker, taskID string) {
	t.Helper()
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: taskID, Channel: "general",
		Details: objectionFeedback, CreatedBy: "human",
	}); err != nil {
		t.Fatalf("human request_changes: %v", err)
	}
}

func TestHumanObjection_FeedbackRendersInPacketAndWake(t *testing.T) {
	t.Parallel()
	b := newObjectionTestBroker(t)
	humanRequestChanges(t, b, "task-obj-1")

	task := b.TaskByID("task-obj-1")
	if task == nil {
		t.Fatal("task vanished")
	}
	if task.ChangesRequested == nil || task.ChangesRequested.Body != objectionFeedback {
		t.Fatalf("ChangesRequested not stamped with feedback: %+v", task.ChangesRequested)
	}
	if task.HumanObjection == nil || task.HumanObjection.Actor != "human" {
		t.Fatalf("HumanObjection not armed for a human reviewer: %+v", task.HumanObjection)
	}

	ctx := launcherForBrokerFixture(b).notifyCtx()
	packet := ctx.BuildTaskExecutionPacket("eng",
		officeActionLog{Kind: "task_updated", Actor: "human"}, *task, "Revise per feedback.")
	if !strings.Contains(packet, "CHANGES REQUESTED by @human") || !strings.Contains(packet, objectionFeedback) {
		t.Fatalf("execution packet must carry the request-changes feedback verbatim, got:\n%s", packet)
	}
	if !strings.Contains(packet, "sovereign") {
		t.Fatalf("packet must state the human objection is sovereign, got:\n%s", packet)
	}
	wake := ctx.TaskNotificationContent(officeActionLog{Kind: "task_updated", Actor: "human"}, *task)
	if !strings.Contains(wake, "CHANGES REQUESTED by @human") || !strings.Contains(wake, objectionFeedback) {
		t.Fatalf("wake notification content must carry the feedback, got:\n%s", wake)
	}
}

func TestHumanObjection_AgentApproveBlockedHumanApproveClears(t *testing.T) {
	t.Parallel()
	b := newObjectionTestBroker(t)
	humanRequestChanges(t, b, "task-obj-1")

	// Lead approve over the open objection → forbidden, naming the objection.
	_, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: "task-obj-1", Channel: "general", CreatedBy: "ceo"})
	var mutationErr *TaskMutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != TaskMutationForbidden {
		t.Fatalf("ceo approve over open human objection: want TaskMutationForbidden, got %v", err)
	}
	if !strings.Contains(mutationErr.Message, "@human") || !strings.Contains(mutationErr.Message, "Dana") {
		t.Fatalf("forbidden error must name the objection (actor + feedback), got %q", mutationErr.Message)
	}

	// Owner complete is blocked the same way (the steer is submit_for_review).
	_, err = b.MutateTask(TaskPostRequest{Action: "complete", ID: "task-obj-1", Channel: "general", CreatedBy: "eng"})
	if !errors.As(err, &mutationErr) || mutationErr.Kind != TaskMutationForbidden {
		t.Fatalf("owner complete over open human objection: want TaskMutationForbidden, got %v", err)
	}
	if !strings.Contains(mutationErr.Message, "submit_for_review") {
		t.Fatalf("forbidden error must steer to submit_for_review, got %q", mutationErr.Message)
	}

	// Decision-endpoint path: a non-human approve is refused too.
	if err := b.RecordTaskDecision("task-obj-1", "approve", "ceo"); !errors.Is(err, ErrHumanObjectionOpen) {
		t.Fatalf("decision-path agent approve: want ErrHumanObjectionOpen, got %v", err)
	}
	if got := b.TaskByID("task-obj-1"); got == nil || strings.EqualFold(strings.TrimSpace(got.status), "done") {
		t.Fatalf("task must not land done over an open human objection, got %+v", got)
	}

	// HUMAN approve clears the objection and lands the task.
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: "task-obj-1", Channel: "general", CreatedBy: "human"}); err != nil {
		t.Fatalf("human approve: %v", err)
	}
	done := b.TaskByID("task-obj-1")
	if done == nil || !strings.EqualFold(strings.TrimSpace(done.status), "done") {
		t.Fatalf("human approve must land done, got %+v", done)
	}
	if done.HumanObjection != nil || done.ChangesRequested != nil {
		t.Fatalf("human approve must clear the objection + latest feedback stamp, got objection=%+v changes=%+v",
			done.HumanObjection, done.ChangesRequested)
	}
}

func TestHumanObjection_HumanRequestChangesRefreshes(t *testing.T) {
	t.Parallel()
	b := newObjectionTestBroker(t)
	humanRequestChanges(t, b, "task-obj-1")
	first := b.TaskByID("task-obj-1").HumanObjection
	if first == nil {
		t.Fatal("first objection not recorded")
	}
	const second = "Second pass: the pricing table is still wrong — use the Q4 sheet."
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: "task-obj-1", Channel: "general",
		Details: second, CreatedBy: "human",
	}); err != nil {
		t.Fatalf("second human request_changes: %v", err)
	}
	refreshed := b.TaskByID("task-obj-1")
	if refreshed.HumanObjection == nil || refreshed.HumanObjection.Body != second {
		t.Fatalf("human request_changes must refresh the open objection, got %+v", refreshed.HumanObjection)
	}
	if refreshed.HumanObjection == first {
		t.Fatal("refresh must assign a fresh struct, not mutate the prior one (rollback safety)")
	}
	if refreshed.ChangesRequested == nil || refreshed.ChangesRequested.Body != second {
		t.Fatalf("latest-feedback stamp must follow the refresh, got %+v", refreshed.ChangesRequested)
	}
}

func TestHumanObjection_AgentRequestChangesDoesNotArmTheGate(t *testing.T) {
	t.Parallel()
	b := newObjectionTestBroker(t)
	const agentFeedback = "Tighten the summary section — it repeats the intro."
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: "task-obj-1", Channel: "general",
		Details: agentFeedback, CreatedBy: "reviewer",
	}); err != nil {
		t.Fatalf("agent request_changes: %v", err)
	}
	task := b.TaskByID("task-obj-1")
	if task.ChangesRequested == nil || task.ChangesRequested.Actor != "reviewer" {
		t.Fatalf("agent verdict must stamp ChangesRequested, got %+v", task.ChangesRequested)
	}
	if task.HumanObjection != nil {
		t.Fatalf("agent verdict must NOT arm the human-sovereignty gate, got %+v", task.HumanObjection)
	}
	// Without a human objection the lead can still approve — and the
	// approve retires the now-stale feedback stamp.
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: "task-obj-1", Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatalf("ceo approve with no human objection: %v", err)
	}
	if got := b.TaskByID("task-obj-1"); got.ChangesRequested != nil {
		t.Fatalf("approve must clear the latest-feedback stamp, got %+v", got.ChangesRequested)
	}
}

func TestHumanObjection_DecisionPathStampsAndHumanApproveClears(t *testing.T) {
	t.Parallel()
	b := newObjectionTestBroker(t)
	// Inbox "Request changes" button path: RecordTaskDecisionWithComment
	// with a human author stamps the task-resident feedback + objection.
	if err := b.RecordTaskDecisionWithComment("task-obj-1", "request_changes", objectionFeedback, "human"); err != nil {
		t.Fatalf("decision-path request_changes: %v", err)
	}
	task := b.TaskByID("task-obj-1")
	if task.ChangesRequested == nil || task.ChangesRequested.Body != objectionFeedback {
		t.Fatalf("decision-path request_changes must stamp ChangesRequested, got %+v", task.ChangesRequested)
	}
	if task.HumanObjection == nil {
		t.Fatal("decision-path human request_changes must arm the objection")
	}
	// Agent approve via the decision path is refused while it stands.
	if err := b.RecordTaskDecision("task-obj-1", "approve", "ceo"); !errors.Is(err, ErrHumanObjectionOpen) {
		t.Fatalf("decision-path agent approve: want ErrHumanObjectionOpen, got %v", err)
	}
	// Human approve via the decision path clears it.
	if err := b.RecordTaskDecisionWithComment("task-obj-1", "approve", "", "human"); err != nil {
		t.Fatalf("decision-path human approve: %v", err)
	}
	cleared := b.TaskByID("task-obj-1")
	if cleared.HumanObjection != nil || cleared.ChangesRequested != nil {
		t.Fatalf("decision-path human approve must clear both stamps, got objection=%+v changes=%+v",
			cleared.HumanObjection, cleared.ChangesRequested)
	}
}

func TestTaskReviewObjection_WireRoundTripAdditive(t *testing.T) {
	t.Parallel()
	objection := &TaskReviewObjection{Actor: "human", Body: objectionFeedback, At: "2026-06-11T00:00:00Z"}
	task := teamTask{
		ID: "task-wire-1", Channel: "general", Title: "Wire check",
		ChangesRequested: objection, HumanObjection: objection,
	}
	blob, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"changes_requested"`, `"human_objection"`, `"actor"`, `"body"`, `"at"`} {
		if !strings.Contains(string(blob), key) {
			t.Fatalf("wire blob missing %s: %s", key, blob)
		}
	}
	var back teamTask
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ChangesRequested == nil || back.ChangesRequested.Body != objectionFeedback ||
		back.HumanObjection == nil || back.HumanObjection.Actor != "human" ||
		back.HumanObjection.At != "2026-06-11T00:00:00Z" {
		t.Fatalf("objection did not round-trip: changes=%+v objection=%+v", back.ChangesRequested, back.HumanObjection)
	}
	// Additive: a blob WITHOUT the new keys still unmarshals clean.
	var legacy teamTask
	if err := json.Unmarshal([]byte(`{"id":"task-legacy","channel":"general","title":"old","status":"open","created_at":"x","updated_at":"x"}`), &legacy); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if legacy.ChangesRequested != nil || legacy.HumanObjection != nil {
		t.Fatal("legacy blob must leave both fields nil")
	}
}
