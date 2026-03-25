package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PickerOption struct {
	Label       string
	Value       string
	Description string
}

type PickerModel struct {
	Title    string
	Options  []PickerOption
	selected int
	active   bool
	OnSelect func(value string)
}

func NewPicker(title string, options []PickerOption) PickerModel {
	return PickerModel{
		Title:   title,
		Options: options,
	}
}

func (p PickerModel) Update(msg tea.Msg) (PickerModel, tea.Cmd) {
	if !p.active {
		return p, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.selected > 0 {
				p.selected--
			}
		case "down", "j":
			if p.selected < len(p.Options)-1 {
				p.selected++
			}
		case "enter":
			if len(p.Options) > 0 {
				opt := p.Options[p.selected]
				if p.OnSelect != nil {
					p.OnSelect(opt.Value)
				}
				return p, func() tea.Msg {
					return PickerSelectMsg{Value: opt.Value, Label: opt.Label}
				}
			}
		default:
			// 1-9 quick-select
			key := msg.String()
			if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
				idx := int(key[0]-'1')
				if idx < len(p.Options) {
					p.selected = idx
					opt := p.Options[idx]
					if p.OnSelect != nil {
						p.OnSelect(opt.Value)
					}
					return p, func() tea.Msg {
						return PickerSelectMsg{Value: opt.Value, Label: opt.Label}
					}
				}
			}
		}
	}
	return p, nil
}

func (p PickerModel) View() string {
	if !p.active {
		return ""
	}

	highlighted := lipgloss.NewStyle().
		Background(lipgloss.Color(NexBlue)).
		Foreground(lipgloss.Color("#ffffff")).
		Padding(0, 1)

	normal := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ValueColor)).
		Padding(0, 1)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(MutedColor))

	var sb strings.Builder
	sb.WriteString(TitleStyle.Render(p.Title) + "\n")

	for i, opt := range p.Options {
		num := fmt.Sprintf("%d. ", i+1)
		label := num + opt.Label
		var row string
		if i == p.selected {
			row = highlighted.Render(label)
		} else {
			row = normal.Render(label)
		}
		if opt.Description != "" {
			row += " " + descStyle.Render(opt.Description)
		}
		sb.WriteString(row + "\n")
	}

	return sb.String()
}

func (p *PickerModel) SetActive(active bool) {
	p.active = active
}

func (p PickerModel) IsActive() bool {
	return p.active
}

// ConfirmModel

type ConfirmModel struct {
	Question  string
	confirmed bool
	active    bool
	answered  bool
}

func NewConfirm(question string) ConfirmModel {
	return ConfirmModel{Question: question}
}

func (c ConfirmModel) Update(msg tea.Msg) (ConfirmModel, tea.Cmd) {
	if !c.active || c.answered {
		return c, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			c.confirmed = true
			c.answered = true
			return c, func() tea.Msg { return ConfirmMsg{Confirmed: true} }
		case "n", "N", "esc":
			c.confirmed = false
			c.answered = true
			return c, func() tea.Msg { return ConfirmMsg{Confirmed: false} }
		}
	}
	return c, nil
}

func (c ConfirmModel) View() string {
	if !c.active {
		return ""
	}
	prompt := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ValueColor)).
		Render(c.Question + " [y/N] ")
	return prompt
}

func (c *ConfirmModel) SetActive(active bool) {
	c.active = active
	if active {
		c.answered = false
	}
}

func (c ConfirmModel) IsActive() bool {
	return c.active
}
