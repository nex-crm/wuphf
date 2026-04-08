package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildWorkspaceSwitcherOptionsIncludesActiveWorkAndThreads(t *testing.T) {
	m := newChannelModel(false)
	m.unreadCount = 3
	m.awaySummary = "3 new since you looked. Next: answer the blocking request."
	m.members = []channelMember{{
		Slug:         "pm",
		Name:         "Product Manager",
		LastMessage:  "Reviewing the launch checklist now",
		LastTime:     time.Now().Add(-time.Minute).Format(time.RFC3339),
		LiveActivity: "Reading the launch checklist",
	}}
	m.requests = []channelInterview{{
		ID:        "req-1",
		Kind:      "approval",
		Status:    "pending",
		Title:     "Approve launch copy",
		Question:  "Approve launch copy?",
		From:      "ceo",
		CreatedAt: time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
	}}
	m.tasks = []channelTask{{
		ID:        "task-1",
		Title:     "Ship launch checklist",
		Owner:     "pm",
		Status:    "in_progress",
		ThreadID:  "msg-1",
		UpdatedAt: time.Now().Add(-time.Minute).Format(time.RFC3339),
	}}
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Need launch review.", Timestamp: time.Now().Add(-3 * time.Minute).Format(time.RFC3339)},
		{ID: "msg-2", From: "pm", Content: "Reply in thread", ReplyTo: "msg-1", Timestamp: time.Now().Add(-2 * time.Minute).Format(time.RFC3339)},
	}

	options := m.buildWorkspaceSwitcherOptions()
	byValue := map[string]bool{}
	descriptions := map[string]string{}
	for _, option := range options {
		byValue[option.Value] = true
		descriptions[option.Value] = option.Description
	}

	for _, want := range []string{"request:req-1", "task:task-1", "thread:msg-1"} {
		if !byValue[want] {
			t.Fatalf("expected switcher option %q, got %+v", want, options)
		}
	}
	if !strings.Contains(descriptions["app:messages"], "3 new since you looked") {
		t.Fatalf("expected office feed description to use away summary, got %q", descriptions["app:messages"])
	}
	if !strings.Contains(descriptions["app:recovery"], "Review: Approve launch copy") {
		t.Fatalf("expected recovery description to promote review target, got %q", descriptions["app:recovery"])
	}
	if !strings.Contains(descriptions["app:artifacts"], "Review Approve launch copy") {
		t.Fatalf("expected artifacts description to summarize review/resume work, got %q", descriptions["app:artifacts"])
	}
}

func TestApplyWorkspaceSwitcherSelectionSupportsTaskAndRequestTargets(t *testing.T) {
	m := newChannelModel(false)
	m.tasks = []channelTask{{
		ID:       "task-1",
		Title:    "Ship launch checklist",
		Status:   "in_progress",
		ThreadID: "msg-1",
	}}
	m.requests = []channelInterview{{
		ID:       "req-1",
		Kind:     "approval",
		Status:   "pending",
		Title:    "Approve launch copy",
		Question: "Approve launch copy?",
		From:     "ceo",
	}}

	if cmd := m.applyWorkspaceSwitcherSelection("task:task-1"); cmd == nil {
		t.Fatal("expected task selection to return a poll command")
	}
	if m.activeApp != officeAppTasks || !m.threadPanelOpen || m.threadPanelID != "msg-1" {
		t.Fatalf("expected task selection to focus tasks/thread, got app=%q threadOpen=%v threadID=%q", m.activeApp, m.threadPanelOpen, m.threadPanelID)
	}

	m.threadPanelOpen = false
	m.threadPanelID = ""
	if cmd := m.applyWorkspaceSwitcherSelection("request:req-1"); cmd == nil {
		t.Fatal("expected request selection to return a focus command")
	}
	if m.activeApp != officeAppRequests || m.pending == nil || m.pending.ID != "req-1" {
		t.Fatalf("expected request selection to focus request state, got app=%q pending=%+v", m.activeApp, m.pending)
	}
}

func TestBuildRecoveryLinesIncludesActionCards(t *testing.T) {
	m := newChannelModel(false)
	m.unreadCount = 2
	m.tasks = []channelTask{{
		ID:           "task-1",
		Title:        "Ship launch checklist",
		Details:      "Checklist almost ready for review.",
		Owner:        "pm",
		Status:       "in_progress",
		ThreadID:     "msg-1",
		WorktreePath: "/tmp/wuphf-task-1",
		UpdatedAt:    time.Now().Add(-time.Minute).Format(time.RFC3339),
	}}
	m.requests = []channelInterview{{
		ID:            "req-1",
		Kind:          "approval",
		Status:        "pending",
		Title:         "Approve launch copy",
		Question:      "Approve launch copy?",
		Context:       "Need final sign-off before launch.",
		From:          "ceo",
		Blocking:      true,
		RecommendedID: "approve",
	}}
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Need launch review.", Timestamp: time.Now().Add(-3 * time.Minute).Format(time.RFC3339)},
		{ID: "msg-2", From: "pm", Content: "Reply in thread", ReplyTo: "msg-1", Timestamp: time.Now().Add(-2 * time.Minute).Format(time.RFC3339)},
	}

	lines := m.buildRecoveryLines(96)
	plain := stripANSI(joinRenderedLines(lines))
	var hasTask, hasRequest, hasThread bool
	for _, line := range lines {
		if line.TaskID == "task-1" {
			hasTask = true
		}
		if line.RequestID == "req-1" {
			hasRequest = true
		}
		if line.ThreadID == "msg-1" {
			hasThread = true
		}
	}

	if !strings.Contains(plain, "Resume human decisions") {
		t.Fatalf("expected resume human decisions section, got %q", plain)
	}
	if !strings.Contains(plain, "Resume active tasks") {
		t.Fatalf("expected resume active tasks section, got %q", plain)
	}
	if !strings.Contains(plain, "Return to recent threads") {
		t.Fatalf("expected recent threads section, got %q", plain)
	}
	if !strings.Contains(plain, "Review next") {
		t.Fatalf("expected review-retained section, got %q", plain)
	}
	if !strings.Contains(plain, "Resume next") {
		t.Fatalf("expected resume-retained section, got %q", plain)
	}
	if !hasTask || !hasRequest || !hasThread {
		t.Fatalf("expected clickable recovery lines, got task=%v request=%v thread=%v", hasTask, hasRequest, hasThread)
	}
}
