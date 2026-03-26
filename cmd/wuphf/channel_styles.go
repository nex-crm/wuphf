package main

import (
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

// ── Sprite library ────────────────────────────────────────────────
//
// 16 multi-line character sprites. Each sprite is 3 lines tall:
//   line 0: hat/hair (identity — what makes the character recognizable)
//   line 1: face (changes with activity)
//   line 2: body (changes with activity)
//
// Characters are NOT role-specific. Any agent can get any sprite.
// CEO always gets sprite 0 (the sunglasses boss). All others are
// assigned by stable hash of the agent's slug.
//
// Sprites are 7 chars wide (padded). This fits in a 24+ char sidebar
// with room for name/role alongside.

// spriteHat is the top line of a character — the identity marker.
// These never change with activity; they're how you recognize the character.
var spriteHats = [16]string{
	" \u2310\u25a0-\u25a0 ", // 0: sunglasses (CEO)
	" \u256d\u2593\u2593\u256e ", // 1: beanie
	"  \u25b2\u25b2  ",         // 2: mohawk
	" \u256d\u2500\u2500\u256e ", // 3: flat cap
	"  \u265b   ",               // 4: crown
	" \u250c\u2584\u2584\u2510 ", // 5: top hat
	"  \u25d7   ",               // 6: beret
	"  \u257d   ",               // 7: antenna
	" \u25d6  \u25d7 ",          // 8: headphones
	"  \u2227\u2227  ",         // 9: pointy ears
	"  \u2740   ",               // 10: flower
	"  \u2584\u2580  ",          // 11: backwards cap
	" \u256d\u223f\u256e ",      // 12: wavy hair
	"  \u2550\u2550  ",          // 13: visor
	" \u2500\u25cb\u25cb\u2500 ", // 14: round goggles
	"  \u2580\u2580  ",          // 15: flat top
}

// spriteFace returns the face line (line 1) for the given activity + frame.
// The face is shared across all sprites — expression is universal.
func spriteFace(activity string, frame int) string {
	f := frame % 2
	switch activity {
	case "talking":
		return pick(f, "(\u00b0\u1d17\u00b0)", "(\u00b0o\u00b0)")
	case "shipping":
		return pick(f, "(\u00b0\u25bf\u00b0)", "(\u00b0_\u00b0)")
	case "plotting":
		return pick(f, "(\u00b0\u2038\u00b0)", "(\u00b0_\u00b0)")
	default: // idle/lurking
		return pick(f, "(\u00b0_\u00b0)", "(\u00b0_\u00b0)")
	}
}

// spriteBody returns the body line (line 2) for the given activity + frame.
func spriteBody(activity string, frame int) string {
	f := frame % 2
	switch activity {
	case "talking":
		return pick(f, " /|\\>", " \\|/ ")
	case "shipping":
		return pick(f, " /|\u2588", " /|\u258a")
	case "plotting":
		return pick(f, " /|. ", " /|  ")
	default:
		return pick(f, " /|\\ ", " /|\\ ")
	}
}

// assignSpriteIndex returns a stable sprite index for a given agent slug.
// CEO always gets 0 (the sunglasses). Everyone else hashes into 1..15
// (skipping 0 to avoid looking like the CEO).
func assignSpriteIndex(slug string) int {
	if slug == "ceo" {
		return 0
	}
	h := fnv.New32a()
	h.Write([]byte(slug))
	return 1 + int(h.Sum32())%(len(spriteHats)-1) // range 1..15
}

// spriteLines returns the 3-line character sprite for an agent.
func spriteLines(slug, activity string, frame int) [3]string {
	idx := assignSpriteIndex(slug)
	return [3]string{
		spriteHats[idx],
		spriteFace(activity, frame),
		spriteBody(activity, frame),
	}
}

// agentCharacter returns a single-line compact face for inline use.
// Uses the hat as a prefix badge so the character is recognizable in one line.
func agentCharacter(slug, activity string, frame int) string {
	idx := assignSpriteIndex(slug)
	hat := strings.TrimSpace(spriteHats[idx])
	face := spriteFace(activity, frame)
	return hat + face
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
