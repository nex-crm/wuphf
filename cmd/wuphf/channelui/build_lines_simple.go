package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BuildRequestLines renders the "Open requests" feed for the requests
// app. Returns an empty-state explanation when requests is empty.
func BuildRequestLines(requests []Interview, contentWidth int) []RenderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	highlight := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Bold(true)
	if len(requests) == 0 {
		return []RenderedLine{
			{Text: ""},
			{Text: muted.Render("  No open requests right now. The team is self-sufficient.")},
			{Text: muted.Render("  When an agent needs a real decision, it shows up here — unlike Toby's requests, these matter.")},
		}
	}
	var lines []RenderedLine
	lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Open requests")})
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
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: "  " + RequestKindPill(req.Kind) + " " + title.Render(req.Question), RequestID: req.ID})
		if req.Title != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render(req.Title+" · "+meta), RequestID: req.ID})
		} else {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render(meta), RequestID: req.ID})
		}
		for _, line := range AppendWrapped(nil, MaxInt(20, contentWidth-4), "  "+req.Context) {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, RenderedLine{Text: line, RequestID: req.ID})
			}
		}
		if timing := RenderTimingSummary(req.DueAt, req.FollowUpAt, req.ReminderAt, req.RecheckAt); timing != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render(timing), RequestID: req.ID})
		}
		if req.RecommendedID != "" {
			lines = append(lines, RenderedLine{Text: "  " + highlight.Render("Recommended: "+req.RecommendedID), RequestID: req.ID})
		}
		dismissHint := "Click to focus, answer, or dismiss. Dismiss cancels the request."
		if req.Blocking {
			dismissHint = "Click to focus, answer, or dismiss. Dismiss cancels the request and unblocks the team."
		}
		lines = append(lines, RenderedLine{Text: "  " + muted.Render(dismissHint), RequestID: req.ID})
	}
	return lines
}

// BuildSkillLines renders the "Skills" feed for the skills app. Returns
// an empty-state explanation (and a /skill create hint) when skills is
// empty.
func BuildSkillLines(skills []Skill, contentWidth int) []RenderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted))
	if len(skills) == 0 {
		return []RenderedLine{
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
	var lines []RenderedLine
	lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Skills")})
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
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: "  ⚡ " + lipgloss.NewStyle().Bold(true).Render(skill.Title) + "  " + status})
		if skill.Description != "" {
			for _, line := range AppendWrapped(nil, MaxInt(20, contentWidth-4), "  "+skill.Description) {
				lines = append(lines, RenderedLine{Text: line})
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
			metaParts = append(metaParts, "by "+DisplayName(skill.CreatedBy))
		}
		if len(skill.Tags) > 0 {
			metaParts = append(metaParts, strings.Join(skill.Tags, ", "))
		}
		if len(metaParts) > 0 {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render(strings.Join(metaParts, " · "))})
		}
		if skill.Trigger != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render("trigger: "+skill.Trigger)})
		}
		if skill.WorkflowKey != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render(fmt.Sprintf("workflow: %s via %s", skill.WorkflowKey, FallbackString(skill.WorkflowProvider, "one")))})
		}
		if skill.WorkflowSchedule != "" {
			lines = append(lines, RenderedLine{Text: "  " + muted.Render("schedule: "+skill.WorkflowSchedule)})
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
			lines = append(lines, RenderedLine{Text: "  " + muted.Render("relay: "+strings.Join(relayParts, " · "))})
		}
		if skill.LastExecutionAt != "" || skill.LastExecutionStatus != "" {
			runParts := []string{}
			if skill.LastExecutionStatus != "" {
				runParts = append(runParts, skill.LastExecutionStatus)
			}
			if skill.LastExecutionAt != "" {
				runParts = append(runParts, PrettyRelativeTime(skill.LastExecutionAt))
			}
			lines = append(lines, RenderedLine{Text: "  " + muted.Render("last run: "+strings.Join(runParts, " · "))})
		}
	}
	return lines
}
