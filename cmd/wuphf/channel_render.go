package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func buildOfficeMessageLines(messages []brokerMessage, expanded map[string]bool, contentWidth int, threadsDefaultExpand bool, unreadAnchorID string, unreadCount int) []renderedLine {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))

	var lines []renderedLine
	if len(messages) == 0 {
		lines = append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: mutedStyle.Render("  Welcome to The WUPHF Office. The cast is assembled.")},
			renderedLine{Text: mutedStyle.Render("  Drop a company-building thought in #general, or tag a teammate to get things moving.")},
			renderedLine{Text: ""},
			renderedLine{Text: mutedStyle.Render("  Suggested: Let's build an AI notetaking company. (Ryan Howard would've called it NoteWUPHF.)")},
			renderedLine{Text: mutedStyle.Render("  The CEO triages first, then the right specialists pile in — unlike the original WUPHF.com, this ships.")},
		)
		return lines
	}

	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Today")})
	for _, tm := range officeThreadedMessages(messages, expanded, threadsDefaultExpand) {
		lines = append(lines, renderOfficeMessageBlock(tm, contentWidth, unreadAnchorID, unreadCount)...)
	}

	return lines
}

func renderReactions(reactions []brokerReaction) string {
	if len(reactions) == 0 {
		return ""
	}
	// Group by emoji: 👍 @ceo @pm
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

func messageUsageTotal(usage *brokerMessageUsage) int {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
}

func renderMessageUsageMeta(usage *brokerMessageUsage, accent string) string {
	total := messageUsageTotal(usage)
	if total == 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Bold(true).
		Render(formatTokenCount(total))
}

func buildOneOnOneMessageLines(messages []brokerMessage, expanded map[string]bool, contentWidth int, agentName string, unreadAnchorID string, unreadCount int) []renderedLine {
	if len(messages) == 0 {
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
		return []renderedLine{
			{Text: ""},
			{Text: mutedStyle.Render("  Conference room reserved. Direct session reset. Agent pane reloaded in place.")},
			{Text: mutedStyle.Render("  No colleagues, no sidebar, no Toby. Just you and " + agentName + ".")},
			{Text: ""},
			{Text: mutedStyle.Render("  Suggested: Help me think through the v1 launch plan.")},
			{Text: mutedStyle.Render("  Whatever you say here stays in this room. Like Vegas. Or Threat Level Midnight.")},
		}
	}
	return buildOfficeMessageLines(messages, expanded, contentWidth, true, unreadAnchorID, unreadCount)
}

func defaultHumanMessageTitle(kind, from string) string {
	switch strings.TrimSpace(kind) {
	case "human_decision":
		return fmt.Sprintf("%s needs your call", displayName(from))
	case "human_action":
		return fmt.Sprintf("%s wants you to do something", displayName(from))
	default:
		return fmt.Sprintf("%s has an update for you", displayName(from))
	}
}

func sliceRenderedLines(lines []renderedLine, msgH, scroll int) ([]renderedLine, int, int, int) {
	total := len(lines)
	scroll = clampScroll(total, msgH, scroll)
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

// reverseAny reverses items in place. Kept in package main (instead of
// hoisted alongside the typed Reverse* helpers in channelui) because Go
// does not allow taking the value of a generic function — so it cannot
// be aliased through channelui_aliases.go. channel_artifacts.go and
// channel_activity.go still call it directly. Removed in PR 9 once
// those callers move into channelui.
func reverseAny[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

// renderMarkdown renders markdown text for terminal display using glamour.
// Falls back to raw text if rendering fails.
func renderMarkdown(text string, width int) string {
	if width < 20 {
		width = 20
	}
	// Short messages without markdown syntax — skip rendering overhead
	if !strings.ContainsAny(text, "*_`#|-[]>") {
		return text
	}
	key := markdownCacheKey(width, text)
	if cached, ok := channelRenderCache.getMarkdown(key); ok {
		return cached
	}
	r, err := channelRenderCache.renderer(width)
	if err != nil {
		return text
	}
	rendered, err := r.Render(text)
	if err != nil {
		return text
	}
	// Trim trailing whitespace glamour adds
	result := strings.TrimRight(rendered, "\n ")
	// Remove glamour's auto-linked mailto: URLs that duplicate email addresses
	result = strings.ReplaceAll(result, "mailto:", "")
	channelRenderCache.putMarkdown(key, result)
	return result
}
