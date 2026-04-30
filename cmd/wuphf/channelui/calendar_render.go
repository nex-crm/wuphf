package channelui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// NormalizeSidebarSlug lower-cases value, trims whitespace, and
// replaces spaces and underscores with hyphens. Used to canonicalize
// channel slugs so equality comparisons (e.g. "is this action for the
// active channel?") tolerate keyboard noise.
func NormalizeSidebarSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

// BuildCalendarLines renders the calendar app: a toolbar row, a
// "Teammate calendars" strip with the next event for each member, an
// agenda grouped into Earlier/Today/Tomorrow/Upcoming buckets, and a
// "Recent actions" list pinned to the active channel (with the latest
// bridge_channel action force-pinned so it always shows). filterSlug
// optionally narrows the agenda to a single participant.
func BuildCalendarLines(actions []Action, jobs []SchedulerJob, tasks []Task, requests []Interview, activeChannel string, members []Member, viewRange CalendarRange, filterSlug string, contentWidth int) []RenderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted))
	var lines []RenderedLine
	lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Calendar")})
	lines = append(lines, RenderedLine{Text: BuildCalendarToolbar(viewRange, filterSlug)})
	events := FilterCalendarEvents(CollectCalendarEvents(jobs, tasks, requests, activeChannel, members), viewRange, filterSlug)
	byParticipant := NextCalendarEventByParticipant(events)
	if len(byParticipant) > 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Teammate calendars")})
		for _, name := range OrderedCalendarParticipants(byParticipant, members) {
			event := byParticipant[name]
			lines = append(lines, RenderCalendarParticipantCard(name, event, contentWidth, AgentSlugForDisplay(name, members))...)
		}
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Agenda")})
	}
	if len(events) == 0 {
		lines = append(lines, RenderedLine{Text: "  " + muted.Render("No scheduled work yet.")})
		lines = append(lines, RenderedLine{Text: "  " + muted.Render("Follow-ups, reminders, and recurring jobs will land here.")})
	} else {
		currentBucket := ""
		for _, event := range events {
			bucket := CalendarBucketLabel(event.When)
			if bucket != currentBucket {
				lines = append(lines, RenderedLine{Text: ""})
				lines = append(lines, RenderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render(bucket)})
				currentBucket = bucket
			}
			lines = append(lines, RenderedLine{Text: ""})
			lines = append(lines, RenderCalendarEventCard(event, contentWidth)...)
		}
	}
	recentActionCap := len(actions)
	if recentActionCap > 4 {
		recentActionCap = 4
	}
	recentActions := make([]Action, 0, recentActionCap)
	var pinnedBridge *Action
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		channel := NormalizeSidebarSlug(action.Channel)
		if strings.TrimSpace(action.Kind) != "bridge_channel" && channel != "" && channel != NormalizeSidebarSlug(activeChannel) {
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
			recentActions = append([]Action{*pinnedBridge}, recentActions...)
			if recentActionCap > 0 && len(recentActions) > recentActionCap {
				recentActions = recentActions[:recentActionCap]
			}
		}
	}
	if len(recentActions) > 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Recent actions")})
		for _, action := range recentActions {
			metaParts := []string{}
			if action.Actor != "" {
				metaParts = append(metaParts, "@"+action.Actor)
			}
			if action.Kind != "" {
				metaParts = append(metaParts, action.Kind)
			}
			if action.CreatedAt != "" {
				metaParts = append(metaParts, PrettyRelativeTime(action.CreatedAt))
			}
			lines = append(lines, RenderedLine{Text: ""})
			lines = append(lines, RenderCalendarActionCard(action, strings.Join(metaParts, " · "), contentWidth)...)
		}
	}
	return lines
}

// BuildCalendarToolbar renders the keyboard-hint pill row at the top
// of the calendar app: "d Day", "w Week", "f <filter>", "a reset". The
// active range is rendered with an accent pill, the inactive one
// muted.
func BuildCalendarToolbar(viewRange CalendarRange, filterSlug string) string {
	day := SubtlePill("Day", "#CBD5E1", "#1E293B")
	week := SubtlePill("Week", "#CBD5E1", "#1E293B")
	if viewRange == CalendarRangeDay {
		day = AccentPill("Day", "#1264A3")
	} else {
		week = AccentPill("Week", "#1264A3")
	}
	filterLabel := "All teammates"
	if strings.TrimSpace(filterSlug) != "" {
		filterLabel = DisplayName(filterSlug)
	}
	return "  " + MutedText("d") + " " + day + "   " + MutedText("w") + " " + week + "   " + MutedText("f") + " " + SubtlePill(filterLabel, "#E2E8F0", "#334155") + "   " + MutedText("a reset")
}

// RenderCalendarEventCard renders a calendar event as a rounded-border
// card: a header (kind pill + title + optional channel pill), a
// time/status row, optional participants, optional secondary line
// (provider / schedule expression / etc.), and a contextual "Open ..."
// CTA based on which target IDs are set.
func RenderCalendarEventCard(event CalendarEvent, contentWidth int) []RenderedLine {
	cardWidth := MaxInt(24, contentWidth-4)
	accent, bg := CalendarEventColors(event.Kind)
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		AccentPill(strings.ToUpper(event.Kind), accent),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(event.Title),
	)
	if event.Channel != "" {
		header = lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", SubtlePill("#"+event.Channel, "#CBD5E1", "#1E293B"))
	}
	timeLine := lipgloss.JoinHorizontal(lipgloss.Left,
		SubtlePill(event.WhenLabel, "#F8FAFC", accent),
		"  ",
		MutedText(event.StatusOrFallback()),
	)
	if event.IntervalLabel != "" {
		timeLine = lipgloss.JoinHorizontal(lipgloss.Left, timeLine, "  ", SubtlePill(event.IntervalLabel, "#D6E4FF", "#1E3A8A"))
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
	cta := MutedText("Open event")
	if event.ThreadID != "" {
		cta = MutedText("Open thread")
	} else if event.TaskID != "" {
		cta = MutedText("Open task")
	} else if event.RequestID != "" {
		cta = MutedText("Open request")
	}
	bodyParts := []string{header, timeLine}
	if participants != "" {
		bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Render(participants))
	}
	if secondary != "" {
		bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted)).Render(secondary))
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
	return RenderedCardLines(card, event.TaskID, event.RequestID, event.ThreadID, "")
}

// RenderCalendarParticipantCard renders the smaller "next event for
// teammate <name>" card used in the Teammate calendars strip. Border
// and accent take the agent's color when known.
func RenderCalendarParticipantCard(name string, event CalendarEvent, contentWidth int, agentSlug string) []RenderedLine {
	cardWidth := MaxInt(20, contentWidth-10)
	accent := "#334155"
	if color := AgentColorMap[agentSlug]; color != "" {
		accent = color
	}
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		SubtlePill(AgentAvatar(agentSlug)+" "+name, "#F8FAFC", accent),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(event.Title),
	)
	body := []string{
		header,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Render(event.WhenLabel + " · " + strings.ToLower(event.Kind) + " · #" + event.Channel),
		MutedText("Open next item"),
	}
	card := lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Background(lipgloss.Color("#17161C")).
		Padding(0, 1).
		MarginLeft(2).
		Render(strings.Join(body, "\n"))
	return RenderedCardLines(card, event.TaskID, event.RequestID, event.ThreadID, agentSlug)
}

// RenderCalendarActionCard renders one row of the "Recent actions"
// strip below the agenda. Header is "[kind] summary"; body is the
// pre-joined meta string.
func RenderCalendarActionCard(action Action, meta string, contentWidth int) []RenderedLine {
	cardWidth := MaxInt(24, contentWidth-6)
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		SubtlePill(action.Kind, "#E5E7EB", "#334155"),
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
		Render(strings.Join([]string{header, MutedText(meta)}, "\n"))
	return RenderedCardLines(card, "", "", "", "")
}

// RenderedCardLines splits a pre-rendered card string into per-line
// RenderedLine entries, attaching click metadata (TaskID / RequestID
// / ThreadID / AgentSlug) so the channel feed can route clicks back
// to the right target. Callers wanting to attach a prefilled composer
// PromptValue should use RenderedCardLinesWithPrompt instead.
func RenderedCardLines(card, taskID, requestID, threadID, agentSlug string) []RenderedLine {
	return RenderedCardLinesWithPrompt(card, taskID, requestID, threadID, agentSlug, "")
}

// RenderedCardLinesWithPrompt is like RenderedCardLines but stamps a
// PromptValue onto every line — used by recovery-surgery cards that
// want a click to prefill the composer with a suggested prompt.
func RenderedCardLinesWithPrompt(card, taskID, requestID, threadID, agentSlug, promptValue string) []RenderedLine {
	var lines []RenderedLine
	for _, line := range strings.Split(card, "\n") {
		lines = append(lines, RenderedLine{
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
