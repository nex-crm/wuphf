package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ChannelAdapter converts raw GossipBus events into clean, readable
// StreamMessages suitable for the channel view.
type ChannelAdapter struct {
	messagesCh  chan StreamMessage   // output channel for clean messages
	lastContent map[string]string    // per-agent dedup: last content
	lastTime    map[string]time.Time // per-agent dedup: last message time
	agentNames  map[string]string    // slug → display name
	mu          sync.Mutex
}

// NewChannelAdapter creates a ChannelAdapter with buffered output channel.
func NewChannelAdapter() *ChannelAdapter {
	return &ChannelAdapter{
		messagesCh:  make(chan StreamMessage, 64),
		lastContent: make(map[string]string),
		lastTime:    make(map[string]time.Time),
		agentNames:  make(map[string]string),
	}
}

// SetAgentName registers a display name for an agent slug.
func (ca *ChannelAdapter) SetAgentName(slug, name string) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.agentNames[slug] = name
}

// Messages returns the channel for receiving clean StreamMessages.
func (ca *ChannelAdapter) Messages() <-chan StreamMessage {
	return ca.messagesCh
}

// HandleEvent processes a GossipEvent and may produce a StreamMessage.
func (ca *ChannelAdapter) HandleEvent(event GossipEvent) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	// Skip thinking events entirely — too verbose for channel
	if event.Type == "thinking" {
		return
	}

	// Skip empty content
	if strings.TrimSpace(event.Content) == "" {
		return
	}

	// Build the display content based on event type
	var content string
	var role string

	displayName := ca.displayName(event.FromSlug)

	switch event.Type {
	case "text":
		role = "agent"
		content = fmt.Sprintf("%s: %s", displayName, ca.truncateContent(event.Content, 200))

	case "tool_use":
		role = "tool_use"
		// Content contains tool name (e.g. "Bash mkdir" or just "Bash")
		toolName := event.Content
		if idx := strings.Index(toolName, " "); idx > 0 {
			toolName = toolName[:idx]
		}
		content = fmt.Sprintf("  ⚡ %s → %s", displayName, toolName)

	case "tool_result":
		role = "tool_result"
		result := ca.truncateContent(event.Content, 80)
		content = fmt.Sprintf("  ↳ %s", result)

	default:
		// Unknown event type — skip
		return
	}

	// Deduplication: same agent, same content within 2 seconds
	dedupKey := event.FromSlug + ":" + event.Type
	if ca.lastContent[dedupKey] == content {
		if !event.Timestamp.IsZero() && event.Timestamp.Sub(ca.lastTime[dedupKey]) < 2*time.Second {
			return
		}
	}
	ca.lastContent[dedupKey] = content
	ca.lastTime[dedupKey] = event.Timestamp

	msg := StreamMessage{
		Role:      role,
		AgentSlug: event.FromSlug,
		AgentName: displayName,
		Content:   content,
		Timestamp: event.Timestamp,
	}

	// Non-blocking send
	select {
	case ca.messagesCh <- msg:
	default:
	}
}

// displayName returns the display name for an agent slug, falling back to
// uppercase first letter of slug if no name registered.
func (ca *ChannelAdapter) displayName(slug string) string {
	if name, ok := ca.agentNames[slug]; ok {
		return name
	}
	switch slug {
	case "fe":
		return "Frontend Engineer"
	case "be":
		return "Backend Engineer"
	case "ai":
		return "AI Engineer"
	}
	if len(slug) == 0 {
		return "Agent"
	}
	return strings.ToUpper(slug[:1]) + slug[1:]
}

// truncateContent truncates content to maxLen chars, appending "..." if truncated.
func (ca *ChannelAdapter) truncateContent(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
