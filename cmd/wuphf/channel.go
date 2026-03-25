package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/setup"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

type channelMsg struct {
	messages []brokerMessage
}

type channelMembersMsg struct {
	members []channelMember
}

type channelInterviewMsg struct {
	pending *channelInterview
}

type channelUsageMsg struct {
	usage channelUsageState
}

type brokerMessage struct {
	ID          string   `json:"id"`
	From        string   `json:"from"`
	Kind        string   `json:"kind,omitempty"`
	Source      string   `json:"source,omitempty"`
	SourceLabel string   `json:"source_label,omitempty"`
	EventID     string   `json:"event_id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Content     string   `json:"content"`
	Tagged      []string `json:"tagged"`
	ReplyTo     string   `json:"reply_to"`
	Timestamp   string   `json:"timestamp"`
}

type channelMember struct {
	Slug        string `json:"slug"`
	LastMessage string `json:"lastMessage"`
	LastTime    string `json:"lastTime"`
}

type channelInterviewOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type channelInterview struct {
	ID            string                   `json:"id"`
	From          string                   `json:"from"`
	Question      string                   `json:"question"`
	Context       string                   `json:"context"`
	Options       []channelInterviewOption `json:"options"`
	RecommendedID string                   `json:"recommended_id"`
	CreatedAt     string                   `json:"created_at"`
}

type channelUsageTotals struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	CostUsd             float64 `json:"cost_usd"`
	Requests            int     `json:"requests"`
}

type channelUsageState struct {
	Total  channelUsageTotals            `json:"total"`
	Agents map[string]channelUsageTotals `json:"agents"`
}

type channelTickMsg time.Time
type channelPostDoneMsg struct{ err error }
type channelInterviewAnswerDoneMsg struct{ err error }
type channelResetDoneMsg struct{ err error }
type channelInitDoneMsg struct {
	err    error
	notice string
}
type channelIntegrationDoneMsg struct {
	label string
	url   string
	err   error
}

var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9_-]+)`)

var brokerTokenPath = "/tmp/wuphf-broker-token"

func currentBrokerAuthToken() string {
	if token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN")); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN")); token != "" {
		return token
	}
	data, err := os.ReadFile(brokerTokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// newBrokerRequest creates an HTTP request with the broker auth header.
func newBrokerRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if brokerAuthToken := currentBrokerAuthToken(); brokerAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+brokerAuthToken)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

var channelSlashCommands = []tui.SlashCommand{
	{Name: "init", Description: "Run setup"},
	{Name: "integrate", Description: "Connect an integration"},
	{Name: "reply", Description: "Reply in thread by message ID"},
	{Name: "threads", Description: "Browse and manage threads"},
	{Name: "expand", Description: "Expand a collapsed thread"},
	{Name: "collapse", Description: "Collapse a thread"},
	{Name: "cancel", Description: "Exit reply/setup mode"},
	{Name: "reset", Description: "Reset channel and agents"},
	{Name: "quit", Description: "Exit WUPHF"},
}

type channelPickerMode string

const (
	channelPickerNone         channelPickerMode = ""
	channelPickerInitProvider channelPickerMode = "init_provider"
	channelPickerInitPack     channelPickerMode = "init_pack"
	channelPickerIntegrations channelPickerMode = "integrations"
	channelPickerThreads      channelPickerMode = "threads"
	channelPickerThreadAction channelPickerMode = "thread_action"
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
	messages             []brokerMessage
	members              []channelMember
	pending              *channelInterview
	lastID               string
	replyToID            string
	expandedThreads      map[string]bool
	clickableThreads     map[int]string // rendered line index → message ID for click-to-expand
	threadsDefaultExpand bool           // true = expand threads by default
	autocomplete         tui.AutocompleteModel
	mention              tui.MentionModel
	input                []rune
	inputPos             int
	width                int
	height               int
	scroll               int
	unreadCount          int
	posting              bool
	selectedOption       int
	notice               string
	snoozedInterview     string
	initFlow             tui.InitFlowModel
	picker               tui.PickerModel
	pickerMode           channelPickerMode

	// 3-column layout state
	focus            focusArea
	sidebarCollapsed bool
	threadPanelOpen  bool
	threadPanelID    string
	threadInput      []rune
	threadInputPos   int
	threadScroll     int
	usage            channelUsageState
	lastCtrlCAt      time.Time
}

func newChannelModel(threadsCollapsed bool) channelModel {
	m := channelModel{
		expandedThreads:      make(map[string]bool),
		threadsDefaultExpand: !threadsCollapsed,
		autocomplete:         tui.NewAutocomplete(channelSlashCommands),
		mention:              tui.NewMention(channelMentionAgents(nil)),
		initFlow:             tui.NewInitFlow(),
	}
	if config.ResolveNoNex() {
		m.notice = "Running in office-only mode. Nex tools are disabled for this session."
	} else if config.ResolveAPIKey("") == "" {
		m.notice = "No WUPHF API key configured. Starting setup..."
		m.initFlow, _ = m.initFlow.Start()
	}
	return m
}

func (m channelModel) Init() tea.Cmd {
	return tea.Batch(
		pollBroker(""),
		pollMembers(),
		pollInterview(),

		tickChannel(),
	)
}

func (m channelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.focus == focusThread && m.threadPanelOpen {
				m.threadScroll++
			} else {
				m.scroll++
			}
		case tea.MouseButtonWheelDown:
			if m.focus == focusThread && m.threadPanelOpen {
				if m.threadScroll > 0 {
					m.threadScroll--
				}
			} else {
				if m.scroll > 0 {
					m.scroll--
					if m.scroll == 0 {
						m.unreadCount = 0
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
					m.unreadCount = 0
					return m, nil
				case "autocomplete":
					if idx, ok := popupActionIndex(action.Value); ok {
						for m.autocomplete.SelectedIndex() != idx {
							m.autocomplete.Next()
						}
						if name := m.autocomplete.Accept(); name != "" {
							return m.runActiveCommand("/" + name)
						}
					}
					return m, nil
				case "mention":
					if idx, ok := popupActionIndex(action.Value); ok {
						for m.mention.SelectedIndex() != idx {
							m.mention.Next()
						}
						if mention := m.mention.Accept(); mention != "" {
							m.insertAcceptedMention(mention)
						}
					}
					return m, nil
				}
			}
		}
		return m, nil

	case tea.KeyMsg:
		// ── Global keys (always active) ───────────────────────────────
		switch msg.String() {
		case "ctrl+c":
			now := time.Now()
			if !m.lastCtrlCAt.IsZero() && now.Sub(m.lastCtrlCAt) <= 2*time.Second {
				killTeamSession()
				return m, tea.Quit
			}
			m.lastCtrlCAt = now
			m.notice = "Press Ctrl+C again to quit WUPHF."
			return m, nil
		case "ctrl+b":
			m.sidebarCollapsed = !m.sidebarCollapsed
			return m, nil
		}

		// ── Esc: close overlays/thread, then cycle ────────────────────
		if msg.String() == "esc" {
			// Close overlays first
			if m.picker.IsActive() {
				m.picker.SetActive(false)
				if m.pickerMode == channelPickerIntegrations {
					m.notice = "Integration canceled."
				} else {
					m.initFlow = tui.NewInitFlow()
					m.notice = "Setup canceled."
				}
				m.pickerMode = channelPickerNone
				return m, nil
			}
			if m.autocomplete.IsVisible() || m.mention.IsVisible() {
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
				m.mention, _ = m.mention.Update(msg)
				return m, nil
			}
			if m.pending != nil && m.pending.ID != "" {
				m.snoozedInterview = m.pending.ID
				m.notice = "Interview snoozed. Team remains paused until it is answered."
				return m, nil
			}
			// Close thread panel
			if m.threadPanelOpen {
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
			return m, nil
		}

		// ── Tab: cycle focus 0→1→2→0 (only visible panels) ───────────
		if msg.String() == "tab" && !m.autocomplete.IsVisible() && !m.mention.IsVisible() && !m.picker.IsActive() {
			m.focus = m.nextFocus()
			m.updateOverlaysForCurrentInput()
			return m, nil
		}

		// ── Global overlays/pickers before panel-specific handling ────
		if m.picker.IsActive() {
			var cmd tea.Cmd
			m.picker, cmd = m.picker.Update(msg)
			return m, cmd
		}
		if m.initFlow.Phase() == tui.InitAPIKey {
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

		// ── Route by focus area ───────────────────────────────────────
		if m.focus == focusThread && m.threadPanelOpen {
			return m.updateThread(msg)
		}
		if m.focus == focusSidebar && !m.sidebarCollapsed {
			return m.updateSidebar(msg)
		}

		// ── focusMain: existing behavior ──────────────────────────────
		switch msg.String() {
		case "enter":
			m.lastCtrlCAt = time.Time{}
			if len(m.input) > 0 {
				text := string(m.input)
				trimmed := strings.TrimSpace(text)
				if trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q" {
					killTeamSession()
					return m, tea.Quit
				}
				if strings.HasPrefix(trimmed, "/") {
					return m.runActiveCommand(trimmed)
				}

				m.input = nil
				m.inputPos = 0
				m.notice = ""

				m.posting = true
				if m.pending != nil {
					return m, postInterviewAnswer(*m.pending, "", "", text)
				}
				return m, postToChannel(text, m.replyToID)
			} else if m.pending != nil {
				opt := m.selectedInterviewOption()
				if opt != nil {
					m.posting = true
					return m, postInterviewAnswer(*m.pending, opt.ID, opt.Label, "")
				}
			}
		case "backspace":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos > 0 {
				m.input = append(m.input[:m.inputPos-1], m.input[m.inputPos:]...)
				m.inputPos--
				m.updateInputOverlays()
			}
		case "ctrl+u":
			m.lastCtrlCAt = time.Time{}
			m.input = nil
			m.inputPos = 0
			m.updateInputOverlays()
		case "ctrl+a":
			m.lastCtrlCAt = time.Time{}
			m.inputPos = 0
			m.updateInputOverlays()
		case "ctrl+e":
			m.lastCtrlCAt = time.Time{}
			m.inputPos = len(m.input)
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
			m.unreadCount = 0
		case "pgup":
			m.lastCtrlCAt = time.Time{}
			m.scroll += maxInt(10, m.height/2)
		case "pgdown":
			m.lastCtrlCAt = time.Time{}
			m.scroll -= maxInt(10, m.height/2)
			if m.scroll < 0 {
				m.scroll = 0
			}
			if m.scroll == 0 {
				m.unreadCount = 0
			}
		default:
			m.lastCtrlCAt = time.Time{}
			// Type character
			if msg.Type == tea.KeySpace {
				ch := []rune{' '}
				tail := make([]rune, len(m.input[m.inputPos:]))
				copy(tail, m.input[m.inputPos:])
				m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
				m.inputPos++
				m.updateInputOverlays()
			} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
				ch := msg.Runes
				if len(ch) > 0 {
					tail := make([]rune, len(m.input[m.inputPos:]))
					copy(tail, m.input[m.inputPos:])
					m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
					m.inputPos += len(ch)
					m.updateInputOverlays()
				}
			}
		}

	case channelPostDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Send failed: " + msg.err.Error()
		} else if m.replyToID != "" {
			m.notice = fmt.Sprintf("Reply sent to %s. Use /cancel to leave the thread.", m.replyToID)
		}

	case channelInterviewAnswerDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Interview answer failed: " + msg.err.Error()
		} else {
			m.pending = nil
			m.input = nil
			m.inputPos = 0
		}

	case channelResetDoneMsg:
		m.posting = false
		if msg.err == nil {
			m.messages = nil
			m.members = nil
			m.pending = nil
			m.lastID = ""
			m.replyToID = ""
			m.expandedThreads = make(map[string]bool)
			m.input = nil
			m.inputPos = 0
			m.scroll = 0
			m.unreadCount = 0
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
			m.snoozedInterview = ""
		} else {
			m.notice = "Reset failed: " + msg.err.Error()
		}

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

	case channelMsg:
		if len(msg.messages) > 0 {
			if m.scroll > 0 {
				m.scroll += len(msg.messages)
				m.unreadCount += len(msg.messages)
			}
			m.messages = append(m.messages, msg.messages...)
			m.lastID = msg.messages[len(msg.messages)-1].ID
		}

	case channelMembersMsg:
		m.members = msg.members
		m.updateOverlaysForCurrentInput()

	case channelUsageMsg:
		m.usage = msg.usage
		if m.usage.Agents == nil {
			m.usage.Agents = make(map[string]channelUsageTotals)
		}

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
			m.picker = tui.NewPicker("Thread: "+truncateText(msg.Label, 40), actions)
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
		case tui.InitPackChoice:
			m.picker = tui.NewPicker("Choose Agent Pack", tui.PackOptions())
			m.picker.SetActive(true)
			m.pickerMode = channelPickerInitPack
		case tui.InitDone:
			m.posting = true
			return m, tea.Batch(cmd, applyTeamSetup())
		}
		return m, cmd

	case channelInterviewMsg:
		prevID := ""
		if m.pending != nil {
			prevID = m.pending.ID
		}
		m.pending = msg.pending
		if m.pending == nil {
			m.snoozedInterview = ""
		}
		if m.pending != nil && m.pending.ID != prevID {
			m.selectedOption = m.recommendedOptionIndex()
			m.input = nil
			m.inputPos = 0
			m.snoozedInterview = ""
		}

	case channelTickMsg:
		return m, tea.Batch(
			pollBroker(m.lastID),
			pollMembers(),
			pollInterview(),

			tickChannel(),
		)
	}

	return m, nil
}

func (m channelModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)

	// ── Sidebar ──────────────────────────────────────────────────────
	sidebar := ""
	if layout.ShowSidebar {
		sidebar = renderSidebar(mergePackMembers(m.members), "general", layout.SidebarW, layout.ContentH)
	}

	// ── Thread panel ─────────────────────────────────────────────────
	thread := ""
	if layout.ShowThread {
		threadPopup := ""
		if m.focus == focusThread {
			threadPopup = m.renderActivePopup(maxInt(layout.ThreadW-4, 24))
		}
		thread = renderThreadPanel(m.messages, m.threadPanelID,
			layout.ThreadW, layout.ContentH,
			m.threadInput, m.threadInputPos, m.threadScroll,
			threadPopup, m.focus == focusThread)
	}

	// ── Main panel: header + messages + composer ─────────────────────
	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}

	// Channel header (2 lines)
	headerStyle := channelHeaderStyle(mainW)
	headerLine1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).
		Render("# general")
	headerMeta := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).
		Render("  The WUPHF Office · Founding Team building together")
	if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 {
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(slackActive)).
			Render(fmt.Sprintf("Spend to date %s · %s", formatUsd(m.usage.Total.CostUsd), formatTokenCount(m.usage.Total.TotalTokens)))
	}
	if m.unreadCount > 0 && m.scroll > 0 {
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(slackActive)).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("%d new", m.unreadCount))
	}
	if m.pending != nil {
		headerMeta += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Bold(true).Render("Interview pending")
	}
	channelHeader := headerStyle.Render(headerLine1 + headerMeta)
	if usageLine := renderUsageStrip(m.usage, m.members, mainW); usageLine != "" {
		channelHeader += "\n" + usageLine
	}
	headerH := lipgloss.Height(channelHeader)

	// Composer
	typingAgents := typingAgentsFromMembers(m.members)
	composerStr := renderComposer(mainW, m.input, m.inputPos, "general",
		m.replyToID, typingAgents, m.pending, m.selectedOption,
		m.focus == focusMain)

	// Interview card (above composer)
	interviewCard := ""
	if m.pending != nil && m.pending.ID != m.snoozedInterview {
		interviewCard = renderInterviewCard(*m.pending, m.selectedOption, mainW-4)
	}

	// Init/picker overlays
	initPanel := ""
	if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}

	composerH := lipgloss.Height(composerStr)
	interviewH := lipgloss.Height(interviewCard)
	initH := lipgloss.Height(initPanel)

	// Message area height
	msgH := layout.ContentH - headerH - composerH - interviewH - initH - 1 // 1 for status bar
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	allLines := buildOfficeMessageLines(m.messages, m.expandedThreads, contentWidth, m.threadsDefaultExpand)
	visibleRows, scroll, _, _ := sliceRenderedLines(allLines, msgH, m.scroll)
	var visible []string
	for _, row := range visibleRows {
		visible = append(visible, row.Text)
	}
	for len(visible) < msgH {
		visible = append(visible, "")
	}
	if m.unreadCount > 0 && scroll > 0 && len(visible) > 0 {
		jumpLabel := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(slackActive)).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("Jump to latest · %d new", m.unreadCount))
		visible[0] = "  " + jumpLabel
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		visible = overlayBottomLines(visible, strings.Split(popup, "\n"))
	}

	msgPanel := mainPanelStyle(mainW, msgH).Render(strings.Join(visible, "\n"))

	// Assemble main column
	mainParts := []string{channelHeader, msgPanel}
	if interviewCard != "" {
		mainParts = append(mainParts, interviewCard)
	}
	if initPanel != "" {
		mainParts = append(mainParts, initPanel)
	}
	mainParts = append(mainParts, composerStr)
	mainCol := strings.Join(mainParts, "\n")

	// ── Compose 3 columns ────────────────────────────────────────────
	border := renderVerticalBorder(layout.ContentH, slackBorder)
	var panels []string
	if sidebar != "" {
		panels = append(panels, sidebar, border)
	}
	panels = append(panels, mainCol)
	if thread != "" {
		panels = append(panels, border, thread)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)

	// ── Status bar ───────────────────────────────────────────────────
	agentCount := countUniqueAgents(m.messages)
	onlineCount := len(m.members)
	scrollHint := "PgUp/PgDn"
	if scroll > 0 {
		scrollHint = fmt.Sprintf("%d above", scroll)
	}
	focusLabel := "main"
	if m.focus == focusSidebar {
		focusLabel = "sidebar"
	} else if m.focus == focusThread {
		focusLabel = "thread"
	}
	statusBar := statusBarStyle(m.width).Render(fmt.Sprintf(
		" %s %d online │ %d msgs │ %d agents │ %s │ Tab focus:%s │ Ctrl+B sidebar │ /quit",
		"\u25CF", onlineCount, len(m.messages), agentCount, scrollHint, focusLabel,
	))
	if m.pending != nil {
		statusText := " Interview pending │ ↑/↓ choose │ Enter submit"
		if m.pending.ID == m.snoozedInterview {
			statusText = " Interview paused │ Esc snoozed it │ team remains blocked until answered"
		}
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Render(statusText),
		)
	} else if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 {
		statusBar = statusBarStyle(m.width).Render(fmt.Sprintf(
			" %s %d online │ bill %s │ tokens %s │ %s │ Tab focus:%s │ /quit",
			"\u25CF", onlineCount, formatUsd(m.usage.Total.CostUsd), formatTokenCount(m.usage.Total.TotalTokens), scrollHint, focusLabel,
		))
	} else if m.notice != "" {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(" " + m.notice),
		)
	} else if m.replyToID != "" {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(
				fmt.Sprintf(" ↩ Reply mode │ thread %s │ /cancel to return", m.replyToID),
			),
		)
	}

	return content + "\n" + statusBar
}

type mouseAction struct {
	Kind  string
	Value string
}

func popupActionIndex(raw string) (int, bool) {
	var idx int
	if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil || idx < 0 {
		return 0, false
	}
	return idx, true
}

func (m channelModel) mouseActionAt(x, y int) (mouseAction, bool) {
	if m.width == 0 || m.height == 0 || y >= m.height-1 {
		return mouseAction{}, false
	}

	layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
	sidebarW := 0
	if layout.ShowSidebar {
		sidebarW = layout.SidebarW
		if x < sidebarW {
			return mouseAction{Kind: "focus", Value: "sidebar"}, true
		}
		x -= sidebarW + 1
	}

	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}
	if x >= 0 && x < mainW {
		if action, ok := m.mainPanelMouseAction(x, y, mainW, layout.ContentH); ok {
			return action, true
		}
		return mouseAction{Kind: "focus", Value: "main"}, true
	}

	if layout.ShowThread {
		threadStart := mainW + 1
		if x >= threadStart {
			return mouseAction{Kind: "focus", Value: "thread"}, true
		}
	}

	return mouseAction{}, false
}

func (m channelModel) mainPanelMouseAction(x, y, mainW, contentH int) (mouseAction, bool) {
	headerH, msgH, popupRows := m.mainPanelGeometry(mainW, contentH)
	if y < headerH {
		return mouseAction{}, false
	}

	msgTop := headerH
	msgBottom := headerH + msgH
	if y >= msgTop && y < msgBottom {
		row := y - msgTop
		if m.unreadCount > 0 && m.scroll > 0 && row == 0 {
			return mouseAction{Kind: "jump-latest"}, true
		}
		if len(popupRows) > 0 {
			popupStart := msgBottom - len(popupRows)
			if y >= popupStart {
				idx := y - popupStart
				if m.autocomplete.IsVisible() {
					if idx < 0 || idx >= len(m.autocomplete.Matches()) {
						return mouseAction{}, false
					}
					return mouseAction{Kind: "autocomplete", Value: fmt.Sprintf("%d", idx)}, true
				}
				if m.mention.IsVisible() {
					if idx < 0 || idx >= len(m.mention.Matches()) {
						return mouseAction{}, false
					}
					return mouseAction{Kind: "mention", Value: fmt.Sprintf("%d", idx)}, true
				}
			}
		}

		contentWidth := mainW - 2
		if contentWidth < 32 {
			contentWidth = 32
		}
		allLines := buildOfficeMessageLines(m.messages, m.expandedThreads, contentWidth, m.threadsDefaultExpand)
		visibleRows, _, _, _ := sliceRenderedLines(allLines, msgH, m.scroll)
		if row >= 0 && row < len(visibleRows) && visibleRows[row].ThreadID != "" {
			return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
		}
	}

	return mouseAction{}, false
}

func (m channelModel) mainPanelGeometry(mainW, contentH int) (headerH, msgH int, popupRows []string) {
	headerStyle := channelHeaderStyle(mainW)
	headerLine1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).
		Render("# general")
	headerMeta := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).
		Render("  The WUPHF Office · Founding Team building together")
	if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 {
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(slackActive)).
			Render(fmt.Sprintf("Spend to date %s · %s", formatUsd(m.usage.Total.CostUsd), formatTokenCount(m.usage.Total.TotalTokens)))
	}
	channelHeader := headerStyle.Render(headerLine1 + headerMeta)
	if usageLine := renderUsageStrip(m.usage, m.members, mainW); usageLine != "" {
		channelHeader += "\n" + usageLine
	}
	headerH = lipgloss.Height(channelHeader)

	typingAgents := typingAgentsFromMembers(m.members)
	composerStr := renderComposer(mainW, m.input, m.inputPos, "general",
		m.replyToID, typingAgents, m.pending, m.selectedOption,
		m.focus == focusMain)
	interviewCard := ""
	if m.pending != nil && m.pending.ID != m.snoozedInterview {
		interviewCard = renderInterviewCard(*m.pending, m.selectedOption, mainW-4)
	}
	initPanel := ""
	if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}
	msgH = contentH - headerH - lipgloss.Height(composerStr) - lipgloss.Height(interviewCard) - lipgloss.Height(initPanel) - 1
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		popupRows = strings.Split(popup, "\n")
	}
	return headerH, msgH, popupRows
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

func (m channelModel) selectedInterviewOption() *channelInterviewOption {
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

func countUniqueAgents(messages []brokerMessage) int {
	seen := make(map[string]bool)
	for _, m := range messages {
		if m.From == "you" || m.From == "nex" || m.Kind == "automation" {
			continue
		}
		seen[m.From] = true
	}
	return len(seen)
}

func formatUsd(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

func formatTokenCount(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM tok", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk tok", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d tok", tokens)
	}
}

func renderUsageStrip(usage channelUsageState, members []channelMember, width int) string {
	if len(usage.Agents) == 0 || width < 40 {
		return ""
	}

	var ordered []string
	seen := make(map[string]bool)
	for _, member := range members {
		if _, ok := usage.Agents[member.Slug]; ok && !seen[member.Slug] {
			ordered = append(ordered, member.Slug)
			seen[member.Slug] = true
		}
	}
	for _, slug := range []string{"ceo", "pm", "fe", "be", "ai", "designer", "cmo", "cro"} {
		if _, ok := usage.Agents[slug]; ok && !seen[slug] {
			ordered = append(ordered, slug)
			seen[slug] = true
		}
	}
	for slug := range usage.Agents {
		if !seen[slug] {
			ordered = append(ordered, slug)
		}
	}

	pillStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CBD5E1")).
		Background(lipgloss.Color("#111827")).
		Padding(0, 1)

	var pills []string
	for _, slug := range ordered {
		totals := usage.Agents[slug]
		if totals.TotalTokens == 0 && totals.CostUsd == 0 {
			continue
		}
		label := fmt.Sprintf("%s %s · %s", displayName(slug), formatTokenCount(totals.TotalTokens), formatUsd(totals.CostUsd))
		pills = append(pills, pillStyle.Render(label))
	}
	return strings.Join(pills, " ")
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
	switch msg.String() {
	case "enter":
		if len(m.threadInput) > 0 {
			text := string(m.threadInput)
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "/") {
				return m.runCommand(trimmed, m.threadPanelID)
			}
			m.threadInput = nil
			m.threadInputPos = 0
			m.posting = true
			return m, postToChannel(text, m.threadPanelID)
		}
	case "backspace":
		if m.threadInputPos > 0 {
			m.threadInput = append(m.threadInput[:m.threadInputPos-1], m.threadInput[m.threadInputPos:]...)
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "ctrl+u":
		m.threadInput = nil
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+a":
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+e":
		m.threadInputPos = len(m.threadInput)
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
		if msg.Type == tea.KeySpace {
			ch := []rune{' '}
			tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
			copy(tail, m.threadInput[m.threadInputPos:])
			m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
			m.threadInputPos++
			m.updateThreadOverlays()
		} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
			ch := msg.Runes
			if len(ch) > 0 {
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

// updateSidebar handles key events when the sidebar is focused.
func (m channelModel) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Sidebar is currently display-only; arrow keys could navigate members
	// but there is no action to take. For now, just consume the keys.
	switch msg.String() {
	case "up", "down":
		// Reserved for future sidebar navigation
	}
	return m, nil
}

func appendWrapped(lines []string, width int, text string) []string {
	if width <= 0 {
		return append(lines, strings.Split(text, "\n")...)
	}
	wrapped := ansi.Wrap(text, width, "")
	return append(lines, strings.Split(wrapped, "\n")...)
}

type threadedMessage struct {
	Message            brokerMessage
	Depth              int
	ParentLabel        string
	Collapsed          bool
	HiddenReplies      int
	ThreadParticipants []string
}

func flattenThreadMessages(messages []brokerMessage, expanded map[string]bool) []threadedMessage {
	if len(messages) == 0 {
		return nil
	}
	byID := make(map[string]brokerMessage, len(messages))
	children := make(map[string][]brokerMessage)
	var roots []brokerMessage

	for _, msg := range messages {
		byID[msg.ID] = msg
	}
	for _, msg := range messages {
		if msg.ReplyTo != "" {
			if _, ok := byID[msg.ReplyTo]; ok {
				children[msg.ReplyTo] = append(children[msg.ReplyTo], msg)
				continue
			}
		}
		roots = append(roots, msg)
	}

	var out []threadedMessage
	var walk func(msg brokerMessage, depth int)
	walk = func(msg brokerMessage, depth int) {
		parentLabel := ""
		if msg.ReplyTo != "" {
			parentLabel = msg.ReplyTo
			if parent, ok := byID[msg.ReplyTo]; ok {
				parentLabel = "@" + parent.From
			}
		}
		tm := threadedMessage{
			Message:     msg,
			Depth:       depth,
			ParentLabel: parentLabel,
		}
		if len(children[msg.ID]) > 0 && !expanded[msg.ID] {
			tm.Collapsed = true
			tm.HiddenReplies = countThreadReplies(children, msg.ID)
			tm.ThreadParticipants = threadParticipants(children, msg.ID)
		}
		out = append(out, tm)
		if tm.Collapsed {
			return
		}
		for _, child := range children[msg.ID] {
			walk(child, depth+1)
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}
	return out
}

func countThreadReplies(children map[string][]brokerMessage, rootID string) int {
	count := 0
	for _, child := range children[rootID] {
		count++
		count += countThreadReplies(children, child.ID)
	}
	return count
}

func threadParticipants(children map[string][]brokerMessage, rootID string) []string {
	seen := make(map[string]bool)
	var participants []string
	var walk func(id string)
	walk = func(id string) {
		for _, child := range children[id] {
			name := displayName(child.From)
			if !seen[name] {
				seen[name] = true
				participants = append(participants, name)
			}
			walk(child.ID)
		}
	}
	walk(rootID)
	return participants
}

func findMessageByID(messages []brokerMessage, id string) (brokerMessage, bool) {
	for _, msg := range messages {
		if msg.ID == id {
			return msg, true
		}
	}
	return brokerMessage{}, false
}

// buildThreadPickerOptions returns picker options for all root messages that have replies.
func (m channelModel) buildThreadPickerOptions() []tui.PickerOption {
	// Find root messages with replies
	replyCount := make(map[string]int)
	for _, msg := range m.messages {
		if msg.ReplyTo != "" {
			replyCount[msg.ReplyTo]++
		}
	}

	var options []tui.PickerOption
	for _, msg := range m.messages {
		count, hasReplies := replyCount[msg.ID]
		if !hasReplies || msg.ReplyTo != "" {
			continue // skip non-root or messages without replies
		}

		preview := truncateText(msg.Content, 50)
		status := "collapsed"
		if m.expandedThreads[msg.ID] {
			status = "expanded"
		}

		options = append(options, tui.PickerOption{
			Label:       fmt.Sprintf("@%s: %s", msg.From, preview),
			Value:       msg.ID,
			Description: fmt.Sprintf("%d replies · %s", count, status),
		})
	}
	return options
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func hasThreadReplies(messages []brokerMessage, id string) bool {
	for _, msg := range messages {
		if msg.ReplyTo == id {
			return true
		}
	}
	return false
}

func pluralSuffix(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampScroll(total, viewHeight, scroll int) int {
	if scroll < 0 {
		return 0
	}
	maxScroll := total - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		return maxScroll
	}
	return scroll
}

// mergePackMembers returns all pack agents as members, enriched with
// broker activity data when available. Agents who haven't posted yet
// show as "idle" instead of being absent from the sidebar.
func mergePackMembers(brokerMembers []channelMember) []channelMember {
	// Default pack agents (founding team)
	packSlugs := []string{"ceo", "pm", "fe", "be", "ai", "designer", "cmo", "cro"}

	// Build lookup from broker data
	brokerMap := make(map[string]channelMember)
	for _, m := range brokerMembers {
		brokerMap[m.Slug] = m
	}

	var result []channelMember
	for _, slug := range packSlugs {
		if bm, ok := brokerMap[slug]; ok {
			result = append(result, bm)
		} else {
			result = append(result, channelMember{
				Slug:        slug,
				LastMessage: "",
				LastTime:    "",
			})
		}
	}
	// Add any broker members not in the pack (e.g., "you")
	for _, m := range brokerMembers {
		found := false
		for _, s := range packSlugs {
			if m.Slug == s {
				found = true
				break
			}
		}
		if !found {
			result = append(result, m)
		}
	}
	return result
}

func displayName(slug string) string {
	switch slug {
	case "ceo":
		return "CEO"
	case "pm":
		return "Product Manager"
	case "fe":
		return "Frontend Engineer"
	case "be":
		return "Backend Engineer"
	case "ai":
		return "AI Engineer"
	case "designer":
		return "Designer"
	case "cmo":
		return "CMO"
	case "cro":
		return "CRO"
	case "nex":
		return "Nex"
	case "you":
		return "You"
	default:
		return "@" + slug
	}
}

func roleLabel(slug string) string {
	switch slug {
	case "ceo":
		return "strategy"
	case "pm":
		return "product"
	case "fe":
		return "frontend"
	case "be":
		return "backend"
	case "ai":
		return "AI Engineer"
	case "designer":
		return "design"
	case "cmo":
		return "marketing"
	case "cro":
		return "revenue"
	case "nex":
		return "context graph"
	case "you":
		return "human"
	default:
		return "teammate"
	}
}

func renderDateSeparator(width int, label string) string {
	lineWidth := width - len(label) - 8
	if lineWidth < 4 {
		lineWidth = 4
	}
	segment := strings.Repeat("─", lineWidth/2)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748B")).
		Render(fmt.Sprintf("%s  %s  %s", segment, label, segment))
}

func inferMood(text string) string {
	lower := strings.ToLower(text)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "love this") || strings.Contains(lower, "excited") || strings.Contains(lower, "let's go") || strings.Contains(lower, "great wedge"):
		return "energized"
	case strings.Contains(lower, "hmm") || strings.Contains(lower, "skept") || strings.Contains(lower, "push back") || strings.Contains(lower, "bloodbath") || strings.Contains(lower, "crowded"):
		return "skeptical"
	case strings.Contains(lower, "worr") || strings.Contains(lower, "risk") || strings.Contains(lower, "concern"):
		return "concerned"
	case strings.Contains(lower, "blocked") || strings.Contains(lower, "stuck") || strings.Contains(lower, "hard part"):
		return "tense"
	case strings.Contains(lower, "done") || strings.Contains(lower, "shipped") || strings.Contains(lower, "works"):
		return "relieved"
	case strings.Contains(lower, "need") || strings.Contains(lower, "should") || strings.Contains(lower, "must") || strings.Contains(lower, "v1"):
		return "focused"
	default:
		return ""
	}
}

func renderInterviewCard(interview channelInterview, selected int, width int) string {
	cardWidth := width
	if cardWidth < 40 {
		cardWidth = 40
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))

	lines := []string{
		labelStyle.Render("Human Interview"),
		titleStyle.Render(fmt.Sprintf("@%s needs your decision", interview.From)),
		"",
		textStyle.Width(cardWidth - 4).Render(interview.Question),
	}
	if strings.TrimSpace(interview.Context) != "" {
		lines = append(lines, "")
		lines = append(lines, muted.Width(cardWidth-4).Render(interview.Context))
	}
	lines = append(lines, "", muted.Render("Options"))
	for i, option := range interview.Options {
		prefix := "  "
		if i == selected {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
		}
		label := option.Label
		if option.ID == interview.RecommendedID {
			label += " (Recommended)"
		}
		lines = append(lines, prefix+titleStyle.Render(label))
		if strings.TrimSpace(option.Description) != "" {
			lines = append(lines, "    "+muted.Width(cardWidth-8).Render(option.Description))
		}
	}
	customPrefix := "  "
	if selected >= len(interview.Options) {
		customPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
	}
	customLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackMuted)).
		Render("Something else")
	lines = append(lines, customPrefix+customLine)
	lines = append(lines, "    "+muted.Width(cardWidth-8).Render("Type your own answer directly in the composer below."))
	lines = append(lines, "", muted.Render("Press Enter to accept the selected option, or type your own answer below."))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F59E0B")).
		Padding(0, 1).
		Width(cardWidth).
		Render(strings.Join(lines, "\n")) + "\n"
}

func highlightMentions(text string, agentColors map[string]string) string {
	return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		slug := strings.TrimPrefix(strings.ToLower(match), "@")
		color := agentColors[slug]
		if color == "" {
			return match
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true).
			Render(match)
	})
}

func postToChannel(text string, replyTo string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"from":     "you",
			"content":  text,
			"tagged":   extractTagsFromText(text),
			"reply_to": strings.TrimSpace(replyTo),
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/messages", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelPostDoneMsg{err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelPostDoneMsg{}
	}
}

func channelMentionAgents(members []channelMember) []tui.AgentMention {
	defaults := []tui.AgentMention{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "pm", Name: "Product Manager"},
		{Slug: "fe", Name: "Frontend Engineer"},
		{Slug: "be", Name: "Backend Engineer"},
		{Slug: "ai", Name: "AI Engineer"},
		{Slug: "designer", Name: "Designer"},
		{Slug: "cmo", Name: "CMO"},
		{Slug: "cro", Name: "CRO"},
	}
	seen := make(map[string]bool, len(defaults))
	mentions := make([]tui.AgentMention, 0, len(defaults)+len(members))
	for _, ag := range defaults {
		seen[ag.Slug] = true
		mentions = append(mentions, ag)
	}
	for _, member := range members {
		if seen[member.Slug] {
			continue
		}
		seen[member.Slug] = true
		mentions = append(mentions, tui.AgentMention{Slug: member.Slug, Name: displayName(member.Slug)})
	}
	return mentions
}

func (m *channelModel) updateOverlaysForInput(input []rune, cursor int) {
	text := string(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(input) {
		cursor = len(input)
	}
	m.autocomplete.UpdateQuery(strings.TrimLeft(text[:cursor], " "))
	m.mention.UpdateAgents(channelMentionAgents(m.members))
	m.mention.UpdateQuery(text[:cursor])
}

func (m *channelModel) updateInputOverlays() {
	m.updateOverlaysForInput(m.input, m.inputPos)
}

func (m *channelModel) updateThreadOverlays() {
	m.updateOverlaysForInput(m.threadInput, m.threadInputPos)
}

func (m *channelModel) updateOverlaysForCurrentInput() {
	if m.focus == focusThread && m.threadPanelOpen {
		m.updateThreadOverlays()
		return
	}
	if m.focus == focusMain {
		m.updateInputOverlays()
		return
	}
	m.autocomplete.Dismiss()
	m.mention.Dismiss()
}

func (m *channelModel) setActiveInput(text string) {
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInput = []rune(text)
		m.threadInputPos = len(m.threadInput)
		m.updateThreadOverlays()
		return
	}
	m.input = []rune(text)
	m.inputPos = len(m.input)
	m.updateInputOverlays()
}

func (m *channelModel) activeInputString() string {
	if m.focus == focusThread && m.threadPanelOpen {
		return string(m.threadInput)
	}
	return string(m.input)
}

func (m *channelModel) insertAcceptedMention(mention string) {
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInput, m.threadInputPos = replaceMentionInInput(m.threadInput, m.threadInputPos, mention)
		m.updateThreadOverlays()
		return
	}
	m.input, m.inputPos = replaceMentionInInput(m.input, m.inputPos, mention)
	m.updateInputOverlays()
}

func replaceMentionInInput(input []rune, pos int, mention string) ([]rune, int) {
	text := string(input)
	if pos < 0 {
		pos = 0
	}
	if pos > len(input) {
		pos = len(input)
	}
	atIdx := strings.LastIndex(text[:pos], "@")
	if atIdx < 0 {
		return input, pos
	}
	updated := []rune(text[:atIdx] + mention + " " + text[pos:])
	return updated, atIdx + len([]rune(mention)) + 1
}

func (m channelModel) renderActivePopup(width int) string {
	if width < 24 {
		width = 24
	}
	if m.autocomplete.IsVisible() {
		var options []composerPopupOption
		for _, cmd := range m.autocomplete.Matches() {
			options = append(options, composerPopupOption{
				Label: "/" + cmd.Name,
				Meta:  cmd.Description,
			})
		}
		return renderComposerPopup(options, m.autocomplete.SelectedIndex(), width, slackActive)
	}
	if m.mention.IsVisible() {
		var options []composerPopupOption
		for _, ag := range m.mention.Matches() {
			options = append(options, composerPopupOption{
				Label: "@" + ag.Slug,
				Meta:  ag.Name,
			})
		}
		return renderComposerPopup(options, m.mention.SelectedIndex(), width, "#2BAC76")
	}
	return ""
}

func overlayBottomLines(base, overlay []string) []string {
	if len(base) == 0 || len(overlay) == 0 {
		return base
	}
	out := append([]string(nil), base...)
	start := len(out) - len(overlay)
	if start < 0 {
		start = 0
		overlay = overlay[len(overlay)-len(out):]
	}
	for i, line := range overlay {
		out[start+i] = line
	}
	return out
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
		if threadTarget != "" {
			clearThread()
			m.updateThreadOverlays()
			return
		}
		clearMain()
		m.updateInputOverlays()
	}

	switch {
	case trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q":
		killTeamSession()
		return m, tea.Quit
	case trimmed == "/reset":
		clearCurrent()
		m.notice = ""
		m.posting = true
		return m, resetTeamSession()
	case trimmed == "/integrate":
		clearCurrent()
		if config.ResolveNoNex() {
			m.notice = "Nex integration is disabled for this session (--no-nex)."
			return m, nil
		}
		if config.ResolveAPIKey("") == "" {
			m.notice = "Run /init first. No WUPHF API key is configured."
			m.initFlow, _ = m.initFlow.Start()
			return m, nil
		}
		m.picker = tui.NewPicker("Choose Integration", channelIntegrationOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerIntegrations
		m.notice = "Choose an integration to connect."
		return m, nil
	case trimmed == "/init":
		clearCurrent()
		if config.ResolveNoNex() {
			m.notice = "Nex integration is disabled for this session (--no-nex). Restart WUPHF without --no-nex to run setup."
			return m, nil
		}
		m.notice = "Starting setup..."
		var cmd tea.Cmd
		m.initFlow, cmd = m.initFlow.Start()
		return m, cmd
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
			m.notice = "Reply mode cleared."
		} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone || m.picker.IsActive() {
			m.initFlow = tui.NewInitFlow()
			m.picker.SetActive(false)
			m.notice = "Setup canceled."
		} else {
			m.notice = "Nothing to cancel."
		}
		return m, nil
	case strings.HasPrefix(trimmed, "/reply"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/reply"))
		if target == "" {
			m.notice = "Usage: /reply <message-id>"
			return m, nil
		}
		if _, ok := findMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found.", target)
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
				if hasThreadReplies(m.messages, msg.ID) {
					m.expandedThreads[msg.ID] = true
				}
			}
			m.notice = "Expanded all threads."
			return m, nil
		}
		if _, ok := findMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found.", target)
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
		if _, ok := findMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found.", target)
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
		return m, nil
	}
}

func extractTagsFromText(text string) []string {
	var tags []string
	for _, word := range strings.Fields(text) {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			tag := strings.TrimRight(word[1:], ".,!?;:")
			tags = append(tags, tag)
		}
	}
	return tags
}

func pollBroker(sinceID string) tea.Cmd {
	return func() tea.Msg {
		url := "http://127.0.0.1:7890/messages?limit=100"
		if sinceID != "" {
			url += "&since_id=" + sinceID
		}
		req, err := newBrokerRequest(http.MethodGet, url, nil)
		if err != nil {
			return channelMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelMsg{}
		}

		var result struct {
			Messages []brokerMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMsg{}
		}
		return channelMsg{messages: result.Messages}
	}
}

func pollMembers() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/members", nil)
		if err != nil {
			return channelMembersMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMembersMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelMembersMsg{}
		}

		var result struct {
			Members []channelMember `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMembersMsg{}
		}
		return channelMembersMsg{members: result.Members}
	}
}

func pollUsage() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/usage", nil)
		if err != nil {
			return channelUsageMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelUsageMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelUsageMsg{}
		}

		var result channelUsageState
		if err := json.Unmarshal(body, &result); err != nil {
			return channelUsageMsg{}
		}
		if result.Agents == nil {
			result.Agents = make(map[string]channelUsageTotals)
		}
		return channelUsageMsg{usage: result}
	}
}

func pollInterview() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/interview", nil)
		if err != nil {
			return channelInterviewMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterviewMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelInterviewMsg{}
		}

		var result struct {
			Pending *channelInterview `json:"pending"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelInterviewMsg{}
		}
		return channelInterviewMsg{pending: result.Pending}
	}
}

func postInterviewAnswer(interview channelInterview, choiceID, choiceText, customText string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"id":          interview.ID,
			"choice_id":   choiceID,
			"choice_text": choiceText,
			"custom_text": customText,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/interview/answer", bytes.NewReader(body))
		if err != nil {
			return channelInterviewAnswerDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterviewAnswerDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelInterviewAnswerDoneMsg{err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelInterviewAnswerDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelInterviewAnswerDoneMsg{}
	}
}

func channelIntegrationOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(channelIntegrationSpecs))
	for _, spec := range channelIntegrationSpecs {
		options = append(options, tui.PickerOption{
			Label:       spec.Label,
			Value:       spec.Value,
			Description: spec.Description,
		})
	}
	return options
}

func findChannelIntegration(value string) (channelIntegrationSpec, bool) {
	for _, spec := range channelIntegrationSpecs {
		if spec.Value == value {
			return spec, true
		}
	}
	return channelIntegrationSpec{}, false
}

func connectIntegration(spec channelIntegrationSpec) tea.Cmd {
	return func() tea.Msg {
		apiKey := config.ResolveAPIKey("")
		if apiKey == "" {
			return channelIntegrationDoneMsg{err: errors.New("run /init first to configure your WUPHF API key")}
		}
		client := api.NewClient(apiKey)
		result, err := api.Post[map[string]any](client,
			fmt.Sprintf("/v1/integrations/%s/%s/connect", spec.Type, spec.Provider),
			nil,
			30*time.Second,
		)
		if err != nil {
			return channelIntegrationDoneMsg{err: err}
		}

		authURL := mapString(result, "auth_url")
		if authURL != "" {
			_ = openBrowserURL(authURL)
		}
		connectID := mapString(result, "connect_id")
		if connectID == "" {
			return channelIntegrationDoneMsg{label: spec.Label, url: authURL}
		}

		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			statusResp, err := api.Get[map[string]any](client,
				fmt.Sprintf("/v1/integrations/connect/%s/status", connectID),
				15*time.Second,
			)
			if err != nil {
				if _, ok := err.(*api.AuthError); ok {
					return channelIntegrationDoneMsg{err: err}
				}
				continue
			}
			status := strings.ToLower(mapString(statusResp, "status"))
			switch status {
			case "connected", "complete", "completed", "active":
				return channelIntegrationDoneMsg{label: spec.Label, url: authURL}
			case "failed", "error":
				reason := mapString(statusResp, "error")
				if reason == "" {
					reason = status
				}
				return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection failed: %s", spec.Label, reason)}
			}
		}

		if authURL != "" {
			return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection timed out. Finish OAuth at %s", spec.Label, authURL)}
		}
		return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection timed out", spec.Label)}
	}
}

func resetTeamSession() tea.Cmd {
	return func() tea.Msg {
		// Clear broker messages + restart agent Claude sessions
		// but keep the channel TUI pane alive
		l, err := team.NewLauncher("")
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ResetSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		return channelResetDoneMsg{}
	}
}

func applyTeamSetup() tea.Cmd {
	return func() tea.Msg {
		notice, err := setup.InstallLatestCLI()
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		l, err := team.NewLauncher("")
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelInitDoneMsg{err: err}
		}
		return channelInitDoneMsg{notice: notice + " Setup applied. Team reloaded with the new configuration."}
	}
}

func tickChannel() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return channelTickMsg(t)
	})
}

func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

func openBrowserURL(url string) error {
	var cmd *exec.Cmd
	switch {
	case url == "":
		return nil
	case isDarwin():
		cmd = exec.Command("open", url)
	case isLinux():
		cmd = exec.Command("xdg-open", url)
	case isWindows():
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

func isDarwin() bool  { return runtime.GOOS == "darwin" }
func isLinux() bool   { return runtime.GOOS == "linux" }
func isWindows() bool { return runtime.GOOS == "windows" }

// killTeamSession kills the entire wuphf-team tmux session and all agent processes.
func killTeamSession() {
	// Kill tmux session (kills all agent processes in all panes/windows)
	exec.Command("tmux", "-L", "wuphf", "kill-session", "-t", "wuphf-team").Run()
	// Stop the broker
	http.Get("http://127.0.0.1:7890/health") // just to check; broker stops with the process
}

// renderA2UIBlocks extracts A2UI JSON blocks from message content,
// renders them via the GenerativeModel, and returns remaining text + rendered output.
// A2UI blocks are detected by ```a2ui ... ``` fences or inline {"type":"card",...} objects.
func renderA2UIBlocks(content string, width int) (textPart string, rendered string) {
	// Look for ```a2ui ... ``` fenced blocks
	fenceRe := regexp.MustCompile("(?s)```a2ui\\s*\n(.*?)```")
	matches := fenceRe.FindAllStringSubmatchIndex(content, -1)

	if len(matches) == 0 {
		// Also try to detect a bare A2UI JSON object embedded in the message.
		if idx := strings.Index(content, `{"type":"`); idx >= 0 {
			jsonStart, endIdx := extractJSONObject(content, idx)
			if jsonStart == "" {
				return content, ""
			}
			var comp tui.A2UIComponent
			if err := json.Unmarshal([]byte(jsonStart), &comp); err == nil && isA2UIType(comp.Type) {
				gm := tui.NewGenerativeModel()
				gm.SetWidth(width)
				gm.SetSchema(comp)
				if err := gm.Validate(); err != nil {
					return content, ""
				}
				parts := []string{strings.TrimSpace(content[:idx]), strings.TrimSpace(content[endIdx:])}
				textPart = strings.TrimSpace(strings.Join(parts, "\n"))
				rendered = gm.View()
				return
			}
		}
		return content, ""
	}

	// Process fenced blocks
	var textParts []string
	lastEnd := 0
	var renderedParts []string

	for _, match := range matches {
		// Text before the fence
		if match[0] > lastEnd {
			textParts = append(textParts, content[lastEnd:match[0]])
		}

		// Extract JSON inside fence
		jsonStr := content[match[2]:match[3]]
		var comp tui.A2UIComponent
		if err := json.Unmarshal([]byte(jsonStr), &comp); err == nil && isA2UIType(comp.Type) {
			gm := tui.NewGenerativeModel()
			gm.SetWidth(width)
			gm.SetSchema(comp)
			if err := gm.Validate(); err != nil {
				textParts = append(textParts, "```a2ui\n"+jsonStr+"\n```")
			} else {
				renderedParts = append(renderedParts, gm.View())
			}
		} else {
			// Invalid A2UI JSON — show as code block
			textParts = append(textParts, "```a2ui\n"+jsonStr+"\n```")
		}

		lastEnd = match[1]
	}

	// Text after last fence
	if lastEnd < len(content) {
		textParts = append(textParts, content[lastEnd:])
	}

	textPart = strings.TrimSpace(strings.Join(textParts, "\n"))
	rendered = strings.Join(renderedParts, "\n")
	return
}

func extractJSONObject(content string, start int) (string, int) {
	if start < 0 || start >= len(content) || content[start] != '{' {
		return "", 0
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		ch := content[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : i+1], i + 1
			}
		}
	}
	return "", 0
}

// isA2UIType checks if a type string is a valid A2UI component type.
func isA2UIType(t string) bool {
	switch t {
	case "row", "column", "card", "text", "textfield", "list", "table", "progress", "spacer":
		return true
	}
	return false
}

func runChannelView(threadsCollapsed bool) {
	defer func() {
		if r := recover(); r != nil {
			reportChannelCrash(fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack()))
		}
	}()
	p := tea.NewProgram(newChannelModel(threadsCollapsed), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		reportChannelCrash(fmt.Sprintf("channel view error: %v\n", err))
	}
}

func reportChannelCrash(details string) {
	_ = appendChannelCrashLog(details)
	fmt.Fprintln(os.Stderr, "WUPHF channel crashed.")
	fmt.Fprintln(os.Stderr, "Log:", channelCrashLogPath())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The rest of the team is still running.")
	fmt.Fprintln(os.Stderr, "Use `tmux -L wuphf attach -t wuphf-team` to inspect panes,")
	fmt.Fprintln(os.Stderr, "then restart WUPHF when ready.")
	select {}
}

func appendChannelCrashLog(details string) error {
	path := channelCrashLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n[%s]\n%s\n", time.Now().Format(time.RFC3339), details)
	return err
}

func channelCrashLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wuphf-channel-crash.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-crash.log")
}
