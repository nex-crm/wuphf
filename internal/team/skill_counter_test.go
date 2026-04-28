package team

// skill_counter_test.go covers the Hermes-style counter semantics in
// isolation from the broker hot path: threshold, reset, cooldown, and
// the recent-tool-call ring buffer. Broker-level wiring (the actual nudge
// task creation) is exercised in skill_review_nudge_test.go.

import (
	"testing"
	"time"
)

func TestSkillCounter_IncrementUnderThreshold(t *testing.T) {
	c := NewSkillCounterWith(10, 60*time.Minute)
	for i := 1; i <= 9; i++ {
		fired, n := c.Increment("ceo", "team_broadcast", "hello")
		if fired {
			t.Fatalf("iter %d: expected no nudge, got shouldNudge=true", i)
		}
		if n != i {
			t.Fatalf("iter %d: expected iterations=%d, got %d", i, i, n)
		}
	}
}

func TestSkillCounter_ThresholdFires(t *testing.T) {
	c := NewSkillCounterWith(10, 60*time.Minute)
	for i := 1; i <= 9; i++ {
		if fired, _ := c.Increment("ceo", "team_broadcast", ""); fired {
			t.Fatalf("iter %d: nudge fired before threshold", i)
		}
	}
	fired, n := c.Increment("ceo", "team_broadcast", "")
	if !fired {
		t.Fatalf("threshold iter: expected shouldNudge=true, got false")
	}
	if n != 0 {
		t.Fatalf("threshold iter: expected iterations reset to 0, got %d", n)
	}

	stats := c.Stats()["ceo"]
	if stats.NudgesFiredTotal != 1 {
		t.Fatalf("expected NudgesFiredTotal=1, got %d", stats.NudgesFiredTotal)
	}
}

func TestSkillCounter_ResetOnSkillCreate(t *testing.T) {
	c := NewSkillCounterWith(10, 60*time.Minute)

	// 5 increments — under threshold.
	for i := 1; i <= 5; i++ {
		if fired, _ := c.Increment("ceo", "team_broadcast", ""); fired {
			t.Fatalf("iter %d: unexpected nudge fired", i)
		}
	}

	// Reset — simulates team_skill_create / team_skill_patch.
	c.Reset("ceo")

	// Another 5 increments — total is 10 cumulative but only 5 since reset.
	for i := 1; i <= 5; i++ {
		fired, n := c.Increment("ceo", "team_broadcast", "")
		if fired {
			t.Fatalf("post-reset iter %d: nudge fired despite reset", i)
		}
		if n != i {
			t.Fatalf("post-reset iter %d: expected iterations=%d, got %d", i, i, n)
		}
	}
}

func TestSkillCounter_CooldownPreventsRespam(t *testing.T) {
	c := NewSkillCounterWith(3, 5*time.Minute)
	// Drive through the threshold once.
	c.Increment("ceo", "team_broadcast", "")
	c.Increment("ceo", "team_broadcast", "")
	fired, _ := c.Increment("ceo", "team_broadcast", "")
	if !fired {
		t.Fatalf("first cycle: expected nudge to fire at threshold")
	}

	// Drive through it again immediately — cooldown must suppress.
	c.Increment("ceo", "team_broadcast", "")
	c.Increment("ceo", "team_broadcast", "")
	fired2, _ := c.Increment("ceo", "team_broadcast", "")
	if fired2 {
		t.Fatalf("second cycle: cooldown failed to suppress respam")
	}

	stats := c.Stats()["ceo"]
	if stats.NudgesFiredTotal != 1 {
		t.Fatalf("expected NudgesFiredTotal=1 (cooldown blocked second), got %d", stats.NudgesFiredTotal)
	}
}

func TestSkillCounter_CooldownExpiresAndReFires(t *testing.T) {
	c := NewSkillCounterWith(3, 5*time.Minute)
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	c.SetClock(func() time.Time { return now })

	// First cycle.
	c.Increment("ceo", "team_broadcast", "")
	c.Increment("ceo", "team_broadcast", "")
	if fired, _ := c.Increment("ceo", "team_broadcast", ""); !fired {
		t.Fatalf("first cycle: expected nudge to fire")
	}

	// Advance past cooldown.
	now = now.Add(10 * time.Minute)
	c.SetClock(func() time.Time { return now })

	c.Increment("ceo", "team_broadcast", "")
	c.Increment("ceo", "team_broadcast", "")
	fired, _ := c.Increment("ceo", "team_broadcast", "")
	if !fired {
		t.Fatalf("second cycle after cooldown: expected nudge to fire again")
	}

	stats := c.Stats()["ceo"]
	if stats.NudgesFiredTotal != 2 {
		t.Fatalf("expected NudgesFiredTotal=2 across two cycles, got %d", stats.NudgesFiredTotal)
	}
}

func TestSkillCounter_PerAgentIsolated(t *testing.T) {
	c := NewSkillCounterWith(3, 60*time.Minute)
	// CEO increments alone.
	c.Increment("ceo", "team_broadcast", "")
	c.Increment("ceo", "team_broadcast", "")

	// Engineer increments — should be on its own counter.
	if fired, n := c.Increment("eng", "team_broadcast", ""); fired || n != 1 {
		t.Fatalf("eng first iter: expected (false, 1), got (%v, %d)", fired, n)
	}

	// CEO crosses threshold.
	if fired, _ := c.Increment("ceo", "team_broadcast", ""); !fired {
		t.Fatalf("ceo threshold: expected nudge")
	}

	// Engineer is still well under threshold.
	if fired, n := c.Increment("eng", "team_broadcast", ""); fired || n != 2 {
		t.Fatalf("eng second iter: expected (false, 2), got (%v, %d)", fired, n)
	}
}

func TestSkillCounter_RecentToolCalls(t *testing.T) {
	c := NewSkillCounterWith(100, 60*time.Minute)
	for i := 0; i < 5; i++ {
		c.Increment("ceo", "team_broadcast", "msg-"+string(rune('a'+i)))
	}
	calls := c.RecentToolCalls("ceo", 10)
	if len(calls) != 5 {
		t.Fatalf("expected 5 calls in buffer, got %d", len(calls))
	}
	if calls[0].Summary != "msg-a" || calls[4].Summary != "msg-e" {
		t.Fatalf("unexpected ring buffer ordering: %+v", calls)
	}
}

func TestSkillCounter_RecentToolCallsRingCaps(t *testing.T) {
	c := NewSkillCounterWith(1000, 60*time.Minute)
	for i := 0; i < 30; i++ {
		c.Increment("ceo", "tool", "summary")
	}
	calls := c.RecentToolCalls("ceo", 10)
	if len(calls) != 10 {
		t.Fatalf("expected the limit=10 to bound returned calls, got %d", len(calls))
	}
	// Ring buffer cap is 16; calling RecentToolCalls(..., 10) returns
	// the latest 10 of those 16. The buffer should never grow unbounded.
	if total := len(c.perAgent["ceo"].recentCalls); total > recentToolCallsCap {
		t.Fatalf("ring buffer exceeded cap: %d > %d", total, recentToolCallsCap)
	}
}

func TestSkillCounter_EmptySlugIsNoop(t *testing.T) {
	c := NewSkillCounterWith(3, 60*time.Minute)
	if fired, n := c.Increment("", "team_broadcast", ""); fired || n != 0 {
		t.Fatalf("empty slug: expected (false, 0), got (%v, %d)", fired, n)
	}
	c.Reset("")
	if got := len(c.Stats()); got != 0 {
		t.Fatalf("empty slug Reset created an entry: %d", got)
	}
}

func TestSkillCounter_TotalNudgesFired(t *testing.T) {
	c := NewSkillCounterWith(2, 0) // no cooldown so we can fire fast
	c.Increment("ceo", "t", "")
	c.Increment("ceo", "t", "") // fire #1 for ceo
	c.Increment("eng", "t", "")
	c.Increment("eng", "t", "") // fire #1 for eng
	if total := c.TotalNudgesFired(); total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
}

func TestIsSkillAuthoringTool(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"team_skill_create", true},
		{"team_skill_patch", true},
		{"team_skill_run", false},
		{"team_broadcast", false},
		{"  team_skill_create  ", true}, // trims
		{"", false},
	}
	for _, tc := range cases {
		got := IsSkillAuthoringTool(tc.name)
		if got != tc.want {
			t.Fatalf("IsSkillAuthoringTool(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
