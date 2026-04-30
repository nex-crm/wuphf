package channelui

import "time"

// CloneRenderedLines returns a fresh slice with the same elements as
// lines, or nil for empty input. Used by the render cache to hand out
// snapshots that callers can mutate without affecting the cached
// version. RenderedLine is a value type so a shallow copy is enough.
func CloneRenderedLines(lines []RenderedLine) []RenderedLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]RenderedLine, len(lines))
	copy(out, lines)
	return out
}

// CloneThreadedMessages mirrors CloneRenderedLines for ThreadedMessage
// slices.
func CloneThreadedMessages(items []ThreadedMessage) []ThreadedMessage {
	if len(items) == 0 {
		return nil
	}
	out := make([]ThreadedMessage, len(items))
	copy(out, items)
	return out
}

// RenderTimeBucket returns a coarse "now" timestamp used to scope
// render-cache keys: per-second granularity for direct DMs and the
// office messages app (where freshness matters), per-30-seconds
// elsewhere (where it doesn't).
func RenderTimeBucket(activeApp OfficeApp, direct bool) int64 {
	if direct || activeApp == OfficeAppMessages {
		return time.Now().Unix()
	}
	return time.Now().Unix() / 30
}
