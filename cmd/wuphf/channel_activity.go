package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type memberRuntimeSummary struct {
	Activity memberActivity
	Detail   string
	Bubble   string
}

func deriveMemberRuntimeSummary(member channelMember, tasks []channelTask, now time.Time) memberRuntimeSummary {
	act := classifyActivity(member)
	task, hasTask := activeSidebarTask(tasks, member.Slug)
	if hasTask {
		act = applyTaskActivity(act, task)
	}

	detail := summarizeLiveActivity(member.LiveActivity)
	if hasTask {
		taskLine := taskStatusLine(task)
		switch {
		case taskLine != "" && detail != "":
			detail = taskLine + " · " + detail
		case taskLine != "":
			detail = taskLine
		}
	}
	if detail == "" && strings.TrimSpace(member.LastMessage) != "" && act.Label != "lurking" {
		detail = summarizeSentence(member.LastMessage)
	}

	bubble := officeAside(member.Slug, act.Label, member.LastMessage, now)
	if hasTask && bubble == "" {
		bubble = taskBubbleText(task)
	}

	return memberRuntimeSummary{
		Activity: act,
		Detail:   detail,
		Bubble:   bubble,
	}
}

func taskStatusLine(task channelTask) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		return "Working on " + title
	case "review":
		return "Reviewing " + title
	case "blocked":
		return "Blocked on " + title
	case "claimed", "pending", "open":
		return "Queued: " + title
	default:
		return title
	}
}

func summarizeLiveActivity(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := sanitizeActivityLine(lines[i])
		if line == "" {
			continue
		}
		return line
	}
	return ""
}

func sanitizeActivityLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "shift+tab"),
		strings.Contains(lower, "permissions"),
		strings.Contains(lower, "bypass"),
		strings.HasPrefix(line, "❯"),
		strings.HasPrefix(line, "─"),
		strings.HasPrefix(line, "━"):
		return ""
	case strings.Contains(lower, "rg "),
		strings.Contains(lower, "grep "),
		strings.Contains(lower, "search"):
		return "Searching the codebase"
	case strings.Contains(lower, "read "),
		strings.Contains(lower, "open "),
		strings.Contains(lower, "inspect"):
		return "Reading files"
	case strings.Contains(lower, "go test"),
		strings.Contains(lower, "npm test"),
		strings.Contains(lower, "pytest"):
		return "Running tests"
	case strings.Contains(lower, "go build"),
		strings.Contains(lower, "npm run build"),
		strings.Contains(lower, "bun run build"):
		return "Building the project"
	case strings.Contains(lower, "curl "),
		strings.Contains(lower, "http://"),
		strings.Contains(lower, "https://"):
		return "Calling an external system"
	}
	return summarizeSentence(line)
}

func summarizeSentence(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	text = strings.Trim(text, "\"")
	text = strings.TrimSpace(text)
	if len(text) <= 88 {
		return text
	}
	return text[:85] + "..."
}

func buildLiveWorkLines(members []channelMember, tasks []channelTask, actions []channelAction, contentWidth int, focusSlug string) []renderedLine {
	var lines []renderedLine
	now := time.Now()
	var active []channelMember
	for _, member := range members {
		if member.Slug == "you" || member.Slug == "human" {
			continue
		}
		summary := deriveMemberRuntimeSummary(member, tasks, now)
		if summary.Activity.Label == "lurking" && summary.Detail == "" {
			continue
		}
		if focusSlug != "" && member.Slug != focusSlug {
			continue
		}
		active = append(active, member)
	}
	if len(active) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Live work now")})
		for _, member := range active {
			summary := deriveMemberRuntimeSummary(member, tasks, now)
			nameColor := agentColorMap[member.Slug]
			if nameColor == "" {
				nameColor = "#64748B"
			}
			name := member.Name
			if strings.TrimSpace(name) == "" {
				name = displayName(member.Slug)
			}
			header := activityPill(summary.Activity) + " " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(nameColor)).Render(name)
			body := summary.Detail
			if body == "" {
				body = "Working quietly"
			}
			for _, line := range renderRuntimeEventCard(contentWidth, header, body, "#334155", nil) {
				lines = append(lines, renderedLine{Text: line})
			}
		}
	}

	recent := recentExternalActions(actions, 3)
	if len(recent) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Recent external actions")})
		for _, action := range recent {
			metaParts := []string{}
			if actor := strings.TrimSpace(action.Actor); actor != "" {
				metaParts = append(metaParts, "@"+actor)
			}
			if source := strings.TrimSpace(action.Source); source != "" {
				metaParts = append(metaParts, source)
			}
			if created := strings.TrimSpace(action.CreatedAt); created != "" {
				metaParts = append(metaParts, prettyRelativeTime(created))
			}
			title := actionStatePill(action.Kind) + " " + lipgloss.NewStyle().Bold(true).Render(action.Summary)
			for _, line := range renderRuntimeEventCard(contentWidth, title, strings.Join(metaParts, " · "), "#1E3A8A", nil) {
				lines = append(lines, renderedLine{Text: line})
			}
		}
	}
	return lines
}

func buildDirectExecutionLines(actions []channelAction, focusSlug string, contentWidth int) []renderedLine {
	recent := recentDirectExecutionActions(actions, focusSlug, 6)
	if len(recent) == 0 {
		return nil
	}
	lines := []renderedLine{
		{Text: ""},
		{Text: renderDateSeparator(contentWidth, "Execution timeline")},
	}
	for _, action := range recent {
		title := strings.TrimSpace(action.Summary)
		if title == "" {
			title = strings.ReplaceAll(action.Kind, "_", " ")
		}
		when := strings.TrimSpace(shortClock(action.CreatedAt))
		if when == "" {
			when = prettyRelativeTime(action.CreatedAt)
		}
		meta := executionMetaLine(action)
		header := subtlePill(when, "#E2E8F0", "#1E293B") + " " + actionStatePill(action.Kind) + " " + lipgloss.NewStyle().Bold(true).Render(title)
		for _, line := range renderRuntimeEventCard(contentWidth, header, meta, "#1D4ED8", nil) {
			lines = append(lines, renderedLine{Text: line})
		}
	}
	return lines
}

func renderRuntimeStrip(members []channelMember, tasks []channelTask, requests []channelInterview, actions []channelAction, width int, focusSlug string) string {
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
		summary := deriveMemberRuntimeSummary(member, tasks, now)
		if summary.Activity.Label == "blocked" {
			blockedCount++
		}
		if summary.Activity.Label == "lurking" && summary.Detail == "" {
			continue
		}
		name := member.Name
		if strings.TrimSpace(name) == "" {
			name = displayName(member.Slug)
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
		pills = append(pills, subtlePill(fmt.Sprintf("%d active", len(activeDetails)), "#E2E8F0", "#334155"))
	}
	if blockedCount > 0 {
		pills = append(pills, accentPill(fmt.Sprintf("%d blocked", blockedCount), "#B91C1C"))
	}
	if waitingHuman > 0 {
		pills = append(pills, accentPill(fmt.Sprintf("%d need you", waitingHuman), "#B45309"))
	}
	if latest, ok := latestRelevantAction(actions, focusSlug); ok {
		label := describeActionState(latest)
		if len(label) > 52 {
			label = label[:49] + "..."
		}
		pills = append(pills, subtlePill(label, "#D6E4FF", "#1E3A8A"))
	}
	if len(pills) == 0 && len(activeDetails) == 0 {
		return ""
	}

	detail := "Quiet right now."
	if len(activeDetails) > 0 {
		if focusSlug != "" {
			detail = activeDetails[0]
		} else {
			limit := minInt(2, len(activeDetails))
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
	return cardStyle.Render(line1 + "\n" + mutedText(detail))
}

func renderRuntimeEventCard(width int, title, body, accent string, extra []string) []string {
	if width < 28 {
		return []string{"  " + title, "    " + body}
	}
	lines := []string{title}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, mutedText(body))
	}
	for _, line := range extra {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, mutedText(line))
	}
	card := lipgloss.NewStyle().
		Width(width-2).
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(accent)).
		Background(lipgloss.Color("#16181E")).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
	return strings.Split(card, "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func recentDirectExecutionActions(actions []channelAction, focusSlug string, limit int) []channelAction {
	var filtered []channelAction
	for _, action := range actions {
		if !strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
			continue
		}
		actor := strings.TrimSpace(action.Actor)
		if focusSlug != "" && actor != "" && actor != focusSlug && actor != "scheduler" {
			continue
		}
		filtered = append(filtered, action)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := append([]channelAction(nil), filtered...)
	reverseAny(out)
	return out
}

func executionMetaLine(action channelAction) string {
	parts := []string{}
	if source := strings.TrimSpace(action.Source); source != "" {
		parts = append(parts, source)
	}
	if actor := strings.TrimSpace(action.Actor); actor != "" {
		parts = append(parts, "@"+actor)
	}
	if related := strings.TrimSpace(action.RelatedID); related != "" {
		parts = append(parts, related)
	}
	if when := strings.TrimSpace(action.CreatedAt); when != "" {
		parts = append(parts, prettyRelativeTime(when))
	}
	return strings.Join(parts, " · ")
}

func oneOnOneRuntimeLine(officeMembers []officeMemberInfo, members []channelMember, tasks []channelTask, actions []channelAction, slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	var selected channelMember
	found := false
	for _, member := range mergeOfficeMembers(officeMembers, members, nil) {
		if member.Slug == slug {
			selected = member
			found = true
			break
		}
	}
	if !found {
		return ""
	}
	summary := deriveMemberRuntimeSummary(selected, tasks, time.Now())
	parts := []string{displayName(slug)}
	if summary.Activity.Label != "" {
		parts = append(parts, summary.Activity.Label)
	}
	if summary.Detail != "" {
		parts = append(parts, summary.Detail)
	}
	if latest, ok := latestRelevantAction(actions, slug); ok {
		parts = append(parts, describeActionState(latest))
	}
	return strings.Join(parts, " · ")
}

func latestRelevantAction(actions []channelAction, slug string) (channelAction, bool) {
	slug = strings.TrimSpace(slug)
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		if !strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
			continue
		}
		actor := strings.TrimSpace(action.Actor)
		if actor != "" && actor != slug && actor != "scheduler" {
			continue
		}
		return action, true
	}
	return channelAction{}, false
}

func describeActionState(action channelAction) string {
	switch {
	case strings.Contains(action.Kind, "failed"):
		return fmt.Sprintf("last action failed: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "planned"):
		return fmt.Sprintf("dry-run ready: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "scheduled"):
		return fmt.Sprintf("scheduled: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "registered"):
		return fmt.Sprintf("listening: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "executed"), strings.Contains(action.Kind, "created"):
		return fmt.Sprintf("completed: %s", strings.TrimSpace(action.Summary))
	default:
		return strings.TrimSpace(action.Summary)
	}
}

func activityPill(act memberActivity) string {
	switch act.Label {
	case "working", "shipping":
		return accentPill(act.Label, "#7C3AED")
	case "reviewing":
		return accentPill(act.Label, "#2563EB")
	case "blocked":
		return accentPill(act.Label, "#B91C1C")
	case "queued", "plotting":
		return accentPill(act.Label, "#B45309")
	case "talking":
		return accentPill(act.Label, "#15803D")
	case "away":
		return subtlePill(act.Label, "#CBD5E1", "#475569")
	default:
		return subtlePill(act.Label, "#CBD5E1", "#334155")
	}
}

func actionStatePill(kind string) string {
	switch {
	case strings.Contains(kind, "failed"):
		return accentPill("failed", "#B91C1C")
	case strings.Contains(kind, "planned"):
		return accentPill("planned", "#1D4ED8")
	case strings.Contains(kind, "registered"), strings.Contains(kind, "received"):
		return accentPill("listening", "#7C3AED")
	case strings.Contains(kind, "executed"), strings.Contains(kind, "created"), strings.Contains(kind, "scheduled"):
		return accentPill("completed", "#15803D")
	default:
		return subtlePill(strings.ReplaceAll(kind, "_", " "), "#E2E8F0", "#334155")
	}
}
