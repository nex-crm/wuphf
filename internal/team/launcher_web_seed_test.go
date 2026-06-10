package team

import (
	"net"
	"strings"
	"testing"
	"time"
)

// TestSkipTaskSeedsWelcomeOnly asserts the post-R6 onboarding invariant:
// when the wizard finishes with skip_task=true, #general lands with the
// system welcome and NO staged agent presence line. The demo_seed
// machinery was removed (core-loop R6) — the loop wants a real first
// paint, so any reappearance of a synthetic agent post here is a
// regression.
func TestSkipTaskSeedsWelcomeOnly(t *testing.T) {
	ensureOperationsFallbackFS(t)
	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("", true, "niche-crm", nil, ""); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	msgs := b.ChannelMessages("general")
	var welcome *channelMessage
	for i := range msgs {
		m := &msgs[i]
		if m.Kind == "system" && strings.Contains(m.Content, "Welcome to your office") {
			welcome = m
		}
		if m.Kind == "demo_seed" {
			t.Errorf("demo_seed message seeded in #general after R6 removal: %+v", *m)
		}
		if m.From != "system" {
			t.Errorf("expected only system messages in #general on skip_task; got From=%q: %+v", m.From, *m)
		}
	}
	if welcome == nil {
		t.Fatalf("expected system welcome in #general; got %d messages: %+v", len(msgs), msgs)
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
