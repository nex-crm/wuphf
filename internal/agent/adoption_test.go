package agent_test

import (
	"os"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

func ptr(f float64) *float64 { return &f }

func freshInsight(relevance float64) agent.GossipInsight {
	return agent.GossipInsight{
		Content:   "test insight",
		Source:    "other-agent",
		Timestamp: time.Now().UnixMilli(),
		Relevance: relevance,
	}
}

func staleInsight(relevance float64, ageWeeks float64) agent.GossipInsight {
	ageMs := int64(ageWeeks * 7 * 24 * 60 * 60 * 1000)
	return agent.GossipInsight{
		Content:   "old insight",
		Source:    "other-agent",
		Timestamp: time.Now().UnixMilli() - ageMs,
		Relevance: relevance,
	}
}

// TestScoreInsight_Adopt verifies that a high-quality, fresh insight scores >= AdoptThreshold.
func TestScoreInsight_Adopt(t *testing.T) {
	insight := freshInsight(1.0)
	score := agent.ScoreInsight(insight, "", ptr(1.0))

	if score.Decision != "adopt" {
		t.Errorf("expected 'adopt', got %q (total=%.3f)", score.Decision, score.Total)
	}
	if score.Total < agent.AdoptThreshold {
		t.Errorf("expected total >= %.1f, got %.3f", agent.AdoptThreshold, score.Total)
	}
}

// TestScoreInsight_Test verifies that a mid-range insight scores in the test range.
func TestScoreInsight_Test(t *testing.T) {
	// cred=0.5, rel=0.5, fresh → total = 0.5*0.4 + 0.5*0.4 + ~1.0*0.2 = 0.6 → "test"
	insight := freshInsight(0.5)
	score := agent.ScoreInsight(insight, "", ptr(0.5))

	if score.Decision != "test" {
		t.Errorf("expected 'test', got %q (total=%.3f)", score.Decision, score.Total)
	}
	if score.Total >= agent.AdoptThreshold || score.Total < agent.TestThreshold {
		t.Errorf("expected total in [%.1f, %.1f), got %.3f",
			agent.TestThreshold, agent.AdoptThreshold, score.Total)
	}
}

// TestScoreInsight_Reject verifies that a poor-quality, very stale insight is rejected.
func TestScoreInsight_Reject(t *testing.T) {
	insight := staleInsight(0.0, 52) // 1-year-old insight with zero relevance
	score := agent.ScoreInsight(insight, "", ptr(0.0))

	if score.Decision != "reject" {
		t.Errorf("expected 'reject', got %q (total=%.3f)", score.Decision, score.Total)
	}
	if score.Total >= agent.TestThreshold {
		t.Errorf("expected total < %.1f, got %.3f", agent.TestThreshold, score.Total)
	}
}

// TestScoreInsight_DefaultCredibility verifies that nil credibility defaults to 0.5.
func TestScoreInsight_DefaultCredibility(t *testing.T) {
	insight := freshInsight(0.5)
	score := agent.ScoreInsight(insight, "", nil)

	if score.SourceCredibility != 0.5 {
		t.Errorf("expected default credibility 0.5, got %.2f", score.SourceCredibility)
	}
}

// TestScoreInsight_RelevanceClamped verifies that relevance > 1 is clamped to 1.
func TestScoreInsight_RelevanceClamped(t *testing.T) {
	insight := freshInsight(5.0) // out-of-range relevance
	score := agent.ScoreInsight(insight, "", ptr(1.0))

	if score.SemanticRelevance != 1.0 {
		t.Errorf("expected relevance clamped to 1.0, got %.2f", score.SemanticRelevance)
	}
}

// TestCredibilityTracker_DefaultScore verifies a fresh tracker returns 0.5.
func TestCredibilityTracker_DefaultScore(t *testing.T) {
	dir := t.TempDir()
	tracker := agent.NewCredibilityTracker(dir)

	score := tracker.GetCredibility("planner")
	if score != 0.5 {
		t.Errorf("expected default 0.5, got %.2f", score)
	}
}

// TestCredibilityTracker_RecordAndGet verifies success/failure tracking.
func TestCredibilityTracker_RecordAndGet(t *testing.T) {
	dir := t.TempDir()
	tracker := agent.NewCredibilityTracker(dir)

	tracker.RecordOutcome("planner", true)
	tracker.RecordOutcome("planner", true)
	tracker.RecordOutcome("planner", false)

	score := tracker.GetCredibility("planner")
	expected := 2.0 / 3.0
	if diff := score - expected; diff > 0.001 || diff < -0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, score)
	}
}

// TestCredibilityTracker_Persistence verifies scores survive a reload.
func TestCredibilityTracker_Persistence(t *testing.T) {
	dir := t.TempDir()

	t1 := agent.NewCredibilityTracker(dir)
	t1.RecordOutcome("researcher", true)
	t1.RecordOutcome("researcher", true)

	// Load from same directory
	t2 := agent.NewCredibilityTracker(dir)
	score := t2.GetCredibility("researcher")
	if score != 1.0 {
		t.Errorf("expected persisted score 1.0, got %.2f", score)
	}
}

// TestCredibilityTracker_AllFailures verifies 0.0 score when all outcomes fail.
func TestCredibilityTracker_AllFailures(t *testing.T) {
	dir := t.TempDir()
	tracker := agent.NewCredibilityTracker(dir)

	tracker.RecordOutcome("bad-agent", false)
	tracker.RecordOutcome("bad-agent", false)

	score := tracker.GetCredibility("bad-agent")
	if score != 0.0 {
		t.Errorf("expected 0.0 for all failures, got %.2f", score)
	}
}

// TestCredibilityTracker_MultipleAgents verifies isolation between agents.
func TestCredibilityTracker_MultipleAgents(t *testing.T) {
	dir := t.TempDir()
	tracker := agent.NewCredibilityTracker(dir)

	tracker.RecordOutcome("agent-a", true)
	tracker.RecordOutcome("agent-b", false)

	if tracker.GetCredibility("agent-a") != 1.0 {
		t.Errorf("agent-a should have 1.0")
	}
	if tracker.GetCredibility("agent-b") != 0.0 {
		t.Errorf("agent-b should have 0.0")
	}
}

// TestCredibilityTracker_CreatesDir verifies the tracker creates its directory on first save.
func TestCredibilityTracker_CreatesDir(t *testing.T) {
	parent := t.TempDir()
	dir := parent + "/nested/credibility"

	tracker := agent.NewCredibilityTracker(dir)
	tracker.RecordOutcome("agent", true)

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected directory to be created, got: %v", err)
	}
}
