package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
	"github.com/nex-crm/wuphf/internal/workspace"
)

// Slash-command dispatch. Owns the routing from a trimmed input string
// to a (model, tea.Cmd) pair. The dispatcher is intentionally one large
// switch — readability over cleverness; each case is independently
// reviewable, and the alternative (per-command files) would scatter the
// shared `clearCurrent`/`clearMain`/`clearThread` closures.
//
// Helpers (mutators, network commands, picker-option builders) live in
// their own kin files and are referenced here as plain function calls.

func (m *channelModel) maybeActivateChannelPickerFromInput() bool {
	if m.focus != focusMain || m.picker.IsActive() || m.isOneOnOne() {
		return false
	}
	switch string(m.input) {
	case "/switch ", "/s ":
		options := m.buildSwitchChannelPickerOptions()
		if len(options) == 0 {
			m.notice = "No channels yet. Even Michael Scott had a #general. Create one."
			return false
		}
		m.input = nil
		m.inputPos = 0
		m.autocomplete.Dismiss()
		m.mention.Dismiss()
		m.picker = tui.NewPicker("Switch Channel", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerChannels
		m.notice = "Choose a channel to switch to."
		return true
	default:
		return false
	}
}

func (m channelModel) runActiveCommand(trimmed string) (tea.Model, tea.Cmd) {
	threadTarget := ""
	if m.focus == focusThread && m.threadPanelOpen {
		threadTarget = m.threadPanelID
	}
	return m.runCommand(trimmed, threadTarget)
}

func (m channelModel) runCommand(trimmed, threadTarget string) (tea.Model, tea.Cmd) {
	clearMain := func() {
		m.input = nil
		m.inputPos = 0
	}
	clearThread := func() {
		m.threadInput = nil
		m.threadInputPos = 0
	}
	clearCurrent := func() {
		m.doctor = nil
		m.confirm = nil
		if threadTarget != "" {
			clearThread()
			m.updateThreadOverlays()
			return
		}
		clearMain()
		m.updateInputOverlays()
	}

	if m.isOneOnOne() && strings.HasPrefix(trimmed, "/") {
		// Blacklist: commands that only make sense in team/office mode
		teamOnly := []string{
			"/tasks", "/task ", "/task\n",
			"/channels", "/channel ", "/channel\n",
			"/agents", "/agent ", "/agent\n", "/agent prompt",
			"/reply ", "/reply\n",
			"/threads", "/expand ", "/expand\n", "/collapse ", "/collapse\n",
		}
		blocked := false
		for _, prefix := range teamOnly {
			if trimmed == strings.TrimSpace(prefix) || strings.HasPrefix(trimmed, prefix) {
				blocked = true
				break
			}
		}
		if blocked {
			m.notice = "1:1 mode disables office collaboration commands."
			return m, nil
		}
	}

	switch {
	case trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q":
		killTeamSession()
		return m, tea.Quit
	case trimmed == "/shred":
		// Full wipe: runtime + team + company + office + workflows. Next launch
		// reopens onboarding. Done in-process so the user doesn't have to
		// remember the CLI verb.
		res, err := workspace.Shred()
		if err != nil {
			m.notice = fmt.Sprintf("shred failed: %v", err)
			return m, nil
		}
		fmt.Fprintf(os.Stderr, "shred: removed %d path(s). Onboarding will reopen on next launch.\n", len(res.Removed))
		killTeamSession()
		return m, tea.Quit
	case trimmed == "/1o1":
		clearCurrent()
		m.picker = tui.NewPicker("Direct Session", m.buildOneOnOneModePickerOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerOneOnOneMode
		return m, nil
	case strings.HasPrefix(trimmed, "/1o1 "):
		clearCurrent()
		agent := strings.TrimSpace(strings.TrimPrefix(trimmed, "/1o1"))
		if agent == "" {
			agent = team.DefaultOneOnOneAgent
		}
		m.posting = true
		return m, switchSessionMode(team.SessionModeOneOnOne, agent)
	case trimmed == "/messages" || trimmed == "/general":
		clearCurrent()
		m.activeApp = channelui.OfficeAppMessages
		m.syncSidebarCursorToActive()
		if m.isOneOnOne() {
			m.notice = "Viewing your direct session."
		} else {
			m.notice = "Viewing #general."
		}
		return m, nil
	case trimmed == "/inbox":
		clearCurrent()
		if !m.isOneOnOne() {
			m.notice = "/inbox only applies in direct 1:1 mode."
			return m, nil
		}
		m.activeApp = channelui.OfficeAppInbox
		m.syncSidebarCursorToActive()
		m.notice = "Viewing the selected agent inbox."
		return m, nil
	case trimmed == "/outbox":
		clearCurrent()
		if !m.isOneOnOne() {
			m.notice = "/outbox only applies in direct 1:1 mode."
			return m, nil
		}
		m.activeApp = channelui.OfficeAppOutbox
		m.syncSidebarCursorToActive()
		m.notice = "Viewing the selected agent outbox."
		return m, nil
	case trimmed == "/recover" || trimmed == "/resume":
		clearCurrent()
		m.activeApp = channelui.OfficeAppRecovery
		m.syncSidebarCursorToActive()
		if m.isOneOnOne() {
			m.notice = "Viewing the direct-session recovery summary."
		} else {
			m.notice = "Viewing the office recovery summary."
		}
		return m, m.pollCurrentState()
	case trimmed == "/rewind":
		clearCurrent()
		options := m.buildRecoveryPromptPickerOptions()
		if len(options) == 0 {
			m.notice = "Nothing to rewind yet. Give it a minute — or a Pretzel Day."
			return m, nil
		}
		m.picker = tui.NewPicker("Rewind From...", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerRewind
		m.notice = "Choose where recovery should start."
		return m, nil
	case trimmed == "/insert":
		clearCurrent()
		options := m.buildInsertPickerOptions()
		if len(options) == 0 {
			m.notice = "Nothing to insert. Creed hasn't updated the archives yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Insert Reference", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerInsert
		m.notice = "Choose a reference to insert into the composer."
		return m, nil
	case trimmed == "/search":
		clearCurrent()
		options := m.buildSearchPickerOptions()
		if len(options) == 0 {
			m.notice = "Nothing searchable yet. Creed is still organizing the filing system."
			return m, nil
		}
		m.picker = tui.NewPicker("Search Workspace", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerSearch
		m.notice = "Choose where to jump next."
		return m, nil
	case trimmed == "/tasks":
		clearCurrent()
		m.activeApp = channelui.OfficeAppTasks
		m.syncSidebarCursorToActive()
		m.notice = "Viewing tasks in #" + m.activeChannel + "."
		return m, tea.Batch(pollTasks(m.activeChannel))
	case trimmed == "/task":
		clearCurrent()
		options := m.buildTaskPickerOptions()
		if len(options) == 0 {
			m.notice = "No open tasks in #" + m.activeChannel + ". Either ahead of schedule, or at the vending machine."
			return m, nil
		}
		m.picker = tui.NewPicker("Tasks in #"+m.activeChannel, options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerTasks
		return m, nil
	case strings.HasPrefix(trimmed, "/task "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			m.notice = "Usage: /task <claim|release|complete|review|approve|block> <task-id>"
			return m, nil
		}
		action, taskID := parts[1], parts[2]
		switch action {
		case "claim", "release", "complete", "review", "approve", "block":
			m.posting = true
			return m, mutateTask(action, taskID, "you", m.activeChannel)
		default:
			m.notice = "Usage: /task <claim|release|complete|review|approve|block> <task-id>"
			return m, nil
		}
	case trimmed == "/collab":
		clearCurrent()
		m.notice = "Collaborative mode: all agents see all messages — open floor plan, Michael Scott style."
		return m, switchFocusMode(false)
	case trimmed == "/focus":
		clearCurrent()
		m.notice = "Delegation mode: CEO routes, specialists execute. This is how a real office works."
		return m, switchFocusMode(true)
	case trimmed == "/reset":
		clearCurrent()
		m.confirm = m.confirmationForReset()
		m.notice = "Confirm reset."
		return m, nil
	case trimmed == "/reset-dm" || strings.HasPrefix(trimmed, "/reset-dm "):
		clearCurrent()
		agent := ""
		if strings.HasPrefix(trimmed, "/reset-dm ") {
			agent = strings.TrimSpace(strings.TrimPrefix(trimmed, "/reset-dm "))
			agent = strings.TrimPrefix(agent, "@")
		}
		if m.isOneOnOne() {
			agent = m.oneOnOneAgentSlug()
		}
		if agent == "" {
			m.notice = "Usage: /reset-dm <agent> or use in 1:1 mode"
			return m, nil
		}
		m.confirm = channelui.ConfirmationForResetDM(agent, m.activeChannel)
		m.notice = "Confirm clearing the direct transcript."
		return m, nil
	case trimmed == "/dm":
		clearCurrent()
		m.notice = "Usage: /dm <agent-slug>"
		return m, nil
	case strings.HasPrefix(trimmed, "/dm "):
		clearCurrent()
		slug := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "/dm ")))
		slug = strings.TrimPrefix(slug, "@")
		if slug == "" {
			m.notice = "Usage: /dm <agent-slug>"
			return m, nil
		}
		m.notice = fmt.Sprintf("Opening DM with %s…", slug)
		return m, createDMChannel(slug)
	case trimmed == "/integrate":
		clearCurrent()
		memoryStatus := team.ResolveMemoryBackendStatus()
		if memoryStatus.SelectedKind != config.MemoryBackendNex {
			m.notice = "Managed integrations are Nex-only right now. Select the Nex memory backend to use /integrate."
			return m, nil
		}
		if config.ResolveNoNex() {
			m.notice = "Nex is disabled (--no-nex), so managed integrations are unavailable for this run."
			return m, nil
		}
		if config.ResolveAPIKey("") == "" {
			m.notice = "No WUPHF API key configured. Run /init — Ryan Howard skipped this step. Don't be Ryan."
			m.initFlow, _ = m.initFlow.Start()
			return m, nil
		}
		m.picker = tui.NewPicker("Choose Integration", channelIntegrationOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerIntegrations
		m.notice = "Choose an integration to connect. Ryan Howard would've connected them all to one site."
		return m, nil
	case trimmed == "/doctor":
		clearCurrent()
		m.notice = "Checking readiness..."
		return m, runDoctorChecks()
	case trimmed == "/connect":
		clearCurrent()
		m.picker = tui.NewPicker("Connect a channel", []tui.PickerOption{
			{Label: "Telegram", Value: "telegram", Description: "Connect a Telegram group as a shared office channel"},
			{Label: "OpenClaw", Value: "openclaw", Description: "Bridge an OpenClaw session into the office"},
			{Label: "Slack (coming soon)", Value: "slack", Description: "Connect a Slack workspace channel"},
			{Label: "Discord (coming soon)", Value: "discord", Description: "Connect a Discord server channel"},
		})
		m.picker.SetActive(true)
		m.pickerMode = channelPickerConnect
		return m, nil
	case trimmed == "/connect telegram":
		clearCurrent()
		return m, m.startTelegramConnect()
	case trimmed == "/connect openclaw":
		clearCurrent()
		m.startOpenclawConnect()
		return m, nil
	case trimmed == "/switch" || trimmed == "/s":
		clearCurrent()
		options := m.buildSwitchChannelPickerOptions()
		if len(options) == 0 {
			m.notice = "No channels yet. Even Michael Scott had a #general. Create one."
			return m, nil
		}
		m.picker = tui.NewPicker("Switch Channel", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerChannels
		m.notice = "Choose a channel to switch to."
		return m, nil
	case trimmed == "/switcher":
		clearCurrent()
		options := m.buildWorkspaceSwitcherOptions()
		if len(options) == 0 {
			m.notice = "No destinations are available."
			return m, nil
		}
		m.picker = tui.NewPicker("Workspace Switcher", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerSwitcher
		m.notice = "Choose where to jump next."
		return m, nil
	case trimmed == "/channels":
		clearCurrent()
		options := m.buildChannelPickerOptions()
		if len(options) == 0 {
			m.notice = "No channels yet. Even Michael Scott had a #general. Create one."
			return m, nil
		}
		m.picker = tui.NewPicker("Channels", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerChannels
		return m, nil
	case trimmed == "/requests":
		clearCurrent()
		m.activeApp = channelui.OfficeAppRequests
		m.syncSidebarCursorToActive()
		m.notice = "Viewing requests in #" + m.activeChannel + "."
		return m, tea.Batch(pollRequests(m.activeChannel))
	case trimmed == "/request":
		clearCurrent()
		options := m.buildRequestPickerOptions()
		if len(options) == 0 {
			m.notice = "No open requests in #" + m.activeChannel + ". The team is self-sufficient — unlike some regional managers."
			return m, nil
		}
		m.picker = tui.NewPicker("Requests in #"+m.activeChannel, options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerRequests
		return m, nil
	case strings.HasPrefix(trimmed, "/request "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			m.notice = "Usage: /request <focus|answer|dismiss> <request-id>"
			return m, nil
		}
		action, reqID := parts[1], parts[2]
		req, ok := m.findRequestByID(reqID)
		if !ok {
			m.notice = "Request not found: " + reqID
			return m, nil
		}
		switch action {
		case "focus":
			return m.focusRequest(req, "Focused request "+req.ID)
		case "answer":
			return m.answerRequest(req)
		case "dismiss", "snooze", "cancel":
			if m.pending != nil && m.pending.ID == req.ID {
				m.pending = nil
				m.input = nil
				m.inputPos = 0
				m.updateInputOverlays()
			}
			m.notice = "Request canceled."
			m.posting = true
			return m, cancelRequest(req)
		default:
			m.notice = "Usage: /request <focus|answer|dismiss> <request-id>"
			return m, nil
		}
	case trimmed == "/policies":
		clearCurrent()
		m.activeApp = channelui.OfficeAppPolicies
		m.syncSidebarCursorToActive()
		m.notice = "Viewing Nex and office insights."
		return m, pollOfficeLedger()
	case trimmed == "/calendar" || trimmed == "/queue":
		clearCurrent()
		m.activeApp = channelui.OfficeAppCalendar
		m.syncSidebarCursorToActive()
		m.notice = "Viewing the office calendar."
		return m, pollOfficeLedger()
	case strings.HasPrefix(trimmed, "/calendar "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		m.activeApp = channelui.OfficeAppCalendar
		m.syncSidebarCursorToActive()
		if len(parts) < 2 {
			m.notice = "Usage: /calendar [day|week|all|@agent|agent]"
			return m, nil
		}
		arg := strings.TrimSpace(parts[1])
		switch {
		case arg == "day" || arg == "today":
			m.calendarRange = channelui.CalendarRangeDay
			m.notice = "Calendar now shows today."
			return m, pollOfficeLedger()
		case arg == "week":
			m.calendarRange = channelui.CalendarRangeWeek
			m.notice = "Calendar now shows this week."
			return m, pollOfficeLedger()
		case arg == "all":
			m.calendarFilter = ""
			m.notice = "Showing all teammate calendars."
			return m, pollOfficeLedger()
		case arg == "filter":
			options := m.buildCalendarAgentPickerOptions()
			if len(options) == 0 {
				m.notice = "No teammate filters available."
				return m, nil
			}
			m.picker = tui.NewPicker("Filter Calendar", options)
			m.picker.SetActive(true)
			m.pickerMode = channelPickerCalendarAgent
			return m, nil
		default:
			filter := strings.TrimPrefix(arg, "@")
			if filter == "" {
				m.notice = "Usage: /calendar [day|week|all|@agent|agent]"
				return m, nil
			}
			m.calendarFilter = filter
			m.notice = "Filtering calendar for " + channelui.DisplayName(filter) + "."
			return m, pollOfficeLedger()
		}
	case trimmed == "/skills":
		clearCurrent()
		m.activeApp = channelui.OfficeAppSkills
		m.syncSidebarCursorToActive()
		m.notice = "Viewing skills."
		return m, pollSkills("")
	case trimmed == "/artifacts":
		clearCurrent()
		m.activeApp = channelui.OfficeAppArtifacts
		m.syncSidebarCursorToActive()
		m.notice = "Viewing recent execution artifacts."
		return m, m.pollCurrentState()
	case strings.HasPrefix(trimmed, "/skill create "):
		clearCurrent()
		desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "/skill create "))
		if desc == "" {
			m.notice = "Usage: /skill create <description>"
			return m, nil
		}
		m.posting = true
		return m, createSkill(desc, m.activeChannel)
	case strings.HasPrefix(trimmed, "/skill invoke "):
		clearCurrent()
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "/skill invoke "))
		if name == "" {
			m.notice = "Usage: /skill invoke <name>"
			return m, nil
		}
		m.posting = true
		return m, invokeSkill(name)
	case trimmed == "/skill":
		clearCurrent()
		m.notice = "Usage: /skill create <description> or /skill invoke <name>"
		return m, nil
	case strings.HasPrefix(trimmed, "/skill "):
		clearCurrent()
		m.notice = "Usage: /skill create <description> or /skill invoke <name>"
		return m, nil
	case strings.HasPrefix(trimmed, "/channel "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			m.notice = "Usage: /channel add <slug> <description...> or /channel remove <slug>"
			return m, nil
		}
		switch parts[1] {
		case "add":
			description := strings.TrimSpace(strings.Join(parts[3:], " "))
			if description == "" {
				m.notice = "Usage: /channel add <slug> <description...>"
				return m, nil
			}
			m.posting = true
			return m, mutateChannel("create", parts[2], description)
		case "remove":
			m.posting = true
			return m, mutateChannel("remove", parts[2], "")
		default:
			m.notice = "Usage: /channel add <slug> <description...> or /channel remove <slug>"
			return m, nil
		}
	case trimmed == "/agents":
		clearCurrent()
		options := m.buildAgentPickerOptions()
		if len(options) == 0 {
			m.notice = "No agent actions available for this channel."
			return m, nil
		}
		m.picker = tui.NewPicker("Agents in #"+m.activeChannel, options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerAgents
		return m, nil
	case strings.HasPrefix(trimmed, "/agent "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			m.notice = "Usage: /agent <add|remove|disable|enable> <slug>, /agent create, /agent edit <slug>, or /agent prompt <request>"
			return m, nil
		}
		if parts[1] == "prompt" {
			prompt := strings.TrimSpace(strings.TrimPrefix(trimmed, "/agent prompt"))
			if prompt == "" {
				m.notice = "Usage: /agent prompt <describe the teammate you want>"
				return m, nil
			}
			m.posting = true
			return m, generateOfficeMemberFromPrompt(prompt, m.activeChannel)
		}
		if parts[1] == "create" {
			if len(parts) == 2 {
				m.memberDraft = &channelMemberDraft{Mode: "create"}
				m.input = nil
				m.inputPos = 0
				m.notice = "New teammate setup started."
				return m, nil
			}
			if len(parts) < 4 {
				m.notice = "Usage: /agent create <slug> <Display Name>"
				return m, nil
			}
			m.posting = true
			return m, mutateOfficeMemberSpec(channelMemberDraft{
				Mode: "create",
				Slug: parts[2],
				Name: strings.Join(parts[3:], " "),
				Role: strings.Join(parts[3:], " "),
			}, m.activeChannel)
		}
		if parts[1] == "edit" {
			if len(parts) < 3 {
				m.notice = "Usage: /agent edit <slug>"
				return m, nil
			}
			draft, ok := m.startEditMemberDraft(parts[2])
			if !ok {
				m.notice = fmt.Sprintf("Office member %s not found.", parts[2])
				return m, nil
			}
			m.memberDraft = draft
			m.input = nil
			m.inputPos = 0
			m.notice = "Editing teammate profile."
			return m, nil
		}
		if parts[1] == "retire" {
			m.posting = true
			return m, mutateOfficeMember("remove", parts[2], "")
		}
		m.posting = true
		return m, mutateChannelMember(m.activeChannel, parts[1], parts[2])
	case trimmed == "/init":
		clearCurrent()
		m.notice = "Starting setup..."
		var cmd tea.Cmd
		m.initFlow, cmd = m.initFlow.Start()
		return m, cmd
	case trimmed == "/provider":
		clearCurrent()
		m.picker = tui.NewPicker("Switch LLM Provider", tui.ProviderOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerProvider
		m.notice = "Choose an LLM provider."
		return m, nil
	case trimmed == "/cancel":
		clearCurrent()
		if m.replyToID != "" {
			m.replyToID = ""
			m.threadPanelOpen = false
			m.threadPanelID = ""
			clearThread()
			m.threadScroll = 0
			if m.focus == focusThread {
				m.focus = focusMain
			}
			m.notice = "Reply mode cleared. Thread closed — cleaner than a Dwight negotiation."
		} else if m.doctor != nil {
			m.doctor = nil
			m.notice = "Health check done. The doctor says ship it (not medical advice)."
		} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone || m.picker.IsActive() {
			m.initFlow = tui.NewInitFlow()
			m.picker.SetActive(false)
			m.notice = "Setup canceled. Come back when you're ready. That's what she said."
		} else {
			m.notice = "Nothing to cancel. Even Michael Scott knows when there's nothing to cancel."
		}
		return m, nil
	case strings.HasPrefix(trimmed, "/reply"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/reply"))
		if target == "" {
			m.notice = "Usage: /reply <message-id>"
			return m, nil
		}
		if _, ok := channelui.FindMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found. Maybe Creed filed it.", target)
			return m, nil
		}
		m.replyToID = target
		m.threadPanelOpen = true
		m.threadPanelID = target
		clearThread()
		m.threadScroll = 0
		m.focus = focusThread
		m.notice = fmt.Sprintf("Replying in thread %s.", target)
		m.updateThreadOverlays()
		return m, nil
	case strings.HasPrefix(trimmed, "/expand"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/expand"))
		if target == "" {
			m.notice = "Usage: /expand <message-id|all>"
			return m, nil
		}
		if target == "all" {
			for _, msg := range m.messages {
				if channelui.HasThreadReplies(m.messages, msg.ID) {
					m.expandedThreads[msg.ID] = true
				}
			}
			m.notice = "Expanded all threads."
			return m, nil
		}
		if _, ok := channelui.FindMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found. Maybe Creed filed it.", target)
			return m, nil
		}
		m.expandedThreads[target] = true
		m.notice = fmt.Sprintf("Expanded thread %s.", target)
		return m, nil
	case strings.HasPrefix(trimmed, "/collapse"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/collapse"))
		if target == "" {
			m.notice = "Usage: /collapse <message-id|all>"
			return m, nil
		}
		if target == "all" {
			m.expandedThreads = make(map[string]bool)
			m.notice = "Collapsed all threads."
			return m, nil
		}
		if _, ok := channelui.FindMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found. Maybe Creed filed it.", target)
			return m, nil
		}
		delete(m.expandedThreads, target)
		m.notice = fmt.Sprintf("Collapsed thread %s.", target)
		return m, nil
	case trimmed == "/threads":
		clearCurrent()
		options := m.buildThreadPickerOptions()
		if len(options) == 0 {
			m.notice = "No threads yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Threads", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerThreads
		return m, nil
	default:
		// Check if the command matches a skill name
		cmdName := strings.TrimPrefix(trimmed, "/")
		if len(strings.Fields(cmdName)) > 0 {
			cmdName = strings.Fields(cmdName)[0] // first word only
		}
		for _, sk := range m.skills {
			if sk.Name == cmdName && sk.Status == "active" {
				clearCurrent()
				m.posting = true
				m.notice = "Invoking skill: " + sk.Title
				return m, invokeSkill(sk.Name)
			}
		}
		if strings.HasPrefix(trimmed, "/") && cmdName != "" {
			m.setTransientNotice(fmt.Sprintf("Unknown command /%s — type / to see available commands. Even Michael knows the commands.", cmdName))
		}
		return m, nil
	}
}
