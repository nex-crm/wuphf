package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func TestFlattenThreadRepliesNestedDepth(t *testing.T) {
	messages := []channelui.BrokerMessage{
		{ID: "root", From: "ceo", Content: "Should we ship?"},
		{ID: "r1", From: "fe", Content: "Yes", ReplyTo: "root"},
		{ID: "r1a", From: "be", Content: "Agree", ReplyTo: "r1"},
		{ID: "r2", From: "pm", Content: "Wait", ReplyTo: "root"},
		{ID: "unrelated", From: "cmo", Content: "Different topic"},
	}
	out := channelui.FlattenThreadReplies(messages, "root")
	if len(out) != 3 {
		t.Fatalf("expected 3 thread replies, got %d", len(out))
	}
	if out[0].Message.ID != "r1" || out[0].Depth != 0 {
		t.Fatalf("expected first reply r1 at depth 0, got %#v", out[0])
	}
	if out[1].Message.ID != "r1a" || out[1].Depth != 1 || out[1].ParentLabel != "@fe" {
		t.Fatalf("expected nested r1a depth 1 under @fe, got %#v", out[1])
	}
	if out[2].Message.ID != "r2" || out[2].Depth != 0 {
		t.Fatalf("expected sibling r2 depth 0, got %#v", out[2])
	}
}

func TestFlattenThreadRepliesUnknownParentReturnsNothing(t *testing.T) {
	messages := []channelui.BrokerMessage{
		{ID: "a", From: "fe", Content: "hi"},
		{ID: "b", From: "be", Content: "reply", ReplyTo: "a"},
	}
	out := channelui.FlattenThreadReplies(messages, "missing")
	if len(out) != 0 {
		t.Fatalf("unknown parent should yield zero replies, got %d", len(out))
	}
}

func TestRenderThreadReplyContainsAuthorAndBody(t *testing.T) {
	reply := channelui.ThreadedMessage{
		Message: channelui.BrokerMessage{
			ID:        "r1",
			From:      "fe",
			Content:   "Looks good",
			Timestamp: "2026-04-29T10:00:00Z",
		},
		Depth:       1,
		ParentLabel: "@ceo",
	}
	lines := channelui.RenderThreadReply(reply, 60)
	plain := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(strings.ToLower(plain), "frontend") {
		t.Fatalf("expected author label in reply, got %q", plain)
	}
	if !strings.Contains(plain, "Looks good") {
		t.Fatalf("expected reply body, got %q", plain)
	}
	if !strings.Contains(plain, "↳") {
		t.Fatalf("expected nested reply marker, got %q", plain)
	}
	if !strings.Contains(plain, "reply to @ceo") {
		t.Fatalf("expected parent label, got %q", plain)
	}
}

func TestRenderThreadRepliesMultipleProducesLines(t *testing.T) {
	replies := []channelui.ThreadedMessage{
		{Message: channelui.BrokerMessage{ID: "r1", From: "fe", Content: "A"}, Depth: 0, ParentLabel: "@ceo"},
		{Message: channelui.BrokerMessage{ID: "r2", From: "be", Content: "B"}, Depth: 0, ParentLabel: "@ceo"},
	}
	lines := channelui.RenderThreadReplies(replies, 60)
	if len(lines) == 0 {
		t.Fatalf("expected lines for replies")
	}
	plain := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "A") || !strings.Contains(plain, "B") {
		t.Fatalf("expected both reply bodies, got %q", plain)
	}
}

func TestRenderThreadRepliesEmptyReturnsNil(t *testing.T) {
	if got := channelui.RenderThreadReplies(nil, 60); got != nil {
		t.Fatalf("empty replies should return nil, got %v", got)
	}
}

func TestRenderThreadMessageHasAvatarAndBody(t *testing.T) {
	msg := channelui.BrokerMessage{
		ID:        "m1",
		From:      "ceo",
		Content:   "Approve the launch",
		Timestamp: "2026-04-29T10:00:00Z",
	}
	lines := channelui.RenderThreadMessage(msg, 60, true)
	plain := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Approve the launch") {
		t.Fatalf("expected message body, got %q", plain)
	}
}

func TestRenderThreadInputPlaceholderWhenEmpty(t *testing.T) {
	got := renderThreadInput(nil, 0, 30, false, false)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Reply") {
		t.Fatalf("expected Reply label, got %q", plain)
	}
	if !strings.Contains(plain, "Reply in thread") {
		t.Fatalf("expected placeholder copy, got %q", plain)
	}
}

func TestRenderThreadInputRendersExistingInput(t *testing.T) {
	input := []rune("ship it")
	got := renderThreadInput(input, len(input), 30, true, true)
	plain := stripANSI(got)
	if !strings.Contains(plain, "ship it") {
		t.Fatalf("expected user input echoed, got %q", plain)
	}
}

func TestRenderThreadPanelMissingParentShowsNotice(t *testing.T) {
	got := renderThreadPanel(nil, "missing", 60, 20, nil, 0, 0, "", false, false)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Thread message not found") {
		t.Fatalf("expected missing-parent notice, got %q", plain)
	}
}

func TestRenderThreadPanelRendersParentAndReplies(t *testing.T) {
	messages := []channelui.BrokerMessage{
		{ID: "root", From: "ceo", Content: "Approve?", Timestamp: "2026-04-29T10:00:00Z"},
		{ID: "r1", From: "fe", Content: "Approved", ReplyTo: "root", Timestamp: "2026-04-29T10:01:00Z"},
	}
	got := renderThreadPanel(messages, "root", 60, 20, nil, 0, 0, "", true, false)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Approve?") {
		t.Fatalf("expected parent body, got %q", plain)
	}
	if !strings.Contains(plain, "Approved") {
		t.Fatalf("expected reply body, got %q", plain)
	}
	if !strings.Contains(plain, "1 reply") {
		t.Fatalf("expected reply count divider, got %q", plain)
	}
}

func TestRenderThreadPanelTooSmallReturnsEmpty(t *testing.T) {
	if got := renderThreadPanel(nil, "x", 4, 2, nil, 0, 0, "", false, false); got != "" {
		t.Fatalf("undersized panel should return empty string, got %q", got)
	}
}
