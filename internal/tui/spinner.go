package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type SpinnerModel struct {
	frame  int
	label  string
	active bool
}

func NewSpinner(label string) SpinnerModel {
	return SpinnerModel{label: label}
}

func (s SpinnerModel) Tick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return SpinnerTickMsg{Time: t}
	})
}

func (s SpinnerModel) Update(msg tea.Msg) (SpinnerModel, tea.Cmd) {
	switch msg.(type) {
	case SpinnerTickMsg:
		if s.active {
			s.frame = (s.frame + 1) % len(spinnerFrames)
			return s, s.Tick()
		}
	}
	return s, nil
}

func (s SpinnerModel) View() string {
	if !s.active {
		return ""
	}
	return SystemStyle.Render(spinnerFrames[s.frame] + " " + s.label)
}

func (s *SpinnerModel) SetActive(active bool) {
	s.active = active
}

func (s *SpinnerModel) SetLabel(label string) {
	s.label = label
}

func (s SpinnerModel) IsActive() bool {
	return s.active
}
