package team

// promotion_sweep_test.go covers PR 6 of the notebook-wiki-promise series:
// the periodic broker sweep that drains the demand index into the review log.
//
// Coverage focuses on the wiring concerns owned by PR 6 (cadence selection,
// budget gate, content-volume gate, demand-pressure escalation, idempotent
// handoff to the demand index). PR 3's TestAutoEscalateDemandCandidates_*
// already exercises the escalation contract itself; we only verify the sweep
// invokes it on the right cadence with the right gates.

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeSweepClock is a manual clock the sweep tick logic consults via the
// PromotionSweep.clock function pointer. Tests advance it explicitly so no
// goroutine ever waits on real wall time.
type fakeSweepClock struct {
	now time.Time
}

func (c *fakeSweepClock) Now() time.Time { return c.now }

func (c *fakeSweepClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// fakePromotionEscalator records calls to AutoEscalateDemandCandidates so a
// test can assert how many sweeps actually pushed work to the review log.
type fakePromotionEscalator struct {
	calls       int
	returnErr   error
	currentSize func() int
}

func (f *fakePromotionEscalator) AutoEscalate(ctx context.Context) error {
	f.calls++
	return f.returnErr
}

func (f *fakePromotionEscalator) CandidateCount() int {
	if f.currentSize == nil {
		return 0
	}
	return f.currentSize()
}

func (f *fakePromotionEscalator) NearThresholdCount() int {
	if f.currentSize == nil {
		return 0
	}
	return f.currentSize()
}

// fakeNotebookCounter tracks the "any new notebook entries since last sweep?"
// signal. The content-volume gate consults LastCommitTime; the sweep stores
// the previous reading and short-circuits when the value is unchanged.
type fakeNotebookCounter struct {
	commits int
	last    time.Time
}

func (f *fakeNotebookCounter) NotebookCommitCount() int { return f.commits }

func (f *fakeNotebookCounter) NotebookLastCommitTime() time.Time { return f.last }

func newSweepUnderTest(t *testing.T, clock *fakeSweepClock, esc *fakePromotionEscalator, nb *fakeNotebookCounter, cfg PromotionSweepConfig) *PromotionSweep {
	t.Helper()
	s := NewPromotionSweep(esc, nb, cfg)
	s.clock = clock.Now
	return s
}

func defaultTestConfig() PromotionSweepConfig {
	return PromotionSweepConfig{
		Interval:         time.Hour,
		DailyTokenBudget: 10000,
		LLMEnabled:       false,
	}
}

func TestPromotionSweep_CadenceFromConfig(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	esc := &fakePromotionEscalator{}
	nb := &fakeNotebookCounter{commits: 1, last: clock.now}
	cfg := defaultTestConfig()
	cfg.Interval = 30 * time.Minute
	s := newSweepUnderTest(t, clock, esc, nb, cfg)

	got := s.currentCadence()
	if got != 30*time.Minute {
		t.Fatalf("currentCadence() = %s; want 30m", got)
	}
}

func TestPromotionSweep_ContentVolumeGate(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	esc := &fakePromotionEscalator{}
	nb := &fakeNotebookCounter{commits: 5, last: clock.now}
	s := newSweepUnderTest(t, clock, esc, nb, defaultTestConfig())

	// First tick: no prior baseline, so the sweep MUST run and seed the
	// baseline. AutoEscalate is called once.
	s.tick(context.Background())
	if esc.calls != 1 {
		t.Fatalf("first tick: AutoEscalate calls = %d; want 1", esc.calls)
	}
	if got := s.Counters().Skipped; got != 0 {
		t.Fatalf("first tick: Counters.Skipped = %d; want 0", got)
	}

	// Second tick: notebook commit count and timestamp unchanged. The
	// content-volume gate must short-circuit and increment Skipped without
	// re-calling AutoEscalate.
	clock.Advance(time.Hour)
	s.tick(context.Background())
	if esc.calls != 1 {
		t.Fatalf("second tick (no new commits): AutoEscalate calls = %d; want still 1", esc.calls)
	}
	if got := s.Counters().Skipped; got != 1 {
		t.Fatalf("second tick: Counters.Skipped = %d; want 1", got)
	}

	// Third tick: a new notebook commit landed. Gate must re-open.
	clock.Advance(time.Hour)
	nb.commits++
	nb.last = clock.now
	s.tick(context.Background())
	if esc.calls != 2 {
		t.Fatalf("third tick (new commit): AutoEscalate calls = %d; want 2", esc.calls)
	}
}

func TestPromotionSweep_BudgetGate(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	esc := &fakePromotionEscalator{}
	nb := &fakeNotebookCounter{commits: 1, last: clock.now}
	cfg := defaultTestConfig()
	cfg.DailyTokenBudget = 0 // zero budget → first tick exhausts immediately
	s := newSweepUnderTest(t, clock, esc, nb, cfg)

	// First tick under zero budget: AutoEscalate still runs (escalation
	// itself does not consume tokens; the LLM hook does, and is gated
	// separately by the LLM flag). The budget counter goes to 0 used / 0
	// available → exhausted=true. Cadence escalates to "wait until midnight"
	// for the NEXT tick.
	s.tick(context.Background())
	if esc.calls != 1 {
		t.Fatalf("zero-budget first tick: AutoEscalate calls = %d; want 1", esc.calls)
	}
	if !s.budgetExhausted() {
		t.Fatalf("zero-budget first tick: expected budgetExhausted to be true")
	}

	wantCadence := s.cadenceFromBudget(1.0) // 100% saturated
	if got := s.currentCadence(); got != wantCadence {
		t.Fatalf("after exhaustion: cadence = %s; want %s", got, wantCadence)
	}
}

func TestPromotionSweep_AdaptiveCadenceFromDemandPressure(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	cfg := defaultTestConfig()

	cases := []struct {
		name        string
		nearCount   int
		wantBaseDur time.Duration
	}{
		// 0 near-threshold candidates → base cadence (1h).
		{"no_pressure", 0, cfg.Interval},
		// At/over the demand-pressure floor → 3x faster cadence.
		{"some_pressure", demandPressureFloor, cfg.Interval / 3},
		// Way over → still capped at 3x.
		{"high_pressure", demandPressureFloor * 4, cfg.Interval / 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			esc := &fakePromotionEscalator{currentSize: func() int { return tc.nearCount }}
			nb := &fakeNotebookCounter{commits: 1, last: clock.now}
			s := newSweepUnderTest(t, clock, esc, nb, cfg)
			got := s.currentCadence()
			if got != tc.wantBaseDur {
				t.Fatalf("cadence with near=%d = %s; want %s", tc.nearCount, got, tc.wantBaseDur)
			}
		})
	}
}

func TestPromotionSweep_StartStopIsIdempotent(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	esc := &fakePromotionEscalator{}
	nb := &fakeNotebookCounter{commits: 0, last: clock.now}
	s := newSweepUnderTest(t, clock, esc, nb, defaultTestConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	s.Start(ctx) // second Start must be a no-op, not panic or spawn 2 goroutines.
	s.Stop(50 * time.Millisecond)
	s.Stop(50 * time.Millisecond) // second Stop must be a no-op.
}

func TestPromotionSweep_HonoursContextCancellation(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	esc := &fakePromotionEscalator{}
	nb := &fakeNotebookCounter{commits: 0, last: clock.now}
	cfg := defaultTestConfig()
	// Tiny interval so the run loop reaches its select promptly. The test
	// does not wait for the timer to fire — it cancels the context, which
	// should unblock the goroutine without ever ticking.
	cfg.Interval = 10 * time.Second
	s := newSweepUnderTest(t, clock, esc, nb, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	cancel()
	// Stop returns when run() exits. If cancellation is honoured, this
	// returns near-instantly. If not, the test times out.
	s.Stop(2 * time.Second)
	if esc.calls != 0 {
		t.Fatalf("expected zero AutoEscalate calls after immediate cancel; got %d", esc.calls)
	}
}

func TestPromotionSweep_EscalateError_IsLoggedAndCounted(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	esc := &fakePromotionEscalator{returnErr: errors.New("boom")}
	nb := &fakeNotebookCounter{commits: 1, last: clock.now}
	s := newSweepUnderTest(t, clock, esc, nb, defaultTestConfig())

	s.tick(context.Background())
	if got := s.Counters().Failed; got != 1 {
		t.Fatalf("Counters.Failed = %d; want 1", got)
	}
	if got := s.Counters().Sweeps; got != 1 {
		t.Fatalf("Counters.Sweeps = %d; want 1", got)
	}
}

func TestPromotionSweep_NilEscalator_NoOps(t *testing.T) {
	clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
	nb := &fakeNotebookCounter{commits: 1, last: clock.now}
	// Nil-safe: NewPromotionSweep with a nil escalator must not panic on tick.
	s := NewPromotionSweep(nil, nb, defaultTestConfig())
	s.clock = clock.Now
	s.tick(context.Background())
	if got := s.Counters().Sweeps; got != 1 {
		t.Fatalf("Counters.Sweeps with nil escalator = %d; want 1 (sweep ran, no-op'd)", got)
	}
}

func TestPromotionSweep_BudgetCadenceTransitions(t *testing.T) {
	cases := []struct {
		name         string
		saturation   float64
		wantMultiple int // base interval divided by this many
	}{
		{"none", 0.0, 1},
		{"under_50", 0.25, 1},
		{"at_50", 0.5, 3},
		{"at_75", 0.75, 3},
		{"at_100", 1.0, 12}, // 100% → minimum cadence (5m default with 1h base)
	}
	cfg := defaultTestConfig()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeSweepClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
			esc := &fakePromotionEscalator{}
			nb := &fakeNotebookCounter{commits: 1, last: clock.now}
			s := newSweepUnderTest(t, clock, esc, nb, cfg)
			got := s.cadenceFromBudget(tc.saturation)
			want := cfg.Interval / time.Duration(tc.wantMultiple)
			if got != want {
				t.Fatalf("cadenceFromBudget(%.2f) = %s; want %s", tc.saturation, got, want)
			}
		})
	}
}

func TestPromotionSweep_EnvOverridesParse(t *testing.T) {
	t.Run("interval_default_when_unset", func(t *testing.T) {
		t.Setenv(envPromotionSweepInterval, "")
		cfg := promotionSweepConfigFromEnv()
		if cfg.Interval != defaultPromotionSweepInterval {
			t.Fatalf("default interval = %s; want %s", cfg.Interval, defaultPromotionSweepInterval)
		}
	})
	t.Run("interval_parsed", func(t *testing.T) {
		t.Setenv(envPromotionSweepInterval, "15m")
		cfg := promotionSweepConfigFromEnv()
		if cfg.Interval != 15*time.Minute {
			t.Fatalf("parsed interval = %s; want 15m", cfg.Interval)
		}
	})
	t.Run("interval_invalid_falls_back_to_default", func(t *testing.T) {
		t.Setenv(envPromotionSweepInterval, "not-a-duration")
		cfg := promotionSweepConfigFromEnv()
		if cfg.Interval != defaultPromotionSweepInterval {
			t.Fatalf("invalid interval should fall back to default; got %s", cfg.Interval)
		}
	})
	t.Run("budget_parsed", func(t *testing.T) {
		t.Setenv(envPromotionSweepTokenBudget, "25000")
		cfg := promotionSweepConfigFromEnv()
		if cfg.DailyTokenBudget != 25000 {
			t.Fatalf("parsed token budget = %d; want 25000", cfg.DailyTokenBudget)
		}
	})
	t.Run("llm_flag_default_false", func(t *testing.T) {
		t.Setenv(envPromotionSweepLLM, "")
		cfg := promotionSweepConfigFromEnv()
		if cfg.LLMEnabled {
			t.Fatalf("default LLMEnabled should be false")
		}
	})
	t.Run("llm_flag_true", func(t *testing.T) {
		t.Setenv(envPromotionSweepLLM, "true")
		cfg := promotionSweepConfigFromEnv()
		if !cfg.LLMEnabled {
			t.Fatalf("LLMEnabled should be true when env set to 'true'")
		}
	})
}
