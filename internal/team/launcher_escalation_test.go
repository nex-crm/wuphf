package team

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

func TestPostEscalation_WritesToGeneralChannel(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	l.postEscalation("eng", "eng-42", agent.EscalationStuck, "stuck in build_context for 20 ticks")

	msgs := b.ChannelMessages("general")
	for _, m := range msgs {
		if strings.Contains(m.Content, "stuck") || strings.Contains(m.Content, "Heads up") {
			return
		}
	}
	t.Fatalf("expected escalation message in #general, found none; got %d messages", len(msgs))
}

func TestPostEscalation_MaxRetries_WritesToGeneralChannel(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	l.postEscalation("pm", "pm-7", agent.EscalationMaxRetries, "tool_call failed: timeout")

	msgs := b.ChannelMessages("general")
	for _, m := range msgs {
		if strings.Contains(m.Content, "Heads up") && strings.Contains(m.Content, "erroring") {
			return
		}
	}
	t.Fatalf("expected max-retries escalation message in #general, found none; got %d messages", len(msgs))
}

func TestPostEscalation_CreatesSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	l.postEscalation("eng", "eng-42", agent.EscalationStuck, "stuck in build_context for 20 ticks")

	var found teamTask
	for _, task := range b.AllTasks() {
		if task.Title == "Self-heal @eng on eng-42" {
			found = task
			break
		}
	}
	if found.ID == "" {
		t.Fatalf("expected self-healing task, got %+v", b.AllTasks())
	}
	if found.Owner != "ceo" {
		t.Fatalf("expected self-healing task owned by ceo, got %+v", found)
	}
	if found.TaskType != "incident" || found.PipelineID != "incident" || found.ExecutionMode != "office" {
		t.Fatalf("expected incident office task, got %+v", found)
	}
	if !strings.Contains(found.Details, "Classify the blocker") || !strings.Contains(found.Details, "missing or outdated skill/playbook") {
		t.Fatalf("expected repair-loop details, got %q", found.Details)
	}
}

func TestPostEscalation_ReusesSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	l.postEscalation("eng", "eng-42", agent.EscalationStuck, "first stuck event")
	l.postEscalation("eng", "eng-42", agent.EscalationMaxRetries, "second stuck event")

	var count int
	var found teamTask
	for _, task := range b.AllTasks() {
		if task.Title == "Self-heal @eng on eng-42" {
			count++
			found = task
		}
	}
	if count != 1 {
		t.Fatalf("expected one reusable self-healing task, got %d tasks: %+v", count, b.AllTasks())
	}
	if !strings.Contains(found.Details, "first stuck event") || !strings.Contains(found.Details, "second stuck event") {
		t.Fatalf("expected reused self-healing task to retain both incident details, got %q", found.Details)
	}
	if got := strings.Count(found.Details, "Latest incident:"); got != 1 {
		t.Fatalf("expected one appended latest incident block, got %d in %q", got, found.Details)
	}
}

func TestPostEscalation_DoesNotReuseCanceledSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	l.postEscalation("eng", "eng-42", agent.EscalationStuck, "first stuck event")

	var canceledID string
	for _, task := range b.AllTasks() {
		if task.Title == "Self-heal @eng on eng-42" {
			canceledID = task.ID
			break
		}
	}
	if canceledID == "" {
		t.Fatalf("expected initial self-healing task, got %+v", b.AllTasks())
	}

	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID == canceledID {
			b.tasks[i].status = "canceled"
			break
		}
	}
	b.mu.Unlock()

	l.postEscalation("eng", "eng-42", agent.EscalationMaxRetries, "second stuck event")
	l.postEscalation("eng", "eng-42", agent.EscalationStuck, "third stuck event")

	var selfHealingTasks []teamTask
	for _, task := range b.AllTasks() {
		if task.Title == "Self-heal @eng on eng-42" {
			selfHealingTasks = append(selfHealingTasks, task)
		}
	}
	if len(selfHealingTasks) != 2 {
		t.Fatalf("expected canceled task plus one active replacement, got %d tasks: %+v", len(selfHealingTasks), b.AllTasks())
	}

	var replacement teamTask
	for _, task := range selfHealingTasks {
		if task.ID == canceledID {
			if task.status != "canceled" {
				t.Fatalf("expected original self-healing task to stay canceled, got %+v", task)
			}
			continue
		}
		replacement = task
	}
	if replacement.ID == "" {
		t.Fatalf("expected replacement self-healing task, got %+v", selfHealingTasks)
	}
	if replacement.Status() != "in_progress" {
		t.Fatalf("expected replacement self-healing task to be actionable, got %+v", replacement)
	}
	if !strings.Contains(replacement.Details, "second stuck event") || !strings.Contains(replacement.Details, "third stuck event") {
		t.Fatalf("expected replacement to collect new incident details, got %q", replacement.Details)
	}
}

func TestPostEscalation_DoesNotNestSelfHealingTasks(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	task, _, err := l.requestSelfHealing("eng", "eng-42", agent.EscalationStuck, "initial incident")
	if err != nil {
		t.Fatalf("request self-healing: %v", err)
	}
	l.postEscalation("ceo", task.ID, agent.EscalationStuck, "self-healing lane stuck")

	var count int
	for _, candidate := range b.AllTasks() {
		if isSelfHealingTaskTitle(candidate.Title) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected no nested self-healing task, got %d tasks: %+v", count, b.AllTasks())
	}
}

// TestPostEscalation_CapsActiveSelfHealingPerAgent guards the per-agent
// active-self-heal cap. Without the cap, an agent that fails on N distinct
// task IDs (one per stuck escalation) accumulates N self-heal tasks because
// the dedupe in requestSelfHealingLocked is keyed on (agent, taskID) and
// each new taskID lands a new entry. In practice this produced hundreds of
// stale incidents per agent on long-running installs; the cap collapses
// overflow into the most recent active self-heal so the agent still has a
// single, well-trafficked repair lane to fix.
func TestPostEscalation_CapsActiveSelfHealingPerAgent(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}
	const burst = 25

	for i := 0; i < burst; i++ {
		l.postEscalation("eng", fmt.Sprintf("eng-%d", i), agent.EscalationStuck, fmt.Sprintf("stuck event %d", i))
	}

	var active int
	var mostRecent teamTask
	for _, candidate := range b.AllTasks() {
		if !isSelfHealingTaskTitle(candidate.Title) {
			continue
		}
		if isTerminalTeamTaskStatus(candidate.Status()) {
			continue
		}
		if !strings.Contains(candidate.Title, "@eng") {
			continue
		}
		active++
		if mostRecent.ID == "" || candidate.UpdatedAt > mostRecent.UpdatedAt {
			mostRecent = candidate
		}
	}
	if active > maxActiveSelfHealsPerAgent {
		t.Fatalf("expected at most %d active self-heal tasks for @eng, got %d", maxActiveSelfHealsPerAgent, active)
	}
	if mostRecent.ID == "" {
		t.Fatal("expected at least one active self-heal task to absorb the burst")
	}
	if !strings.Contains(mostRecent.Details, fmt.Sprintf("stuck event %d", burst-1)) {
		t.Fatalf("expected most recent self-heal to absorb the latest incident, details=%q", mostRecent.Details)
	}
}

// countActiveSelfHealsForAgent mirrors the prod overflow lookup: anchored
// "@<slug> " match so prefix-overlapping slugs (eng vs engineering) do not
// blur the count.
func countActiveSelfHealsForAgent(b *Broker, agentSlug string) int {
	needle := "@" + agentSlug + " "
	n := 0
	for _, task := range b.AllTasks() {
		if !isSelfHealingTaskTitle(task.Title) || isTerminalTeamTaskStatus(task.status) {
			continue
		}
		if strings.Contains(task.Title, needle) {
			n++
		}
	}
	return n
}

// TestRequestSelfHealing_RejectsAgentSlugSubstringCollision guards the
// title-needle anchoring in findOverflowSelfHealForAgentLocked. With a naive
// substring match, an @eng request would steal a merge slot from an
// @engineering self-heal whose slug @eng is a prefix of. The anchored
// "@<slug> " needle keeps each agent's repair lane isolated.
func TestRequestSelfHealing_RejectsAgentSlugSubstringCollision(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	for i := 0; i < maxActiveSelfHealsPerAgent; i++ {
		l.postEscalation("engineering", fmt.Sprintf("eng-%d", i), agent.EscalationStuck, "engineering blocker")
	}
	if got := countActiveSelfHealsForAgent(b, "engineering"); got != maxActiveSelfHealsPerAgent {
		t.Fatalf("setup: expected @engineering at cap (%d), got %d", maxActiveSelfHealsPerAgent, got)
	}

	l.postEscalation("eng", "task-99", agent.EscalationStuck, "eng first incident")

	if got := countActiveSelfHealsForAgent(b, "engineering"); got != maxActiveSelfHealsPerAgent {
		t.Fatalf("@engineering active count must not change, got %d", got)
	}
	if got := countActiveSelfHealsForAgent(b, "eng"); got != 1 {
		t.Fatalf("expected one fresh @eng self-heal, got %d", got)
	}
}

// TestRequestSelfHealing_PerAgentIndependent guards that one agent at the
// cap does not consume another agent's first self-heal slot via overflow
// merging. Each agent has its own repair lane.
func TestRequestSelfHealing_PerAgentIndependent(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	for i := 0; i < maxActiveSelfHealsPerAgent+5; i++ {
		l.postEscalation("eng", fmt.Sprintf("eng-%d", i), agent.EscalationStuck, "eng burst")
	}
	engBefore := countActiveSelfHealsForAgent(b, "eng")
	if engBefore != maxActiveSelfHealsPerAgent {
		t.Fatalf("setup: expected @eng pinned at cap (%d), got %d", maxActiveSelfHealsPerAgent, engBefore)
	}

	l.postEscalation("pm", "pm-1", agent.EscalationStuck, "pm first incident")

	if got := countActiveSelfHealsForAgent(b, "eng"); got != engBefore {
		t.Fatalf("@eng active count must not change after @pm request, got %d (was %d)", got, engBefore)
	}
	if got := countActiveSelfHealsForAgent(b, "pm"); got != 1 {
		t.Fatalf("expected one fresh @pm self-heal, got %d", got)
	}
}

// TestRequestSelfHealing_ExactReuseWinsOverOverflow guards the priority of
// the exact (agent, taskID) reuse path over the overflow-merge path. When
// an agent is at the cap and a new request matches an existing self-heal's
// title exactly, that existing task is updated in place — not merged into
// the most-recently-updated unrelated overflow target.
func TestRequestSelfHealing_ExactReuseWinsOverOverflow(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	taskIDs := make([]string, maxActiveSelfHealsPerAgent)
	for i := 0; i < maxActiveSelfHealsPerAgent; i++ {
		taskIDs[i] = fmt.Sprintf("eng-%d", i)
		l.postEscalation("eng", taskIDs[i], agent.EscalationStuck, "first")
	}

	// The OLDEST is what we re-fire on; the NEWEST is the overflow target
	// (most-recently updated active self-heal for @eng). If the exact-match
	// path wins, the new incident lands on OLDEST. If overflow wins, it
	// lands on NEWEST. The two body assertions disambiguate.
	oldestTitle := selfHealingTaskTitle("eng", taskIDs[0])
	newestTitle := selfHealingTaskTitle("eng", taskIDs[len(taskIDs)-1])
	var oldestID, newestID string
	for _, task := range b.AllTasks() {
		switch task.Title {
		case oldestTitle:
			oldestID = task.ID
		case newestTitle:
			newestID = task.ID
		}
	}
	if oldestID == "" || newestID == "" {
		t.Fatalf("setup: missing self-heal tasks (oldest=%q newest=%q)", oldestID, newestID)
	}

	l.postEscalation("eng", taskIDs[0], agent.EscalationMaxRetries, "second incident")

	if got := countActiveSelfHealsForAgent(b, "eng"); got != maxActiveSelfHealsPerAgent {
		t.Fatalf("active count must stay at cap (%d), got %d", maxActiveSelfHealsPerAgent, got)
	}
	var oldestAfter, newestAfter teamTask
	for _, task := range b.AllTasks() {
		switch task.ID {
		case oldestID:
			oldestAfter = task
		case newestID:
			newestAfter = task
		}
	}
	if !strings.Contains(oldestAfter.Details, "second incident") {
		t.Fatalf("exact-match path should append the new incident to OLDEST, details=%q", oldestAfter.Details)
	}
	if strings.Contains(newestAfter.Details, "second incident") {
		t.Fatalf("overflow target NEWEST must not absorb the incident, details=%q", newestAfter.Details)
	}
	if strings.Contains(oldestAfter.Details, "merged from per-agent self-heal overflow") {
		t.Fatalf("exact-match path must not use the overflow body marker, details=%q", oldestAfter.Details)
	}
}

// TestClampSelfHealCap guards that an env override of 0 or below falls back
// to the default cap. Accepting non-positive values would silently disable
// the cap and reintroduce the per-agent task explosion this fix prevents.
func TestClampSelfHealCap(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{in: -5, want: defaultMaxActiveSelfHealsPerAgent},
		{in: -1, want: defaultMaxActiveSelfHealsPerAgent},
		{in: 0, want: defaultMaxActiveSelfHealsPerAgent},
		{in: 1, want: 1},
		{in: defaultMaxActiveSelfHealsPerAgent, want: defaultMaxActiveSelfHealsPerAgent},
		{in: 25, want: 25},
	}
	for _, tc := range cases {
		if got := clampSelfHealCap(tc.in); got != tc.want {
			t.Errorf("clampSelfHealCap(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestPostEscalation_NilBroker_DoesNotPanic(t *testing.T) {
	l := &Launcher{broker: nil}
	// Should be a no-op, not a panic.
	l.postEscalation("eng", "eng-1", agent.EscalationStuck, "detail")
}

func TestPostEscalation_PostedBySystem(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}

	l.postEscalation("eng", "eng-99", agent.EscalationStuck, "some detail")

	msgs := b.ChannelMessages("general")
	if len(msgs) == 0 {
		t.Fatal("expected at least one message in #general")
	}
	last := msgs[len(msgs)-1]
	if last.From != "system" {
		t.Fatalf("expected message from 'system', got %q", last.From)
	}
}
