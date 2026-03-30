package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
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

	// Always expand threads — collapsed is confusing, expanded is readable
	for _, msg := range messages {
		if msg.ReplyTo == "" && hasThreadReplies(messages, msg.ID) {
			expanded[msg.ID] = true
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
			continue
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

func buildOneOnOneMessageLines(messages []brokerMessage, expanded map[string]bool, contentWidth int, agentName string) []renderedLine {
	if len(messages) == 0 {
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
		return []renderedLine{
			{Text: ""},
			{Text: mutedStyle.Render("  Direct 1:1 with " + agentName + ".")},
			{Text: mutedStyle.Render("  There is no office, no channel, and no teammate roster in this mode.")},
			{Text: ""},
			{Text: mutedStyle.Render("  Suggested: Help me think through the v1 launch plan.")},
			{Text: mutedStyle.Render("  This conversation is just you and " + agentName + ".")},
		}
	}
	return buildOfficeMessageLines(messages, expanded, contentWidth, true)
}

func humanMessageLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "human_decision":
		return "decision"
	case "human_action":
		return "action"
	default:
		return "report"
	}
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

func buildInsightLines(signals []channelSignal, decisions []channelDecision, alerts []channelWatchdog, actions []channelAction, contentWidth int) []renderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	if len(signals) == 0 && len(decisions) == 0 && len(alerts) == 0 && len(recentExternalActions(actions, 8)) == 0 {
		return []renderedLine{
			{Text: ""},
			{Text: muted.Render("  No office signals yet.")},
			{Text: muted.Render("  When Nex, One relays, or the office policy engine notices something important, it will land here with the reasoning trail.")},
		}
	}
	var lines []renderedLine
	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Insights")})
	appendWrappedLine := func(text string) {
		wrapped := appendWrapped(nil, maxInt(20, contentWidth-4), text)
		for _, line := range wrapped {
			lines = append(lines, renderedLine{Text: line})
		}
	}
	if len(signals) > 0 {
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Signals")})
		for _, signal := range reverseSignals(signals, 8) {
			metaParts := []string{signal.Source}
			if signal.Urgency != "" {
				metaParts = append(metaParts, signal.Urgency)
			}
			if signal.Owner != "" {
				metaParts = append(metaParts, "@"+signal.Owner)
			}
			if signal.Channel != "" {
				metaParts = append(metaParts, "#"+signal.Channel)
			}
			if signal.Confidence != "" {
				metaParts = append(metaParts, signal.Confidence)
			}
			lines = append(lines, renderedLine{Text: ""})
			appendWrappedLine("  " + subtlePill(strings.ToUpper(fallbackString(signal.Kind, "signal")), "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render(fallbackString(signal.Title, "Office signal")))
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
			appendWrappedLine("  " + signal.Content)
		}
	}
	if len(decisions) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Decisions")})
		for _, decision := range reverseDecisions(decisions, 8) {
			metaParts := []string{decision.Kind}
			if decision.Owner != "" {
				metaParts = append(metaParts, "@"+decision.Owner)
			}
			if len(decision.SignalIDs) > 0 {
				metaParts = append(metaParts, fmt.Sprintf("%d signal(s)", len(decision.SignalIDs)))
			}
			if decision.Channel != "" {
				metaParts = append(metaParts, "#"+decision.Channel)
			}
			lines = append(lines, renderedLine{Text: ""})
			appendWrappedLine("  " + accentPill("decision", "#1264A3") + " " + lipgloss.NewStyle().Bold(true).Render(decision.Summary))
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
			if strings.TrimSpace(decision.Reason) != "" {
				appendWrappedLine("  " + muted.Render("Why: "+decision.Reason))
			}
		}
	}
	if len(alerts) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("Watchdogs")})
		for _, alert := range reverseWatchdogs(activeWatchdogs(alerts), 6) {
			metaParts := []string{alert.Kind}
			if alert.Owner != "" {
				metaParts = append(metaParts, "@"+alert.Owner)
			}
			if alert.Channel != "" {
				metaParts = append(metaParts, "#"+alert.Channel)
			}
			if alert.Status != "" {
				metaParts = append(metaParts, alert.Status)
			}
			lines = append(lines, renderedLine{Text: ""})
			appendWrappedLine("  " + subtlePill("watchdog", "#FEF3C7", "#92400E") + " " + lipgloss.NewStyle().Bold(true).Render(alert.Summary))
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
		}
	}
	if external := recentExternalActions(actions, 8); len(external) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  " + lipgloss.NewStyle().Bold(true).Render("External actions")})
		for _, act := range external {
			metaParts := []string{fallbackString(act.Source, "one")}
			if act.Actor != "" {
				metaParts = append(metaParts, "@"+act.Actor)
			}
			if act.Channel != "" {
				metaParts = append(metaParts, "#"+act.Channel)
			}
			if act.RelatedID != "" {
				metaParts = append(metaParts, act.RelatedID)
			}
			lines = append(lines, renderedLine{Text: ""})
			appendWrappedLine("  " + subtlePill(strings.ReplaceAll(act.Kind, "_", " "), "#DBEAFE", "#1D4ED8") + " " + lipgloss.NewStyle().Bold(true).Render(act.Summary))
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
		}
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
		"review":      "#2563EB",
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
		if task.TaskType != "" {
			metaParts = append(metaParts, task.TaskType)
		}
		if task.PipelineStage != "" {
			metaParts = append(metaParts, "stage "+task.PipelineStage)
		}
		if task.ReviewState != "" && task.ReviewState != "not_required" {
			metaParts = append(metaParts, "review "+task.ReviewState)
		}
		if task.ExecutionMode != "" {
			metaParts = append(metaParts, task.ExecutionMode)
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
		if task.SourceSignalID != "" || task.SourceDecisionID != "" {
			sourceBits := []string{}
			if task.SourceSignalID != "" {
				sourceBits = append(sourceBits, "signal "+task.SourceSignalID)
			}
			if task.SourceDecisionID != "" {
				sourceBits = append(sourceBits, "decision "+task.SourceDecisionID)
			}
			lines = append(lines, renderedLine{Text: "  " + muted.Render("Triggered by "+strings.Join(sourceBits, " · ")), TaskID: task.ID})
		}
		if task.WorktreePath != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render("Workspace: "+task.WorktreePath), TaskID: task.ID})
		}
		taskActionHint := "Click to claim, complete, block, or release."
		if task.Status == "review" || task.ReviewState == "ready_for_review" {
			taskActionHint = "Click to approve, block, or release."
		} else if task.ReviewState == "pending_review" || task.ExecutionMode == "local_worktree" {
			taskActionHint = "Click to claim, send to review, block, or release."
		}
		lines = append(lines, renderedLine{Text: "  " + muted.Render(taskActionHint), TaskID: task.ID})
	}
	return lines
}

func buildSkillLines(skills []channelSkill, contentWidth int) []renderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	if len(skills) == 0 {
		return []renderedLine{
			{Text: ""},
			{Text: muted.Render("  No skills yet.")},
			{Text: muted.Render("  Skills are reusable prompts the team builds over time.")},
			{Text: muted.Render("  Use /skill create <description> to define one.")},
		}
	}
	statusColor := map[string]string{
		"active":   "#22C55E",
		"draft":    "#94A3B8",
		"disabled": "#EF4444",
	}
	var lines []renderedLine
	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Skills")})
	for _, skill := range skills {
		color := statusColor[skill.Status]
		if color == "" {
			color = "#22C55E"
		}
		statusLabel := skill.Status
		if statusLabel == "" {
			statusLabel = "active"
		}
		status := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(statusLabel)
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: "  ⚡ " + lipgloss.NewStyle().Bold(true).Render(skill.Title) + "  " + status})
		if skill.Description != "" {
			for _, line := range appendWrapped(nil, maxInt(20, contentWidth-4), "  "+skill.Description) {
				lines = append(lines, renderedLine{Text: line})
			}
		}
		metaParts := []string{}
		if skill.Name != "" {
			metaParts = append(metaParts, skill.Name)
		}
		if skill.UsageCount > 0 {
			metaParts = append(metaParts, fmt.Sprintf("%d uses", skill.UsageCount))
		}
		if skill.CreatedBy != "" {
			metaParts = append(metaParts, "by "+displayName(skill.CreatedBy))
		}
		if len(skill.Tags) > 0 {
			metaParts = append(metaParts, strings.Join(skill.Tags, ", "))
		}
		if len(metaParts) > 0 {
			lines = append(lines, renderedLine{Text: "  " + muted.Render(strings.Join(metaParts, " · "))})
		}
		if skill.Trigger != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render("trigger: "+skill.Trigger)})
		}
		if skill.WorkflowKey != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render(fmt.Sprintf("workflow: %s via %s", skill.WorkflowKey, fallbackString(skill.WorkflowProvider, "one")))})
		}
		if skill.WorkflowSchedule != "" {
			lines = append(lines, renderedLine{Text: "  " + muted.Render("schedule: " + skill.WorkflowSchedule)})
		}
		if skill.RelayID != "" || skill.RelayPlatform != "" || len(skill.RelayEventTypes) > 0 {
			relayParts := []string{}
			if skill.RelayPlatform != "" {
				relayParts = append(relayParts, skill.RelayPlatform)
			}
			if len(skill.RelayEventTypes) > 0 {
				relayParts = append(relayParts, strings.Join(skill.RelayEventTypes, ", "))
			}
			if skill.RelayID != "" {
				relayParts = append(relayParts, skill.RelayID)
			}
			lines = append(lines, renderedLine{Text: "  " + muted.Render("relay: " + strings.Join(relayParts, " · "))})
		}
		if skill.LastExecutionAt != "" || skill.LastExecutionStatus != "" {
			runParts := []string{}
			if skill.LastExecutionStatus != "" {
				runParts = append(runParts, skill.LastExecutionStatus)
			}
			if skill.LastExecutionAt != "" {
				runParts = append(runParts, prettyRelativeTime(skill.LastExecutionAt))
			}
			lines = append(lines, renderedLine{Text: "  " + muted.Render("last run: " + strings.Join(runParts, " · "))})
		}
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
	Provider         string
	ScheduleExpr     string
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
			Provider:         strings.TrimSpace(job.Provider),
			ScheduleExpr:     strings.TrimSpace(job.ScheduleExpr),
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
		secondary := label
		if strings.TrimSpace(task.PipelineStage) != "" {
			secondary += " · " + task.PipelineStage
		}
		if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" {
			secondary += " · " + task.ReviewState
		}
		events = append(events, calendarEvent{
			When:             when,
			WhenLabel:        prettyCalendarWhen(when),
			Kind:             "task",
			Title:            task.Title,
			Secondary:        secondary,
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

func prettyRelativeTime(ts string) string {
	parsed, ok := parseChannelTime(ts)
	if !ok {
		return ts
	}
	now := time.Now()
	diff := now.Sub(parsed)
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff/time.Minute))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff/time.Hour))
	default:
		return parsed.Format("Jan 2 15:04")
	}
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

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func reverseSignals(signals []channelSignal, limit int) []channelSignal {
	if limit > 0 && len(signals) > limit {
		signals = signals[len(signals)-limit:]
	}
	out := append([]channelSignal(nil), signals...)
	reverseAny(out)
	return out
}

func reverseDecisions(decisions []channelDecision, limit int) []channelDecision {
	if limit > 0 && len(decisions) > limit {
		decisions = decisions[len(decisions)-limit:]
	}
	out := append([]channelDecision(nil), decisions...)
	reverseAny(out)
	return out
}

func activeWatchdogs(alerts []channelWatchdog) []channelWatchdog {
	var out []channelWatchdog
	for _, alert := range alerts {
		if strings.TrimSpace(alert.Status) == "resolved" {
			continue
		}
		out = append(out, alert)
	}
	return out
}

func reverseWatchdogs(alerts []channelWatchdog, limit int) []channelWatchdog {
	if limit > 0 && len(alerts) > limit {
		alerts = alerts[len(alerts)-limit:]
	}
	out := append([]channelWatchdog(nil), alerts...)
	reverseAny(out)
	return out
}

func recentExternalActions(actions []channelAction, limit int) []channelAction {
	var filtered []channelAction
	for _, action := range actions {
		if !strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
			continue
		}
		filtered = append(filtered, action)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := append([]channelAction(nil), filtered...)
	reverseAny(out)
	return out
}

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
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
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
	return result
}
