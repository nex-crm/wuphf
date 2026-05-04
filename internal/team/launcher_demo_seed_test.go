package team

import (
	"net"
	"strings"
	"testing"
	"time"
)

// TestSkipTaskSeedsWelcomeAndPresence asserts the central onboarding
// invariant: when the wizard finishes with skip_task=true, #general lands
// with two messages — a system welcome and a lead presence line marked
// Kind="demo_seed". Without coverage here a future change to
// postKickoffLocked could silently drop one or both and the channel would
// look empty on first paint without anything in CI catching it.
func TestSkipTaskSeedsWelcomeAndPresence(t *testing.T) {
	ensureOperationsFallbackFS(t)
	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("", true, "niche-crm", nil, ""); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	msgs := b.ChannelMessages("general")
	var welcome, presence *channelMessage
	for i := range msgs {
		m := &msgs[i]
		if m.Kind == "system" && strings.Contains(m.Content, "Welcome to your office") {
			welcome = m
		}
		if m.Kind == "demo_seed" {
			presence = m
		}
	}
	if welcome == nil {
		t.Fatalf("expected system welcome in #general; got %d messages: %+v", len(msgs), msgs)
	}
	if presence == nil {
		t.Fatalf("expected demo_seed presence line in #general; got %d messages: %+v", len(msgs), msgs)
	}
	if presence.From == "system" {
		t.Errorf("presence line must be From=lead, not system; got From=%q", presence.From)
	}
	// Tagged: [] (not nil) — the empty-but-non-nil shape is what keeps
	// content-routing from picking up the line as an implicit @mention.
	if presence.Tagged == nil || len(presence.Tagged) != 0 {
		t.Errorf("presence line Tagged must be []; got %#v", presence.Tagged)
	}
}

// TestDeliverMessageNotificationSkipsDemoSeed asserts the central guard:
// demo_seed messages must never reach notificationTargetsForMessage. The
// filter lives in deliverMessageNotification (not notifyAgentsLoop) so any
// caller — including primeVisibleAgents and replays — is protected.
//
// We assert by calling deliverMessageNotification directly with a
// demo_seed message and confirming notifyLastDelivered stays empty: the
// real targets path always touches that map before sending, so an empty
// map after the call proves the early-return fired.
func TestDeliverMessageNotificationSkipsDemoSeed(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "Lead", BuiltIn: true},
		{Slug: "builder", Name: "Builder", Role: "Builder"},
	}
	b.mu.Unlock()
	l := &Launcher{broker: b}

	l.deliverMessageNotification(channelMessage{
		ID:      "msg-1",
		From:    "ceo",
		Channel: "general",
		Kind:    "demo_seed",
		Content: "CEO online. Drop a directive in the composer.",
		Tagged:  []string{},
	})

	l.notifyMu.Lock()
	delivered := len(l.notifyLastDelivered)
	l.notifyMu.Unlock()
	if delivered != 0 {
		t.Fatalf("demo_seed message must not record a delivery; got %d entries: %+v", delivered, l.notifyLastDelivered)
	}
}

// TestBuildNotificationContextExcludesDemoSeed asserts that the cosmetic
// CEO presence line never appears in the prompt context shown to agents.
// Without this filter, replaying a synthetic onboarding line as if it were
// real channel history would let agents react to the seed as a directive.
func TestBuildNotificationContextExcludesDemoSeed(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "Lead", BuiltIn: true},
	}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = []string{"ceo"}
		}
	}
	b.appendMessageLocked(channelMessage{
		ID:      "msg-seed",
		From:    "ceo",
		Channel: "general",
		Kind:    "demo_seed",
		Content: "CEO online. Drop a directive in the composer.",
	})
	b.appendMessageLocked(channelMessage{
		ID:      "msg-real",
		From:    "you",
		Channel: "general",
		Content: "Real human message that should appear.",
	})
	b.mu.Unlock()
	l := &Launcher{broker: b}

	ctx := l.buildNotificationContext("general", "", "", 5)
	if !strings.Contains(ctx, "Real human message") {
		t.Fatalf("expected the real human message in context; got %q", ctx)
	}
	if strings.Contains(ctx, "CEO online") {
		t.Fatalf("demo_seed content leaked into prompt context: %q", ctx)
	}
}

// TestServeWebUIReturnsErrorOnBoundPort asserts that ServeWebUI surfaces a
// port-conflict error synchronously rather than swallowing it inside the
// goroutine. Pre-fix this was a log.Printf that left the launcher claiming
// success while the listener was dead.
func TestServeWebUIReturnsErrorOnBoundPort(t *testing.T) {
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer hold.Close()
	port := hold.Addr().(*net.TCPAddr).Port

	b := newTestBroker(t)
	if err := b.ServeWebUI(port); err == nil {
		t.Fatalf("ServeWebUI on busy port %d returned nil error", port)
	}
}

// TestWaitForWebReadyTimesOutOnDeadAddr asserts the negative-path return
// value the launcher relies on to skip openBrowser. Picks an unbound port
// (closed-and-released) and a tight ceiling so the test stays sub-second.
func TestWaitForWebReadyTimesOutOnDeadAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	start := time.Now()
	if waitForWebReady(addr, 200*time.Millisecond) {
		t.Fatalf("waitForWebReady on dead %s returned true", addr)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("waitForWebReady took %v on dead addr; expected ≤ ~timeout", elapsed)
	}
}

// TestWaitForWebReadyReturnsTrueOnLiveAddr asserts the positive-path
// return so we know the bool gate distinguishes the two states (otherwise
// "always returns false" would silently pass the negative test alone).
func TestWaitForWebReadyReturnsTrueOnLiveAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	if !waitForWebReady(addr, 2*time.Second) {
		t.Fatalf("waitForWebReady on live %s returned false", addr)
	}
}
