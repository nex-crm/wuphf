package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func TestNormalizeMailboxScopeAcceptsKnownScopes(t *testing.T) {
	cases := map[string]string{
		"inbox":   "inbox",
		"INBOX":   "inbox",
		" Inbox ": "inbox",
		"outbox":  "outbox",
		"agent":   "agent",
		"":        "",
		"random":  "",
	}
	for input, want := range cases {
		if got := channelui.NormalizeMailboxScope(input); got != want {
			t.Errorf("channelui.NormalizeMailboxScope(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMailboxOutboxOnlyOwnedByViewer(t *testing.T) {
	msg := channelui.BrokerMessage{ID: "m1", From: "fe", Content: "hi"}
	if !channelui.MailboxMessageBelongsToViewerOutbox(msg, "fe") {
		t.Fatalf("viewer's own message should be in their outbox")
	}
	if channelui.MailboxMessageBelongsToViewerOutbox(msg, "be") {
		t.Fatalf("other-author message should not be in viewer outbox")
	}
	if channelui.MailboxMessageBelongsToViewerOutbox(msg, "") {
		t.Fatalf("empty viewer should never match")
	}
}

func TestMailboxInboxIncludesHumanAndDirectTags(t *testing.T) {
	idx := map[string]channelui.BrokerMessage{}

	human := channelui.BrokerMessage{ID: "m1", From: "human", Content: "ping"}
	if !channelui.MailboxMessageBelongsToViewerInbox(human, "fe", idx) {
		t.Fatalf("messages from human should land in any agent's inbox")
	}

	directTag := channelui.BrokerMessage{ID: "m2", From: "ceo", Content: "do this", Tagged: []string{"fe"}}
	if !channelui.MailboxMessageBelongsToViewerInbox(directTag, "fe", idx) {
		t.Fatalf("messages tagging the viewer should land in their inbox")
	}

	allTag := channelui.BrokerMessage{ID: "m3", From: "ceo", Content: "team-wide", Tagged: []string{"all"}}
	if !channelui.MailboxMessageBelongsToViewerInbox(allTag, "fe", idx) {
		t.Fatalf("messages tagged @all should land in every viewer's inbox")
	}

	own := channelui.BrokerMessage{ID: "m4", From: "fe", Content: "self"}
	if channelui.MailboxMessageBelongsToViewerInbox(own, "fe", idx) {
		t.Fatalf("own messages must not appear in viewer's inbox lane")
	}

	other := channelui.BrokerMessage{ID: "m5", From: "be", Content: "tagged elsewhere", Tagged: []string{"pm"}}
	if channelui.MailboxMessageBelongsToViewerInbox(other, "fe", idx) {
		t.Fatalf("messages tagging someone else should not be in viewer's inbox")
	}
}

func TestMailboxInboxFollowsThreadReplies(t *testing.T) {
	root := channelui.BrokerMessage{ID: "root", From: "fe", Content: "viewer wrote this"}
	reply := channelui.BrokerMessage{ID: "r1", From: "be", Content: "reply", ReplyTo: "root"}
	idx := map[string]channelui.BrokerMessage{"root": root, "r1": reply}

	if !channelui.MailboxMessageBelongsToViewerInbox(reply, "fe", idx) {
		t.Fatalf("a reply to viewer's message should be inbox-bound")
	}

	// Cycle protection: msg replies to itself.
	cycle := channelui.BrokerMessage{ID: "c1", From: "be", Content: "loop", ReplyTo: "c1"}
	cycleIdx := map[string]channelui.BrokerMessage{"c1": cycle}
	if channelui.MailboxMessageBelongsToViewerInbox(cycle, "fe", cycleIdx) {
		t.Fatalf("self-cycle reply should not match anyone's inbox")
	}
}

func TestFilterMessagesForViewerScopeUnknownScopeReturnsCopy(t *testing.T) {
	msgs := []channelui.BrokerMessage{{ID: "a", From: "fe"}, {ID: "b", From: "be"}}
	got := channelui.FilterMessagesForViewerScope(msgs, "fe", "")
	if len(got) != 2 {
		t.Fatalf("empty scope should pass everything through, got %d", len(got))
	}
	// Verify it returns a copy, not the same backing array.
	got[0].ID = "mutated"
	if msgs[0].ID == "mutated" {
		t.Fatalf("channelui.FilterMessagesForViewerScope must not share backing array")
	}
}

func TestFilterMessagesForViewerScopeAgentMode(t *testing.T) {
	msgs := []channelui.BrokerMessage{
		{ID: "a", From: "fe", Content: "viewer wrote"},                   // outbox
		{ID: "b", From: "human", Content: "human ping"},                  // inbox (human)
		{ID: "c", From: "be", Content: "tag fe", Tagged: []string{"fe"}}, // inbox (tagged)
		{ID: "d", From: "be", Content: "for someone else", Tagged: []string{"pm"}},
	}
	got := channelui.FilterMessagesForViewerScope(msgs, "fe", "agent")
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["a"] || !ids["b"] || !ids["c"] {
		t.Fatalf("agent scope should include outbox+inbox, got %v", ids)
	}
	if ids["d"] {
		t.Fatalf("agent scope should exclude unrelated message, got %v", ids)
	}
}

func TestBuildInboxLinesEmptyShowsCoachingCopy(t *testing.T) {
	lines := buildInboxLines(nil, nil, 80)
	joined := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(joined, "Inbox") {
		t.Fatalf("inbox header missing: %q", joined)
	}
	if !strings.Contains(joined, "Nothing is waiting in the inbox lane") {
		t.Fatalf("empty-state coaching copy missing: %q", joined)
	}
}

func TestBuildInboxLinesShowsRequestsAndMessages(t *testing.T) {
	requests := []channelui.Interview{{
		ID:        "req-1",
		Kind:      "decision",
		From:      "ceo",
		Question:  "Approve the launch?",
		Context:   "Need green light",
		CreatedAt: "2026-04-29T10:00:00Z",
	}}
	messages := []channelui.BrokerMessage{{
		ID:        "m1",
		From:      "ceo",
		Content:   "FYI here is the plan",
		Timestamp: "2026-04-29T10:00:00Z",
	}}
	lines := buildInboxLines(messages, requests, 80)
	joined := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(joined, "Open requests") {
		t.Fatalf("expected Open requests separator, got %q", joined)
	}
	if !strings.Contains(joined, "Approve the launch?") {
		t.Fatalf("expected request question to render, got %q", joined)
	}
	if !strings.Contains(joined, "Inbox messages") {
		t.Fatalf("expected Inbox messages separator, got %q", joined)
	}
	if !strings.Contains(joined, "FYI here is the plan") {
		t.Fatalf("expected message body to render, got %q", joined)
	}
}

func TestBuildOutboxLinesEmptyShowsCoachingCopy(t *testing.T) {
	lines := buildOutboxLines(nil, nil, 80)
	joined := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(joined, "Outbox") {
		t.Fatalf("outbox header missing: %q", joined)
	}
	if !strings.Contains(joined, "Nothing is in the outbox yet") {
		t.Fatalf("empty-state coaching copy missing: %q", joined)
	}
}

func TestBuildOutboxLinesShowsAuthoredMessagesAndActions(t *testing.T) {
	messages := []channelui.BrokerMessage{{
		ID:        "m1",
		From:      "fe",
		Content:   "Shipped the homepage update",
		Timestamp: "2026-04-29T10:00:00Z",
	}}
	actions := []channelui.Action{{
		ID:        "a1",
		Kind:      "github_pr_opened",
		Summary:   "Opened PR #42",
		Actor:     "fe",
		Source:    "github",
		CreatedAt: "2026-04-29T10:05:00Z",
	}}
	lines := buildOutboxLines(messages, actions, 80)
	joined := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(joined, "Authored messages") {
		t.Fatalf("expected Authored messages separator, got %q", joined)
	}
	if !strings.Contains(joined, "Shipped the homepage update") {
		t.Fatalf("expected message body, got %q", joined)
	}
	if !strings.Contains(joined, "Recent actions") {
		t.Fatalf("expected Recent actions separator, got %q", joined)
	}
	if !strings.Contains(joined, "Opened PR #42") {
		t.Fatalf("expected action summary, got %q", joined)
	}
}
