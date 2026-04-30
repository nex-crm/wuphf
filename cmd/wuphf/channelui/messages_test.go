package channelui

import "testing"

func TestCountRepliesFollowsNestedThread(t *testing.T) {
	messages := []BrokerMessage{
		{ID: "root", From: "ceo"},
		{ID: "r1", From: "fe", ReplyTo: "root", Timestamp: "2026-04-29T10:00:00Z"},
		{ID: "r2", From: "be", ReplyTo: "r1", Timestamp: "2026-04-29T10:05:00Z"},
		{ID: "r3", From: "pm", ReplyTo: "root", Timestamp: "2026-04-29T10:10:00Z"},
	}
	count, last := CountReplies(messages, "root")
	if count != 3 {
		t.Fatalf("expected 3 replies counting nested, got %d", count)
	}
	if last == "" {
		t.Fatalf("expected last reply timestamp, got empty")
	}
}

func TestCountRepliesNoReplies(t *testing.T) {
	messages := []BrokerMessage{{ID: "root"}}
	count, last := CountReplies(messages, "root")
	if count != 0 || last != "" {
		t.Fatalf("expected zero replies for solo message, got count=%d last=%q", count, last)
	}
}

func TestParseTimestampHandlesInvalidString(t *testing.T) {
	if !ParseTimestamp("nope").IsZero() {
		t.Fatalf("invalid string should yield zero time")
	}
}

func TestFormatShortTimeFallsBackOnInvalid(t *testing.T) {
	if got := FormatShortTime("not-a-time"); got != "" {
		t.Fatalf("expected empty for unparsable short input, got %q", got)
	}
	if got := FormatShortTime("2026-04-29T15:30:00Z"); got == "" {
		t.Fatalf("expected formatted time for valid RFC3339")
	}
}
