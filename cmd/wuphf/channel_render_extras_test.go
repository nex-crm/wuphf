package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func TestBuildSkillLinesEmptyShowsCoachingCopy(t *testing.T) {
	lines := channelui.BuildSkillLines(nil, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "No skills yet") {
		t.Fatalf("expected empty-state copy, got %q", plain)
	}
	if !strings.Contains(plain, "/skill create") {
		t.Fatalf("expected skill creation hint, got %q", plain)
	}
}

func TestBuildSkillLinesRendersAllMetadata(t *testing.T) {
	skills := []channelui.Skill{{
		ID:                  "s1",
		Name:                "summarize",
		Title:               "Summarize PR",
		Description:         "Boil down a pull request to its decisions.",
		Status:              "active",
		UsageCount:          7,
		CreatedBy:           "ceo",
		Tags:                []string{"writing", "code"},
		Trigger:             "@summarize <pr>",
		WorkflowKey:         "summarize-pr",
		WorkflowProvider:    "anthropic",
		WorkflowSchedule:    "0 9 * * *",
		RelayID:             "relay-1",
		RelayPlatform:       "github",
		RelayEventTypes:     []string{"pull_request"},
		LastExecutionAt:     "2026-04-29T08:00:00Z",
		LastExecutionStatus: "success",
	}}
	lines := channelui.BuildSkillLines(skills, 80)
	plain := stripANSI(joinRenderedLines(lines))
	for _, want := range []string{
		"Summarize PR",
		"Boil down a pull request",
		"summarize",
		"7 uses",
		"writing, code",
		"trigger: @summarize <pr>",
		"workflow: summarize-pr via anthropic",
		"schedule: 0 9 * * *",
		"relay: github · pull_request · relay-1",
		"last run: success",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q in skill lines, got %q", want, plain)
		}
	}
}

func TestBuildSkillLinesUnknownStatusFallsBackToActive(t *testing.T) {
	skills := []channelui.Skill{{ID: "s1", Title: "x", Status: ""}}
	lines := channelui.BuildSkillLines(skills, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "active") {
		t.Fatalf("blank status must default to active, got %q", plain)
	}
}

func TestBuildOneOnOneMessageLinesEmptyShowsCoachingCopy(t *testing.T) {
	lines := buildOneOnOneMessageLines(nil, nil, 80, "Frontend", "", 0)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "Frontend") {
		t.Fatalf("expected agent name in empty state, got %q", plain)
	}
	if !strings.Contains(plain, "Suggested:") {
		t.Fatalf("expected suggestion in empty state, got %q", plain)
	}
}

func TestBuildOneOnOneMessageLinesPopulatedDelegatesToOffice(t *testing.T) {
	messages := []channelui.BrokerMessage{
		{ID: "m1", From: "fe", Content: "hello", Timestamp: "2026-04-29T10:00:00Z"},
	}
	lines := buildOneOnOneMessageLines(messages, nil, 80, "Frontend", "", 0)
	if len(lines) == 0 {
		t.Fatalf("expected lines for non-empty 1:1, got nothing")
	}
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "hello") {
		t.Fatalf("expected message content rendered, got %q", plain)
	}
}

func TestRequestCalendarEventsProducesEventForEachTimeField(t *testing.T) {
	req := channelui.Interview{
		ID:         "req-1",
		From:       "ceo",
		Question:   "Approve?",
		Channel:    "office",
		Status:     "open",
		DueAt:      "2026-04-29T10:00:00Z",
		FollowUpAt: "2026-04-29T11:00:00Z",
		ReminderAt: "2026-04-29T12:00:00Z",
		RecheckAt:  "2026-04-29T13:00:00Z",
	}
	events := channelui.RequestCalendarEvents(req, "office", nil)
	if len(events) != 4 {
		t.Fatalf("expected 4 calendar events from 4 time fields, got %d", len(events))
	}
	wantSecondaries := map[string]bool{"due": false, "follow up": false, "reminder": false, "recheck": false}
	for _, ev := range events {
		if ev.Kind != "request" {
			t.Errorf("expected kind=request, got %q", ev.Kind)
		}
		if ev.RequestID != "req-1" {
			t.Errorf("expected request id echoed, got %q", ev.RequestID)
		}
		wantSecondaries[ev.Secondary] = true
	}
	for label, ok := range wantSecondaries {
		if !ok {
			t.Errorf("missing event for %q", label)
		}
	}
}

func TestRequestCalendarEventsSkipsBlankTimestamps(t *testing.T) {
	req := channelui.Interview{ID: "req-2", From: "ceo", Question: "x", DueAt: "2026-04-29T10:00:00Z"}
	events := channelui.RequestCalendarEvents(req, "office", nil)
	if len(events) != 1 {
		t.Fatalf("only DueAt set, expected 1 event, got %d", len(events))
	}
	if events[0].Secondary != "due" {
		t.Fatalf("expected due-only event, got %q", events[0].Secondary)
	}
}

func TestRequestCalendarEventsBlankStatusDefaultsToPending(t *testing.T) {
	req := channelui.Interview{ID: "req-3", From: "ceo", DueAt: "2026-04-29T10:00:00Z"}
	events := channelui.RequestCalendarEvents(req, "office", nil)
	if len(events) != 1 || events[0].Status != "pending" {
		t.Fatalf("blank status should default to pending, got %#v", events)
	}
}

func TestCalendarParticipantsForRequestUsesRequester(t *testing.T) {
	members := []channelui.Member{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "fe", Name: "Frontend"},
	}
	req := channelui.Interview{ID: "r1", From: "fe", Channel: "office"}
	names := channelui.CalendarParticipantsForRequest(req, "office", members)
	if len(names) == 0 {
		t.Fatalf("expected at least one participant name")
	}
	found := false
	for _, name := range names {
		if name == "Frontend" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Frontend in participants, got %v", names)
	}

	slugs := channelui.CalendarParticipantSlugsForRequest(req, "office", members)
	foundSlug := false
	for _, s := range slugs {
		if s == "fe" {
			foundSlug = true
		}
	}
	if !foundSlug {
		t.Fatalf("expected fe in slugs, got %v", slugs)
	}
}

func TestCalendarParticipantsForRequestEmptyFromUsesChannelMembers(t *testing.T) {
	members := []channelui.Member{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "fe", Name: "Frontend"},
	}
	req := channelui.Interview{ID: "r1", From: "", Channel: "office"}
	names := channelui.CalendarParticipantsForRequest(req, "office", members)
	// When req has no From, it should still produce some output (channel-wide).
	if len(names) == 0 {
		t.Fatalf("expected channel-wide participants when From is blank, got nothing")
	}
}

func TestSummarizeUnreadMessagesGroups(t *testing.T) {
	cases := []struct {
		messages []channelui.BrokerMessage
		want     string
	}{
		{nil, ""},
		{[]channelui.BrokerMessage{{From: "fe"}}, "1 new from"},
		{[]channelui.BrokerMessage{{From: "fe"}, {From: "be"}}, " and "},
		{[]channelui.BrokerMessage{{From: "fe"}, {From: "be"}, {From: "pm"}}, ", and "},
		{[]channelui.BrokerMessage{{From: ""}, {From: "  "}}, "2 new messages"},
	}
	for _, tc := range cases {
		got := channelui.SummarizeUnreadMessages(tc.messages)
		if !strings.Contains(got, tc.want) {
			t.Errorf("messages=%v: expected %q in %q", tc.messages, tc.want, got)
		}
	}
}
