package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func truncateLabel(label string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(label)
	if len(r) <= max {
		return label
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// Sidebar theme colors.
const (
	sidebarBG      = "#1A1D21"
	sidebarMuted   = "#ABABAD"
	sidebarDivider = "#35373B"
	sidebarActive  = "#1264A3"

	dotTalking  = "#2BAC76"
	dotThinking = "#E8912D"
	dotCoding   = "#8B5CF6"
	dotIdle     = "#ABABAD"
)

// sidebarAgentColors maps slugs to their display colors.
var sidebarAgentColors = map[string]string{
	"ceo": "#EAB308", "pm": "#22C55E", "fe": "#3B82F6",
	"be": "#8B5CF6", "ai": "#14B8A6", "designer": "#EC4899",
	"cmo": "#F97316", "cro": "#06B6D4", "you": "#FFFFFF",
}

// sidebarName returns a short name suitable for the narrow sidebar column.
func sidebarName(slug string) string {
	return displayName(slug)
}

// memberActivity describes what an agent is doing based on recency and content.
type memberActivity struct {
	Label string
	Color string
	Dot   string
}

// classifyActivity determines activity from last message time and content.
func classifyActivity(m channelMember) memberActivity {
	now := time.Now()
	elapsed := now.Sub(now) // default: max duration (idle)

	if m.LastTime != "" {
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02T15:04:05.000Z",
			"2006-01-02T15:04:05Z",
		} {
			if t, err := time.Parse(layout, m.LastTime); err == nil {
				elapsed = now.Sub(t)
				break
			}
		}
	}

	// Check for tool-use keywords indicating "coding".
	if elapsed < 30*time.Second && m.LastMessage != "" {
		lower := strings.ToLower(m.LastMessage)
		for _, kw := range []string{"bash", "edit", "read", "write", "grep", "glob"} {
			if strings.Contains(lower, kw) {
				return memberActivity{Label: "coding", Color: dotCoding, Dot: "\u26A1"}
			}
		}
	}

	if m.LastTime == "" {
		return memberActivity{Label: "idle", Color: dotIdle, Dot: "\u25CB"}
	}

	switch {
	case elapsed < 10*time.Second:
		return memberActivity{Label: "talking", Color: dotTalking, Dot: "\U0001F7E2"}
	case elapsed < 30*time.Second:
		return memberActivity{Label: "thinking", Color: dotThinking, Dot: "\U0001F7E1"}
	default:
		return memberActivity{Label: "idle", Color: dotIdle, Dot: "\u25CB"}
	}
}

// renderSidebar renders the Slack-style sidebar with channels and team members.
func renderSidebar(members []channelMember, activeChannel string, width, height int) string {
	if width < 2 {
		return ""
	}

	bg := lipgloss.Color(sidebarBG)
	innerW := width - 2 // 1 char padding each side

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted)).
		Bold(true).
		PaddingLeft(1)
	workspaceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)
	workspaceMetaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted))
	activeRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(sidebarActive)).
		Bold(true).
		Padding(0, 1)
	channelRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted)).
		Padding(0, 1)
	memberMetaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, " "+workspaceStyle.Render("WUPHF"))
	lines = append(lines, " "+workspaceMetaStyle.Render("The WUPHF Office"))
	lines = append(lines, "")
	lines = append(lines, " "+headerStyle.Render("Channel"))

	activeName := "# general"
	if activeChannel == "" || activeChannel == "general" {
		lines = append(lines, " "+activeRowStyle.Width(innerW-1).Render(activeName))
	} else {
		lines = append(lines, " "+channelRowStyle.Render(activeName))
	}

	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sidebarDivider))
	divider := dividerStyle.Render(strings.Repeat("\u2500", innerW))
	lines = append(lines, " "+divider)

	lines = append(lines, " "+headerStyle.Render("People"))

	usedLines := len(lines)
	maxMembers := height - usedLines - 1
	if maxMembers < 0 {
		maxMembers = 0
	}

	visibleCount := len(members)
	overflow := 0
	if visibleCount > maxMembers {
		overflow = visibleCount - maxMembers
		visibleCount = maxMembers
	}

	for i := 0; i < visibleCount; i++ {
		m := members[i]
		act := classifyActivity(m)

		dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(act.Color))
		dot := dotStyle.Render(act.Dot)

		agentColor := sidebarAgentColors[m.Slug]
		if agentColor == "" {
			agentColor = "#64748B"
		}
		name := truncateLabel(sidebarName(m.Slug), innerW-8)
		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(agentColor)).
			Bold(true)
		nameRendered := nameStyle.Render(name)
		role := truncateLabel(roleLabel(m.Slug), innerW-8)
		roleRendered := memberMetaStyle.Render(role)
		leftPart := dot + " " + nameRendered
		pad := innerW - ansi.StringWidth(leftPart) - ansi.StringWidth(act.Label)
		if pad < 1 {
			pad = 1
		}
		lines = append(lines, " "+leftPart+strings.Repeat(" ", pad)+memberMetaStyle.Render(act.Label))
		lines = append(lines, "   "+roleRendered)
	}

	if overflow > 0 {
		more := memberMetaStyle.Render(fmt.Sprintf("\u22EF +%d more", overflow))
		lines = append(lines, " "+more)
	}

	// Pad remaining height with empty lines.
	for len(lines) < height {
		lines = append(lines, "")
	}

	// Truncate if somehow over height.
	if len(lines) > height {
		lines = lines[:height]
	}

	// Apply sidebar background to each line, padded to full width.
	panel := lipgloss.NewStyle().
		Width(width).
		Background(bg)

	var rendered []string
	for _, l := range lines {
		rendered = append(rendered, panel.Render(l))
	}

	return strings.Join(rendered, "\n")
}
