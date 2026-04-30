package channelui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BuildPolicyLines renders the "Insights" feed for the policies app —
// signals, decisions, watchdogs, and external actions, each capped to
// the most recent eight (six for external) and shown newest-first.
func BuildPolicyLines(signals []Signal, decisions []Decision, alerts []Watchdog, actions []Action, contentWidth int) []RenderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted))
	var lines []RenderedLine
	lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Insights")})

	if len(signals) == 0 && len(decisions) == 0 && len(alerts) == 0 && len(actions) == 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: muted.Render("  No office insights yet. Give the team a minute.")})
		lines = append(lines, RenderedLine{Text: muted.Render("  Signals, decisions, watchdogs, and external actions will appear here")})
		lines = append(lines, RenderedLine{Text: muted.Render("  as the office starts tracking higher-signal work — like the Dundies, but actually useful.")})
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: muted.Render("  Use /policies to refresh this ledger. Even Michael checked his metrics eventually.")})
		return lines
	}

	appendWrappedLine := func(text string) {
		wrapped := AppendWrapped(nil, MaxInt(20, contentWidth-4), text)
		for _, line := range wrapped {
			lines = append(lines, RenderedLine{Text: line})
		}
	}

	appendSection := func(title string) {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, title)})
	}

	for _, signal := range ReverseSignals(signals, 8) {
		if len(lines) == 1 {
			appendSection("Signals")
		}
		metaParts := []string{}
		if kind := DisplaySignalKind(signal); kind != "" {
			metaParts = append(metaParts, kind)
		}
		if signal.Owner != "" {
			metaParts = append(metaParts, "@"+signal.Owner)
		}
		if signal.Channel != "" {
			metaParts = append(metaParts, "#"+signal.Channel)
		}
		if signal.Urgency != "" {
			metaParts = append(metaParts, "urgency "+signal.Urgency)
		}
		if signal.Confidence != "" {
			metaParts = append(metaParts, "confidence "+signal.Confidence)
		}
		appendWrappedLine("  " + AccentPill("signal", "#7C3AED") + " " + lipgloss.NewStyle().Bold(true).Render(FallbackString(signal.Title, "Office signal")))
		if len(metaParts) > 0 {
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
		}
		appendWrappedLine("  " + signal.Content)
	}

	if len(decisions) > 0 {
		appendSection("Decisions")
	}
	for _, decision := range ReverseDecisions(decisions, 8) {
		metaParts := []string{}
		if decision.Owner != "" {
			metaParts = append(metaParts, "by @"+decision.Owner)
		}
		if decision.Channel != "" {
			metaParts = append(metaParts, "#"+decision.Channel)
		}
		lines = append(lines, RenderedLine{Text: ""})
		appendWrappedLine("  " + AccentPill("policy", "#1264A3") + " " + lipgloss.NewStyle().Bold(true).Render("Decisions · "+DisplayDecisionSummary(decision.Summary)))
		if len(metaParts) > 0 {
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
		}
		if strings.TrimSpace(decision.Reason) != "" {
			appendWrappedLine("  " + muted.Render("Why: "+decision.Reason))
		}
	}

	watchdogs := ActiveWatchdogs(alerts)
	if len(watchdogs) > 0 {
		appendSection("Watchdogs")
	}
	for _, alert := range ReverseWatchdogs(watchdogs, 8) {
		metaParts := []string{}
		if alert.Owner != "" {
			metaParts = append(metaParts, "@"+alert.Owner)
		}
		if alert.Channel != "" {
			metaParts = append(metaParts, "#"+alert.Channel)
		}
		if alert.Kind != "" {
			metaParts = append(metaParts, alert.Kind)
		}
		if alert.Status != "" {
			metaParts = append(metaParts, alert.Status)
		}
		appendWrappedLine("  " + AccentPill("watchdog", "#DC2626") + " " + lipgloss.NewStyle().Bold(true).Render(FallbackString(alert.Summary, "Watchdog alert")))
		if len(metaParts) > 0 {
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
		}
	}

	external := RecentExternalActions(actions, 6)
	if len(external) > 0 {
		appendSection("External Actions")
	}
	for _, action := range external {
		metaParts := []string{}
		if action.Actor != "" {
			metaParts = append(metaParts, "@"+action.Actor)
		}
		if action.Channel != "" {
			metaParts = append(metaParts, "#"+action.Channel)
		}
		if action.Kind != "" {
			metaParts = append(metaParts, action.Kind)
		}
		if action.Source != "" {
			metaParts = append(metaParts, action.Source)
		}
		appendWrappedLine("  " + AccentPill("action", "#0F766E") + " " + lipgloss.NewStyle().Bold(true).Render(FallbackString(action.Summary, "External action")))
		if len(metaParts) > 0 {
			appendWrappedLine("  " + muted.Render(strings.Join(metaParts, " · ")))
		}
	}
	return lines
}

// BuildTaskLines renders the "Tasks" feed for the tasks app, including
// per-task metadata (status, owner, channel, type, pipeline stage,
// review state, execution mode), wrapped details, timing summary,
// source attribution, worktree path, and a contextual click hint.
func BuildTaskLines(tasks []Task, contentWidth int) []RenderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted))
	if len(tasks) == 0 {
		return []RenderedLine{
			{Text: ""},
			{Text: muted.Render("  No active work tracked yet. Either the team is ahead of schedule,")},
			{Text: muted.Render("  or everyone's at the vending machine. Tag someone in #general to find out.")},
		}
	}
	statusColor := map[string]string{
		"open":        "#94A3B8",
		"in_progress": "#F59E0B",
		"review":      "#2563EB",
		"done":        "#22C55E",
	}
	var lines []RenderedLine
	lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Tasks")})
	for _, task := range tasks {
		color := statusColor[task.Status]
		if color == "" {
			color = "#94A3B8"
		}
		status := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(strings.ReplaceAll(task.Status, "_", " "))
		metaParts := []string{task.ID, status}
		if task.Owner != "" {
			metaParts = append(metaParts, "owner "+DisplayName(task.Owner))
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
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: "  " + TaskStatusPill(task.Status) + " " + lipgloss.NewStyle().Bold(true).Render(task.Title), TaskID: task.ID})
		lines = append(lines, RenderedLine{Text: "  " + muted.Render(meta), TaskID: task.ID})
		if task.Details != "" {
			for _, line := range AppendWrapped(nil, MaxInt(20, contentWidth-4), "  "+task.Details) {
				lines = append(lines, RenderedLine{Text: line, TaskID: task.ID})
			}
		}
		if timing := RenderTimingSummary(task.DueAt, task.FollowUpAt, task.ReminderAt, task.RecheckAt); timing != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render(timing), TaskID: task.ID})
		}
		if task.SourceSignalID != "" || task.SourceDecisionID != "" {
			sourceBits := []string{}
			if task.SourceSignalID != "" {
				sourceBits = append(sourceBits, "signal "+task.SourceSignalID)
			}
			if task.SourceDecisionID != "" {
				sourceBits = append(sourceBits, "decision "+task.SourceDecisionID)
			}
			lines = append(lines, RenderedLine{Text: "  " + muted.Render("Triggered by "+strings.Join(sourceBits, " · ")), TaskID: task.ID})
		}
		if task.WorktreePath != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render("Workspace: "+task.WorktreePath), TaskID: task.ID})
		}
		taskActionHint := "Click to claim, complete, block, or release."
		if task.Status == "review" || task.ReviewState == "ready_for_review" {
			taskActionHint = "Click to approve, block, or release."
		} else if task.ReviewState == "pending_review" || task.ExecutionMode == "local_worktree" {
			taskActionHint = "Click to claim, send to review, block, or release."
		}
		lines = append(lines, RenderedLine{Text: "  " + muted.Render(taskActionHint), TaskID: task.ID})
	}
	return lines
}
