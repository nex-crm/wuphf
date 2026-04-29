package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FlattenThreadReplies returns the descendants of parentID in
// depth-first order, each tagged with its depth and the @parent label
// to render in the meta strip. The root parent itself is not
// included in the result.
func FlattenThreadReplies(allMessages []BrokerMessage, parentID string) []ThreadedMessage {
	byID := make(map[string]BrokerMessage, len(allMessages))
	children := BuildReplyChildren(allMessages)
	for _, msg := range allMessages {
		byID[msg.ID] = msg
	}

	var out []ThreadedMessage
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		for _, child := range children[id] {
			parentLabel := parentID
			if parent, ok := byID[child.ReplyTo]; ok {
				parentLabel = "@" + parent.From
			}
			out = append(out, ThreadedMessage{
				Message:     child,
				Depth:       depth,
				ParentLabel: parentLabel,
			})
			walk(child.ID, depth+1)
		}
	}

	walk(parentID, 0)
	return out
}

// RenderThreadReplies renders each reply via RenderThreadReply,
// concatenating the per-reply line slices. Returns nil for empty
// input.
func RenderThreadReplies(replies []ThreadedMessage, width int) []string {
	if len(replies) == 0 {
		return nil
	}

	var lines []string
	for _, reply := range replies {
		lines = append(lines, RenderThreadReply(reply, width)...)
	}
	return lines
}

// RenderThreadReply renders a single threaded reply: a header line
// (avatar + colored name + timestamp + meta), then the body wrapped
// to width-4 with a depth-indented "│" or "┆" guide. Mentions in the
// body are highlighted via HighlightMentions(AgentColorMap).
func RenderThreadReply(reply ThreadedMessage, width int) []string {
	msg := reply.Message
	color := AgentColorMap[msg.From]
	if color == "" {
		color = "#9CA3AF"
	}
	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
	tsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackTimestamp))
	metaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackMuted))

	name := DisplayName(msg.From)
	ts := FormatShortTime(msg.Timestamp)

	prefix := "  " + strings.Repeat("  ", reply.Depth)
	if reply.Depth > 0 {
		prefix += "↳ "
	}

	meta := RoleLabel(msg.From)
	if usageMeta := RenderMessageUsageMeta(msg.Usage, color); usageMeta != "" {
		meta += " · " + usageMeta
	}
	if reply.ParentLabel != "" {
		meta += " · reply to " + reply.ParentLabel
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("%s%s %s  %s  %s",
		prefix,
		AgentAvatar(msg.From),
		nameStyle.Render(name),
		tsStyle.Render(ts),
		metaStyle.Render(meta),
	))

	bodyPrefix := "  " + strings.Repeat("  ", reply.Depth)
	if reply.Depth > 0 {
		bodyPrefix += lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("┆") + " "
	} else {
		bodyPrefix += lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("│") + " "
	}

	for _, paragraph := range strings.Split(msg.Content, "\n") {
		paragraph = HighlightMentions(paragraph, AgentColorMap)
		for _, wrappedLine := range strings.Split(ansi.Wrap(paragraph, width-4, ""), "\n") {
			lines = append(lines, bodyPrefix+wrappedLine)
		}
	}
	lines = append(lines, "")
	return lines
}

// RenderThreadMessage renders a single message in compact thread
// style — avatar + name on the left, timestamp right-aligned (with a
// minimum 2-space gap), an optional usage strip, and the body wrapped
// to width-4. isParent is currently unused but retained for parity
// with the package-main signature in case parent-vs-reply styling
// diverges later.
func RenderThreadMessage(msg BrokerMessage, width int, isParent bool) []string {
	_ = isParent
	color := AgentColorMap[msg.From]
	if color == "" {
		color = "#9CA3AF"
	}
	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
	tsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackTimestamp))

	name := DisplayName(msg.From)
	ts := FormatShortTime(msg.Timestamp)

	nameRendered := nameStyle.Render(name)
	tsRendered := tsStyle.Render(ts)
	nameWidth := lipgloss.Width(nameRendered)
	tsWidth := lipgloss.Width(tsRendered)
	gap := width - nameWidth - tsWidth - 4
	if gap < 2 {
		gap = 2
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("  %s %s%s%s",
		AgentAvatar(msg.From), nameRendered, strings.Repeat(" ", gap), tsRendered))
	if usageMeta := RenderMessageUsageMeta(msg.Usage, color); usageMeta != "" {
		lines = append(lines, "  "+usageMeta)
	}

	for _, paragraph := range strings.Split(msg.Content, "\n") {
		paragraph = HighlightMentions(paragraph, AgentColorMap)
		wrapped := ansi.Wrap(paragraph, width-4, "")
		for _, wl := range strings.Split(wrapped, "\n") {
			lines = append(lines, "  "+wl)
		}
	}
	return lines
}
