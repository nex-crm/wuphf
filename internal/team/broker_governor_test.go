package team

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

type fakeHeadlessController struct {
	mu        chan string // buffered; receives the slug passed to Cancel
	cancelled []string
}

func (f *fakeHeadlessController) CancelHeadlessTurns(slug string) {
	f.cancelled = append(f.cancelled, slug)
	select {
	case f.mu <- slug:
	default:
	}
}

func newGovernorTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	return b
}

// TestGovernorNoteTurnAutoPauses is the regression test for the core gap: the
// dispatch loop running with no cumulative checkpoint. With a 1-turn gate, the
// first completed turn must pause dispatch and emit an SSE governor event.
func TestGovernorNoteTurnAutoPauses(t *testing.T) {
	b := newGovernorTestBroker(t)
	b.governor = newGovernor(governorConfig{MaxTurnsPerGate: 1}, 0, 0)

	events, unsub := b.SubscribeGovernor(4)
	defer unsub()

	if b.GovernorStatus().Paused {
		t.Fatalf("should start unpaused")
	}
	b.governorNoteTurn()

	if !b.GovernorStatus().Paused {
		t.Fatalf("expected pause after the turn-count gate tripped")
	}
	select {
	case st := <-events:
		if !st.Paused || st.Reason != pauseTurns {
			t.Fatalf("governor event = %+v, want paused/turns", st)
		}
	case <-time.After(time.Second):
		t.Fatalf("no governor SSE event after auto-pause")
	}
}

func TestGovernorBudgetUsesRecordedUsage(t *testing.T) {
	b := newGovernorTestBroker(t)
	b.governor = newGovernor(governorConfig{MaxSessionTokens: 1000}, 0, 0)

	// Seed session usage through the real accounting path.
	b.RecordAgentUsage("ceo", "claude", provider.ClaudeUsage{InputTokens: 600, OutputTokens: 600})
	b.governorNoteTurn()
	if !b.GovernorStatus().Paused {
		t.Fatalf("expected budget pause once session usage (1200) crossed 1000")
	}
	st := b.GovernorStatus()
	if st.Reason != pauseBudget {
		t.Fatalf("reason = %q, want budget", st.Reason)
	}
	if st.TokensSinceCheckpoint < 1000 {
		t.Fatalf("tokensSinceCheckpoint = %d, want >= 1000", st.TokensSinceCheckpoint)
	}
}

func TestGovernorStopCancelsInFlight(t *testing.T) {
	b := newGovernorTestBroker(t)
	ctl := &fakeHeadlessController{mu: make(chan string, 1)}
	b.SetHeadlessDispatchController(ctl)

	b.GovernorStop("ceo")
	if !b.GovernorStatus().Paused {
		t.Fatalf("stop should pause dispatch")
	}
	if len(ctl.cancelled) != 1 || ctl.cancelled[0] != "ceo" {
		t.Fatalf("controller cancel = %v, want [ceo]", ctl.cancelled)
	}
}

func TestGovernorResumeClearsPause(t *testing.T) {
	b := newGovernorTestBroker(t)
	b.governor = newGovernor(governorConfig{MaxTurnsPerGate: 1}, 0, 0)
	b.governorNoteTurn()
	if !b.GovernorStatus().Paused {
		t.Fatalf("precondition: should be paused")
	}
	b.GovernorResume()
	st := b.GovernorStatus()
	if st.Paused {
		t.Fatalf("resume should clear pause")
	}
	if st.TurnsSinceCheckpoint != 0 {
		t.Fatalf("resume should reset turn count, got %d", st.TurnsSinceCheckpoint)
	}
}

// TestGovernorCountsOnlyRealTurns is the regression test for the phantom-turn
// bug: the dispatch worker's idle-drain iteration (beginHeadlessCodexTurn !ok)
// must NOT be credited to the governor. Two real turns must count as exactly
// two, not three (two turns + the trailing empty-queue drain).
func TestGovernorCountsOnlyRealTurns(t *testing.T) {
	processed := make(chan string, 4)
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, _ context.Context, _ string, notification string, _ ...string) error {
		processed <- notification
		return nil
	})

	l := newHeadlessLauncherForTest(t)
	l.broker = newGovernorTestBroker(t)
	// High threshold so the count is observable without an auto-pause racing in.
	l.broker.governor = newGovernor(governorConfig{MaxTurnsPerGate: 100}, 0, 0)

	l.enqueueHeadlessCodexTurn("fe", "first")
	l.enqueueHeadlessCodexTurn("fe", "second")
	waitForString(t, processed)
	waitForString(t, processed)

	// Let the worker drain NATURALLY to empty (the idle-drain iteration) rather
	// than calling stopHeadlessWorkers, which would preempt that iteration at
	// the top-of-loop stop check and hide the phantom count. We wait until the
	// worker has deregistered itself (set inside beginHeadlessCodexTurn on the
	// empty queue), then join its goroutine so any trailing governorNoteTurn has
	// run before we read the count.
	deadline := time.Now().Add(2 * time.Second)
	for {
		l.headless.mu.Lock()
		_, running := l.headless.workers["fe"]
		l.headless.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not drain to idle in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
	l.stopHeadlessWorkers() // join the (already-exiting) worker goroutine

	if got := l.broker.GovernorStatus().TurnsSinceCheckpoint; got != 2 {
		t.Fatalf("governor counted %d turns, want exactly 2 (idle drain must not count)", got)
	}
}

func TestHandleGovernorRejectsOutOfRangeBump(t *testing.T) {
	b := newGovernorTestBroker(t)
	b.governor = newGovernor(governorConfig{MaxSessionTokens: 1000}, 0, 0)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"valid resume_more", `{"action":"resume_more","addTokens":5000}`, http.StatusOK},
		{"absurd cost (would set +Inf)", `{"action":"resume_more","addCostUsd":1e308}`, http.StatusBadRequest},
		{"absurd tokens (overflow risk)", `{"action":"resume_more","addTokens":2000000000}`, http.StatusBadRequest},
		{"negative tokens", `{"action":"resume_more","addTokens":-1}`, http.StatusBadRequest},
		{"unknown action", `{"action":"nuke"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/governor", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			b.handleGovernor(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	// The absurd-cost request must NOT have crippled the cost gate.
	if b.GovernorStatus().MaxCostUsd > governorMaxAddCostUsd+defaultGovernorMaxCostUsd {
		t.Fatalf("cost ceiling was corrupted to %v", b.GovernorStatus().MaxCostUsd)
	}
}

func TestHandleGovernorCapsBodySize(t *testing.T) {
	b := newGovernorTestBroker(t)
	huge := `{"action":"pause","slug":"` + strings.Repeat("a", 4096) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/governor", strings.NewReader(huge))
	rec := httptest.NewRecorder()
	b.handleGovernor(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want 400", rec.Code)
	}
	if b.GovernorStatus().Paused {
		t.Fatalf("oversized body should not have paused the team")
	}
}

func TestGovernorResumeMoreRaisesBudget(t *testing.T) {
	b := newGovernorTestBroker(t)
	b.governor = newGovernor(governorConfig{MaxSessionTokens: 1000}, 0, 0)
	b.RecordAgentUsage("ceo", "claude", provider.ClaudeUsage{InputTokens: 1000})
	b.governorNoteTurn()
	if !b.GovernorStatus().Paused {
		t.Fatalf("precondition: should be paused on budget")
	}
	b.GovernorResumeMore(5000, 0)
	if b.GovernorStatus().Paused {
		t.Fatalf("resume_more should clear pause")
	}
	if got := b.GovernorStatus().MaxTokens; got != 6000 {
		t.Fatalf("MaxTokens = %d, want 6000 after +5000", got)
	}
}
