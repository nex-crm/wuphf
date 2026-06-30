package team

// governor.go owns session-level run control: the cumulative budget /
// turn-count checkpoint that pauses the autonomous headless dispatch loop at
// sensible stoppage points, plus the manual pause/stop/resume surface.
//
// Motivation: every pre-existing safeguard in the dispatch path is PER-TURN
// (max-turns 15/30, 8 tool iterations, 4-12 min timeouts). Nothing bounded
// CUMULATIVE token/cost spend or gave a human a one-click interrupt. Agents
// looped as long as work was enqueued. The governor adds two things:
//
//  1. A checkpoint: after each completed autonomous turn the broker calls
//     noteTurnComplete with the session's running token/cost totals. When
//     spend since the last checkpoint crosses a budget, OR the team has run N
//     turns without a human in the loop, the governor pauses dispatch and the
//     broker raises a review notice. The user reviews, then Continue /
//     Continue +budget / Stop.
//  2. A live interrupt: the worker loop parks on gate() whenever paused, so a
//     manual pause/stop halts new turns immediately without busy-spinning.
//
// State lives on the Broker (broker_governor.go) because usage accounting, the
// HTTP surface, and SSE fanout all live there; the launcher's dispatch loop
// reaches in via the broker.

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// headlessDispatchController is the narrow cancellation surface the governor's
// Stop uses to interrupt in-flight headless turns. The Launcher implements it
// (CancelHeadlessTurns); wired via Broker.SetHeadlessDispatchController. It
// lives on the governor (not the broker) so the "stop cancels in-flight"
// mechanism is co-located with the pause state it belongs to.
type headlessDispatchController interface {
	// CancelHeadlessTurns cancels in-flight turns. slug == "" cancels all.
	CancelHeadlessTurns(slug string)
}

// pauseReason tags why dispatch is paused so the UI can explain it.
type pauseReason string

const (
	pauseNone   pauseReason = ""
	pauseManual pauseReason = "manual" // user hit Pause
	pauseStop   pauseReason = "stop"   // user hit Stop (also cancels in-flight)
	pauseBudget pauseReason = "budget" // cumulative tokens/cost crossed the cap
	pauseTurns  pauseReason = "turns"  // ran the per-checkpoint turn count
)

// Default checkpoint thresholds, used only when automatic pausing is opted back
// in (WUPHF_GOVERNOR_ENABLED). They are overridable via env (loadGovernorConfig).
const (
	defaultGovernorMaxTokens  = 150_000
	defaultGovernorMaxCostUsd = 3.0
	defaultGovernorMaxTurns   = 12

	// defaultGovernorDisabled turns OFF automatic budget/turn pausing by default.
	// The operator surface is single-agent, human-initiated work (describe an app,
	// it builds); an auto-pause at 150k tokens silently froze multi-million-token
	// builds, which read as a hang. Manual pause/stop still work; re-enable the
	// automatic checkpoint with WUPHF_GOVERNOR_ENABLED=1.
	defaultGovernorDisabled = true
)

type governorConfig struct {
	// MaxSessionTokens pauses when (sessionTokens - baseline) >= this. 0 disables.
	MaxSessionTokens int
	// MaxSessionCostUsd pauses when (sessionCost - baseline) >= this. 0 disables.
	MaxSessionCostUsd float64
	// MaxTurnsPerGate pauses every N completed turns since the last checkpoint.
	// 0 disables.
	MaxTurnsPerGate int
	// Disabled turns off ALL automatic pausing. Manual pause/stop still work.
	Disabled bool
}

// loadGovernorConfig reads thresholds from the environment, falling back to the
// conservative defaults above. Mirrors the env-override pattern used by
// compactionTokenLimit in internal/agent/task_runtime.go.
func loadGovernorConfig() governorConfig {
	cfg := governorConfig{
		MaxSessionTokens:  defaultGovernorMaxTokens,
		MaxSessionCostUsd: defaultGovernorMaxCostUsd,
		MaxTurnsPerGate:   defaultGovernorMaxTurns,
		Disabled:          defaultGovernorDisabled,
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_BUDGET_MAX_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MaxSessionTokens = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_BUDGET_MAX_COST_USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			cfg.MaxSessionCostUsd = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_CHECKPOINT_EVERY_TURNS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MaxTurnsPerGate = n
		}
	}
	// Automatic pausing is off by default (defaultGovernorDisabled). Opt back in
	// with WUPHF_GOVERNOR_ENABLED; WUPHF_GOVERNOR_DISABLED still forces it off and
	// wins if both are set.
	if envTruthy(os.Getenv("WUPHF_GOVERNOR_ENABLED")) {
		cfg.Disabled = false
	}
	if envTruthy(os.Getenv("WUPHF_GOVERNOR_DISABLED")) {
		cfg.Disabled = true
	}
	return cfg
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// governor holds all pause/budget state behind its own mutex. It never touches
// broker or launcher state directly — the broker passes usage snapshots in and
// reacts to the (tripped, reason) result. This keeps it unit-testable with no
// broker, launcher, or live provider (see governor_test.go).
type governor struct {
	mu       sync.Mutex
	cfg      governorConfig
	paused   bool
	reason   pauseReason
	pausedAt time.Time

	// resumeCh is the park channel parked workers select on. Invariant: while
	// paused it is an OPEN, never-closed channel; resume() closes it (waking
	// every parked worker) and installs a fresh one. Closing-on-resume rather
	// than per-pause keeps the close/recreate paired so we never double-close.
	resumeCh chan struct{}

	// turns counts completed autonomous turns since the last checkpoint;
	// baseTokens/baseCost snapshot the session usage at the last checkpoint so
	// budgets measure spend SINCE the human last looked, not all-time.
	turns      int
	baseTokens int
	baseCost   float64

	// ctl cancels in-flight turns on Stop. nil in unit tests and until the
	// launcher wires it.
	ctl headlessDispatchController
}

// setController wires the in-flight cancellation hook used by Stop.
func (g *governor) setController(c headlessDispatchController) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ctl = c
}

// cancelInFlight cancels in-flight turns (slug == "" = all) if a controller is
// wired. Safe to call when none is.
func (g *governor) cancelInFlight(slug string) {
	g.mu.Lock()
	ctl := g.ctl
	g.mu.Unlock()
	if ctl != nil {
		ctl.CancelHeadlessTurns(slug)
	}
}

// headlessTurnEnqueuer is the optional capability to enqueue a fresh agent turn
// directly (the Launcher implements it). The broker uses it to dispatch a single
// agent for a specific job — e.g. an app edit straight to the App Builder —
// WITHOUT routing through the CEO/lead orchestration, which is the pi-skeleton
// shape: one agent, one job, no multi-agent hop.
type headlessTurnEnqueuer interface {
	EnqueueHeadlessTurn(slug, prompt, channel string)
}

// enqueuer returns the wired controller as a turn enqueuer when it supports it.
func (g *governor) enqueuer() (headlessTurnEnqueuer, bool) {
	g.mu.Lock()
	ctl := g.ctl
	g.mu.Unlock()
	e, ok := ctl.(headlessTurnEnqueuer)
	return e, ok
}

func newGovernor(cfg governorConfig, baseTokens int, baseCost float64) *governor {
	return &governor{
		cfg:        cfg,
		resumeCh:   make(chan struct{}),
		baseTokens: baseTokens,
		baseCost:   baseCost,
	}
}

// gate blocks the calling worker while dispatch is paused. Returns true to
// proceed with the next turn, or false if stop is signalled while parked (the
// worker should then exit). When not paused it returns true immediately, so
// the steady-state cost is a single mutex lock per turn.
func (g *governor) gate(stop <-chan struct{}) bool {
	for {
		g.mu.Lock()
		if !g.paused {
			g.mu.Unlock()
			return true
		}
		ch := g.resumeCh
		g.mu.Unlock()

		select {
		case <-stop:
			return false
		case <-ch:
			// Woken by resume() closing the prior channel. Loop to re-check
			// paused — if a fresh pause raced in before we re-acquired g.mu,
			// park on the new channel instead.
		}
	}
}

// noteTurnComplete records one completed autonomous turn and reports whether a
// threshold tripped (in which case the governor is now paused). The broker
// passes the session's running totals; budgets compare against the last
// checkpoint's baseline. Disabled config and an already-paused governor never
// auto-trip (no double notices).
func (g *governor) noteTurnComplete(tokens int, cost float64) (bool, pauseReason) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.turns++
	if g.cfg.Disabled || g.paused {
		return false, pauseNone
	}
	reason := pauseNone
	switch {
	case g.cfg.MaxSessionTokens > 0 && tokens-g.baseTokens >= g.cfg.MaxSessionTokens:
		reason = pauseBudget
	case g.cfg.MaxSessionCostUsd > 0 && cost-g.baseCost >= g.cfg.MaxSessionCostUsd:
		reason = pauseBudget
	case g.cfg.MaxTurnsPerGate > 0 && g.turns >= g.cfg.MaxTurnsPerGate:
		reason = pauseTurns
	}
	if reason == pauseNone {
		return false, pauseNone
	}
	g.pauseLocked(reason)
	return true, reason
}

// pauseManual sets a manual pause/stop. Idempotent; escalates the reason (a
// later Stop over an earlier Pause records "stop").
func (g *governor) pauseManual(reason pauseReason) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pauseLocked(reason)
}

// pauseLocked flips into the paused state. resumeCh is already open (from
// construction or the previous resume), so parked workers will block on it.
func (g *governor) pauseLocked(reason pauseReason) {
	if reason == pauseNone {
		return
	}
	if !g.paused {
		g.paused = true
		g.pausedAt = time.Now()
	}
	// Never downgrade a Stop: a later or stale pause/budget/turn reason must not
	// lose the stopped state (Stop already cancelled in-flight work).
	if g.reason != pauseStop || reason == pauseStop {
		g.reason = reason
	}
}

// resume clears any pause, wakes parked workers, and rebaselines the checkpoint
// to the current session usage so the next budget window starts fresh. Safe to
// call when not paused (rebaselines without touching the park channel).
func (g *governor) resume(tokens int, cost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.turns = 0
	g.baseTokens = tokens
	g.baseCost = cost
	if g.paused {
		g.paused = false
		g.reason = pauseNone
		g.pausedAt = time.Time{} // zero so a running status omits PausedAt
		close(g.resumeCh)
		g.resumeCh = make(chan struct{})
	}
}

// rebaseline resets the checkpoint window without changing pause state. Used at
// broker construction so a state file with prior usage doesn't trip an instant
// pause on the first turn of a new session.
func (g *governor) rebaseline(tokens int, cost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.turns = 0
	g.baseTokens = tokens
	g.baseCost = cost
}

// bumpBudget raises the session thresholds (the "Continue +budget" action).
func (g *governor) bumpBudget(addTokens int, addCost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Only raise a threshold that is currently enabled. A 0 threshold means
	// "this gate is disabled" (config contract); resume_more must not silently
	// turn it on.
	if addTokens > 0 && g.cfg.MaxSessionTokens > 0 {
		g.cfg.MaxSessionTokens += addTokens
	}
	if addCost > 0 && g.cfg.MaxSessionCostUsd > 0 {
		g.cfg.MaxSessionCostUsd += addCost
	}
}

// governorStatus is the JSON shape returned by GET /governor and pushed over
// SSE. "...SinceCheckpoint" fields are spend since the last human checkpoint.
type governorStatus struct {
	Paused                bool        `json:"paused"`
	Reason                pauseReason `json:"reason"`
	PausedAt              string      `json:"pausedAt,omitempty"`
	TurnsSinceCheckpoint  int         `json:"turnsSinceCheckpoint"`
	TokensSinceCheckpoint int         `json:"tokensSinceCheckpoint"`
	CostSinceCheckpoint   float64     `json:"costSinceCheckpoint"`
	MaxTokens             int         `json:"maxTokens"`
	MaxCostUsd            float64     `json:"maxCostUsd"`
	MaxTurns              int         `json:"maxTurns"`
	Disabled              bool        `json:"disabled"`
}

func (g *governor) status(tokens int, cost float64) governorStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := governorStatus{
		Paused:                g.paused,
		Reason:                g.reason,
		TurnsSinceCheckpoint:  g.turns,
		TokensSinceCheckpoint: tokens - g.baseTokens,
		CostSinceCheckpoint:   cost - g.baseCost,
		MaxTokens:             g.cfg.MaxSessionTokens,
		MaxCostUsd:            g.cfg.MaxSessionCostUsd,
		MaxTurns:              g.cfg.MaxTurnsPerGate,
		Disabled:              g.cfg.Disabled,
	}
	if !g.pausedAt.IsZero() {
		st.PausedAt = g.pausedAt.UTC().Format(time.RFC3339)
	}
	// A delayed usage event could land after a rebaseline and make the delta
	// negative; clamp so the UI never shows "-3 tokens since checkpoint".
	if st.TokensSinceCheckpoint < 0 {
		st.TokensSinceCheckpoint = 0
	}
	if st.CostSinceCheckpoint < 0 {
		st.CostSinceCheckpoint = 0
	}
	return st
}
