package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestResolveInitialOfficeAppSupportsRecovery(t *testing.T) {
	if got := resolveInitialOfficeApp("recovery"); got != officeAppRecovery {
		t.Fatalf("expected recovery app, got %q", got)
	}
}

func TestRecoverCommandSwitchesToRecoveryApp(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.input = []rune("/recover")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if got.activeApp != officeAppRecovery {
		t.Fatalf("expected recovery app, got %q", got.activeApp)
	}
	if !strings.Contains(got.notice, "recovery summary") {
		t.Fatalf("expected recovery notice, got %q", got.notice)
	}
}

func TestCurrentAwaySummaryUsesRecoveryFocus(t *testing.T) {
	m := newChannelModel(false)
	m.unreadCount = 3
	m.requests = []channelInterview{
		{
			ID:       "req-1",
			Title:    "Review launch scope",
			Question: "Review the proposed launch scope.",
			From:     "ceo",
			Blocking: true,
			Status:   "pending",
		},
	}

	got := m.currentAwaySummary()
	if !strings.Contains(got, "Review launch scope from @ceo") {
		t.Fatalf("expected focus summary, got %q", got)
	}
	if !strings.Contains(got, "Next: Answer the blocking human request before moving more work") {
		t.Fatalf("expected next-step summary, got %q", got)
	}
}

func TestBuildRecoveryLinesShowsSummaryAndHighlights(t *testing.T) {
	m := newChannelModel(false)
	m.unreadCount = 4
	m.tasks = []channelTask{
		{ID: "task-1", Title: "Ship launch checklist", Owner: "pm", Status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/tmp/wuphf-task-1"},
	}
	m.requests = []channelInterview{
		{ID: "req-1", Title: "Review launch scope", Question: "Review the launch scope", From: "ceo", Blocking: true, Status: "pending"},
	}
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Need final scope review before launch.", Timestamp: "2026-04-06T10:00:00Z"},
		{ID: "msg-2", From: "pm", Content: "Checklist is nearly ready.", Timestamp: "2026-04-06T10:01:00Z"},
	}

	snapshot := m.currentRuntimeSnapshot()
	awaySummary := summarizeAwayRecovery(m.unreadCount, snapshot.Recovery)
	lines := buildRecoveryLines(snapshot, 88, awaySummary, m.unreadCount, true)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "What changed while you were gone") {
		t.Fatalf("expected away-summary card, got %q", plain)
	}
	if !strings.Contains(plain, "What to do next") {
		t.Fatalf("expected next-steps card, got %q", plain)
	}
	if !strings.Contains(plain, "Latest highlights") {
		t.Fatalf("expected highlights card, got %q", plain)
	}
	if !strings.Contains(plain, "Current state") {
		t.Fatalf("expected runtime-state card, got %q", plain)
	}
}
