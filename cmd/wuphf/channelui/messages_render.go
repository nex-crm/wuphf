package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderReactions renders a message's reactions as a row of muted
// pills, one per emoji, showing the count of reactors. Returns "" for
// empty input. Insertion order of distinct emojis is preserved so
// reactions don't reorder on every redraw.
func RenderReactions(reactions []BrokerReaction) string {
	if len(reactions) == 0 {
		return ""
	}
	groups := make(map[string][]string)
	order := make([]string, 0)
	for _, r := range reactions {
		if _, exists := groups[r.Emoji]; !exists {
			order = append(order, r.Emoji)
		}
		groups[r.Emoji] = append(groups[r.Emoji], r.From)
	}
	pillStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#2C2D31")).
		Foreground(lipgloss.Color("#D1D2D3")).
		Padding(0, 1)
	var parts []string
	for _, emoji := range order {
		agents := groups[emoji]
		label := emoji + " " + fmt.Sprintf("%d", len(agents))
		parts = append(parts, pillStyle.Render(label))
	}
	return strings.Join(parts, " ")
}

// MessageUsageTotal returns the total tokens spent on a message,
// preferring TotalTokens when set and falling back to the sum of the
// individual buckets (input + output + cache reads + cache creation).
// Returns 0 for nil usage.
func MessageUsageTotal(usage *BrokerMessageUsage) int {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
}

// RenderMessageUsageMeta returns a bold accent-colored token-count
// label suitable for inclusion in a message's meta strip. Returns ""
// when the usage total is zero.
func RenderMessageUsageMeta(usage *BrokerMessageUsage, accent string) string {
	total := MessageUsageTotal(usage)
	if total == 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Bold(true).
		Render(FormatTokenCount(total))
}

// DefaultHumanMessageTitle returns a fallback title used when a
// human_* message arrives without a Title set. The phrasing depends
// on the kind: human_decision → "<from> needs your call",
// human_action → "<from> wants you to do something", anything else →
// "<from> has an update for you".
func DefaultHumanMessageTitle(kind, from string) string {
	switch strings.TrimSpace(kind) {
	case "human_decision":
		return fmt.Sprintf("%s needs your call", DisplayName(from))
	case "human_action":
		return fmt.Sprintf("%s wants you to do something", DisplayName(from))
	default:
		return fmt.Sprintf("%s has an update for you", DisplayName(from))
	}
}

// SliceRenderedLines returns a windowed view of lines for a viewport
// of height msgH at scroll offset scroll. The returned start and end
// indices satisfy start <= end <= len(lines), and the returned scroll
// is clamped to the valid range. The scroll value treats 0 as
// "pinned to bottom"; positive values scroll back through history.
func SliceRenderedLines(lines []RenderedLine, msgH, scroll int) ([]RenderedLine, int, int, int) {
	total := len(lines)
	scroll = ClampScroll(total, msgH, scroll)
	end := total - scroll
	if end > total {
		end = total
	}
	if end < 1 && total > 0 {
		end = 1
	}
	start := end - msgH
	if start < 0 {
		start = 0
	}
	if total == 0 {
		return nil, scroll, 0, 0
	}
	return lines[start:end], scroll, start, end
}

// FormatTokenCount renders a token count as a compact label —
// "1.2M tok" / "5.0k tok" / "42 tok" — suitable for status lines
// where space is tight.
func FormatTokenCount(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM tok", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk tok", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d tok", tokens)
	}
}
