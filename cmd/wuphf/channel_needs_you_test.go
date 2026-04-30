package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func TestCurrentMainViewportLinesPrependsNeedsYouStrip(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 40
	m.activeApp = channelui.OfficeAppMessages
	m.requests = []channelui.Interview{{
		ID:       "req-1",
		Kind:     "approval",
		Status:   "pending",
		Title:    "Approve launch copy",
		Question: "Approve launch copy?",
		Context:  "Need final sign-off before shipping.",
		From:     "ceo",
		Blocking: true,
	}}
	m.messages = []channelui.BrokerMessage{{ID: "msg-1", From: "pm", Content: "Main feed update."}}

	lines := m.currentMainViewportLines(96, 20)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Approve launch copy") {
		t.Fatalf("expected blocking request strip in main viewport, got %q", plain)
	}
	if !strings.Contains(plain, "Main feed update.") {
		t.Fatalf("expected transcript to remain visible below needs-you strip, got %q", plain)
	}
}
