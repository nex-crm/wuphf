package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type renderedLine struct {
	Text      string
	ThreadID  string
	TaskID    string
	RequestID string
	AgentSlug string
}

func buildOfficeMessageLines(messages []brokerMessage, expanded map[string]bool, contentWidth int, threadsDefaultExpand bool) []renderedLine {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Italic(true)

	var lines []renderedLine
	if len(messages) == 0 {
		lines = append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: mutedStyle.Render("  Welcome to The WUPHF Office.")},
			renderedLine{Text: mutedStyle.Render("  This is #general. Drop a company-building thought here or tag a teammate.")},
			renderedLine{Text: ""},
			renderedLine{Text: mutedStyle.Render("  Suggested: Let's build an AI notetaking company.")},
			renderedLine{Text: mutedStyle.Render("  WUPHF will let the CEO triage first, then the right specialists pile in if they actually should.")},
		)
		return lines
	}

	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Today")})

	if threadsDefaultExpand {
		for _, msg := range messages {
			if msg.ReplyTo == "" && hasThreadReplies(messages, msg.ID) {
				if _, explicit := expanded[msg.ID]; !explicit {
					expanded[msg.ID] = true
				}
			}
		}
	}

	for _, tm := range flattenThreadMessages(messages, expanded) {
		msg := tm.Message
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

		if msg.Kind == "automation" || msg.From == "nex" {
			lines = append(lines, renderedLine{Text: ""})
			headerPrefix := "  " + strings.Repeat("  ", tm.Depth)
			if tm.Depth > 0 {
				headerPrefix += "↳ "
			}
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
			appendWrappedLine(fmt.Sprintf("%s%s %s  %s  %s", headerPrefix, agentAvatar(msg.From), nameStyle.Render(displayName(msg.From)), mutedStyle.Render(ts), mutedStyle.Render(meta)))

			prefix := "  " + strings.Repeat("  ", tm.Depth)
			if tm.Depth > 0 {
				prefix += ruleStyle.Render("┆") + " "
			} else {
				prefix += ruleStyle.Render("│") + " "
			}
			if msg.Title != "" {
				appendWrappedLine(prefix + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(msg.Title))
			}
			textPart, a2uiRendered := renderA2UIBlocks(msg.Content, contentWidth-4)
			for _, paragraph := range strings.Split(textPart, "\n") {
				appendWrappedLine(prefix + paragraph)
			}
			if a2uiRendered != "" {
				for _, lineText := range strings.Split(a2uiRendered, "\n") {
					lines = append(lines, renderedLine{Text: prefix + lineText})
				}
			}
			continue
		}

		if strings.HasPrefix(msg.Content, "[STATUS]") {
			status := strings.TrimPrefix(msg.Content, "[STATUS] ")
			statusPrefix := "  " + strings.Repeat("  ", tm.Depth)
			if tm.Depth > 0 {
				statusPrefix += "↳ "
			}
			appendWrappedLine(fmt.Sprintf("%s%s  %s %s", statusPrefix, mutedStyle.Render(ts), nameStyle.Render("@"+msg.From), statusStyle.Render("is "+status)))
			continue
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
		for _, paragraph := range strings.Split(textPart, "\n") {
			paragraph = highlightMentions(paragraph, agentColorMap)
			appendWrappedLine(prefix + paragraph)
		}
		if a2uiRendered != "" {
			for _, lineText := range strings.Split(a2uiRendered, "\n") {
				lines = append(lines, renderedLine{Text: prefix + lineText})
			}
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
			label := fmt.Sprintf("  ↩ %d repl%s%s", tm.HiddenReplies, pluralSuffix(tm.HiddenReplies), participantStr)
			lines = append(lines, renderedLine{Text: label, ThreadID: msg.ID})
		}
	}

	return lines
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

func buildRequestLines(requests []channelInterview, contentWidth int) []renderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	highlight := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Bold(true)
	if len(requests) == 0 {
		return []renderedLine{
			{Text: ""},
			{Text: muted.Render("  No open requests right now.")},
			{Text: muted.Render("  If an agent needs a real decision, it will show up here.")},
		}
	}
	var lines []renderedLine
	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Open requests")})
	for _, req := range requests {
		metaParts := []string{strings.ToUpper(req.Kind), req.ID, "@" + req.From}
		if req.Status != "" {
			metaParts = append(metaParts, strings.ReplaceAll(req.Status, "_", " "))
		}
		if req.Blocking {
			metaParts = append(metaParts, "blocking")
		}
		if req.Required {
			metaParts = append(metaParts, "required")
		}
		meta := strings.Join(metaParts, " · ")
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + requestKindPill(req.Kind) + " " + title.Render(req.Question), RequestID: req.ID})
		if req.Title != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render(req.Title+" · "+meta), RequestID: req.ID})
		} else {
			lines = append(lines, renderedLine{Text: "  " + muted.Render(meta), RequestID: req.ID})
		}
		for _, line := range appendWrapped(nil, maxInt(20, contentWidth-4), "  "+req.Context) {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, renderedLine{Text: line, RequestID: req.ID})
			}
		}
		if timing := renderTimingSummary(req.DueAt, req.FollowUpAt, req.ReminderAt, req.RecheckAt); timing != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render(timing), RequestID: req.ID})
		}
		if req.RecommendedID != "" {
			lines = append(lines, renderedLine{Text: "  " + highlight.Render("Recommended: "+req.RecommendedID), RequestID: req.ID})
		}
		lines = append(lines, renderedLine{Text: "  " + muted.Render("Click to focus, answer, or snooze. Esc hides it locally; the team still waits."), RequestID: req.ID})
	}
	return lines
}

func buildTaskLines(tasks []channelTask, contentWidth int) []renderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	if len(tasks) == 0 {
		return []renderedLine{
			{Text: ""},
			{Text: muted.Render("  No active work tracked yet.")},
			{Text: muted.Render("  When the team claims real work, it will appear here.")},
		}
	}
	statusColor := map[string]string{
		"open":        "#94A3B8",
		"in_progress": "#F59E0B",
		"done":        "#22C55E",
	}
	var lines []renderedLine
	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Tasks")})
	for _, task := range tasks {
		color := statusColor[task.Status]
		if color == "" {
			color = "#94A3B8"
		}
		status := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(strings.ReplaceAll(task.Status, "_", " "))
		metaParts := []string{task.ID, status}
		if task.Owner != "" {
			metaParts = append(metaParts, "owner "+displayName(task.Owner))
		}
		if task.Channel != "" {
			metaParts = append(metaParts, "#"+task.Channel)
		}
		meta := strings.Join(metaParts, " · ")
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + taskStatusPill(task.Status) + " " + lipgloss.NewStyle().Bold(true).Render(task.Title), TaskID: task.ID})
		lines = append(lines, renderedLine{Text: "  " + muted.Render(meta), TaskID: task.ID})
		if task.Details != "" {
			for _, line := range appendWrapped(nil, maxInt(20, contentWidth-4), "  "+task.Details) {
				lines = append(lines, renderedLine{Text: line, TaskID: task.ID})
			}
		}
		if timing := renderTimingSummary(task.DueAt, task.FollowUpAt, task.ReminderAt, task.RecheckAt); timing != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render(timing), TaskID: task.ID})
		}
		lines = append(lines, renderedLine{Text: "  " + muted.Render("Click to claim, complete, block, or release."), TaskID: task.ID})
	}
	return lines
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
	if len(actions) == 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + muted.Render("No recent office actions.")})
		return lines
	}
	lines = append(lines, renderedLine{Text: ""})
	lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Recent actions")})
	for _, action := range actions {
		metaParts := []string{action.Kind}
		if action.Actor != "" {
			metaParts = append(metaParts, "@"+action.Actor)
		}
		if action.Channel != "" {
			metaParts = append(metaParts, "#"+action.Channel)
		}
		if action.Source != "" {
			metaParts = append(metaParts, action.Source)
		}
		metaParts = append(metaParts, prettyWhen(action.CreatedAt, "at"))
		lines = append(lines, renderCalendarActionCard(action, strings.Join(metaParts, " · "), contentWidth)...)
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
	var lines []renderedLine
	for _, line := range strings.Split(card, "\n") {
		lines = append(lines, renderedLine{
			Text:      line,
			TaskID:    taskID,
			RequestID: requestID,
			ThreadID:  threadID,
			AgentSlug: agentSlug,
		})
	}
	return lines
}

func calendarEventColors(kind string) (string, string) {
	switch kind {
	case "task":
		return "#2563EB", "#131A27"
	case "request":
		return "#D97706", "#24170E"
	case "job":
		return "#7C3AED", "#1E1630"
	default:
		return "#334155", "#17161C"
	}
}

func (e calendarEvent) StatusOrFallback() string {
	if strings.TrimSpace(e.Status) != "" {
		return e.Status
	}
	switch e.Kind {
	case "task":
		return "scheduled task"
	case "request":
		return "pending request"
	case "job":
		return "scheduled job"
	default:
		return "scheduled"
	}
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

func mutedText(label string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).Render(label)
}

type calendarEvent struct {
	When             time.Time
	WhenLabel        string
	Kind             string
	Title            string
	Secondary        string
	Channel          string
	Status           string
	IntervalLabel    string
	Participants     []string
	ParticipantSlugs []string
	TaskID           string
	RequestID        string
	ThreadID         string
}

func collectCalendarEvents(jobs []channelSchedulerJob, tasks []channelTask, requests []channelInterview, activeChannel string, members []channelMember) []calendarEvent {
	var events []calendarEvent
	for _, job := range jobs {
		whenText := strings.TrimSpace(job.NextRun)
		if whenText == "" {
			whenText = strings.TrimSpace(job.LastRun)
		}
		when, ok := parseChannelTime(whenText)
		if !ok {
			continue
		}
		participants := calendarParticipantsForJob(job, tasks, requests, activeChannel, members)
		interval := ""
		if job.IntervalMinutes > 0 {
			interval = fmt.Sprintf("every %s min", formatMinutes(job.IntervalMinutes))
		}
		events = append(events, calendarEvent{
			When:             when,
			WhenLabel:        prettyCalendarWhen(when),
			Kind:             "job",
			Title:            job.Label,
			Secondary:        strings.TrimSpace(job.Status),
			Channel:          chooseCalendarChannel(job.Channel, activeChannel),
			Status:           strings.TrimSpace(job.Status),
			IntervalLabel:    interval,
			Participants:     participants,
			ParticipantSlugs: calendarParticipantSlugsForJob(job, tasks, requests, activeChannel, members),
			TaskID:           schedulerTargetTaskID(job),
			RequestID:        schedulerTargetRequestID(job),
			ThreadID:         schedulerTargetThreadID(job, tasks, requests),
		})
	}
	for _, task := range tasks {
		if task.Status == "done" {
			continue
		}
		for _, ev := range taskCalendarEvents(task, activeChannel, members) {
			events = append(events, ev)
		}
	}
	for _, req := range requests {
		for _, ev := range requestCalendarEvents(req, activeChannel, members) {
			events = append(events, ev)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].When.Equal(events[j].When) {
			if events[i].Kind == events[j].Kind {
				return events[i].Title < events[j].Title
			}
			return events[i].Kind < events[j].Kind
		}
		return events[i].When.Before(events[j].When)
	})
	return events
}

func taskCalendarEvents(task channelTask, activeChannel string, members []channelMember) []calendarEvent {
	var events []calendarEvent
	appendTaskEvent := func(label, whenText string) {
		when, ok := parseChannelTime(whenText)
		if !ok {
			return
		}
		participants := calendarParticipantsForTask(task, activeChannel, members)
		status := strings.ReplaceAll(task.Status, "_", " ")
		events = append(events, calendarEvent{
			When:             when,
			WhenLabel:        prettyCalendarWhen(when),
			Kind:             "task",
			Title:            task.Title,
			Secondary:        label,
			Channel:          chooseCalendarChannel(task.Channel, activeChannel),
			Status:           status,
			Participants:     participants,
			ParticipantSlugs: calendarParticipantSlugsForTask(task, activeChannel, members),
			TaskID:           task.ID,
			ThreadID:         task.ThreadID,
		})
	}
	appendTaskEvent("due", task.DueAt)
	appendTaskEvent("follow up", task.FollowUpAt)
	appendTaskEvent("reminder", task.ReminderAt)
	appendTaskEvent("recheck", task.RecheckAt)
	return dedupeCalendarEvents(events)
}

func requestCalendarEvents(req channelInterview, activeChannel string, members []channelMember) []calendarEvent {
	var events []calendarEvent
	appendRequestEvent := func(label, whenText string) {
		when, ok := parseChannelTime(whenText)
		if !ok {
			return
		}
		participants := calendarParticipantsForRequest(req, activeChannel, members)
		status := strings.TrimSpace(req.Status)
		if status == "" {
			status = "pending"
		}
		events = append(events, calendarEvent{
			When:             when,
			WhenLabel:        prettyCalendarWhen(when),
			Kind:             "request",
			Title:            req.Question,
			Secondary:        label,
			Channel:          chooseCalendarChannel(req.Channel, activeChannel),
			Status:           status,
			Participants:     participants,
			ParticipantSlugs: calendarParticipantSlugsForRequest(req, activeChannel, members),
			RequestID:        req.ID,
			ThreadID:         req.ReplyTo,
		})
	}
	appendRequestEvent("due", req.DueAt)
	appendRequestEvent("follow up", req.FollowUpAt)
	appendRequestEvent("reminder", req.ReminderAt)
	appendRequestEvent("recheck", req.RecheckAt)
	return dedupeCalendarEvents(events)
}

func dedupeCalendarEvents(events []calendarEvent) []calendarEvent {
	seen := make(map[string]bool)
	var out []calendarEvent
	for _, event := range events {
		identity := event.Kind + "|" + event.Title
		if strings.TrimSpace(event.TaskID) != "" {
			identity = "task|" + strings.TrimSpace(event.TaskID)
		} else if strings.TrimSpace(event.RequestID) != "" {
			identity = "request|" + strings.TrimSpace(event.RequestID)
		} else if strings.TrimSpace(event.ThreadID) != "" {
			identity = identity + "|thread:" + strings.TrimSpace(event.ThreadID)
		}
		key := identity + "|" + event.Secondary + "|" + event.When.Format(time.RFC3339)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, event)
	}
	return out
}

func filterCalendarEvents(events []calendarEvent, viewRange calendarRange, filterSlug string) []calendarEvent {
	now := time.Now()
	filterSlug = strings.TrimSpace(filterSlug)
	var out []calendarEvent
	for _, event := range events {
		if filterSlug != "" && !containsString(event.ParticipantSlugs, filterSlug) {
			continue
		}
		switch viewRange {
		case calendarRangeDay:
			end := now.Add(24 * time.Hour)
			if event.When.After(end) {
				continue
			}
		default:
			end := now.Add(7 * 24 * time.Hour)
			if event.When.After(end) {
				continue
			}
		}
		out = append(out, event)
	}
	return out
}

func prettyCalendarWhen(when time.Time) string {
	now := time.Now()
	switch {
	case sameDay(when, now):
		return "today " + when.Format("15:04")
	case sameDay(when, now.Add(24*time.Hour)):
		return "tomorrow " + when.Format("15:04")
	default:
		return when.Format("Mon Jan 2 15:04")
	}
}

func calendarBucketLabel(when time.Time) string {
	now := time.Now()
	switch {
	case when.Before(now):
		return "Earlier"
	case sameDay(when, now):
		return "Today"
	case sameDay(when, now.Add(24*time.Hour)):
		return "Tomorrow"
	default:
		return "Upcoming"
	}
}

func chooseCalendarChannel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func calendarParticipantsForTask(task channelTask, activeChannel string, members []channelMember) []string {
	slugs := make([]string, 0, 2)
	if owner := strings.TrimSpace(task.Owner); owner != "" {
		slugs = append(slugs, owner)
	}
	return calendarParticipantNames(slugs, chooseCalendarChannel(task.Channel, activeChannel), members, len(slugs) == 0)
}

func calendarParticipantSlugsForTask(task channelTask, activeChannel string, members []channelMember) []string {
	slugs := make([]string, 0, 2)
	if owner := strings.TrimSpace(task.Owner); owner != "" {
		slugs = append(slugs, owner)
	}
	return calendarParticipantSlugs(slugs, chooseCalendarChannel(task.Channel, activeChannel), members, len(slugs) == 0)
}

func calendarParticipantsForRequest(req channelInterview, activeChannel string, members []channelMember) []string {
	slugs := make([]string, 0, 2)
	if from := strings.TrimSpace(req.From); from != "" {
		slugs = append(slugs, from)
	}
	return calendarParticipantNames(slugs, chooseCalendarChannel(req.Channel, activeChannel), members, len(slugs) == 0)
}

func calendarParticipantSlugsForRequest(req channelInterview, activeChannel string, members []channelMember) []string {
	slugs := make([]string, 0, 2)
	if from := strings.TrimSpace(req.From); from != "" {
		slugs = append(slugs, from)
	}
	return calendarParticipantSlugs(slugs, chooseCalendarChannel(req.Channel, activeChannel), members, len(slugs) == 0)
}

func calendarParticipantsForJob(job channelSchedulerJob, tasks []channelTask, requests []channelInterview, activeChannel string, members []channelMember) []string {
	switch strings.TrimSpace(job.TargetType) {
	case "task":
		for _, task := range tasks {
			if task.ID == job.TargetID {
				return calendarParticipantsForTask(task, activeChannel, members)
			}
		}
	case "request":
		for _, req := range requests {
			if req.ID == job.TargetID {
				return calendarParticipantsForRequest(req, activeChannel, members)
			}
		}
	}
	channel := chooseCalendarChannel(job.Channel, activeChannel)
	return calendarParticipantNames(nil, channel, members, true)
}

func calendarParticipantSlugsForJob(job channelSchedulerJob, tasks []channelTask, requests []channelInterview, activeChannel string, members []channelMember) []string {
	switch strings.TrimSpace(job.TargetType) {
	case "task":
		for _, task := range tasks {
			if task.ID == job.TargetID {
				return calendarParticipantSlugsForTask(task, activeChannel, members)
			}
		}
	case "request":
		for _, req := range requests {
			if req.ID == job.TargetID {
				return calendarParticipantSlugsForRequest(req, activeChannel, members)
			}
		}
	}
	channel := chooseCalendarChannel(job.Channel, activeChannel)
	return calendarParticipantSlugs(nil, channel, members, true)
}

func calendarParticipantNames(primary []string, channel string, members []channelMember, fallbackToChannel bool) []string {
	slugs := calendarParticipantSlugs(primary, channel, members, fallbackToChannel)
	var names []string
	for _, slug := range slugs {
		name := displayName(slug)
		for _, member := range members {
			if member.Slug == slug {
				if strings.TrimSpace(member.Name) != "" {
					name = member.Name
				}
				break
			}
		}
		names = append(names, name)
	}
	// Sort for stable display (slugs already sorted when fallback, but
	// names may differ from slug sort order)
	sort.Strings(names)
	if len(names) > 4 {
		return append(names[:4], fmt.Sprintf("+%d more", len(names)-4))
	}
	return names
}

func calendarParticipantSlugs(primary []string, channel string, members []channelMember, fallbackToChannel bool) []string {
	seen := make(map[string]bool)
	var slugs []string
	addSlug := func(slug string) {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}
	for _, slug := range primary {
		addSlug(slug)
	}
	if fallbackToChannel && len(slugs) == 0 {
		for _, member := range members {
			if member.Disabled {
				continue
			}
			addSlug(member.Slug)
		}
		// Sort alphabetically so order is stable across poll ticks
		sort.Strings(slugs)
	}
	return slugs
}

func nextCalendarEventByParticipant(events []calendarEvent) map[string]calendarEvent {
	out := make(map[string]calendarEvent)
	for _, event := range events {
		for _, participant := range event.Participants {
			if strings.HasPrefix(participant, "+") {
				continue
			}
			existing, ok := out[participant]
			if !ok || event.When.Before(existing.When) {
				out[participant] = event
			}
		}
	}
	return out
}

func orderedCalendarParticipants(byParticipant map[string]calendarEvent, members []channelMember) []string {
	// Collect all participant names, then sort alphabetically for stable display.
	// Previously this followed member poll order which shifts every tick.
	var names []string
	for name := range byParticipant {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func schedulerTargetTaskID(job channelSchedulerJob) string {
	if strings.TrimSpace(job.TargetType) == "task" {
		return strings.TrimSpace(job.TargetID)
	}
	return ""
}

func schedulerTargetRequestID(job channelSchedulerJob) string {
	if strings.TrimSpace(job.TargetType) == "request" {
		return strings.TrimSpace(job.TargetID)
	}
	return ""
}

func schedulerTargetThreadID(job channelSchedulerJob, tasks []channelTask, requests []channelInterview) string {
	switch strings.TrimSpace(job.TargetType) {
	case "task":
		for _, task := range tasks {
			if task.ID == job.TargetID {
				return strings.TrimSpace(task.ThreadID)
			}
		}
	case "request":
		for _, req := range requests {
			if req.ID == job.TargetID {
				return strings.TrimSpace(req.ReplyTo)
			}
		}
	}
	return ""
}

func agentSlugForDisplay(name string, members []channelMember) string {
	for _, member := range members {
		if member.Name == name || displayName(member.Slug) == name {
			return member.Slug
		}
	}
	return ""
}

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func shortClock(ts string) string {
	if len(ts) >= 16 && strings.Contains(ts, "T") {
		return ts[11:16]
	}
	return ts
}

func formatMinutes(v int) string {
	if v <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d", v)
}

func renderTimingSummary(dueAt, followUpAt, reminderAt, recheckAt string) string {
	var parts []string
	if label := prettyWhen(dueAt, "due"); label != "" {
		parts = append(parts, label)
	}
	if label := prettyWhen(followUpAt, "follow up"); label != "" {
		parts = append(parts, label)
	}
	if label := prettyWhen(reminderAt, "remind"); label != "" {
		parts = append(parts, label)
	}
	if label := prettyWhen(recheckAt, "recheck"); label != "" {
		parts = append(parts, label)
	}
	return strings.Join(parts, " · ")
}

func prettyWhen(ts, prefix string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	parsed, ok := parseChannelTime(ts)
	if !ok {
		return strings.TrimSpace(prefix + " " + ts)
	}
	now := time.Now()
	label := parsed.Format("Mon 15:04")
	switch {
	case sameDay(parsed, now):
		label = parsed.Format("15:04")
	case sameDay(parsed, now.Add(24*time.Hour)):
		label = "tomorrow " + parsed.Format("15:04")
	case parsed.Before(now.Add(-24 * time.Hour)):
		label = parsed.Format("Jan 2 15:04")
	}
	if parsed.Before(now) && prefix == "due" {
		return "overdue since " + label
	}
	return strings.TrimSpace(prefix + " " + label)
}

func parseChannelTime(ts string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	} {
		if parsed, err := time.Parse(layout, ts); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func sameDay(left, right time.Time) bool {
	ly, lm, ld := left.Date()
	ry, rm, rd := right.Date()
	return ly == ry && lm == rm && ld == rd
}
