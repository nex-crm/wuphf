package channelui

import (
	"fmt"
	"strings"
	"time"
)

// MaxInt returns the larger of two ints.
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ClampScroll keeps a scroll offset inside the addressable range for a
// buffer of total lines viewed through a viewHeight-row window. Negative
// scrolls clamp to 0; scrolls past the end clamp to total-viewHeight.
func ClampScroll(total, viewHeight, scroll int) int {
	if scroll < 0 {
		return 0
	}
	maxScroll := total - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		return maxScroll
	}
	return scroll
}

// OverlayBottomLines paints the overlay slice over the trailing rows of
// base and returns the result. If base is shorter than overlay, the
// front of overlay is dropped. base is never mutated.
func OverlayBottomLines(base, overlay []string) []string {
	if len(base) == 0 || len(overlay) == 0 {
		return base
	}
	out := append([]string(nil), base...)
	start := len(out) - len(overlay)
	if start < 0 {
		start = 0
		overlay = overlay[len(overlay)-len(out):]
	}
	for i, line := range overlay {
		out[start+i] = line
	}
	return out
}

// FindMessageByID returns the broker message with the matching ID, or
// the zero value and false if nothing matches.
func FindMessageByID(messages []BrokerMessage, id string) (BrokerMessage, bool) {
	for _, msg := range messages {
		if msg.ID == id {
			return msg, true
		}
	}
	return BrokerMessage{}, false
}

// ContainsString reports whether items contains target after trimming
// surrounding whitespace from each candidate (so " fe " matches "fe").
func ContainsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

// ShortClock extracts an "HH:MM" substring out of an ISO timestamp.
// Falls through unchanged for inputs that don't fit the pattern (rather
// than returning empty) so partially-malformed timestamps remain
// readable in the UI.
func ShortClock(ts string) string {
	if len(ts) >= 16 && strings.Contains(ts, "T") {
		return ts[11:16]
	}
	return ts
}

// FormatMinutes renders an interval-minutes value, mapping 0 (and
// negative inputs) to "off" so toggled-off schedules read clearly in
// listings.
func FormatMinutes(v int) string {
	if v <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d", v)
}

// FallbackString returns value if non-blank, otherwise fallback.
func FallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

// ParseChannelTime parses one of the broker's accepted timestamp shapes
// (RFC3339 plus two common variants) and returns the parsed time. The
// bool reports whether parsing succeeded.
func ParseChannelTime(ts string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	} {
		if parsed, err := time.Parse(layout, ts); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// SameDay reports whether two times fall on the same calendar date in
// their respective locations (no normalization to UTC).
func SameDay(left, right time.Time) bool {
	ly, lm, ld := left.Date()
	ry, rm, rd := right.Date()
	return ly == ry && lm == rm && ld == rd
}

// PrettyWhen formats a timestamp string for compact "due/follow-up/etc."
// labels. Today renders as "HH:MM", tomorrow as "tomorrow HH:MM", >24h
// past as "Jan 2 15:04", everything else as "Mon 15:04". For "due"
// labels in the past, the output flips to "overdue since …".
func PrettyWhen(ts, prefix string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	parsed, ok := ParseChannelTime(ts)
	if !ok {
		return strings.TrimSpace(prefix + " " + ts)
	}
	now := time.Now()
	label := parsed.Format("Mon 15:04")
	switch {
	case SameDay(parsed, now):
		label = parsed.Format("15:04")
	case SameDay(parsed, now.Add(24*time.Hour)):
		label = "tomorrow " + parsed.Format("15:04")
	case parsed.Before(now.Add(-24 * time.Hour)):
		label = parsed.Format("Jan 2 15:04")
	}
	if parsed.Before(now) && prefix == "due" {
		return "overdue since " + label
	}
	return strings.TrimSpace(prefix + " " + label)
}

// PrettyRelativeTime renders a timestamp as a human relative duration
// ("just now", "5m ago", "3h ago") for recent inputs and falls back to
// "Jan 2 15:04" once we're more than a day away. Unparsable inputs are
// returned unchanged.
func PrettyRelativeTime(ts string) string {
	parsed, ok := ParseChannelTime(ts)
	if !ok {
		return ts
	}
	now := time.Now()
	diff := now.Sub(parsed)
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff/time.Minute))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff/time.Hour))
	default:
		return parsed.Format("Jan 2 15:04")
	}
}

// RenderTimingSummary joins non-empty due/follow-up/reminder/recheck
// labels with " · " separators.
func RenderTimingSummary(dueAt, followUpAt, reminderAt, recheckAt string) string {
	var parts []string
	if label := PrettyWhen(dueAt, "due"); label != "" {
		parts = append(parts, label)
	}
	if label := PrettyWhen(followUpAt, "follow up"); label != "" {
		parts = append(parts, label)
	}
	if label := PrettyWhen(reminderAt, "remind"); label != "" {
		parts = append(parts, label)
	}
	if label := PrettyWhen(recheckAt, "recheck"); label != "" {
		parts = append(parts, label)
	}
	return strings.Join(parts, " · ")
}
