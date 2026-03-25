package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// claudeStreamEvent mirrors the NDJSON envelope from claude --output-format stream-json.
// Kept minimal — only fields we need for gossip extraction.
type claudeStreamEvent struct {
	Type    string              `json:"type"`
	Subtype string             `json:"subtype,omitempty"`
	Message *claudeEventMessage `json:"message,omitempty"`
}

type claudeEventMessage struct {
	Content []claudeEventBlock `json:"content"`
}

type claudeEventBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Name     string `json:"name,omitempty"`
	Input    any    `json:"input,omitempty"`
	Content  any    `json:"content,omitempty"`
}

// OutputObserver reads NDJSON from a reader, parses Claude stream events,
// and emits GossipEvents to the bus.
type OutputObserver struct {
	slug   string
	bus    *GossipBus
	reader io.Reader
}

// NewOutputObserver creates an observer for the given agent slug.
func NewOutputObserver(slug string, bus *GossipBus, reader io.Reader) *OutputObserver {
	return &OutputObserver{
		slug:   slug,
		bus:    bus,
		reader: reader,
	}
}

// Start launches a goroutine that reads NDJSON lines and emits events.
func (o *OutputObserver) Start() {
	go o.run()
}

func (o *OutputObserver) run() {
	scanner := bufio.NewScanner(o.reader)
	// Allow large lines (Claude tool results can be big)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg claudeStreamEvent
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // not JSON, skip
		}

		events := o.extractEvents(msg)
		for _, ev := range events {
			o.bus.Emit(ev)
		}
	}
}

// extractEvents parses a claude stream message into zero or more GossipEvents.
func (o *OutputObserver) extractEvents(msg claudeStreamEvent) []GossipEvent {
	// Skip system/init/rate_limit messages
	switch msg.Type {
	case "system", "init", "rate_limit", "error":
		return nil
	}

	if msg.Message == nil {
		// assistant messages with subtype "result" that have no content blocks
		if msg.Type == "result" {
			return nil
		}
		return nil
	}

	var events []GossipEvent
	now := time.Now()
	if o.bus.now != nil {
		now = o.bus.now()
	}

	for _, block := range msg.Message.Content {
		var ev *GossipEvent

		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				ev = &GossipEvent{
					FromSlug:  o.slug,
					Type:      "thinking",
					Content:   block.Thinking,
					Timestamp: now,
				}
			}
		case "text":
			if block.Text != "" {
				ev = &GossipEvent{
					FromSlug:  o.slug,
					Type:      "text",
					Content:   block.Text,
					Timestamp: now,
				}
			}
		case "tool_use":
			content := block.Name
			if block.Input != nil {
				if inputStr, err := json.Marshal(block.Input); err == nil {
					content = fmt.Sprintf("%s %s", block.Name, string(inputStr))
				}
			}
			ev = &GossipEvent{
				FromSlug:  o.slug,
				Type:      "tool_use",
				Content:   content,
				Timestamp: now,
			}
		case "tool_result":
			content := ""
			switch v := block.Content.(type) {
			case string:
				content = v
			default:
				if raw, err := json.Marshal(v); err == nil {
					content = string(raw)
				}
			}
			ev = &GossipEvent{
				FromSlug:  o.slug,
				Type:      "tool_result",
				Content:   content,
				Timestamp: now,
			}
		}

		if ev != nil {
			events = append(events, *ev)
		}
	}

	return events
}
