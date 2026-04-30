package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ComposerPopupOption is one row in the composer's autocomplete /
// command popup — a primary label and optional secondary meta string
// (e.g. "/recover    open recovery view").
type ComposerPopupOption struct {
	Label string
	Meta  string
}

// RenderComposerPopup renders the autocomplete popup with a
// rounded-border surround, one row per option, and a muted footer
// hint. The selected row uses an accent background. Returns "" for
// empty input. accent is the color for the selected row.
func RenderComposerPopup(options []ComposerPopupOption, selectedIdx int, width int, accent string) string {
	if len(options) == 0 {
		return ""
	}

	maxShow := len(options)

	popupStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#111218")).
		Foreground(lipgloss.Color(SlackText)).
		Width(width).
		Padding(0, 1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(SlackBorder))

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(accent)).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackText))

	var lines []string
	for i := 0; i < maxShow; i++ {
		option := options[i]
		entry := fmt.Sprintf("  %-18s %s", option.Label, option.Meta)
		if i == selectedIdx {
			lines = append(lines, selectedStyle.Render(entry))
		} else {
			lines = append(lines, normalStyle.Render(entry))
		}
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted)).Render("  Enter submit • Tab complete • Esc close"))
	return popupStyle.Render(strings.Join(lines, "\n"))
}

// TypingAgentsFromMembers returns display names for members currently
// active — recently posting (ClassifyActivity is "talking" or
// "shipping") or with non-empty LiveActivity. The "you" slug is
// filtered out. Names prefer member.Name and fall back to
// DisplayName(slug).
func TypingAgentsFromMembers(members []Member) []string {
	var typing []string
	for _, m := range members {
		if m.Slug == "you" {
			continue
		}
		act := ClassifyActivity(m)
		if act.Label == "talking" || act.Label == "shipping" {
			if m.Name != "" {
				typing = append(typing, m.Name)
			} else {
				typing = append(typing, DisplayName(m.Slug))
			}
		} else if m.LiveActivity != "" {
			name := m.Name
			if name == "" {
				name = DisplayName(m.Slug)
			}
			typing = append(typing, name)
		}
	}
	return typing
}
