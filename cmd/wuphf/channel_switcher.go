package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

func (m channelModel) buildWorkspaceSwitcherOptions() []tui.PickerOption {
	options := []tui.PickerOption{}
	if m.isOneOnOne() {
		options = append(options, tui.PickerOption{
			Label:       "Back to office",
			Value:       "mode:office",
			Description: m.officeFeedDescription(),
		})
	} else {
		options = append(options,
			tui.PickerOption{Label: "Office feed", Value: "app:messages", Description: m.officeFeedDescription()},
			tui.PickerOption{Label: "Recovery", Value: "app:recovery", Description: m.recoverySwitcherDescription()},
			tui.PickerOption{Label: "Tasks", Value: "app:tasks", Description: "Active work in #" + m.activeChannel},
			tui.PickerOption{Label: "Requests", Value: "app:requests", Description: "Human decisions and interviews"},
			tui.PickerOption{Label: "Policies", Value: "app:policies", Description: "Signals, decisions, and watchdogs"},
			tui.PickerOption{Label: "Calendar", Value: "app:calendar", Description: "Scheduled work and follow-ups"},
			tui.PickerOption{Label: "Artifacts", Value: "app:artifacts", Description: "Task logs, workflow runs, and approvals"},
			tui.PickerOption{Label: "Skills", Value: "app:skills", Description: "Reusable skills and workflows"},
		)
		for _, ch := range m.channels {
			if strings.TrimSpace(ch.Slug) == "" {
				continue
			}
			options = append(options, tui.PickerOption{
				Label:       "#" + ch.Slug,
				Value:       "channel:" + ch.Slug,
				Description: fallbackChannelDescription(ch),
			})
		}
	}

	for _, member := range mergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo()) {
		if member.Slug == "you" || strings.TrimSpace(member.Slug) == "" {
			continue
		}
		description := "Direct session with @" + member.Slug
		summary := deriveMemberRuntimeSummary(member, m.tasks, time.Now())
		if strings.TrimSpace(summary.Detail) != "" {
			description = summary.Detail
		} else if strings.TrimSpace(member.LastMessage) != "" {
			description = summarizeSentence(member.LastMessage)
		}
		options = append(options, tui.PickerOption{
			Label:       "1:1 with " + member.Name,
			Value:       "dm:" + member.Slug,
			Description: description,
		})
	}

	for _, req := range m.switcherPendingRequests(3) {
		label := "Request " + req.ID + " · " + truncateText(req.TitleOrQuestion(), 40)
		description := strings.TrimSpace(strings.Join([]string{
			strings.ReplaceAll(strings.TrimSpace(req.Kind), "_", " "),
			"@" + fallbackString(req.From, "unknown"),
			switcherTiming(req.CreatedAt, req.DueAt),
		}, " · "))
		if req.Blocking || req.Required {
			description = "Needs you now · " + description
		}
		options = append(options, tui.PickerOption{
			Label:       label,
			Value:       "request:" + req.ID,
			Description: strings.Trim(description, " ·"),
		})
	}

	for _, task := range m.switcherActiveTasks(4) {
		descriptionParts := []string{
			strings.ReplaceAll(strings.TrimSpace(task.Status), "_", " "),
			"@" + fallbackString(task.Owner, "unowned"),
			switcherTiming(task.UpdatedAt, task.DueAt),
		}
		if strings.TrimSpace(task.WorktreePath) != "" {
			descriptionParts = append(descriptionParts, "worktree")
		}
		if strings.TrimSpace(task.ThreadID) != "" {
			descriptionParts = append(descriptionParts, "thread "+task.ThreadID)
		}
		options = append(options, tui.PickerOption{
			Label:       "Task " + task.ID + " · " + truncateText(task.Title, 44),
			Value:       "task:" + task.ID,
			Description: strings.Trim(strings.Join(descriptionParts, " · "), " ·"),
		})
	}

	for _, msg := range m.switcherRecentThreads(3) {
		options = append(options, tui.PickerOption{
			Label:       "Thread " + msg.ID + " · @" + fallbackString(msg.From, "unknown"),
			Value:       "thread:" + msg.ID,
			Description: truncateText(strings.TrimSpace(msg.Content), 72),
		})
	}

	if m.threadPanelOpen && strings.TrimSpace(m.threadPanelID) != "" {
		options = append(options, tui.PickerOption{
			Label:       "Current thread " + m.threadPanelID,
			Value:       "thread:" + m.threadPanelID,
			Description: "Jump back into the active thread panel",
		})
	}
	return options
}

func fallbackChannelDescription(ch channelInfo) string {
	if strings.TrimSpace(ch.Description) != "" {
		return ch.Description
	}
	if len(ch.Members) > 0 {
		return fmt.Sprintf("%d member%s", len(ch.Members), pluralSuffix(len(ch.Members)))
	}
	return "Shared office channel"
}

func (m *channelModel) applyWorkspaceSwitcherSelection(value string) tea.Cmd {
	switch {
	case value == "mode:office":
		m.confirm = confirmationForSessionSwitch(team.SessionModeOffice, team.DefaultOneOnOneAgent)
		m.notice = "Confirm returning to the full office."
		return nil
	case strings.HasPrefix(value, "dm:"):
		agent := strings.TrimSpace(strings.TrimPrefix(value, "dm:"))
		if agent == "" {
			agent = team.DefaultOneOnOneAgent
		}
		if m.isOneOnOne() && team.NormalizeOneOnOneAgent(m.oneOnOneAgent) == team.NormalizeOneOnOneAgent(agent) {
			m.notice = "Already viewing that direct session."
			return nil
		}
		m.confirm = confirmationForSessionSwitch(team.SessionModeOneOnOne, agent)
		m.notice = "Confirm the direct session switch."
		return nil
	case strings.HasPrefix(value, "channel:"):
		channel := normalizeWorkspaceChannel(strings.TrimPrefix(value, "channel:"))
		if channel == "" {
			return nil
		}
		m.activeChannel = channel
		m.activeApp = officeAppMessages
		m.messages = nil
		m.members = nil
		m.requests = nil
		m.tasks = nil
		m.lastID = ""
		m.replyToID = ""
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.scroll = 0
		m.unreadCount = 0
		m.syncSidebarCursorToActive()
		m.notice = "Switched to #" + channel
		return tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel))
	case strings.HasPrefix(value, "app:"):
		app := officeApp(strings.TrimSpace(strings.TrimPrefix(value, "app:")))
		m.activeApp = app
		m.syncSidebarCursorToActive()
		m.notice = "Viewing " + strings.Title(string(app)) + "."
		switch app {
		case officeAppRecovery:
			return m.pollCurrentState()
		case officeAppTasks:
			return pollTasks(m.activeChannel)
		case officeAppRequests:
			return pollRequests(m.activeChannel)
		case officeAppPolicies:
			return pollOfficeLedger()
		case officeAppCalendar:
			return tea.Batch(pollTasks(m.activeChannel), pollRequests(m.activeChannel), pollOfficeLedger())
		case officeAppArtifacts:
			return m.pollCurrentState()
		default:
			return nil
		}
	case strings.HasPrefix(value, "thread:"):
		threadID := strings.TrimSpace(strings.TrimPrefix(value, "thread:"))
		if threadID == "" {
			return nil
		}
		m.threadPanelOpen = true
		m.threadPanelID = threadID
		m.replyToID = threadID
		m.focus = focusThread
		m.notice = "Replying in thread " + threadID
		return nil
	case strings.HasPrefix(value, "task:"), strings.HasPrefix(value, "request:"):
		return m.applySearchSelection(value, value)
	default:
		return nil
	}
}

func (m channelModel) officeFeedDescription() string {
	if summary := strings.TrimSpace(m.currentAwaySummary()); summary != "" {
		return summary
	}
	if req, ok := selectNeedsYouRequest(m.requests); ok {
		return "Needs you: " + truncateText(req.TitleOrQuestion(), 64)
	}
	return "Main office feed"
}

func (m channelModel) recoverySwitcherDescription() string {
	recovery := m.currentRuntimeSnapshot().Recovery
	if focus := trimRecoverySentence(recovery.Focus); focus != "" {
		return truncateText(focus, 72)
	}
	if len(recovery.NextSteps) > 0 {
		return truncateText("Next: "+recovery.NextSteps[0], 72)
	}
	return "Resume work with focus, changes, and next steps"
}

func (m channelModel) switcherPendingRequests(limit int) []channelInterview {
	requests := recentHumanArtifactRequests(m.requests, 0)
	filtered := make([]channelInterview, 0, len(requests))
	for _, req := range requests {
		if !isOpenInterviewStatus(req.Status) {
			continue
		}
		filtered = append(filtered, req)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func (m channelModel) switcherActiveTasks(limit int) []channelTask {
	filtered := make([]channelTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		switch status {
		case "", "done", "completed", "canceled", "cancelled":
			continue
		default:
			filtered = append(filtered, task)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		leftRank, rightRank := taskSwitcherRank(filtered[i]), taskSwitcherRank(filtered[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftTime, lok := parseChannelTime(fallbackString(filtered[i].UpdatedAt, filtered[i].CreatedAt))
		rightTime, rok := parseChannelTime(fallbackString(filtered[j].UpdatedAt, filtered[j].CreatedAt))
		switch {
		case lok && rok:
			if !leftTime.Equal(rightTime) {
				return leftTime.After(rightTime)
			}
		case lok:
			return true
		case rok:
			return false
		}
		return filtered[i].ID > filtered[j].ID
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func taskSwitcherRank(task channelTask) int {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	switch status {
	case "blocked":
		return 0
	case "review":
		return 1
	case "in_progress":
		return 2
	case "claimed", "pending", "open":
		return 3
	default:
		return 4
	}
}

func (m channelModel) switcherRecentThreads(limit int) []brokerMessage {
	roots := make([]brokerMessage, 0, limit)
	seen := map[string]bool{}
	for _, msg := range m.recentRootMessages(24) {
		rootID := threadRootMessageID(m.messages, msg.ID)
		if rootID == "" || seen[rootID] {
			continue
		}
		if !hasThreadReplies(m.messages, rootID) && strings.TrimSpace(msg.ReplyTo) == "" {
			continue
		}
		root, ok := findMessageByID(m.messages, rootID)
		if !ok {
			continue
		}
		roots = append(roots, root)
		seen[rootID] = true
		if limit > 0 && len(roots) >= limit {
			break
		}
	}
	return roots
}

func switcherTiming(createdAt, dueAt string) string {
	if due := strings.TrimSpace(dueAt); due != "" {
		return "due " + prettyRelativeTime(due)
	}
	if created := strings.TrimSpace(createdAt); created != "" {
		return prettyRelativeTime(created)
	}
	return ""
}

func normalizeWorkspaceChannel(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return ""
	}
	slug = strings.TrimPrefix(slug, "#")
	return strings.ReplaceAll(slug, " ", "-")
}
