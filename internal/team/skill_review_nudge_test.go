package team

// skill_review_nudge_test.go covers the broker-level wiring: when the
// SkillCounter crosses the threshold via the tool-event hot path, a
// skill_review_nudge task should land in b.tasks with the agent as
// owner and the right shape.

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFireSkillReviewNudge_CreatesTask(t *testing.T) {
	b := newTestBroker(t)
	// Inject a counter with a low threshold so we don't have to spam.
	b.SetSkillCounter(NewSkillCounterWith(3, 60*time.Minute))

	// Drive 3 tool events through recordAgentToolEvent — the third should
	// fire the nudge task.
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", `{"text":"hi"}`)
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", `{"text":"again"}`)
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", `{"text":"third"}`)

	b.mu.Lock()
	defer b.mu.Unlock()

	var nudgeTask *teamTask
	for i := range b.tasks {
		if b.tasks[i].TaskType == skillReviewNudgeTaskType {
			nudgeTask = &b.tasks[i]
			break
		}
	}
	if nudgeTask == nil {
		t.Fatalf("expected a %s task, got %d tasks total", skillReviewNudgeTaskType, len(b.tasks))
	}
	if nudgeTask.Owner != "ceo" {
		t.Fatalf("expected Owner=ceo, got %q", nudgeTask.Owner)
	}
	if nudgeTask.PipelineID != "skill_review" {
		t.Fatalf("expected PipelineID=skill_review, got %q", nudgeTask.PipelineID)
	}
	if nudgeTask.ExecutionMode != "office" {
		t.Fatalf("expected ExecutionMode=office, got %q", nudgeTask.ExecutionMode)
	}
	if nudgeTask.Status() != "in_progress" {
		t.Fatalf("expected Status=in_progress, got %q", nudgeTask.Status())
	}
	if nudgeTask.Title != skillReviewNudgeTitle {
		t.Fatalf("unexpected Title: %q", nudgeTask.Title)
	}
	if !strings.Contains(nudgeTask.Details, "team_broadcast") {
		t.Fatalf("expected task body to list recent tool calls; got:\n%s", nudgeTask.Details)
	}
	if !strings.Contains(nudgeTask.Details, "team_skill_create(action=propose)") {
		t.Fatalf("expected task body to mention team_skill_create(action=propose); got:\n%s", nudgeTask.Details)
	}

	if got := atomic.LoadInt64(&b.skillCompileMetrics.CounterNudgesFiredTotal); got != 1 {
		t.Fatalf("expected CounterNudgesFiredTotal=1, got %d", got)
	}
}

func TestRecordAgentToolEvent_SkillCreateResetsCounter(t *testing.T) {
	b := newTestBroker(t)
	b.SetSkillCounter(NewSkillCounterWith(3, 60*time.Minute))

	// Two calls — counter at 2, no nudge yet.
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", "")
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", "")

	// team_skill_create resets, so the next two tool calls should not
	// trigger the threshold (cumulative would have been 4 without reset).
	b.recordAgentToolEvent("ceo", "call", "team_skill_create", "")
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", "")
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", "")

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, tk := range b.tasks {
		if tk.TaskType == skillReviewNudgeTaskType {
			t.Fatalf("expected NO nudge task after skill_create reset, got %s", tk.ID)
		}
	}
	if got := atomic.LoadInt64(&b.skillCompileMetrics.CounterNudgesFiredTotal); got != 0 {
		t.Fatalf("expected CounterNudgesFiredTotal=0 after reset, got %d", got)
	}
}

func TestRecordAgentToolEvent_OnlyCallPhaseCounts(t *testing.T) {
	b := newTestBroker(t)
	b.SetSkillCounter(NewSkillCounterWith(3, 60*time.Minute))

	// One real call — counter=1.
	b.recordAgentToolEvent("ceo", "call", "team_broadcast", "")
	// result + error are post-call follow-ups: must NOT increment.
	b.recordAgentToolEvent("ceo", "result", "team_broadcast", "")
	b.recordAgentToolEvent("ceo", "error", "team_broadcast", "")

	stats := b.skillCounter.Stats()["ceo"]
	if stats.Iterations != 1 {
		t.Fatalf("expected iterations=1 (only call phase counts), got %d", stats.Iterations)
	}
}

func TestRecordAgentToolEvent_EmptyPhaseTreatedAsCall(t *testing.T) {
	// Older agents may not tag the phase. We treat empty phase as "call"
	// so they still drive the counter. The post hot path posts call/result
	// separately so we won't double-count from one client.
	b := newTestBroker(t)
	b.SetSkillCounter(NewSkillCounterWith(3, 60*time.Minute))

	b.recordAgentToolEvent("ceo", "", "team_broadcast", "")
	b.recordAgentToolEvent("ceo", "", "team_broadcast", "")
	stats := b.skillCounter.Stats()["ceo"]
	if stats.Iterations != 2 {
		t.Fatalf("empty-phase increments expected to count, got iterations=%d", stats.Iterations)
	}
}

func TestRecordAgentToolEvent_NoSlugNoEffect(t *testing.T) {
	b := newTestBroker(t)
	b.SetSkillCounter(NewSkillCounterWith(3, 60*time.Minute))

	b.recordAgentToolEvent("", "call", "team_broadcast", "")
	b.recordAgentToolEvent("ceo", "call", "", "")

	stats := b.skillCounter.Stats()
	if len(stats) != 0 {
		t.Fatalf("expected no agents tracked, got %d", len(stats))
	}
}

func TestBuildSkillReviewNudgeBody_EmptyRecent(t *testing.T) {
	body := buildSkillReviewNudgeBody(0, nil)
	if !strings.Contains(body, "team_skill_create(action=propose)") {
		t.Fatalf("body missing instructions: %s", body)
	}
	if strings.Contains(body, "Recent activity:") {
		t.Fatalf("empty case should NOT include the recent-activity list: %s", body)
	}
}

func TestBuildSkillReviewNudgeBody_WithRecent(t *testing.T) {
	now := time.Now().UTC()
	recent := []recentToolCall{
		{ToolName: "team_broadcast", Summary: "post a message", At: now},
		{ToolName: "team_wiki_read", Summary: "look up an article", At: now.Add(time.Minute)},
	}
	body := buildSkillReviewNudgeBody(2, recent)
	if !strings.Contains(body, "team_broadcast") {
		t.Fatalf("body missing first tool name: %s", body)
	}
	if !strings.Contains(body, "team_wiki_read") {
		t.Fatalf("body missing second tool name: %s", body)
	}
	if !strings.Contains(body, "Recent activity:") {
		t.Fatalf("body missing recent-activity header: %s", body)
	}
}
