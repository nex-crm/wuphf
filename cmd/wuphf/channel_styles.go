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
		return "☕"
	case "pm":
		return "📋"
	case "fe":
		return "🖥"
	case "be":
		return "🛠"
	case "ai":
		return "🤖"
	case "designer":
		return "✏️"
	case "cmo":
		return "📣"
	case "cro":
		return "💼"
	case "nex":
		return "🛰"
	case "you":
		return "🙂"
	default:
		return "•"
	}
}

// agentCharacter returns an animated character face for the sidebar.
// Each persona has a distinct visual identity using Unicode box/bracket
// characters as "frames" — the accessory/bracket style encodes the role,
// and the internal expression changes with activity state.
//
// Two frames per state create subtle aliveness (alternating each second).
//
// Design language:
//   CEO:      [⌐■_■]  sunglasses founder — cool, decisive
//   PM:       [°□°]   wide-eyed organizer — seeing everything
//   FE:      <°_°>    angle brackets — code-brained
//   BE:      {¬_¬}    curly braces — skeptical systems thinker
//   AI:      «○_○»    guillemets — data/model vibes
//   Designer: ~°‿°~   tildes — creative, flowing
//   CMO:      ♪°_°    music note — storyteller energy
//   CRO:      $°_°    dollar — revenue-focused
func agentCharacter(slug, activity string, frame int) string {
	f := frame % 2
	switch slug {
	case "ceo":
		switch activity {
		case "talking":
			return pick(f, "[⌐■ᗜ■]", "[⌐■ᗜ■]ᐊ")
		case "shipping":
			return pick(f, "[⌐■▿■]", "[⌐■▿■]▸")
		case "plotting":
			return pick(f, "[⌐■_■]…", "[⌐■‸■] ")
		default:
			return pick(f, "[⌐■_■]", "[⌐■_■] ")
		}
	case "pm":
		switch activity {
		case "talking":
			return pick(f, "[°ᗜ°]▐", "[°ᗜ°]▐!")
		case "shipping":
			return pick(f, "[°▿°]▐", "[°▿°]▸")
		case "plotting":
			return pick(f, "[°‸°]▐", "[°_°]▐…")
		default:
			return pick(f, "[°□°]▐", "[°□°]▐")
		}
	case "fe":
		switch activity {
		case "talking":
			return pick(f, "<°ᗜ°>", "<°ᗜ°>ᐊ")
		case "shipping":
			return pick(f, "<°▿°>█", "<°▿°>▊")
		case "plotting":
			return pick(f, "<°‸°>", "<°_°>…")
		default:
			return pick(f, "<°_°>", "<°_°> ")
		}
	case "be":
		switch activity {
		case "talking":
			return pick(f, "{¬ᗜ¬}", "{¬ᗜ¬}ᐊ")
		case "shipping":
			return pick(f, "{¬▿¬}█", "{¬▿¬}▊")
		case "plotting":
			return pick(f, "{¬‸¬}", "{¬_¬}…")
		default:
			return pick(f, "{¬_¬}", "{¬_¬} ")
		}
	case "ai":
		switch activity {
		case "talking":
			return pick(f, "«○ᗜ○»", "«○ᗜ○»ᐊ")
		case "shipping":
			return pick(f, "«●_●»", "«○_○»")
		case "plotting":
			return pick(f, "«○‸○»", "«○_○»…")
		default:
			return pick(f, "«○_○»", "«○_○» ")
		}
	case "designer":
		switch activity {
		case "talking":
			return pick(f, "~°ᗜ°~", "~°ᗜ°~ᐊ")
		case "shipping":
			return pick(f, "~°▿°~✎", "~°▿°~✎")
		case "plotting":
			return pick(f, "~°‸°~", "~°_°~…")
		default:
			return pick(f, "~°‿°~", "~°‿°~ ")
		}
	case "cmo":
		switch activity {
		case "talking":
			return pick(f, "♪°ᗜ°", "♫°ᗜ°ᐊ")
		case "shipping":
			return pick(f, "♪°▿°►", "♫°▿°▸")
		case "plotting":
			return pick(f, "♪°‸°", "♫°_°…")
		default:
			return pick(f, "♪°ᴗ°", "♫°ᴗ° ")
		}
	case "cro":
		switch activity {
		case "talking":
			return pick(f, "$°ᗜ°", "$°ᗜ°ᐊ")
		case "shipping":
			return pick(f, "$°▿°▸", "$°▿°►")
		case "plotting":
			return pick(f, "$°‸°", "$°_°…")
		default:
			return pick(f, "$°_°", "$°_° ")
		}
	default:
		return pick(f, "(°_°)", "(°_°) ")
	}
}

func pick(frame int, a, b string) string {
	if frame == 0 {
		return a
	}
	return b
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
