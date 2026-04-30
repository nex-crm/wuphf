package channelui

import (
	"testing"
	"time"
)

// Builds a cyclic ReplyTo graph (A→B→A) and asserts each thread walker
// terminates instead of recursing forever. Without the visited-set
// guards added in this file's sibling sources, every assertion below
// would hang the test runner. The exact return shape is secondary —
// the contract being locked is "broker data with malformed reply
// chains cannot deadlock the renderer."
//
// Each call is wrapped in mustReturnWithin so a regression that
// reintroduces unguarded recursion fails the suite within 1s instead
// of blocking until the global `go test -timeout` kicks in.

func cyclicMessages() []BrokerMessage {
	return []BrokerMessage{
		{ID: "a", From: "ceo", ReplyTo: "b", Timestamp: "2026-04-29T10:00:00Z"},
		{ID: "b", From: "pm", ReplyTo: "a", Timestamp: "2026-04-29T10:01:00Z"},
	}
}

// mustReturnWithin runs fn on a goroutine and fails the test if it
// doesn't return inside d. Used by the cycle-regression tests to turn
// "infinite recursion" failures into immediate test failures instead
// of suite-wide hangs. The surviving goroutine on timeout is leaked
// intentionally — that's acceptable because the test process is about
// to fail and exit anyway.
func mustReturnWithin(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return
	case <-time.After(d):
		t.Fatalf("function did not return within %s — likely infinite recursion regression", d)
	}
}

func TestThreadRootMessageIDTerminatesOnCycle(t *testing.T) {
	mustReturnWithin(t, time.Second, func() {
		got := ThreadRootMessageID(cyclicMessages(), "a")
		if got == "" {
			t.Errorf("expected a non-empty root id, got empty")
		}
	})
}

func TestCountRepliesTerminatesOnCycle(t *testing.T) {
	mustReturnWithin(t, time.Second, func() {
		count, _ := CountReplies(cyclicMessages(), "a")
		if count < 0 {
			t.Errorf("expected non-negative reply count, got %d", count)
		}
	})
}

func TestCountThreadRepliesTerminatesOnCycle(t *testing.T) {
	children := map[string][]BrokerMessage{
		"a": {{ID: "b", ReplyTo: "a"}},
		"b": {{ID: "a", ReplyTo: "b"}},
	}
	mustReturnWithin(t, time.Second, func() {
		if got := CountThreadReplies(children, "a"); got < 0 {
			t.Errorf("expected non-negative count, got %d", got)
		}
	})
}

func TestThreadParticipantsTerminatesOnCycle(t *testing.T) {
	children := map[string][]BrokerMessage{
		"a": {{ID: "b", From: "pm", ReplyTo: "a"}},
		"b": {{ID: "a", From: "ceo", ReplyTo: "b"}},
	}
	mustReturnWithin(t, time.Second, func() {
		_ = ThreadParticipants(children, "a")
	})
}

func TestFlattenThreadMessagesTerminatesOnCycle(t *testing.T) {
	// Pure-cycle data (every message has a present parent) used to
	// produce an empty output because no node became a root. The
	// pure-cycle guard now promotes the chronologically-first message
	// to a synthetic root so the thread still renders.
	mustReturnWithin(t, time.Second, func() {
		out := FlattenThreadMessages(cyclicMessages(), nil)
		if len(out) == 0 {
			t.Errorf("pure-cycle thread should still render at least one message")
		}
	})
}

func TestFlattenThreadRepliesTerminatesOnCycle(t *testing.T) {
	mustReturnWithin(t, time.Second, func() {
		_ = FlattenThreadReplies(cyclicMessages(), "a")
	})
}
