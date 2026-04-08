package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m channelModel) currentMainViewportLines(contentWidth, msgH int) []renderedLine {
	workspace := m.currentWorkspaceUIState()
	needsYou := workspace.needsYouLines(contentWidth)
	bodyHeight := msgH
	if len(needsYou) > 0 && bodyHeight-len(needsYou) >= 8 {
		bodyHeight -= len(needsYou)
	} else {
		needsYou = nil
	}

	if m.isOneOnOne() {
		if m.activeApp == officeAppRecovery {
			return m.currentMainLines(contentWidth)
		}
		if m.activeApp == officeAppInbox || m.activeApp == officeAppOutbox {
			return append(needsYou, m.currentMainLines(contentWidth)...)
		}
		if len(m.messages) == 0 {
			return append(needsYou, m.buildDirectFeedLines(contentWidth)...)
		}
		lines := buildOneOnOneViewportSuffix(m.messages, m.actions, m.tasks, m.members, m.expandedThreads, contentWidth, bodyHeight, m.scroll, m.oneOnOneAgentName(), m.oneOnOneAgentSlug(), m.unreadAnchorID, m.unreadCount)
		return append(needsYou, lines...)
	}
	if m.activeApp == officeAppMessages {
		if len(m.messages) == 0 {
			return append(needsYou, m.buildOfficeFeedLines(contentWidth)...)
		}
		lines := buildOfficeViewportSuffix(m.messages, m.expandedThreads, contentWidth, bodyHeight, m.scroll, m.threadsDefaultExpand, m.unreadAnchorID, m.unreadCount, m.members, m.tasks, m.actions)
		return append(needsYou, lines...)
	}
	return m.currentMainLines(contentWidth)
}

func buildOfficeViewportSuffix(messages []brokerMessage, expanded map[string]bool, contentWidth, msgH, scroll int, threadsDefaultExpand bool, unreadAnchorID string, unreadCount int, members []channelMember, tasks []channelTask, actions []channelAction) []renderedLine {
	tail := buildLiveWorkLines(members, tasks, actions, contentWidth, "")
	return buildVirtualizedOfficeViewport(messages, expanded, contentWidth, msgH, scroll, threadsDefaultExpand, unreadAnchorID, unreadCount, tail)
}

func buildOneOnOneViewportSuffix(messages []brokerMessage, actions []channelAction, tasks []channelTask, members []channelMember, expanded map[string]bool, contentWidth, msgH, scroll int, agentName, agentSlug, unreadAnchorID string, unreadCount int) []renderedLine {
	var tail []renderedLine
	tail = append(tail, buildDirectExecutionLines(actions, agentSlug, contentWidth)...)
	tail = append(tail, buildLiveWorkLines(members, tasks, nil, contentWidth, agentSlug)...)
	if len(messages) == 0 {
		limit := msgH + scroll
		if limit < 1 {
			limit = 1
		}
		lines := append(buildOneOnOneMessageLines(messages, expanded, contentWidth, agentName, unreadAnchorID, unreadCount), tail...)
		if len(lines) > limit {
			return cloneRenderedLines(lines[len(lines)-limit:])
		}
		return lines
	}
	return buildVirtualizedOfficeViewport(messages, expanded, contentWidth, msgH, scroll, true, unreadAnchorID, unreadCount, tail)
}

func officeThreadedMessages(messages []brokerMessage, expanded map[string]bool, threadsDefaultExpand bool) []threadedMessage {
	return cachedThreadedMessages(messages, expanded, threadsDefaultExpand)
}

func renderOfficeMessageBlock(tm threadedMessage, contentWidth int, unreadAnchorID string, unreadCount int) []renderedLine {
	msg := tm.Message
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Italic(true)

	var lines []renderedLine
	if unreadAnchorID != "" && msg.ID == unreadAnchorID {
		lines = append(lines, renderedLine{Text: renderUnreadDivider(contentWidth, unreadCount)})
	}
	ts := msg.Timestamp
	if len(ts) > 19 {
		ts = ts[11:19]
	}

	color := agentColorMap[msg.From]
	if color == "" {
		color = "#9CA3AF"
	}
	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
	ruleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

	appendWrappedLine := func(text string) {
		wrapped := appendWrapped(nil, contentWidth, text)
		for _, line := range wrapped {
			lines = append(lines, renderedLine{Text: line})
		}
	}

	if strings.HasPrefix(msg.Kind, "human_") {
		lines = append(lines, renderedLine{Text: ""})
		headerPrefix := "  " + strings.Repeat("  ", tm.Depth)
		if tm.Depth > 0 {
			headerPrefix += "↳ "
		}
		meta := fmt.Sprintf("for you · %s · %s", humanMessageLabel(msg.Kind), msg.ID)
		if tm.Depth > 0 {
			meta += fmt.Sprintf(" · thread reply to %s", tm.ParentLabel)
		}
		appendWrappedLine(fmt.Sprintf("%s%s %s  %s  %s",
			headerPrefix,
			agentAvatar(msg.From),
			nameStyle.Render(displayName(msg.From)),
			mutedStyle.Render(ts),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render(meta),
		))

		prefix := "  " + strings.Repeat("  ", tm.Depth)
		if tm.Depth > 0 {
			prefix += lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render("┆") + " "
		} else {
			prefix += lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render("│") + " "
		}
		titleLine := msg.Title
		if titleLine == "" {
			titleLine = defaultHumanMessageTitle(msg.Kind, msg.From)
		}
		appendWrappedLine(prefix + subtlePill("for you", "#FEF3C7", "#92400E") + " " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(titleLine))
		textPart, a2uiRendered := renderA2UIBlocks(msg.Content, contentWidth-4)
		for _, paragraph := range strings.Split(textPart, "\n") {
			paragraph = highlightMentions(paragraph, agentColorMap)
			appendWrappedLine(prefix + paragraph)
		}
		if a2uiRendered != "" {
			for _, lineText := range strings.Split(a2uiRendered, "\n") {
				lines = append(lines, renderedLine{Text: prefix + lineText})
			}
		}
		return lines
	}

	if msg.Kind == "automation" || msg.From == "nex" {
		lines = append(lines, renderedLine{Text: ""})
		source := msg.Source
		if source == "" {
			source = "context graph"
		} else {
			source = strings.ReplaceAll(source, "_", " ")
		}
		meta := fmt.Sprintf("%s · automated · %s", source, msg.ID)
		if tm.Depth > 0 {
			meta += fmt.Sprintf(" · thread reply to %s", tm.ParentLabel)
		}
		textPart, a2uiRendered := renderA2UIBlocks(msg.Content, contentWidth-4)
		titleLine := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(fallbackString(msg.Title, "Automation update"))
		for _, lineText := range renderRuntimeEventCard(contentWidth, subtlePill("automation", "#F8FAFC", "#334155")+" "+titleLine, meta, "#7C3AED", strings.Split(textPart, "\n")) {
			lines = append(lines, renderedLine{Text: "  " + lineText})
		}
		if a2uiRendered != "" {
			for _, lineText := range strings.Split(a2uiRendered, "\n") {
				lines = append(lines, renderedLine{Text: "    " + lineText})
			}
		}
		return lines
	}

	if strings.HasPrefix(msg.Content, "[STATUS]") {
		status := strings.TrimPrefix(msg.Content, "[STATUS] ")
		titleLine := subtlePill("status", "#E2E8F0", "#334155") + " " + nameStyle.Render("@"+msg.From) + " " + statusStyle.Render("is "+status)
		for _, lineText := range renderRuntimeEventCard(contentWidth, titleLine, mutedStyle.Render(ts), "#475569", nil) {
			lines = append(lines, renderedLine{Text: "  " + lineText})
		}
		return lines
	}

	if msg.From == "system" && (msg.Kind == "routing" || msg.Kind == "stage") {
		label := "routing"
		if msg.Kind == "stage" {
			label = "stage"
		}
		for _, lineText := range renderRuntimeEventCard(contentWidth, subtlePill(label, "#E5E7EB", "#334155")+" "+mutedStyle.Render(ts), msg.Content, "#475569", nil) {
			lines = append(lines, renderedLine{Text: "  " + lineText})
		}
		return lines
	}

	mood := inferMood(msg.Content)
	meta := roleLabel(msg.From) + " · " + msg.ID
	if mood != "" {
		meta += " · " + mood
	}
	if tm.Depth > 0 {
		meta += fmt.Sprintf(" · thread reply to %s", tm.ParentLabel)
	}
	metaStyle := mutedStyle
	if mood != "" {
		metaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	}
	lines = append(lines, renderedLine{Text: ""})
	headerPrefix := "  " + strings.Repeat("  ", tm.Depth)
	if tm.Depth > 0 {
		headerPrefix += "↳ "
	}
	appendWrappedLine(fmt.Sprintf("%s%s %s  %s  %s", headerPrefix, agentAvatar(msg.From), nameStyle.Render(displayName(msg.From)), mutedStyle.Render(ts), metaStyle.Render(meta)))

	prefix := "  " + strings.Repeat("  ", tm.Depth)
	if tm.Depth > 0 {
		prefix += ruleStyle.Render("┆") + " "
	} else {
		prefix += ruleStyle.Render("│") + " "
	}

	textPart, a2uiRendered := renderA2UIBlocks(msg.Content, contentWidth-4)
	rendered := renderMarkdown(textPart, contentWidth-len(prefix)-2)
	for _, paragraph := range strings.Split(rendered, "\n") {
		paragraph = highlightMentions(paragraph, agentColorMap)
		appendWrappedLine(prefix + paragraph)
	}
	if a2uiRendered != "" {
		for _, lineText := range strings.Split(a2uiRendered, "\n") {
			lines = append(lines, renderedLine{Text: prefix + lineText})
		}
	}
	if reactionLine := renderReactions(msg.Reactions); reactionLine != "" {
		appendWrappedLine(prefix + reactionLine)
	}
	if tm.Collapsed && tm.HiddenReplies > 0 {
		var coloredNames []string
		for _, p := range tm.ThreadParticipants {
			pColor := agentColorMap[strings.TrimPrefix(strings.ToLower(p), "@")]
			if pColor == "" {
				for slug, name := range map[string]string{
					"ceo": "CEO", "pm": "Product Manager", "fe": "Frontend Engineer", "be": "Backend Engineer",
					"ai": "AI Engineer", "designer": "Designer", "cmo": "CMO", "cro": "CRO",
				} {
					if p == name {
						pColor = agentColorMap[slug]
						break
					}
				}
			}
			if pColor == "" {
				pColor = "#ABABAD"
			}
			coloredNames = append(coloredNames, lipgloss.NewStyle().Foreground(lipgloss.Color(pColor)).Bold(true).Render(p))
		}
		participantStr := ""
		if len(coloredNames) > 0 {
			participantStr = "  " + strings.Join(coloredNames, ", ")
		}
		label := fmt.Sprintf("  ↩ %d %s%s", tm.HiddenReplies, pluralizeWord(tm.HiddenReplies, "reply", "replies"), participantStr)
		lines = append(lines, renderedLine{Text: label, ThreadID: msg.ID})
	}

	return lines
}

func prependRenderedLines(dst, prefix []renderedLine) []renderedLine {
	if len(prefix) == 0 {
		return dst
	}
	out := make([]renderedLine, 0, len(prefix)+len(dst))
	out = append(out, prefix...)
	out = append(out, dst...)
	return out
}
