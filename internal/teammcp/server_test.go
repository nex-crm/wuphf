package teammcp

import "testing"

func TestSuppressBroadcastReasonBlocksOutOfDomainReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"Here is my thought.",
		"",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "We need better launch positioning and campaign messaging."},
		},
		nil,
	)
	if reason == "" {
		t.Fatal("expected FE reply to be suppressed for marketing-only work")
	}
}

func TestSuppressBroadcastReasonAllowsOwnedTaskReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"Shipping the signup work now.",
		"msg-1",
		[]brokerMessage{
			{ID: "msg-1", From: "ceo", Content: "Frontend, take the signup flow."},
		},
		[]brokerTaskSummary{
			{ID: "task-1", Owner: "fe", Status: "in_progress", ThreadID: "msg-1", Title: "Own signup flow"},
		},
	)
	if reason != "" {
		t.Fatalf("expected owned-task reply to be allowed, got %q", reason)
	}
}

func TestSuppressBroadcastReasonBlocksAfterUntargetedCEOReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"I can take this too.",
		"msg-1",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "What should we do here?"},
			{ID: "msg-2", From: "ceo", Content: "PM owns this. Let's keep scope tight.", ReplyTo: "msg-1"},
		},
		nil,
	)
	if reason == "" {
		t.Fatal("expected untargeted post-CEO specialist reply to be suppressed")
	}
}
