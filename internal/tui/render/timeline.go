package render

import (
	"fmt"
	"strings"
)

// TimelineEvent represents a single event in a timeline view.
type TimelineEvent struct {
	Type      string // "created", "updated", "deleted", "note", "task", "relationship"
	Timestamp string
	Actor     string
	Content   string
}

var typeIcons = map[string]string{
	"created":      "●",
	"updated":      "◆",
	"deleted":      "✕",
	"note":         "✎",
	"task":         "☐",
	"relationship": "⇄",
}

// RenderTimeline renders a vertical timeline of events with icons and connectors.
func RenderTimeline(events []TimelineEvent) string {
	if len(events) == 0 {
		return MutedStyle.Render("(no timeline events)")
	}

	var b strings.Builder
	for i, ev := range events {
		icon, ok := typeIcons[ev.Type]
		if !ok {
			icon = "·"
		}

		content := ev.Content
		if len(content) > 100 {
			content = content[:97] + "..."
		}

		meta := MutedStyle.Render(fmt.Sprintf("%s  %s", ev.Timestamp, ev.Actor))
		line := fmt.Sprintf("  %s  %s — %s", icon, meta, content)
		b.WriteString(line)
		b.WriteString("\n")

		if i < len(events)-1 {
			b.WriteString(MutedStyle.Render("  │"))
			b.WriteString("\n")
		}
	}
	return b.String()
}
