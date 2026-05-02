package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Header rendering queries: currentHeaderTitle / currentHeaderMeta /
// currentAppLabel produce the strings the channel header strip
// renders. currentMainLines is the cached main-viewport line getter.
//
// Pure read-only views over channelModel state. No mutation.

func (m channelModel) currentHeaderTitle() string {
	if m.isOneOnOne() && m.activeApp != channelui.OfficeAppRecovery && m.activeApp != channelui.OfficeAppInbox && m.activeApp != channelui.OfficeAppOutbox {
		return "1:1 with " + m.oneOnOneAgentName()
	}
	switch m.activeApp {
	case channelui.OfficeAppRecovery:
		if m.isOneOnOne() {
			return "1:1 with " + m.oneOnOneAgentName() + " · Recovery"
		}
		return "# " + m.activeChannel + " · Recovery"
	case channelui.OfficeAppInbox:
		if m.isOneOnOne() {
			return "1:1 with " + m.oneOnOneAgentName() + " · Inbox"
		}
		return "# " + m.activeChannel + " · Inbox"
	case channelui.OfficeAppOutbox:
		if m.isOneOnOne() {
			return "1:1 with " + m.oneOnOneAgentName() + " · Outbox"
		}
		return "# " + m.activeChannel + " · Outbox"
	case channelui.OfficeAppArtifacts:
		return "# " + m.activeChannel + " · Artifacts"
	case channelui.OfficeAppTasks:
		return "# " + m.activeChannel + " · Tasks"
	case channelui.OfficeAppRequests:
		return "# " + m.activeChannel + " · Requests"
	case channelui.OfficeAppPolicies:
		return "# " + m.activeChannel + " · Insights"
	case channelui.OfficeAppCalendar:
		return "# " + m.activeChannel + " · Calendar"
	case channelui.OfficeAppSkills:
		return "# " + m.activeChannel + " · Skills"
	default:
		return "# " + m.activeChannel
	}
}

func (m channelModel) currentHeaderMeta() string {
	workspace := m.currentWorkspaceUIState()
	if m.activeApp == channelui.OfficeAppRecovery {
		snapshot := workspace.Runtime
		if m.isOneOnOne() {
			return fmt.Sprintf("  Re-entry summary for %s · %d running tasks · %d open requests · %d new since you looked", m.oneOnOneAgentName(), workspace.RunningTasks, workspace.OpenRequests, workspace.UnreadCount)
		}
		parts := []string{
			fmt.Sprintf("Re-entry summary for #%s", channelui.FallbackString(snapshot.Channel, m.activeChannel)),
			fmt.Sprintf("%d blocking requests", workspace.BlockingCount),
			fmt.Sprintf("%d running tasks", workspace.RunningTasks),
			fmt.Sprintf("%d new since you looked", workspace.UnreadCount),
		}
		if workspace.Readiness.Level != channelui.WorkspaceReadinessReady && strings.TrimSpace(workspace.Readiness.Headline) != "" {
			parts = append(parts, strings.ToLower(workspace.Readiness.Headline))
		}
		return "  " + strings.Join(parts, " · ")
	}
	if m.isOneOnOne() && (m.activeApp == channelui.OfficeAppInbox || m.activeApp == channelui.OfficeAppOutbox) {
		scopeLabel := "inbox"
		if m.activeApp == channelui.OfficeAppOutbox {
			scopeLabel = "outbox"
		}
		scopeCount := len(channelui.FilterMessagesForViewerScope(m.messages, m.oneOnOneAgentSlug(), scopeLabel))
		parts := []string{
			fmt.Sprintf("%s lane for %s", titleCaser.String(scopeLabel), m.oneOnOneAgentName()),
			fmt.Sprintf("%d visible messages", scopeCount),
		}
		if workspace.RunningTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d running tasks", workspace.RunningTasks))
		}
		if strings.TrimSpace(workspace.Focus) != "" {
			parts = append(parts, "focus: "+workspace.Focus)
		}
		return "  " + strings.Join(parts, " · ")
	}
	if m.isOneOnOne() {
		return workspace.HeaderMeta()
	}
	switch m.activeApp {
	case channelui.OfficeAppInbox:
		return fmt.Sprintf("  Inbox lane · %d visible messages · %d open requests", len(m.messages), len(m.requests))
	case channelui.OfficeAppOutbox:
		return fmt.Sprintf("  Outbox lane · %d visible messages · %d recent actions", len(m.messages), len(m.actions))
	case channelui.OfficeAppTasks:
		open, inProgress, review, blocked, overdue := 0, 0, 0, 0, 0
		for _, task := range m.tasks {
			switch task.Status {
			case "in_progress":
				inProgress++
			case "review":
				review++
			case "blocked":
				blocked++
			default:
				open++
			}
			if parsed, ok := channelui.ParseChannelTime(task.DueAt); ok && parsed.Before(time.Now()) && task.Status != "done" {
				overdue++
			}
		}
		return fmt.Sprintf("  Clear ownership, no duplicate work · %d open · %d moving · %d in review · %d blocked · %d overdue", open, inProgress, review, blocked, overdue)
	case channelui.OfficeAppRequests:
		blocking, urgent := 0, 0
		for _, req := range m.requests {
			if req.Blocking {
				blocking++
			}
			if parsed, ok := channelui.ParseChannelTime(req.DueAt); ok && parsed.Before(time.Now().Add(2*time.Hour)) {
				urgent++
			}
		}
		return fmt.Sprintf("  Decisions and approvals the team is waiting on · %d open · %d blocking · %d soon", len(m.requests), blocking, urgent)
	case channelui.OfficeAppPolicies:
		highSignal := 0
		for _, signal := range m.signals {
			if signal.Urgency == "high" || signal.Blocking || signal.RequiresHuman {
				highSignal++
			}
		}
		activeWatchdogs := 0
		for _, alert := range m.watchdogs {
			if strings.TrimSpace(alert.Status) != "resolved" {
				activeWatchdogs++
			}
		}
		external := 0
		for _, action := range m.actions {
			if strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
				external++
			}
		}
		return fmt.Sprintf("  Signals, Decisions, External Actions, and Watchdogs driving the office · %d signals · %d decisions · %d external · %d active watchdogs · %d high signal", len(m.signals), len(m.decisions), external, activeWatchdogs, highSignal)
	case channelui.OfficeAppCalendar:
		events := channelui.FilterCalendarEvents(channelui.CollectCalendarEvents(m.scheduler, m.tasks, m.requests, m.activeChannel, m.members), m.calendarRange, m.calendarFilter)
		dueSoon := 0
		now := time.Now()
		for _, event := range events {
			if !event.When.After(now.Add(15 * time.Minute)) {
				dueSoon++
			}
		}
		view := "week"
		if m.calendarRange == channelui.CalendarRangeDay {
			view = "day"
		}
		filter := "everyone"
		if strings.TrimSpace(m.calendarFilter) != "" {
			filter = channelui.DisplayName(m.calendarFilter)
		}
		scheduledWorkflows := 0
		for _, job := range m.scheduler {
			if strings.TrimSpace(job.Kind) == "one_workflow" {
				scheduledWorkflows++
			}
		}
		return fmt.Sprintf("  %s view · %s · %d upcoming · %d due soon · %d scheduled workflows · %d recent actions", view, filter, len(events), dueSoon, scheduledWorkflows, len(m.actions))
	case channelui.OfficeAppSkills:
		active := 0
		workflowBacked := 0
		for _, skill := range m.skills {
			if skill.Status == "" || skill.Status == "active" {
				active++
			}
			if strings.TrimSpace(skill.WorkflowKey) != "" {
				workflowBacked++
			}
		}
		return fmt.Sprintf("  Reusable team skills · %d total · %d active · %d workflow-backed", len(m.skills), active, workflowBacked)
	case channelui.OfficeAppArtifacts:
		summary := m.currentArtifactSummary()
		if summary == "" {
			return "  Retained task logs, approvals, and workflow history for this office"
		}
		return "  " + summary
	default:
		return workspace.HeaderMeta()
	}
}

func (m channelModel) currentAppLabel() string {
	if m.isOneOnOne() && m.activeApp != channelui.OfficeAppRecovery && m.activeApp != channelui.OfficeAppInbox && m.activeApp != channelui.OfficeAppOutbox {
		return "messages"
	}
	switch m.activeApp {
	case channelui.OfficeAppRecovery:
		return "recovery"
	case channelui.OfficeAppInbox:
		return "inbox"
	case channelui.OfficeAppOutbox:
		return "outbox"
	case channelui.OfficeAppTasks:
		return "tasks"
	case channelui.OfficeAppRequests:
		return "requests"
	case channelui.OfficeAppPolicies:
		return "policies"
	case channelui.OfficeAppCalendar:
		return "calendar"
	case channelui.OfficeAppArtifacts:
		return "artifacts"
	case channelui.OfficeAppSkills:
		return "skills"
	default:
		return "messages"
	}
}

func (m channelModel) currentMainLines(contentWidth int) []channelui.RenderedLine {
	return m.cachedMainLines(contentWidth)
}
