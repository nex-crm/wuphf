package render

import (
	"strings"
	"testing"
)

func TestRenderTimelineBasic(t *testing.T) {
	events := []TimelineEvent{
		{Type: "created", Timestamp: "2026-03-23 10:00", Actor: "alice", Content: "Created the record"},
		{Type: "updated", Timestamp: "2026-03-23 11:00", Actor: "bob", Content: "Updated priority field"},
		{Type: "note", Timestamp: "2026-03-23 12:00", Actor: "carol", Content: "Added a follow-up note"},
	}
	result := RenderTimeline(events)

	if !strings.Contains(result, "●") {
		t.Error("expected created icon ● in output")
	}
	if !strings.Contains(result, "◆") {
		t.Error("expected updated icon ◆ in output")
	}
	if !strings.Contains(result, "✎") {
		t.Error("expected note icon ✎ in output")
	}

	// Verify connectors between events (but not after last)
	lines := strings.Split(result, "\n")
	connectorCount := 0
	for _, line := range lines {
		if strings.Contains(line, "│") {
			connectorCount++
		}
	}
	if connectorCount != 2 {
		t.Errorf("expected 2 connectors between 3 events, got %d", connectorCount)
	}
}

func TestRenderTimelineEmpty(t *testing.T) {
	result := RenderTimeline(nil)
	if !strings.Contains(result, "(no timeline events)") {
		t.Errorf("expected empty message, got: %s", result)
	}

	result2 := RenderTimeline([]TimelineEvent{})
	if !strings.Contains(result2, "(no timeline events)") {
		t.Errorf("expected empty message for empty slice, got: %s", result2)
	}
}

func TestRenderTimelineTruncation(t *testing.T) {
	longContent := strings.Repeat("x", 150)
	events := []TimelineEvent{
		{Type: "note", Timestamp: "2026-03-23 10:00", Actor: "alice", Content: longContent},
	}
	result := RenderTimeline(events)

	if strings.Contains(result, longContent) {
		t.Error("expected content to be truncated, but found full content")
	}
	if !strings.Contains(result, "...") {
		t.Error("expected truncation indicator '...' in output")
	}

	// Verify the content portion is 100 chars (97 + "...")
	// The truncated content should be 97 x's + "..."
	expected := strings.Repeat("x", 97) + "..."
	if !strings.Contains(result, expected) {
		t.Error("expected truncated content to be 97 chars + '...'")
	}
}

func TestRenderTimelineDefaultIcon(t *testing.T) {
	events := []TimelineEvent{
		{Type: "unknown_type", Timestamp: "2026-03-23 10:00", Actor: "alice", Content: "Something happened"},
	}
	result := RenderTimeline(events)
	if !strings.Contains(result, "·") {
		t.Error("expected default icon · for unknown type")
	}
}
