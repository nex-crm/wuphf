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

	if waitLines := buildWaitStateLines(tasks, contentWidth, focusSlug, len(active) > 0, len(recent) > 0); len(waitLines) > 0 {
		lines = append(lines, waitLines...)
	}
	return lines
}

func buildWaitStateLines(tasks []channelTask, contentWidth int, focusSlug string, hasActive bool, hasRecentActions bool) []renderedLine {
	blocked := blockedWorkTasks(tasks, focusSlug, 2)
	if len(blocked) > 0 {
		lines := []renderedLine{
			{Text: ""},
			{Text: renderDateSeparator(contentWidth, "Blocked work")},
		}
		for _, task := range blocked {
			extra := []string{"Owner @" + fallbackString(task.Owner, "unowned")}
			if strings.TrimSpace(task.ThreadID) != "" {
				extra = append(extra, "Thread "+task.ThreadID)
			}
			extra = append(extra, "Open task")
			body := strings.TrimSpace(task.Details)
			if body == "" {
				body = "This work is stalled until the blocker is cleared."
			}
			for _, line := range renderRuntimeEventCard(contentWidth, accentPill("blocked", "#B91C1C")+" "+lipgloss.NewStyle().Bold(true).Render(task.Title), body, "#B91C1C", extra) {
				lines = append(lines, renderedLine{Text: line, TaskID: task.ID})
			}
		}
		return lines
	}

	if hasActive || hasRecentActions {
		return nil
	}

	title := subtlePill("quiet", "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render("Nothing is moving right now")
	body := "This lane is idle. Stanley would be doing the crossword. Use the quiet moment to recover context, choose the next conversation, or give the team a sharper direction."
	extra := []string{"/switcher for active work · /recover for recap · /search to jump directly"}
	if strings.TrimSpace(focusSlug) != "" {
		title = subtlePill("idle", "#E2E8F0", "#334155") + " " + lipgloss.NewStyle().Bold(true).Render(displayName(focusSlug)+" is waiting for direction")
		body = "This direct session is idle. Ask for a plan, request a review pass, or drop in a concrete decision to unlock the next move. Unlike Jim, this agent is not pranking anyone."
		extra = []string{"Try: give one clear goal, ask for a brief, or request a tradeoff decision"}
	}

	lines := []renderedLine{
		{Text: ""},
		{Text: renderDateSeparator(contentWidth, "Wait state")},
	}
	for _, line := range renderRuntimeEventCard(contentWidth, title, body, "#475569", extra) {
		lines = append(lines, renderedLine{Text: line})
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
