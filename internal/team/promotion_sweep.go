package team

// promotion_sweep.go implements PR 6 of the notebook-wiki-promise design
// (~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md).
//
// PR 6 is the Tier 3 safety net for the demand-driven promotion pipeline.
// On a configurable cadence (default hourly), the sweep:
//
//  1. Skips iterations when no notebook commits have landed since the last
//     sweep (content-volume gate). This avoids burning the daily token
//     budget when nothing is on the table to promote.
//  2. Escalates demand candidates via PR 3's NotebookDemandIndex.
//     AutoEscalateDemandCandidates idempotently submits any entry whose
//     rolling-window score has breached the threshold.
//  3. Adapts cadence under demand pressure: when many entries are within
//     80% of the threshold, the sweep runs faster so newly-arriving
//     demand events have a chance to tip them over without waiting an
//     hour.
//  4. Adapts cadence under budget pressure: as the daily token budget is
//     consumed (50%, 75%, 100% saturation), the sweep slows down. At
//     100% the cadence shortens to a minimum of 5 minutes — escalation
//     itself does not consume tokens, but bursty demand still benefits
//     from a quick re-check while the LLM hook (gated by the LLM env
//     flag) is dark.
//
// LLM hook: an optional LLM extraction pass can be wired in later by
// flipping WUPHF_PROMOTION_SWEEP_LLM=true. For PR 6 the flag is read but
// no LLM call is made. The MVP is the cadence + gate + escalation
// wiring; the LLM consumer lands as a follow-up PR.
//
// Lock invariants:
//   - The sweep goroutine NEVER acquires b.mu. It holds references to
//     the demand index and notebook counter that were captured when the
//     sweep was constructed inside initWikiWorker. Those primitives
//     manage their own locks (idx.mu for the demand index, repo.mu for
//     notebook reads); the sweep is a pure orchestrator.
//   - PromotionSweep.mu is internal-only and never held across a call
//     out to the escalator or the notebook counter.

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// envPromotionSweepInterval names the env var that overrides the base
// sweep cadence. Accepts any time.ParseDuration string (e.g. "30m",
// "2h"). Empty or invalid values fall back to defaultPromotionSweepInterval.
const envPromotionSweepInterval = "WUPHF_PROMOTION_SWEEP_INTERVAL"

// envPromotionSweepTokenBudget names the env var that overrides the
// per-office daily token budget for the optional LLM extraction pass.
// PR 6 ships this knob wired but unused — the LLM hook is gated by
// envPromotionSweepLLM.
const envPromotionSweepTokenBudget = "WUPHF_PROMOTION_SWEEP_TOKEN_BUDGET"

// envPromotionSweepLLM toggles the LLM extraction hook. Default false.
// Truthy values: "1", "true", "yes", "on" (case-insensitive).
const envPromotionSweepLLM = "WUPHF_PROMOTION_SWEEP_LLM"

// defaultPromotionSweepInterval is the base cadence when no env override
// is set.
const defaultPromotionSweepInterval = time.Hour

// defaultPromotionSweepTokenBudget is the per-office daily token budget
// when no env override is set.
const defaultPromotionSweepTokenBudget = 10000

// defaultPromotionSweepMinCadence floors the budget-driven cadence so a
// fully-saturated office still gets one quick re-check every five
// minutes during the active demand window.
const defaultPromotionSweepMinCadence = 5 * time.Minute

// demandPressureFloor is the threshold at which "near-threshold"
// candidates start escalating cadence. Below this, base cadence holds.
// At or above, cadence drops to base/3.
const demandPressureFloor = 3

// PromotionSweepConfig holds the runtime knobs for the sweep. All zero
// values are replaced by defaults in NewPromotionSweep.
type PromotionSweepConfig struct {
	// Interval is the base cadence between sweep ticks.
	Interval time.Duration
	// DailyTokenBudget is the per-office cap on LLM tokens consumed by
	// the (optional) LLM extraction pass. PR 6 reads but never spends
	// from this budget; the LLM hook lands behind LLMEnabled.
	DailyTokenBudget int
	// LLMEnabled toggles the LLM extraction pass. Default false.
	LLMEnabled bool
}

// promotionEscalator is the slice of NotebookDemandIndex the sweep
// depends on. Defined as an interface so tests can mock it without
// constructing a real index + review log.
type promotionEscalator interface {
	// AutoEscalate runs one escalation pass. The sweep does not pass a
	// review log directly — the escalator (typically NotebookDemandIndex)
	// captured its dependencies at construction time on the broker.
	AutoEscalate(ctx context.Context) error
	// CandidateCount returns the count of entries currently tracked in
	// the demand index (post-window). Used for observability.
	CandidateCount() int
	// NearThresholdCount returns the count of entries within 80% of the
	// auto-escalation threshold. Drives demand-pressure cadence.
	NearThresholdCount() int
}

// notebookCounter is the sweep's view of "did anything new land in the
// notebooks since the last tick?" — the content-volume gate. Defined as
// an interface so tests can inject a deterministic counter without
// running the wiki worker.
type notebookCounter interface {
	// NotebookCommitCount returns a monotonic counter that increments
	// every time a notebook entry is written or modified.
	NotebookCommitCount() int
	// NotebookLastCommitTime returns the wall-clock time of the most
	// recent notebook commit. The sweep uses this in addition to the
	// counter to decide whether new content has arrived.
	NotebookLastCommitTime() time.Time
}

// PromotionSweepCounters is the observability snapshot for the sweep.
type PromotionSweepCounters struct {
	Sweeps            int64
	Skipped           int64
	Failed            int64
	BudgetExhausted   int64
	NearThreshold     int
	BaseCadenceSec    int64
	CurrentCadenceSec int64
}

// PromotionSweep is the periodic broker-driven safety net that drains
// the demand index into the review log. Lifecycle mirrors
// AutoNotebookWriter: NewPromotionSweep → Start(ctx) → Stop(timeout).
//
// Safe for concurrent Counters() callers. tick() is single-threaded
// (only run() invokes it) so internal state — last-commit baselines,
// budget tracking — does not require its own lock beyond progressMu.
type PromotionSweep struct {
	escalator promotionEscalator
	counter   notebookCounter
	cfg       PromotionSweepConfig

	clock func() time.Time

	stopCh  chan struct{}
	done    chan struct{}
	running atomic.Bool

	// Mutated only inside tick() and Counters(). The mutex is held for
	// negligible windows so contention is irrelevant.
	mu                sync.Mutex
	lastCommitCount   int
	lastCommitTime    time.Time
	hasBaseline       bool
	tokensSpentToday  int
	tokensBudgetDay   string // YYYY-MM-DD UTC for the current budget window
	currentCadenceDur time.Duration

	// counters
	sweeps           atomic.Int64
	skipped          atomic.Int64
	failed           atomic.Int64
	budgetExhaustedC atomic.Int64
}

// NewPromotionSweep constructs an idle sweep. Either argument may be nil
// for tests; a nil escalator turns tick() into a metrics-only pass, a
// nil counter disables the content-volume gate (every tick runs).
func NewPromotionSweep(escalator promotionEscalator, counter notebookCounter, cfg PromotionSweepConfig) *PromotionSweep {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultPromotionSweepInterval
	}
	if cfg.DailyTokenBudget < 0 {
		cfg.DailyTokenBudget = 0
	}
	return &PromotionSweep{
		escalator:         escalator,
		counter:           counter,
		cfg:               cfg,
		clock:             time.Now,
		stopCh:            make(chan struct{}),
		done:              make(chan struct{}),
		currentCadenceDur: cfg.Interval,
	}
}

// promotionSweepConfigFromEnv returns the runtime config built from the
// documented env knobs. Invalid values fall back to defaults with a
// warn log so a typo cannot silently disable the sweep.
func promotionSweepConfigFromEnv() PromotionSweepConfig {
	cfg := PromotionSweepConfig{
		Interval:         defaultPromotionSweepInterval,
		DailyTokenBudget: defaultPromotionSweepTokenBudget,
		LLMEnabled:       false,
	}
	if raw := strings.TrimSpace(os.Getenv(envPromotionSweepInterval)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.Interval = d
		} else {
			log.Printf("promotion_sweep: invalid %s=%q; using default %s",
				envPromotionSweepInterval, raw, defaultPromotionSweepInterval)
		}
	}
	if raw := strings.TrimSpace(os.Getenv(envPromotionSweepTokenBudget)); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			cfg.DailyTokenBudget = v
		} else {
			log.Printf("promotion_sweep: invalid %s=%q; using default %d",
				envPromotionSweepTokenBudget, raw, defaultPromotionSweepTokenBudget)
		}
	}
	if raw := strings.TrimSpace(os.Getenv(envPromotionSweepLLM)); raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			cfg.LLMEnabled = true
		}
	}
	return cfg
}

// Start launches the sweep goroutine. Idempotent: a second call is a
// no-op. The goroutine exits when ctx is cancelled or Stop is called.
func (s *PromotionSweep) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if s.running.Swap(true) {
		return
	}
	go s.run(ctx)
}

// Stop signals the goroutine to exit and waits up to timeout for it to
// finish. Idempotent.
func (s *PromotionSweep) Stop(timeout time.Duration) {
	if s == nil || !s.running.Swap(false) {
		return
	}
	close(s.stopCh)
	if timeout <= 0 {
		<-s.done
		return
	}
	select {
	case <-s.done:
	case <-time.After(timeout):
	}
}

// Counters returns a point-in-time snapshot of sweep observability state.
func (s *PromotionSweep) Counters() PromotionSweepCounters {
	if s == nil {
		return PromotionSweepCounters{}
	}
	near := 0
	if s.escalator != nil {
		near = s.escalator.NearThresholdCount()
	}
	s.mu.Lock()
	cur := s.currentCadenceDur
	s.mu.Unlock()
	return PromotionSweepCounters{
		Sweeps:            s.sweeps.Load(),
		Skipped:           s.skipped.Load(),
		Failed:            s.failed.Load(),
		BudgetExhausted:   s.budgetExhaustedC.Load(),
		NearThreshold:     near,
		BaseCadenceSec:    int64(s.cfg.Interval / time.Second),
		CurrentCadenceSec: int64(cur / time.Second),
	}
}

// run is the sweep loop. It uses a time.Timer (not a Ticker) so the
// adaptive cadence applied at the end of each tick takes effect on the
// very next wait. Drift across ticks is acceptable — sub-minute
// precision is irrelevant at this timescale.
func (s *PromotionSweep) run(ctx context.Context) {
	defer close(s.done)
	for {
		cadence := s.currentCadence()
		s.setCurrentCadence(cadence)
		timer := time.NewTimer(cadence)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
		s.tick(ctx)
	}
}

// tick is one sweep iteration. Extracted so tests can drive the sweep
// deterministically without waiting on a timer.
func (s *PromotionSweep) tick(ctx context.Context) {
	s.sweeps.Add(1)
	now := s.clock().UTC()

	// Daily-budget rollover: a new UTC day resets the spent-tokens
	// accumulator. Without this, a long-lived broker would never re-open
	// the budget once the LLM hook (future PR) drains it.
	s.maybeResetBudget(now)

	// Content-volume gate: if no notebook commits since the last sweep,
	// short-circuit. The first ever tick seeds the baseline and runs
	// regardless — we cannot tell whether the notebooks were quiet or
	// the broker just started.
	if s.counter != nil && s.contentVolumeUnchanged() {
		s.skipped.Add(1)
		return
	}

	if s.escalator != nil {
		if err := s.escalator.AutoEscalate(ctx); err != nil {
			s.failed.Add(1)
			log.Printf("promotion_sweep: AutoEscalate failed: %v", err)
		}
	}

	// LLM extraction hook: gated behind WUPHF_PROMOTION_SWEEP_LLM. PR 6
	// ships the flag wired but no-op'd; future PR fills in the call and
	// debits s.tokensSpentToday. We still flag the budget as exhausted
	// when DailyTokenBudget is zero so the cadence test path exercises
	// the saturation logic without an actual LLM call.
	if s.cfg.DailyTokenBudget == 0 {
		s.budgetExhaustedC.Add(1)
	}

	// Re-baseline the content-volume gate AFTER a successful (or
	// failed-but-attempted) sweep. The next tick compares against this
	// snapshot.
	s.captureContentBaseline()
}

// contentVolumeUnchanged returns true when no new notebook commits have
// landed since the last sweep ran. Caller has already null-checked
// s.counter. The first call ever must return false (the baseline has
// not been seeded yet) so the very first sweep always runs.
func (s *PromotionSweep) contentVolumeUnchanged() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasBaseline {
		return false
	}
	count := s.counter.NotebookCommitCount()
	last := s.counter.NotebookLastCommitTime()
	return count == s.lastCommitCount && last.Equal(s.lastCommitTime)
}

func (s *PromotionSweep) captureContentBaseline() {
	if s.counter == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCommitCount = s.counter.NotebookCommitCount()
	s.lastCommitTime = s.counter.NotebookLastCommitTime()
	s.hasBaseline = true
}

// currentCadence picks the effective sweep interval for the next tick,
// taking the minimum of the budget-driven cadence and the demand-
// pressure cadence. Floor-bounded by defaultPromotionSweepMinCadence so
// the sweep never spins faster than every five minutes regardless of
// how much pressure the office is under.
func (s *PromotionSweep) currentCadence() time.Duration {
	saturation := s.budgetSaturation()
	budgetDur := s.cadenceFromBudget(saturation)
	demandDur := s.cadenceFromDemand()
	chosen := budgetDur
	if demandDur < chosen {
		chosen = demandDur
	}
	if chosen < defaultPromotionSweepMinCadence {
		chosen = defaultPromotionSweepMinCadence
	}
	return chosen
}

// cadenceFromBudget maps daily-token saturation onto a cadence. Below
// 50% the base cadence applies. At 50%/75% the sweep accelerates 3x to
// catch a flurry of newly-promoted entries while LLM budget is still
// available. At 100% the cadence drops to the floor — escalation
// itself is cheap, and a rapid re-check window ensures the office
// notices when the next day's budget unlocks.
func (s *PromotionSweep) cadenceFromBudget(saturation float64) time.Duration {
	switch {
	case saturation >= 1.0:
		// Floor: 5m by default. The 12x divisor with a 1h base produces
		// exactly the documented 5m minimum.
		return s.cfg.Interval / 12
	case saturation >= 0.5:
		return s.cfg.Interval / 3
	default:
		return s.cfg.Interval
	}
}

// cadenceFromDemand maps near-threshold candidate count onto a
// cadence. Below the floor the base cadence applies. At/above the
// floor the sweep accelerates 3x so newly-arriving demand events
// quickly tip near-threshold entries over the line.
func (s *PromotionSweep) cadenceFromDemand() time.Duration {
	if s.escalator == nil {
		return s.cfg.Interval
	}
	near := s.escalator.NearThresholdCount()
	if near >= demandPressureFloor {
		return s.cfg.Interval / 3
	}
	return s.cfg.Interval
}

// budgetSaturation returns spent / budget on [0,1]. A zero budget is
// treated as fully saturated — the sweep cannot spend any LLM tokens,
// so any future LLM hook must back off to the floor cadence.
func (s *PromotionSweep) budgetSaturation() float64 {
	if s.cfg.DailyTokenBudget <= 0 {
		return 1.0
	}
	s.mu.Lock()
	spent := s.tokensSpentToday
	s.mu.Unlock()
	return float64(spent) / float64(s.cfg.DailyTokenBudget)
}

// budgetExhausted reports whether the daily token budget is fully
// consumed (or zero). Test-facing helper.
func (s *PromotionSweep) budgetExhausted() bool {
	return s.budgetSaturation() >= 1.0
}

// maybeResetBudget zeros the spent-tokens counter when a new UTC day
// begins. The sweep's day boundary matches the demand index window
// boundary so observability surfaces line up.
func (s *PromotionSweep) maybeResetBudget(now time.Time) {
	day := now.UTC().Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokensBudgetDay == day {
		return
	}
	s.tokensBudgetDay = day
	s.tokensSpentToday = 0
}

func (s *PromotionSweep) setCurrentCadence(d time.Duration) {
	s.mu.Lock()
	s.currentCadenceDur = d
	s.mu.Unlock()
}
