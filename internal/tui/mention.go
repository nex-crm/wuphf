package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxMentionMatches = 8

type AgentMention struct {
	Slug string
	Name string
}

type MentionModel struct {
	visible  bool
	matches  []AgentMention
	selected int
	query    string
	agents   []AgentMention
}

func NewMention(agents []AgentMention) MentionModel {
	return MentionModel{agents: agents}
}

func (m *MentionModel) UpdateAgents(agents []AgentMention) {
	m.agents = agents
}

// UpdateQuery triggers on "@" preceded by space or at start of input.
func (m *MentionModel) UpdateQuery(input string) {
	atIdx := strings.LastIndex(input, "@")
	if atIdx < 0 {
		m.visible = false
		m.query = ""
		m.matches = nil
		return
	}

	// Must be at start or preceded by whitespace
	if atIdx > 0 && input[atIdx-1] != ' ' && input[atIdx-1] != '\t' {
		m.visible = false
		m.query = ""
		m.matches = nil
		return
	}

	q := strings.ToLower(input[atIdx+1:])
	m.query = q

	var matches []AgentMention
	for _, ag := range m.agents {
		if strings.HasPrefix(strings.ToLower(ag.Slug), q) ||
			strings.HasPrefix(strings.ToLower(ag.Name), q) {
			matches = append(matches, ag)
			if len(matches) >= maxMentionMatches {
				break
			}
		}
	}

	m.matches = matches
	m.visible = len(matches) > 0
	if m.selected >= len(matches) {
		m.selected = 0
	}
}

func (m MentionModel) IsVisible() bool {
	return m.visible
}

func (m *MentionModel) Accept() string {
	if len(m.matches) == 0 {
		return ""
	}
	slug := m.matches[m.selected].Slug
	m.visible = false
	m.matches = nil
	m.query = ""
	return "@" + slug
}

func (m MentionModel) Matches() []AgentMention {
	if len(m.matches) == 0 {
		return nil
	}
	out := make([]AgentMention, len(m.matches))
	copy(out, m.matches)
	return out
}

func (m MentionModel) Selected() (AgentMention, bool) {
	if !m.visible || len(m.matches) == 0 {
		return AgentMention{}, false
	}
	return m.matches[m.selected], true
}

func (m MentionModel) SelectedIndex() int {
	return m.selected
}

func (m *MentionModel) Dismiss() {
	m.visible = false
	m.selected = 0
}

func (m *MentionModel) Next() {
	if len(m.matches) == 0 {
		return
	}
	m.selected = (m.selected + 1) % len(m.matches)
}

func (m *MentionModel) Prev() {
	if len(m.matches) == 0 {
		return
	}
	m.selected = (m.selected - 1 + len(m.matches)) % len(m.matches)
}

func (m MentionModel) Update(msg tea.Msg) (MentionModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			m.Next()
		case "shift+tab", "up":
			m.Prev()
		case "esc":
			m.Dismiss()
		}
	}
	return m, nil
}

func (m MentionModel) View() string {
	if !m.visible || len(m.matches) == 0 {
		return ""
	}

	highlighted := lipgloss.NewStyle().
		Background(lipgloss.Color(NexGreen)).
		Foreground(lipgloss.Color("#000000")).
		Padding(0, 1)

	normal := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ValueColor)).
		Padding(0, 1)

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(MutedColor)).
		Padding(0, 1)

	var rows []string
	for i, ag := range m.matches {
		label := "@" + ag.Slug
		if i == m.selected {
			rows = append(rows, highlighted.Render(label)+" "+nameStyle.Render(ag.Name))
		} else {
			rows = append(rows, normal.Render(label)+" "+nameStyle.Render(ag.Name))
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 0)

	return box.Render(strings.Join(rows, "\n"))
}
