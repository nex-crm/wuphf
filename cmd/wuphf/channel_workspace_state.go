package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) currentWorkspaceUIState() channelui.WorkspaceUIState {
	snapshot := m.currentRuntimeSnapshot()
	awaySummary := channelui.ResolveWorkspaceAwaySummary(strings.TrimSpace(m.awaySummary), m.unreadCount, snapshot.Recovery)
	state := channelui.WorkspaceUIState{
		Runtime:         snapshot,
		CurrentApp:      m.activeApp,
		BrokerConnected: m.brokerConnected,
		Direct:          m.isOneOnOne(),
		Channel:         m.activeChannel,
		AgentName:       m.oneOnOneAgentName(),
		AgentSlug:       m.oneOnOneAgentSlug(),
		PeerCount:       len(m.members),
		RunningTasks:    channelui.CountRunningRuntimeTasks(snapshot.Tasks),
		OpenRequests:    len(snapshot.Requests),
		IsolatedCount:   channelui.CountIsolatedRuntimeTasks(snapshot.Tasks),
		UnreadCount:     m.unreadCount,
		AwaySummary:     awaySummary,
		Focus:           channelui.TrimRecoverySentence(snapshot.Recovery.Focus),
		Memory:          team.ResolveMemoryBackendStatus(),
		NoNex:           config.ResolveNoNex(),
	}

	for _, req := range snapshot.Requests {
		if channelui.RuntimeRequestIsOpen(req) && (req.Blocking || req.Required) {
			state.BlockingCount++
		}
	}
	if req, ok := channelui.SelectNeedsYouRequest(m.requests); ok {
		reqCopy := req
		state.NeedsYou = &reqCopy
		if strings.TrimSpace(state.Focus) == "" {
			state.Focus = req.TitleOrQuestion()
		}
	}
	if tasks := channelui.RecoveryActiveTasks(m.tasks, 1); len(tasks) > 0 {
		taskCopy := tasks[0]
		state.PrimaryTask = &taskCopy
		if strings.TrimSpace(state.Focus) == "" {
			state.Focus = taskCopy.Title
		}
	}
	if state.NeedsYou != nil {
		state.NextStep = "Answer " + state.NeedsYou.ID + " before the team moves further."
	} else if len(snapshot.Recovery.NextSteps) > 0 {
		state.NextStep = strings.TrimSpace(snapshot.Recovery.NextSteps[0])
	} else if state.Direct {
		state.NextStep = "Keep the discussion in this direct session or jump back with /switcher."
	} else {
		state.NextStep = "Tag a teammate, open /switcher, or use /recover to regain context."
	}
	state.Readiness = channelui.DeriveWorkspaceReadiness(state, m.doctor)
	return state
}

func (m channelModel) buildOfficeIntroLines(contentWidth int) []channelui.RenderedLine {
	state := m.currentWorkspaceUIState()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))
	lines := []channelui.RenderedLine{
		{Text: channelui.RenderDateSeparator(contentWidth, "Office overview")},
		{Text: ""},
	}
	title := channelui.SubtlePill("office", "#F8FAFC", "#1264A3") + " " + lipgloss.NewStyle().Bold(true).Render("The WUPHF Office")
	body := "Welcome to The WUPHF Office. Live company-building coordination across channels, direct sessions, tasks, and decisions. Michael Scott would be proud — and also confused, but mostly proud."
	extra := []string{
		fmt.Sprintf("%d teammates · %d running tasks · %d open requests", state.PeerCount, state.RunningTasks, state.OpenRequests),
	}
	if strings.TrimSpace(state.Focus) != "" {
		extra = append(extra, "Focus: "+state.Focus)
	}
	if strings.TrimSpace(state.NextStep) != "" {
		extra = append(extra, "Next: "+state.NextStep)
	}
	for _, line := range channelui.RenderRuntimeEventCard(contentWidth, title, body, "#1264A3", extra) {
		lines = append(lines, channelui.RenderedLine{Text: "  " + line})
	}

	readinessTitle, readinessBody, readinessAccent, readinessExtra := state.ReadinessCard()
	if state.BrokerConnected {
		if state.Memory.ActiveKind != config.MemoryBackendNone {
			readinessExtra = append(readinessExtra, "Memory backend: "+state.Memory.ActiveLabel)
		} else {
			readinessExtra = append(readinessExtra, "Memory backend: "+state.Memory.SelectedLabel)
		}
	}
	for _, line := range channelui.RenderRuntimeEventCard(contentWidth, readinessTitle, readinessBody, readinessAccent, readinessExtra) {
		lines = append(lines, channelui.RenderedLine{Text: "  " + line})
	}

	if state.NeedsYou != nil {
		lines = append(lines, state.NeedsYouLines(contentWidth)...)
	} else {
		lines = append(lines, channelui.RenderedLine{Text: ""})
		lines = append(lines, channelui.RenderedLine{Text: mutedStyle.Render("  Suggested: /switcher for active work, /recover for context, or tag a teammate in #general. Bears. Beets. Ship it.")})
	}
	return lines
}

func (m channelModel) buildDirectIntroLines(contentWidth int) []channelui.RenderedLine {
	state := m.currentWorkspaceUIState()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))
	lines := []channelui.RenderedLine{
		{Text: channelui.RenderDateSeparator(contentWidth, "Direct session")},
		{Text: ""},
	}
	title := channelui.SubtlePill("1:1", "#F8FAFC", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render("Direct session with "+m.oneOnOneAgentName())
	body := "Direct session reset. Agent pane reloaded in place. This surface is just you and the selected agent. No office channels, no colleague chatter, no Toby. The door is closed."
	extra := []string{"Use /switcher to jump back to the office."}
	if strings.TrimSpace(state.Focus) != "" {
		extra = append(extra, "Focus: "+state.Focus)
	}
	if strings.TrimSpace(state.NextStep) != "" {
		extra = append(extra, "Next: "+state.NextStep)
	}
	for _, line := range channelui.RenderRuntimeEventCard(contentWidth, title, body, "#334155", extra) {
		lines = append(lines, channelui.RenderedLine{Text: "  " + line})
	}

	if !state.BrokerConnected {
		readinessTitle, readinessBody, readinessAccent, readinessExtra := state.ReadinessCard()
		for _, line := range channelui.RenderRuntimeEventCard(contentWidth, readinessTitle, readinessBody, readinessAccent, readinessExtra) {
			lines = append(lines, channelui.RenderedLine{Text: "  " + line})
		}
	} else {
		lines = append(lines, channelui.RenderedLine{Text: mutedStyle.Render("  Suggested: ask for planning help, a review pass, or a direct decision memo. Dwight would want a full briefing first. You do not have to do that.")})
	}
	return lines
}

func (m channelModel) buildOfficeFeedLines(contentWidth int) []channelui.RenderedLine {
	if len(m.messages) == 0 {
		lines := m.buildOfficeIntroLines(contentWidth)
		lines = append(lines, channelui.BuildLiveWorkLines(m.members, m.tasks, m.actions, contentWidth, "")...)
		return lines
	}
	lines := buildOfficeMessageLines(m.messages, m.expandedThreads, contentWidth, m.threadsDefaultExpand, m.unreadAnchorID, m.unreadCount)
	lines = append(lines, channelui.BuildLiveWorkLines(m.members, m.tasks, m.actions, contentWidth, "")...)
	return lines
}

func (m channelModel) buildDirectFeedLines(contentWidth int) []channelui.RenderedLine {
	if len(m.messages) == 0 {
		lines := m.buildDirectIntroLines(contentWidth)
		focusSlug := m.oneOnOneAgentSlug()
		lines = append(lines, channelui.BuildDirectExecutionLines(m.actions, focusSlug, contentWidth)...)
		lines = append(lines, channelui.BuildLiveWorkLines(m.members, m.tasks, nil, contentWidth, focusSlug)...)
		return lines
	}
	lines := buildOneOnOneMessageLines(m.messages, m.expandedThreads, contentWidth, m.oneOnOneAgentName(), m.unreadAnchorID, m.unreadCount)
	focusSlug := m.oneOnOneAgentSlug()
	lines = append(lines, channelui.BuildDirectExecutionLines(m.actions, focusSlug, contentWidth)...)
	lines = append(lines, channelui.BuildLiveWorkLines(m.members, m.tasks, nil, contentWidth, focusSlug)...)
	return lines
}
