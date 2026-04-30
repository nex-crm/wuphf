package channelui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// MemberRuntimeSummary is the per-row presentation slice surfaced
// by DeriveMemberRuntimeSummary: a classified Activity, a one-line
// Detail string for the meta row, and an optional Bubble caption
// for the avatar thought bubble.
type MemberRuntimeSummary struct {
	Activity MemberActivity
	Detail   string
	Bubble   string
}

// DeriveMemberRuntimeSummary classifies a sidebar member into a
// runtime activity, picks a meta-line detail, and resolves the
// thought-bubble caption. ActiveSidebarTask refines the activity
// classification; LastMessage is summarized when the member is
// idle but recently spoke; OfficeAside / TaskBubbleText pick the
// bubble text. Used by the sidebar, runtime strip, and the
// 1:1 mode runtime line.
func DeriveMemberRuntimeSummary(member Member, tasks []Task, now time.Time) MemberRuntimeSummary {
	act := ClassifyActivity(member)
	task, hasTask := ActiveSidebarTask(tasks, member.Slug)
	if hasTask {
		act = ApplyTaskActivity(act, task)
	}

	detail := SummarizeLiveActivity(member.LiveActivity)
	if hasTask {
		taskLine := TaskStatusLine(task)
		switch {
		case taskLine != "" && detail != "":
			detail = taskLine + " · " + detail
		case taskLine != "":
			detail = taskLine
		}
	}
	if detail == "" && strings.TrimSpace(member.LastMessage) != "" && act.Label != "lurking" {
		detail = SummarizeSentence(member.LastMessage)
	}

	bubble := OfficeAside(member.Slug, act.Label, member.LastMessage, now)
	if hasTask && bubble == "" {
		bubble = TaskBubbleText(task)
	}

	return MemberRuntimeSummary{
		Activity: act,
		Detail:   detail,
		Bubble:   bubble,
	}
}

// BuildLiveWorkLines renders the "Live work now" + "Recent
// external actions" + wait-state strip blocks. Members with the
// "you" / "human" slug are skipped; lurking members with empty
// detail are skipped; when focusSlug is non-empty only that slug
// is rendered. Falls back to a "wait state" card when no active
// members or recent actions exist.
func BuildLiveWorkLines(members []Member, tasks []Task, actions []Action, contentWidth int, focusSlug string) []RenderedLine {
	var lines []RenderedLine
	now := time.Now()
	var active []Member
	for _, member := range members {
		if member.Slug == "you" || member.Slug == "human" {
			continue
		}
		summary := DeriveMemberRuntimeSummary(member, tasks, now)
		if summary.Activity.Label == "lurking" && summary.Detail == "" {
			continue
		}
		if focusSlug != "" && member.Slug != focusSlug {
			continue
		}
		active = append(active, member)
	}
	if len(active) > 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Live work now")})
		for _, member := range active {
			summary := DeriveMemberRuntimeSummary(member, tasks, now)
			nameColor := AgentColor(member.Slug)
			name := member.Name
			if strings.TrimSpace(name) == "" {
				name = DisplayName(member.Slug)
			}
			header := ActivityPill(summary.Activity) + " " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(nameColor)).Render(name)
			body := summary.Detail
			if body == "" {
				body = "Working quietly"
			}
			for _, line := range RenderRuntimeEventCard(contentWidth, header, body, "#334155", nil) {
				lines = append(lines, RenderedLine{Text: line})
			}
		}
	}

	recent := RecentExternalActions(actions, 3)
	if len(recent) > 0 {
		lines = append(lines, RenderedLine{Text: ""})
		lines = append(lines, RenderedLine{Text: RenderDateSeparator(contentWidth, "Recent external actions")})
		for _, action := range recent {
			metaParts := []string{}
			if actor := strings.TrimSpace(action.Actor); actor != "" {
				metaParts = append(metaParts, "@"+actor)
			}
			if source := strings.TrimSpace(action.Source); source != "" {
				metaParts = append(metaParts, source)
			}
			if created := strings.TrimSpace(action.CreatedAt); created != "" {
				metaParts = append(metaParts, PrettyRelativeTime(created))
			}
			title := ActionStatePill(action.Kind) + " " + lipgloss.NewStyle().Bold(true).Render(action.Summary)
			for _, line := range RenderRuntimeEventCard(contentWidth, title, strings.Join(metaParts, " · "), "#1E3A8A", nil) {
				lines = append(lines, RenderedLine{Text: line})
			}
		}
	}

	if waitLines := BuildWaitStateLines(tasks, contentWidth, focusSlug, len(active) > 0, len(recent) > 0); len(waitLines) > 0 {
		lines = append(lines, waitLines...)
	}
	return lines
}

// BuildWaitStateLines renders the "Blocked work" or "Wait state"
// section. Blocked tasks (up to 2) take precedence; otherwise the
// "Nothing is moving" / "is waiting for direction" card is shown
// when there's no active work and no recent actions. Returns nil
// when neither condition is met.
func BuildWaitStateLines(tasks []Task, contentWidth int, focusSlug string, hasActive bool, hasRecentActions bool) []RenderedLine {
	blocked := BlockedWorkTasks(tasks, focusSlug, 2)
	if len(blocked) > 0 {
		lines := []RenderedLine{
			{Text: ""},
			{Text: RenderDateSeparator(contentWidth, "Blocked work")},
		}
		for _, task := range blocked {
			extra := []string{"Owner @" + FallbackString(task.Owner, "unowned")}
			if strings.TrimSpace(task.ThreadID) != "" {
				extra = append(extra, "Thread "+task.ThreadID)
			}
			extra = append(extra, "Open task")
			body := strings.TrimSpace(task.Details)
			if body == "" {
				body = "This work is stalled until the blocker is cleared."
			}
			for _, line := range RenderRuntimeEventCard(contentWidth, AccentPill("blocked", "#B91C1C")+" "+lipgloss.NewStyle().Bold(true).Render(task.Title), body, "#B91C1C", extra) {
				lines = append(lines, RenderedLine{Text: line, TaskID: task.ID})
			}
		}
		return lines
	}

	if hasActive || hasRecentActions {
		return nil
	}

	title := SubtlePill("quiet", "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render("Nothing is moving right now")
	body := "This lane is idle. Stanley would be doing the crossword. Use the quiet moment to recover context, choose the next conversation, or give the team a sharper direction."
	extra := []string{"/switcher for active work · /recover for recap · /search to jump directly"}
	if strings.TrimSpace(focusSlug) != "" {
		title = SubtlePill("idle", "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render(DisplayName(focusSlug)+" is waiting for direction")
		body = "This direct session is idle. Ask for a plan, request a review pass, or drop in a concrete decision to unlock the next move. Unlike Jim, this agent is not pranking anyone."
		extra = []string{"Try: give one clear goal, ask for a brief, or request a tradeoff decision"}
	}

	lines := []RenderedLine{
		{Text: ""},
		{Text: RenderDateSeparator(contentWidth, "Wait state")},
	}
	for _, line := range RenderRuntimeEventCard(contentWidth, title, body, "#475569", extra) {
		lines = append(lines, RenderedLine{Text: line})
	}
	return lines
}

// BuildDirectExecutionLines renders the "Execution timeline"
// section for a 1:1 / direct session — recent external actions
// scoped to focusSlug, each card rendered with timestamp pill +
// state pill + summary header and an execution meta line. Returns
// nil when there are no actions to surface.
func BuildDirectExecutionLines(actions []Action, focusSlug string, contentWidth int) []RenderedLine {
	recent := RecentDirectExecutionActions(actions, focusSlug, 6)
	if len(recent) == 0 {
		return nil
	}
	lines := []RenderedLine{
		{Text: ""},
		{Text: RenderDateSeparator(contentWidth, "Execution timeline")},
	}
	for _, action := range recent {
		title := strings.TrimSpace(action.Summary)
		if title == "" {
			title = strings.ReplaceAll(action.Kind, "_", " ")
		}
		when := strings.TrimSpace(ShortClock(action.CreatedAt))
		if when == "" {
			when = PrettyRelativeTime(action.CreatedAt)
		}
		meta := ExecutionMetaLine(action)
		header := SubtlePill(when, "#E2E8F0", "#1E293B") + " " + ActionStatePill(action.Kind) + " " + lipgloss.NewStyle().Bold(true).Render(title)
		for _, line := range RenderRuntimeEventCard(contentWidth, header, meta, "#1D4ED8", nil) {
			lines = append(lines, RenderedLine{Text: line})
		}
	}
	return lines
}

// RenderRuntimeStrip renders the two-line runtime status strip
// shown at the top of the channel feed: a row of summary pills
// (active count, blocked count, needs-you count, latest action
// label) and a one-line Detail string. Returns "" when width is
// below 32 columns or when there's nothing relevant to show.
func RenderRuntimeStrip(members []Member, tasks []Task, requests []Interview, actions []Action, width int, focusSlug string) string {
	if width < 32 {
		return ""
	}
	now := time.Now()
	activeDetails := []string{}
	blockedCount := 0
	waitingHuman := 0
	for _, member := range members {
		if member.Slug == "you" || member.Slug == "human" {
			continue
		}
		if focusSlug != "" && member.Slug != focusSlug {
			continue
		}
		summary := DeriveMemberRuntimeSummary(member, tasks, now)
		if summary.Activity.Label == "blocked" {
			blockedCount++
		}
		if summary.Activity.Label == "lurking" && summary.Detail == "" {
			continue
		}
		name := member.Name
		if strings.TrimSpace(name) == "" {
			name = DisplayName(member.Slug)
		}
		detail := summary.Detail
		if detail == "" {
			detail = summary.Activity.Label
		}
		activeDetails = append(activeDetails, name+" · "+detail)
	}
	for _, req := range requests {
		if req.Blocking || req.Required {
			waitingHuman++
		}
	}

	var pills []string
	if len(activeDetails) > 0 {
		pills = append(pills, SubtlePill(fmt.Sprintf("%d active", len(activeDetails)), "#E2E8F0", "#334155"))
	}
	if blockedCount > 0 {
		pills = append(pills, AccentPill(fmt.Sprintf("%d blocked", blockedCount), "#B91C1C"))
	}
	if waitingHuman > 0 {
		pills = append(pills, AccentPill(fmt.Sprintf("%d need you", waitingHuman), "#B45309"))
	}
	if latest, ok := LatestRelevantAction(actions, focusSlug); ok {
		label := DescribeActionState(latest)
		if len(label) > 52 {
			label = label[:49] + "..."
		}
		pills = append(pills, SubtlePill(label, "#D6E4FF", "#1E3A8A"))
	}
	if len(pills) == 0 && len(activeDetails) == 0 {
		return ""
	}

	detail := "Quiet right now."
	if len(activeDetails) > 0 {
		if focusSlug != "" {
			detail = activeDetails[0]
		} else {
			limit := MinInt(2, len(activeDetails))
			detail = strings.Join(activeDetails[:limit], "   ·   ")
		}
	}

	line1 := strings.Join(pills, " ")
	cardStyle := lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#2F3946")).
		Background(lipgloss.Color("#181A20")).
		Padding(0, 1)
	return cardStyle.Render(line1 + "\n" + MutedText(detail))
}

// OneOnOneRuntimeLine renders the compact runtime descriptor for
// the channel header in 1:1 mode: "<Display> · <activity> ·
// <detail> · <latest action>". Falls back to "" when slug is
// empty or the slug isn't found in the merged office roster.
func OneOnOneRuntimeLine(officeMembers []OfficeMember, members []Member, tasks []Task, actions []Action, slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	var selected Member
	found := false
	for _, member := range MergeOfficeMembers(officeMembers, members, nil) {
		if member.Slug == slug {
			selected = member
			found = true
			break
		}
	}
	if !found {
		return ""
	}
	summary := DeriveMemberRuntimeSummary(selected, tasks, time.Now())
	parts := []string{DisplayName(slug)}
	if summary.Activity.Label != "" {
		parts = append(parts, summary.Activity.Label)
	}
	if summary.Detail != "" {
		parts = append(parts, summary.Detail)
	}
	if latest, ok := LatestRelevantAction(actions, slug); ok {
		parts = append(parts, DescribeActionState(latest))
	}
	return strings.Join(parts, " · ")
}
