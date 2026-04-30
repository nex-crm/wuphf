package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

// WorkspaceReadinessLevel classifies how prepared the workspace is
// for live work. "ready" means the runtime is attached and healthy,
// "warn" surfaces a missing or blocking condition, and "preview"
// covers the offline / manifest-only fallback.
type WorkspaceReadinessLevel string

const (
	WorkspaceReadinessReady   WorkspaceReadinessLevel = "ready"
	WorkspaceReadinessWarn    WorkspaceReadinessLevel = "warn"
	WorkspaceReadinessPreview WorkspaceReadinessLevel = "preview"
)

// WorkspaceReadinessState is the human-facing readiness summary
// surfaced in the readiness card and on the sidebar / status lines.
type WorkspaceReadinessState struct {
	Level    WorkspaceReadinessLevel
	Headline string
	Detail   string
	NextStep string
}

// WorkspaceUIState is a derived snapshot of the channel UI's view of
// the live office: the underlying runtime snapshot, broker / direct
// session flags, headline counts, and the current focus / next-step
// sentences. Built once per render via
// channelModel.currentWorkspaceUIState() and threaded through the
// header, sidebar, status line, and recovery view.
type WorkspaceUIState struct {
	Runtime         team.RuntimeSnapshot
	Memory          team.MemoryBackendStatus
	Readiness       WorkspaceReadinessState
	CurrentApp      OfficeApp
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
	NeedsYou        *Interview
	PrimaryTask     *Task
	NoNex           bool
}

// ResolveWorkspaceAwaySummary returns the cached away-summary
// string when set, otherwise it computes the summary from the
// recovery snapshot. Returns "" when there are no unread messages —
// the away strip is hidden in that state.
func ResolveWorkspaceAwaySummary(cached string, unreadCount int, recovery team.SessionRecovery) string {
	if unreadCount == 0 {
		return ""
	}
	cached = strings.TrimSpace(cached)
	if cached != "" {
		return cached
	}
	return SummarizeAwayRecovery(unreadCount, recovery)
}

// DeriveWorkspaceReadiness picks a WorkspaceReadinessState from the
// pre-built workspace fields plus the latest doctor report. Order
// of precedence: offline preview → no-memory local-only ready →
// memory-needs-setup warn → doctor failures → doctor warnings →
// blocking + needs-you → ready.
func DeriveWorkspaceReadiness(state WorkspaceUIState, doctor *DoctorReport) WorkspaceReadinessState {
	if !state.BrokerConnected {
		return WorkspaceReadinessState{
			Level:    WorkspaceReadinessPreview,
			Headline: "Offline preview",
			Detail:   "The workspace is showing manifest-backed context, not the live office runtime.",
			NextStep: "Launch WUPHF to attach the live office, or run /doctor to inspect runtime readiness.",
		}
	}
	if state.Memory.SelectedKind == config.MemoryBackendNone {
		return WorkspaceReadinessState{
			Level:    WorkspaceReadinessReady,
			Headline: "Local-only runtime",
			Detail:   state.Memory.Detail,
			NextStep: state.Memory.NextStep,
		}
	}
	if state.Memory.ActiveKind == config.MemoryBackendNone {
		return WorkspaceReadinessState{
			Level:    WorkspaceReadinessWarn,
			Headline: "Memory backend needs setup",
			Detail:   state.Memory.Detail,
			NextStep: FirstWorkspaceString(state.Memory.NextStep, "/doctor shows the remaining runtime blockers."),
		}
	}
	if doctor != nil {
		ok, warn, fail := doctor.Counts()
		switch {
		case fail > 0:
			return WorkspaceReadinessState{
				Level:    WorkspaceReadinessWarn,
				Headline: "Runtime blocked",
				Detail:   doctor.StatusLine(),
				NextStep: FirstDoctorNextStep(*doctor, "/doctor shows the full readiness report."),
			}
		case warn > 0:
			return WorkspaceReadinessState{
				Level:    WorkspaceReadinessWarn,
				Headline: "Runtime has warnings",
				Detail:   doctor.StatusLine(),
				NextStep: FirstDoctorNextStep(*doctor, fmt.Sprintf("%d checks are healthy; inspect /doctor for the warnings.", ok)),
			}
		}
	}
	if state.BlockingCount > 0 && state.NeedsYou != nil {
		return WorkspaceReadinessState{
			Level:    WorkspaceReadinessWarn,
			Headline: "Waiting on you",
			Detail:   fmt.Sprintf("The runtime is healthy, but %s is blocking the team.", state.NeedsYou.ID),
			NextStep: fmt.Sprintf("Answer %s or open /recover before delegating more work.", state.NeedsYou.ID),
		}
	}
	return WorkspaceReadinessState{
		Level:    WorkspaceReadinessReady,
		Headline: "Ready to work",
		Detail:   fmt.Sprintf("The live office runtime is attached and ready for collaboration with %s memory.", state.Memory.ActiveLabel),
		NextStep: "Use /switcher to move through the office, or /recover to regain context before replying.",
	}
}

// ReadinessCard returns the title / body / accent / extra-rows
// tuple that the office and direct session intros render through
// renderRuntimeEventCard. Title carries a colored pill chosen by
// readiness level; body falls back to a generic "current" sentence
// when Detail is empty; extra surfaces the NextStep + Focus when
// non-empty.
func (s WorkspaceUIState) ReadinessCard() (title, body, accent string, extra []string) {
	accent = "#15803D"
	title = SubtlePill("ready", "#DCFCE7", "#166534") + " " + lipgloss.NewStyle().Bold(true).Render(s.Readiness.Headline)
	switch s.Readiness.Level {
	case WorkspaceReadinessPreview:
		accent = "#D97706"
		title = SubtlePill("preview", "#FEF3C7", "#92400E") + " " + lipgloss.NewStyle().Bold(true).Render(s.Readiness.Headline)
	case WorkspaceReadinessWarn:
		accent = "#B45309"
		title = SubtlePill("attention", "#FEF3C7", "#92400E") + " " + lipgloss.NewStyle().Bold(true).Render(s.Readiness.Headline)
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

// NeedsYouLines forwards the workspace's pending interview into the
// shared "needs you" strip renderer. Returns nil when no interview
// is pending.
func (s WorkspaceUIState) NeedsYouLines(contentWidth int) []RenderedLine {
	return BuildNeedsYouLinesForRequest(s.NeedsYou, contentWidth)
}

// HeaderMeta renders the office channel header's right-side meta
// line: "<peers> teammates · <running> running · <open> open
// requests" and a focus suffix in the office mode, "Direct
// conversation only · …" in direct mode, and the offline preview
// fallback when the broker is detached. Output is leading-space
// padded for the existing two-space header indent.
func (s WorkspaceUIState) HeaderMeta() string {
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
		if strings.TrimSpace(s.Readiness.Headline) != "" && s.Readiness.Level != WorkspaceReadinessReady {
			parts = append(parts, strings.ToLower(s.Readiness.Headline))
		}
		if strings.TrimSpace(s.Focus) != "" {
			parts = append(parts, "focus: "+s.Focus)
		}
		return "  " + strings.Join(parts, " · ")
	}
	if !s.BrokerConnected {
		return fmt.Sprintf("  Offline preview · manifest roster loaded · %d teammates ready for #%s", s.PeerCount, FallbackString(s.Channel, "general"))
	}
	parts := []string{
		fmt.Sprintf("%d teammates", s.PeerCount),
		fmt.Sprintf("%d running", s.RunningTasks),
		fmt.Sprintf("%d open requests", s.OpenRequests),
	}
	if strings.TrimSpace(s.Readiness.Headline) != "" && s.Readiness.Level != WorkspaceReadinessReady {
		parts = append(parts, strings.ToLower(s.Readiness.Headline))
	}
	if s.BlockingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting on you", s.BlockingCount))
	}
	if strings.TrimSpace(s.Focus) != "" {
		parts = append(parts, "focus: "+TruncateText(s.Focus, 56))
	}
	return "  " + strings.Join(parts, " · ")
}

// DefaultStatusLine renders the bottom status line for the channel
// view. The exact wording is picked from a precedence: direct mode
// first, then offline preview, then needs-you / readiness warn /
// while-away / focus-task / live office.
func (s WorkspaceUIState) DefaultStatusLine(scrollHint string) string {
	if s.Direct {
		label := "offline preview"
		if s.BrokerConnected {
			label = "direct session live"
		}
		if s.Readiness.Level == WorkspaceReadinessWarn {
			label = "direct session attention"
		}
		runtimeHint := "ready"
		if strings.TrimSpace(s.Focus) != "" {
			runtimeHint = s.Focus
		} else if strings.TrimSpace(s.NextStep) != "" {
			runtimeHint = s.NextStep
		}
		return fmt.Sprintf(" %s │ %s │ %s │ Ctrl+J newline │ /switcher │ /doctor", label, scrollHint, TruncateText(runtimeHint, 72))
	}
	if !s.BrokerConnected {
		return " Team offline │ manifest preview only │ /doctor explains readiness"
	}
	if s.BlockingCount > 0 && s.NeedsYou != nil {
		return fmt.Sprintf(" Needs you now │ %s │ /request answer %s │ /recover", TruncateText(s.NeedsYou.TitleOrQuestion(), 72), s.NeedsYou.ID)
	}
	if s.Readiness.Level == WorkspaceReadinessWarn && strings.TrimSpace(s.Readiness.NextStep) != "" {
		return fmt.Sprintf(" Attention │ %s │ %s │ /doctor", TruncateText(s.Readiness.Headline, 32), TruncateText(s.Readiness.NextStep, 72))
	}
	if strings.TrimSpace(s.AwaySummary) != "" && s.UnreadCount > 0 {
		return fmt.Sprintf(" While away │ %s │ %s │ /recover", TruncateText(s.AwaySummary, 72), scrollHint)
	}
	if s.PrimaryTask != nil {
		return fmt.Sprintf(" Focus │ %s │ %s │ /switcher │ /doctor", TruncateText(s.PrimaryTask.Title, 72), scrollHint)
	}
	return fmt.Sprintf(" Office live │ %s │ /switcher │ /doctor", scrollHint)
}

// SidebarSummaryLine renders the workspace summary line in the
// sidebar header — view label · channel · headline counts —
// falling back to "Offline preview · #channel · N teammates" when
// the broker is detached.
func (s WorkspaceUIState) SidebarSummaryLine(activeApp OfficeApp) string {
	channelLabel := "#" + FallbackString(s.Channel, "general")
	if !s.BrokerConnected {
		return fmt.Sprintf("Offline preview · %s · %d teammates", channelLabel, s.PeerCount)
	}

	parts := []string{SidebarViewLabel(activeApp), channelLabel}
	switch {
	case s.BlockingCount > 0:
		parts = append(parts, fmt.Sprintf("%d waiting", s.BlockingCount))
	case s.Readiness.Level == WorkspaceReadinessWarn:
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

// SidebarHintLine renders the second sidebar header line — the
// "what to do next" hint — picked from a precedence: offline,
// needs-you, readiness warn, while-away, memory-needs-setup,
// general next-step / focus, default catch-all.
func (s WorkspaceUIState) SidebarHintLine() string {
	switch {
	case !s.BrokerConnected:
		return s.Readiness.NextStep
	case s.BlockingCount > 0 && s.NeedsYou != nil:
		return fmt.Sprintf("Need you: %s · /request answer %s", s.NeedsYou.TitleOrQuestion(), s.NeedsYou.ID)
	case s.Readiness.Level == WorkspaceReadinessWarn && strings.TrimSpace(s.Readiness.NextStep) != "":
		return s.Readiness.NextStep
	case strings.TrimSpace(s.AwaySummary) != "" && s.UnreadCount > 0:
		return "While away: " + s.AwaySummary
	case s.Memory.SelectedKind == config.MemoryBackendNex && s.Memory.ActiveKind == config.MemoryBackendNone:
		return "/init finishes Nex setup · /doctor explains what is missing"
	case s.Memory.SelectedKind == config.MemoryBackendGBrain && s.Memory.ActiveKind == config.MemoryBackendNone:
		return FirstWorkspaceString(s.Memory.NextStep, "/doctor explains what is missing")
	case strings.TrimSpace(s.NextStep) != "":
		return s.NextStep
	case strings.TrimSpace(s.Focus) != "":
		return "Focus: " + s.Focus
	default:
		return "Use /switcher or /recover to move through live office context"
	}
}
