package team

import (
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
			b.tasks[i].Status = "canceled"
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
			if task.Status != "canceled" {
				t.Fatalf("expected original self-healing task to stay canceled, got %+v", task)
			}
			continue
		}
		replacement = task
	}
	if replacement.ID == "" {
		t.Fatalf("expected replacement self-healing task, got %+v", selfHealingTasks)
	}
	if replacement.Status != "in_progress" {
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
