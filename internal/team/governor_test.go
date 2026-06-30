package team

import (
	"testing"
	"time"
)

func testGovernorConfig() governorConfig {
	return governorConfig{
		MaxSessionTokens:  1000,
		MaxSessionCostUsd: 5.0,
		MaxTurnsPerGate:   3,
	}
}

func TestGovernorNoteTurnTrips(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cfg        governorConfig
		tokens     int
		cost       float64
		wantTrip   bool
		wantReason pauseReason
	}{
		{"under all thresholds", testGovernorConfig(), 100, 1.0, false, pauseNone},
		{"token budget", testGovernorConfig(), 1000, 0, true, pauseBudget},
		{"cost budget", testGovernorConfig(), 0, 5.0, true, pauseBudget},
		{"disabled never trips", governorConfig{MaxSessionTokens: 1, Disabled: true}, 10_000, 0, false, pauseNone},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newGovernor(tt.cfg, 0, 0)
			tripped, reason := g.noteTurnComplete(tt.tokens, tt.cost)
			if tripped != tt.wantTrip || reason != tt.wantReason {
				t.Fatalf("noteTurnComplete = (%v, %q), want (%v, %q)", tripped, reason, tt.wantTrip, tt.wantReason)
			}
			if tripped && !g.status(tt.tokens, tt.cost).Paused {
				t.Fatalf("expected paused after trip")
			}
		})
	}
}

func TestGovernorTurnCountTrips(t *testing.T) {
	t.Parallel()
	g := newGovernor(governorConfig{MaxTurnsPerGate: 3}, 0, 0)
	for i := 0; i < 2; i++ {
		if tripped, _ := g.noteTurnComplete(0, 0); tripped {
			t.Fatalf("turn %d tripped early", i+1)
		}
	}
	tripped, reason := g.noteTurnComplete(0, 0)
	if !tripped || reason != pauseTurns {
		t.Fatalf("third turn = (%v, %q), want (true, %q)", tripped, reason, pauseTurns)
	}
}

func TestGovernorMeasuresSinceBaseline(t *testing.T) {
	t.Parallel()
	// Baseline 800 tokens already spent; a 1000-token budget should trip only
	// when the session reaches 1800, not 1000.
	g := newGovernor(governorConfig{MaxSessionTokens: 1000}, 800, 0)
	if tripped, _ := g.noteTurnComplete(1500, 0); tripped {
		t.Fatalf("tripped at 1500 (delta 700 < 1000)")
	}
	if tripped, _ := g.noteTurnComplete(1800, 0); !tripped {
		t.Fatalf("expected trip at 1800 (delta 1000)")
	}
}

func TestGovernorResumeRebaselines(t *testing.T) {
	t.Parallel()
	g := newGovernor(governorConfig{MaxSessionTokens: 1000, MaxTurnsPerGate: 5}, 0, 0)
	tripped, _ := g.noteTurnComplete(1000, 0)
	if !tripped {
		t.Fatalf("expected initial trip")
	}
	g.resume(1000, 2.0)
	st := g.status(1000, 2.0)
	if st.Paused {
		t.Fatalf("still paused after resume")
	}
	if st.TurnsSinceCheckpoint != 0 || st.TokensSinceCheckpoint != 0 {
		t.Fatalf("resume did not reset window: turns=%d tokens=%d", st.TurnsSinceCheckpoint, st.TokensSinceCheckpoint)
	}
	// After rebaseline at 1000, the next budget window needs another 1000.
	if tripped, _ := g.noteTurnComplete(1500, 2.0); tripped {
		t.Fatalf("tripped too soon after resume (delta 500 < 1000)")
	}
	if tripped, _ := g.noteTurnComplete(2000, 2.0); !tripped {
		t.Fatalf("expected trip at 2000 (delta 1000 from new baseline)")
	}
}

func TestGovernorGateParksUntilResume(t *testing.T) {
	t.Parallel()
	g := newGovernor(governorConfig{MaxTurnsPerGate: 1}, 0, 0)

	// Not paused: gate returns immediately.
	if !g.gate(make(chan struct{})) {
		t.Fatalf("gate should pass when not paused")
	}

	g.pauseManual(pauseManual)
	done := make(chan bool, 1)
	go func() { done <- g.gate(make(chan struct{})) }()

	select {
	case <-done:
		t.Fatalf("gate returned while paused; should have parked")
	case <-time.After(50 * time.Millisecond):
	}

	g.resume(0, 0)
	select {
	case ok := <-done:
		if !ok {
			t.Fatalf("gate returned false after resume, want true")
		}
	case <-time.After(time.Second):
		t.Fatalf("gate did not wake after resume")
	}
}

func TestGovernorGateReturnsFalseOnStop(t *testing.T) {
	t.Parallel()
	g := newGovernor(governorConfig{}, 0, 0)
	g.pauseManual(pauseStop)
	stop := make(chan struct{})
	done := make(chan bool, 1)
	go func() { done <- g.gate(stop) }()
	close(stop)
	select {
	case ok := <-done:
		if ok {
			t.Fatalf("gate returned true on stop, want false")
		}
	case <-time.After(time.Second):
		t.Fatalf("gate did not return after stop")
	}
}

func TestGovernorBumpBudget(t *testing.T) {
	t.Parallel()
	g := newGovernor(governorConfig{MaxSessionTokens: 1000}, 0, 0)
	g.bumpBudget(500, 0)
	if tripped, _ := g.noteTurnComplete(1200, 0); tripped {
		t.Fatalf("tripped at 1200 after raising budget to 1500")
	}
	if tripped, _ := g.noteTurnComplete(1500, 0); !tripped {
		t.Fatalf("expected trip at 1500 (the raised budget)")
	}
}

func TestGovernorStopNotDowngradedByLaterPause(t *testing.T) {
	t.Parallel()
	g := newGovernor(governorConfig{}, 0, 0)
	g.pauseManual(pauseStop)
	// A stale/late manual pause must not downgrade the stopped reason.
	g.pauseManual(pauseManual)
	if got := g.status(0, 0).Reason; got != pauseStop {
		t.Fatalf("reason downgraded to %q, want stop preserved", got)
	}
	// A turn-count trip while stopped must also not clobber it.
	g.noteTurnComplete(0, 0)
	if got := g.status(0, 0).Reason; got != pauseStop {
		t.Fatalf("reason changed to %q after turn, want stop preserved", got)
	}
}

func TestGovernorBumpBudgetDoesNotEnableDisabledGate(t *testing.T) {
	t.Parallel()
	// Token gate disabled (0); cost gate enabled.
	g := newGovernor(governorConfig{MaxSessionTokens: 0, MaxSessionCostUsd: 5}, 0, 0)
	g.bumpBudget(50_000, 2)
	st := g.status(0, 0)
	if st.MaxTokens != 0 {
		t.Fatalf("disabled token gate was re-enabled to %d", st.MaxTokens)
	}
	if st.MaxCostUsd != 7 {
		t.Fatalf("enabled cost gate = %v, want 7 after +2", st.MaxCostUsd)
	}
}

func TestLoadGovernorConfigEnv(t *testing.T) {
	t.Setenv("WUPHF_BUDGET_MAX_TOKENS", "42000")
	t.Setenv("WUPHF_BUDGET_MAX_COST_USD", "9.5")
	t.Setenv("WUPHF_CHECKPOINT_EVERY_TURNS", "7")
	t.Setenv("WUPHF_GOVERNOR_DISABLED", "true")
	cfg := loadGovernorConfig()
	if cfg.MaxSessionTokens != 42000 || cfg.MaxSessionCostUsd != 9.5 || cfg.MaxTurnsPerGate != 7 || !cfg.Disabled {
		t.Fatalf("unexpected config from env: %+v", cfg)
	}
}

// Automatic pausing is OFF by default; a single-agent build must not be frozen
// by a budget checkpoint nobody asked for. Manual pause/stop still work.
func TestLoadGovernorConfigDisabledByDefault(t *testing.T) {
	cfg := loadGovernorConfig()
	if !cfg.Disabled {
		t.Fatalf("expected automatic pausing disabled by default, got %+v", cfg)
	}
}

// WUPHF_GOVERNOR_ENABLED opts auto-pausing back in; WUPHF_GOVERNOR_DISABLED
// still wins when both are set.
func TestLoadGovernorConfigEnabledOptIn(t *testing.T) {
	t.Setenv("WUPHF_GOVERNOR_ENABLED", "1")
	if loadGovernorConfig().Disabled {
		t.Fatalf("WUPHF_GOVERNOR_ENABLED should re-enable automatic pausing")
	}
	t.Setenv("WUPHF_GOVERNOR_DISABLED", "1")
	if !loadGovernorConfig().Disabled {
		t.Fatalf("WUPHF_GOVERNOR_DISABLED must win over ENABLED")
	}
}
