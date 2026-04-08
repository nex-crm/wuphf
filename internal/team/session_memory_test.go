package team

import (
	"strings"
	"testing"
)

func TestBuildSessionMemorySnapshotIncludesSerializableSummaries(t *testing.T) {
	snapshot := BuildSessionMemorySnapshot(SessionModeOneOnOne, "pm", []RuntimeTask{{
		ID:             "task-1",
		Title:          "Polish launch checklist",
		Owner:          "pm",
		Status:         "in_progress",
		PipelineStage:  "review",
		ReviewState:    "pending_review",
		ExecutionMode:  "local_worktree",
		WorktreePath:   "/tmp/wuphf-task-1",
		WorktreeBranch: "task/1",
	}}, []RuntimeRequest{{
		ID:       "req-1",
		Kind:     "approval",
		Title:    "Approve launch timing",
		Question: "Should we ship tomorrow?",
		From:     "ceo",
		Blocking: true,
		Status:   "pending",
	}}, []RuntimeMessage{{
		ID:      "msg-1",
		From:    "ceo",
		Content: "We need a final timing call before tomorrow.",
	}})

	if snapshot.Version != 1 {
		t.Fatalf("expected version 1, got %d", snapshot.Version)
	}
	if snapshot.SessionMode != SessionModeOneOnOne || snapshot.DirectAgent != "pm" {
		t.Fatalf("unexpected session metadata: %+v", snapshot)
	}
	if snapshot.Focus == "" || !strings.Contains(snapshot.Focus, "Approve launch timing") {
		t.Fatalf("expected request focus, got %+v", snapshot)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Summary == "" {
		t.Fatalf("expected summarized task, got %+v", snapshot.Tasks)
	}
	if len(snapshot.Requests) != 1 || snapshot.Requests[0].Summary == "" {
		t.Fatalf("expected summarized request, got %+v", snapshot.Requests)
	}
	if len(snapshot.Messages) != 1 || !strings.Contains(snapshot.Messages[0].Summary, "@ceo:") {
		t.Fatalf("expected summarized recent message, got %+v", snapshot.Messages)
	}
	if len(snapshot.NextSteps) == 0 {
		t.Fatalf("expected next steps, got %+v", snapshot)
	}
}

func TestSessionMemorySnapshotRestorationContextCollectsResumeHandles(t *testing.T) {
	snapshot := SessionMemorySnapshot{
		Focus:     "Resume launch work.",
		NextSteps: []string{"Answer the blocker.", "Use the worktree."},
		Tasks: []SessionMemoryTaskSummary{
			{ID: "task-1", Status: "in_progress", WorktreePath: "/tmp/wuphf-task-1", ThreadID: "msg-9"},
			{ID: "task-2", Status: "review", WorktreePath: "/tmp/wuphf-task-1", ThreadID: "msg-10"},
		},
		Requests: []SessionMemoryRequestSummary{
			{ID: "req-1", Status: "pending", Blocking: true, ReplyTo: "msg-9"},
			{ID: "req-2", Status: "answered", Blocking: true, ReplyTo: "msg-12"},
		},
	}

	ctx := snapshot.RestorationContext()
	if ctx.Focus != "Resume launch work." {
		t.Fatalf("unexpected focus: %+v", ctx)
	}
	if len(ctx.ActiveTaskIDs) != 2 {
		t.Fatalf("expected two active task ids, got %+v", ctx)
	}
	if len(ctx.PendingRequestIDs) != 1 || ctx.PendingRequestIDs[0] != "req-1" {
		t.Fatalf("expected only pending blocking request, got %+v", ctx.PendingRequestIDs)
	}
	if len(ctx.WorkingDirectories) != 1 || ctx.WorkingDirectories[0] != "/tmp/wuphf-task-1" {
		t.Fatalf("expected deduped worktree path, got %+v", ctx.WorkingDirectories)
	}
	if len(ctx.ThreadIDs) != 2 {
		t.Fatalf("expected deduped thread ids, got %+v", ctx.ThreadIDs)
	}
}

func TestBuildSessionMemorySnapshotFromOfficeStateReconstructsContext(t *testing.T) {
	snapshot := BuildSessionMemorySnapshotFromOfficeState(SessionModeOffice, "", []teamTask{
		{
			ID:             "task-7",
			Title:          "Ship release candidate",
			Owner:          "fe",
			Status:         "in_progress",
			PipelineStage:  "execution",
			ReviewState:    "pending_review",
			ExecutionMode:  "local_worktree",
			WorktreePath:   "/tmp/wuphf-task-7",
			WorktreeBranch: "task/7",
			ThreadID:       "msg-7",
		},
		{
			ID:     "task-8",
			Title:  "Old done task",
			Status: "done",
		},
	}, []humanInterview{
		{
			ID:            "req-3",
			Kind:          "confirm",
			Status:        "pending",
			From:          "ceo",
			Title:         "Confirm launch plan",
			Question:      "Should the office proceed with this launch plan?",
			Blocking:      true,
			Required:      true,
			ReplyTo:       "msg-7",
			RecommendedID: "confirm_proceed",
		},
		{
			ID:       "req-4",
			Status:   "answered",
			Title:    "Old answered request",
			Question: "Ignore me",
		},
	}, []officeActionLog{
		{ID: "action-1", Kind: "task_created", Actor: "ceo", Summary: "Opened launch task", RelatedID: "task-7", CreatedAt: "2026-04-07T10:00:00Z"},
		{ID: "action-2", Kind: "request_created", Actor: "ceo", Summary: "Asked for launch confirmation", RelatedID: "req-3", CreatedAt: "2026-04-07T10:05:00Z"},
	}, []channelMessage{
		{ID: "msg-1", From: "ceo", Content: "We need a launch decision today.", Timestamp: "2026-04-07T10:00:00Z"},
		{ID: "msg-7", From: "fe", Content: "Release candidate is ready for review.", Timestamp: "2026-04-07T10:06:00Z"},
	})

	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].ID != "task-7" {
		t.Fatalf("expected only relevant active task summary, got %+v", snapshot.Tasks)
	}
	if len(snapshot.Requests) != 1 || snapshot.Requests[0].ID != "req-3" {
		t.Fatalf("expected only active request summary, got %+v", snapshot.Requests)
	}
	if len(snapshot.Actions) != 2 {
		t.Fatalf("expected action summaries, got %+v", snapshot.Actions)
	}
	if len(snapshot.Messages) != 2 {
		t.Fatalf("expected recent message summaries, got %+v", snapshot.Messages)
	}
	if !strings.Contains(snapshot.Focus, "Confirm launch plan") {
		t.Fatalf("expected request-led focus, got %+v", snapshot)
	}
	restore := snapshot.RestorationContext()
	if len(restore.WorkingDirectories) != 1 || restore.WorkingDirectories[0] != "/tmp/wuphf-task-7" {
		t.Fatalf("expected worktree restore hint, got %+v", restore)
	}
	if len(restore.PendingRequestIDs) != 1 || restore.PendingRequestIDs[0] != "req-3" {
		t.Fatalf("expected pending request restore hint, got %+v", restore)
	}
}

func TestSessionMemorySnapshotToRecoveryPreservesFields(t *testing.T) {
	snapshot := SessionMemorySnapshot{
		Focus:      "Keep the release on hold.",
		NextSteps:  []string{"Wait for legal."},
		Highlights: []string{"@ceo: Legal review is still pending."},
	}
	recovery := snapshot.ToRecovery()
	if recovery.Focus != snapshot.Focus {
		t.Fatalf("expected focus round-trip, got %+v", recovery)
	}
	if len(recovery.NextSteps) != 1 || recovery.NextSteps[0] != "Wait for legal." {
		t.Fatalf("unexpected next steps: %+v", recovery)
	}
	if len(recovery.Highlights) != 1 || recovery.Highlights[0] != "@ceo: Legal review is still pending." {
		t.Fatalf("unexpected highlights: %+v", recovery)
	}
}
