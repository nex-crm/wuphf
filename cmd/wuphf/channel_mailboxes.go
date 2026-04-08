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

func filterMessagesForViewerScope(messages []brokerMessage, viewerSlug, scope string) []brokerMessage {
	scope = normalizeMailboxScope(scope)
	if scope == "" {
		return append([]brokerMessage(nil), messages...)
	}
	index := make(map[string]brokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			index[id] = msg
		}
	}
	filtered := make([]brokerMessage, 0, len(messages))
	for _, msg := range messages {
		if mailboxMessageMatchesViewerScope(msg, viewerSlug, scope, index) {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func normalizeMailboxScope(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "inbox", "outbox", "agent":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func mailboxMessageMatchesViewerScope(msg brokerMessage, viewerSlug, scope string, messagesByID map[string]brokerMessage) bool {
	switch normalizeMailboxScope(scope) {
	case "inbox":
		return mailboxMessageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
	case "outbox":
		return mailboxMessageBelongsToViewerOutbox(msg, viewerSlug)
	case "agent":
		return mailboxMessageBelongsToViewerOutbox(msg, viewerSlug) || mailboxMessageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
	default:
		return true
	}
}

func mailboxMessageBelongsToViewerOutbox(msg brokerMessage, viewerSlug string) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	return viewerSlug != "" && strings.TrimSpace(msg.From) == viewerSlug
}

func mailboxMessageBelongsToViewerInbox(msg brokerMessage, viewerSlug string, messagesByID map[string]brokerMessage) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	if viewerSlug == "" {
		return false
	}
	from := strings.TrimSpace(msg.From)
	switch from {
	case viewerSlug:
		return false
	case "you", "human", "ceo":
		return true
	}
	for _, tagged := range msg.Tagged {
		tagged = strings.TrimSpace(tagged)
		if tagged == viewerSlug || tagged == "all" {
			return true
		}
	}
	return mailboxMessageRepliesToViewerThread(msg, viewerSlug, messagesByID)
}

func mailboxMessageRepliesToViewerThread(msg brokerMessage, viewerSlug string, messagesByID map[string]brokerMessage) bool {
	replyTo := strings.TrimSpace(msg.ReplyTo)
	if replyTo == "" || viewerSlug == "" {
		return false
	}
	seen := map[string]bool{}
	for replyTo != "" {
		if seen[replyTo] {
			return false
		}
		seen[replyTo] = true
		parent, ok := messagesByID[replyTo]
		if !ok {
			return false
		}
		if strings.TrimSpace(parent.From) == viewerSlug {
			return true
		}
		replyTo = strings.TrimSpace(parent.ReplyTo)
	}
	return false
}
