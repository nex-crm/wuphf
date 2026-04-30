package channelui

import (
	"strings"
	"time"
)

// CountReplies walks the reply tree rooted at parentID and reports the
// total count plus the formatted time of the most recent reply (suitable
// for "Last reply 3:04 PM" labels in the inline thread indicator).
func CountReplies(messages []BrokerMessage, parentID string) (count int, lastReplyTime string) {
	children := BuildReplyChildren(messages)
	var lastTS time.Time

	var walk func(id string)
	walk = func(id string) {
		for _, msg := range children[id] {
			count++
			ts := ParseTimestamp(msg.Timestamp)
			if ts.After(lastTS) {
				lastTS = ts
				lastReplyTime = FormatShortTime(msg.Timestamp)
			}
			walk(msg.ID)
		}
	}

	walk(parentID)
	return count, lastReplyTime
}

// BuildReplyChildren indexes a message slice by ReplyTo so callers can
// walk a thread without rescanning the whole list per node. Messages
// with an empty ReplyTo are skipped (top-level posts have no parent).
func BuildReplyChildren(messages []BrokerMessage) map[string][]BrokerMessage {
	children := make(map[string][]BrokerMessage)
	for _, msg := range messages {
		if strings.TrimSpace(msg.ReplyTo) == "" {
			continue
		}
		children[msg.ReplyTo] = append(children[msg.ReplyTo], msg)
	}
	return children
}

// ParseTimestamp parses an RFC3339 (or RFC3339Nano) string into a
// time.Time, returning the zero value on failure. The broker emits both
// formats depending on the source, so the fallback is load-bearing.
func ParseTimestamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

// FormatShortTime renders a timestamp as "3:04 PM". On parse failure
// the function tries to slice an "HH:MM" substring out of the raw
// string before giving up — this keeps obviously-malformed-but-readable
// timestamps from rendering as empty cells.
func FormatShortTime(timestamp string) string {
	t := ParseTimestamp(timestamp)
	if t.IsZero() {
		if len(timestamp) > 16 {
			return timestamp[11:16]
		}
		return ""
	}
	return t.Format("3:04 PM")
}
