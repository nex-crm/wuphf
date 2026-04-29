package channelui

import "github.com/charmbracelet/lipgloss"

// ── Slack dark-theme palette ────────────────────────────────────────
const (
	SlackSidebarBg   = "#19171D"
	SlackMainBg      = "#1F1D24"
	SlackThreadBg    = "#18171D"
	SlackBorder      = "#2A2830"
	SlackActive      = "#1264A3"
	SlackHover       = "#2B2931"
	SlackText        = "#E8E8EA"
	SlackMuted       = "#A6A6AC"
	SlackTimestamp   = "#616164"
	SlackDivider     = "#34313B"
	SlackMentionBg   = "#E8912D"
	SlackMentionText = "#F2C744"
	SlackOnline      = "#2BAC76"
	SlackAway        = "#E8912D"
	SlackBusy        = "#8B5CF6"
	SlackInputBorder = "#565856"
	SlackInputFocus  = "#1264A3"
)

// AgentColorMap maps agent slugs to their brand colors.
var AgentColorMap = map[string]string{
	"all":      "#FFFFFF",
	"ceo":      "#EAB308",
	"pm":       "#22C55E",
	"fe":       "#3B82F6",
	"be":       "#8B5CF6",
	"ai":       "#14B8A6",
	"designer": "#EC4899",
	"cmo":      "#F97316",
	"cro":      "#06B6D4",
	"nex":      "#7C3AED",
	"you":      "#38BDF8",
	"human":    "#38BDF8",
}

// StatusDotColors maps activity states to dot colors.
var StatusDotColors = map[string]string{
	"talking":  SlackOnline,
	"thinking": SlackAway,
	"coding":   SlackBusy,
	"idle":     SlackMuted,
}

// ── Style constructors ──────────────────────────────────────────────

func SidebarStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(SlackSidebarBg)).
		Foreground(lipgloss.Color(SlackText)).
		Padding(1, 1)
}

func MainPanelStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Height(height).
		Background(lipgloss.Color(SlackMainBg)).
		Foreground(lipgloss.Color(SlackText))
}

func ThreadPanelStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(SlackThreadBg)).
		Foreground(lipgloss.Color(SlackText)).
		Padding(1, 1)
}

func StatusBarStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color(SlackSidebarBg)).
		Foreground(lipgloss.Color(SlackMuted)).
		Padding(0, 1)
}

func ChannelHeaderStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color(SlackMainBg)).
		Foreground(lipgloss.Color(SlackText)).
		Bold(true).
		Padding(0, 2, 1, 2).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(SlackBorder))
}

func ComposerBorderStyle(width int, focused bool) lipgloss.Style {
	borderColor := SlackInputBorder
	if focused {
		borderColor = SlackInputFocus
	}
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width+4). // account for border + padding
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Background(lipgloss.Color("#17161C")).
		Padding(0, 1)
}

func TimestampStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackTimestamp))
}

func MutedTextStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackMuted))
}

func AgentNameStyle(slug string) lipgloss.Style {
	color := AgentColorMap[slug]
	if color == "" {
		color = SlackMuted
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
}

func ActiveChannelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color(SlackActive)).
		PaddingLeft(1)
}

func DateSeparatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackDivider)).
		Bold(true)
}

func ThreadIndicatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackActive)).
		Bold(true).
		Underline(true)
}

func AgentAvatar(slug string) string {
	switch slug {
	case "ceo":
		return "◆"
	case "pm":
		return "▣"
	case "fe":
		return "▤"
	case "be":
		return "▥"
	case "ai":
		return "◉"
	case "designer":
		return "◌"
	case "cmo":
		return "✶"
	case "cro":
		return "◈"
	case "nex":
		return "◎"
	case "you":
		return "●"
	default:
		return "•"
	}
}

func MascotAccent(slug string) string {
	switch slug {
	case "ceo":
		return "⌐"
	case "pm":
		return "□"
	case "fe":
		return "="
	case "be":
		return "#"
	case "ai":
		return "*"
	case "designer":
		return "~"
	case "cmo":
		return "!"
	case "cro":
		return "$"
	case "nex":
		return "◎"
	case "you":
		return "+"
	default:
		return "•"
	}
}

func MascotEyes(slug string) (string, string) {
	switch slug {
	case "ceo":
		return "■", "■"
	case "ai", "nex":
		return "◉", "◉"
	case "designer":
		return "◕", "◕"
	case "cmo":
		return "✶", "✶"
	default:
		return "•", "•"
	}
}

func MascotMouth(activity string, frame int) string {
	switch activity {
	case "talking":
		if frame%2 == 0 {
			return "o"
		}
		return "ᴗ"
	case "shipping":
		if frame%2 == 0 {
			return "⌣"
		}
		return "▿"
	case "plotting":
		if frame%2 == 0 {
			return "~"
		}
		return "ˎ"
	default:
		if frame%2 == 0 {
			return "‿"
		}
		return "_"
	}
}

func MascotTop(activity string, frame int) string {
	switch activity {
	case "talking":
		if frame%2 == 0 {
			return " /^^\\\\"
		}
		return " /~~\\\\"
	case "plotting":
		if frame%2 == 0 {
			return " /~~\\\\"
		}
		return " /^^\\\\"
	default:
		return " /^^\\\\"
	}
}

func MascotProp(slug string) string {
	switch slug {
	case "ceo":
		return "$"
	case "pm":
		return "]"
	case "fe":
		return "="
	case "be":
		return "#"
	case "ai":
		return "*"
	case "designer":
		return "~"
	case "cmo":
		return "!"
	case "cro":
		return "%"
	case "nex":
		return "o"
	case "you":
		return "+"
	default:
		return "|"
	}
}

func MascotLines(slug, activity string, frame int) [3]string {
	leftEye, rightEye := MascotEyes(slug)
	face := "(" + leftEye + MascotMouth(activity, frame) + rightEye + ")"
	body := " /|" + MascotProp(slug) + "\\ "
	return [3]string{
		MascotTop(activity, frame),
		face,
		body,
	}
}

// AgentCharacter returns a compact mascot-like face for inline use.
// WUPHF uses one coherent visual grammar: a role accent + a rounded face.
func AgentCharacter(slug, activity string, frame int) string {
	leftEye, rightEye := MascotEyes(slug)
	return MascotAccent(slug) + "(" + leftEye + MascotMouth(activity, frame) + rightEye + ")"
}

func AccentPill(label, color string) string {
	if label == "" {
		return ""
	}
	if color == "" {
		color = SlackActive
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(color)).
		Padding(0, 1).
		Bold(true).
		Render(label)
}

func SubtlePill(label, fg, bg string) string {
	if label == "" {
		return ""
	}
	if fg == "" {
		fg = SlackText
	}
	if bg == "" {
		bg = SlackHover
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Padding(0, 1).
		Render(label)
}

func TaskStatusPill(status string) string {
	switch status {
	case "in_progress":
		return AccentPill("moving", "#D97706")
	case "review":
		return AccentPill("review", "#2563EB")
	case "blocked":
		return AccentPill("blocked", "#B91C1C")
	case "done":
		return AccentPill("done", "#15803D")
	default:
		return SubtlePill("open", "#CBD5E1", "#334155")
	}
}

func RequestKindPill(kind string) string {
	switch kind {
	case "approval":
		return AccentPill("approval", "#B45309")
	case "confirm":
		return AccentPill("confirm", "#1D4ED8")
	case "secret":
		return AccentPill("private", "#7C3AED")
	case "freeform":
		return SubtlePill("open question", "#E5E7EB", "#374151")
	case "interview":
		return SubtlePill("interview", "#F8FAFC", "#4B5563")
	default:
		return SubtlePill(kind, "#E5E7EB", "#374151")
	}
}
