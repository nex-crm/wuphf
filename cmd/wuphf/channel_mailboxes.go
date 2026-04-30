package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func buildInboxLines(messages []channelui.BrokerMessage, requests []channelui.Interview, contentWidth int) []channelui.RenderedLine {
	lines := []channelui.RenderedLine{{Text: channelui.RenderDateSeparator(contentWidth, "Inbox")}}
	if len(requests) == 0 && len(messages) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))
		return append(lines,
			channelui.RenderedLine{Text: ""},
			channelui.RenderedLine{Text: muted.Render("  Nothing is waiting in the inbox lane.")},
			channelui.RenderedLine{Text: muted.Render("  Human asks, CEO guidance, tags, and thread replies will collect here.")},
		)
	}
	if len(requests) > 0 {
		lines = append(lines, channelui.BuildRequestLines(requests, contentWidth)...)
	}
	if len(messages) > 0 {
		if len(lines) > 1 {
			lines = append(lines, channelui.RenderedLine{Text: ""})
		}
		lines = append(lines, channelui.RenderedLine{Text: channelui.RenderDateSeparator(contentWidth, "Inbox messages")})
		lines = append(lines, buildOfficeMessageLines(messages, map[string]bool{}, contentWidth, true, "", 0)...)
	}
	return lines
}

func buildOutboxLines(messages []channelui.BrokerMessage, actions []channelui.Action, contentWidth int) []channelui.RenderedLine {
	lines := []channelui.RenderedLine{{Text: channelui.RenderDateSeparator(contentWidth, "Outbox")}}
	if len(messages) == 0 && len(actions) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))
		return append(lines,
			channelui.RenderedLine{Text: ""},
			channelui.RenderedLine{Text: muted.Render("  Nothing is in the outbox yet.")},
			channelui.RenderedLine{Text: muted.Render("  Agent-authored updates and recent external actions will collect here.")},
		)
	}
	if len(messages) > 0 {
		lines = append(lines, channelui.RenderedLine{Text: channelui.RenderDateSeparator(contentWidth, "Authored messages")})
		lines = append(lines, buildOfficeMessageLines(messages, map[string]bool{}, contentWidth, true, "", 0)...)
	}
	if len(actions) > 0 {
		lines = append(lines, channelui.RenderedLine{Text: ""})
		lines = append(lines, channelui.RenderedLine{Text: channelui.RenderDateSeparator(contentWidth, "Recent actions")})
		for _, action := range actions {
			header := channelui.SubtlePill(channelui.ArtifactClock(action.CreatedAt, time.Time{}), "#E2E8F0", "#0F172A") +
				" " + channelui.ActionStatePill(action.Kind) +
				" " + lipgloss.NewStyle().Bold(true).Render(channelui.FallbackString(action.Summary, strings.ReplaceAll(action.Kind, "_", " ")))
			extra := []string{}
			if actor := strings.TrimSpace(action.Actor); actor != "" {
				extra = append(extra, "@"+actor)
			}
			if source := strings.TrimSpace(action.Source); source != "" {
				extra = append(extra, source)
			}
			for _, line := range channelui.RenderRuntimeEventCard(contentWidth, header, channelui.PrettyRelativeTime(action.CreatedAt), "#1D4ED8", extra) {
				lines = append(lines, channelui.RenderedLine{Text: "  " + line})
			}
		}
	}
	return lines
}
