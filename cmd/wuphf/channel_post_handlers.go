package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Done-message handlers — Update() switch bodies for the
// channelXxxDoneMsg family. Each one is the response side of a POST
// the TUI fired through channel_broker.go (postToChannel,
// resetTeamSession, switchSessionMode, etc.).
//
// Pattern: clear m.posting, surface the result via m.notice, fan out
// follow-up polls so the next render reflects the new state.

func (m channelModel) handleChannelPostDoneMsg(msg channelPostDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Send failed: " + msg.err.Error()
	} else if strings.TrimSpace(msg.notice) != "" {
		m.notice = msg.notice
	} else if m.replyToID != "" {
		m.notice = fmt.Sprintf("Reply sent to %s. Use /cancel to leave the thread.", m.replyToID)
	}
	switch strings.TrimSpace(msg.action) {
	case "create":
		if slug := channelui.NormalizeSidebarSlug(msg.slug); slug != "" {
			m.activeChannel = slug
			m.activeApp = channelui.OfficeAppMessages
			m.messages = nil
			m.members = nil
			m.tasks = nil
			m.requests = nil
			m.lastID = ""
			m.replyToID = ""
			m.threadPanelOpen = false
			m.threadPanelID = ""
			m.scroll = 0
			m.clearUnreadState()
			m.syncSidebarCursorToActive()
		}
	case "remove":
		if channelui.NormalizeSidebarSlug(msg.slug) == channelui.NormalizeSidebarSlug(m.activeChannel) {
			m.activeChannel = "general"
			m.activeApp = channelui.OfficeAppMessages
			m.messages = nil
			m.members = nil
			m.tasks = nil
			m.requests = nil
			m.lastID = ""
			m.replyToID = ""
			m.threadPanelOpen = false
			m.threadPanelID = ""
			m.scroll = 0
			m.clearUnreadState()
			m.syncSidebarCursorToActive()
		}
	}
	return m, tea.Batch(pollChannels(), pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
}

func (m channelModel) handleChannelInterviewAnswerDoneMsg(msg channelInterviewAnswerDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Request answer failed: " + msg.err.Error()
		return m, nil
	}
	m.pending = nil
	m.input = nil
	m.inputPos = 0
	return m, tea.Batch(pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
}

func (m channelModel) handleChannelCancelDoneMsg(msg channelCancelDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Request cancel failed: " + msg.err.Error()
		return m, tea.Batch(pollRequests(m.activeChannel), pollBroker(m.lastID, m.activeChannel))
	}
	if m.pending != nil && m.pending.ID == msg.requestID {
		m.pending = nil
		m.input = nil
		m.inputPos = 0
		m.updateInputOverlays()
	}
	return m, tea.Batch(pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
}

func (m channelModel) handleChannelInterruptDoneMsg(msg channelInterruptDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Failed to pause team: " + msg.err.Error()
	} else {
		m.notice = "Team paused. Answer the interrupt to resume."
	}
	return m, tea.Batch(pollRequests(m.activeChannel), pollBroker(m.lastID, m.activeChannel))
}

func (m channelModel) handleChannelResetDoneMsg(msg channelResetDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	m.confirm = nil
	if msg.err != nil {
		m.notice = "Reset failed: " + msg.err.Error()
		return m, nil
	}
	if normalized := team.NormalizeSessionMode(msg.sessionMode); normalized != "" {
		m.sessionMode = normalized
	}
	if strings.TrimSpace(msg.oneOnOneAgent) != "" || m.sessionMode == team.SessionModeOneOnOne {
		m.oneOnOneAgent = team.NormalizeOneOnOneAgent(msg.oneOnOneAgent)
	}
	m.messages = nil
	m.members = nil
	m.requests = nil
	m.pending = nil
	m.lastID = ""
	m.replyToID = ""
	m.expandedThreads = make(map[string]bool)
	m.input = nil
	m.inputPos = 0
	m.scroll = 0
	m.clearUnreadState()
	m.notice = ""
	m.initFlow = tui.NewInitFlow()
	m.picker.SetActive(false)
	m.threadPanelOpen = false
	m.threadPanelID = ""
	m.threadInput = nil
	m.threadInputPos = 0
	m.threadScroll = 0
	m.focus = focusMain
	m.pickerMode = channelPickerNone
	m.doctor = nil
	m.tasks = nil
	m.actions = nil
	m.scheduler = nil
	m.refreshSlashCommands()
	if m.isOneOnOne() {
		m.activeApp = channelui.OfficeAppMessages
		m.sidebarCollapsed = true
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.replyToID = ""
	}
	m.notice = strings.TrimSpace(msg.notice)
	if m.notice == "" {
		m.notice = "Office reset. Team panes reloaded in place."
	}
	return m, m.pollCurrentState()
}

func (m channelModel) handleChannelResetDMDoneMsg(msg channelResetDMDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	m.confirm = nil
	if msg.err != nil {
		m.notice = "Failed to clear DMs: " + msg.err.Error()
	} else {
		m.notice = fmt.Sprintf("Cleared %d direct messages.", msg.removed)
		m.messages = nil
		m.lastID = ""
	}
	return m, m.pollCurrentState()
}

func (m channelModel) handleChannelDMCreatedMsg(msg channelDMCreatedMsg) (channelModel, tea.Cmd) {
	if msg.err != nil {
		m.notice = "Failed to open DM: " + msg.err.Error()
		return m, nil
	}
	// Switch to the DM channel (slug is now deterministic, e.g. "engineering__human").
	m.activeChannel = msg.slug
	m.focus = focusMain
	m.lastID = ""
	m.messages = nil
	agentDisplay := msg.agentSlug
	if msg.name != "" {
		agentDisplay = msg.name
	}
	m.notice = fmt.Sprintf("DM with %s — Ctrl+D to return to #general", agentDisplay)
	return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel))
}

func (m channelModel) handleChannelInitDoneMsg(msg channelInitDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Setup failed: " + msg.err.Error()
	} else {
		m.notice = strings.TrimSpace(msg.notice)
		if m.notice == "" {
			m.notice = "Setup applied. Team reloaded with the new configuration."
		}
	}
	m.initFlow = tui.NewInitFlow()
	m.picker.SetActive(false)
	m.pickerMode = channelPickerNone
	return m, nil
}

func (m channelModel) handleChannelIntegrationDoneMsg(msg channelIntegrationDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	m.picker.SetActive(false)
	m.pickerMode = channelPickerNone
	if msg.err != nil {
		m.notice = "Integration failed: " + msg.err.Error()
	} else if msg.url != "" {
		m.notice = fmt.Sprintf("%s connected. Browser opened at %s", msg.label, msg.url)
	} else {
		m.notice = fmt.Sprintf("%s connected.", msg.label)
	}
	return m, nil
}

func (m channelModel) handleChannelDoctorDoneMsg(msg channelDoctorDoneMsg) (channelModel, tea.Cmd) {
	if msg.err != nil {
		m.notice = "Doctor failed: " + msg.err.Error()
		m.doctor = nil
	} else {
		report := msg.report
		m.doctor = &report
		m.notice = "Doctor: " + report.StatusLine()
	}
	return m, nil
}

func (m channelModel) handleChannelMemberDraftDoneMsg(msg channelMemberDraftDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Agent update failed: " + msg.err.Error()
		return m, nil
	}
	m.notice = msg.notice
	m.memberDraft = nil
	m.input = nil
	m.inputPos = 0
	return m, tea.Batch(pollOfficeMembers(), pollChannels(), pollMembers(m.activeChannel), pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
}

func (m channelModel) handleChannelTaskMutationDoneMsg(msg channelTaskMutationDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Task update failed: " + msg.err.Error()
	} else if strings.TrimSpace(msg.notice) != "" {
		m.notice = msg.notice
	}
	return m, tea.Batch(pollTasks(m.activeChannel), pollOfficeLedger())
}
