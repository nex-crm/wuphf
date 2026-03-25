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
		Foreground(lipgloss.Color(slackActive))
}
