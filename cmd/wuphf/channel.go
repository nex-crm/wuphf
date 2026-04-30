package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	case channelInterviewAnswerDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Request answer failed: " + msg.err.Error()
		} else {
			m.pending = nil
			m.input = nil
			m.inputPos = 0
			return m, tea.Batch(pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
		}

	case channelCancelDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Request cancel failed: " + msg.err.Error()
			return m, tea.Batch(pollRequests(m.activeChannel), pollBroker(m.lastID, m.activeChannel))
		} else {
			if m.pending != nil && m.pending.ID == msg.requestID {
				m.pending = nil
				m.input = nil
				m.inputPos = 0
				m.updateInputOverlays()
			}
			return m, tea.Batch(pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
		}

	case channelInterruptDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Failed to pause team: " + msg.err.Error()
		} else {
			m.notice = "Team paused. Answer the interrupt to resume."
		}
		return m, tea.Batch(pollRequests(m.activeChannel), pollBroker(m.lastID, m.activeChannel))

	case channelResetDoneMsg:
		m.posting = false
		m.confirm = nil
		if msg.err == nil {
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
		} else {
			m.notice = "Reset failed: " + msg.err.Error()
		}

	case channelResetDMDoneMsg:
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

	case channelDMCreatedMsg:
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

	case channelInitDoneMsg:
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

	case channelIntegrationDoneMsg:
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

	case channelDoctorDoneMsg:
		if msg.err != nil {
			m.notice = "Doctor failed: " + msg.err.Error()
			m.doctor = nil
		} else {
			report := msg.report
			m.doctor = &report
			m.notice = "Doctor: " + report.StatusLine()
		}

	case telegramDiscoverMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Telegram error: " + msg.err.Error()
			return m, nil
		}
		m.telegramToken = msg.token

		// Merge discovered groups with existing manifest channels
		allGroups := msg.groups
		manifest, _ := company.LoadManifest()
		for _, ch := range manifest.Channels {
			if ch.Surface == nil || ch.Surface.Provider != "telegram" || ch.Surface.RemoteID == "" || ch.Surface.RemoteID == "0" {
				continue
			}
			// Check if already discovered
			found := false
			for _, g := range allGroups {
				if fmt.Sprintf("%d", g.ChatID) == ch.Surface.RemoteID {
					found = true
					break
				}
			}
			if !found {
				chatID, _ := strconv.ParseInt(ch.Surface.RemoteID, 10, 64)
				if chatID != 0 {
					title := ch.Surface.RemoteTitle
					if title == "" {
						title = ch.Name
					}
					allGroups = append(allGroups, team.TelegramGroup{
						ChatID: chatID,
						Title:  title,
						Type:   "group",
					})
				}
			}
		}
		m.telegramGroups = allGroups

		// Build picker: DM + discovered groups + manual group entry
		options := []tui.PickerOption{
			{Label: "Direct message with Telegram bot", Value: "dm", Description: "Anyone can DM the bot to reach the office"},
		}
		for _, g := range allGroups {
			options = append(options, tui.PickerOption{
				Label:       g.Title,
				Value:       fmt.Sprintf("%d", g.ChatID),
				Description: fmt.Sprintf("Shared %s channel", g.Type),
			})
		}
		if len(allGroups) == 0 {
			options = append(options, tui.PickerOption{
				Label:       "Waiting for groups...",
				Value:       "retry",
				Description: "Add the bot to a Telegram group and send a message, then try again",
			})
		}
		m.picker = tui.NewPicker(fmt.Sprintf("Bot \"%s\" verified. Choose how to connect:", msg.botName), options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerTelegramGroup
		return m, nil

	case openclawSessionsMsg:
		m.posting = false
		if msg.err != nil {
			options := []tui.PickerOption{
				{Label: "Retry with different gateway URL", Value: "retry-url", Description: "Go back and change the URL/token"},
			}
			m.picker = tui.NewPicker(fmt.Sprintf("OpenClaw dial failed: %s", msg.err.Error()), options)
			m.picker.SetActive(true)
			m.pickerMode = channelPickerOpenclawSession
			m.notice = "OpenClaw connect failed: " + msg.err.Error()
			return m, nil
		}
		m.openclawSessions = msg.sessions
		if len(msg.sessions) == 0 {
			m.notice = "OpenClaw gateway returned no sessions. Start one in OpenClaw and retry /connect openclaw."
			return m, nil
		}
		options := make([]tui.PickerOption, 0, len(msg.sessions))
		for _, s := range msg.sessions {
			label := s.Label
			if label == "" {
				label = s.SessionKey
			}
			desc := s.Preview
			options = append(options, tui.PickerOption{
				Label:       label,
				Value:       s.SessionKey,
				Description: desc,
			})
		}
		m.picker = tui.NewPicker("Pick an OpenClaw session to bridge:", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerOpenclawSession
		m.notice = fmt.Sprintf("Found %d OpenClaw session(s). Pick one to bridge.", len(msg.sessions))
		return m, nil

	case openclawConnectDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "OpenClaw connect failed: " + msg.err.Error()
			return m, nil
		}
		m.notice = fmt.Sprintf("@%s is now in the office", msg.slug)
		return m, nil

	case telegramConnectDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Telegram connect failed: " + msg.err.Error()
			return m, nil
		}
		m.notice = fmt.Sprintf("Connected \"%s\" as #%s. Restart WUPHF to activate the Telegram bridge.", msg.groupTitle, msg.channelSlug)
		m.activeChannel = msg.channelSlug
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
		manifest, _ := company.LoadManifest()
		m.channels = channelui.ChannelInfosFromManifest(manifest)
		return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollChannels())

	case channelMemberDraftDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Agent update failed: " + msg.err.Error()
		} else {
			m.notice = msg.notice
			m.memberDraft = nil
			m.input = nil
			m.inputPos = 0
			return m, tea.Batch(pollOfficeMembers(), pollChannels(), pollMembers(m.activeChannel), pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
		}

	case channelTaskMutationDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Task update failed: " + msg.err.Error()
		} else if strings.TrimSpace(msg.notice) != "" {
			m.notice = msg.notice
		}
		return m, tea.Batch(pollTasks(m.activeChannel), pollOfficeLedger())

	case channelMsg:
		if len(msg.messages) > 0 {
			hadHistory := m.lastID != ""
			uniqueMessages, added := channelui.AppendUniqueMessages(m.messages, msg.messages)
			if added == 0 {
				break
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

	case channelMembersMsg:
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

	case channelOfficeMembersMsg:
		if len(msg.members) == 0 {
			msg.members = channelui.OfficeMembersFallback(m.officeMembers)
		}
		m.officeMembers = msg.members
		channelui.SetOfficeDirectory(msg.members)
		m.updateOverlaysForCurrentInput()

	case channelChannelsMsg:
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

	case channelUsageMsg:
		m.usage = msg.usage
		if m.usage.Agents == nil {
			m.usage.Agents = make(map[string]channelui.UsageTotals)
		}

	case channelHealthMsg:
		m.brokerConnected = msg.Connected
		if msg.Connected {
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
		}

	case channelTasksMsg:
		m.tasks = msg.tasks

	case channelSkillsMsg:
		m.skills = msg.skills
		m.refreshSlashCommands()
		return m, nil

	case channelActionsMsg:
		m.actions = msg.actions

	case channelSignalsMsg:
		m.signals = msg.signals

	case channelDecisionsMsg:
		m.decisions = msg.decisions

	case channelWatchdogsMsg:
		m.watchdogs = msg.alerts

	case channelSchedulerMsg:
		m.scheduler = msg.jobs

	case tui.PickerSelectMsg:
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
					return m.focusRequest(req, "Focused request "+req.ID)
				}
			case "answer":
				if req, ok := m.findRequestByID(reqID); ok {
					return m.answerRequest(req)
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

	case tui.InitFlowMsg:
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

	case channelRequestsMsg:
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

	case channelTickMsg:
		m.tickFrame++
		if m.notice != "" && !m.noticeExpireAt.IsZero() && time.Now().After(m.noticeExpireAt) {
			m.notice = ""
			m.noticeExpireAt = time.Time{}
		}
		return m, m.pollCurrentState()
	}

	return m, nil
}

func (m channelModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	layout := channelui.ComputeLayout(m.width, m.height, m.threadPanelOpen && !m.isOneOnOne(), m.sidebarCollapsed || m.isOneOnOne())
	workspaceState := m.currentWorkspaceUIState()

	// ── Sidebar ──────────────────────────────────────────────────────
	sidebar := ""
	if layout.ShowSidebar && !m.isOneOnOne() {
		sidebar = cachedSidebarRender(m.channels, channelui.MergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo()), m.tasks, m.activeChannel, m.activeApp, m.sidebarCursor, m.sidebarRosterOffset, m.focus == focusSidebar, m.quickJumpTarget, workspaceState, layout.SidebarW, layout.ContentH, m.onboardingChecklist)
	}

	// ── Thread panel ─────────────────────────────────────────────────
	thread := ""
	if layout.ShowThread && !m.isOneOnOne() {
		threadPopup := ""
		if m.focus == focusThread {
			threadPopup = m.renderActivePopup(channelui.MaxInt(layout.ThreadW-4, 24))
		}
		thread = renderThreadPanel(m.messages, m.threadPanelID,
			layout.ThreadW, layout.ContentH,
			m.threadInput, m.threadInputPos, m.threadScroll,
			threadPopup, m.focus == focusThread, m.threadInputHistory.Len() > 0)
	}

	activePending := m.visiblePendingRequest()
	// ── Main panel: header + messages + composer ─────────────────────
	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}

	// Channel header (2 lines)
	headerStyle := channelui.ChannelHeaderStyle(mainW)
	headerLine1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).
		Render(channelui.AppIcon(m.activeApp) + " " + m.currentHeaderTitle())
	headerMeta := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted)).
		Render(m.currentHeaderMeta())
	if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceLabel := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceLabel = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(channelui.SlackActive)).
			Render(fmt.Sprintf("Session %s · %s  Total %s · %s%s",
				channelui.FormatUSD(m.usage.Session.CostUsd),
				channelui.FormatTokenCount(m.usage.Session.TotalTokens),
				channelui.FormatUSD(m.usage.Total.CostUsd),
				channelui.FormatTokenCount(m.usage.Total.TotalTokens),
				sinceLabel,
			))
	}
	if m.activeApp == channelui.OfficeAppMessages && m.unreadCount > 0 && m.scroll > 0 {
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(channelui.SlackActive)).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("%d new", m.unreadCount))
		if awaySummary := m.currentAwaySummary(); strings.TrimSpace(awaySummary) != "" {
			headerMeta += "  " + lipgloss.NewStyle().
				Foreground(lipgloss.Color("#BFDBFE")).
				Render(awaySummary)
		}
	}
	if m.pending != nil {
		headerMeta += "  " + channelui.AccentPill("request pending", "#B45309")
	} else if len(m.requests) > 0 {
		headerMeta += "  " + channelui.SubtlePill(fmt.Sprintf("%d open requests", len(m.requests)), "#FDE68A", "#78350F")
	}
	channelHeader := headerStyle.Render(headerLine1 + headerMeta)
	if usageLine := channelui.RenderUsageStrip(m.usage, m.members, mainW); usageLine != "" {
		channelHeader += "\n" + usageLine
	}
	headerH := lipgloss.Height(channelHeader)
	runtimeStrip := ""
	if m.activeApp == channelui.OfficeAppMessages || m.isOneOnOne() {
		focusSlug := ""
		if m.isOneOnOne() {
			focusSlug = m.oneOnOneAgentSlug()
		}
		runtimeStrip = channelui.RenderRuntimeStrip(m.members, m.tasks, m.requests, m.actions, mainW-4, focusSlug)
	}
	runtimeH := lipgloss.Height(runtimeStrip)

	// Composer
	typingAgents := channelui.TypingAgentsFromMembers(m.members)
	liveActivities := channelui.LiveActivityFromMembers(m.members)
	composerStr := renderComposer(mainW, m.input, m.inputPos, m.composerTargetLabel(),
		m.replyToID, typingAgents, liveActivities, activePending, m.selectedOption, m.composerHint(m.composerTargetLabel(), m.replyToID, activePending),
		m.focus == focusMain, m.tickFrame)
	if m.memberDraft != nil {
		composerStr = renderComposer(mainW, m.input, m.inputPos, memberDraftComposerLabel(*m.memberDraft),
			"", typingAgents, nil, nil, 0, m.composerHint(memberDraftComposerLabel(*m.memberDraft), "", nil), m.focus == focusMain, m.tickFrame)
	}

	// Interview card (above composer)
	interviewCard := ""
	if activePending != nil {
		interviewCard = channelui.RenderInterviewCard(*activePending, m.selectedOption, m.interviewPhaseTitle(), mainW-4)
	}
	memberDraftCard := ""
	if m.memberDraft != nil {
		memberDraftCard = renderMemberDraftCard(*m.memberDraft, mainW-4)
	}
	doctorCard := ""
	if m.doctor != nil {
		doctorCard = channelui.RenderDoctorCard(*m.doctor, mainW-4)
	}
	confirmCard := ""
	if m.confirm != nil {
		confirmCard = channelui.RenderConfirmCard(*m.confirm, mainW-4)
	}

	// Init/picker overlays
	initPanel := ""
	if confirmCard != "" {
		initPanel = confirmCard
	} else if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}

	composerH := lipgloss.Height(composerStr)
	interviewH := lipgloss.Height(interviewCard)
	memberDraftH := lipgloss.Height(memberDraftCard)
	doctorH := lipgloss.Height(doctorCard)
	initH := lipgloss.Height(initPanel)

	// Message area height
	msgH := layout.ContentH - headerH - runtimeH - composerH - interviewH - memberDraftH - doctorH - initH - 1 // 1 for status bar
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	allLines := m.currentMainViewportLines(contentWidth, msgH)
	visibleRows, scroll, _, _ := channelui.SliceRenderedLines(allLines, msgH, m.scroll)
	var visible []string
	for _, row := range visibleRows {
		visible = append(visible, row.Text)
	}
	for len(visible) < msgH {
		visible = append(visible, "")
	}
	if m.activeApp == channelui.OfficeAppMessages && m.unreadCount > 0 && scroll > 0 && len(visible) > 0 {
		visible[0] = channelui.RenderAwayStrip(contentWidth, m.unreadCount, m.currentAwaySummary())
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		visible = channelui.OverlayBottomLines(visible, strings.Split(popup, "\n"))
	}

	msgPanel := channelui.MainPanelStyle(mainW, msgH).Render(strings.Join(visible, "\n"))

	// Assemble main column
	mainParts := []string{channelHeader}
	if runtimeStrip != "" {
		mainParts = append(mainParts, runtimeStrip)
	}
	mainParts = append(mainParts, msgPanel)
	if interviewCard != "" {
		mainParts = append(mainParts, interviewCard)
	}
	if memberDraftCard != "" {
		mainParts = append(mainParts, memberDraftCard)
	}
	if doctorCard != "" {
		mainParts = append(mainParts, doctorCard)
	}
	if initPanel != "" {
		mainParts = append(mainParts, initPanel)
	}
	if m.activeApp == channelui.OfficeAppMessages || m.memberDraft != nil {
		mainParts = append(mainParts, composerStr)
	}
	mainCol := strings.Join(mainParts, "\n")

	// ── Compose 3 columns ────────────────────────────────────────────
	border := channelui.RenderVerticalBorder(layout.ContentH, channelui.SlackBorder)
	var panels []string
	if sidebar != "" {
		panels = append(panels, sidebar, border)
	}
	panels = append(panels, mainCol)
	if thread != "" {
		panels = append(panels, border, thread)
	}

	content := lipgloss.NewStyle().MaxWidth(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, panels...))

	// ── Status bar ───────────────────────────────────────────────────
	onlineCount := len(m.members)
	scrollHint := "PgUp/PgDn"
	if scroll > 0 {
		scrollHint = fmt.Sprintf("%d above", scroll)
	}
	var statusBar string
	if m.pending != nil {
		statusText := m.interviewStatusLine()
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Render(statusText),
		)
	} else if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceStatus := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceStatus = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		statusBar = channelui.StatusBarStyle(m.width).Render(fmt.Sprintf(
			" %s %d online │ session %s · %s │ total %s · %s%s │ %s │ Ctrl+J newline │ /doctor",
			"\u25CF", onlineCount,
			channelui.FormatUSD(m.usage.Session.CostUsd), channelui.FormatTokenCount(m.usage.Session.TotalTokens),
			channelui.FormatUSD(m.usage.Total.CostUsd), channelui.FormatTokenCount(m.usage.Total.TotalTokens),
			sinceStatus, scrollHint,
		))
	} else if m.quickJumpTarget != quickJumpNone {
		label := "channels"
		if m.quickJumpTarget == quickJumpApps {
			label = "apps"
		}
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				fmt.Sprintf(" Quick nav │ Ctrl+G channels · Ctrl+O apps │ 1-9 switch %s │ Esc cancel", label),
			),
		)
	} else if m.notice != "" {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(" " + m.notice),
		)
	} else if m.isOneOnOne() {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				workspaceState.DefaultStatusLine(scrollHint),
			),
		)
	} else if !m.brokerConnected {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render(workspaceState.DefaultStatusLine(scrollHint)),
		)
	} else if m.replyToID != "" {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				fmt.Sprintf(" ↩ Reply mode │ thread %s │ Ctrl+J newline │ /cancel to return", m.replyToID),
			),
		)
	} else if m.activeApp != channelui.OfficeAppMessages {
		message := fmt.Sprintf(" Viewing %s │ %s │ /messages to return │ /doctor", m.currentAppLabel(), scrollHint)
		if m.activeApp == channelui.OfficeAppCalendar {
			filter := "all"
			if strings.TrimSpace(m.calendarFilter) != "" {
				filter = "@" + m.calendarFilter
			}
			message = fmt.Sprintf(" Calendar │ d day · w week · f filter · a all │ current %s/%s", m.calendarRange, filter)
		}
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				message,
			),
		)
	} else {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(workspaceState.DefaultStatusLine(scrollHint)),
		)
	}

	return content + "\n" + statusBar
}

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

// Mouse hit-testing (mouseAction type, mouseActionAt, mainPanelMouseAction),
// main-panel geometry (mainPanelGeometry), and the recovery-prompt click
// handler (applyRecoveryPrompt) all moved to channel_mouse.go.

func (m channelModel) visiblePendingRequest() *channelui.Interview {
	if m.pending == nil {
		return nil
	}
	if m.pending.Channel != "" && m.pending.Channel != m.activeChannel {
		return nil
	}
	return m.pending
}

func (m channelModel) composerTargetLabel() string {
	if m.isOneOnOne() {
		return "1:1 with " + m.oneOnOneAgentName()
	}
	if chInfo := m.currentChannelInfo(); chInfo != nil && chInfo.IsDM() {
		name := chInfo.Name
		if name == "" {
			name = chInfo.Slug
		}
		return "DM→" + name
	}
	return m.activeChannel
}

func (m channelModel) recommendedOptionIndex() int {
	if m.pending == nil {
		return 0
	}
	for i, option := range m.pending.Options {
		if option.ID == m.pending.RecommendedID {
			return i
		}
	}
	return 0
}

func (m channelModel) interviewOptionCount() int {
	if m.pending == nil {
		return 0
	}
	return len(m.pending.Options) + 1
}

func (m channelModel) selectedInterviewOption() *channelui.InterviewOption {
	if m.pending == nil {
		return nil
	}
	if len(m.pending.Options) == 0 {
		return nil
	}
	if m.selectedOption < 0 {
		return &m.pending.Options[0]
	}
	if m.selectedOption >= len(m.pending.Options) {
		return nil
	}
	return &m.pending.Options[m.selectedOption]
}

// nextFocus cycles through visible panels: main → sidebar → thread → main.
func (m channelModel) nextFocus() focusArea {
	order := []focusArea{focusMain}
	if !m.sidebarCollapsed {
		order = append(order, focusSidebar)
	}
	if m.threadPanelOpen {
		order = append(order, focusThread)
	}
	for i, f := range order {
		if f == m.focus {
			return order[(i+1)%len(order)]
		}
	}
	return focusMain
}

// updateThread handles key events when the thread panel is focused.
func (m channelModel) updateThread(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if motionKey, ok := composerMotionKey(msg); ok {
		if nextPos, handled := channelui.MoveComposerCursor(m.threadInput, m.threadInputPos, motionKey); handled {
			m.threadInputPos = nextPos
			m.updateThreadOverlays()
		}
		return m, nil
	}
	key := msg.String()
	if msg.Type == tea.KeyCtrlJ {
		key = "ctrl+j"
	}
	switch key {
	case "enter":
		if len(m.threadInput) > 0 {
			text := string(m.threadInput)
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "/") {
				m.threadInputHistory.Record(m.threadInput, m.threadInputPos)
				return m.runCommand(trimmed, m.threadPanelID)
			}
			m.threadInputHistory.Record(m.threadInput, m.threadInputPos)
			m.threadInput = nil
			m.threadInputPos = 0
			m.posting = true
			return m, postToChannel(text, m.threadPanelID, m.activeChannel)
		}
	case "backspace":
		if m.threadInputPos > 0 {
			m.threadInputHistory.ResetRecall()
			m.threadInput = append(m.threadInput[:m.threadInputPos-1], m.threadInput[m.threadInputPos:]...)
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "ctrl+u":
		m.threadInputHistory.ResetRecall()
		m.threadInput = nil
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+p":
		if snapshot, ok := m.threadInputHistory.Previous(m.threadInput, m.threadInputPos); ok {
			m.restoreThreadSnapshot(snapshot)
		}
	case "ctrl+n":
		if snapshot, ok := m.threadInputHistory.Next(); ok {
			m.restoreThreadSnapshot(snapshot)
		}
	case "ctrl+a":
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+e":
		m.threadInputPos = len(m.threadInput)
		m.updateThreadOverlays()
	case "ctrl+j":
		m.threadInputHistory.ResetRecall()
		ch := []rune{'\n'}
		tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
		copy(tail, m.threadInput[m.threadInputPos:])
		m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
		m.threadInputPos++
		m.updateThreadOverlays()
	case "left":
		if m.threadInputPos > 0 {
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "right":
		if m.threadInputPos < len(m.threadInput) {
			m.threadInputPos++
			m.updateThreadOverlays()
		}
	case "up":
		m.threadScroll++
	case "down":
		m.threadScroll--
		if m.threadScroll < 0 {
			m.threadScroll = 0
		}
	case "pgup":
		m.threadScroll += 5
	case "pgdown":
		m.threadScroll -= 5
		if m.threadScroll < 0 {
			m.threadScroll = 0
		}
	default:
		if ch := composerInsertRunes(msg); len(ch) > 0 {
			m.threadInputHistory.ResetRecall()
			m.threadInput, m.threadInputPos = channelui.InsertComposerRunes(m.threadInput, m.threadInputPos, ch)
			m.updateThreadOverlays()
		} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
			ch := msg.Runes
			if len(ch) == 0 {
				ch = []rune(msg.String())
			}
			if len(ch) > 0 {
				m.threadInputHistory.ResetRecall()
				tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
				copy(tail, m.threadInput[m.threadInputPos:])
				m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
				m.threadInputPos += len(ch)
				m.updateThreadOverlays()
			}
		}
	}
	return m, nil
}

// Sidebar state, items, cursor, selection, and the updateSidebar key
// handler all live in channel_sidebar_state.go.

// Picker option builders moved to channel_pickers.go.

// Lookup helpers + request-action dispatch moved to channel_lookups.go.

// Composer overlay management + composer motion/insert helpers moved
// to channel_input.go.

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

// HTTP/network commands moved to channel_client.go.

func tickChannel() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return channelTickMsg(t)
	})
}

// killTeamSession kills the entire wuphf-team tmux session and all agent processes.
func killTeamSession() {
	// Best-effort cleanup at process exit; cap each step so a hung tmux or
	// broker doesn't keep us alive forever.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Kill tmux session (kills all agent processes in all panes/windows)
	_ = exec.CommandContext(ctx, "tmux", "-L", "wuphf", "kill-session", "-t", "wuphf-team").Run()
	// Stop the broker
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerURL("/health"), nil)
	if err != nil {
		return
	}
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

func runChannelView(threadsCollapsed bool, initialApp channelui.OfficeApp, skipSplash bool) {
	defer func() {
		if r := recover(); r != nil {
			reportChannelCrash(fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack()))
		}
	}()

	// Check if onboarding is needed before launching the channel view.
	if os.Getenv("WUPHF_SKIP_ONBOARDING") == "" {
		state, err := fetchOnboardingState(brokerBaseURL())
		if err == nil && !state.Onboarded {
			om := newOnboardingModel(brokerBaseURL(), 0, 0)
			op := tea.NewProgram(om, tea.WithAltScreen())
			if _, err := op.Run(); err != nil {
				reportChannelCrash(fmt.Sprintf("onboarding error: %v\n", err))
				return
			}
			// Fall through to channel view after onboarding completes.
		}
	}

	if !skipSplash && os.Getenv("WUPHF_NO_SPLASH") == "" {
		splash := tea.NewProgram(newSplashModel(), tea.WithAltScreen())
		if _, err := splash.Run(); err != nil {
			reportChannelCrash(fmt.Sprintf("splash error: %v\n", err))
			return
		}
	}

	p := tea.NewProgram(newChannelModelWithApp(threadsCollapsed, initialApp), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		reportChannelCrash(fmt.Sprintf("channel view error: %v\n", err))
	}
}

func reportChannelCrash(details string) {
	_ = channelui.AppendChannelCrashLog(details)
	fmt.Fprintln(os.Stderr, "WUPHF channel crashed.")
	fmt.Fprintln(os.Stderr, "Log:", channelui.ChannelCrashLogPath())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The rest of the team is still running.")
	if strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")) != "" {
		fmt.Fprintln(os.Stderr, "Restart WUPHF when ready to reconnect to the headless office runtime.")
	} else {
		fmt.Fprintln(os.Stderr, "Use `tmux -L wuphf attach -t wuphf-team` to inspect panes,")
		fmt.Fprintln(os.Stderr, "then restart WUPHF when ready.")
	}
	select {}
}
