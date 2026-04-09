package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

type workspaceReadinessLevel string

const (
	workspaceReadinessReady   workspaceReadinessLevel = "ready"
	workspaceReadinessWarn    workspaceReadinessLevel = "warn"
	workspaceReadinessPreview workspaceReadinessLevel = "preview"
)

type workspaceReadinessState struct {
	Level    workspaceReadinessLevel
	Headline string
	Detail   string
	NextStep string
}

type workspaceUIState struct {
	Runtime         team.RuntimeSnapshot
	Readiness       workspaceReadinessState
	CurrentApp      officeApp
	BrokerConnected bool
	Direct          bool
	Channel         string
	AgentName       string
	AgentSlug       string
	PeerCount       int
	RunningTasks    int
	OpenRequests    int
	BlockingCount   int
	IsolatedCount   int
	UnreadCount     int
	AwaySummary     string
	Focus           string
	NextStep        string
	NeedsYou        *channelInterview
	PrimaryTask     *channelTask
	NoNex           bool
	APIConfigured   bool
}

func (m channelModel) currentWorkspaceUIState() workspaceUIState {
	snapshot := m.currentRuntimeSnapshot()
	awaySummary := resolveWorkspaceAwaySummary(strings.TrimSpace(m.awaySummary), m.unreadCount, snapshot.Recovery)
	state := workspaceUIState{
		Runtime:         snapshot,
		CurrentApp:      m.activeApp,
		BrokerConnected: m.brokerConnected,
		Direct:          m.isOneOnOne(),
		Channel:         m.activeChannel,
		AgentName:       m.oneOnOneAgentName(),
		AgentSlug:       m.oneOnOneAgentSlug(),
		PeerCount:       len(m.members),
		RunningTasks:    countRunningRuntimeTasks(snapshot.Tasks),
		OpenRequests:    len(snapshot.Requests),
		IsolatedCount:   countIsolatedRuntimeTasks(snapshot.Tasks),
		UnreadCount:     m.unreadCount,
		AwaySummary:     awaySummary,
		Focus:           trimRecoverySentence(snapshot.Recovery.Focus),
		NoNex:           config.ResolveNoNex(),
		APIConfigured:   strings.TrimSpace(config.ResolveAPIKey("")) != "",
	}

	for _, req := range snapshot.Requests {
		if runtimeRequestIsOpen(req) && (req.Blocking || req.Required) {
			state.BlockingCount++
		}
	}
	if req, ok := selectNeedsYouRequest(m.requests); ok {
		reqCopy := req
		state.NeedsYou = &reqCopy
		if strings.TrimSpace(state.Focus) == "" {
			state.Focus = req.TitleOrQuestion()
		}
	}
	if tasks := recoveryActiveTasks(m.tasks, 1); len(tasks) > 0 {
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
	state.Readiness = deriveWorkspaceReadiness(state, m.doctor)
	return state
}

func resolveWorkspaceAwaySummary(cached string, unreadCount int, recovery team.SessionRecovery) string {
	if unreadCount == 0 {
		return ""
	}
	cached = strings.TrimSpace(cached)
	if cached != "" {
		return cached
	}
	return summarizeAwayRecovery(unreadCount, recovery)
}

func runtimeRequestIsOpen(req team.RuntimeRequest) bool {
	status := strings.ToLower(strings.TrimSpace(req.Status))
	return status == "" || status == "pending" || status == "open" || status == "draft"
}

func deriveWorkspaceReadiness(state workspaceUIState, doctor *channelDoctorReport) workspaceReadinessState {
	if !state.BrokerConnected {
		return workspaceReadinessState{
			Level:    workspaceReadinessPreview,
			Headline: "Offline preview",
			Detail:   "The workspace is showing manifest-backed context, not the live office runtime.",
			NextStep: "Launch WUPHF to attach the live office, or run /doctor to inspect runtime readiness.",
		}
	}
	if state.NoNex {
		return workspaceReadinessState{
			Level:    workspaceReadinessReady,
			Headline: "Local-only runtime",
			Detail:   "The office is live, but Nex-backed memory and integrations are disabled for this run.",
			NextStep: "Restart without --no-nex when you want memory, integrations, and provider-backed context.",
		}
	}
	if !state.APIConfigured {
		return workspaceReadinessState{
			Level:    workspaceReadinessWarn,
			Headline: "Finish setup",
			Detail:   "The office runtime is up, but Nex-backed context and integrations are not configured yet.",
			NextStep: "Run /init to finish API-key setup, or /doctor to inspect the remaining blockers.",
		}
	}
	if doctor != nil {
		ok, warn, fail := doctor.counts()
		switch {
		case fail > 0:
			return workspaceReadinessState{
				Level:    workspaceReadinessWarn,
				Headline: "Runtime blocked",
				Detail:   doctor.StatusLine(),
				NextStep: firstDoctorNextStep(*doctor, "/doctor shows the full readiness report."),
			}
		case warn > 0:
			return workspaceReadinessState{
				Level:    workspaceReadinessWarn,
				Headline: "Runtime has warnings",
				Detail:   doctor.StatusLine(),
				NextStep: firstDoctorNextStep(*doctor, fmt.Sprintf("%d checks are healthy; inspect /doctor for the warnings.", ok)),
			}
		}
	}
	if state.BlockingCount > 0 && state.NeedsYou != nil {
		return workspaceReadinessState{
			Level:    workspaceReadinessWarn,
			Headline: "Waiting on you",
			Detail:   fmt.Sprintf("The runtime is healthy, but %s is blocking the team.", state.NeedsYou.ID),
			NextStep: fmt.Sprintf("Answer %s or open /recover before delegating more work.", state.NeedsYou.ID),
		}
	}
	return workspaceReadinessState{
		Level:    workspaceReadinessReady,
		Headline: "Ready to work",
		Detail:   "The live office runtime is attached and ready for collaboration.",
		NextStep: "Use /switcher to move through the office, or /recover to regain context before replying.",
	}
}

func firstDoctorNextStep(report channelDoctorReport, fallback string) string {
	for _, check := range report.Checks {
		if strings.TrimSpace(check.NextStep) == "" {
			continue
		}
		if check.Severity == doctorFail || check.Severity == doctorWarn {
			return check.NextStep
		}
	}
	return fallback
}

func (s workspaceUIState) readinessCard() (title, body, accent string, extra []string) {
	accent = "#15803D"
	title = subtlePill("ready", "#DCFCE7", "#166534") + " " + lipgloss.NewStyle().Bold(true).Render(s.Readiness.Headline)
	switch s.Readiness.Level {
	case workspaceReadinessPreview:
		accent = "#D97706"
		title = subtlePill("preview", "#FEF3C7", "#92400E") + " " + lipgloss.NewStyle().Bold(true).Render(s.Readiness.Headline)
	case workspaceReadinessWarn:
		accent = "#B45309"
		title = subtlePill("attention", "#FEF3C7", "#92400E") + " " + lipgloss.NewStyle().Bold(true).Render(s.Readiness.Headline)
	}
	body = s.Readiness.Detail
	if body == "" {
		body = "Workspace state is current."
	}
	if strings.TrimSpace(s.Readiness.NextStep) != "" {
		extra = append(extra, s.Readiness.NextStep)
	}
	if strings.TrimSpace(s.Focus) != "" {
		extra = append(extra, "Focus: "+s.Focus)
	}
	return title, body, accent, extra
}

func (s workspaceUIState) needsYouLines(contentWidth int) []renderedLine {
	return buildNeedsYouLinesForRequest(s.NeedsYou, contentWidth)
}

func (s workspaceUIState) headerMeta() string {
	if s.Direct {
		if !s.BrokerConnected {
			return "  Direct session preview · only this agent can speak here"
		}
		parts := []string{"Direct conversation only"}
		if s.RunningTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d running", s.RunningTasks))
		}
		if s.BlockingCount > 0 {
			parts = append(parts, fmt.Sprintf("%d waiting on you", s.BlockingCount))
		}
		if strings.TrimSpace(s.Readiness.Headline) != "" && s.Readiness.Level != workspaceReadinessReady {
			parts = append(parts, strings.ToLower(s.Readiness.Headline))
		}
		if strings.TrimSpace(s.Focus) != "" {
			parts = append(parts, "focus: "+s.Focus)
		}
		return "  " + strings.Join(parts, " · ")
	}
	if !s.BrokerConnected {
		return fmt.Sprintf("  Offline preview · manifest roster loaded · %d teammates ready for #%s", s.PeerCount, fallbackString(s.Channel, "general"))
	}
	parts := []string{
		fmt.Sprintf("%d teammates", s.PeerCount),
		fmt.Sprintf("%d running", s.RunningTasks),
		fmt.Sprintf("%d open requests", s.OpenRequests),
	}
	if strings.TrimSpace(s.Readiness.Headline) != "" && s.Readiness.Level != workspaceReadinessReady {
		parts = append(parts, strings.ToLower(s.Readiness.Headline))
	}
	if s.BlockingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting on you", s.BlockingCount))
	}
	if strings.TrimSpace(s.Focus) != "" {
		parts = append(parts, "focus: "+truncateText(s.Focus, 56))
	}
	return "  " + strings.Join(parts, " · ")
}

func (s workspaceUIState) defaultStatusLine(scrollHint string) string {
	if s.Direct {
		label := "offline preview"
		if s.BrokerConnected {
			label = "direct session live"
		}
		if s.Readiness.Level == workspaceReadinessWarn {
			label = "direct session attention"
		}
		runtimeHint := "ready"
		if strings.TrimSpace(s.Focus) != "" {
			runtimeHint = s.Focus
		} else if strings.TrimSpace(s.NextStep) != "" {
			runtimeHint = s.NextStep
		}
		return fmt.Sprintf(" %s │ %s │ %s │ Ctrl+J newline │ /switcher │ /doctor", label, scrollHint, truncateText(runtimeHint, 72))
	}
	if !s.BrokerConnected {
		return " Team offline │ manifest preview only │ /doctor explains readiness"
	}
	if s.BlockingCount > 0 && s.NeedsYou != nil {
		return fmt.Sprintf(" Needs you now │ %s │ /request answer %s │ /recover", truncateText(s.NeedsYou.TitleOrQuestion(), 72), s.NeedsYou.ID)
	}
	if s.Readiness.Level == workspaceReadinessWarn && strings.TrimSpace(s.Readiness.NextStep) != "" {
		return fmt.Sprintf(" Attention │ %s │ %s │ /doctor", truncateText(s.Readiness.Headline, 32), truncateText(s.Readiness.NextStep, 72))
	}
	if strings.TrimSpace(s.AwaySummary) != "" && s.UnreadCount > 0 {
		return fmt.Sprintf(" While away │ %s │ %s │ /recover", truncateText(s.AwaySummary, 72), scrollHint)
	}
	if s.PrimaryTask != nil {
		return fmt.Sprintf(" Focus │ %s │ %s │ /switcher │ /doctor", truncateText(s.PrimaryTask.Title, 72), scrollHint)
	}
	return fmt.Sprintf(" Office live │ %s │ /switcher │ /doctor", scrollHint)
}

func (s workspaceUIState) sidebarSummaryLine(activeApp officeApp) string {
	channelLabel := "#" + fallbackString(s.Channel, "general")
	if !s.BrokerConnected {
		return fmt.Sprintf("Offline preview · %s · %d teammates", channelLabel, s.PeerCount)
	}

	parts := []string{sidebarViewLabel(activeApp), channelLabel}
	switch {
	case s.BlockingCount > 0:
		parts = append(parts, fmt.Sprintf("%d waiting", s.BlockingCount))
	case s.Readiness.Level == workspaceReadinessWarn:
		parts = append(parts, "attention")
	case s.RunningTasks > 0:
		parts = append(parts, fmt.Sprintf("%d running", s.RunningTasks))
	case s.OpenRequests > 0:
		parts = append(parts, fmt.Sprintf("%d requests", s.OpenRequests))
	case s.PeerCount > 0:
		parts = append(parts, fmt.Sprintf("%d teammates", s.PeerCount))
	}
	return strings.Join(parts, " · ")
}

func (s workspaceUIState) sidebarHintLine() string {
	switch {
	case !s.BrokerConnected:
		return s.Readiness.NextStep
	case s.BlockingCount > 0 && s.NeedsYou != nil:
		return fmt.Sprintf("Need you: %s · /request answer %s", s.NeedsYou.TitleOrQuestion(), s.NeedsYou.ID)
	case s.Readiness.Level == workspaceReadinessWarn && strings.TrimSpace(s.Readiness.NextStep) != "":
		return s.Readiness.NextStep
	case strings.TrimSpace(s.AwaySummary) != "" && s.UnreadCount > 0:
		return "While away: " + s.AwaySummary
	case !s.NoNex && !s.APIConfigured:
		return "/init finishes setup · /doctor explains what is missing"
	case strings.TrimSpace(s.NextStep) != "":
		return s.NextStep
	case strings.TrimSpace(s.Focus) != "":
		return "Focus: " + s.Focus
	default:
		return "Use /switcher or /recover to move through live office context"
	}
}

func sidebarViewLabel(activeApp officeApp) string {
	switch activeApp {
	case officeAppRecovery:
		return "Recovery view"
	case officeAppTasks:
		return "Task board"
	case officeAppRequests:
		return "Decision queue"
	case officeAppPolicies:
		return "Insights view"
	case officeAppCalendar:
		return "Calendar view"
	case officeAppArtifacts:
		return "Artifacts view"
	case officeAppSkills:
		return "Skills view"
	default:
		return "Message lane"
	}
}

func (m channelModel) buildOfficeIntroLines(contentWidth int) []renderedLine {
	state := m.currentWorkspaceUIState()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	lines := []renderedLine{
		{Text: renderDateSeparator(contentWidth, "Office overview")},
		{Text: ""},
	}
	title := subtlePill("office", "#F8FAFC", "#1264A3") + " " + lipgloss.NewStyle().Bold(true).Render("The WUPHF Office")
	body := "Welcome to The WUPHF Office. Live company-building coordination across channels, direct sessions, tasks, and decisions."
	extra := []string{
		fmt.Sprintf("%d teammates · %d running tasks · %d open requests", state.PeerCount, state.RunningTasks, state.OpenRequests),
	}
	if strings.TrimSpace(state.Focus) != "" {
		extra = append(extra, "Focus: "+state.Focus)
	}
	if strings.TrimSpace(state.NextStep) != "" {
		extra = append(extra, "Next: "+state.NextStep)
	}
	for _, line := range renderRuntimeEventCard(contentWidth, title, body, "#1264A3", extra) {
		lines = append(lines, renderedLine{Text: "  " + line})
	}

	readinessTitle, readinessBody, readinessAccent, readinessExtra := state.readinessCard()
	if state.NoNex && state.BrokerConnected {
		readinessExtra = append(readinessExtra, "Nex is disabled for this run; memory and integrations are local-only.")
	}
	for _, line := range renderRuntimeEventCard(contentWidth, readinessTitle, readinessBody, readinessAccent, readinessExtra) {
		lines = append(lines, renderedLine{Text: "  " + line})
	}

	if state.NeedsYou != nil {
		for _, line := range state.needsYouLines(contentWidth) {
			lines = append(lines, line)
		}
	} else {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: mutedStyle.Render("  Suggested: /switcher for active work, /recover for context, or tag a teammate in #general.")})
	}
	return lines
}

func (m channelModel) buildDirectIntroLines(contentWidth int) []renderedLine {
	state := m.currentWorkspaceUIState()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	lines := []renderedLine{
		{Text: renderDateSeparator(contentWidth, "Direct session")},
		{Text: ""},
	}
	title := subtlePill("1:1", "#F8FAFC", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render("Direct session with "+m.oneOnOneAgentName())
	body := "Direct session reset. Agent pane reloaded in place. This surface is just you and the selected agent. Office channels and teammate chatter stay out of the way."
	extra := []string{"Use /switcher to jump back to the office."}
	if strings.TrimSpace(state.Focus) != "" {
		extra = append(extra, "Focus: "+state.Focus)
	}
	if strings.TrimSpace(state.NextStep) != "" {
		extra = append(extra, "Next: "+state.NextStep)
	}
	for _, line := range renderRuntimeEventCard(contentWidth, title, body, "#334155", extra) {
		lines = append(lines, renderedLine{Text: "  " + line})
	}

	if !state.BrokerConnected {
		readinessTitle, readinessBody, readinessAccent, readinessExtra := state.readinessCard()
		for _, line := range renderRuntimeEventCard(contentWidth, readinessTitle, readinessBody, readinessAccent, readinessExtra) {
			lines = append(lines, renderedLine{Text: "  " + line})
		}
	} else {
		lines = append(lines, renderedLine{Text: mutedStyle.Render("  Suggested: ask for planning help, a review pass, or a direct decision memo.")})
	}
	return lines
}

func (m channelModel) buildOfficeFeedLines(contentWidth int) []renderedLine {
	if len(m.messages) == 0 {
		lines := m.buildOfficeIntroLines(contentWidth)
		lines = append(lines, buildLiveWorkLines(m.members, m.tasks, m.actions, contentWidth, "")...)
		return lines
	}
	lines := buildOfficeMessageLines(m.messages, m.expandedThreads, contentWidth, m.threadsDefaultExpand, m.unreadAnchorID, m.unreadCount)
	lines = append(lines, buildLiveWorkLines(m.members, m.tasks, m.actions, contentWidth, "")...)
	return lines
}

func (m channelModel) buildDirectFeedLines(contentWidth int) []renderedLine {
	if len(m.messages) == 0 {
		lines := m.buildDirectIntroLines(contentWidth)
		focusSlug := m.oneOnOneAgentSlug()
		lines = append(lines, buildDirectExecutionLines(m.actions, focusSlug, contentWidth)...)
		lines = append(lines, buildLiveWorkLines(m.members, m.tasks, nil, contentWidth, focusSlug)...)
		return lines
	}
	lines := buildOneOnOneMessageLines(m.messages, m.expandedThreads, contentWidth, m.oneOnOneAgentName(), m.unreadAnchorID, m.unreadCount)
	focusSlug := m.oneOnOneAgentSlug()
	lines = append(lines, buildDirectExecutionLines(m.actions, focusSlug, contentWidth)...)
	lines = append(lines, buildLiveWorkLines(m.members, m.tasks, nil, contentWidth, focusSlug)...)
	return lines
}
