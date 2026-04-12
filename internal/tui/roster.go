package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const rosterWidth = 28

// latestTextLen is the max chars of streaming text shown per agent row.
const latestTextLen = 22

var activePhases = map[string]bool{
	"build_context": true,
	"stream_llm":    true,
	"execute_tool":  true,
	// Gossip-driven activity phases.
	"talking":   true,
	"thinking":  true,
	"coding":    true,
	"listening": true,
}

type AgentEntry struct {
	Slug  string
	Name  string
	Phase string
}

type RosterModel struct {
	agents     []AgentEntry
	spinner    SpinnerModel
	width      int
	latestText map[string]string // last streaming snippet per agent
	cursor     int               // selected agent index (-1 = none)
	focused    bool              // roster has keyboard focus
}

func NewRoster() RosterModel {
	s := NewSpinner("")
	return RosterModel{
		spinner:    s,
		width:      rosterWidth,
		latestText: make(map[string]string),
		cursor:     -1,
	}
}

// SetAgentText updates the latest streaming snippet shown for an agent.
func (r *RosterModel) SetAgentText(slug, text string) {
	if r.latestText == nil {
		r.latestText = make(map[string]string)
	}
	// Keep only the trailing portion so it fits on one line.
	if len([]rune(text)) > latestTextLen {
		runes := []rune(text)
		text = "…" + string(runes[len(runes)-latestTextLen+1:])
	}
	r.latestText[slug] = text
}

// SetCursor moves the selection cursor. Pass -1 to clear.
func (r *RosterModel) SetCursor(i int) {
	if len(r.agents) == 0 {
		r.cursor = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(r.agents) {
		i = len(r.agents) - 1
	}
	r.cursor = i
}

// CursorAgent returns the slug of the currently selected agent, or "".
func (r *RosterModel) CursorAgent() string {
	if r.cursor < 0 || r.cursor >= len(r.agents) {
		return ""
	}
	return r.agents[r.cursor].Slug
}

// AgentCount returns the number of agents in the roster.
func (r *RosterModel) AgentCount() int {
	return len(r.agents)
}

// SetFocused marks the roster as having keyboard focus.
func (r *RosterModel) SetFocused(b bool) {
	r.focused = b
	if !b {
		r.cursor = -1
	}
}

func (r *RosterModel) UpdateAgents(agents []AgentEntry) {
	r.agents = agents

	// Keep spinner active if any agent is in an active phase
	anyActive := false
	for _, ag := range agents {
		if activePhases[ag.Phase] {
			anyActive = true
			break
		}
	}
	r.spinner.SetActive(anyActive)
}

// UpdateFromGossip maps a GossipEvent type to a roster activity phase for the agent.
func (r *RosterModel) UpdateFromGossip(slug, eventType string) {
	phase := gossipEventToPhase(eventType)
	for i, ag := range r.agents {
		if ag.Slug == slug {
			r.agents[i].Phase = phase
			break
		}
	}
	r.UpdateAgents(r.agents)
}

// SetAgentPhase directly sets an agent's phase (for non-gossip state like "dead").
func (r *RosterModel) SetAgentPhase(slug, phase string) {
	for i, ag := range r.agents {
		if ag.Slug == slug {
			r.agents[i].Phase = phase
			break
		}
	}
	r.UpdateAgents(r.agents)
}

// gossipEventToPhase maps gossip event types to roster display phases.
func gossipEventToPhase(eventType string) string {
	switch eventType {
	case "text":
		return "talking"
	case "thinking":
		return "thinking"
	case "tool_use":
		return "coding"
	case "tool_result":
		return "coding"
	default:
		return "listening"
	}
}

func (r RosterModel) Update(msg tea.Msg) (RosterModel, tea.Cmd) {
	var cmd tea.Cmd
	r.spinner, cmd = r.spinner.Update(msg)
	return r, cmd
}

func (r RosterModel) View() string {
	borderColor := "#374151"
	if r.focused {
		borderColor = NexPurple
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(NexPurple)).
		Render("TEAM")

	var rows []string
	rows = append(rows, header)

	for i, ag := range r.agents {
		icon := r.agentIcon(ag.Phase)
		nameStr := ag.Name
		maxName := rosterWidth - 11
		if maxName < 4 {
			maxName = 4
		}
		if len([]rune(nameStr)) > maxName {
			nameStr = string([]rune(nameStr)[:maxName])
		}

		label := phaseLabel(ag.Phase)
		pStyle := phaseColor(ag.Phase)

		line := pStyle.Render(icon) + " " +
			lipgloss.NewStyle().Foreground(lipgloss.Color(ValueColor)).Render(nameStr) +
			" " + pStyle.Render(label)

		// Show latest streaming text as a dim subtitle.
		if snippet, ok := r.latestText[ag.Slug]; ok && snippet != "" {
			dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
			line += "\n  " + dim.Render(snippet)
		}

		// Highlight cursor row when roster is focused.
		if r.focused && i == r.cursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#1F2937")).
				Foreground(lipgloss.Color(NexPurple)).
				Bold(true).
				Render("▶ " + strings.TrimLeft(line, " "))
		}

		rows = append(rows, line)
	}

	// Keyboard hint when focused.
	if r.focused {
		hint := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Render("↑↓ nav  d DM  Esc close")
		rows = append(rows, "", hint)
	}

	inner := strings.Join(rows, "\n")

	sidebar := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(1, 1).
		Width(rosterWidth)

	return sidebar.Render(inner)
}

func (r RosterModel) agentIcon(phase string) string {
	switch phase {
	case "idle":
		return "○"
	case "done":
		return "●"
	case "error":
		return "●"
	case "dead":
		return "✕"
	// Gossip-driven activity icons.
	case "talking":
		return "●"
	case "thinking":
		return "◐"
	case "coding":
		return "⚡"
	case "listening":
		return "◆"
	default:
		if activePhases[phase] {
			return spinnerFrames[r.spinner.frame]
		}
		return "○"
	}
}

func phaseShortLabel(phase string) string {
	switch phase {
	case "idle":
		return "idle"
	case "build_context":
		return "ctx"
	case "stream_llm":
		return "llm"
	case "execute_tool":
		return "tool"
	case "done":
		return "done"
	case "error":
		return "err"
	case "dead":
		return "dead"
	case "talking":
		return "talk"
	case "thinking":
		return "think"
	case "coding":
		return "code"
	case "listening":
		return "listen"
	default:
		return phase
	}
}

func phaseLabel(phase string) string {
	switch phase {
	case "build_context":
		return "preparing"
	case "stream_llm":
		return "thinking"
	case "execute_tool":
		return "running tool"
	case "idle":
		return "idle"
	case "done":
		return "done"
	case "error":
		return "error"
	case "dead":
		return "exited"
	// Gossip-driven labels.
	case "talking":
		return "talking"
	case "thinking":
		return "thinking"
	case "coding":
		return "coding"
	case "listening":
		return "listening"
	default:
		return phase
	}
}

func phaseColor(phase string) lipgloss.Style {
	switch phase {
	case "build_context":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Warning))
	case "stream_llm":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Info))
	case "execute_tool":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(NexPurple))
	case "done":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Success))
	case "error", "dead":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Error))
	// Gossip-driven colors.
	case "talking":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Success))
	case "thinking":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Warning))
	case "coding":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(NexPurple))
	case "listening":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Info))
	default:
		return SystemStyle
	}
}
