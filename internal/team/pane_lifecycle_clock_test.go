package team

// Tests for the C5f clock-injected paneLifecycle methods. The 1.5s
// sleep in DetectDeadPanesAfterSpawn and the 1s sleeps in
// PrimeVisibleAgents previously made these methods un-testable
// without time.Sleep in the test (the hard "no sleeps in tests"
// rule). Now that paneLifecycle takes a clock interface the tests
// drive them through manualClock.Advance — same pattern as
// scheduler_test.go.

import (
	"testing"
	"time"
)

func TestDetectDeadPanesAfterSpawn_RecordsDeadPanesUnderManualClock(t *testing.T) {
	fake := newFakeTmuxRunner()
	// First display-message returns "1" (dead); capture-pane returns the
	// last 200 lines as the snippet. fakeTmuxRunner keys off the
	// sub-command so we can return both via the same call sequence.
	fake.outputs["display-message"] = []byte("1\n")
	fake.outputs["capture-pane"] = []byte("error: model unavailable\n")
	setTmuxRunnerForTest(t, fake)

	posted := 0
	recorded := map[string]string{}
	deps := paneLifecycleDeps{
		agentPaneTargets: func() map[string]notificationTarget {
			return map[string]notificationTarget{
				"ceo": {PaneTarget: "wuphf-team:team.1"},
				"fe":  {PaneTarget: "wuphf-team:team.2"},
			}
		},
		recordFailure: func(slug, reason string) {
			recorded[slug] = reason
		},
		postSystemMessage: func(channel, body, kind string) { posted++ },
	}

	clk := newManualClock(time.Unix(0, 0))
	pl := newPaneLifecycleWithDeps("wuphf-team", deps).withClock(clk)

	// Run DetectDeadPanesAfterSpawn in a goroutine so we can advance the
	// clock past its 1500ms guard without the test goroutine blocking.
	done := make(chan struct{})
	go func() {
		pl.DetectDeadPanesAfterSpawn([]officeMember{{Slug: "ceo"}, {Slug: "fe"}})
		close(done)
	}()
	// Wait for the After registration before advancing — same pattern
	// as scheduler_test.go (race-free synchronization).
	<-clk.registered
	clk.Advance(1500 * time.Millisecond)
	<-done

	// Both panes are reported dead -> both should be recorded + each
	// should have posted a system message.
	if len(recorded) != 2 {
		t.Errorf("recorded = %v, want 2 entries (ceo, fe)", recorded)
	}
	if posted != 2 {
		t.Errorf("postSystemMessage calls = %d, want 2", posted)
	}
	for _, slug := range []string{"ceo", "fe"} {
		if reason, ok := recorded[slug]; !ok {
			t.Errorf("recordFailure missing for %s", slug)
		} else if !stringContains(reason, "pane died on launch") {
			t.Errorf("recordFailure[%s] = %q, want 'pane died on launch...' prefix", slug, reason)
		}
	}
}

func TestPrimeVisibleAgents_NoTargetsExitsImmediately(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	deps := paneLifecycleDeps{
		agentPaneTargets: func() map[string]notificationTarget { return nil },
	}

	clk := newManualClock(time.Unix(0, 0))
	pl := newPaneLifecycleWithDeps("wuphf-team", deps).withClock(clk)

	done := make(chan struct{})
	go func() {
		pl.PrimeVisibleAgents()
		close(done)
	}()
	<-clk.registered
	clk.Advance(1 * time.Second)
	<-done

	// Empty agentPaneTargets short-circuits — no capture-pane / send-keys
	// should fire.
	if got := len(fake.callsFor("capture-pane")); got != 0 {
		t.Errorf("capture-pane calls = %d, want 0 (no targets)", got)
	}
	if got := len(fake.callsFor("send-keys")); got != 0 {
		t.Errorf("send-keys calls = %d, want 0", got)
	}
}

func TestPrimeVisibleAgents_SendsEnterWhenPaneNeedsPriming(t *testing.T) {
	fake := newFakeTmuxRunner()
	// First capture returns the trust-prompt text; second returns clean.
	// fakeTmuxRunner is keyed off subcommand so all capture-pane calls
	// share the same canned output. To get the "first prime, then ready"
	// flow we'd need a sequenced fake — overkill for this test. Instead,
	// pin the simpler invariant: when capture returns priming text, we
	// send Enter, and we keep doing it for up to 3 attempts.
	fake.outputs["capture-pane"] = []byte("Trust this folder?")
	setTmuxRunnerForTest(t, fake)

	deps := paneLifecycleDeps{
		agentPaneTargets: func() map[string]notificationTarget {
			return map[string]notificationTarget{
				"ceo": {PaneTarget: "wuphf-team:team.1"},
			}
		},
	}

	clk := newManualClock(time.Unix(0, 0))
	pl := newPaneLifecycleWithDeps("wuphf-team", deps).withClock(clk)

	done := make(chan struct{})
	go func() {
		pl.PrimeVisibleAgents()
		close(done)
	}()

	// First sleep: 1s warmup. PrimeVisibleAgents loops up to 3 times,
	// sleeping 1s between iterations when not ready. So the goroutine
	// will register at most 4 sleeps total (warmup + 3 between). Drive
	// each in turn.
	for i := 0; i < 4; i++ {
		select {
		case <-clk.registered:
		case <-time.After(2 * time.Second):
			t.Fatalf("never registered sleeper %d", i)
		}
		clk.Advance(1 * time.Second)
	}
	<-done

	sends := fake.callsFor("send-keys")
	if len(sends) != 3 {
		// Each of the 3 attempts saw priming text and sent Enter.
		t.Errorf("send-keys calls = %d, want 3 (3 attempts × 1 priming pane)", len(sends))
	}
	for _, call := range sends {
		if len(call) < 4 || call[3] != "Enter" {
			t.Errorf("send-keys args = %v, want last arg Enter", call)
		}
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
