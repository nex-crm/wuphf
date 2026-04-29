package team

// skill_counter.go owns the Hermes-style per-agent activity counter that
// drives the Stage B' "skill review" nudge. The contract:
//
//  1. Every successful agent MCP tool call increments iters_since_skill[slug].
//  2. team_skill_create / team_skill_patch reset the counter to 0 (the agent
//     just codified something — the tally restarts from there).
//  3. When the counter crosses WUPHF_SKILL_NUDGE_INTERVAL (default 10), the
//     broker fires a "skill_review_nudge" task into the agent's lane. The
//     agent picks the task up on its next turn and decides whether the work
//     pattern is worth proposing as a reusable skill.
//  4. A cooldown (WUPHF_SKILL_NUDGE_COOLDOWN, default 60m) prevents the same
//     agent from getting nudged repeatedly while their previous nudge sits
//     unhandled.
//
// The counter also keeps a small ring buffer of recent tool-call summaries
// so the nudge body can list "here is what you have been doing" without the
// broker having to re-query the action log.

import (
	"os"
	"strings"
	"sync"
	"time"
)

// Default thresholds. Both are overridable via env vars; tests can also
// override directly through the constructor or setter methods.
const (
	defaultSkillNudgeInterval = 10
	defaultSkillNudgeCooldown = 60 * time.Minute
	// recentToolCallsCap is how many recent tool-call summaries we keep
	// per agent. The nudge body lists the last 10, so 16 gives a small
	// cushion if a few were summarized away.
	recentToolCallsCap = 16
)

// SkillCounterMetrics exposes per-agent telemetry suitable for serialization
// in the /skills/compile/stats response. Returned via SkillCounter.Stats.
type SkillCounterMetrics struct {
	Iterations       int       `json:"iterations"`
	LastResetAt      time.Time `json:"last_reset_at,omitempty"`
	LastNudgedAt     time.Time `json:"last_nudged_at,omitempty"`
	NudgesFiredTotal int64     `json:"nudges_fired_total"`
}

// recentToolCall is one entry in the per-agent ring buffer.
type recentToolCall struct {
	ToolName string
	Summary  string
	At       time.Time
}

// agentCounterState is the mutable per-agent record. The parent SkillCounter
// guards access via its own mutex.
type agentCounterState struct {
	iterations       int
	lastResetAt      time.Time
	lastNudgedAt     time.Time
	nudgesFiredTotal int64
	recentCalls      []recentToolCall
}

// SkillCounter is the broker-side state for the Hermes counter pattern.
// It owns its own mutex so callers can invoke Increment / Reset / Stats from
// any goroutine — including the MCP tool-event hot path — without holding
// b.mu. The threshold + cooldown are immutable post-construction; callers
// who need to test edge cases use NewSkillCounterWith for direct overrides.
type SkillCounter struct {
	mu        sync.Mutex
	perAgent  map[string]*agentCounterState
	threshold int
	cooldown  time.Duration
	// nowFn is injected for deterministic tests; defaults to time.Now.
	nowFn func() time.Time
}

// NewSkillCounter constructs a counter using the env-configured threshold
// and cooldown. Use NewSkillCounterWith from tests that want explicit
// thresholds.
func NewSkillCounter() *SkillCounter {
	return NewSkillCounterWith(skillNudgeIntervalFromEnv(), skillNudgeCooldownFromEnv())
}

// NewSkillCounterWith constructs a counter with explicit threshold +
// cooldown. Threshold <= 0 is normalized to 1 to avoid an off-by-one in
// callers that pass 0. Cooldown < 0 is normalized to 0 (no cooldown).
func NewSkillCounterWith(threshold int, cooldown time.Duration) *SkillCounter {
	if threshold <= 0 {
		threshold = 1
	}
	if cooldown < 0 {
		cooldown = 0
	}
	return &SkillCounter{
		perAgent:  make(map[string]*agentCounterState),
		threshold: threshold,
		cooldown:  cooldown,
		nowFn:     time.Now,
	}
}

// SetClock replaces the counter's time source. Tests use this to drive
// cooldown logic deterministically.
func (c *SkillCounter) SetClock(now func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now == nil {
		c.nowFn = time.Now
		return
	}
	c.nowFn = now
}

// Threshold returns the configured nudge threshold. Useful for tests and
// telemetry.
func (c *SkillCounter) Threshold() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.threshold
}

// Cooldown returns the configured nudge cooldown.
func (c *SkillCounter) Cooldown() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cooldown
}

// Increment registers one tool call for agentSlug. Returns shouldNudge=true
// when the counter has reached the threshold AND no nudge has fired within
// the cooldown window. When shouldNudge is true, Increment also resets the
// counter to 0 and stamps lastNudgedAt — treat one nudge fire as the
// "reset event" so the next nudge requires another N tool calls.
//
// toolName + summary are recorded in the per-agent ring buffer so the
// nudge task body can list recent activity. Empty toolName is allowed
// (caller already validated) but is recorded as "(unknown)" for clarity.
func (c *SkillCounter) Increment(agentSlug, toolName, summary string) (shouldNudge bool, iterations int) {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return false, 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.getOrCreateLocked(agentSlug)
	st.iterations++
	if toolName == "" {
		toolName = "(unknown)"
	}
	st.recentCalls = append(st.recentCalls, recentToolCall{
		ToolName: toolName,
		Summary:  summary,
		At:       c.nowFn(),
	})
	if len(st.recentCalls) > recentToolCallsCap {
		// Drop the oldest. Allocate a fresh slice rather than re-slice in
		// place so the underlying array shrinks; otherwise long-lived agents
		// keep a buffer N times larger than they need.
		cp := make([]recentToolCall, recentToolCallsCap)
		copy(cp, st.recentCalls[len(st.recentCalls)-recentToolCallsCap:])
		st.recentCalls = cp
	}

	if st.iterations < c.threshold {
		return false, st.iterations
	}

	// Threshold met — check cooldown.
	now := c.nowFn()
	if c.cooldown > 0 && !st.lastNudgedAt.IsZero() && now.Sub(st.lastNudgedAt) < c.cooldown {
		// Still in cooldown. Hold the iteration count steady at the
		// threshold — we don't want to keep counting forever, but we
		// also don't want to reset because the agent's next nudge eligibility
		// depends on cooldown elapsing, not on N more tool calls.
		st.iterations = c.threshold
		return false, st.iterations
	}

	// Fire the nudge. Reset iterations and stamp lastNudgedAt.
	st.iterations = 0
	st.lastNudgedAt = now
	st.lastResetAt = now
	st.nudgesFiredTotal++
	return true, 0
}

// Reset clears the per-agent counter without firing a nudge. Called when
// the agent invokes team_skill_create or team_skill_patch — they just
// codified something, so the tally restarts from zero. Reset never blocks
// a future nudge: it only zeroes iterations + records the reset time.
func (c *SkillCounter) Reset(agentSlug string) {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.getOrCreateLocked(agentSlug)
	st.iterations = 0
	st.lastResetAt = c.nowFn()
}

// Stats returns a copy of the per-agent metrics suitable for JSON
// serialization. Safe to call concurrently with Increment / Reset.
func (c *SkillCounter) Stats() map[string]SkillCounterMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.perAgent) == 0 {
		return map[string]SkillCounterMetrics{}
	}
	out := make(map[string]SkillCounterMetrics, len(c.perAgent))
	for slug, st := range c.perAgent {
		out[slug] = SkillCounterMetrics{
			Iterations:       st.iterations,
			LastResetAt:      st.lastResetAt,
			LastNudgedAt:     st.lastNudgedAt,
			NudgesFiredTotal: st.nudgesFiredTotal,
		}
	}
	return out
}

// TotalNudgesFired sums NudgesFiredTotal across all tracked agents. Used by
// the broker for the aggregate counter_nudges_fired_total telemetry value.
func (c *SkillCounter) TotalNudgesFired() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for _, st := range c.perAgent {
		total += st.nudgesFiredTotal
	}
	return total
}

// RecentToolCalls returns a snapshot of up to limit most-recent calls for
// agentSlug, oldest-first. Returns nil if the agent has no tracked calls.
// Used by fireSkillReviewNudgeLocked to build the task body.
func (c *SkillCounter) RecentToolCalls(agentSlug string, limit int) []recentToolCall {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" || limit <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	st, ok := c.perAgent[agentSlug]
	if !ok || len(st.recentCalls) == 0 {
		return nil
	}
	start := 0
	if len(st.recentCalls) > limit {
		start = len(st.recentCalls) - limit
	}
	out := make([]recentToolCall, 0, len(st.recentCalls)-start)
	out = append(out, st.recentCalls[start:]...)
	return out
}

// getOrCreateLocked returns the agent record, creating it if absent.
// Caller must hold c.mu.
func (c *SkillCounter) getOrCreateLocked(slug string) *agentCounterState {
	if st, ok := c.perAgent[slug]; ok {
		return st
	}
	st := &agentCounterState{
		recentCalls: make([]recentToolCall, 0, recentToolCallsCap),
	}
	c.perAgent[slug] = st
	return st
}

// IsSkillAuthoringTool reports whether toolName corresponds to one of the
// skill-authoring MCP tools whose invocation should reset the counter
// instead of incrementing it. Lives here (not in teammcp) so the broker
// hot path stays inside the team package and avoids an import cycle.
func IsSkillAuthoringTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "team_skill_create", "team_skill_patch":
		return true
	}
	return false
}

// ── env helpers ───────────────────────────────────────────────────────────

func skillNudgeIntervalFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("WUPHF_SKILL_NUDGE_INTERVAL"))
	if raw == "" {
		return defaultSkillNudgeInterval
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return defaultSkillNudgeInterval
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return defaultSkillNudgeInterval
	}
	return n
}

func skillNudgeCooldownFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_SKILL_NUDGE_COOLDOWN"))
	if raw == "" {
		return defaultSkillNudgeCooldown
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return defaultSkillNudgeCooldown
	}
	return d
}
