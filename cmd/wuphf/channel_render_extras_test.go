package main

import (
	"strings"
	"testing"
)

func TestBuildSkillLinesEmptyShowsCoachingCopy(t *testing.T) {
	lines := buildSkillLines(nil, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "No skills yet") {
		t.Fatalf("expected empty-state copy, got %q", plain)
	}
	if !strings.Contains(plain, "/skill create") {
		t.Fatalf("expected skill creation hint, got %q", plain)
	}
}

func TestBuildSkillLinesRendersAllMetadata(t *testing.T) {
	skills := []channelSkill{{
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
	lines := buildSkillLines(skills, 80)
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
	skills := []channelSkill{{ID: "s1", Title: "x", Status: ""}}
	lines := buildSkillLines(skills, 80)
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
	messages := []brokerMessage{
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
	req := channelInterview{
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
	events := requestCalendarEvents(req, "office", nil)
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
	req := channelInterview{ID: "req-2", From: "ceo", Question: "x", DueAt: "2026-04-29T10:00:00Z"}
	events := requestCalendarEvents(req, "office", nil)
	if len(events) != 1 {
		t.Fatalf("only DueAt set, expected 1 event, got %d", len(events))
	}
	if events[0].Secondary != "due" {
		t.Fatalf("expected due-only event, got %q", events[0].Secondary)
	}
}

func TestRequestCalendarEventsBlankStatusDefaultsToPending(t *testing.T) {
	req := channelInterview{ID: "req-3", From: "ceo", DueAt: "2026-04-29T10:00:00Z"}
	events := requestCalendarEvents(req, "office", nil)
	if len(events) != 1 || events[0].Status != "pending" {
		t.Fatalf("blank status should default to pending, got %#v", events)
	}
}

func TestCalendarParticipantsForRequestUsesRequester(t *testing.T) {
	members := []channelMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "fe", Name: "Frontend"},
	}
	req := channelInterview{ID: "r1", From: "fe", Channel: "office"}
	names := calendarParticipantsForRequest(req, "office", members)
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

	slugs := calendarParticipantSlugsForRequest(req, "office", members)
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
	members := []channelMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "fe", Name: "Frontend"},
	}
	req := channelInterview{ID: "r1", From: "", Channel: "office"}
	names := calendarParticipantsForRequest(req, "office", members)
	// When req has no From, it should still produce some output (channel-wide).
	if len(names) == 0 {
		t.Fatalf("expected channel-wide participants when From is blank, got nothing")
	}
}

func TestContainsStringMatchesTrimmedTarget(t *testing.T) {
	items := []string{" fe ", "be", "ceo"}
	if !containsString(items, "fe") {
		t.Fatalf("expected trimmed match for fe")
	}
	if !containsString(items, "be") {
		t.Fatalf("expected match for be")
	}
	if containsString(items, "pm") {
		t.Fatalf("unexpected match for pm")
	}
	if containsString(nil, "fe") {
		t.Fatalf("nil slice should not match")
	}
}

func TestRenderTimingSummaryJoinsParts(t *testing.T) {
	got := renderTimingSummary("2030-01-01T10:00:00Z", "", "", "")
	if got == "" {
		t.Fatalf("expected non-empty timing summary, got empty")
	}
	if !strings.Contains(got, "due") {
		t.Fatalf("expected 'due' label in timing summary, got %q", got)
	}
}

func TestRenderTimingSummaryAllBlank(t *testing.T) {
	if got := renderTimingSummary("", "", "", ""); got != "" {
		t.Fatalf("blank inputs should yield empty timing summary, got %q", got)
	}
}

func TestPrettyWhenUnparsable(t *testing.T) {
	got := prettyWhen("not-a-time", "due")
	if !strings.Contains(got, "not-a-time") {
		t.Fatalf("unparsable timestamps should fall through, got %q", got)
	}
}

func TestSummarizeUnreadMessagesGroups(t *testing.T) {
	cases := []struct {
		messages []brokerMessage
		want     string
	}{
		{nil, ""},
		{[]brokerMessage{{From: "fe"}}, "1 new from"},
		{[]brokerMessage{{From: "fe"}, {From: "be"}}, " and "},
		{[]brokerMessage{{From: "fe"}, {From: "be"}, {From: "pm"}}, ", and "},
		{[]brokerMessage{{From: ""}, {From: "  "}}, "2 new messages"},
	}
	for _, tc := range cases {
		got := summarizeUnreadMessages(tc.messages)
		if !strings.Contains(got, tc.want) {
			t.Errorf("messages=%v: expected %q in %q", tc.messages, tc.want, got)
		}
	}
}

func TestCountRepliesFollowsNestedThread(t *testing.T) {
	messages := []brokerMessage{
		{ID: "root", From: "ceo"},
		{ID: "r1", From: "fe", ReplyTo: "root", Timestamp: "2026-04-29T10:00:00Z"},
		{ID: "r2", From: "be", ReplyTo: "r1", Timestamp: "2026-04-29T10:05:00Z"},
		{ID: "r3", From: "pm", ReplyTo: "root", Timestamp: "2026-04-29T10:10:00Z"},
	}
	count, last := countReplies(messages, "root")
	if count != 3 {
		t.Fatalf("expected 3 replies counting nested, got %d", count)
	}
	if last == "" {
		t.Fatalf("expected last reply timestamp, got empty")
	}
}

func TestCountRepliesNoReplies(t *testing.T) {
	messages := []brokerMessage{{ID: "root"}}
	count, last := countReplies(messages, "root")
	if count != 0 || last != "" {
		t.Fatalf("expected zero replies for solo message, got count=%d last=%q", count, last)
	}
}

func TestParseTimestampHandlesInvalidString(t *testing.T) {
	if !parseTimestamp("nope").IsZero() {
		t.Fatalf("invalid string should yield zero time")
	}
}

func TestFormatShortTimeFallsBackOnInvalid(t *testing.T) {
	if got := formatShortTime("not-a-time"); got != "" {
		t.Fatalf("expected empty for unparsable short input, got %q", got)
	}
	// Long enough to slice — should return raw HH:MM substring.
	if got := formatShortTime("2026-04-29T15:30:00Z"); got == "" {
		t.Fatalf("expected formatted time for valid RFC3339")
	}
}
