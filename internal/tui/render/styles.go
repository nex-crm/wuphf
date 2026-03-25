package render

import "github.com/charmbracelet/lipgloss"

// Shared styles for render functions.
var (
	HeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4d97ff"))
	MutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#838485"))
	RowEvenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cfd0d2"))
	RowOddStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#999a9b"))
	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#03a04c"))
	ErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#e23428"))
	WarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#df750c"))
	InfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4d97ff"))
	PurpleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#cf72d9"))
)
