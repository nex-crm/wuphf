package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// AppendWrapped appends text to lines, wrapping it to fit width using
// ANSI-aware wrapping (so escape sequences don't get split mid-code).
// width <= 0 disables wrapping (the input is split on existing newlines
// only).
func AppendWrapped(lines []string, width int, text string) []string {
	if width <= 0 {
		return append(lines, strings.Split(text, "\n")...)
	}
	wrapped := ansi.Wrap(text, width, "")
	return append(lines, strings.Split(wrapped, "\n")...)
}

// TruncateText shortens s to at most max runes (treated as bytes —
// adequate for the ASCII-only labels this helper handles) and appends an
// ellipsis "…" when truncation occurs.
func TruncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// MutedText renders label in the muted-foreground style. Used for
// secondary labels, captions, and timestamps.
func MutedText(label string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted)).Render(label)
}

// RenderDateSeparator draws a centered "─── label ───" row, sized to fit
// width, in the date-separator color.
func RenderDateSeparator(width int, label string) string {
	lineWidth := width - len(label) - 8
	if lineWidth < 4 {
		lineWidth = 4
	}
	segment := strings.Repeat("─", lineWidth/2)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748B")).
		Render(fmt.Sprintf("%s  %s  %s", segment, label, segment))
}

// HumanMessageLabel returns the noun used in human-facing message
// summaries: "decision" for human_decision, "action" for human_action,
// "report" otherwise. Acts on a kind string so it doesn't pull
// BrokerMessage in.
func HumanMessageLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "human_decision":
		return "decision"
	case "human_action":
		return "action"
	default:
		return "report"
	}
}

// RenderUnreadDivider draws a "── N new since you looked ──" separator
// at contentWidth. unreadCount of 0 falls back to the generic "New
// since you looked" wording.
func RenderUnreadDivider(contentWidth int, unreadCount int) string {
	label := " New since you looked "
	if unreadCount > 0 {
		label = fmt.Sprintf(" %d new since you looked ", unreadCount)
	}
	lineLen := contentWidth - len(label) - 2
	if lineLen < 4 {
		lineLen = 4
	}
	left := strings.Repeat("─", lineLen/2)
	right := strings.Repeat("─", lineLen-lineLen/2)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#93C5FD")).
		Render("  " + left + label + right)
}

// DisplayDecisionSummary normalizes the verbose "Human directed the
// office:" prefix down to "Human directive:" so the policy-ledger lines
// stay tight without losing the directive marker.
func DisplayDecisionSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if strings.HasPrefix(summary, "Human directed the office:") {
		return strings.Replace(summary, "Human directed the office:", "Human directive:", 1)
	}
	return summary
}
