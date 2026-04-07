package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/commands"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/orchestration"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/setup"
)

// StreamMessage represents a message in the chat stream.
type StreamMessage struct {
	Role      string // "user", "agent", "system"
	AgentSlug string
	AgentName string
	Content   string
	Timestamp time.Time
}

// defaultSlashCommands are the built-in slash commands for autocomplete.
// One canonical command per action. No aliases.
var defaultSlashCommands = []SlashCommand{
	{Name: "ask", Description: "Ask the AI a question"},
	{Name: "search", Description: "Search knowledge base"},
	{Name: "remember", Description: "Store information"},
	{Name: "object", Description: "Object commands (list/get/create/update/delete)"},
	{Name: "record", Description: "Record commands (list/get/create/upsert/update/delete/timeline)"},
	{Name: "note", Description: "Note commands (list/get/create/update/delete)"},
	{Name: "task", Description: "Task commands (list/get/create/update/delete)"},
	{Name: "list", Description: "List commands (list/get/create/delete/records/add-member)"},
	{Name: "rel", Description: "Relationship commands (list-defs/create-def/create/delete)"},
	{Name: "attribute", Description: "Attribute commands (create/update/delete)"},
	{Name: "agent", Description: "Agent commands (list/<slug>)"},
	{Name: "graph", Description: "View context graph"},
	{Name: "insights", Description: "View insights"},
	{Name: "calendar", Description: "View calendar"},
	{Name: "config", Description: "Config commands (show/set/path)"},
	{Name: "detect", Description: "Detect installed AI platforms"},
	{Name: "init", Description: "Run setup"},
	{Name: "provider", Description: "Switch LLM provider"},
	{Name: "help", Description: "Show all commands"},
	{Name: "clear", Description: "Clear messages"},
	{Name: "quit", Description: "Exit WUPHF"},
}

// AgentStatusEntry holds the current status for an agent in the status line.
type AgentStatusEntry struct {
	Slug     string
	Name     string
	Activity string // "idle", "text", "thinking", "tool_use", "tool_result"
}

// StreamModel is the main chat stream view with agent roster sidebar.
type StreamModel struct {
	messages     []StreamMessage
	inputValue   []rune
	inputPos     int
	autocomplete AutocompleteModel
	mention      MentionModel
	picker       PickerModel
	confirm      ConfirmModel
	roster       RosterModel
	spinner      SpinnerModel
	statusBar    StatusBarModel

	runtime     *Runtime
	agentEvents chan tea.Msg

	width, height int
	scrollOffset  int
	loading       bool
	mode          string // "normal" or "insert"

	streaming         map[string]string          // partial text per agent slug
	wiredAgents       map[string]bool            // agents with event handlers registered
	queuedDelegations []orchestration.Delegation // delegations waiting for a concurrency slot
	pendingLeadTask   string
	pendingLeadHints  []string
	lastAgentEvent    map[string]time.Time
	lastAgentPulse    map[string]time.Time
	showThinking      bool
	spinnerTicking    bool

	// Channel mode: agents run in tmux, no provider loop.
	channelMode bool
	agentStatus []AgentStatusEntry

	initFlow InitFlowModel
}

var (
	debugMetadataPattern = regexp.MustCompile(`(?m)^(session_id|entity_references|tools_used|metadata|status)\s*:`)
	blankLinePattern     = regexp.MustCompile(`\n{3,}`)
)

// NewStreamModel creates a new StreamModel wired to the runtime.
func NewStreamModel(rt *Runtime, events chan tea.Msg) StreamModel {
	m := StreamModel{
		autocomplete:   NewAutocomplete(defaultSlashCommands),
		mention:        NewMention(nil),
		roster:         NewRoster(),
		spinner:        NewSpinner(""),
		statusBar:      NewStatusBar(),
		runtime:        rt,
		agentEvents:    events,
		mode:           "insert",
		streaming:      make(map[string]string),
		wiredAgents:    make(map[string]bool),
		lastAgentEvent: make(map[string]time.Time),
		lastAgentPulse: make(map[string]time.Time),
		spinnerTicking: true,
		initFlow:       NewInitFlow(),
	}
	m.statusBar.Mode = "INSERT"
	m.statusBar.Breadcrumbs = []string{"stream"}
	m.messages = append(m.messages, StreamMessage{
		Role:      "system",
		Content:   m.initialWelcome(),
		Timestamp: time.Now(),
	})
	return m
}

// Init returns the initial commands (spinner tick).
func (m StreamModel) Init() tea.Cmd {
	return m.spinner.Tick()
}

// Update handles all incoming messages.
func (m StreamModel) Update(msg tea.Msg) (StreamModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.statusBar.Width = msg.Width

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollOffset++
		case tea.MouseButtonWheelDown:
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		}
		return m, nil

	case tea.KeyMsg:
		// Scroll keys work in any mode
		switch msg.String() {
		case "pgup", "ctrl+u":
			m.scrollOffset += 10
			return m, nil
		case "pgdown", "ctrl+d":
			m.scrollOffset -= 10
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			return m, nil
		}

		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.picker.IsActive() {
			var cmd tea.Cmd
			m.picker, cmd = m.picker.Update(msg)
			return m, cmd
		}
		if m.confirm.IsActive() {
			var cmd tea.Cmd
			m.confirm, cmd = m.confirm.Update(msg)
			return m, cmd
		}
		if m.initFlow.requiresTextInput() {
			var cmd tea.Cmd
			m.initFlow, cmd = m.initFlow.Update(msg)
			return m, cmd
		}
		if m.mode == "insert" {
			return m.updateInsertMode(msg)
		}
		return m.updateNormalMode(msg)

	case AgentTextMsg:
		m.streaming[msg.AgentSlug] = m.streaming[msg.AgentSlug] + msg.Text
		m.noteAgentActivity(msg.AgentSlug, time.Now())

	case AgentThinkingMsg:
		// Show thinking as a dimmed system message
		agentName := msg.AgentSlug
		if ma, ok := m.runtime.AgentService.Get(msg.AgentSlug); ok {
			agentName = ma.Config.Name
		}
		// Truncate long thinking text
		thinking := msg.Text
		if len(thinking) > 200 {
			thinking = thinking[:200] + "..."
		}
		m.messages = append(m.messages, StreamMessage{
			Role:      "thinking",
			AgentSlug: msg.AgentSlug,
			AgentName: agentName,
			Content:   thinking,
			Timestamp: time.Now(),
		})
		m.noteAgentActivity(msg.AgentSlug, time.Now())

	case AgentToolUseMsg:
		agentName := msg.AgentSlug
		if ma, ok := m.runtime.AgentService.Get(msg.AgentSlug); ok {
			agentName = ma.Config.Name
		}
		// Format tool use display
		toolDisplay := msg.ToolName
		if msg.ToolInput != "" && msg.ToolInput != "{}" && msg.ToolInput != "null" {
			input := msg.ToolInput
			if len(input) > 120 {
				input = input[:120] + "..."
			}
			toolDisplay = msg.ToolName + " " + input
		}
		m.messages = append(m.messages, StreamMessage{
			Role:      "tool_use",
			AgentSlug: msg.AgentSlug,
			AgentName: agentName,
			Content:   toolDisplay,
			Timestamp: time.Now(),
		})
		m.spinner.SetLabel(agentName + " → " + msg.ToolName)
		m.noteAgentActivity(msg.AgentSlug, time.Now())

	case AgentToolResultMsg:
		agentName := msg.AgentSlug
		if ma, ok := m.runtime.AgentService.Get(msg.AgentSlug); ok {
			agentName = ma.Config.Name
		}
		content := msg.Content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		m.messages = append(m.messages, StreamMessage{
			Role:      "tool_result",
			AgentSlug: msg.AgentSlug,
			AgentName: agentName,
			Content:   content,
			Timestamp: time.Now(),
		})
		m.noteAgentActivity(msg.AgentSlug, time.Now())

	case AgentDoneMsg:
		if text, ok := m.streaming[msg.AgentSlug]; ok && text != "" {
			agentName := m.agentDisplayName(msg.AgentSlug)
			displayText := m.displayAgentText(msg.AgentSlug, text)
			if displayText != "" {
				m.messages = append(m.messages, StreamMessage{
					Role:      "agent",
					AgentSlug: msg.AgentSlug,
					AgentName: agentName,
					Content:   displayText,
					Timestamp: time.Now(),
				})
			}
			delete(m.streaming, msg.AgentSlug)

			// If the team-lead just finished, parse for delegations to specialists
			if msg.AgentSlug == m.runtime.TeamLeadSlug && m.runtime.Delegator != nil {
				var knownSlugs []string
				for _, a := range m.runtime.AgentService.List() {
					if a.Config.Slug != m.runtime.TeamLeadSlug {
						knownSlugs = append(knownSlugs, a.Config.Slug)
					}
				}
				delegations := m.runtime.Delegator.ExtractDelegations(text, knownSlugs)
				if len(delegations) == 0 && len(m.pendingLeadHints) > 0 && m.pendingLeadTask != "" {
					for _, slug := range m.pendingLeadHints {
						delegations = append(delegations, orchestration.Delegation{
							AgentSlug: slug,
							Task:      m.pendingLeadTask,
						})
					}
					m.appendSystemMessage("CEO delegation fallback: dispatching suggested specialists.")
				}
				immediate, queued := m.runtime.Delegator.ApplyLimit(delegations)
				m.queuedDelegations = append(m.queuedDelegations, queued...)
				m.startDelegations(immediate)
				m.pendingLeadTask = ""
				m.pendingLeadHints = nil
				if len(delegations) > 0 {
					m.loading = true
					m.spinner.SetActive(true)
					m.spinner.SetLabel("agents working...")
				}
			}

			// If a specialist just finished, check queued delegations
			if msg.AgentSlug != m.runtime.TeamLeadSlug && len(m.queuedDelegations) > 0 {
				next := m.queuedDelegations[0]
				m.queuedDelegations = m.queuedDelegations[1:]
				m.startDelegations([]orchestration.Delegation{next})
			}
		}
		m.noteAgentActivity(msg.AgentSlug, time.Now())
		m.loading = m.hasActiveAgents()
		if !m.loading {
			m.spinner.SetActive(false)
		}
		m.updateSpinnerLabel()
		m.updateRoster()

	case AgentErrorMsg:
		delete(m.streaming, msg.AgentSlug)
		m.appendSystemMessage(fmt.Sprintf("Error from %s: %v", msg.AgentSlug, msg.Err))

		// Advance queued delegations on specialist failure (free the slot)
		if msg.AgentSlug != m.runtime.TeamLeadSlug && len(m.queuedDelegations) > 0 {
			next := m.queuedDelegations[0]
			m.queuedDelegations = m.queuedDelegations[1:]
			m.startDelegations([]orchestration.Delegation{next})
		}

		m.loading = m.hasActiveAgents()
		if !m.loading {
			m.spinner.SetActive(false)
		}
		m.noteAgentActivity(msg.AgentSlug, time.Now())
		m.updateSpinnerLabel()
		m.updateRoster()

	case PhaseChangeMsg:
		m.noteAgentActivity(msg.AgentSlug, time.Now())
		m.handlePhaseChange(msg)
		m.updateSpinnerLabel()
		m.updateRoster()

	case SpinnerTickMsg:
		var sCmd tea.Cmd
		m.spinner, sCmd = m.spinner.Update(msg)
		if sCmd != nil {
			m.spinnerTicking = true
			cmds = append(cmds, sCmd)
		} else {
			m.spinnerTicking = false
		}
		var rCmd tea.Cmd
		m.roster, rCmd = m.roster.Update(msg)
		cmds = append(cmds, rCmd)
		m.emitProgressPulses(msg.Time)
		m.updateSpinnerLabel()

	case PickerSelectMsg:
		m.picker.SetActive(false)
		if m.initFlow.IsActive() {
			var cmd tea.Cmd
			m.initFlow, cmd = m.initFlow.Update(msg)
			cmds = append(cmds, cmd)
		} else if msg.Value != "" {
			// Standalone /provider pick — save and reconfigure.
			cfg, _ := config.Load()
			cfg.LLMProvider = msg.Value
			_ = config.Save(cfg)
			m.runtime.Reconfigure()
			m.resetDelegationState()
			m.rewireAllAgents()
			m.appendSystemMessage(fmt.Sprintf("Provider switched to %s. %s", msg.Value, m.runtimeSummary()))
			m.updateSpinnerLabel()
		}

	case ConfirmMsg:
		m.confirm.SetActive(false)

	case InitFlowMsg:
		m.initFlow, _ = m.initFlow.Update(msg)
		switch InitPhase(msg.Phase) {
		case InitProviderChoice:
			m.picker = NewPicker("Choose LLM Provider", ProviderOptions())
			m.picker.SetActive(true)
		case InitPackChoice:
			m.picker = NewPicker("Choose Agent Pack", PackOptions())
			m.picker.SetActive(true)
		case InitDone:
			return m, applySoloSetup()
		}

	case SetupApplyMsg:
		if msg.Err != nil {
			m.appendSystemMessage("Setup failed: " + msg.Err.Error())
			break
		}
		m.runtime.Reconfigure()
		m.resetDelegationState()
		m.rewireAllAgents()
		if strings.TrimSpace(msg.Notice) != "" {
			m.appendSystemMessage(msg.Notice)
		}
		heading, instructions := m.initFlow.phaseText()
		m.appendSystemMessage(heading + " — " + instructions)
		m.appendSystemMessage(m.runtimeSummary())
		m.updateSpinnerLabel()

	case SlashResultMsg:
		if msg.Err != nil {
			m.appendSystemMessage("Error: " + msg.Err.Error())
		} else if msg.Output != "" {
			m.appendSystemMessage(msg.Output)
		}
	}

	return m, tea.Batch(cmds...)
}

// updateInsertMode handles key events when in insert mode.
func (m StreamModel) updateInsertMode(msg tea.KeyMsg) (StreamModel, tea.Cmd) {
	key := msg.String()

	// Delegate to autocomplete when visible
	if m.autocomplete.IsVisible() {
		switch key {
		case "tab":
			// Tab accepts the autocomplete selection
			name := m.autocomplete.Accept()
			if name != "" {
				m.inputValue = []rune("/" + name + " ")
				m.inputPos = len(m.inputValue)
			}
			return m, nil
		case "shift+tab":
			m.autocomplete.Prev()
			return m, nil
		case "enter":
			// Enter accepts the autocomplete selection and submits
			name := m.autocomplete.Accept()
			if name != "" {
				m.inputValue = []rune("/" + name)
				m.inputPos = len(m.inputValue)
			}
			return m.handleSubmit()
		case "esc":
			m.autocomplete.Dismiss()
			return m, nil
		case "up":
			m.autocomplete.Prev()
			return m, nil
		case "down":
			m.autocomplete.Next()
			return m, nil
		}
	}

	// Delegate to mention when visible
	if m.mention.IsVisible() {
		switch key {
		case "tab":
			m.mention.Next()
			return m, nil
		case "shift+tab":
			m.mention.Prev()
			return m, nil
		case "enter":
			slug := m.mention.Accept()
			if slug != "" {
				input := string(m.inputValue)
				atIdx := strings.LastIndex(input, "@")
				if atIdx >= 0 {
					m.inputValue = []rune(input[:atIdx] + slug + " ")
					m.inputPos = len(m.inputValue)
				}
			}
			return m, nil
		case "esc":
			m.mention.Dismiss()
			return m, nil
		}
	}

	switch key {
	case "enter":
		return m.handleSubmit()
	case "esc":
		m.mode = "normal"
		m.statusBar.Mode = "NORMAL"
		return m, nil
	case "backspace":
		if m.inputPos > 0 {
			m.inputValue = append(m.inputValue[:m.inputPos-1], m.inputValue[m.inputPos:]...)
			m.inputPos--
			m.updateInputOverlays()
		}
		return m, nil
	case "delete":
		if m.inputPos < len(m.inputValue) {
			m.inputValue = append(m.inputValue[:m.inputPos], m.inputValue[m.inputPos+1:]...)
			m.updateInputOverlays()
		}
		return m, nil
	case "left":
		if m.inputPos > 0 {
			m.inputPos--
		}
		return m, nil
	case "right":
		if m.inputPos < len(m.inputValue) {
			m.inputPos++
		}
		return m, nil
	case "home", "ctrl+a":
		m.inputPos = 0
		return m, nil
	case "end", "ctrl+e":
		m.inputPos = len(m.inputValue)
		return m, nil
	case "ctrl+u":
		m.inputValue = m.inputValue[m.inputPos:]
		m.inputPos = 0
		m.updateInputOverlays()
		return m, nil
	case "ctrl+k":
		m.inputValue = m.inputValue[:m.inputPos]
		m.updateInputOverlays()
		return m, nil
	case "tab", "shift+tab":
		return m, nil
	default:
		if msg.Type == tea.KeySpace {
			runes := []rune{' '}
			newInput := make([]rune, len(m.inputValue)+1)
			copy(newInput, m.inputValue[:m.inputPos])
			copy(newInput[m.inputPos:], runes)
			copy(newInput[m.inputPos+1:], m.inputValue[m.inputPos:])
			m.inputValue = newInput
			m.inputPos++
			m.updateInputOverlays()
			return m, nil
		}
		if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
			return m, nil
		}
		runes := sanitizeInputRunes(msg.Runes)
		if len(runes) > 0 {
			newInput := make([]rune, len(m.inputValue)+len(runes))
			copy(newInput, m.inputValue[:m.inputPos])
			copy(newInput[m.inputPos:], runes)
			copy(newInput[m.inputPos+len(runes):], m.inputValue[m.inputPos:])
			m.inputValue = newInput
			m.inputPos += len(runes)
			m.updateInputOverlays()
		}
		return m, nil
	}
}

func sanitizeInputRunes(in []rune) []rune {
	out := make([]rune, 0, len(in))
	lastWasSpace := false
	for _, r := range in {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			if len(out) > 0 && !lastWasSpace {
				out = append(out, ' ')
				lastWasSpace = true
			}
		case r >= 32:
			out = append(out, r)
			lastWasSpace = r == ' '
		}
	}
	return out
}

// updateNormalMode handles key events when in normal mode.
func (m StreamModel) updateNormalMode(msg tea.KeyMsg) (StreamModel, tea.Cmd) {
	switch msg.String() {
	case "i":
		m.mode = "insert"
		m.statusBar.Mode = "INSERT"
	case "j":
		if m.scrollOffset > 0 {
			m.scrollOffset--
		}
	case "k":
		m.scrollOffset++
	case "G":
		m.scrollOffset = 0
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// handleSubmit processes the current input as a slash command or natural language message.
func (m StreamModel) handleSubmit() (StreamModel, tea.Cmd) {
	input := strings.TrimSpace(string(m.inputValue))
	if input == "" {
		return m, nil
	}

	m.inputValue = nil
	m.inputPos = 0
	m.autocomplete.Dismiss()
	m.mention.Dismiss()

	if strings.HasPrefix(input, "/") {
		return m.handleSlashCommand(input)
	}

	// Add as user message
	m.messages = append(m.messages, StreamMessage{
		Role:      "user",
		Content:   input,
		Timestamp: time.Now(),
	})

	// Route via message router
	available := m.availableAgents()
	result := m.runtime.MessageRouter.Route(input, available)

	// Ensure primary agent exists
	primarySlug := result.Primary
	if _, ok := m.runtime.AgentService.Get(primarySlug); !ok {
		if _, err := m.runtime.AgentService.CreateFromTemplate(primarySlug, primarySlug); err != nil {
			primarySlug = "team-lead"
		} else {
			_ = m.runtime.AgentService.Start(primarySlug)
		}
	}

	// Wire events + queue the user's message as a real follow-up turn.
	m.wireAgent(primarySlug)
	if ma, ok := m.runtime.AgentService.Get(primarySlug); ok {
		m.runtime.MessageRouter.RegisterAgent(primarySlug, ma.Config.Expertise)
	}
	if primarySlug == m.runtime.TeamLeadSlug {
		m.pendingLeadTask = input
		m.pendingLeadHints = append([]string(nil), result.Collaborators...)
		if len(result.Collaborators) > 0 {
			hint := "Delegate using only these specialist slugs when relevant: @" + strings.Join(result.Collaborators, ", @")
			_ = m.runtime.AgentService.Steer(primarySlug, hint)
			m.appendSystemMessage(fmt.Sprintf("%s coordinating with %s.", m.agentDisplayName(primarySlug), m.formatAgentNames(result.Collaborators)))
			var proactive []orchestration.Delegation
			for _, slug := range result.Collaborators {
				proactive = append(proactive, orchestration.Delegation{
					AgentSlug: slug,
					Task:      input,
				})
			}
			immediate, queued := m.runtime.Delegator.ApplyLimit(proactive)
			m.queuedDelegations = append(m.queuedDelegations, queued...)
			if len(queued) > 0 {
				m.appendSystemMessage(fmt.Sprintf("%d more delegation(s) queued.", len(queued)))
			}
			m.startDelegations(immediate)
			m.pendingLeadTask = ""
			m.pendingLeadHints = nil
			m.loading = true
			m.spinner.SetActive(true)
			m.updateSpinnerLabel()
		}
	} else {
		m.pendingLeadTask = ""
		m.pendingLeadHints = nil
		if result.IsFollowUp {
			m.appendSystemMessage(fmt.Sprintf("Continuing with %s.", m.agentDisplayName(primarySlug)))
		} else {
			m.appendSystemMessage(fmt.Sprintf("Directing this to %s.", m.agentDisplayName(primarySlug)))
		}
	}
	_ = m.runtime.AgentService.FollowUp(primarySlug, input)
	m.runtime.AgentService.EnsureRunning(primarySlug)

	// Collaborators are populated for informational purposes only.
	// The team-lead will narrate and the delegator will extract delegations
	// to specialists when the team-lead's response arrives.

	m.runtime.MessageRouter.RecordAgentActivity(primarySlug)

	m.loading = true
	m.spinner.SetActive(true)
	m.updateSpinnerLabel()
	m.updateRoster()

	return m, tea.Batch(m.ensureSpinnerTick())
}

// handleSlashCommand processes slash commands. TUI-specific commands are handled
// inline; all others are routed through the commands.Dispatch registry.
func (m StreamModel) handleSlashCommand(input string) (StreamModel, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := strings.TrimPrefix(parts[0], "/")

	// TUI-specific commands that need direct model access
	switch cmd {
	case "clear":
		m.messages = []StreamMessage{{
			Role:      "system",
			Content:   "Chat cleared.",
			Timestamp: time.Now(),
		}}
		m.scrollOffset = 0
		return m, nil
	case "init":
		var initCmd tea.Cmd
		m.initFlow, initCmd = m.initFlow.Start()
		m.messages = append(m.messages, StreamMessage{
			Role:      "system",
			Content:   "Starting setup...",
			Timestamp: time.Now(),
		})
		return m, initCmd
	case "provider":
		m.picker = NewPicker("Switch LLM Provider", ProviderOptions())
		m.picker.SetActive(true)
		return m, nil
	case "thinking":
		m.showThinking = !m.showThinking
		if m.showThinking {
			m.appendSystemMessage("Agent thinking expanded.")
		} else {
			m.appendSystemMessage("Agent thinking collapsed.")
		}
		return m, nil
	case "reset":
		if err := provider.ResetClaudeSessions(); err != nil {
			m.appendSystemMessage("Failed to reset Claude session persistence: " + err.Error())
			return m, nil
		}
		m.runtime.Reconfigure()
		m.resetDelegationState()
		m.appendSystemMessage("Claude session persistence reset. The next Claude task will start fresh.")
		return m, nil
	case "agents":
		m.appendSystemMessage(m.renderAgentsSnapshot())
		return m, nil
	case "generative":
		g := NewGenerativeModel()
		g.width = m.width - 10
		g.SetData(map[string]any{
			"title":    "Agent Dashboard",
			"status":   "active",
			"progress": 0.72,
			"tasks":    []any{"Scan codebase", "Generate report", "Deploy changes"},
			"metrics": []any{
				[]any{"Metric", "Value"},
				[]any{"Latency", "42ms"},
				[]any{"Throughput", "1.2k/s"},
				[]any{"Errors", "0"},
			},
		})
		g.SetSchema(A2UIComponent{
			Type: "column",
			Children: []A2UIComponent{
				{Type: "card", Props: map[string]any{"title": "Overview"}, Children: []A2UIComponent{
					{Type: "row", Children: []A2UIComponent{
						{Type: "text", DataRef: "/title", Props: map[string]any{"bold": true}},
						{Type: "text", DataRef: "/status", Props: map[string]any{"color": Success}},
					}},
					{Type: "spacer"},
					{Type: "text", Props: map[string]any{"content": "Progress:", "dimmed": true}},
					{Type: "progress", DataRef: "/progress"},
				}},
				{Type: "spacer"},
				{Type: "card", Props: map[string]any{"title": "Tasks"}, Children: []A2UIComponent{
					{Type: "list", DataRef: "/tasks"},
				}},
				{Type: "spacer"},
				{Type: "card", Props: map[string]any{"title": "Metrics"}, Children: []A2UIComponent{
					{Type: "table", DataRef: "/metrics"},
				}},
			},
		})
		m.messages = append(m.messages, StreamMessage{
			Role:      "system",
			Content:   "Generative UI demo:\n\n" + g.View(),
			Timestamp: time.Now(),
		})
		return m, nil
	case "chat":
		return m, func() tea.Msg { return ViewSwitchMsg{Target: ViewChat} }
	case "quit", "q":
		return m, tea.Quit
	}

	// Route all other commands through the dispatch registry
	apiKey := config.ResolveAPIKey("")
	result := commands.DispatchWithService(input, apiKey, "text", 0, m.runtime.AgentService)
	output := result.Output
	if output == "" && result.Error != "" {
		output = "Error: " + result.Error
	}
	if output == "" {
		output = fmt.Sprintf("/%s — done", cmd)
	}
	m.messages = append(m.messages, StreamMessage{
		Role:      "system",
		Content:   output,
		Timestamp: time.Now(),
	})
	return m, nil
}

// View renders the layout. In channel mode: full-width messages + agent status line.
// In classic mode: two-column with roster sidebar.
func (m StreamModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// In channel mode, use full width (no roster sidebar).
	showRoster := !m.channelMode
	var lw int
	if showRoster {
		rw := rosterWidth + 4
		lw = m.width - rw - 1
		if lw < 30 {
			showRoster = false
			lw = m.width
		}
	} else {
		lw = m.width
	}

	// Height budget: title(1) + agentStatus?(1) + messages(flex) + spinner?(1) + input(3) + statusbar(1)
	usedH := 5
	if m.loading {
		usedH++
	}
	if m.channelMode && len(m.agentStatus) > 0 {
		usedH++ // agent status line
	}
	msgH := m.height - usedH
	if msgH < 1 {
		msgH = 1
	}

	// Title
	title := TitleStyle.Render("wuphf v0.1.0")

	// Messages
	msgsView := m.renderMessages(lw, msgH)

	// Build left column
	var leftParts []string
	leftParts = append(leftParts, title, msgsView)
	if m.loading {
		leftParts = append(leftParts, m.spinner.View())
	}

	// Agent status line (channel mode only, above input).
	if m.channelMode && len(m.agentStatus) > 0 {
		leftParts = append(leftParts, m.renderAgentStatusLine(lw))
	}

	leftParts = append(leftParts, m.renderInput(lw))

	// Init flow API key input overlay
	if m.initFlow.requiresTextInput() {
		leftParts = append(leftParts, m.initFlow.View())
	}

	// Picker overlay (used by init flow and /provider)
	if pv := m.picker.View(); pv != "" {
		leftParts = append(leftParts, pv)
	}

	// Autocomplete / mention overlays
	if ac := m.autocomplete.View(); ac != "" {
		leftParts = append(leftParts, ac)
	}
	if mn := m.mention.View(); mn != "" {
		leftParts = append(leftParts, mn)
	}

	left := lipgloss.JoinVertical(lipgloss.Left, leftParts...)

	// Two-column layout (classic mode only)
	var content string
	if showRoster {
		right := m.roster.View()
		content = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	} else {
		content = left
	}

	// Status bar at bottom
	sb := m.statusBar
	sb.Width = m.width
	return content + "\n" + sb.View()
}

// renderMessages renders the scrollable message list.
func (m StreamModel) renderMessages(width, height int) string {
	if height <= 0 {
		return ""
	}

	var lines []string
	hiddenThinking := 0
	for _, msg := range m.messages {
		if msg.Role == "thinking" && !m.showThinking {
			hiddenThinking++
			continue
		}
		lines = append(lines, wrapForViewport(m.renderMessage(msg, width), width)...)
	}
	if hiddenThinking > 0 {
		lines = append(lines, wrapForViewport(SystemStyle.Render(fmt.Sprintf("  %d thinking update(s) hidden. Use /thinking to expand.", hiddenThinking)), width)...)
	}

	// Append streaming partial texts with cursor
	for slug, partial := range m.streaming {
		agentName := slug
		if ma, ok := m.runtime.AgentService.Get(slug); ok {
			agentName = ma.Config.Name
		}
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor))
		live := sanitizeAgentOutput(partial)
		if live == "" {
			continue
		}
		lines = append(lines, wrapForViewport(m.agentPrefix(slug, agentName)+dimStyle.Render(live+"_"), width)...)
	}

	total := len(lines)
	if total == 0 {
		return strings.Repeat("\n", height-1)
	}

	// Scroll: offset 0 = show latest, higher = scroll up
	end := total - m.scrollOffset
	if end > total {
		end = total
	}
	if end < 1 {
		end = 1
	}
	start := end - height
	if start < 0 {
		start = 0
	}

	visible := lines[start:end]
	result := strings.Join(visible, "\n")

	// Pad to fill height (push content to bottom)
	if len(visible) < height {
		result = strings.Repeat("\n", height-len(visible)) + result
	}

	return result
}

// renderMessage formats a single stream message by role.
func (m StreamModel) renderMessage(msg StreamMessage, width int) string {
	thinkingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
	toolUseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(NexPurple)).Bold(true)
	toolResultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	switch msg.Role {
	case "user":
		return UserStyle.Render("You: ") + msg.Content
	case "agent":
		return m.agentPrefix(msg.AgentSlug, msg.AgentName) + msg.Content
	case "system":
		return SystemStyle.Render("  " + msg.Content)
	case "thinking":
		return thinkingStyle.Render("  💭 " + msg.AgentName + ": " + msg.Content)
	case "tool_use":
		return toolUseStyle.Render("  ⚡ " + msg.AgentName + " → " + msg.Content)
	case "tool_result":
		return toolResultStyle.Render("  ↳ " + summarizeToolResult(msg.Content))
	default:
		return msg.Content
	}
}

func summarizeToolResult(content string) string {
	if !shouldFoldToolResult(content) {
		return content
	}

	lineCount := countLines(content)
	first, last := summarizeEdgeLines(content)
	summary := fmt.Sprintf("[folded output: %d lines, %d chars]", lineCount, len(content))
	if first != "" {
		summary += " " + first
	}
	if last != "" && last != first {
		summary += " ... " + last
	}
	return summary
}

func shouldFoldToolResult(content string) bool {
	return countLines(content) > 10 || len(content) > 500
}

func countLines(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func summarizeEdgeLines(content string) (string, string) {
	lines := strings.Split(content, "\n")
	first := ""
	last := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = truncateToolSummaryLine(line, 60)
		}
		last = truncateToolSummaryLine(line, 60)
	}
	return first, last
}

func truncateToolSummaryLine(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "..."
}

// agentPrefix returns a styled name prefix based on the agent's role.
func (m StreamModel) agentPrefix(slug, name string) string {
	if slug == m.runtime.TeamLeadSlug {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EAB308")).
			Bold(true)
		return style.Render(name + ": ")
	}
	// Specialist agents: dim with "│ " prefix
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#22C55E")).
		Bold(true)
	return style.Render("↳ " + name + ": ")
}

// renderInput renders the bordered text input area.
func (m StreamModel) renderInput(width int) string {
	var inputStr string

	if len(m.inputValue) == 0 {
		if m.mode == "insert" {
			inputStr = SystemStyle.Render(fmt.Sprintf("Message %s or use @agent... (/help, /thinking, /quit)", m.agentDisplayName(m.runtime.TeamLeadSlug)))
		}
	} else if m.mode == "insert" {
		// Render with cursor
		before := string(m.inputValue[:m.inputPos])
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		var cursor, after string
		if m.inputPos < len(m.inputValue) {
			cursor = cursorStyle.Render(string(m.inputValue[m.inputPos]))
			after = string(m.inputValue[m.inputPos+1:])
		} else {
			cursor = cursorStyle.Render(" ")
			after = ""
		}
		inputStr = before + cursor + after
	} else {
		inputStr = string(m.inputValue)
	}

	iw := width - 4
	if iw < 10 {
		iw = 10
	}
	return InputBorderStyle.Width(iw).Render(inputStr)
}

// wireAgent registers event handlers on an agent's loop to forward events to the TUI channel.
func (m StreamModel) wireAgent(slug string) {
	if m.wiredAgents[slug] {
		return
	}
	ma, ok := m.runtime.AgentService.Get(slug)
	if !ok {
		return
	}

	ch := m.agentEvents
	ma.Loop.On(agent.EventMessage, func(args ...any) {
		if len(args) > 0 {
			if text, ok := args[0].(string); ok {
				select {
				case ch <- AgentTextMsg{AgentSlug: slug, Text: text}:
				default:
				}
			}
		}
	})
	ma.Loop.On(agent.EventDone, func(args ...any) {
		select {
		case ch <- AgentDoneMsg{AgentSlug: slug}:
		default:
		}
	})
	ma.Loop.On(agent.EventError, func(args ...any) {
		errStr := "unknown error"
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				errStr = s
			}
		}
		select {
		case ch <- AgentErrorMsg{AgentSlug: slug, Err: fmt.Errorf("%s", errStr)}:
		default:
		}
	})
	ma.Loop.On(agent.EventPhaseChange, func(args ...any) {
		var from, to string
		if len(args) >= 2 {
			if f, ok := args[0].(agent.AgentPhase); ok {
				from = string(f)
			}
			if t, ok := args[1].(agent.AgentPhase); ok {
				to = string(t)
			}
		}
		select {
		case ch <- PhaseChangeMsg{AgentSlug: slug, From: from, To: to}:
		default:
		}
	})
	ma.Loop.On(agent.EventThinking, func(args ...any) {
		if len(args) > 0 {
			if text, ok := args[0].(string); ok {
				select {
				case ch <- AgentThinkingMsg{AgentSlug: slug, Text: text}:
				default:
				}
			}
		}
	})
	ma.Loop.On(agent.EventToolUse, func(args ...any) {
		toolName, toolInput := "", ""
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				toolName = s
			}
		}
		if len(args) > 1 {
			if s, ok := args[1].(string); ok {
				toolInput = s
			}
		}
		select {
		case ch <- AgentToolUseMsg{AgentSlug: slug, ToolName: toolName, ToolInput: toolInput}:
		default:
		}
	})
	ma.Loop.On(agent.EventToolResult, func(args ...any) {
		if len(args) > 0 {
			if text, ok := args[0].(string); ok {
				select {
				case ch <- AgentToolResultMsg{AgentSlug: slug, Content: text}:
				default:
				}
			}
		}
	})

	m.wiredAgents[slug] = true
}

// updateInputOverlays syncs autocomplete and mention state with current input.
func (m *StreamModel) updateInputOverlays() {
	input := string(m.inputValue)
	m.autocomplete.UpdateQuery(input)

	// Refresh mention agent list
	agents := m.runtime.AgentService.List()
	mentions := make([]AgentMention, len(agents))
	for i, a := range agents {
		mentions[i] = AgentMention{Slug: a.Config.Slug, Name: a.Config.Name}
	}
	m.mention.UpdateAgents(mentions)
	m.mention.UpdateQuery(input)
}

// updateRoster syncs the roster display with agent service state.
func (m *StreamModel) updateRoster() {
	agents := m.runtime.AgentService.List()
	entries := make([]AgentEntry, len(agents))
	for i, a := range agents {
		entries[i] = AgentEntry{
			Slug:  a.Config.Slug,
			Name:  a.Config.Name,
			Phase: string(a.State.Phase),
		}
	}
	m.roster.UpdateAgents(entries)
}

// availableAgents returns AgentInfo for all agents in the service.
func (m StreamModel) availableAgents() []orchestration.AgentInfo {
	agents := m.runtime.AgentService.List()
	infos := make([]orchestration.AgentInfo, len(agents))
	for i, a := range agents {
		infos[i] = orchestration.AgentInfo{
			Slug:      a.Config.Slug,
			Expertise: a.Config.Expertise,
		}
	}
	return infos
}

// hasActiveAgents returns true if any agent is in an active (non-idle, non-done) phase.
func (m StreamModel) hasActiveAgents() bool {
	for _, a := range m.runtime.AgentService.List() {
		state, ok := m.runtime.AgentService.GetState(a.Config.Slug)
		if !ok {
			continue
		}
		switch state.Phase {
		case agent.PhaseBuildContext, agent.PhaseStreamLLM, agent.PhaseExecuteTool:
			return true
		}
	}
	return false
}

// startDelegations steers and starts a set of delegations.
func (m *StreamModel) startDelegations(delegations []orchestration.Delegation) {
	for _, d := range delegations {
		m.appendSystemMessage(fmt.Sprintf("Dispatch → %s: %s", m.agentDisplayName(d.AgentSlug), summarizeTask(d.Task)))
		steerMsg := orchestration.FormatSteerMessage(d)
		_ = m.runtime.AgentService.Steer(d.AgentSlug, steerMsg)
		_ = m.runtime.AgentService.FollowUp(d.AgentSlug, d.Task)
		m.runtime.AgentService.EnsureRunning(d.AgentSlug)
		if !m.wiredAgents[d.AgentSlug] {
			m.wireAgent(d.AgentSlug)
		}
		m.noteAgentActivity(d.AgentSlug, time.Now())
	}
	m.updateSpinnerLabel()
}

// resetDelegationState clears queued delegations and streaming state after a reconfigure.
func (m *StreamModel) resetDelegationState() {
	m.queuedDelegations = nil
	m.streaming = make(map[string]string)
	m.pendingLeadTask = ""
	m.pendingLeadHints = nil
	m.lastAgentEvent = make(map[string]time.Time)
	m.lastAgentPulse = make(map[string]time.Time)
	m.loading = false
	m.spinner.SetActive(false)
	m.spinnerTicking = false
}

// rewireAllAgents resets event wiring and re-wires all agents after a reconfigure.
func (m *StreamModel) rewireAllAgents() {
	m.wiredAgents = make(map[string]bool)
	for _, a := range m.runtime.AgentService.List() {
		m.wireAgent(a.Config.Slug)
	}
	m.updateRoster()
}

// waitForAgentEvent returns a tea.Cmd that blocks until the next agent event arrives.
func waitForAgentEvent(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *StreamModel) noteAgentActivity(slug string, at time.Time) {
	m.lastAgentEvent[slug] = at
	m.lastAgentPulse[slug] = at
}

func (m *StreamModel) handlePhaseChange(msg PhaseChangeMsg) {
	switch msg.To {
	case string(agent.PhaseBuildContext):
		m.appendSystemMessage(fmt.Sprintf("%s is preparing context.", m.agentDisplayName(msg.AgentSlug)))
	case string(agent.PhaseStreamLLM):
		if msg.AgentSlug == m.runtime.TeamLeadSlug {
			m.appendSystemMessage(fmt.Sprintf("%s is coordinating the response.", m.agentDisplayName(msg.AgentSlug)))
		} else {
			m.appendSystemMessage(fmt.Sprintf("%s is actively working.", m.agentDisplayName(msg.AgentSlug)))
		}
	case string(agent.PhaseExecuteTool):
		m.appendSystemMessage(fmt.Sprintf("%s is running tools.", m.agentDisplayName(msg.AgentSlug)))
	}
}

func (m *StreamModel) emitProgressPulses(now time.Time) {
	if !m.loading {
		return
	}
	for _, a := range m.runtime.AgentService.List() {
		state, ok := m.runtime.AgentService.GetState(a.Config.Slug)
		if !ok || !isActivePhase(state.Phase) {
			continue
		}
		last := m.lastAgentPulse[a.Config.Slug]
		if !last.IsZero() && now.Sub(last) < 3*time.Second {
			continue
		}
		m.lastAgentPulse[a.Config.Slug] = now
		m.appendSystemMessage(progressPulseText(a.Config.Name, a.Config.Slug == m.runtime.TeamLeadSlug, state.Phase, state.CurrentTask))
	}
}

func (m *StreamModel) updateSpinnerLabel() {
	label := activeWorkSummary(m.runtime.AgentService.List(), m.runtime)
	if label == "" && len(m.queuedDelegations) > 0 {
		label = fmt.Sprintf("%d task(s) queued", len(m.queuedDelegations))
	}
	if label == "" {
		label = "waiting for work..."
	}
	m.spinner.SetLabel(label)
}

// updateAgentStatus updates the status entry for an agent (used by channel mode).
func (m *StreamModel) updateAgentStatus(slug, name, activity string) {
	for i, s := range m.agentStatus {
		if s.Slug == slug {
			m.agentStatus[i].Activity = activity
			return
		}
	}
	m.agentStatus = append(m.agentStatus, AgentStatusEntry{
		Slug:     slug,
		Name:     name,
		Activity: activity,
	})
}

// renderAgentStatusLine renders a compact 1-line agent activity bar.
// Format: ● CEO talking  ◐ PM thinking  ⚡ FE coding  ○ BE idle
func (m StreamModel) renderAgentStatusLine(width int) string {
	var parts []string
	for _, s := range m.agentStatus {
		icon := agentActivityIcon(s.Activity)
		label := agentActivityLabel(s.Activity)
		shortName := s.Name
		if len(shortName) > 8 {
			shortName = shortName[:8]
		}
		style := agentActivityStyle(s.Activity)
		part := style.Render(icon + " " + shortName + " " + label)
		parts = append(parts, part)
	}
	line := strings.Join(parts, "  ")

	barStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(MutedColor)).
		Width(width)
	return barStyle.Render(line)
}

func agentActivityIcon(activity string) string {
	switch activity {
	case "text":
		return "●"
	case "thinking":
		return "◐"
	case "tool_use", "tool_result":
		return "⚡"
	default:
		return "○"
	}
}

func agentActivityLabel(activity string) string {
	switch activity {
	case "text":
		return "talking"
	case "thinking":
		return "thinking"
	case "tool_use", "tool_result":
		return "coding"
	default:
		return "idle"
	}
}

func agentActivityStyle(activity string) lipgloss.Style {
	switch activity {
	case "text":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Success))
	case "thinking":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Warning))
	case "tool_use", "tool_result":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(NexPurple))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor))
	}
}

func (m *StreamModel) appendSystemMessage(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	m.messages = append(m.messages, StreamMessage{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	})
}

func applySoloSetup() tea.Cmd {
	return func() tea.Msg {
		notice, err := setup.InstallLatestCLI()
		return SetupApplyMsg{Notice: notice, Err: err}
	}
}

func (m *StreamModel) ensureSpinnerTick() tea.Cmd {
	if !m.spinner.IsActive() || m.spinnerTicking {
		return nil
	}
	m.spinnerTicking = true
	return m.spinner.Tick()
}

func (m StreamModel) initialWelcome() string {
	return m.runtimeSummary()
}

func (m StreamModel) runtimeSummary() string {
	packName := m.runtime.PackSlug
	if pack := agent.GetPack(m.runtime.PackSlug); pack != nil {
		packName = pack.Name
	}
	agents := m.runtime.AgentService.List()
	if len(agents) == 0 {
		return fmt.Sprintf("No active agents in %s.", packName)
	}
	return fmt.Sprintf("%s ready with %d agents: %s. Use @slug for direct work.", packName, len(agents), m.formatAgentNames(agentSlugs(agents)))
}

func (m StreamModel) renderAgentsSnapshot() string {
	packName := m.runtime.PackSlug
	if pack := agent.GetPack(m.runtime.PackSlug); pack != nil {
		packName = pack.Name
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("%s roster:", packName))
	for _, a := range m.runtime.AgentService.List() {
		role := "specialist"
		if a.Config.Slug == m.runtime.TeamLeadSlug {
			role = "lead"
		}
		lines = append(lines, fmt.Sprintf("- @%s %s [%s] — %s", a.Config.Slug, a.Config.Name, role, phaseLabel(string(a.State.Phase))))
	}
	return strings.Join(lines, "\n")
}

func (m StreamModel) agentDisplayName(slug string) string {
	if ma, ok := m.runtime.AgentService.Get(slug); ok {
		return ma.Config.Name
	}
	return slug
}

func (m StreamModel) formatAgentNames(slugs []string) string {
	names := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		names = append(names, m.agentDisplayName(slug))
	}
	return strings.Join(names, ", ")
}

func (m StreamModel) displayAgentText(slug, text string) string {
	clean := sanitizeAgentOutput(text)
	if clean == "" {
		return ""
	}
	return clean
}

func sanitizeAgentOutput(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if idx := debugMetadataPattern.FindStringIndex(text); idx != nil {
		text = text[:idx[0]]
	}
	text = strings.TrimSpace(text)
	text = blankLinePattern.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func condenseLeadOutput(text string) string {
	lines := nonEmptyLines(text)
	if len(lines) == 0 {
		return ""
	}
	var kept []string
	kept = append(kept, clipSentence(lines[0], 180))
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") {
			kept = append(kept, clipSentence(trimmed, 140))
		}
	}
	return strings.Join(kept, "\n")
}

func nonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func clipSentence(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	if idx := strings.LastIndex(cut, " "); idx > limit/2 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut) + "..."
}

func summarizeTask(task string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		return "new task"
	}
	return clipSentence(task, 90)
}

func agentSlugs(agents []*agent.ManagedAgent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.Config.Slug)
	}
	return out
}

func wrapForViewport(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	wrapped := lipgloss.NewStyle().MaxWidth(width).Render(text)
	return strings.Split(wrapped, "\n")
}

func isActivePhase(phase agent.AgentPhase) bool {
	switch phase {
	case agent.PhaseBuildContext, agent.PhaseStreamLLM, agent.PhaseExecuteTool:
		return true
	default:
		return false
	}
}

func progressPulseText(name string, isLead bool, phase agent.AgentPhase, task string) string {
	shortTask := summarizeTask(task)
	switch phase {
	case agent.PhaseBuildContext:
		if shortTask != "" {
			return fmt.Sprintf("%s is still preparing: %s", name, shortTask)
		}
		return fmt.Sprintf("%s is still preparing context.", name)
	case agent.PhaseExecuteTool:
		return fmt.Sprintf("%s is still running tools.", name)
	case agent.PhaseStreamLLM:
		if isLead {
			return fmt.Sprintf("%s is still coordinating the team response.", name)
		}
		if shortTask != "" {
			return fmt.Sprintf("%s is still working on: %s", name, shortTask)
		}
		return fmt.Sprintf("%s is still working.", name)
	default:
		return fmt.Sprintf("%s is still active.", name)
	}
}

func activeWorkSummary(agents []*agent.ManagedAgent, rt *Runtime) string {
	parts := make([]string, 0, len(agents))
	for _, a := range agents {
		switch a.State.Phase {
		case agent.PhaseBuildContext:
			parts = append(parts, fmt.Sprintf("%s preparing", a.Config.Name))
		case agent.PhaseStreamLLM:
			if a.Config.Slug == rt.TeamLeadSlug {
				parts = append(parts, fmt.Sprintf("%s coordinating", a.Config.Name))
			} else {
				parts = append(parts, fmt.Sprintf("%s working", a.Config.Name))
			}
		case agent.PhaseExecuteTool:
			parts = append(parts, fmt.Sprintf("%s using tools", a.Config.Name))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	summary := strings.Join(parts, " • ")
	return clipSentence(summary, 110)
}
