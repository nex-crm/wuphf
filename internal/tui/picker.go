package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type PickerOption struct {
	Label       string
	Value       string
	Description string
}

type PickerModel struct {
	Title      string
	Options    []PickerOption
	query      string
	filtered   []int
	selected   int
	active     bool
	OnSelect   func(value string)
	TextInput  bool   // when true, shows a text input instead of options
	TextPrompt string // label for the text input
	textBuf    []rune
	textPos    int
}

func NewPicker(title string, options []PickerOption) PickerModel {
	p := PickerModel{
		Title:   title,
		Options: options,
	}
	p.applyFilter("")
	return p
}

func (p PickerModel) Update(msg tea.Msg) (PickerModel, tea.Cmd) {
	if !p.active {
		return p, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if p.TextInput {
			return p.updateTextInput(msg)
		}
		switch msg.String() {
		case "up":
			p.moveSelection(-1)
		case "down":
			p.moveSelection(1)
		case "backspace":
			if len(p.query) > 0 {
				query := []rune(p.query)
				p.applyFilter(string(query[:len(query)-1]))
			}
		case "esc":
			p.active = false
		case "enter":
			if opt, ok := p.selectedOption(); ok {
				if p.OnSelect != nil {
					p.OnSelect(opt.Value)
				}
				return p, func() tea.Msg {
					return PickerSelectMsg{Value: opt.Value, Label: opt.Label}
				}
			}
		case "ctrl+u":
			p.applyFilter("")
		}

		switch msg.Type {
		case tea.KeyRunes:
			if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
				idx := int(msg.Runes[0] - '1')
				if idx < len(p.filtered) {
					p.selected = idx
					if opt, ok := p.selectedOption(); ok {
						if p.OnSelect != nil {
							p.OnSelect(opt.Value)
						}
						return p, func() tea.Msg {
							return PickerSelectMsg{Value: opt.Value, Label: opt.Label}
						}
					}
				}
			}
			p.applyFilter(p.query + string(msg.Runes))
		case tea.KeySpace:
			p.applyFilter(p.query + " ")
		}
	}
	return p, nil
}

func (p PickerModel) updateTextInput(msg tea.KeyMsg) (PickerModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := strings.TrimSpace(string(p.textBuf))
		return p, func() tea.Msg {
			return PickerSelectMsg{Value: value, Label: value}
		}
	case "backspace":
		if len(p.textBuf) > 0 {
			p.textBuf = p.textBuf[:len(p.textBuf)-1]
		}
	case "esc":
		p.active = false
		return p, func() tea.Msg {
			return PickerSelectMsg{Value: "", Label: ""}
		}
	default:
		// Handle both single chars and pasted text (multi-rune burst)
		if msg.Type == tea.KeyRunes {
			p.textBuf = append(p.textBuf, msg.Runes...)
		} else {
			runes := []rune(msg.String())
			for _, r := range runes {
				if r >= 32 {
					p.textBuf = append(p.textBuf, r)
				}
			}
		}
	}
	return p, nil
}

// TextValue returns the current text input value.
func (p PickerModel) TextValue() string {
	return string(p.textBuf)
}

func (p PickerModel) View() string {
	if !p.active {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(TitleStyle.Render(p.Title) + "\n")

	if p.TextInput {
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(NexBlue)).Bold(true)
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor))

		sb.WriteString("\n")
		sb.WriteString(promptStyle.Render(p.TextPrompt) + "\n\n")
		sb.WriteString("  " + string(p.textBuf) + cursorStyle.Render(" ") + "\n\n")
		sb.WriteString(mutedStyle.Render("  Enter to confirm · Esc to cancel") + "\n")
		return sb.String()
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

	searchStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(MutedColor))

	var rows []string
	rows = append(rows, TitleStyle.Render(p.Title))
	rows = append(rows, searchStyle.Render("Search: "+p.query))

	if len(p.filtered) == 0 {
		rows = append(rows, descStyle.Render("No matches"))
		return strings.Join(rows, "\n")
	}

	for i, idx := range p.filtered {
		opt := p.Options[idx]
		prefix := "  "
		labelStyle := normal
		if i == p.selected {
			prefix = "> "
			labelStyle = highlighted
		}

		label := opt.Label
		if i < 9 {
			label = fmt.Sprintf("%d. %s", i+1, label)
		}
		row := prefix + labelStyle.Render(label)
		if opt.Description != "" {
			row += " " + descStyle.Render(opt.Description)
		}
		rows = append(rows, row)
	}

	return strings.Join(rows, "\n")
}

func (p *PickerModel) SetActive(active bool) {
	p.active = active
	if active {
		p.applyFilter("")
	}
}

func (p PickerModel) IsActive() bool {
	return p.active
}

func (p *PickerModel) applyFilter(query string) {
	p.query = query
	p.selected = 0

	if len(p.Options) == 0 {
		p.filtered = nil
		return
	}

	if strings.TrimSpace(query) == "" {
		p.filtered = p.filtered[:0]
		for i := range p.Options {
			p.filtered = append(p.filtered, i)
		}
		return
	}

	matches := fuzzy.FindFrom(query, pickerOptions(p.Options))
	p.filtered = p.filtered[:0]
	for _, match := range matches {
		p.filtered = append(p.filtered, match.Index)
	}
}

func (p *PickerModel) moveSelection(delta int) {
	if len(p.filtered) == 0 {
		return
	}

	next := p.selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(p.filtered) {
		next = len(p.filtered) - 1
	}
	p.selected = next
}

func (p PickerModel) selectedOption() (PickerOption, bool) {
	if len(p.filtered) == 0 || p.selected < 0 || p.selected >= len(p.filtered) {
		return PickerOption{}, false
	}
	return p.Options[p.filtered[p.selected]], true
}

func (p *PickerModel) UpdateQuery(query string) {
	p.applyFilter(query)
}

func (p PickerModel) Query() string {
	return p.query
}

func (p PickerModel) FilteredOptions() []PickerOption {
	if len(p.filtered) == 0 {
		return nil
	}

	options := make([]PickerOption, 0, len(p.filtered))
	for _, idx := range p.filtered {
		options = append(options, p.Options[idx])
	}
	return options
}

type pickerOptions []PickerOption

func (p pickerOptions) String(i int) string {
	return p[i].Label
}

func (p pickerOptions) Len() int {
	return len(p)
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
