package channelui

import "testing"

// Builds a cyclic ReplyTo graph (A→B→A) and asserts each thread walker
// terminates instead of recursing forever. Without the visited-set
// guards added in this file's sibling sources, every assertion below
// would hang the test runner. The exact return shape is secondary —
// the contract being locked is "broker data with malformed reply
// chains cannot deadlock the renderer."

func cyclicMessages() []BrokerMessage {
	return []BrokerMessage{
		{ID: "a", From: "ceo", ReplyTo: "b", Timestamp: "2026-04-29T10:00:00Z"},
		{ID: "b", From: "pm", ReplyTo: "a", Timestamp: "2026-04-29T10:01:00Z"},
	}
}

func TestThreadRootMessageIDTerminatesOnCycle(t *testing.T) {
	got := ThreadRootMessageID(cyclicMessages(), "a")
	if got == "" {
		t.Fatalf("expected a non-empty root id, got empty")
	}
}

func TestCountRepliesTerminatesOnCycle(t *testing.T) {
	count, _ := CountReplies(cyclicMessages(), "a")
	if count < 0 {
		t.Fatalf("expected non-negative reply count, got %d", count)
	}
}

func TestCountThreadRepliesTerminatesOnCycle(t *testing.T) {
	children := map[string][]BrokerMessage{
		"a": {{ID: "b", ReplyTo: "a"}},
		"b": {{ID: "a", ReplyTo: "b"}},
	}
	if got := CountThreadReplies(children, "a"); got < 0 {
		t.Fatalf("expected non-negative count, got %d", got)
	}
}

func TestThreadParticipantsTerminatesOnCycle(t *testing.T) {
	children := map[string][]BrokerMessage{
		"a": {{ID: "b", From: "pm", ReplyTo: "a"}},
		"b": {{ID: "a", From: "ceo", ReplyTo: "b"}},
	}
	_ = ThreadParticipants(children, "a")
}

func TestFlattenThreadMessagesTerminatesOnCycle(t *testing.T) {
	// The cyclic A↔B graph has no real roots (every message has a
	// parent that exists in byID), so the function returns an empty
	// slice — which is fine. The contract under test here is "no hang":
	// the visited-set guard inside walk prevents infinite recursion if
	// a hypothetical future change re-introduces cyclic descent.
	_ = FlattenThreadMessages(cyclicMessages(), nil)
}

func TestFlattenThreadRepliesTerminatesOnCycle(t *testing.T) {
	_ = FlattenThreadReplies(cyclicMessages(), "a")
}
