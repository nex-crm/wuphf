package main

import (
	"strings"
	"time"
)

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
