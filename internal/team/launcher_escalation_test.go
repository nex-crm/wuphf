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
