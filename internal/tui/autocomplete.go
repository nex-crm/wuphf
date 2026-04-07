package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxAutocompleteMatches = 24

type SlashCommand struct {
	Name        string
	Description string
	Category    string
}

type AutocompleteModel struct {
	visible  bool
	matches  []SlashCommand
	selected int
	query    string
	commands []SlashCommand
}

func NewAutocomplete(commands []SlashCommand) AutocompleteModel {
	return AutocompleteModel{commands: commands}
}

func (a AutocompleteModel) Update(msg tea.Msg) (AutocompleteModel, tea.Cmd) {
	if !a.visible {
		return a, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			a.Next()
		case "shift+tab", "up":
			a.Prev()
		case "esc":
			a.Dismiss()
		}
	}
	return a, nil
}

func (a AutocompleteModel) View() string {
	if !a.visible || len(a.matches) == 0 {
		return ""
	}

	highlighted := lipgloss.NewStyle().
		Background(lipgloss.Color(NexBlue)).
		Foreground(lipgloss.Color("#ffffff")).
		Padding(0, 1)

	normal := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ValueColor)).
		Padding(0, 1)

	desc := lipgloss.NewStyle().
		Foreground(lipgloss.Color(MutedColor)).
		Padding(0, 1)
	category := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D6E4FF")).
		Background(lipgloss.Color("#22324A")).
		Padding(0, 1)

	var rows []string
	for i, cmd := range a.matches {
		name := "/" + cmd.Name
		entry := ""
		if cmd.Category != "" {
			entry = category.Render(cmd.Category) + " "
		}
		if i == a.selected {
			rows = append(rows, entry+highlighted.Render(name)+" "+desc.Render(cmd.Description))
		} else {
			rows = append(rows, entry+normal.Render(name)+" "+desc.Render(cmd.Description))
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 0)

	return box.Render(strings.Join(rows, "\n"))
}

func (a *AutocompleteModel) UpdateQuery(input string) {
	if !strings.HasPrefix(input, "/") {
		a.visible = false
		a.query = ""
		a.matches = nil
		return
	}

	q := strings.ToLower(strings.TrimPrefix(input, "/"))
	a.query = q

	var matches []SlashCommand
	for _, cmd := range a.commands {
		if strings.HasPrefix(strings.ToLower(cmd.Name), q) {
			matches = append(matches, cmd)
			if len(matches) >= maxAutocompleteMatches {
				break
			}
		}
	}

	a.matches = matches
	a.visible = len(matches) > 0
	if a.selected >= len(matches) {
		a.selected = 0
	}
}

func (a AutocompleteModel) IsVisible() bool {
	return a.visible
}

func (a AutocompleteModel) Selected() (SlashCommand, bool) {
	if !a.visible || len(a.matches) == 0 {
		return SlashCommand{}, false
	}
	return a.matches[a.selected], true
}

func (a AutocompleteModel) Matches() []SlashCommand {
	if len(a.matches) == 0 {
		return nil
	}
	out := make([]SlashCommand, len(a.matches))
	copy(out, a.matches)
	return out
}

func (a AutocompleteModel) SelectedIndex() int {
	return a.selected
}

func (a *AutocompleteModel) Accept() string {
	if len(a.matches) == 0 {
		return ""
	}
	name := a.matches[a.selected].Name
	a.visible = false
	a.matches = nil
	a.query = ""
	return name
}

func (a *AutocompleteModel) Dismiss() {
	a.visible = false
	a.selected = 0
}

func (a *AutocompleteModel) Next() {
	if len(a.matches) == 0 {
		return
	}
	a.selected = (a.selected + 1) % len(a.matches)
}

func (a *AutocompleteModel) Prev() {
	if len(a.matches) == 0 {
		return
	}
	a.selected = (a.selected - 1 + len(a.matches)) % len(a.matches)
}
