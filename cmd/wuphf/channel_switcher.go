package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/tui"
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) buildWorkspaceSwitcherOptions() []tui.PickerOption {
	options := []tui.PickerOption{}
	if m.isOneOnOne() {
		options = append(options, tui.PickerOption{
			Label:       "Back to office",
			Value:       "mode:office",
			Description: "Return to the full office feed and roster",
		})
	} else {
		options = append(options,
			tui.PickerOption{Label: "Office feed", Value: "app:messages", Description: "Main channel feed"},
			tui.PickerOption{Label: "Recovery", Value: "app:recovery", Description: "Resume work with focus, changes, and next steps"},
			tui.PickerOption{Label: "Tasks", Value: "app:tasks", Description: "Active work in #" + m.activeChannel},
			tui.PickerOption{Label: "Requests", Value: "app:requests", Description: "Human decisions and interviews"},
			tui.PickerOption{Label: "Policies", Value: "app:policies", Description: "Signals, decisions, and watchdogs"},
			tui.PickerOption{Label: "Calendar", Value: "app:calendar", Description: "Scheduled work and follow-ups"},
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
		options = append(options, tui.PickerOption{
			Label:       "1:1 with " + member.Name,
			Value:       "dm:" + member.Slug,
			Description: "Direct session with @" + member.Slug,
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
	default:
		return nil
	}
}

func normalizeWorkspaceChannel(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return ""
	}
	slug = strings.TrimPrefix(slug, "#")
	return strings.ReplaceAll(slug, " ", "-")
}
