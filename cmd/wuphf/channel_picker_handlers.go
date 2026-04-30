package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Picker + InitFlow handlers — Update() switch bodies for the
// tui.PickerSelectMsg + tui.InitFlowMsg cases. The picker dispatch
// is the single biggest case in the original Update switch (~430 LOC,
// 19 picker modes); pulling it out makes the picker-mode contract
// reviewable in isolation. Each picker mode owns one transition:
// what the selected value means, what new picker (if any) opens
// next, what tea.Cmd fires.
//
// channelPickerNone is the implicit "no picker active" state; every
// path through this handler resets pickerMode to it before
// transitioning so a stale picker mode can't outlive its picker.

func (m channelModel) handlePickerSelectMsg(msg tui.PickerSelectMsg) (channelModel, tea.Cmd) {
	switch m.pickerMode {
	case channelPickerIntegrations:
		spec, ok := findChannelIntegration(msg.Value)
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		if !ok {
			m.notice = "Unknown integration selection."
			return m, nil
		}
		m.posting = true
		m.notice = fmt.Sprintf("Opening %s OAuth flow in your browser...", spec.Label)
		return m, connectIntegration(spec)
	case channelPickerChannels:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		switch {
		case strings.HasPrefix(msg.Value, "app:"):
			switch channelui.OfficeApp(strings.TrimPrefix(msg.Value, "app:")) {
			case channelui.OfficeAppMessages:
				m.activeApp = channelui.OfficeAppMessages
				m.notice = "Viewing messages."
				m.syncSidebarCursorToActive()
				return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel))
			case channelui.OfficeAppTasks:
				m.activeApp = channelui.OfficeAppTasks
				m.notice = "Viewing tasks in #" + m.activeChannel + "."
				m.syncSidebarCursorToActive()
				return m, pollTasks(m.activeChannel)
			case channelui.OfficeAppRequests:
				m.activeApp = channelui.OfficeAppRequests
				m.notice = "Viewing requests in #" + m.activeChannel + "."
				m.syncSidebarCursorToActive()
				return m, pollRequests(m.activeChannel)
			case channelui.OfficeAppPolicies:
				m.activeApp = channelui.OfficeAppPolicies
				m.notice = "Viewing policies and decisions."
				m.syncSidebarCursorToActive()
				return m, pollOfficeLedger()
			case channelui.OfficeAppCalendar:
				m.activeApp = channelui.OfficeAppCalendar
				m.notice = "Viewing the office calendar."
				m.syncSidebarCursorToActive()
				return m, nil
			}
		case strings.HasPrefix(msg.Value, "session:1o1:"):
			agent := strings.TrimSpace(strings.TrimPrefix(msg.Value, "session:1o1:"))
			if agent == "" {
				agent = team.DefaultOneOnOneAgent
			}
			m.confirm = confirmationForSessionSwitch(team.SessionModeOneOnOne, agent)
			m.notice = "Confirm the direct session switch."
			return m, nil
		case msg.Value == "session:office":
			m.confirm = confirmationForSessionSwitch(team.SessionModeOffice, team.DefaultOneOnOneAgent)
			m.notice = "Confirm the session switch."
			return m, nil
		case strings.HasPrefix(msg.Value, "switch:"):
			m.activeChannel = strings.TrimPrefix(msg.Value, "switch:")
			m.lastID = ""
			m.messages = nil
			m.members = nil
			m.replyToID = ""
			m.threadPanelOpen = false
			m.threadPanelID = ""
			m.notice = "Switched to #" + m.activeChannel
			return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel))
		case strings.HasPrefix(msg.Value, "remove:"):
			m.posting = true
			return m, mutateChannel("remove", strings.TrimPrefix(msg.Value, "remove:"), "")
		}
		return m, nil
	case channelPickerSwitcher:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		return m, m.applyWorkspaceSwitcherSelection(msg.Value)
	case channelPickerInsert:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		if strings.TrimSpace(msg.Value) == "" {
			m.notice = "Nothing inserted."
			return m, nil
		}
		m.insertIntoActiveComposer(msg.Value)
		m.notice = "Inserted reference into the composer."
		return m, nil
	case channelPickerSearch:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		return m, m.applySearchSelection(msg.Value, msg.Label)
	case channelPickerRewind:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		m.applyRecoveryPrompt(msg.Value)
		return m, nil
	case channelPickerAgents:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		if msg.Value == "create:new" {
			m.notice = "Use /agent create <slug> <Display Name> to add a new office member."
			return m, nil
		}
		parts := strings.SplitN(msg.Value, ":", 2)
		if len(parts) != 2 {
			return m, nil
		}
		if parts[0] == "edit" {
			draft, ok := m.startEditMemberDraft(parts[1])
			if !ok {
				m.notice = fmt.Sprintf("Office member %s not found.", parts[1])
				return m, nil
			}
			m.memberDraft = draft
			m.notice = "Editing teammate profile."
			return m, nil
		}
		m.posting = true
		return m, mutateChannelMember(m.activeChannel, parts[0], parts[1])
	case channelPickerRequests:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		for _, req := range m.requests {
			if req.ID == msg.Value {
				return m, m.openRequestActionPicker(req)
			}
		}
		return m, nil
	case channelPickerCalendarAgent:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		if msg.Value == "all" {
			m.calendarFilter = ""
			m.notice = "Showing all teammate calendars."
			return m, nil
		}
		m.calendarFilter = strings.TrimSpace(msg.Value)
		if m.calendarFilter == "" {
			m.notice = "Showing all teammate calendars."
		} else {
			m.notice = "Filtering calendar for " + channelui.DisplayName(m.calendarFilter) + "."
		}
		return m, nil
	case channelPickerOneOnOneMode:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		switch strings.TrimSpace(msg.Value) {
		case "enable":
			options := m.buildOneOnOneAgentPickerOptions()
			if len(options) == 0 {
				m.notice = "No office agents are available for direct mode."
				return m, nil
			}
			m.picker = tui.NewPicker("Choose Direct Agent", options)
			m.picker.SetActive(true)
			m.pickerMode = channelPickerOneOnOneAgent
			return m, nil
		case "disable":
			if !m.isOneOnOne() {
				m.notice = "Already running the full office team."
				return m, nil
			}
			m.confirm = confirmationForSessionSwitch(team.SessionModeOffice, team.DefaultOneOnOneAgent)
			m.notice = "Confirm the session switch."
			return m, nil
		default:
			return m, nil
		}
	case channelPickerOneOnOneAgent:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		agent := strings.TrimSpace(msg.Value)
		if agent == "" {
			agent = team.DefaultOneOnOneAgent
		}
		m.confirm = confirmationForSessionSwitch(team.SessionModeOneOnOne, agent)
		m.notice = "Confirm the direct session switch."
		return m, nil
	case channelPickerConnect:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		switch msg.Value {
		case "telegram":
			return m, m.startTelegramConnect()
		case "openclaw":
			m.startOpenclawConnect()
			return m, nil
		default:
			m.notice = msg.Label + " is not available yet."
			return m, nil
		}
	case channelPickerTelegramToken:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		token := strings.TrimSpace(msg.Value)
		if token == "" {
			m.notice = "Telegram connection canceled."
			return m, nil
		}
		_ = os.Setenv("WUPHF_TELEGRAM_BOT_TOKEN", token)
		config.SaveTelegramBotToken(token)
		m.posting = true
		m.notice = "Verifying bot token..."
		return m, discoverTelegramGroups(token)
	case channelPickerTelegramChatID:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		chatIDStr := strings.TrimSpace(msg.Value)
		if chatIDStr == "" {
			m.notice = "Canceled."
			return m, nil
		}
		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			m.notice = "Invalid chat ID. Must be a number like -5093020979."
			return m, nil
		}
		// Verify the chat exists using getChat
		title, verifyErr := team.VerifyChat(m.telegramToken, chatID)
		if verifyErr != nil {
			m.notice = "Could not verify chat: " + verifyErr.Error()
			return m, nil
		}
		if title == "" {
			title = fmt.Sprintf("Telegram %d", chatID)
		}
		m.posting = true
		m.notice = fmt.Sprintf("Connecting \"%s\"...", title)
		return m, connectTelegramGroup(m.telegramToken, team.TelegramGroup{
			ChatID: chatID,
			Title:  title,
			Type:   "group",
		})
	case channelPickerTelegramGroup:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone

		if msg.Value == "dm" {
			m.posting = true
			m.notice = "Setting up direct message channel..."
			dmGroup := team.TelegramGroup{ChatID: 0, Title: "Telegram DM", Type: "private"}
			return m, connectTelegramGroup(m.telegramToken, dmGroup)
		}

		if msg.Value == "retry" {
			m.posting = true
			m.notice = "Checking for groups..."
			return m, discoverTelegramGroups(m.telegramToken)
		}

		var selected *team.TelegramGroup
		for i := range m.telegramGroups {
			if fmt.Sprintf("%d", m.telegramGroups[i].ChatID) == msg.Value {
				selected = &m.telegramGroups[i]
				break
			}
		}
		if selected == nil {
			m.notice = "Unknown group selection."
			return m, nil
		}
		m.posting = true
		m.notice = fmt.Sprintf("Connecting \"%s\"...", selected.Title)
		return m, connectTelegramGroup(m.telegramToken, *selected)
	case channelPickerOpenclawURL:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		url := strings.TrimSpace(msg.Value)
		if url == "" {
			url = "ws://127.0.0.1:18789"
		}
		m.openclawURL = url
		m.promptOpenclawToken()
		return m, nil
	case channelPickerOpenclawToken:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		token := strings.TrimSpace(msg.Value)
		if token == "" {
			m.notice = "OpenClaw connection canceled."
			return m, nil
		}
		m.openclawToken = token
		m.posting = true
		m.notice = "Dialing OpenClaw gateway..."
		return m, fetchOpenclawSessions(m.openclawURL, m.openclawToken)
	case channelPickerOpenclawSession:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		key := strings.TrimSpace(msg.Value)
		if key == "" {
			m.notice = "OpenClaw connection canceled."
			return m, nil
		}
		if key == "retry-url" {
			m.promptOpenclawURL()
			return m, nil
		}
		var selected *openclawSessionOption
		for i := range m.openclawSessions {
			if m.openclawSessions[i].SessionKey == key {
				selected = &m.openclawSessions[i]
				break
			}
		}
		if selected == nil {
			m.notice = "Unknown OpenClaw session selection."
			return m, nil
		}
		m.posting = true
		m.notice = fmt.Sprintf("Bridging \"%s\"...", selected.Label)
		return m, connectOpenclawSession(m.openclawURL, m.openclawToken, *selected)
	case channelPickerTasks:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		for _, task := range m.tasks {
			if task.ID == msg.Value {
				return m, m.openTaskActionPicker(task)
			}
		}
		return m, nil
	case channelPickerTaskAction:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		parts := strings.SplitN(msg.Value, ":", 2)
		if len(parts) != 2 {
			return m, nil
		}
		action, taskID := parts[0], parts[1]
		switch action {
		case "claim", "release", "complete", "approve", "block":
			m.posting = true
			return m, mutateTask(action, taskID, "you", m.activeChannel)
		case "open":
			if task, ok := m.findTaskByID(taskID); ok && task.ThreadID != "" {
				m.threadPanelOpen = true
				m.threadPanelID = task.ThreadID
				m.replyToID = task.ThreadID
			}
			return m, nil
		}
		return m, nil
	case channelPickerRequestAction:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		parts := strings.SplitN(msg.Value, ":", 2)
		if len(parts) != 2 {
			return m, nil
		}
		action, reqID := parts[0], parts[1]
		switch action {
		case "focus":
			if req, ok := m.findRequestByID(reqID); ok {
				next, cmd := m.focusRequest(req, "Focused request "+req.ID)
				return next.(channelModel), cmd
			}
		case "answer":
			if req, ok := m.findRequestByID(reqID); ok {
				next, cmd := m.answerRequest(req)
				return next.(channelModel), cmd
			}
		case "dismiss", "snooze", "cancel":
			if req, ok := m.findRequestByID(reqID); ok {
				if m.pending != nil && m.pending.ID == req.ID {
					m.pending = nil
					m.input = nil
					m.inputPos = 0
					m.updateInputOverlays()
				}
				m.notice = "Request canceled."
				m.posting = true
				return m, cancelRequest(req)
			}
			return m, nil
		case "open":
			if req, ok := m.findRequestByID(reqID); ok && req.ReplyTo != "" {
				m.threadPanelOpen = true
				m.threadPanelID = req.ReplyTo
				m.replyToID = req.ReplyTo
				m.notice = "Opened thread for request " + req.ID
			}
			return m, nil
		}
		return m, nil
	case channelPickerThreads:
		// User selected a thread — show action sub-picker
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		selectedMsgID := msg.Value
		actions := []tui.PickerOption{
			{Label: "Reply in thread", Value: "reply:" + selectedMsgID, Description: "Set reply mode for this thread"},
		}
		if m.expandedThreads[selectedMsgID] {
			actions = append(actions, tui.PickerOption{Label: "Collapse thread", Value: "collapse:" + selectedMsgID, Description: "Hide replies inline"})
		} else {
			actions = append(actions, tui.PickerOption{Label: "Expand thread", Value: "expand:" + selectedMsgID, Description: "Show replies inline"})
		}
		m.picker = tui.NewPicker("Thread: "+channelui.TruncateText(msg.Label, 40), actions)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerThreadAction
		return m, nil
	case channelPickerThreadAction:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		parts := strings.SplitN(msg.Value, ":", 2)
		if len(parts) != 2 {
			return m, nil
		}
		action, msgID := parts[0], parts[1]
		switch action {
		case "reply":
			m.replyToID = msgID
			m.expandedThreads[msgID] = true // auto-expand so you see the thread
			m.notice = fmt.Sprintf("Replying in thread %s — type your reply and press Enter", msgID)
		case "expand":
			m.expandedThreads[msgID] = true
		case "collapse":
			delete(m.expandedThreads, msgID)
		}
		return m, nil
	case channelPickerProvider:
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		m.posting = true
		return m, applyProviderSelection(msg.Value)
	default:
		m.picker.SetActive(false)
		var cmd tea.Cmd
		m.initFlow, cmd = m.initFlow.Update(msg)
		return m, cmd
	}
}

func (m channelModel) handleInitFlowMsg(msg tui.InitFlowMsg) (channelModel, tea.Cmd) {
	var cmd tea.Cmd
	m.initFlow, cmd = m.initFlow.Update(msg)
	switch m.initFlow.Phase() {
	case tui.InitProviderChoice:
		m.picker = tui.NewPicker("Choose LLM Provider", tui.ProviderOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerInitProvider
	case tui.InitBlueprintChoice, tui.InitPackChoice:
		m.picker = tui.NewPicker("Choose Operation Template", tui.BlueprintOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerInitBlueprint
	case tui.InitDone:
		m.posting = true
		return m, tea.Batch(cmd, applyTeamSetup())
	}
	return m, cmd
}
