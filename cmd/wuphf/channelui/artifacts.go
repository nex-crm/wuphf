package channelui

import (
	"sort"
	"strings"
	"time"
)

// ArtifactLifecyclePill renders an artifact lifecycle state as a
// colored pill. The four color args are accents that callers pick
// from the surrounding theme: runningColor (default running),
// pendingColor (yellow-ish), failedColor (red-ish), completedColor
// (green-ish). Empty / unknown states fall back to "retained".
func ArtifactLifecyclePill(state, runningColor, pendingColor, failedColor, completedColor string) string {
	label := strings.ReplaceAll(FallbackString(strings.TrimSpace(state), "retained"), "_", " ")
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked", "failed", "canceled", "cancelled":
		return AccentPill(label, failedColor)
	case "pending", "review", "started":
		return SubtlePill(label, "#FEF3C7", pendingColor)
	case "completed":
		return SubtlePill(label, "#DCFCE7", completedColor)
	default:
		return SubtlePill(label, "#DBEAFE", runningColor)
	}
}

// ArtifactAccentColor returns the artifact card border color matching
// state — red for blocked/failed/canceled, amber for
// pending/review/started, green for completed, fallback otherwise.
func ArtifactAccentColor(state, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked", "failed", "canceled", "cancelled":
		return "#B91C1C"
	case "pending", "review", "started":
		return "#B45309"
	case "completed":
		return "#15803D"
	default:
		return fallback
	}
}

// ParseArtifactTimestamp tries to parse primary first, then fallback,
// returning the first parseable time or the zero time when both fail
// or are empty. Strings are trimmed before parsing.
func ParseArtifactTimestamp(primary, fallback string) time.Time {
	for _, candidate := range []string{strings.TrimSpace(primary), strings.TrimSpace(fallback)} {
		if candidate == "" {
			continue
		}
		if ts, ok := ParseChannelTime(candidate); ok {
			return ts
		}
	}
	return time.Time{}
}

// RecentHumanArtifactRequests filters requests to the human-decision
// kinds (approval / confirm / choice / interview), sorts by CreatedAt
// newest-first, and caps to limit. limit <= 0 keeps all matches.
func RecentHumanArtifactRequests(requests []Interview, limit int) []Interview {
	filtered := make([]Interview, 0, len(requests))
	for _, req := range requests {
		kind := strings.TrimSpace(req.Kind)
		switch kind {
		case "approval", "confirm", "choice", "interview":
			filtered = append(filtered, req)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, lok := ParseChannelTime(filtered[i].CreatedAt)
		right, rok := ParseChannelTime(filtered[j].CreatedAt)
		switch {
		case lok && rok:
			return left.After(right)
		case lok:
			return true
		case rok:
			return false
		default:
			return filtered[i].ID > filtered[j].ID
		}
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// RecentExecutionArtifactActions returns up to limit actions whose
// Kind starts with request_/external_/interrupt_/human_, in
// newest-first order.
func RecentExecutionArtifactActions(actions []Action, limit int) []Action {
	filtered := make([]Action, 0, len(actions))
	for _, action := range actions {
		kind := strings.TrimSpace(action.Kind)
		if strings.HasPrefix(kind, "request_") || strings.HasPrefix(kind, "external_") || strings.HasPrefix(kind, "interrupt_") || strings.HasPrefix(kind, "human_") {
			filtered = append(filtered, action)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := append([]Action(nil), filtered...)
	reverseAny(out)
	return out
}

// ArtifactClock returns a short HH:MM time label — preferring the
// timestamp string parsed via ShortClock, then a local-formatted
// fallback time, finally the literal "artifact" when neither is
// available.
func ArtifactClock(timestamp string, fallback time.Time) string {
	if clock := strings.TrimSpace(ShortClock(timestamp)); clock != "" {
		return clock
	}
	if !fallback.IsZero() {
		return fallback.Local().Format("15:04")
	}
	return "artifact"
}

// ArtifactTime returns timestamp when non-empty, else fallback in
// RFC3339, else "". Used as the canonical timestamp string when
// emitting artifacts.
func ArtifactTime(timestamp string, fallback time.Time) string {
	if strings.TrimSpace(timestamp) != "" {
		return timestamp
	}
	if !fallback.IsZero() {
		return fallback.Format(time.RFC3339)
	}
	return ""
}
