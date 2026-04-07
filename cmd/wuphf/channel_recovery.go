package main

import (
	"fmt"
	"sort"
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

func (m channelModel) buildRecoveryLines(contentWidth int) []renderedLine {
	return buildRecoveryLines(m.currentRuntimeSnapshot(), contentWidth, m.awaySummary, m.unreadCount, m.brokerConnected, m.tasks, m.requests, m.messages)
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

func buildRecoveryLines(snapshot team.RuntimeSnapshot, contentWidth int, awaySummary string, unreadCount int, brokerConnected bool, tasks []channelTask, requests []channelInterview, messages []brokerMessage) []renderedLine {
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

	if actionLines := buildRecoveryActionLines(contentWidth, tasks, requests, messages); len(actionLines) > 0 {
		lines = append(lines, actionLines...)
	}

	return lines
}

func buildRecoveryActionLines(contentWidth int, tasks []channelTask, requests []channelInterview, messages []brokerMessage) []renderedLine {
	lines := []renderedLine{}

	if req, ok := selectNeedsYouRequest(requests); ok {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Resume human decisions")})
		header := accentPill("needs you", "#B45309") + " " + lipgloss.NewStyle().Bold(true).Render(req.TitleOrQuestion())
		body := strings.TrimSpace(req.Context)
		if body == "" {
			body = strings.TrimSpace(req.Question)
		}
		extra := []string{"Asked by @" + fallbackString(req.From, "unknown")}
		if strings.TrimSpace(req.RecommendedID) != "" {
			extra = append(extra, "Recommended: "+req.RecommendedID)
		}
		extra = append(extra, "Open request")
		lines = append(lines, prefixedCardLines(renderedCardLines(renderRecoveryActionCard(contentWidth, header, body, "#B45309", extra), "", req.ID, strings.TrimSpace(req.ReplyTo), ""), "  ")...)
	}

	if activeTasks := recoveryActiveTasks(tasks, 3); len(activeTasks) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Resume active tasks")})
		for _, task := range activeTasks {
			header := taskStatusPill(task.Status) + " " + lipgloss.NewStyle().Bold(true).Render(task.Title)
			body := strings.TrimSpace(task.Details)
			if body == "" {
				body = "Owner @" + fallbackString(task.Owner, "unowned")
			}
			extra := []string{"Owner @" + fallbackString(task.Owner, "unowned")}
			if strings.TrimSpace(task.ThreadID) != "" {
				extra = append(extra, "Thread "+task.ThreadID)
			}
			if strings.TrimSpace(task.WorktreePath) != "" {
				extra = append(extra, "Worktree "+task.WorktreePath)
			}
			extra = append(extra, "Open task")
			threadID := strings.TrimSpace(task.ThreadID)
			lines = append(lines, prefixedCardLines(renderedCardLines(renderRecoveryActionCard(contentWidth, header, body, "#2563EB", extra), task.ID, "", threadID, ""), "  ")...)
		}
	}

	if recent := recoveryRecentThreads(messages, 3); len(recent) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Return to recent threads")})
		for _, msg := range recent {
			header := subtlePill("@"+fallbackString(msg.From, "unknown"), "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render("Thread "+msg.ID)
			body := truncateText(strings.TrimSpace(msg.Content), 160)
			extra := []string{}
			if when := strings.TrimSpace(msg.Timestamp); when != "" {
				extra = append(extra, prettyRelativeTime(when))
			}
			extra = append(extra, "Open thread")
			lines = append(lines, prefixedCardLines(renderedCardLines(renderRecoveryActionCard(contentWidth, header, body, "#475569", extra), "", "", msg.ID, ""), "  ")...)
		}
	}

	return lines
}

func renderRecoveryActionCard(contentWidth int, header, body, accent string, extra []string) string {
	cardWidth := maxInt(24, contentWidth-6)
	parts := []string{header}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, mutedText(body))
	}
	for _, line := range extra {
		if strings.TrimSpace(line) != "" {
			parts = append(parts, mutedText(line))
		}
	}
	return lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Background(lipgloss.Color("#16181E")).
		Padding(0, 1).
		Render(strings.Join(parts, "\n"))
}

func prefixedCardLines(lines []renderedLine, prefix string) []renderedLine {
	out := make([]renderedLine, 0, len(lines))
	for _, line := range lines {
		line.Text = prefix + line.Text
		out = append(out, line)
	}
	return out
}

func recoveryActiveTasks(tasks []channelTask, limit int) []channelTask {
	filtered := make([]channelTask, 0, len(tasks))
	for _, task := range tasks {
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "", "done", "completed", "canceled", "cancelled":
			continue
		default:
			filtered = append(filtered, task)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, lok := parseChannelTime(filtered[i].UpdatedAt)
		right, rok := parseChannelTime(filtered[j].UpdatedAt)
		switch {
		case lok && rok:
			return left.After(right)
		case lok:
			return true
		case rok:
			return false
		default:
			return filtered[i].ID > filtered[j].ID
		}
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func recoveryRecentThreads(messages []brokerMessage, limit int) []brokerMessage {
	roots := []brokerMessage{}
	seen := map[string]bool{}
	for i := len(messages) - 1; i >= 0 && len(roots) < limit; i-- {
		msg := messages[i]
		rootID := threadRootMessageID(messages, msg.ID)
		if rootID == "" || seen[rootID] {
			continue
		}
		if !hasThreadReplies(messages, rootID) && strings.TrimSpace(msg.ReplyTo) == "" {
			continue
		}
		root, ok := findMessageByID(messages, rootID)
		if !ok {
			continue
		}
		roots = append(roots, root)
		seen[rootID] = true
	}
	return roots
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
