package tui

import "github.com/charmbracelet/lipgloss"

// Brand colors
const (
	NexBlue   = "#2980fb"
	NexPurple = "#cf72d9"
	NexGreen  = "#97a022"
)

// Status colors
const (
	Success = "#03a04c"
	Warning = "#df750c"
	Error   = "#e23428"
	Info    = "#4d97ff"
)

// Text colors
const (
	ValueColor = "#cfd0d2"
	LabelColor = "#999a9b"
	MutedColor = "#838485"
)

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(NexPurple)).
			Padding(0, 1)

	UserStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60A5FA")).
			Bold(true)

	AgentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399")).
			Bold(true)

	SystemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(MutedColor))

	SidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")).
			Padding(1, 2).
			Width(26)

	InputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(NexPurple)).
				Padding(0, 1)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Error))

	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Success))

	StatusBarStyle = lipgloss.NewStyle().
			Reverse(true)
)
