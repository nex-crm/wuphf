package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type StatusBarModel struct {
	Mode        string
	Breadcrumbs []string
	TokensUsed  int
	CostUsd     float64
	Elapsed     time.Duration
	Hint        string
	Width       int
}

func NewStatusBar() StatusBarModel {
	return StatusBarModel{Mode: "NORMAL"}
}

func (s StatusBarModel) View() string {
	var modeBadge lipgloss.Style
	if s.Mode == "INSERT" {
		modeBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#22C55E")).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(0, 1)
	} else {
		modeBadge = lipgloss.NewStyle().
			Background(lipgloss.Color(NexBlue)).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			Padding(0, 1)
	}

	mode := modeBadge.Render(s.Mode)

	crumbs := strings.Join(s.Breadcrumbs, " > ")

	left := mode
	if crumbs != "" {
		left += " " + SystemStyle.Render(crumbs)
	}

	right := SystemStyle.Render(fmt.Sprintf(
		"tokens: %d | $%.2f | %.1fs",
		s.TokensUsed,
		s.CostUsd,
		s.Elapsed.Seconds(),
	))

	hint := ""
	if s.Hint != "" {
		hint = "  " + SystemStyle.Render(s.Hint)
	}

	// Calculate fill width
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right) + lipgloss.Width(hint)
	fill := s.Width - leftLen - rightLen - 2
	if fill < 1 {
		fill = 1
	}

	bar := left + strings.Repeat(" ", fill) + right + hint

	return StatusBarStyle.Width(s.Width).Render(bar)
}
