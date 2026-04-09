package main

import (
	"strings"
	"testing"
)

func TestBuildNeedsYouLinesPrefersBlockingRequests(t *testing.T) {
	requests := []channelInterview{
		{ID: "req-1", Kind: "approval", Status: "pending", Title: "Optional note", Question: "Optional note?", From: "pm"},
		{ID: "req-2", Kind: "approval", Status: "pending", Title: "Ship launch copy", Question: "Ship launch copy?", Context: "Need approval before publishing.", From: "ceo", Blocking: true, RecommendedID: "approve"},
	}

	lines := buildNeedsYouLines(requests, 96)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Needs attention") {
		t.Fatalf("expected needs-attention separator, got %q", plain)
	}
	if !strings.Contains(plain, "Ship launch copy") {
		t.Fatalf("expected blocking request title, got %q", plain)
	}
	if !strings.Contains(plain, "The team is paused until you answer.") {
		t.Fatalf("expected blocking request guidance, got %q", plain)
	}
}

func TestCurrentMainViewportLinesPrependsNeedsYouStrip(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 40
	m.activeApp = officeAppMessages
	m.requests = []channelInterview{{
		ID:       "req-1",
		Kind:     "approval",
		Status:   "pending",
		Title:    "Approve launch copy",
		Question: "Approve launch copy?",
		Context:  "Need final sign-off before shipping.",
		From:     "ceo",
		Blocking: true,
	}}
	m.messages = []brokerMessage{{ID: "msg-1", From: "pm", Content: "Main feed update."}}

	lines := m.currentMainViewportLines(96, 20)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Approve launch copy") {
		t.Fatalf("expected blocking request strip in main viewport, got %q", plain)
	}
	if !strings.Contains(plain, "Main feed update.") {
		t.Fatalf("expected transcript to remain visible below needs-you strip, got %q", plain)
	}
}
