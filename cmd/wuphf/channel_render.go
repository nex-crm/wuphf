package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func buildOfficeMessageLines(messages []channelui.BrokerMessage, expanded map[string]bool, contentWidth int, threadsDefaultExpand bool, unreadAnchorID string, unreadCount int) []channelui.RenderedLine {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))

	var lines []channelui.RenderedLine
	if len(messages) == 0 {
		lines = append(lines,
			channelui.RenderedLine{Text: ""},
			channelui.RenderedLine{Text: mutedStyle.Render("  Welcome to The WUPHF Office. The cast is assembled.")},
			channelui.RenderedLine{Text: mutedStyle.Render("  Drop a company-building thought in #general, or tag a teammate to get things moving.")},
			channelui.RenderedLine{Text: ""},
			channelui.RenderedLine{Text: mutedStyle.Render("  Suggested: Let's build an AI notetaking company. (Ryan Howard would've called it NoteWUPHF.)")},
			channelui.RenderedLine{Text: mutedStyle.Render("  The CEO triages first, then the right specialists pile in — unlike the original WUPHF.com, this ships.")},
		)
		return lines
	}

	lines = append(lines, channelui.RenderedLine{Text: channelui.RenderDateSeparator(contentWidth, "Today")})
	for _, tm := range officeThreadedMessages(messages, expanded, threadsDefaultExpand) {
		lines = append(lines, renderOfficeMessageBlock(tm, contentWidth, unreadAnchorID, unreadCount)...)
	}

	return lines
}

func buildOneOnOneMessageLines(messages []channelui.BrokerMessage, expanded map[string]bool, contentWidth int, agentName string, unreadAnchorID string, unreadCount int) []channelui.RenderedLine {
	if len(messages) == 0 {
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))
		return []channelui.RenderedLine{
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
