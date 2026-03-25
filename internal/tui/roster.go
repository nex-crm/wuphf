package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const rosterWidth = 28

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
	agents  []AgentEntry
	spinner SpinnerModel
	width   int
}

func NewRoster() RosterModel {
	s := NewSpinner("")
	return RosterModel{
		spinner: s,
		width:   rosterWidth,
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
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(NexPurple)).
		Render("TEAM")

	var rows []string
	rows = append(rows, header)

	for _, ag := range r.agents {
		icon := r.agentIcon(ag.Phase)
		nameStr := ag.Name
		if len(nameStr) > rosterWidth-11 {
			nameStr = nameStr[:rosterWidth-11]
		}

		label := phaseLabel(ag.Phase)
		pStyle := phaseColor(ag.Phase)

		line := pStyle.Render(icon) + " " +
			lipgloss.NewStyle().Foreground(lipgloss.Color(ValueColor)).Render(nameStr) +
			" " + pStyle.Render(label)

		rows = append(rows, line)
	}

	inner := strings.Join(rows, "\n")

	sidebar := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
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
