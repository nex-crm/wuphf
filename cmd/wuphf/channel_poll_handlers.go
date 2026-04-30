package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
)

// Polling response handlers — Update() switch bodies for each
// channelXxxMsg / channelTickMsg type. Update is the router; each
// handler here owns one msg-family's full case body. Returning
// (channelModel, tea.Cmd) instead of mutating-in-switch makes each
// case independently callable + testable.
//
// Pattern: every handler takes the model by value, optionally
// updates fields, and returns (m, cmd). Cmd is nil when the case
// is purely state-update (the common shape). Cases that fan out
// follow-up polls (e.g. channelChannelsMsg auto-pivot when the
// active channel disappears) return the appropriate tea.Cmd.

func (m channelModel) handleChannelMsg(msg channelMsg) (channelModel, tea.Cmd) {
	if len(msg.messages) > 0 {
		hadHistory := m.lastID != ""
		uniqueMessages, added := channelui.AppendUniqueMessages(m.messages, msg.messages)
		if added == 0 {
			return m, nil
		}
		addedMessages := uniqueMessages[len(m.messages):]
		latestHumanFacing := channelui.LatestHumanFacingMessage(addedMessages)
		if m.scroll > 0 {
			m.scroll += added
		}
		m.messages = uniqueMessages
		m.lastID = msg.messages[len(msg.messages)-1].ID
		// Track latest streaming text per agent for sidebar display.
		if m.lastAgentContent == nil {
			m.lastAgentContent = make(map[string]string)
		}
		for _, bm := range addedMessages {
			if bm.From != "" && bm.From != "you" && bm.From != "human" && bm.Content != "" {
				snippet := strings.TrimSpace(bm.Content)
				if len([]rune(snippet)) > 38 {
					runes := []rune(snippet)
					snippet = "…" + string(runes[len(runes)-37:])
				}
				m.lastAgentContent[bm.From] = snippet
			}
		}
		if m.scroll > 0 || m.focus != focusMain || m.threadPanelOpen {
			m.noteIncomingMessages(addedMessages)
		} else {
			m.clearUnreadState()
		}
		if latestHumanFacing != nil && hadHistory {
			m.activeApp = channelui.OfficeAppMessages
			m.notice = fmt.Sprintf("@%s has something for you", latestHumanFacing.From)
		}
	}
	return m, nil
}

func (m channelModel) handleChannelMembersMsg(msg channelMembersMsg) (channelModel, tea.Cmd) {
	m.members = msg.members
	// Overlay last-seen streaming content into LiveActivity when the broker
	// hasn't set it yet (e.g. between polls or when liveActivity is stale).
	if m.lastAgentContent != nil {
		for i, mem := range m.members {
			if snippet, ok := m.lastAgentContent[mem.Slug]; ok && snippet != "" && mem.LiveActivity == "" {
				m.members[i].LiveActivity = snippet
			}
		}
	}
	m.updateOverlaysForCurrentInput()
	return m, nil
}

func (m channelModel) handleChannelOfficeMembersMsg(msg channelOfficeMembersMsg) (channelModel, tea.Cmd) {
	if len(msg.members) == 0 {
		msg.members = channelui.OfficeMembersFallback(m.officeMembers)
	}
	m.officeMembers = msg.members
	channelui.SetOfficeDirectory(msg.members)
	m.updateOverlaysForCurrentInput()
	return m, nil
}

func (m channelModel) handleChannelChannelsMsg(msg channelChannelsMsg) (channelModel, tea.Cmd) {
	if len(msg.channels) == 0 {
		msg.channels = channelui.ChannelInfosFallback(m.channels)
	}
	m.channels = msg.channels
	m.clampSidebarCursor()
	if m.activeChannel == "" {
		m.activeChannel = "general"
	}
	if !channelui.ChannelExists(msg.channels, m.activeChannel) && len(msg.channels) > 0 {
		m.activeChannel = msg.channels[0].Slug
		m.lastID = ""
		return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel))
	}
	return m, nil
}

func (m channelModel) handleChannelUsageMsg(msg channelUsageMsg) (channelModel, tea.Cmd) {
	m.usage = msg.usage
	if m.usage.Agents == nil {
		m.usage.Agents = make(map[string]channelui.UsageTotals)
	}
	return m, nil
}

func (m channelModel) handleChannelHealthMsg(msg channelHealthMsg) (channelModel, tea.Cmd) {
	m.brokerConnected = msg.Connected
	if !msg.Connected {
		return m, nil
	}
	nextMode := team.NormalizeSessionMode(msg.SessionMode)
	nextAgent := team.NormalizeOneOnOneAgent(msg.OneOnOneAgent)
	modeChanged := nextMode != m.sessionMode || nextAgent != m.oneOnOneAgent
	m.sessionMode = nextMode
	m.oneOnOneAgent = nextAgent
	if m.isOneOnOne() {
		m.activeApp = channelui.OfficeAppMessages
		m.sidebarCollapsed = true
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.replyToID = ""
	}
	if modeChanged {
		m.messages = nil
		m.members = nil
		m.tasks = nil
		m.requests = nil
		m.lastID = ""
		m.scroll = 0
		m.clearUnreadState()
		m.refreshSlashCommands()
		if m.isOneOnOne() && strings.TrimSpace(m.notice) == "" {
			m.notice = "Conference room reserved. Direct session reset. Agent pane reloaded in place. No Toby."
		}
		return m, m.pollCurrentState()
	}
	return m, nil
}

func (m channelModel) handleChannelTasksMsg(msg channelTasksMsg) (channelModel, tea.Cmd) {
	m.tasks = msg.tasks
	return m, nil
}

func (m channelModel) handleChannelSkillsMsg(msg channelSkillsMsg) (channelModel, tea.Cmd) {
	m.skills = msg.skills
	m.refreshSlashCommands()
	return m, nil
}

func (m channelModel) handleChannelActionsMsg(msg channelActionsMsg) (channelModel, tea.Cmd) {
	m.actions = msg.actions
	return m, nil
}

func (m channelModel) handleChannelSignalsMsg(msg channelSignalsMsg) (channelModel, tea.Cmd) {
	m.signals = msg.signals
	return m, nil
}

func (m channelModel) handleChannelDecisionsMsg(msg channelDecisionsMsg) (channelModel, tea.Cmd) {
	m.decisions = msg.decisions
	return m, nil
}

func (m channelModel) handleChannelWatchdogsMsg(msg channelWatchdogsMsg) (channelModel, tea.Cmd) {
	m.watchdogs = msg.alerts
	return m, nil
}

func (m channelModel) handleChannelSchedulerMsg(msg channelSchedulerMsg) (channelModel, tea.Cmd) {
	m.scheduler = msg.jobs
	return m, nil
}

func (m channelModel) handleChannelRequestsMsg(msg channelRequestsMsg) (channelModel, tea.Cmd) {
	prevID := ""
	if m.pending != nil {
		prevID = m.pending.ID
	}
	m.requests = msg.requests
	m.pending = msg.pending
	if m.pending != nil && m.pending.ID != prevID {
		m.selectedOption = m.recommendedOptionIndex()
		m.input = nil
		m.inputPos = 0
		if m.pending.Blocking || m.pending.Required {
			m.activeApp = channelui.OfficeAppMessages
			m.syncSidebarCursorToActive()
			m.notice = "Human decision needed. Team is paused until you answer."
			if m.pending.ReplyTo != "" {
				m.threadPanelOpen = true
				m.threadPanelID = m.pending.ReplyTo
			}
		}
	}
	return m, nil
}

func (m channelModel) handleChannelTickMsg(_ channelTickMsg) (channelModel, tea.Cmd) {
	m.tickFrame++
	if m.notice != "" && !m.noticeExpireAt.IsZero() && time.Now().After(m.noticeExpireAt) {
		m.notice = ""
		m.noticeExpireAt = time.Time{}
	}
	return m, m.pollCurrentState()
}
