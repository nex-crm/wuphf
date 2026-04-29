package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func visibleSidebarApps(apps []officeSidebarApp, activeApp officeApp, maxRows int) []officeSidebarApp {
	if maxRows <= 0 || len(apps) == 0 {
		return nil
	}
	if len(apps) <= maxRows {
		return apps
	}
	visible := append([]officeSidebarApp(nil), apps[:maxRows]...)
	for _, app := range visible {
		if app.App == activeApp {
			return visible
		}
	}
	for _, app := range apps {
		if app.App == activeApp {
			visible[len(visible)-1] = app
			return visible
		}
	}
	return visible
}

// renderSidebar renders the Slack-style sidebar with channels and team members.
func renderSidebar(channels []channelInfo, members []channelMember, tasks []channelTask, activeChannel string, activeApp officeApp, cursor int, rosterOffset int, focused bool, quickJump quickJumpTarget, workspace workspaceUIState, width, height int, checklist ...onboardingChecklist) string {
	if width < 2 {
		return ""
	}

	bg := lipgloss.Color(sidebarBG)
	innerW := width - 2 // 1 char padding each side

	sectionBandStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D4D4D8")).
		Background(lipgloss.Color("#20242A")).
		Bold(true).
		Padding(0, 1)
	workspaceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)
	workspaceMetaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted))
	workspaceSummaryStyle := workspaceMetaStyle
	workspaceHintStyle := workspaceMetaStyle
	activeRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(sidebarActive)).
		Bold(true).
		Padding(0, 1)
	cursorRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB")).
		Background(lipgloss.Color("#253041")).
		Padding(0, 1)
	channelRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted)).
		Padding(0, 1)
	memberMetaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted))

	switch {
	case !workspace.BrokerConnected:
		workspaceSummaryStyle = workspaceSummaryStyle.Foreground(lipgloss.Color("#F59E0B"))
		workspaceHintStyle = workspaceHintStyle.Foreground(lipgloss.Color("#FBBF24"))
	case workspace.BlockingCount > 0:
		workspaceSummaryStyle = workspaceSummaryStyle.Foreground(lipgloss.Color("#FBBF24"))
		workspaceHintStyle = workspaceHintStyle.Foreground(lipgloss.Color("#FCD34D")).Bold(true)
	case strings.TrimSpace(workspace.AwaySummary) != "":
		workspaceSummaryStyle = workspaceSummaryStyle.Foreground(lipgloss.Color("#93C5FD"))
		workspaceHintStyle = workspaceHintStyle.Foreground(lipgloss.Color("#BFDBFE"))
	default:
		workspaceHintStyle = workspaceHintStyle.Foreground(lipgloss.Color("#D1FAE5"))
	}

	summaryLine := truncateLabel(workspace.sidebarSummaryLine(activeApp), maxInt(8, innerW-1))
	hintLine := truncateLabel(workspace.sidebarHintLine(), maxInt(8, innerW-1))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, sidebarPlainRow(workspaceStyle.Render("WUPHF"), width))
	lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Render("The WUPHF Office"), width))
	lines = append(lines, sidebarPlainRow(workspaceSummaryStyle.Render(summaryLine), width))
	lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Render("Ctrl+G channels · Ctrl+O apps · d DM agent"), width))
	lines = append(lines, sidebarPlainRow(workspaceHintStyle.Render(hintLine), width))
	lines = append(lines, "")
	channelHeaderText := "Channels"
	if quickJump == quickJumpChannels {
		channelHeaderText = "Channels · 1-9"
	}
	lines = append(lines, sidebarStyledRow(sectionBandStyle, channelHeaderText, width))
	if len(channels) == 0 {
		channels = []channelInfo{{Slug: "general", Name: "general"}}
	}
	sidebarIndex := 0
	for _, ch := range channels {
		label := "# " + ch.Slug
		shortcut := sidebarShortcutLabel(sidebarIndex)
		if shortcut != "" {
			label = shortcut + "  " + label
		}
		switch {
		case ch.Slug == activeChannel:
			lines = append(lines, sidebarStyledRow(activeRowStyle, label, width))
		case focused && cursor == sidebarIndex:
			lines = append(lines, sidebarStyledRow(cursorRowStyle, label, width))
		default:
			lines = append(lines, sidebarStyledRow(channelRowStyle, label, width))
		}
		sidebarIndex++
	}

	lines = append(lines, "")
	appHeaderText := "Apps"
	if quickJump == quickJumpApps {
		appHeaderText = "Apps · 1-9"
	}
	lines = append(lines, sidebarStyledRow(sectionBandStyle, appHeaderText, width))
	apps := officeSidebarApps()
	const minRosterReserve = 3
	maxAppRows := height - len(lines) - minRosterReserve
	if maxAppRows < 1 {
		maxAppRows = 1
	}
	for _, app := range visibleSidebarApps(apps, activeApp, maxAppRows) {
		label := appIcon(app.App) + " " + app.Label
		appIndex := 0
		for idx, candidate := range apps {
			if candidate.App == app.App {
				appIndex = idx
				break
			}
		}
		shortcut := sidebarShortcutLabel(appIndex)
		if shortcut != "" {
			label = shortcut + "  " + label
		}
		switch {
		case activeApp == app.App:
			lines = append(lines, sidebarStyledRow(activeRowStyle, label, width))
		case focused && cursor == sidebarIndex:
			lines = append(lines, sidebarStyledRow(cursorRowStyle, label, width))
		default:
			lines = append(lines, sidebarStyledRow(channelRowStyle, label, width))
		}
		sidebarIndex++
	}

	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sidebarDivider))
	divider := dividerStyle.Render(strings.Repeat("\u2500", innerW))
	lines = append(lines, sidebarPlainRow(divider, width))

	// Insert onboarding checklist section above the agents list, if provided and active.
	if len(checklist) > 0 {
		if cl := checklist[0]; !cl.Dismissed {
			clSection := renderOnboardingChecklist(cl, width)
			if clSection != "" {
				lines = append(lines, strings.Split(clSection, "\n")...)
			}
		}
	}

	usedLines := len(lines)
	availableLines := height - usedLines - 1
	if availableLines < 0 {
		availableLines = 0
	}
	compact := availableLines < 14
	maxMembers := availableLines / 4
	if compact {
		maxMembers = availableLines // 1 line per member in compact mode
	}
	if maxMembers < 1 {
		maxMembers = 1
	}

	fallbackRoster := len(members) == 0
	if fallbackRoster {
		members = defaultSidebarRoster()
	}

	totalMembers := len(members)
	start := rosterOffset
	if start < 0 {
		start = 0
	}
	if totalMembers <= maxMembers {
		start = 0
	}
	maxStart := totalMembers - maxMembers
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	end := start + maxMembers
	if end > totalMembers {
		end = totalMembers
	}
	peopleHeader := "Agents"
	if fallbackRoster {
		peopleHeader = "Agents · office roster"
	} else if totalMembers > 0 && end > start {
		peopleHeader = fmt.Sprintf("Agents · %d-%d/%d", start+1, end, totalMembers)
	}
	lines = append(lines, sidebarStyledRow(sectionBandStyle, peopleHeader, width))

	now := time.Now()
	for i := start; i < end; i++ {
		m := members[i]
		summary := deriveMemberRuntimeSummary(m, tasks, now)
		act := summary.Activity
		character := renderOfficeCharacter(m, act, now)
		if summary.Bubble != "" {
			character.Bubble = summary.Bubble
		}

		dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(act.Color))
		dot := dotStyle.Render(act.Dot)

		agentColor := sidebarAgentColors[m.Slug]
		if agentColor == "" {
			agentColor = "#64748B"
		}
		name := m.Name
		if name == "" {
			name = displayName(m.Slug)
		}
		sidebarLabel := act.Label
		nameMax := innerW - 8 - ansi.StringWidth(sidebarLabel)
		if nameMax < 8 {
			nameMax = 8
		}
		name = truncateLabel(name, nameMax)
		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(agentColor)).
			Bold(true)
		nameRendered := nameStyle.Render(name)
		accent := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Render("▎")
		leftPart := accent + " " + dot + " " + nameRendered
		if compact {
			// Compact: single line per member with a simple glyph.
			meta := memberMetaStyle.Render(sidebarLabel)
			mini := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Render(agentAvatar(m.Slug))
			line := leftPart + " " + mini
			pad := innerW - ansi.StringWidth(line) - ansi.StringWidth(sidebarLabel)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, sidebarPlainRow(line+strings.Repeat(" ", pad)+meta, width))
		} else {
			// Full mode: two dense rows per member, using the second row for real detail.
			const avatarW = 4
			avatarTop := ""
			avatarBottom := ""
			if len(character.Avatar) > 0 {
				avatarTop = character.Avatar[0]
			}
			if len(character.Avatar) > 1 {
				avatarBottom = character.Avatar[1]
			}
			if ansi.StringWidth(avatarTop) < avatarW {
				avatarTop += strings.Repeat(" ", avatarW-ansi.StringWidth(avatarTop))
			}
			if ansi.StringWidth(avatarBottom) < avatarW {
				avatarBottom += strings.Repeat(" ", avatarW-ansi.StringWidth(avatarBottom))
			}

			linePrefix := avatarTop + " " + leftPart
			pad := innerW - ansi.StringWidth(linePrefix) - ansi.StringWidth(sidebarLabel)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, sidebarPlainRow(linePrefix+strings.Repeat(" ", pad)+memberMetaStyle.Render(sidebarLabel), width))
			detail := strings.TrimSpace(summary.Detail)
			if detail == "" {
				detail = "No updates yet."
			}
			detail = truncateLabel(detail, maxInt(12, innerW-avatarW-2))
			secondLine := avatarBottom
			if secondLine == "" {
				secondLine = strings.Repeat(" ", avatarW)
			}
			secondLine = secondLine + " " + memberMetaStyle.Render(detail)
			lines = append(lines, sidebarPlainRow(secondLine, width))
			if character.Bubble != "" {
				for _, bubbleLine := range renderThoughtBubble(character.Bubble, innerW-2) {
					lines = append(lines, sidebarPlainRow(bubbleLine, width))
				}
			}
		}
	}

	if totalMembers > maxMembers {
		hint := memberMetaStyle.Render("PgUp/PgDn scroll agents")
		lines = append(lines, sidebarPlainRow(hint, width))
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
	panel := lipgloss.NewStyle().Background(bg)

	var rendered []string
	for _, l := range lines {
		visibleWidth := ansi.StringWidth(l)
		if visibleWidth < width {
			l += strings.Repeat(" ", width-visibleWidth)
		}
		rendered = append(rendered, panel.Render(l))
	}

	return strings.Join(rendered, "\n")
}
