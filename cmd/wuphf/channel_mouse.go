package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Mouse hit-testing and main-panel geometry. Owns:
//   - the mouseAction type
//   - mouseActionAt: top-level dispatch over the layout (sidebar / main / thread)
//   - mainPanelMouseAction: dispatch within the main panel into rows / cards / popups
//   - mainPanelGeometry: header / message / composer / popup row counts
//
// The geometry projector is tightly coupled to the rendering pipeline; it
// drives both the mouse hit-test and the message viewport sizing. Renders
// themselves stay in channel_render.go and channelui/.

type mouseAction struct {
	Kind  string
	Value string
}

func (m channelModel) currentLayout() channelui.LayoutDimensions {
	return channelui.ComputeLayout(
		m.width,
		m.height,
		m.threadPanelOpen && !m.isOneOnOne(),
		m.sidebarCollapsed || m.isOneOnOne(),
	)
}

func (m channelModel) mouseActionAt(x, y int) (mouseAction, bool) {
	if m.width == 0 || m.height == 0 || y >= m.height-1 {
		return mouseAction{}, false
	}

	layout := m.currentLayout()
	sidebarW := 0
	if layout.ShowSidebar {
		sidebarW = layout.SidebarW
		if x < sidebarW {
			if item, ok := m.sidebarItemAt(y); ok {
				return mouseAction{Kind: item.Kind, Value: item.Value}, true
			}
			return mouseAction{Kind: "focus", Value: "sidebar"}, true
		}
		x -= sidebarW + 1
	}

	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}
	if x >= 0 && x < mainW {
		if action, ok := m.mainPanelMouseAction(x, y, mainW, layout.ContentH); ok {
			return action, true
		}
		return mouseAction{Kind: "focus", Value: "main"}, true
	}

	if layout.ShowThread {
		threadStart := mainW + 1
		if x >= threadStart {
			return mouseAction{Kind: "focus", Value: "thread"}, true
		}
	}

	return mouseAction{}, false
}

func (m channelModel) mainPanelMouseAction(x, y, mainW, contentH int) (mouseAction, bool) {
	headerH, msgH, popupRows := m.mainPanelGeometry(mainW, contentH)
	if y < headerH {
		return mouseAction{}, false
	}

	msgTop := headerH
	msgBottom := headerH + msgH
	if y >= msgTop && y < msgBottom {
		row := y - msgTop
		if m.activeApp == channelui.OfficeAppMessages && m.unreadCount > 0 && m.scroll > 0 && row == 0 {
			return mouseAction{Kind: "jump-latest"}, true
		}
		if len(popupRows) > 0 {
			popupStart := msgBottom - len(popupRows)
			if y >= popupStart {
				idx := y - popupStart
				if m.autocomplete.IsVisible() {
					if idx < 0 || idx >= len(m.autocomplete.Matches()) {
						return mouseAction{}, false
					}
					return mouseAction{Kind: "autocomplete", Value: fmt.Sprintf("%d", idx)}, true
				}
				if m.mention.IsVisible() {
					if idx < 0 || idx >= len(m.mention.Matches()) {
						return mouseAction{}, false
					}
					return mouseAction{Kind: "mention", Value: fmt.Sprintf("%d", idx)}, true
				}
			}
		}

		contentWidth := mainW - 2
		if contentWidth < 32 {
			contentWidth = 32
		}
		allLines := m.currentMainViewportLines(contentWidth, msgH)
		visibleRows, _, _, _ := channelui.SliceRenderedLines(allLines, msgH, m.scroll)
		if row >= 0 && row < len(visibleRows) {
			if visibleRows[row].PromptValue != "" {
				return mouseAction{Kind: "prompt", Value: visibleRows[row].PromptValue}, true
			}
			switch m.activeApp {
			case channelui.OfficeAppMessages:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
			case channelui.OfficeAppInbox, channelui.OfficeAppOutbox:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			case channelui.OfficeAppTasks:
				if visibleRows[row].TaskID != "" {
					return mouseAction{Kind: "task", Value: visibleRows[row].TaskID}, true
				}
			case channelui.OfficeAppRequests:
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			case channelui.OfficeAppCalendar:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
				if visibleRows[row].TaskID != "" {
					return mouseAction{Kind: "task", Value: visibleRows[row].TaskID}, true
				}
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			case channelui.OfficeAppRecovery, channelui.OfficeAppArtifacts:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
				if visibleRows[row].TaskID != "" {
					return mouseAction{Kind: "task", Value: visibleRows[row].TaskID}, true
				}
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			}
		}
	}

	return mouseAction{}, false
}

func (m channelModel) mainPanelGeometry(mainW, contentH int) (headerH, msgH int, popupRows []string) {
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
	headerH = lipgloss.Height(channelHeader)
	runtimeStrip := ""
	if m.activeApp == channelui.OfficeAppMessages || m.isOneOnOne() {
		focusSlug := ""
		if m.isOneOnOne() {
			focusSlug = m.oneOnOneAgentSlug()
		}
		runtimeStrip = channelui.RenderRuntimeStrip(m.members, m.tasks, m.requests, m.actions, mainW-4, focusSlug)
	}

	activePending := m.visiblePendingRequest()
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
	initPanel := ""
	if confirmCard != "" {
		initPanel = confirmCard
	} else if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}
	msgH = contentH - headerH - lipgloss.Height(runtimeStrip) - lipgloss.Height(composerStr) - lipgloss.Height(interviewCard) - lipgloss.Height(memberDraftCard) - lipgloss.Height(doctorCard) - lipgloss.Height(initPanel) - 1
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		popupRows = strings.Split(popup, "\n")
	}
	return headerH, msgH, popupRows
}

// applyRecoveryPrompt isn't strictly mouse/geometry, but it's the natural
// landing site for clicks on a recovery prompt row (mouseAction Kind="prompt"
// dispatches here). Co-locating it with the mouse dispatcher keeps the
// click-to-effect path readable.
func (m *channelModel) applyRecoveryPrompt(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		m.notice = "Nothing inserted."
		return
	}
	m.activeApp = channelui.OfficeAppMessages
	m.syncSidebarCursorToActive()
	m.focus = focusMain
	m.insertIntoActiveComposer(prompt)
	m.notice = "Inserted a recovery prompt into the composer."
}
