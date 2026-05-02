package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// View() — the Bubbletea render entrypoint. Composes 3 columns
// (sidebar | main | thread), plus a header row, message area,
// composer, and status bar. The actual presentation primitives
// (channelui.RenderXxx, MainPanelStyle, etc.) live in the channelui
// package; View() is the orchestrator that pulls cached lines, computes
// layout, and joins panels.

func (m channelModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	layout := m.currentLayout()
	workspaceState := m.currentWorkspaceUIState()

	// ── Sidebar ──────────────────────────────────────────────────────
	sidebar := ""
	if layout.ShowSidebar && !m.isOneOnOne() {
		sidebar = cachedSidebarRender(m.channels, channelui.MergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo()), m.tasks, m.activeChannel, m.activeApp, m.sidebarCursor, m.sidebarRosterOffset, m.focus == focusSidebar, m.quickJumpTarget, workspaceState, layout.SidebarW, layout.ContentH, m.onboardingChecklist)
	}

	// ── Thread panel ─────────────────────────────────────────────────
	thread := ""
	if layout.ShowThread && !m.isOneOnOne() {
		threadPopup := ""
		if m.focus == focusThread {
			threadPopup = m.renderActivePopup(channelui.MaxInt(layout.ThreadW-4, 24))
		}
		thread = renderThreadPanel(m.messages, m.threadPanelID,
			layout.ThreadW, layout.ContentH,
			m.threadInput, m.threadInputPos, m.threadScroll,
			threadPopup, m.focus == focusThread, m.threadInputHistory.Len() > 0)
	}

	activePending := m.visiblePendingRequest()
	// ── Main panel: header + messages + composer ─────────────────────
	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}

	// Channel header (2 lines)
	headerStyle := channelui.ChannelHeaderStyle(mainW)
	headerLine1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).
		Render(channelui.AppIcon(m.activeApp) + " " + m.currentHeaderTitle())
	headerMeta := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted)).
		Render(m.currentHeaderMeta())
	if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceLabel := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceLabel = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(channelui.SlackActive)).
			Render(fmt.Sprintf("Session %s · %s  Total %s · %s%s",
				channelui.FormatUSD(m.usage.Session.CostUsd),
				channelui.FormatTokenCount(m.usage.Session.TotalTokens),
				channelui.FormatUSD(m.usage.Total.CostUsd),
				channelui.FormatTokenCount(m.usage.Total.TotalTokens),
				sinceLabel,
			))
	}
	if m.activeApp == channelui.OfficeAppMessages && m.unreadCount > 0 && m.scroll > 0 {
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(channelui.SlackActive)).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("%d new", m.unreadCount))
		if awaySummary := m.currentAwaySummary(); strings.TrimSpace(awaySummary) != "" {
			headerMeta += "  " + lipgloss.NewStyle().
				Foreground(lipgloss.Color("#BFDBFE")).
				Render(awaySummary)
		}
	}
	if m.pending != nil {
		headerMeta += "  " + channelui.AccentPill("request pending", "#B45309")
	} else if len(m.requests) > 0 {
		headerMeta += "  " + channelui.SubtlePill(fmt.Sprintf("%d open requests", len(m.requests)), "#FDE68A", "#78350F")
	}
	channelHeader := headerStyle.Render(headerLine1 + headerMeta)
	if usageLine := channelui.RenderUsageStrip(m.usage, m.members, mainW); usageLine != "" {
		channelHeader += "\n" + usageLine
	}
	headerH := lipgloss.Height(channelHeader)
	runtimeStrip := ""
	if m.activeApp == channelui.OfficeAppMessages || m.isOneOnOne() {
		focusSlug := ""
		if m.isOneOnOne() {
			focusSlug = m.oneOnOneAgentSlug()
		}
		runtimeStrip = channelui.RenderRuntimeStrip(m.members, m.tasks, m.requests, m.actions, mainW-4, focusSlug)
	}
	runtimeH := lipgloss.Height(runtimeStrip)

	// Composer
	typingAgents := channelui.TypingAgentsFromMembers(m.members)
	liveActivities := channelui.LiveActivityFromMembers(m.members)
	composerStr := ""
	if m.activeApp == channelui.OfficeAppMessages || m.memberDraft != nil {
		composerStr = renderComposer(mainW, m.input, m.inputPos, m.composerTargetLabel(),
			m.replyToID, typingAgents, liveActivities, activePending, m.selectedOption, m.composerHint(m.composerTargetLabel(), m.replyToID, activePending),
			m.focus == focusMain, m.tickFrame)
	}
	if m.memberDraft != nil {
		composerStr = renderComposer(mainW, m.input, m.inputPos, memberDraftComposerLabel(*m.memberDraft),
			"", typingAgents, nil, nil, 0, m.composerHint(memberDraftComposerLabel(*m.memberDraft), "", nil), m.focus == focusMain, m.tickFrame)
	}

	// Interview card (above composer)
	interviewCard := ""
	if activePending != nil {
		interviewCard = channelui.RenderInterviewCard(*activePending, m.selectedOption, m.interviewPhaseTitle(), mainW-4)
	}
	memberDraftCard := ""
	if m.memberDraft != nil {
		memberDraftCard = renderMemberDraftCard(*m.memberDraft, mainW-4)
	}
	doctorCard := ""
	if m.doctor != nil {
		doctorCard = channelui.RenderDoctorCard(*m.doctor, mainW-4)
	}
	confirmCard := ""
	if m.confirm != nil {
		confirmCard = channelui.RenderConfirmCard(*m.confirm, mainW-4)
	}

	// Init/picker overlays
	initPanel := ""
	if confirmCard != "" {
		initPanel = confirmCard
	} else if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}

	composerH := lipgloss.Height(composerStr)
	interviewH := lipgloss.Height(interviewCard)
	memberDraftH := lipgloss.Height(memberDraftCard)
	doctorH := lipgloss.Height(doctorCard)
	initH := lipgloss.Height(initPanel)

	// Message area height
	msgH := layout.ContentH - headerH - runtimeH - composerH - interviewH - memberDraftH - doctorH - initH - 1 // 1 for status bar
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	allLines := m.currentMainViewportLines(contentWidth, msgH)
	visibleRows, scroll, _, _ := channelui.SliceRenderedLines(allLines, msgH, m.scroll)
	var visible []string
	for _, row := range visibleRows {
		visible = append(visible, row.Text)
	}
	for len(visible) < msgH {
		visible = append(visible, "")
	}
	if m.activeApp == channelui.OfficeAppMessages && m.unreadCount > 0 && scroll > 0 && len(visible) > 0 {
		visible[0] = channelui.RenderAwayStrip(contentWidth, m.unreadCount, m.currentAwaySummary())
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		visible = channelui.OverlayBottomLines(visible, strings.Split(popup, "\n"))
	}

	msgPanel := channelui.MainPanelStyle(mainW, msgH).Render(strings.Join(visible, "\n"))

	// Assemble main column
	mainParts := []string{channelHeader}
	if runtimeStrip != "" {
		mainParts = append(mainParts, runtimeStrip)
	}
	mainParts = append(mainParts, msgPanel)
	if interviewCard != "" {
		mainParts = append(mainParts, interviewCard)
	}
	if memberDraftCard != "" {
		mainParts = append(mainParts, memberDraftCard)
	}
	if doctorCard != "" {
		mainParts = append(mainParts, doctorCard)
	}
	if initPanel != "" {
		mainParts = append(mainParts, initPanel)
	}
	if m.activeApp == channelui.OfficeAppMessages || m.memberDraft != nil {
		mainParts = append(mainParts, composerStr)
	}
	mainCol := strings.Join(mainParts, "\n")

	// ── Compose 3 columns ────────────────────────────────────────────
	border := channelui.RenderVerticalBorder(layout.ContentH, channelui.SlackBorder)
	var panels []string
	if sidebar != "" {
		panels = append(panels, sidebar, border)
	}
	panels = append(panels, mainCol)
	if thread != "" {
		panels = append(panels, border, thread)
	}

	content := lipgloss.NewStyle().MaxWidth(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, panels...))

	// ── Status bar ───────────────────────────────────────────────────
	onlineCount := len(m.members)
	scrollHint := "PgUp/PgDn"
	if scroll > 0 {
		scrollHint = fmt.Sprintf("%d above", scroll)
	}
	var statusBar string
	if m.pending != nil {
		statusText := m.interviewStatusLine()
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Render(statusText),
		)
	} else if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceStatus := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceStatus = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		statusBar = channelui.StatusBarStyle(m.width).Render(fmt.Sprintf(
			" %s %d online │ session %s · %s │ total %s · %s%s │ %s │ Ctrl+J newline │ /doctor",
			"●", onlineCount,
			channelui.FormatUSD(m.usage.Session.CostUsd), channelui.FormatTokenCount(m.usage.Session.TotalTokens),
			channelui.FormatUSD(m.usage.Total.CostUsd), channelui.FormatTokenCount(m.usage.Total.TotalTokens),
			sinceStatus, scrollHint,
		))
	} else if m.quickJumpTarget != quickJumpNone {
		label := "channels"
		if m.quickJumpTarget == quickJumpApps {
			label = "apps"
		}
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				fmt.Sprintf(" Quick nav │ Ctrl+G channels · Ctrl+O apps │ 1-9 switch %s │ Esc cancel", label),
			),
		)
	} else if m.notice != "" {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(" " + m.notice),
		)
	} else if m.isOneOnOne() {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				workspaceState.DefaultStatusLine(scrollHint),
			),
		)
	} else if !m.brokerConnected {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render(workspaceState.DefaultStatusLine(scrollHint)),
		)
	} else if m.replyToID != "" {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				fmt.Sprintf(" ↩ Reply mode │ thread %s │ Ctrl+J newline │ /cancel to return", m.replyToID),
			),
		)
	} else if m.activeApp != channelui.OfficeAppMessages {
		message := fmt.Sprintf(" Viewing %s │ %s │ /messages to return │ /doctor", m.currentAppLabel(), scrollHint)
		if m.activeApp == channelui.OfficeAppCalendar {
			filter := "all"
			if strings.TrimSpace(m.calendarFilter) != "" {
				filter = "@" + m.calendarFilter
			}
			message = fmt.Sprintf(" Calendar │ d day · w week · f filter · a all │ current %s/%s", m.calendarRange, filter)
		}
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(
				message,
			),
		)
	} else {
		statusBar = channelui.StatusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Render(workspaceState.DefaultStatusLine(scrollHint)),
		)
	}

	return content + "\n" + statusBar
}
