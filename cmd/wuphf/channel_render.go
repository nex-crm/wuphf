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

func buildCalendarLines(actions []channelAction, jobs []channelSchedulerJob, tasks []channelTask, requests []channelInterview, activeChannel string, members []channelMember, viewRange calendarRange, filterSlug string, contentWidth int) []renderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	var lines []renderedLine
	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Calendar")})
	lines = append(lines, renderedLine{Text: buildCalendarToolbar(viewRange, filterSlug)})
	events := filterCalendarEvents(collectCalendarEvents(jobs, tasks, requests, activeChannel, members), viewRange, filterSlug)
	byParticipant := nextCalendarEventByParticipant(events)
	if len(byParticipant) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Teammate calendars")})
		for _, name := range orderedCalendarParticipants(byParticipant, members) {
			event := byParticipant[name]
			lines = append(lines, renderCalendarParticipantCard(name, event, contentWidth, agentSlugForDisplay(name, members))...)
		}
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Agenda")})
	}
	if len(events) == 0 {
		lines = append(lines, renderedLine{Text: "  " + muted.Render("No scheduled work yet.")})
		lines = append(lines, renderedLine{Text: "  " + muted.Render("Follow-ups, reminders, and recurring jobs will land here.")})
	} else {
		currentBucket := ""
		for _, event := range events {
			bucket := calendarBucketLabel(event.When)
			if bucket != currentBucket {
				lines = append(lines, renderedLine{Text: ""})
				lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render(bucket)})
				currentBucket = bucket
			}
			lines = append(lines, renderedLine{Text: ""})
			lines = append(lines, renderCalendarEventCard(event, contentWidth)...)
		}
	}
	recentActionCap := len(actions)
	if recentActionCap > 4 {
		recentActionCap = 4
	}
	recentActions := make([]channelAction, 0, recentActionCap)
	var pinnedBridge *channelAction
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		channel := normalizeSidebarSlug(action.Channel)
		if strings.TrimSpace(action.Kind) != "bridge_channel" && channel != "" && channel != normalizeSidebarSlug(activeChannel) {
			continue
		}
		if strings.TrimSpace(action.Kind) == "bridge_channel" && pinnedBridge == nil {
			actionCopy := action
			pinnedBridge = &actionCopy
		}
		if len(recentActions) < recentActionCap {
			recentActions = append(recentActions, action)
		}
	}
	if pinnedBridge != nil {
		hasBridge := false
		for _, action := range recentActions {
			if strings.TrimSpace(action.Kind) == "bridge_channel" {
				hasBridge = true
				break
			}
		}
		if !hasBridge {
			recentActions = append([]channelAction{*pinnedBridge}, recentActions...)
			if recentActionCap > 0 && len(recentActions) > recentActionCap {
				recentActions = recentActions[:recentActionCap]
			}
		}
	}
	if len(recentActions) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Recent actions")})
		for _, action := range recentActions {
			metaParts := []string{}
			if action.Actor != "" {
				metaParts = append(metaParts, "@"+action.Actor)
			}
			if action.Kind != "" {
				metaParts = append(metaParts, action.Kind)
			}
			if action.CreatedAt != "" {
				metaParts = append(metaParts, prettyRelativeTime(action.CreatedAt))
			}
			lines = append(lines, renderedLine{Text: ""})
			lines = append(lines, renderCalendarActionCard(action, strings.Join(metaParts, " · "), contentWidth)...)
		}
	}
	return lines
}

func renderCalendarEventCard(event calendarEvent, contentWidth int) []renderedLine {
	cardWidth := maxInt(24, contentWidth-4)
	accent, bg := calendarEventColors(event.Kind)
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		accentPill(strings.ToUpper(event.Kind), accent),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(event.Title),
	)
	if event.Channel != "" {
		header = lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", subtlePill("#"+event.Channel, "#CBD5E1", "#1E293B"))
	}
	timeLine := lipgloss.JoinHorizontal(lipgloss.Left,
		subtlePill(event.WhenLabel, "#F8FAFC", accent),
		"  ",
		mutedText(event.StatusOrFallback()),
	)
	if event.IntervalLabel != "" {
		timeLine = lipgloss.JoinHorizontal(lipgloss.Left, timeLine, "  ", subtlePill(event.IntervalLabel, "#D6E4FF", "#1E3A8A"))
	}
	participants := ""
	if len(event.Participants) > 0 {
		participants = "With " + strings.Join(event.Participants, ", ")
	}
	secondary := strings.TrimSpace(event.Secondary)
	if event.Provider != "" || event.ScheduleExpr != "" {
		extraParts := []string{}
		if event.Provider != "" {
			extraParts = append(extraParts, event.Provider)
		}
		if event.ScheduleExpr != "" {
			extraParts = append(extraParts, event.ScheduleExpr)
		}
		if secondary != "" {
			secondary = secondary + " · " + strings.Join(extraParts, " · ")
		} else {
			secondary = strings.Join(extraParts, " · ")
		}
	}
	cta := mutedText("Open event")
	if event.ThreadID != "" {
		cta = mutedText("Open thread")
	} else if event.TaskID != "" {
		cta = mutedText("Open task")
	} else if event.RequestID != "" {
		cta = mutedText("Open request")
	}
	bodyParts := []string{header, timeLine}
	if participants != "" {
		bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Render(participants))
	}
	if secondary != "" {
		bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).Render(secondary))
	}
	bodyParts = append(bodyParts, cta)
	card := lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Background(lipgloss.Color(bg)).
		Padding(0, 1).
		MarginLeft(2).
		Render(strings.Join(bodyParts, "\n"))
	return renderedCardLines(card, event.TaskID, event.RequestID, event.ThreadID, "")
}

func renderCalendarParticipantCard(name string, event calendarEvent, contentWidth int, agentSlug string) []renderedLine {
	cardWidth := maxInt(20, contentWidth-10)
	accent := "#334155"
	if color := agentColorMap[agentSlug]; color != "" {
		accent = color
	}
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		subtlePill(agentAvatar(agentSlug)+" "+name, "#F8FAFC", accent),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(event.Title),
	)
	body := []string{
		header,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Render(event.WhenLabel + " · " + strings.ToLower(event.Kind) + " · #" + event.Channel),
		mutedText("Open next item"),
	}
	card := lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Background(lipgloss.Color("#17161C")).
		Padding(0, 1).
		MarginLeft(2).
		Render(strings.Join(body, "\n"))
	return renderedCardLines(card, event.TaskID, event.RequestID, event.ThreadID, agentSlug)
}

func renderCalendarActionCard(action channelAction, meta string, contentWidth int) []renderedLine {
	cardWidth := maxInt(24, contentWidth-6)
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		subtlePill(action.Kind, "#E5E7EB", "#334155"),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(action.Summary),
	)
	card := lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Background(lipgloss.Color("#18171D")).
		Padding(0, 1).
		MarginLeft(2).
		Render(strings.Join([]string{header, mutedText(meta)}, "\n"))
	return renderedCardLines(card, "", "", "", "")
}

func renderedCardLines(card, taskID, requestID, threadID, agentSlug string) []renderedLine {
	return renderedCardLinesWithPrompt(card, taskID, requestID, threadID, agentSlug, "")
}

func renderedCardLinesWithPrompt(card, taskID, requestID, threadID, agentSlug, promptValue string) []renderedLine {
	var lines []renderedLine
	for _, line := range strings.Split(card, "\n") {
		lines = append(lines, renderedLine{
			Text:        line,
			TaskID:      taskID,
			RequestID:   requestID,
			ThreadID:    threadID,
			AgentSlug:   agentSlug,
			PromptValue: promptValue,
		})
	}
	return lines
}

func buildCalendarToolbar(viewRange calendarRange, filterSlug string) string {
	day := subtlePill("Day", "#CBD5E1", "#1E293B")
	week := subtlePill("Week", "#CBD5E1", "#1E293B")
	if viewRange == calendarRangeDay {
		day = accentPill("Day", "#1264A3")
	} else {
		week = accentPill("Week", "#1264A3")
	}
	filterLabel := "All teammates"
	if strings.TrimSpace(filterSlug) != "" {
		filterLabel = displayName(filterSlug)
	}
	return "  " + mutedText("d") + " " + day + "   " + mutedText("w") + " " + week + "   " + mutedText("f") + " " + subtlePill(filterLabel, "#E2E8F0", "#334155") + "   " + mutedText("a reset")
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
