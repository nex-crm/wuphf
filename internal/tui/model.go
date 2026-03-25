package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/chat"
	"github.com/nex-crm/wuphf/internal/config"
)

// channelTickMsg triggers periodic tmux capture and adapter polling.
type channelTickMsg struct{}

// ViewName identifies a top-level view.
type ViewName string

const (
	ViewStream ViewName = "stream"
	ViewHelp   ViewName = "help"
	ViewAgents ViewName = "agents"
	ViewChat   ViewName = "chat"
)

// embeddedTickMsg triggers periodic re-renders for async VT emulator updates.
type embeddedTickMsg struct{}

// Model is the root bubbletea model that owns the stream view and agent infrastructure.
type Model struct {
	stream  StreamModel
	runtime *Runtime

	// Embedded terminal mode (when claude binary is available + --panes flag).
	embedded    bool
	paneManager *PaneManager
	gossipBus   *GossipBus
	roster      RosterModel

	// Channel mode: tmux background agents + clean channel view (default when tmux available).
	channelMode    bool
	tmuxManager    *TmuxManager
	channelBus     *GossipBus
	channelAdapter *ChannelAdapter

	currentView ViewName
	width       int
	height      int

	doublePress *DoublePress
	inputMode   InputMode

	welcomed  bool
	hasAPIKey bool
}

// NewModel creates the root model with an agent service, message router, and stream view.
// panesMode=true forces old multi-pane embedded mode. Default: channel mode with tmux.
func NewModel(panesMode bool) Model {
	events := make(chan tea.Msg, 256)
	rt := NewRuntime(events)

	hasAPIKey := config.ResolveAPIKey("") != ""

	m := Model{
		runtime:     rt,
		currentView: ViewStream,
		doublePress: NewDoublePress(time.Second),
		inputMode:   ModeInsert,
		hasAPIKey:   hasAPIKey,
	}

	if panesMode && HasClaude() {
		// Embedded terminal mode (--panes flag): each agent gets its own PTY.
		pm := NewPaneManager()
		bus := NewGossipBus(rt.TeamLeadSlug)
		roster := NewRoster()

		var entries []AgentEntry
		for _, a := range rt.AgentService.List() {
			entries = append(entries, AgentEntry{
				Slug:  a.Config.Slug,
				Name:  a.Config.Name,
				Phase: "idle",
			})
		}
		roster.UpdateAgents(entries)

		m.embedded = true
		m.paneManager = pm
		m.gossipBus = bus
		m.roster = roster
	} else if HasTmux() && HasClaude() {
		// Channel mode (default): agents run in tmux, output feeds into stream view.
		stream := NewStreamModel(rt, events)
		stream.channelMode = true
		m.stream = stream
		m.channelMode = true
	} else {
		// Classic stream mode: fallback when claude/tmux unavailable.
		stream := NewStreamModel(rt, events)
		for _, a := range rt.AgentService.List() {
			stream.wireAgent(a.Config.Slug)
		}
		stream.updateRoster()
		m.stream = stream
	}

	return m
}

// Init starts the appropriate mode.
func (m Model) Init() tea.Cmd {
	if m.embedded {
		// Bootstrap panes in Init (needs terminal size, but we start with defaults).
		w, h := 120, 40 // will be resized on first WindowSizeMsg
		if err := m.runtime.BootstrapPanes(m.paneManager, m.gossipBus, w, h); err != nil {
			// Fall through — panes will show as empty.
		}
		return tea.Batch(
			embeddedTick(),
		)
	}

	if m.channelMode {
		// Bootstrap tmux background agents.
		tm, bus, adapter, err := m.runtime.BootstrapTmuxChannel()
		if err != nil {
			// Fall back to classic stream mode on error.
			m.channelMode = false
			m.stream.channelMode = false
		} else {
			m.tmuxManager = tm
			m.channelBus = bus
			m.channelAdapter = adapter

			// Start capture polling goroutine.
			go m.tmuxCaptureLoop(tm, bus, adapter)
		}
	}

	var welcomeCmd tea.Cmd
	if !m.hasAPIKey {
		welcomeCmd = func() tea.Msg {
			return SlashResultMsg{Output: "Welcome! Run /init to get started."}
		}
	}

	var channelCmd tea.Cmd
	if m.channelMode {
		channelCmd = channelTick()
	}

	return tea.Batch(
		m.stream.Init(),
		waitForAgentEvent(m.runtime.Events),
		welcomeCmd,
		channelCmd,
	)
}

// Update handles all messages, routing to sub-models as appropriate.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.embedded && m.paneManager != nil {
			m.paneManager.ResizeAll(msg.Width, msg.Height)
		}

	case tea.KeyMsg:
		// Ctrl+C always uses double-press.
		if msg.String() == "ctrl+c" {
			if m.doublePress.Press() {
				m.shutdownPanes()
				m.shutdownTmux()
				return m, tea.Quit
			}
			if m.embedded {
				return m, nil
			}
			hint := func() tea.Msg {
				return SlashResultMsg{Output: "Press Ctrl+C again to exit"}
			}
			return m, hint
		}

		// Ctrl+T: show tmux attach hint.
		if msg.String() == "ctrl+t" && m.channelMode && m.tmuxManager != nil {
			hint := m.tmuxManager.AttachHint("")
			return m, func() tea.Msg {
				return SlashResultMsg{Output: "View agent terminals: " + hint}
			}
		}

		if m.embedded {
			return m.updateEmbedded(msg)
		}

		// Classic mode key handling below.
		if m.currentView != ViewStream {
			switch msg.String() {
			case "q", "esc", "c":
				m.currentView = ViewStream
			}
			return m, nil
		}

		action := MapKey(m.inputMode, msg)
		switch action {
		case ActionInsertMode:
			m.inputMode = ModeInsert
		case ActionNormalMode:
			m.inputMode = ModeNormal
		case ActionHelp:
			m.currentView = ViewHelp
			return m, nil
		case ActionAgents:
			m.currentView = ViewAgents
			return m, nil
		}

	case channelTickMsg:
		if m.channelMode && m.channelAdapter != nil {
			// Drain adapter messages into stream view.
			drained := 0
			for drained < 20 {
				select {
				case smsg := <-m.channelAdapter.Messages():
					m.stream.messages = append(m.stream.messages, smsg)
					m.stream.scrollOffset = 0 // auto-scroll to bottom
					drained++
				default:
					goto doneDrain
				}
			}
		doneDrain:

			// Update agent status line from gossip activity.
			if m.channelBus != nil {
				for _, a := range m.runtime.AgentService.List() {
					activity := m.channelBus.GetActivity(a.Config.Slug)
					m.stream.updateAgentStatus(a.Config.Slug, a.Config.Name, activity)
				}
			}

			return m, channelTick()
		}

	case embeddedTickMsg:
		if m.embedded {
			// Flush any pending gossip batches.
			m.gossipBus.FlushPending()

			// Update roster from latest gossip activity per agent.
			for _, p := range m.paneManager.Panes() {
				activity := m.gossipBus.GetActivity(p.Slug())
				if activity != "idle" {
					m.roster.UpdateFromGossip(p.Slug(), activity)
				}
			}

			// Mark dead panes in roster.
			for _, p := range m.paneManager.Panes() {
				if !p.IsAlive() {
					m.roster.SetAgentPhase(p.Slug(), "dead")
				}
			}

			return m, embeddedTick()
		}

	case ViewSwitchMsg:
		m.currentView = msg.Target
		return m, nil

	case AgentTextMsg, AgentDoneMsg, AgentErrorMsg, PhaseChangeMsg,
		AgentThinkingMsg, AgentToolUseMsg, AgentToolResultMsg:
		if !m.embedded {
			var cmd tea.Cmd
			m.stream, cmd = m.stream.Update(msg)
			return m, tea.Batch(cmd, waitForAgentEvent(m.runtime.Events))
		}
	}

	if !m.embedded {
		var cmd tea.Cmd
		m.stream, cmd = m.stream.Update(msg)
		return m, cmd
	}

	return m, nil
}

// updateEmbedded handles key routing in embedded terminal mode.
func (m Model) updateEmbedded(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "ctrl+b":
		// Toggle broadcast mode.
		m.paneManager.SetBroadcastMode(!m.paneManager.IsBroadcastMode())
		return m, nil

	case "ctrl+n":
		m.paneManager.FocusNext()
		return m, nil

	case "ctrl+p":
		m.paneManager.FocusPrev()
		return m, nil
	}

	// Ctrl+1..7: jump to pane by index.
	if len(key) == 6 && key[:5] == "ctrl+" && key[5] >= '1' && key[5] <= '7' {
		idx := int(key[5]-'0') - 1
		panes := m.paneManager.Panes()
		if idx < len(panes) {
			m.paneManager.FocusPane(panes[idx].Slug())
		}
		return m, nil
	}

	// Forward to focused pane (or all in broadcast mode).
	data := keyToBytes(msg)
	if m.paneManager.IsBroadcastMode() {
		for _, p := range m.paneManager.Panes() {
			p.SendKey(data)
		}
		// Also broadcast via gossip bus for context.
		m.gossipBus.BroadcastUserMessage(string(data))
	} else if f := m.paneManager.Focused(); f != nil {
		f.SendKey(data)
	}

	return m, nil
}

// keyToBytes converts a tea.KeyMsg to raw bytes for PTY input.
func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte("\r")
	case "tab":
		return []byte("\t")
	case "backspace":
		return []byte{0x7f}
	case "esc":
		return []byte{0x1b}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	default:
		// For regular characters and unknown sequences.
		runes := msg.Runes
		if len(runes) > 0 {
			return []byte(string(runes))
		}
		return []byte(msg.String())
	}
}

// shutdownPanes gracefully closes all terminal panes.
func (m *Model) shutdownPanes() {
	if m.paneManager == nil {
		return
	}
	m.paneManager.CloseAll()
}

// shutdownTmux kills the tmux session on exit.
func (m *Model) shutdownTmux() {
	if m.tmuxManager == nil {
		return
	}
	_ = m.tmuxManager.KillSession()
}

// tmuxCaptureLoop polls tmux panes every 500ms, parses NDJSON output,
// and feeds events through GossipBus → ChannelAdapter.
func (m *Model) tmuxCaptureLoop(tm *TmuxManager, bus *GossipBus, adapter *ChannelAdapter) {
	// Track last captured content per agent to only process new lines.
	lastContent := make(map[string]string)
	// Track how many events we've already forwarded to the adapter.
	lastEventIdx := 0

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		windows, err := tm.ListWindows()
		if err != nil {
			continue
		}
		for _, slug := range windows {
			content, err := tm.CapturePaneContent(slug)
			if err != nil || content == lastContent[slug] {
				continue
			}

			// Find new lines by comparing with last capture.
			prev := lastContent[slug]
			lastContent[slug] = content

			newPart := content
			if len(prev) > 0 && strings.HasPrefix(content, prev) {
				newPart = content[len(prev):]
			}

			// Parse new lines as NDJSON and emit to bus.
			for _, line := range strings.Split(newPart, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || line[0] != '{' {
					continue
				}
				obs := NewOutputObserver(slug, bus, strings.NewReader(line+"\n"))
				obs.run()
			}
		}

		// Flush pending gossip events.
		bus.FlushPending()

		// Feed only NEW gossip events to the channel adapter.
		allEvents := bus.EventLog()
		for i := lastEventIdx; i < len(allEvents); i++ {
			adapter.HandleEvent(allEvents[i])
		}
		lastEventIdx = len(allEvents)
	}
}

// channelTick returns a command that fires a channelTickMsg after 500ms.
func channelTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return channelTickMsg{}
	})
}

// embeddedTick returns a command that fires an embeddedTickMsg after 100ms.
func embeddedTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return embeddedTickMsg{}
	})
}

// View renders based on the current top-level view.
func (m Model) View() string {
	if m.embedded {
		switch m.currentView {
		case ViewHelp:
			return m.renderHelpView()
		case ViewAgents:
			return m.renderAgentsView()
		default:
			if m.paneManager != nil {
				w, h := m.width, m.height
				if w == 0 {
					w = 120
				}
				if h == 0 {
					h = 40
				}
				return m.paneManager.View(w, h)
			}
			return "No panes available"
		}
	}

	switch m.currentView {
	case ViewHelp:
		return m.renderHelpView()
	case ViewAgents:
		return m.renderAgentsView()
	case ViewChat:
		return m.renderChatView()
	default:
		return m.stream.View()
	}
}

// renderHelpView renders the keybinding reference screen.
func (m Model) renderHelpView() string {
	title := TitleStyle.Render("Help — Keybindings")
	body := `
Normal Mode:
  j / ↓        Scroll down
  k / ↑        Scroll up
  g            Scroll to top
  G / End      Scroll to bottom
  Ctrl+D       Half page down
  Ctrl+U       Half page up
  i            Enter insert mode
  /            Search
  ?            Help (this screen)
  a            Agents list
  c            Back to chat
  q            Quit

Insert Mode:
  Esc          Normal mode
  Enter        Submit message
  Tab          Autocomplete next
  Shift+Tab    Autocomplete prev
  Ctrl+C       Cancel / exit hint
`
	footer := SystemStyle.Render("  Press 'q', 'esc', or 'c' to return.")
	return title + body + footer
}

// renderAgentsView renders the active agent list.
func (m Model) renderAgentsView() string {
	title := TitleStyle.Render("Active Agents")
	agents := m.runtime.AgentService.List()
	if len(agents) == 0 {
		footer := SystemStyle.Render("  No agents active. Press 'c' or 'esc' to return.")
		return title + "\n\n" + footer
	}

	var lines []string
	lines = append(lines, title, "")
	for _, a := range agents {
		role := "specialist"
		if a.Config.Slug == m.runtime.TeamLeadSlug {
			role = "lead"
		}
		line := fmt.Sprintf("  @%-12s %-18s %-10s %s", a.Config.Slug, a.Config.Name, role, a.State.Phase)
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, SystemStyle.Render("  Press 'c', 'esc', or 'q' to return."))
	return strings.Join(lines, "\n")
}

// renderChatView renders the chat channels and recent messages.
func (m Model) renderChatView() string {
	title := TitleStyle.Render("Chat Channels")

	cm, err := chat.NewChannelManager()
	if err != nil {
		return title + "\n\n" + SystemStyle.Render("  Error loading channels: "+err.Error()) +
			"\n\n" + SystemStyle.Render("  Press 'c', 'esc', or 'q' to return.")
	}

	channels := cm.List()
	if len(channels) == 0 {
		return title + "\n\n" + SystemStyle.Render("  No chat channels yet. Agent messages will create channels automatically.") +
			"\n\n" + SystemStyle.Render("  Press 'c', 'esc', or 'q' to return.")
	}

	var lines []string
	lines = append(lines, title, "")

	ms, msErr := chat.NewMessageStore()

	for _, ch := range channels {
		icon := "#"
		if ch.Type == chat.ChannelDirect {
			icon = "@"
		}
		header := fmt.Sprintf("  %s%-20s  (%s)  %d members", icon, ch.Name, ch.Type, len(ch.Members))
		lines = append(lines, header)

		if msErr == nil {
			msgs, _ := ms.List(ch.ID, 3)
			for _, msg := range msgs {
				ts := msg.Timestamp.Format("15:04")
				lines = append(lines, fmt.Sprintf("    [%s] %s: %s", ts, msg.SenderName, msg.Content))
			}
		}
		lines = append(lines, "")
	}

	lines = append(lines, SystemStyle.Render("  Press 'c', 'esc', or 'q' to return."))
	return strings.Join(lines, "\n")
}
