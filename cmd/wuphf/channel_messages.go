package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// messageGroup represents consecutive messages from the same sender within a
// 5-minute window, rendered as a single visual block (Slack-style).
type messageGroup struct {
	From      string
	Messages  []brokerMessage
	StartTime time.Time
}

// groupMessages groups consecutive same-sender messages within 5 minutes.
// Messages with different senders or gaps > 5 minutes start a new group.
func groupMessages(messages []brokerMessage) []messageGroup {
	if len(messages) == 0 {
		return nil
	}

	var groups []messageGroup
	var current *messageGroup

	for _, msg := range messages {
		ts := parseTimestamp(msg.Timestamp)

		if current == nil ||
			msg.From != current.From ||
			ts.Sub(current.StartTime) > 5*time.Minute {
			// Start a new group
			groups = append(groups, messageGroup{
				From:      msg.From,
				Messages:  []brokerMessage{msg},
				StartTime: ts,
			})
			current = &groups[len(groups)-1]
		} else {
			current.Messages = append(current.Messages, msg)
		}
	}

	return groups
}

// renderMessageGroups renders grouped messages in Slack style:
//   - Name (bold, agent color) left-aligned, timestamp right-aligned on same line
//   - Continuation messages: indented, no name repeat
//   - Thread indicator on root messages with replies
//   - Date separators centered
//   - [STATUS] messages: compact italic
func renderMessageGroups(
	groups []messageGroup,
	width int,
	agentColors map[string]string,
	replyCountFn func(string) (int, string),
) []string {
	if width < 32 {
		width = 32
	}

	timestampStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#616164"))
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Italic(true)
	threadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#616164"))

	var lines []string
	var lastDate string

	for _, group := range groups {
		// Date separator when day changes
		dateLabel := dateLabelFromTime(group.StartTime)
		if dateLabel != lastDate && dateLabel != "" {
			lines = append(lines, renderDateSeparator(width, dateLabel))
			lastDate = dateLabel
		}

		color := agentColors[group.From]
		if color == "" {
			color = "#9CA3AF"
		}
		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true)

		for i, msg := range group.Messages {
			ts := formatShortTime(msg.Timestamp)

			// [STATUS] messages: compact italic, no grouping chrome
			if strings.HasPrefix(msg.Content, "[STATUS]") {
				status := strings.TrimPrefix(msg.Content, "[STATUS] ")
				lines = appendWrapped(lines, width,
					fmt.Sprintf("  %s  %s %s",
						timestampStyle.Render(ts),
						nameStyle.Render("@"+msg.From),
						statusStyle.Render("is "+status),
					),
				)
				continue
			}

			if i == 0 {
				// First message in group: show name + timestamp header
				name := displayName(msg.From)
				nameRendered := nameStyle.Render(name)
				tsRendered := timestampStyle.Render(ts)

				// Name left, timestamp right on the same line
				nameWidth := lipgloss.Width(nameRendered)
				tsWidth := lipgloss.Width(tsRendered)
				gap := width - nameWidth - tsWidth - 4 // 2 padding each side
				if gap < 2 {
					gap = 2
				}
				lines = append(lines, "")
				lines = append(lines, fmt.Sprintf("  %s%s%s",
					nameRendered,
					strings.Repeat(" ", gap),
					tsRendered,
				))
			}

			// Message content — first message at indent 2, continuations same
			prefix := "  "

			// Render A2UI blocks if present
			textPart, a2uiRendered := renderA2UIBlocks(msg.Content, width-4)

			for _, paragraph := range strings.Split(textPart, "\n") {
				paragraph = highlightMentions(paragraph, agentColors)
				lines = appendWrapped(lines, width, prefix+paragraph)
			}
			if a2uiRendered != "" {
				for _, renderedLine := range strings.Split(a2uiRendered, "\n") {
					lines = append(lines, prefix+renderedLine)
				}
			}

			// Thread indicator: only on the first message of the group
			// (which is typically the root message that has replies)
			if i == 0 && replyCountFn != nil {
				count, lastReply := replyCountFn(msg.ID)
				if count > 0 {
					replyWord := "reply"
					if count != 1 {
						replyWord = "replies"
					}
					indicator := fmt.Sprintf("  %s %d %s  Last reply %s",
						threadStyle.Render("↩"),
						count,
						replyWord,
						lastReply,
					)
					lines = append(lines, threadStyle.Render(indicator))
				}
			}
		}
	}

	return lines
}

// countReplies counts replies for a given parentID from the full message list,
// returning the count and the formatted time of the last reply.
func countReplies(messages []brokerMessage, parentID string) (count int, lastReplyTime string) {
	children := buildReplyChildren(messages)
	var lastTS time.Time

	var walk func(id string)
	walk = func(id string) {
		for _, msg := range children[id] {
			count++
			ts := parseTimestamp(msg.Timestamp)
			if ts.After(lastTS) {
				lastTS = ts
				lastReplyTime = formatShortTime(msg.Timestamp)
			}
			walk(msg.ID)
		}
	}

	walk(parentID)
	return count, lastReplyTime
}

func buildReplyChildren(messages []brokerMessage) map[string][]brokerMessage {
	children := make(map[string][]brokerMessage)
	for _, msg := range messages {
		if strings.TrimSpace(msg.ReplyTo) == "" {
			continue
		}
		children[msg.ReplyTo] = append(children[msg.ReplyTo], msg)
	}
	return children
}

// parseTimestamp parses an RFC3339 timestamp string, returning zero time on failure.
func parseTimestamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Try RFC3339Nano as fallback
		t, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

// formatShortTime extracts a short HH:MM AM/PM time from a timestamp string.
func formatShortTime(timestamp string) string {
	t := parseTimestamp(timestamp)
	if t.IsZero() {
		// Fallback: try to extract raw HH:MM from the string
		if len(timestamp) > 16 {
			return timestamp[11:16]
		}
		return ""
	}
	return t.Format("3:04 PM")
}

// dateLabelFromTime returns "Today", "Yesterday", or a formatted date string.
func dateLabelFromTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	msgDate := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	switch {
	case msgDate.Equal(today):
		return "Today"
	case msgDate.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	default:
		return t.Format("Monday, January 2")
	}
}
