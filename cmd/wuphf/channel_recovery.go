package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) currentRuntimeSnapshot() team.RuntimeSnapshot {
	return team.BuildRuntimeSnapshot(team.RuntimeSnapshotInput{
		Channel:     m.activeChannel,
		SessionMode: m.sessionMode,
		DirectAgent: m.oneOnOneAgentSlug(),
		Tasks:       runtimeTasksFromChannel(m.tasks),
		Requests:    runtimeRequestsFromChannel(m.requests),
		Recent:      runtimeMessagesFromChannel(m.messages, 6),
	})
}

func runtimeTasksFromChannel(tasks []channelTask) []team.RuntimeTask {
	out := make([]team.RuntimeTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, team.RuntimeTask{
			ID:             task.ID,
			Title:          strings.TrimSpace(task.Title),
			Owner:          strings.TrimSpace(task.Owner),
			Status:         strings.TrimSpace(task.Status),
			PipelineStage:  strings.TrimSpace(task.PipelineStage),
			ReviewState:    strings.TrimSpace(task.ReviewState),
			ExecutionMode:  strings.TrimSpace(task.ExecutionMode),
			WorktreePath:   strings.TrimSpace(task.WorktreePath),
			WorktreeBranch: strings.TrimSpace(task.WorktreeBranch),
			Blocked:        strings.EqualFold(strings.TrimSpace(task.Status), "blocked"),
		})
	}
	return out
}

func runtimeRequestsFromChannel(requests []channelInterview) []team.RuntimeRequest {
	out := make([]team.RuntimeRequest, 0, len(requests))
	for _, req := range requests {
		out = append(out, team.RuntimeRequest{
			ID:       req.ID,
			Kind:     strings.TrimSpace(req.Kind),
			Title:    strings.TrimSpace(req.Title),
			Question: strings.TrimSpace(req.Question),
			From:     strings.TrimSpace(req.From),
			Blocking: req.Blocking,
			Required: req.Required,
			Status:   strings.TrimSpace(req.Status),
			Channel:  strings.TrimSpace(req.Channel),
			Secret:   req.Secret,
		})
	}
	return out
}

func runtimeMessagesFromChannel(messages []brokerMessage, limit int) []team.RuntimeMessage {
	if limit <= 0 {
		limit = 6
	}
	out := make([]team.RuntimeMessage, 0, minInt(len(messages), limit))
	for i := len(messages) - 1; i >= 0 && len(out) < limit; i-- {
		msg := messages[i]
		out = append(out, team.RuntimeMessage{
			ID:        msg.ID,
			From:      strings.TrimSpace(msg.From),
			Title:     strings.TrimSpace(msg.Title),
			Content:   strings.TrimSpace(msg.Content),
			ReplyTo:   strings.TrimSpace(msg.ReplyTo),
			Timestamp: strings.TrimSpace(msg.Timestamp),
		})
	}
	return out
}

func summarizeAwayRecovery(unreadCount int, recovery team.SessionRecovery) string {
	parts := make([]string, 0, 3)
	if focus := trimRecoverySentence(recovery.Focus); focus != "" {
		parts = append(parts, focus)
	}
	if len(recovery.NextSteps) > 0 {
		if next := trimRecoverySentence(recovery.NextSteps[0]); next != "" {
			parts = append(parts, "Next: "+next)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d new since you looked. Open /recover for the full summary.", unreadCount)
	}
	summary := strings.Join(parts, " ")
	if unreadCount > 0 {
		summary = fmt.Sprintf("%d new since you looked. %s", unreadCount, summary)
	}
	return truncateText(summary, 120)
}

func (m channelModel) currentAwaySummary() string {
	if m.unreadCount == 0 {
		return ""
	}
	if text := strings.TrimSpace(m.awaySummary); text != "" {
		return text
	}
	return summarizeAwayRecovery(m.unreadCount, m.currentRuntimeSnapshot().Recovery)
}

func trimRecoverySentence(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ".")
	return text
}

func renderAwayStrip(width, unreadCount int, summary string) string {
	label := fmt.Sprintf("While away · %d new · /recover", unreadCount)
	if strings.TrimSpace(summary) != "" {
		label = fmt.Sprintf("While away · %s · /recover", strings.TrimSpace(summary))
	}
	label = truncateText(label, maxInt(24, width-6))
	return "  " + lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0F172A")).
		Background(lipgloss.Color("#BFDBFE")).
		Padding(0, 1).
		Bold(true).
		Render(label)
}

func buildRecoveryLines(snapshot team.RuntimeSnapshot, contentWidth int, awaySummary string, unreadCount int, brokerConnected bool) []renderedLine {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	lines := []renderedLine{{Text: renderDateSeparator(contentWidth, "Recovery")}}

	if !brokerConnected && len(snapshot.Tasks) == 0 && len(snapshot.Requests) == 0 && len(snapshot.Recent) == 0 {
		lines = append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: muted.Render("  Offline preview. Launch WUPHF to hydrate the runtime state and recovery summary.")},
			renderedLine{Text: muted.Render("  The recovery view will highlight focus, next steps, and recent changes once the office is live.")},
		)
		return lines
	}

	if unreadCount > 0 || strings.TrimSpace(awaySummary) != "" {
		title := subtlePill("while away", "#F8FAFC", "#1D4ED8") + " " + lipgloss.NewStyle().Bold(true).Render("What changed while you were gone")
		body := strings.TrimSpace(awaySummary)
		if body == "" {
			body = "Use this view to regain context before you reply."
		}
		extra := []string{}
		if focus := strings.TrimSpace(snapshot.Recovery.Focus); focus != "" {
			extra = append(extra, "Focus: "+focus)
		}
		if len(snapshot.Recovery.NextSteps) > 0 {
			extra = append(extra, "Next: "+snapshot.Recovery.NextSteps[0])
		}
		for _, line := range renderRuntimeEventCard(contentWidth, title, body, "#2563EB", extra) {
			lines = append(lines, renderedLine{Text: "  " + line})
		}
	}

	stateBody := fmt.Sprintf("%d running tasks · %d open requests · %d isolated worktrees", countRunningRuntimeTasks(snapshot.Tasks), len(snapshot.Requests), countIsolatedRuntimeTasks(snapshot.Tasks))
	stateExtra := []string{}
	if snapshot.SessionMode == team.SessionModeOneOnOne && strings.TrimSpace(snapshot.DirectAgent) != "" {
		stateExtra = append(stateExtra, "Direct session with @"+snapshot.DirectAgent)
	} else if strings.TrimSpace(snapshot.Channel) != "" {
		stateExtra = append(stateExtra, "Channel: #"+snapshot.Channel)
	}
	if focus := strings.TrimSpace(snapshot.Recovery.Focus); focus != "" {
		stateExtra = append(stateExtra, "Current focus: "+focus)
	}
	for _, line := range renderRuntimeEventCard(contentWidth, subtlePill("runtime", "#E2E8F0", "#334155")+" "+lipgloss.NewStyle().Bold(true).Render("Current state"), stateBody, "#475569", stateExtra) {
		lines = append(lines, renderedLine{Text: "  " + line})
	}

	if len(snapshot.Recovery.NextSteps) > 0 {
		body := snapshot.Recovery.NextSteps[0]
		extra := append([]string(nil), snapshot.Recovery.NextSteps[1:]...)
		for _, line := range renderRuntimeEventCard(contentWidth, subtlePill("next", "#F8FAFC", "#92400E")+" "+lipgloss.NewStyle().Bold(true).Render("What to do next"), body, "#B45309", extra) {
			lines = append(lines, renderedLine{Text: "  " + line})
		}
	}

	if len(snapshot.Recovery.Highlights) > 0 {
		body := snapshot.Recovery.Highlights[0]
		extra := append([]string(nil), snapshot.Recovery.Highlights[1:]...)
		for _, line := range renderRuntimeEventCard(contentWidth, subtlePill("recent", "#E5E7EB", "#334155")+" "+lipgloss.NewStyle().Bold(true).Render("Latest highlights"), body, "#334155", extra) {
			lines = append(lines, renderedLine{Text: "  " + line})
		}
	}

	return lines
}

func countRunningRuntimeTasks(tasks []team.RuntimeTask) int {
	count := 0
	for _, task := range tasks {
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "", "done", "completed", "canceled", "cancelled":
			continue
		default:
			count++
		}
	}
	return count
}

func countIsolatedRuntimeTasks(tasks []team.RuntimeTask) int {
	count := 0
	for _, task := range tasks {
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") ||
			strings.TrimSpace(task.WorktreePath) != "" ||
			strings.TrimSpace(task.WorktreeBranch) != "" {
			count++
		}
	}
	return count
}
