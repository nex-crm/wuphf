package team

import (
	"path/filepath"
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
