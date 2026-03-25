package tui

import (
	"strings"
	"testing"
	"time"
)

// drainOne reads a single message from the adapter with a timeout.
func drainOne(t *testing.T, ca *ChannelAdapter) StreamMessage {
	t.Helper()
	select {
	case msg := <-ca.Messages():
		return msg
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected a message from ChannelAdapter but got none")
		return StreamMessage{}
	}
}

// expectEmpty asserts no message is available on the adapter channel.
func expectEmpty(t *testing.T, ca *ChannelAdapter) {
	t.Helper()
	select {
	case msg := <-ca.Messages():
		t.Fatalf("expected no message but got: %q", msg.Content)
	case <-time.After(50 * time.Millisecond):
		// good — nothing received
	}
}

func TestChannelAdapterText(t *testing.T) {
	ca := NewChannelAdapter()
	ca.SetAgentName("ceo", "CEO")

	ca.HandleEvent(GossipEvent{
		FromSlug:  "ceo",
		Type:      "text",
		Content:   "Let's build the landing page",
		Timestamp: time.Now(),
	})

	msg := drainOne(t, ca)
	if msg.Role != "agent" {
		t.Errorf("expected role 'agent', got %q", msg.Role)
	}
	if msg.AgentSlug != "ceo" {
		t.Errorf("expected slug 'ceo', got %q", msg.AgentSlug)
	}
	if msg.AgentName != "CEO" {
		t.Errorf("expected agent name 'CEO', got %q", msg.AgentName)
	}
	if !strings.Contains(msg.Content, "CEO: Let's build the landing page") {
		t.Errorf("unexpected content: %q", msg.Content)
	}
}

func TestChannelAdapterToolUse(t *testing.T) {
	ca := NewChannelAdapter()
	ca.SetAgentName("fe", "FE")

	ca.HandleEvent(GossipEvent{
		FromSlug:  "fe",
		Type:      "tool_use",
		Content:   "Bash mkdir -p src/components",
		Timestamp: time.Now(),
	})

	msg := drainOne(t, ca)
	if msg.Role != "tool_use" {
		t.Errorf("expected role 'tool_use', got %q", msg.Role)
	}
	// Should show simplified format: ⚡ FE → Bash (no input args)
	expected := "⚡ FE → Bash"
	if !strings.Contains(msg.Content, expected) {
		t.Errorf("expected content containing %q, got %q", expected, msg.Content)
	}
	// Should NOT contain the full command
	if strings.Contains(msg.Content, "mkdir") {
		t.Errorf("tool_use should not include input args, got %q", msg.Content)
	}
}

func TestChannelAdapterToolResult(t *testing.T) {
	ca := NewChannelAdapter()

	longResult := strings.Repeat("x", 150)
	ca.HandleEvent(GossipEvent{
		FromSlug:  "fe",
		Type:      "tool_result",
		Content:   longResult,
		Timestamp: time.Now(),
	})

	msg := drainOne(t, ca)
	if msg.Role != "tool_result" {
		t.Errorf("expected role 'tool_result', got %q", msg.Role)
	}
	// Should be truncated: "  ↳ " + 80 chars + "..."
	if !strings.HasPrefix(msg.Content, "  ↳ ") {
		t.Errorf("expected tool_result prefix '  ↳ ', got %q", msg.Content)
	}
	// The content portion (after prefix) should be 80 + 3 ("...") = 83 chars
	resultPart := strings.TrimPrefix(msg.Content, "  ↳ ")
	if len(resultPart) != 83 {
		t.Errorf("expected truncated result of 83 chars, got %d: %q", len(resultPart), resultPart)
	}
	if !strings.HasSuffix(resultPart, "...") {
		t.Errorf("expected truncated result to end with '...', got %q", resultPart)
	}
}

func TestChannelAdapterSkipsThinking(t *testing.T) {
	ca := NewChannelAdapter()

	ca.HandleEvent(GossipEvent{
		FromSlug:  "ceo",
		Type:      "thinking",
		Content:   "I need to consider the architecture carefully...",
		Timestamp: time.Now(),
	})

	expectEmpty(t, ca)
}

func TestChannelAdapterSkipsEmptyContent(t *testing.T) {
	ca := NewChannelAdapter()

	ca.HandleEvent(GossipEvent{
		FromSlug:  "ceo",
		Type:      "text",
		Content:   "   ",
		Timestamp: time.Now(),
	})

	expectEmpty(t, ca)
}

func TestChannelAdapterDedup(t *testing.T) {
	ca := NewChannelAdapter()
	ca.SetAgentName("ceo", "CEO")

	now := time.Now()

	// First message
	ca.HandleEvent(GossipEvent{
		FromSlug:  "ceo",
		Type:      "text",
		Content:   "Hello team",
		Timestamp: now,
	})
	msg := drainOne(t, ca)
	if !strings.Contains(msg.Content, "Hello team") {
		t.Fatalf("first message not received: %q", msg.Content)
	}

	// Same content within 2 seconds — should be deduplicated
	ca.HandleEvent(GossipEvent{
		FromSlug:  "ceo",
		Type:      "text",
		Content:   "Hello team",
		Timestamp: now.Add(1 * time.Second),
	})
	expectEmpty(t, ca)

	// Same content after 2 seconds — should be allowed
	ca.HandleEvent(GossipEvent{
		FromSlug:  "ceo",
		Type:      "text",
		Content:   "Hello team",
		Timestamp: now.Add(3 * time.Second),
	})
	msg2 := drainOne(t, ca)
	if !strings.Contains(msg2.Content, "Hello team") {
		t.Fatalf("message after dedup window not received: %q", msg2.Content)
	}
}

func TestChannelAdapterDisplayNameFallback(t *testing.T) {
	ca := NewChannelAdapter()
	// No name registered — should capitalize slug

	ca.HandleEvent(GossipEvent{
		FromSlug:  "backend",
		Type:      "text",
		Content:   "Setting up routes",
		Timestamp: time.Now(),
	})

	msg := drainOne(t, ca)
	if !strings.Contains(msg.Content, "Backend: Setting up routes") {
		t.Errorf("expected fallback name 'Backend', got %q", msg.Content)
	}
}
