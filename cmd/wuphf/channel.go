package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

type channelMsg struct {
	messages []channelui.BrokerMessage
}

type channelMembersMsg struct {
	members []channelui.Member
}

type channelOfficeMembersMsg struct {
	members []channelui.OfficeMember
}

type channelChannelsMsg struct {
	channels []channelui.ChannelInfo
}

type channelRequestsMsg struct {
	requests []channelui.Interview
	pending  *channelui.Interview
}

type channelTasksMsg struct {
	tasks []channelui.Task
}

type channelActionsMsg struct {
	actions []channelui.Action
}

type channelSignalsMsg struct {
	signals []channelui.Signal
}

type channelDecisionsMsg struct {
	decisions []channelui.Decision
}

type channelWatchdogsMsg struct {
	alerts []channelui.Watchdog
}

type channelSchedulerMsg struct {
	jobs []channelui.SchedulerJob
}

type channelSkillsMsg struct {
	skills []channelui.Skill
}

type channelUsageMsg struct {
	usage channelui.UsageState
}

type channelHealthMsg struct {
	Connected     bool
	SessionMode   string
	OneOnOneAgent string
}

type channelTickMsg time.Time
type channelPostDoneMsg struct {
	err    error
	notice string
	action string
	slug   string
}
type channelInterviewAnswerDoneMsg struct{ err error }
type channelCancelDoneMsg struct {
	requestID string
	err       error
}
type channelInterruptDoneMsg struct{ err error }
type channelResetDoneMsg struct {
	err           error
	notice        string
	sessionMode   string
	oneOnOneAgent string
}
type channelResetDMDoneMsg struct {
	err     error
	removed int
}
type channelDMCreatedMsg struct {
	err       error
	slug      string // deterministic DM slug e.g. "engineering__human"
	agentSlug string // agent side of the DM
	name      string // display name
}
type channelInitDoneMsg struct {
	err    error
	notice string
}
type channelIntegrationDoneMsg struct {
	label string
	url   string
	err   error
}
type telegramDiscoverMsg struct {
	botName string
	groups  []team.TelegramGroup
	token   string
	err     error
}
type telegramConnectDoneMsg struct {
	channelSlug string
	groupTitle  string
	err         error
}

// openclawSessionOption is the minimal session data we retain for the picker.
type openclawSessionOption struct {
	SessionKey string
	Label      string
	Preview    string
}

type openclawSessionsMsg struct {
	sessions []openclawSessionOption
	err      error
}

type openclawConnectDoneMsg struct {
	slug  string
	label string
	err   error
}

type channelTaskMutationDoneMsg struct {
	notice string
	err    error
}

type channelMemberDraftDoneMsg struct {
	err    error
	notice string
}

type channelMemberDraft struct {
	Mode           string
	OriginalSlug   string
	Step           int
	Slug           string
	Name           string
	Role           string
	Expertise      string
	Personality    string
	PermissionMode string
}

var brokerTokenPath = brokeraddr.DefaultTokenFile

var channelSlashCommands = []tui.SlashCommand{
	{Name: "init", Description: "Run setup (Ryan Howard skipped this step — don't be Ryan)", Category: "setup"},
	{Name: "provider", Description: "Switch LLM provider (choose wisely, Michael)", Category: "setup"},
	{Name: "doctor", Description: "Check readiness and runtime health (Meredith not involved)", Category: "setup"},
	{Name: "integrate", Description: "Connect an integration (beat the Dunder Mifflin fax)", Category: "setup"},
	{Name: "connect", Description: "Bring Telegram, OpenClaw, or other integrations into the office", Category: "setup"},
	{Name: "1o1", Description: "Direct 1:1 with an agent — Toby not invited", Category: "session"},
	{Name: "messages", Description: "Show the main office feed — where it all happens", Category: "navigate"},
	{Name: "inbox", Description: "Show the selected agent inbox lane in 1:1 mode", Category: "navigate"},
	{Name: "outbox", Description: "Show the selected agent outbox lane in 1:1 mode", Category: "navigate"},
	{Name: "recover", Description: "Session recovery — Creed would call this 'continuity'", Category: "navigate"},
	{Name: "resume", Description: "Alias for /recover", Category: "navigate"},
	{Name: "rewind", Description: "Catch up from here — not a full Threat Level Midnight", Category: "navigate"},
	{Name: "search", Description: "Search channels, tasks, requests (Creed files too)", Category: "navigate"},
	{Name: "insert", Description: "Insert a channel, task, request, or message reference", Category: "navigate"},
	{Name: "switcher", Description: "Switch office/direct — faster than Dwight's fire drill", Category: "navigate"},
	{Name: "tasks", Description: "Show active work — Dwight tracks these on paper too", Category: "navigate"},
	{Name: "switch", Description: "Switch to another channel", Category: "navigate"},
	{Name: "channels", Description: "Browse and manage channels", Category: "navigate"},
	{Name: "channel", Description: "Create or remove a channel", Category: "channels"},
	{Name: "agents", Description: "Manage your team (no downsizing announcements)", Category: "people"},
	{Name: "agent", Description: "Add, remove, enable, or disable a teammate", Category: "people"},
	{Name: "agent prompt", Description: "New teammate from a prompt — Ryan calls this 'disruption'", Category: "people"},
	{Name: "task", Description: "Claim, release, or complete a task — ownership matters here", Category: "work"},
	{Name: "policies", Description: "Signals, watchdogs, decisions — no beet farm required", Category: "navigate"},
	{Name: "calendar", Description: "Office schedule — more reliable than Michael's personal calendar", Category: "navigate"},
	{Name: "queue", Description: "Alias for /calendar", Category: "navigate"},
	{Name: "artifacts", Description: "Task logs, approvals, and workflow artifacts — the paper trail Dwight demands", Category: "navigate"},
	{Name: "skills", Description: "Show available skills — everyone has a specialty, even Kevin", Category: "navigate"},
	{Name: "skill", Description: "Create, invoke, or manage a skill — the office gets smarter over time", Category: "work"},
	{Name: "reply", Description: "Reply in thread — threads keep context, unlike forwarded email chains", Category: "conversation"},
	{Name: "threads", Description: "Browse threads — the antidote to 'per my last email'", Category: "conversation"},
	{Name: "expand", Description: "Expand a collapsed thread — Michael never collapses anything", Category: "conversation"},
	{Name: "collapse", Description: "Collapse a thread — keep the office tidy, Dwight approves", Category: "conversation"},
	{Name: "cancel", Description: "Exit current mode — that's what she said (probably)", Category: "conversation"},
	{Name: "collab", Description: "Open-floor mode — everyone hears everything, Michael Scott style", Category: "session"},
	{Name: "focus", Description: "Delegation mode — CEO routes, specialists execute (that's how it was always meant to work)", Category: "session"},
	{Name: "reset", Description: "Reset channel and agents", Category: "session"},
	{Name: "reset-dm", Description: "Clear direct messages with an agent", Category: "session"},
	{Name: "quit", Description: "Exit WUPHF — Michael would make a speech first", Category: "session"},
}

// oneOnOneBlacklist lists command names blocked in 1:1 mode.
var oneOnOneBlacklist = map[string]bool{
	"tasks":        true,
	"task":         true,
	"channels":     true,
	"channel":      true,
	"agents":       true,
	"agent":        true,
	"agent prompt": true,
	"reply":        true,
	"threads":      true,
	"expand":       true,
	"collapse":     true,
	"collab":       true,
	"focus":        true,
}

func buildOneOnOneSlashCommands() []tui.SlashCommand {
	var cmds []tui.SlashCommand
	for _, cmd := range channelSlashCommands {
		if oneOnOneBlacklist[cmd.Name] {
			continue
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}

type channelPickerMode string

const (
	channelPickerNone            channelPickerMode = ""
	channelPickerInitProvider    channelPickerMode = "init_provider"
	channelPickerInitBlueprint   channelPickerMode = "init_blueprint"
	channelPickerInitPack        channelPickerMode = "init_pack" // legacy alias
	channelPickerProvider        channelPickerMode = "provider"
	channelPickerIntegrations    channelPickerMode = "integrations"
	channelPickerRequests        channelPickerMode = "requests"
	channelPickerTasks           channelPickerMode = "tasks"
	channelPickerTaskAction      channelPickerMode = "task_action"
	channelPickerRequestAction   channelPickerMode = "request_action"
	channelPickerThreads         channelPickerMode = "threads"
	channelPickerThreadAction    channelPickerMode = "thread_action"
	channelPickerChannels        channelPickerMode = "channels"
	channelPickerSwitcher        channelPickerMode = "switcher"
	channelPickerInsert          channelPickerMode = "insert"
	channelPickerSearch          channelPickerMode = "search"
	channelPickerRewind          channelPickerMode = "rewind"
	channelPickerAgents          channelPickerMode = "agents"
	channelPickerCalendarAgent   channelPickerMode = "calendar_agent"
	channelPickerOneOnOneMode    channelPickerMode = "one_on_one_mode"
	channelPickerOneOnOneAgent   channelPickerMode = "one_on_one_agent"
	channelPickerTelegramGroup   channelPickerMode = "telegram_group"
	channelPickerConnect         channelPickerMode = "connect"
	channelPickerTelegramToken   channelPickerMode = "telegram_token"
	channelPickerTelegramChatID  channelPickerMode = "telegram_chat_id"
	channelPickerOpenclawURL     channelPickerMode = "openclaw-url"
	channelPickerOpenclawToken   channelPickerMode = "openclaw-token"
	channelPickerOpenclawSession channelPickerMode = "openclaw-session"
)

type quickJumpTarget string

const (
	quickJumpNone     quickJumpTarget = ""
	quickJumpChannels quickJumpTarget = "channels"
	quickJumpApps     quickJumpTarget = "apps"
)

type channelIntegrationSpec struct {
	Label       string
	Value       string
	Type        string
	Provider    string
	Description string
}

var channelIntegrationSpecs = []channelIntegrationSpec{
	{Label: "Gmail", Value: "gmail", Type: "email", Provider: "google", Description: "Connect Google email"},
	{Label: "Google Calendar", Value: "google-calendar", Type: "calendar", Provider: "google", Description: "Connect Google Calendar and the WUPHF Meeting Bot"},
	{Label: "Outlook", Value: "outlook", Type: "email", Provider: "microsoft", Description: "Connect Microsoft email"},
	{Label: "Outlook Calendar", Value: "outlook-calendar", Type: "calendar", Provider: "microsoft", Description: "Connect Outlook Calendar and the WUPHF Meeting Bot"},
	{Label: "Slack", Value: "slack", Type: "messaging", Provider: "slack", Description: "Connect Slack workspace messaging"},
	{Label: "Salesforce", Value: "salesforce", Type: "crm", Provider: "salesforce", Description: "Connect Salesforce CRM"},
	{Label: "HubSpot", Value: "hubspot", Type: "crm", Provider: "hubspot", Description: "Connect HubSpot CRM"},
	{Label: "Attio", Value: "attio", Type: "crm", Provider: "attio", Description: "Connect Attio CRM"},
}

// focusArea identifies which panel currently owns keyboard input.
type focusArea int

const (
	focusMain    focusArea = 0
	focusSidebar focusArea = 1
	focusThread  focusArea = 2
)

type channelModel struct {
	messages             []channelui.BrokerMessage
	members              []channelui.Member
	officeMembers        []channelui.OfficeMember
	channels             []channelui.ChannelInfo
	requests             []channelui.Interview
	tasks                []channelui.Task
	actions              []channelui.Action
	signals              []channelui.Signal
	decisions            []channelui.Decision
	watchdogs            []channelui.Watchdog
	scheduler            []channelui.SchedulerJob
	skills               []channelui.Skill
	pending              *channelui.Interview
	lastID               string
	activeChannel        string
	activeApp            channelui.OfficeApp
	replyToID            string
	expandedThreads      map[string]bool
	clickableThreads     map[int]string // rendered line index → message ID for click-to-expand
	threadsDefaultExpand bool           // true = expand threads by default
	tickFrame            int            // incremented each tick for animations
	autocomplete         tui.AutocompleteModel
	mention              tui.MentionModel
	input                []rune
	inputPos             int
	inputHistory         channelui.History
	width                int
	height               int
	scroll               int
	unreadCount          int
	unreadAnchorID       string
	awaySummary          string
	posting              bool
	selectedOption       int
	notice               string
	noticeExpireAt       time.Time
	confirm              *channelui.ChannelConfirm
	doctor               *channelui.DoctorReport
	memberDraft          *channelMemberDraft
	initFlow             tui.InitFlowModel
	picker               tui.PickerModel
	pickerMode           channelPickerMode

	// 3-column layout state
	focus               focusArea
	sidebarCollapsed    bool
	sidebarCursor       int
	sidebarRosterOffset int
	threadPanelOpen     bool
	threadPanelID       string
	threadInput         []rune
	threadInputPos      int
	threadInputHistory  channelui.History
	threadScroll        int
	usage               channelui.UsageState
	brokerConnected     bool
	sessionMode         string
	oneOnOneAgent       string
	lastCtrlCAt         time.Time
	quickJumpTarget     quickJumpTarget
	calendarRange       channelui.CalendarRange
	calendarFilter      string

	// Telegram connect flow state
	telegramGroups []team.TelegramGroup
	telegramToken  string

	// OpenClaw connect flow state
	openclawURL      string
	openclawToken    string
	openclawSessions []openclawSessionOption

	// lastAgentContent tracks the latest streaming text per agent for sidebar display.
	lastAgentContent map[string]string

	// onboardingChecklist holds the "Getting started" checklist rendered in the sidebar.
	onboardingChecklist onboardingChecklist
}

func newChannelModel(threadsCollapsed bool) channelModel {
	return newChannelModelWithApp(threadsCollapsed, channelui.OfficeAppMessages)
}

func newChannelModelWithApp(threadsCollapsed bool, initialApp channelui.OfficeApp) channelModel {
	manifest, _ := company.LoadManifest()
	officeMembers := channelui.OfficeMembersFromManifest(manifest)
	channels := channelui.ChannelInfosFromManifest(manifest)
	sessionMode := team.SessionModeOffice
	oneOnOneAgent := ""
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE")), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE")), "true") {
		sessionMode = team.SessionModeOneOnOne
		oneOnOneAgent = strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE_AGENT"))
		if oneOnOneAgent == "" {
			oneOnOneAgent = team.DefaultOneOnOneAgent
		}
		initialApp = channelui.OfficeAppMessages
	}
	channelui.SetOfficeDirectory(officeMembers)
	m := channelModel{
		expandedThreads:      make(map[string]bool),
		threadsDefaultExpand: !threadsCollapsed,
		autocomplete:         tui.NewAutocomplete(channelSlashCommands),
		mention:              tui.NewMention(channelMentionAgents(nil)),
		inputHistory:         channelui.NewHistory(),
		initFlow:             tui.NewInitFlow(),
		activeChannel:        "general",
		activeApp:            initialApp,
		calendarRange:        channelui.CalendarRangeWeek,
		officeMembers:        officeMembers,
		channels:             channels,
		sessionMode:          sessionMode,
		oneOnOneAgent:        oneOnOneAgent,
		threadInputHistory:   channelui.NewHistory(),
		lastAgentContent:     make(map[string]string),
	}
	if m.isOneOnOne() {
		m.sidebarCollapsed = true
		m.threadsDefaultExpand = true
		m.autocomplete = tui.NewAutocomplete(buildOneOnOneSlashCommands())
		m.notice = "Conference room reserved. Direct session reset. Agent pane reloaded in place. No Toby."
	}
	memoryStatus := team.ResolveMemoryBackendStatus()
	if memoryStatus.SelectedKind == config.MemoryBackendNone {
		if config.ResolveNoNex() {
			m.notice = "Running in office-only mode. Nex tools are disabled for this session."
		} else {
			m.notice = "Running without an external memory backend for this session."
		}
	} else if memoryStatus.SelectedKind == config.MemoryBackendNex && memoryStatus.ActiveKind == config.MemoryBackendNone && strings.TrimSpace(config.ResolveAPIKey("")) == "" {
		m.notice = "No WUPHF API key configured. Starting setup..."
		m.initFlow, _ = m.initFlow.Start()
	} else if memoryStatus.SelectedKind == config.MemoryBackendGBrain && strings.TrimSpace(config.ResolveOpenAIAPIKey()) == "" && strings.TrimSpace(config.ResolveAnthropicAPIKey()) == "" {
		m.notice = "No OpenAI or Anthropic API key configured for GBrain. Starting setup..."
		m.initFlow, _ = m.initFlow.Start()
	} else if memoryStatus.SelectedKind == config.MemoryBackendGBrain && memoryStatus.ActiveKind == config.MemoryBackendNone && strings.TrimSpace(memoryStatus.Detail) != "" {
		m.notice = memoryStatus.Detail
	}
	m.syncSidebarCursorToActive()
	return m
}

// setTransientNotice sets a notice that auto-clears after the next few ticks.
func (m *channelModel) setTransientNotice(text string) {
	m.notice = text
	m.noticeExpireAt = time.Now().Add(4 * time.Second)
}

func (m channelModel) isOneOnOne() bool {
	return team.NormalizeSessionMode(m.sessionMode) == team.SessionModeOneOnOne
}

func (m channelModel) oneOnOneAgentSlug() string {
	return team.NormalizeOneOnOneAgent(m.oneOnOneAgent)
}

func (m channelModel) oneOnOneAgentName() string {
	slug := m.oneOnOneAgentSlug()
	for _, member := range channelui.MergeOfficeMembers(m.officeMembers, m.members, nil) {
		if member.Slug == slug && strings.TrimSpace(member.Name) != "" {
			return member.Name
		}
	}
	return channelui.DisplayName(slug)
}

func (m *channelModel) refreshSlashCommands() {
	var activeInput []rune
	activeCursor := 0
	preserveOverlays := false
	if m.focus == focusThread && m.threadPanelOpen {
		activeInput = append([]rune(nil), m.threadInput...)
		activeCursor = m.threadInputPos
		preserveOverlays = true
	} else if m.focus == focusMain {
		activeInput = append([]rune(nil), m.input...)
		activeCursor = m.inputPos
		preserveOverlays = true
	}
	var base []tui.SlashCommand
	if m.isOneOnOne() {
		base = buildOneOnOneSlashCommands()
	} else {
		base = make([]tui.SlashCommand, len(channelSlashCommands))
		copy(base, channelSlashCommands)
	}
	var skillCommands []tui.SlashCommand
	for _, sk := range m.skills {
		if sk.Status != "active" {
			continue
		}
		skillCommands = append(skillCommands, tui.SlashCommand{
			Name:        sk.Name,
			Description: sk.Description,
			Category:    "skills",
		})
	}
	base = append(skillCommands, base...)
	m.autocomplete = tui.NewAutocomplete(base)
	if preserveOverlays {
		m.updateOverlaysForInput(activeInput, activeCursor)
		return
	}
	m.updateOverlaysForCurrentInput()
}

func (m channelModel) pollCurrentState() tea.Cmd {
	if m.isOneOnOne() {
		return tea.Sequence(
			pollHealth(),
			pollBroker(m.lastID, m.activeChannel),
			pollMembers(m.activeChannel),
			tickChannel(),
		)
	}
	return tea.Sequence(
		pollHealth(),
		pollChannels(),
		pollOfficeMembers(),
		pollBroker(m.lastID, m.activeChannel),
		pollMembers(m.activeChannel),
		pollRequests(m.activeChannel),
		pollTasks(m.activeChannel),
		pollSkills(""),
		pollOfficeLedger(),
		pollUsage(),
		tickChannel(),
	)
}

func (m channelModel) Init() tea.Cmd {
	m.lastID = ""
	return m.pollCurrentState()
}

func (m channelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.ClearScreen

	case tea.MouseMsg:
		layout := channelui.ComputeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
		inSidebar := layout.ShowSidebar && msg.X < layout.SidebarW
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.focus == focusThread && m.threadPanelOpen {
				m.threadScroll++
			} else if inSidebar {
				if m.sidebarRosterOffset > 0 {
					m.sidebarRosterOffset--
				}
			} else {
				m.scroll++
			}
		case tea.MouseButtonWheelDown:
			if m.focus == focusThread && m.threadPanelOpen {
				if m.threadScroll > 0 {
					m.threadScroll--
				}
			} else if inSidebar {
				m.sidebarRosterOffset++
			} else {
				if m.scroll > 0 {
					m.scroll--
					if m.scroll == 0 {
						m.clearUnreadState()
					}
				}
			}
		case tea.MouseButtonLeft:
			if action, ok := m.mouseActionAt(msg.X, msg.Y); ok {
				switch action.Kind {
				case "focus":
					switch action.Value {
					case "sidebar":
						m.focus = focusSidebar
					case "thread":
						m.focus = focusThread
					default:
						m.focus = focusMain
					}
					m.updateOverlaysForCurrentInput()
					return m, nil
				case "thread":
					m.threadPanelOpen = true
					m.threadPanelID = action.Value
					m.replyToID = action.Value
					m.focus = focusThread
					m.threadScroll = 0
					m.notice = fmt.Sprintf("Replying in thread %s", action.Value)
					return m, nil
				case "jump-latest":
					m.scroll = 0
					m.clearUnreadState()
					return m, nil
				case "autocomplete":
					if idx, ok := channelui.PopupActionIndex(action.Value); ok {
						for m.autocomplete.SelectedIndex() != idx {
							m.autocomplete.Next()
						}
						if name := m.autocomplete.Accept(); name != "" {
							return m.runActiveCommand("/" + name)
						}
					}
					return m, nil
				case "mention":
					if idx, ok := channelui.PopupActionIndex(action.Value); ok {
						for m.mention.SelectedIndex() != idx {
							m.mention.Next()
						}
						if mention := m.mention.Accept(); mention != "" {
							m.insertAcceptedMention(mention)
						}
					}
					return m, nil
				case "task":
					if task, ok := m.findTaskByID(action.Value); ok {
						m.focus = focusMain
						return m, m.openTaskActionPicker(task)
					}
					return m, nil
				case "request":
					if req, ok := m.findRequestByID(action.Value); ok {
						m.focus = focusMain
						return m, m.openRequestActionPicker(req)
					}
					return m, nil
				case "prompt":
					m.focus = focusMain
					m.applyRecoveryPrompt(action.Value)
					return m, nil
				case "channel", "app":
					items := m.sidebarItems()
					for idx, item := range items {
						if item.Kind == action.Kind && item.Value == action.Value {
							m.sidebarCursor = idx
							break
						}
					}
					m.focus = focusSidebar
					return m, m.selectSidebarItem(sidebarItem{Kind: action.Kind, Value: action.Value})
				}
			}
		}
		return m, nil

	case tea.KeyMsg:
		// ── Global keys (always active) ───────────────────────────────
		key := msg.String()
		if msg.Type == tea.KeyCtrlJ {
			key = "ctrl+j"
		}
		switch key {
		case "ctrl+c":
			now := time.Now()
			if !m.lastCtrlCAt.IsZero() && now.Sub(m.lastCtrlCAt) <= 2*time.Second {
				killTeamSession()
				return m, tea.Quit
			}
			m.lastCtrlCAt = now
			m.setTransientNotice("Press Ctrl+C again to quit WUPHF. Toby will file the exit paperwork.")
			return m, nil
		case "ctrl+b":
			if m.isOneOnOne() {
				return m, nil
			}
			m.sidebarCollapsed = !m.sidebarCollapsed
			return m, nil
		case "ctrl+g":
			if m.isOneOnOne() {
				m.setTransientNotice("1:1 mode: no sidebar, no distractions, no Toby. Ideal.")
				return m, nil
			}
			if m.quickJumpTarget == quickJumpChannels {
				m.quickJumpTarget = quickJumpNone
			} else {
				m.quickJumpTarget = quickJumpChannels
				m.setTransientNotice("Quick nav: 1-9 switches channels. Faster than Dwight in a fire drill.")
			}
			return m, nil
		case "ctrl+o":
			if m.isOneOnOne() {
				m.setTransientNotice("1:1 mode: just the direct conversation. Like a conference room with no Toby.")
				return m, nil
			}
			if m.quickJumpTarget == quickJumpApps {
				m.quickJumpTarget = quickJumpNone
			} else {
				m.quickJumpTarget = quickJumpApps
				m.setTransientNotice("Quick nav: 1-9 switches apps. Even faster than Stanley doing the crossword.")
			}
			return m, nil
		case "ctrl+d":
			// Return to #general from a DM channel.
			if chInfo := m.findChannelInfo(m.activeChannel); chInfo != nil && chInfo.IsDM() {
				m.activeChannel = "general"
				m.lastID = ""
				m.messages = nil
				m.setTransientNotice("Back to #general — the heart of the office.")
				return m, pollBroker("", m.activeChannel)
			}
			return m, nil
		}

		if m.quickJumpTarget != quickJumpNone {
			target := m.quickJumpTarget
			items := m.quickJumpItems()
			switch msg.String() {
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				idx := int(msg.String()[0] - '1')
				m.quickJumpTarget = quickJumpNone
				if idx >= 0 && idx < len(items) {
					m.setSidebarCursorForItem(items[idx])
					return m, m.selectSidebarItem(items[idx])
				}
				if target == quickJumpChannels {
					m.setTransientNotice("No channel on that number. Even Michael checks the directory first.")
				} else {
					m.setTransientNotice("No app on that number. Try a different one — WUPHF believes in you.")
				}
				return m, nil
			case "esc":
				m.quickJumpTarget = quickJumpNone
			default:
				m.quickJumpTarget = quickJumpNone
			}
		}

		// ── Esc: close overlays/thread, then cycle ────────────────────
		if msg.String() == "esc" {
			switch m.activeInteractionContext() {
			case contextConfirm:
				if m.confirm != nil && m.confirm.Action == channelui.ChannelConfirmActionSubmitRequest {
					m.confirm = nil
					m.notice = "Review closed. Keep editing before you send."
					return m, nil
				}
				m.confirm = nil
				m.notice = "Canceled."
				return m, nil
			case contextPicker:
				m.picker.SetActive(false)
				if m.pickerMode == channelPickerIntegrations {
					m.notice = "Integration canceled."
				} else {
					m.initFlow = tui.NewInitFlow()
					m.notice = "Setup canceled. Come back when you're ready. That's what she said."
				}
				m.pickerMode = channelPickerNone
				return m, nil
			case contextAutocomplete, contextMention:
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
				m.mention, _ = m.mention.Update(msg)
				return m, nil
			case contextMemberDraft:
				m.memberDraft = nil
				m.input = nil
				m.inputPos = 0
				m.notice = "Agent setup canceled."
				return m, nil
			case contextDoctor:
				m.doctor = nil
				m.notice = "Health check closed. The doctor says you're fine — or at least healthy enough to ship."
				return m, nil
			case contextInterview:
				req := *m.pending
				m.pending = nil
				m.input = nil
				m.inputPos = 0
				m.updateInputOverlays()
				m.posting = true
				m.notice = "Request canceled."
				return m, cancelRequest(req)
			case contextThread:
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.threadInput = nil
				m.threadInputPos = 0
				m.threadScroll = 0
				if m.focus == focusThread {
					m.focus = focusMain
				}
				return m, nil
			}
			// Nothing to close — fire human interrupt to pause the whole team
			if m.pending == nil {
				m.posting = true
				m.notice = "Pausing team..."
				return m, postHumanInterrupt(m.activeChannel)
			}
			return m, nil
		}

		// ── Tab: cycle focus 0→1→2→0 (only visible panels) ───────────
		if msg.String() == "tab" && !m.autocomplete.IsVisible() && !m.mention.IsVisible() && !m.picker.IsActive() {
			m.focus = m.nextFocus()
			m.quickJumpTarget = quickJumpNone
			m.updateOverlaysForCurrentInput()
			return m, nil
		}

		// ── Global overlays/pickers before panel-specific handling ────
		if m.confirm != nil {
			switch msg.String() {
			case "enter":
				return m.executeConfirmation(*m.confirm)
			case "ctrl+c", "esc":
				m.confirm = nil
				m.notice = "Canceled."
				return m, nil
			default:
				return m, nil
			}
		}
		if m.picker.IsActive() {
			var cmd tea.Cmd
			m.picker, cmd = m.picker.Update(msg)
			return m, cmd
		}
		if m.initFlow.IsActive() && m.initFlow.Phase() == tui.InitAPIKey {
			var cmd tea.Cmd
			m.initFlow, cmd = m.initFlow.Update(msg)
			return m, cmd
		}
		if m.autocomplete.IsVisible() {
			switch msg.String() {
			case "tab":
				if name := m.autocomplete.Accept(); name != "" {
					m.setActiveInput("/" + name + " ")
				}
				return m, nil
			case "enter":
				if name := m.autocomplete.Accept(); name != "" {
					return m.runActiveCommand("/" + name)
				}
			case "up", "down", "shift+tab":
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
				return m, nil
			default:
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
			}
		}
		if m.mention.IsVisible() {
			switch msg.String() {
			case "tab", "enter":
				if mention := m.mention.Accept(); mention != "" {
					m.insertAcceptedMention(mention)
				}
				return m, nil
			case "up", "down", "shift+tab":
				var cmd tea.Cmd
				m.mention, cmd = m.mention.Update(msg)
				_ = cmd
				return m, nil
			default:
				var cmd tea.Cmd
				m.mention, cmd = m.mention.Update(msg)
				_ = cmd
			}
		}

		if m.focus == focusMain && m.activeApp == channelui.OfficeAppCalendar && len(m.input) == 0 && !m.posting {
			switch msg.String() {
			case "d":
				m.calendarRange = channelui.CalendarRangeDay
				m.notice = "Calendar now shows today."
				return m, nil
			case "w":
				m.calendarRange = channelui.CalendarRangeWeek
				m.notice = "Calendar now shows this week."
				return m, nil
			case "f":
				options := m.buildCalendarAgentPickerOptions()
				if len(options) == 0 {
					m.notice = "No teammate filters available."
					return m, nil
				}
				m.picker = tui.NewPicker("Filter Calendar", options)
				m.picker.SetActive(true)
				m.pickerMode = channelPickerCalendarAgent
				return m, nil
			case "a":
				m.calendarFilter = ""
				m.notice = "Showing all teammate calendars."
				return m, nil
			}
		}

		// ── Route by focus area ───────────────────────────────────────
		if m.focus == focusThread && m.threadPanelOpen {
			return m.updateThread(msg)
		}
		if m.focus == focusSidebar && !m.sidebarCollapsed {
			return m.updateSidebar(msg)
		}

		// ── focusMain: existing behavior ──────────────────────────────
		if motionKey, ok := composerMotionKey(msg); ok {
			m.lastCtrlCAt = time.Time{}
			if nextPos, handled := channelui.MoveComposerCursor(m.input, m.inputPos, motionKey); handled {
				m.inputPos = nextPos
				m.updateInputOverlays()
			}
			return m, nil
		}
		switch msg.String() {
		case "enter":
			m.lastCtrlCAt = time.Time{}
			if m.memberDraft != nil {
				return m.submitMemberDraft()
			}
			if len(m.input) > 0 {
				text := string(m.input)
				trimmed := strings.TrimSpace(text)
				m.inputHistory.Record(m.input, m.inputPos)
				if trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q" {
					killTeamSession()
					return m, tea.Quit
				}
				if strings.HasPrefix(trimmed, "/") {
					return m.runActiveCommand(trimmed)
				}
				if m.pending != nil {
					m.confirm = channelui.ConfirmationForInterviewAnswer(*m.pending, m.selectedInterviewOption(), text)
					m.notice = "Review your answer before sending."
					return m, nil
				}

				m.input = nil
				m.inputPos = 0
				m.notice = ""
				m.posting = true
				return m, postToChannel(text, m.replyToID, m.activeChannel)
			}
			if m.pending != nil {
				if opt := m.selectedInterviewOption(); opt != nil {
					if channelui.InterviewOptionRequiresText(opt) {
						m.notice = channelui.InterviewOptionTextHint(opt)
						return m, nil
					}
					m.confirm = channelui.ConfirmationForInterviewAnswer(*m.pending, opt, "")
					m.notice = "Review your answer before sending."
					return m, nil
				}
				m.notice = "Choose an option or type your own answer before sending."
				return m, nil
			}
		case "backspace":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos > 0 {
				m.inputHistory.ResetRecall()
				m.input = append(m.input[:m.inputPos-1], m.input[m.inputPos:]...)
				m.inputPos--
				m.updateInputOverlays()
			}
		case "ctrl+u":
			m.lastCtrlCAt = time.Time{}
			m.inputHistory.ResetRecall()
			m.input = nil
			m.inputPos = 0
			m.updateInputOverlays()
		case "ctrl+p":
			m.lastCtrlCAt = time.Time{}
			if snapshot, ok := m.inputHistory.Previous(m.input, m.inputPos); ok {
				m.restoreMainSnapshot(snapshot)
			}
		case "ctrl+n":
			m.lastCtrlCAt = time.Time{}
			if snapshot, ok := m.inputHistory.Next(); ok {
				m.restoreMainSnapshot(snapshot)
			}
		case "ctrl+a":
			m.lastCtrlCAt = time.Time{}
			m.inputPos = 0
			m.updateInputOverlays()
		case "ctrl+e":
			m.lastCtrlCAt = time.Time{}
			m.inputPos = len(m.input)
			m.updateInputOverlays()
		case "ctrl+j":
			m.lastCtrlCAt = time.Time{}
			m.inputHistory.ResetRecall()
			ch := []rune{'\n'}
			tail := make([]rune, len(m.input[m.inputPos:]))
			copy(tail, m.input[m.inputPos:])
			m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
			m.inputPos++
			m.updateInputOverlays()
		case "left":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos > 0 {
				m.inputPos--
				m.updateInputOverlays()
			}
		case "right":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos < len(m.input) {
				m.inputPos++
				m.updateInputOverlays()
			}
		case "up":
			m.lastCtrlCAt = time.Time{}
			if m.pending != nil && m.selectedOption > 0 {
				m.selectedOption--
			} else {
				m.scroll++
			}
		case "down":
			m.lastCtrlCAt = time.Time{}
			if m.pending != nil && m.selectedOption < m.interviewOptionCount()-1 {
				m.selectedOption++
			} else {
				m.scroll--
				if m.scroll < 0 {
					m.scroll = 0
				}
			}
		case "home":
			m.lastCtrlCAt = time.Time{}
			m.scroll = 1 << 30
		case "end":
			m.lastCtrlCAt = time.Time{}
			m.scroll = 0
			m.clearUnreadState()
		case "pgup":
			m.lastCtrlCAt = time.Time{}
			m.scroll += channelui.MaxInt(10, m.height/2)
		case "pgdown":
			m.lastCtrlCAt = time.Time{}
			m.scroll -= channelui.MaxInt(10, m.height/2)
			if m.scroll < 0 {
				m.scroll = 0
			}
			if m.scroll == 0 {
				m.clearUnreadState()
			}
		default:
			m.lastCtrlCAt = time.Time{}
			if ch := composerInsertRunes(msg); len(ch) > 0 {
				m.inputHistory.ResetRecall()
				m.input, m.inputPos = channelui.InsertComposerRunes(m.input, m.inputPos, ch)
				m.updateInputOverlays()
			} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
				ch := msg.Runes
				if len(ch) == 0 {
					ch = []rune(msg.String())
				}
				if len(ch) > 0 {
					m.inputHistory.ResetRecall()
					tail := make([]rune, len(m.input[m.inputPos:]))
					copy(tail, m.input[m.inputPos:])
					m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
					m.inputPos += len(ch)
					m.updateInputOverlays()
				}
			}
			if m.maybeActivateChannelPickerFromInput() {
				return m, nil
			}
		}

	case channelPostDoneMsg:
		return m.handleChannelPostDoneMsg(msg)

	case channelInterviewAnswerDoneMsg:
		return m.handleChannelInterviewAnswerDoneMsg(msg)

	case channelCancelDoneMsg:
		return m.handleChannelCancelDoneMsg(msg)

	case channelInterruptDoneMsg:
		return m.handleChannelInterruptDoneMsg(msg)

	case channelResetDoneMsg:
		return m.handleChannelResetDoneMsg(msg)

	case channelResetDMDoneMsg:
		return m.handleChannelResetDMDoneMsg(msg)

	case channelDMCreatedMsg:
		return m.handleChannelDMCreatedMsg(msg)

	case channelInitDoneMsg:
		return m.handleChannelInitDoneMsg(msg)

	case channelIntegrationDoneMsg:
		return m.handleChannelIntegrationDoneMsg(msg)

	case channelDoctorDoneMsg:
		return m.handleChannelDoctorDoneMsg(msg)

	case telegramDiscoverMsg:
		return m.handleTelegramDiscoverMsg(msg)

	case openclawSessionsMsg:
		return m.handleOpenclawSessionsMsg(msg)

	case openclawConnectDoneMsg:
		return m.handleOpenclawConnectDoneMsg(msg)

	case telegramConnectDoneMsg:
		return m.handleTelegramConnectDoneMsg(msg)

	case channelMemberDraftDoneMsg:
		return m.handleChannelMemberDraftDoneMsg(msg)

	case channelTaskMutationDoneMsg:
		return m.handleChannelTaskMutationDoneMsg(msg)

	case channelMsg:
		return m.handleChannelMsg(msg)

	case channelMembersMsg:
		return m.handleChannelMembersMsg(msg)

	case channelOfficeMembersMsg:
		return m.handleChannelOfficeMembersMsg(msg)

	case channelChannelsMsg:
		return m.handleChannelChannelsMsg(msg)

	case channelUsageMsg:
		return m.handleChannelUsageMsg(msg)

	case channelHealthMsg:
		return m.handleChannelHealthMsg(msg)

	case channelTasksMsg:
		return m.handleChannelTasksMsg(msg)

	case channelSkillsMsg:
		return m.handleChannelSkillsMsg(msg)

	case channelActionsMsg:
		return m.handleChannelActionsMsg(msg)

	case channelSignalsMsg:
		return m.handleChannelSignalsMsg(msg)

	case channelDecisionsMsg:
		return m.handleChannelDecisionsMsg(msg)

	case channelWatchdogsMsg:
		return m.handleChannelWatchdogsMsg(msg)

	case channelSchedulerMsg:
		return m.handleChannelSchedulerMsg(msg)

	case tui.PickerSelectMsg:
		return m.handlePickerSelectMsg(msg)

	case tui.InitFlowMsg:
		return m.handleInitFlowMsg(msg)

	case channelRequestsMsg:
		return m.handleChannelRequestsMsg(msg)

	case channelTickMsg:
		return m.handleChannelTickMsg(msg)
	}

	return m, nil
}

// View() moved to channel_view.go.

// Header rendering queries (currentHeaderTitle, currentHeaderMeta,
// currentAppLabel, currentMainLines) moved to channel_header.go.
// Pure state queries (visiblePendingRequest, composerTargetLabel,
// recommendedOptionIndex, interviewOptionCount, selectedInterviewOption,
// nextFocus) moved to channel_state_queries.go.

// Mouse hit-testing (mouseAction type, mouseActionAt, mainPanelMouseAction),
// main-panel geometry (mainPanelGeometry), and the recovery-prompt click
// handler (applyRecoveryPrompt) all moved to channel_mouse.go.

// Sidebar state, items, cursor, selection, and the updateSidebar key
// handler all live in channel_sidebar_state.go.

// Picker option builders moved to channel_pickers.go.

// Lookup helpers + request-action dispatch moved to channel_lookups.go.

// Composer input helpers and thread keymap moved to channel_composer_input.go.

// runActiveCommand, runCommand, and maybeActivateChannelPickerFromInput moved
// to channel_commands.go.

func (m channelModel) renderActivePopup(width int) string {
	if width < 24 {
		width = 24
	}
	if m.autocomplete.IsVisible() {
		var options []channelui.ComposerPopupOption
		for _, cmd := range m.autocomplete.Matches() {
			meta := cmd.Description
			if strings.TrimSpace(cmd.Category) != "" {
				meta = strings.ToUpper(cmd.Category) + " · " + meta
			}
			options = append(options, channelui.ComposerPopupOption{
				Label: "/" + cmd.Name,
				Meta:  meta,
			})
		}
		return channelui.RenderComposerPopup(options, m.autocomplete.SelectedIndex(), width, channelui.SlackActive)
	}
	if m.mention.IsVisible() {
		var options []channelui.ComposerPopupOption
		for _, ag := range m.mention.Matches() {
			options = append(options, channelui.ComposerPopupOption{
				Label: "@" + ag.Slug,
				Meta:  ag.Name,
			})
		}
		return channelui.RenderComposerPopup(options, m.mention.SelectedIndex(), width, "#2BAC76")
	}
	return ""
}

func (m channelModel) currentChannelInfo() *channelui.ChannelInfo {
	return m.findChannelInfo(m.activeChannel)
}

func (m channelModel) findChannelInfo(slug string) *channelui.ChannelInfo {
	for i := range m.channels {
		if m.channels[i].Slug == slug {
			return &m.channels[i]
		}
	}
	return nil
}

func (m channelModel) buildChannelPickerOptions() []tui.PickerOption {
	var options []tui.PickerOption
	for _, ch := range m.channels {
		description := strings.TrimSpace(ch.Description)
		if description == "" {
			description = fmt.Sprintf("%d members", len(ch.Members))
		} else {
			description = fmt.Sprintf("%s · %d members", description, len(ch.Members))
		}
		options = append(options, tui.PickerOption{
			Label:       "#" + ch.Slug,
			Value:       "switch:" + ch.Slug,
			Description: description,
		})
		if ch.Slug != "general" {
			options = append(options, tui.PickerOption{
				Label:       "Remove #" + ch.Slug,
				Value:       "remove:" + ch.Slug,
				Description: "Delete this channel and its messages/tasks",
			})
		}
	}
	return options
}

func (m channelModel) buildSwitchChannelPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{
		{Label: "Main office feed", Value: "app:messages", Description: "Return to the shared message stream"},
		{Label: "Tasks", Value: "app:tasks", Description: "Review active work for this channel"},
		{Label: "Requests", Value: "app:requests", Description: "Open pending approvals and interviews"},
		{Label: "Policies", Value: "app:policies", Description: "Show signals, decisions, and watchdogs"},
		{Label: "Calendar", Value: "app:calendar", Description: "View the office schedule and teammate calendars"},
	}
	if m.isOneOnOne() {
		options = append(options, tui.PickerOption{
			Label:       "Return to main office",
			Value:       "session:office",
			Description: "Leave direct mode and restore the shared office session",
		})
	} else {
		for _, member := range m.officeMembers {
			name := strings.TrimSpace(member.Name)
			if name == "" {
				name = channelui.DisplayName(member.Slug)
			}
			options = append(options, tui.PickerOption{
				Label:       "1:1 with " + name,
				Value:       "session:1o1:" + member.Slug,
				Description: "Jump into a direct session with " + name,
			})
		}
	}
	for _, option := range m.buildChannelPickerOptions() {
		if strings.HasPrefix(option.Value, "switch:") {
			options = append(options, option)
		}
	}
	return options
}

func (m channelModel) buildAgentPickerOptions() []tui.PickerOption {
	ch := m.currentChannelInfo()
	if ch == nil {
		return nil
	}
	officeMap := make(map[string]channelui.OfficeMember, len(m.officeMembers))
	for _, member := range m.officeMembers {
		officeMap[member.Slug] = member
	}
	disabled := make(map[string]bool, len(ch.Disabled))
	for _, slug := range ch.Disabled {
		disabled[slug] = true
	}
	var options []tui.PickerOption
	for _, slug := range ch.Members {
		name := channelui.DisplayName(slug)
		if meta, ok := officeMap[slug]; ok && meta.Name != "" {
			name = meta.Name
		}
		if slug != "ceo" && disabled[slug] {
			options = append(options, tui.PickerOption{
				Label:       "Enable " + name,
				Value:       "enable:" + slug,
				Description: "Allow this teammate to participate in #" + m.activeChannel,
			})
		} else if slug != "ceo" {
			options = append(options, tui.PickerOption{
				Label:       "Disable " + name,
				Value:       "disable:" + slug,
				Description: "Keep them in the channel but stop notifications there",
			})
		}
		if slug != "ceo" {
			options = append(options, tui.PickerOption{
				Label:       "Remove " + name,
				Value:       "remove:" + slug,
				Description: "Take them out of #" + m.activeChannel,
			})
		}
	}
	for _, member := range m.officeMembers {
		slug := member.Slug
		found := false
		for _, member := range ch.Members {
			if member == slug {
				found = true
				break
			}
		}
		if !found {
			options = append(options, tui.PickerOption{
				Label:       "Add " + member.Name,
				Value:       "add:" + slug,
				Description: "Add them to #" + m.activeChannel,
			})
		}
		if !member.BuiltIn {
			options = append(options, tui.PickerOption{
				Label:       "Edit " + member.Name,
				Value:       "edit:" + slug,
				Description: "Update role, expertise, personality, and permissions",
			})
		}
	}
	options = append(options, tui.PickerOption{
		Label:       "Create new office member…",
		Value:       "create:new",
		Description: "Use /agent create <slug> <Display Name> to add a brand-new teammate",
	})
	return options
}

func (m channelModel) buildOneOnOneModePickerOptions() []tui.PickerOption {
	enableDescription := "Restart WUPHF in direct mode with one selected agent and kill the rest of the Claude sessions"
	if m.isOneOnOne() {
		enableDescription = "Pick a different single agent for this direct session"
	}
	disableDescription := "Restart WUPHF with the full office team"
	if !m.isOneOnOne() {
		disableDescription = "Already using the full office team"
	}
	return []tui.PickerOption{
		{
			Label:       "Enable 1:1 mode",
			Value:       "enable",
			Description: enableDescription,
		},
		{
			Label:       "Disable 1:1 mode",
			Value:       "disable",
			Description: disableDescription,
		},
	}
}

func (m channelModel) buildOneOnOneAgentPickerOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(m.officeMembers))
	for _, member := range m.officeMembers {
		name := member.Name
		if strings.TrimSpace(name) == "" {
			name = channelui.DisplayName(member.Slug)
		}
		description := strings.TrimSpace(member.Role)
		if description == "" {
			description = "Direct session with " + name
		}
		options = append(options, tui.PickerOption{
			Label:       name,
			Value:       member.Slug,
			Description: description,
		})
	}
	return options
}

func (m channelModel) buildCalendarAgentPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{{
		Label:       "All teammates",
		Value:       "all",
		Description: "Show every participant across the office calendar",
	}}
	for _, member := range m.members {
		name := member.Name
		if strings.TrimSpace(name) == "" {
			name = channelui.DisplayName(member.Slug)
		}
		description := member.Role
		if strings.TrimSpace(description) == "" {
			description = "Show only " + name + "'s calendar"
		}
		options = append(options, tui.PickerOption{
			Label:       name,
			Value:       member.Slug,
			Description: description,
		})
	}
	return options
}

// HTTP/network commands moved to channel_broker.go.

// Process lifecycle helpers moved to channel_lifecycle.go.
