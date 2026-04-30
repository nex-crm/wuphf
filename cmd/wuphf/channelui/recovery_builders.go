package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

// BuildRecoveryLines renders the full recovery view: a "while away"
// card (when there is anything new to summarize), the runtime
// status card, the readiness card, the next-step + highlights
// strips, and the action / surgery rows. Returns the offline
// preview message when the broker is detached and the runtime is
// empty.
func BuildRecoveryLines(workspace WorkspaceUIState, contentWidth int, tasks []Task, requests []Interview, messages []BrokerMessage) []RenderedLine {
	snapshot := workspace.Runtime
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted))
	lines := []RenderedLine{{Text: RenderDateSeparator(contentWidth, "Recovery")}}

	if !workspace.BrokerConnected && len(snapshot.Tasks) == 0 && len(snapshot.Requests) == 0 && len(snapshot.Recent) == 0 {
		lines = append(lines,
			RenderedLine{Text: ""},
			RenderedLine{Text: muted.Render("  Offline preview. Launch WUPHF to hydrate the runtime state and recovery summary.")},
			RenderedLine{Text: muted.Render("  The recovery view will highlight focus, next steps, and recent changes once the office is live.")},
		)
		return lines
	}

	if workspace.UnreadCount > 0 || strings.TrimSpace(workspace.AwaySummary) != "" {
		title := SubtlePill("while away", "#F8FAFC", "#1D4ED8") + " " + lipgloss.NewStyle().Bold(true).Render("What changed while you were gone")
		body := strings.TrimSpace(workspace.AwaySummary)
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
		for _, line := range RenderRuntimeEventCard(contentWidth, title, body, "#2563EB", extra) {
			lines = append(lines, RenderedLine{Text: "  " + line})
		}
	}

	stateBody := fmt.Sprintf("%d running tasks · %d open requests · %d isolated worktrees", CountRunningRuntimeTasks(snapshot.Tasks), len(snapshot.Requests), CountIsolatedRuntimeTasks(snapshot.Tasks))
	stateExtra := []string{}
	if snapshot.SessionMode == team.SessionModeOneOnOne && strings.TrimSpace(snapshot.DirectAgent) != "" {
		stateExtra = append(stateExtra, "Direct session with @"+snapshot.DirectAgent)
	} else if strings.TrimSpace(snapshot.Channel) != "" {
		stateExtra = append(stateExtra, "Channel: #"+snapshot.Channel)
	}
	if focus := strings.TrimSpace(snapshot.Recovery.Focus); focus != "" {
		stateExtra = append(stateExtra, "Current focus: "+focus)
	}
	for _, line := range RenderRuntimeEventCard(contentWidth, SubtlePill("runtime", "#E2E8F0", "#334155")+" "+lipgloss.NewStyle().Bold(true).Render("Current state"), stateBody, "#475569", stateExtra) {
		lines = append(lines, RenderedLine{Text: "  " + line})
	}

	readinessTitle, readinessBody, readinessAccent, readinessExtra := workspace.ReadinessCard()
	for _, line := range RenderRuntimeEventCard(contentWidth, readinessTitle, readinessBody, readinessAccent, readinessExtra) {
		lines = append(lines, RenderedLine{Text: "  " + line})
	}

	if len(snapshot.Recovery.NextSteps) > 0 {
		body := snapshot.Recovery.NextSteps[0]
		extra := append([]string(nil), snapshot.Recovery.NextSteps[1:]...)
		for _, line := range RenderRuntimeEventCard(contentWidth, SubtlePill("next", "#F8FAFC", "#92400E")+" "+lipgloss.NewStyle().Bold(true).Render("What to do next"), body, "#B45309", extra) {
			lines = append(lines, RenderedLine{Text: "  " + line})
		}
	}

	if len(snapshot.Recovery.Highlights) > 0 {
		body := snapshot.Recovery.Highlights[0]
		extra := append([]string(nil), snapshot.Recovery.Highlights[1:]...)
		for _, line := range RenderRuntimeEventCard(contentWidth, SubtlePill("recent", "#E5E7EB", "#334155")+" "+lipgloss.NewStyle().Bold(true).Render("Latest highlights"), body, "#334155", extra) {
			lines = append(lines, RenderedLine{Text: "  " + line})
		}
	}

	if actionLines := BuildRecoveryActionLines(contentWidth, tasks, requests, messages); len(actionLines) > 0 {
		lines = append(lines, actionLines...)
	}
	if surgeryLines := BuildRecoverySurgeryLines(contentWidth, tasks, requests, messages); len(surgeryLines) > 0 {
		lines = append(lines, surgeryLines...)
	}

	return lines
}

// BuildRecoveryActionLines renders the "Resume human decisions",
// "Resume active tasks", and "Return to recent threads" sections
// of the recovery view. Each section is gated on whether there is
// material to surface — empty inputs produce empty output. Cards
// are wired with click-target metadata via RenderedCardLines so
// the recovery view can route actions back to the correct picker.
func BuildRecoveryActionLines(contentWidth int, tasks []Task, requests []Interview, messages []BrokerMessage) []RenderedLine {
	lines := []RenderedLine{}

	if req, ok := SelectNeedsYouRequest(requests); ok {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Resume human decisions")})
		header := AccentPill("needs you", "#B45309") + " " + lipgloss.NewStyle().Bold(true).Render(req.TitleOrQuestion())
		body := strings.TrimSpace(req.Context)
		if body == "" {
			body = strings.TrimSpace(req.Question)
		}
		extra := []string{"Asked by @" + FallbackString(req.From, "unknown")}
		if strings.TrimSpace(req.RecommendedID) != "" {
			extra = append(extra, "Recommended: "+req.RecommendedID)
		}
		extra = append(extra, "Open request")
		lines = append(lines, PrefixedCardLines(RenderedCardLines(RenderRecoveryActionCard(contentWidth, header, body, "#B45309", extra), "", req.ID, strings.TrimSpace(req.ReplyTo), ""), "  ")...)
	}

	if activeTasks := RecoveryActiveTasks(tasks, 3); len(activeTasks) > 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Resume active tasks")})
		for _, task := range activeTasks {
			header := TaskStatusPill(task.Status) + " " + lipgloss.NewStyle().Bold(true).Render(task.Title)
			body := strings.TrimSpace(task.Details)
			if body == "" {
				body = "Owner @" + FallbackString(task.Owner, "unowned")
			}
			extra := []string{"Owner @" + FallbackString(task.Owner, "unowned")}
			if strings.TrimSpace(task.ThreadID) != "" {
				extra = append(extra, "Thread "+task.ThreadID)
			}
			if strings.TrimSpace(task.WorktreePath) != "" {
				extra = append(extra, "Worktree "+task.WorktreePath)
			}
			extra = append(extra, "Open task")
			threadID := strings.TrimSpace(task.ThreadID)
			lines = append(lines, PrefixedCardLines(RenderedCardLines(RenderRecoveryActionCard(contentWidth, header, body, "#2563EB", extra), task.ID, "", threadID, ""), "  ")...)
		}
	}

	if recent := RecoveryRecentThreads(messages, 3); len(recent) > 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Return to recent threads")})
		for _, msg := range recent {
			header := SubtlePill("@"+FallbackString(msg.From, "unknown"), "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render("Thread "+msg.ID)
			body := TruncateText(strings.TrimSpace(msg.Content), 160)
			extra := []string{}
			if when := strings.TrimSpace(msg.Timestamp); when != "" {
				extra = append(extra, PrettyRelativeTime(when))
			}
			extra = append(extra, "Open thread")
			lines = append(lines, PrefixedCardLines(RenderedCardLines(RenderRecoveryActionCard(contentWidth, header, body, "#475569", extra), "", "", msg.ID, ""), "  ")...)
		}
	}

	return lines
}

// BuildRecoverySurgeryLines renders the "Transcript surgery"
// section — one card per recovery surgery option, each wired to a
// composer-prefill prompt via RenderedCardLinesWithPrompt. Returns
// an empty slice when there are no options to render.
func BuildRecoverySurgeryLines(contentWidth int, tasks []Task, requests []Interview, messages []BrokerMessage) []RenderedLine {
	lines := []RenderedLine{}
	options := BuildRecoverySurgeryOptions(tasks, requests, messages)
	if len(options) == 0 {
		return lines
	}

	lines = append(lines, RenderedLine{Text: ""})
	lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Transcript surgery")})
	for _, option := range options {
		header := SubtlePill(option.Tag, "#E0F2FE", "#075985") + " " + lipgloss.NewStyle().Bold(true).Render(option.Title)
		extra := append([]string(nil), option.Extra...)
		extra = append(extra, "Click to draft this recap in the composer")
		card := RenderRecoveryActionCard(contentWidth, header, option.Body, option.Accent, extra)
		lines = append(lines, PrefixedCardLines(RenderedCardLinesWithPrompt(card, "", "", "", "", option.Prompt), "  ")...)
	}

	return lines
}
