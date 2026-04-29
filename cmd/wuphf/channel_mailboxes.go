package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func buildInboxLines(messages []brokerMessage, requests []channelInterview, contentWidth int) []renderedLine {
	lines := []renderedLine{{Text: renderDateSeparator(contentWidth, "Inbox")}}
	if len(requests) == 0 && len(messages) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
		return append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: muted.Render("  Nothing is waiting in the inbox lane.")},
			renderedLine{Text: muted.Render("  Human asks, CEO guidance, tags, and thread replies will collect here.")},
		)
	}
	if len(requests) > 0 {
		lines = append(lines, buildRequestLines(requests, contentWidth)...)
	}
	if len(messages) > 0 {
		if len(lines) > 1 {
			lines = append(lines, renderedLine{Text: ""})
		}
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Inbox messages")})
		lines = append(lines, buildOfficeMessageLines(messages, map[string]bool{}, contentWidth, true, "", 0)...)
	}
	return lines
}

func buildOutboxLines(messages []brokerMessage, actions []channelAction, contentWidth int) []renderedLine {
	lines := []renderedLine{{Text: renderDateSeparator(contentWidth, "Outbox")}}
	if len(messages) == 0 && len(actions) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
		return append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: muted.Render("  Nothing is in the outbox yet.")},
			renderedLine{Text: muted.Render("  Agent-authored updates and recent external actions will collect here.")},
		)
	}
	if len(messages) > 0 {
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Authored messages")})
		lines = append(lines, buildOfficeMessageLines(messages, map[string]bool{}, contentWidth, true, "", 0)...)
	}
	if len(actions) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Recent actions")})
		for _, action := range actions {
			header := subtlePill(artifactClock(action.CreatedAt, time.Time{}), "#E2E8F0", "#0F172A") +
				" " + actionStatePill(action.Kind) +
				" " + lipgloss.NewStyle().Bold(true).Render(fallbackString(action.Summary, strings.ReplaceAll(action.Kind, "_", " ")))
			extra := []string{}
			if actor := strings.TrimSpace(action.Actor); actor != "" {
				extra = append(extra, "@"+actor)
			}
			if source := strings.TrimSpace(action.Source); source != "" {
				extra = append(extra, source)
			}
			for _, line := range renderRuntimeEventCard(contentWidth, header, prettyRelativeTime(action.CreatedAt), "#1D4ED8", extra) {
				lines = append(lines, renderedLine{Text: "  " + line})
			}
		}
	}
	return lines
}
