package main

import "github.com/charmbracelet/lipgloss"

// ── Slack dark-theme palette ────────────────────────────────────────
const (
	slackSidebarBg   = "#19171D"
	slackMainBg      = "#1F1D24"
	slackThreadBg    = "#18171D"
	slackBorder      = "#2A2830"
	slackActive      = "#1264A3"
	slackHover       = "#2B2931"
	slackText        = "#E8E8EA"
	slackMuted       = "#A6A6AC"
	slackTimestamp   = "#616164"
	slackDivider     = "#34313B"
	slackMentionBg   = "#E8912D"
	slackMentionText = "#F2C744"
	slackOnline      = "#2BAC76"
	slackAway        = "#E8912D"
	slackBusy        = "#8B5CF6"
	slackInputBorder = "#565856"
	slackInputFocus  = "#1264A3"
)

// agentColorMap maps agent slugs to their brand colors.
var agentColorMap = map[string]string{
	"ceo":      "#EAB308",
	"pm":       "#22C55E",
	"fe":       "#3B82F6",
	"be":       "#8B5CF6",
	"ai":       "#14B8A6",
	"designer": "#EC4899",
	"cmo":      "#F97316",
	"cro":      "#06B6D4",
	"nex":      "#7C3AED",
	"you":      "#FFFFFF",
}

// statusDotColors maps activity states to dot colors.
var statusDotColors = map[string]string{
	"talking":  slackOnline,
	"thinking": slackAway,
	"coding":   slackBusy,
	"idle":     slackMuted,
}

// ── Style constructors ──────────────────────────────────────────────

func sidebarStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(slackSidebarBg)).
		Foreground(lipgloss.Color(slackText)).
		Padding(1, 1)
}

func mainPanelStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(slackMainBg)).
		Foreground(lipgloss.Color(slackText))
}

func threadPanelStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(slackThreadBg)).
		Foreground(lipgloss.Color(slackText)).
		Padding(1, 1)
}

func statusBarStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color(slackSidebarBg)).
		Foreground(lipgloss.Color(slackMuted)).
		Padding(0, 1)
}

func channelHeaderStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color(slackMainBg)).
		Foreground(lipgloss.Color(slackText)).
		Bold(true).
		Padding(0, 2, 1, 2).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(slackBorder))
}

func composerBorderStyle(width int, focused bool) lipgloss.Style {
	borderColor := slackInputBorder
	if focused {
		borderColor = slackInputFocus
	}
	return lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Background(lipgloss.Color("#17161C")).
		Padding(0, 1)
}

func timestampStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackTimestamp))
}

func mutedTextStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackMuted))
}

func agentNameStyle(slug string) lipgloss.Style {
	color := agentColorMap[slug]
	if color == "" {
		color = slackMuted
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
}

func activeChannelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color(slackActive)).
		PaddingLeft(1)
}

func dateSeparatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackDivider)).
		Bold(true)
}

func threadIndicatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackActive)).
		Bold(true).
		Underline(true)
}

func agentAvatar(slug string) string {
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

func mascotAccent(slug string) string {
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

func mascotEyes(slug string) (string, string) {
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

func mascotMouth(activity string, frame int) string {
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

func mascotTop(activity string, frame int) string {
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

func mascotProp(slug string) string {
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

func mascotLines(slug, activity string, frame int) [3]string {
	leftEye, rightEye := mascotEyes(slug)
	face := "(" + leftEye + mascotMouth(activity, frame) + rightEye + ")"
	body := " /|" + mascotProp(slug) + "\\ "
	return [3]string{
		mascotTop(activity, frame),
		face,
		body,
	}
}

// agentCharacter returns a compact mascot-like face for inline use.
// WUPHF uses one coherent visual grammar: a role accent + a rounded face.
func agentCharacter(slug, activity string, frame int) string {
	leftEye, rightEye := mascotEyes(slug)
	return mascotAccent(slug) + "(" + leftEye + mascotMouth(activity, frame) + rightEye + ")"
}

func appIcon(app officeApp) string {
	switch app {
	case officeAppTasks:
		return "☑"
	case officeAppRequests:
		return "?"
	case officeAppInsights:
		return "✦"
	case officeAppCalendar:
		return "◷"
	case officeAppMessages:
		return "•"
	default:
		return "#"
	}
}

func accentPill(label, color string) string {
	if label == "" {
		return ""
	}
	if color == "" {
		color = slackActive
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(color)).
		Padding(0, 1).
		Bold(true).
		Render(label)
}

func subtlePill(label, fg, bg string) string {
	if label == "" {
		return ""
	}
	if fg == "" {
		fg = slackText
	}
	if bg == "" {
		bg = slackHover
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Padding(0, 1).
		Render(label)
}

func taskStatusPill(status string) string {
	switch status {
	case "in_progress":
		return accentPill("moving", "#D97706")
	case "blocked":
		return accentPill("blocked", "#B91C1C")
	case "done":
		return accentPill("done", "#15803D")
	default:
		return subtlePill("open", "#CBD5E1", "#334155")
	}
}

func requestKindPill(kind string) string {
	switch kind {
	case "approval":
		return accentPill("approval", "#B45309")
	case "confirm":
		return accentPill("confirm", "#1D4ED8")
	case "secret":
		return accentPill("private", "#7C3AED")
	case "freeform":
		return subtlePill("open question", "#E5E7EB", "#374151")
	case "interview":
		return subtlePill("interview", "#F8FAFC", "#4B5563")
	default:
		return subtlePill(kind, "#E5E7EB", "#374151")
	}
}
