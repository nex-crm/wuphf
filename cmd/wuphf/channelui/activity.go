package channelui

import (
	"fmt"
	"strings"
)

// TaskStatusLine returns a one-line description of a task derived
// from its status: "Working on …" / "Reviewing …" / "Blocked on …" /
// "Queued: …", or just the title for unknown statuses. Returns ""
// for empty title.
func TaskStatusLine(task Task) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		return "Working on " + title
	case "review":
		return "Reviewing " + title
	case "blocked":
		return "Blocked on " + title
	case "claimed", "pending", "open":
		return "Queued: " + title
	default:
		return title
	}
}

// SummarizeLiveActivity scans raw bottom-up for the last
// non-trivial line and returns its sanitized form (see
// SanitizeActivityLine). Used to render a member's current Claude
// Code activity from a tmux pane snapshot.
func SummarizeLiveActivity(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := SanitizeActivityLine(lines[i])
		if line == "" {
			continue
		}
		return line
	}
	return ""
}

// SanitizeActivityLine maps a raw tmux pane line to a friendly
// summary phrase ("Searching the codebase", "Running tests", etc.)
// when the line looks like a known tool invocation. Lines that look
// like UI chrome (permissions banner, prompt prefix) return "". Lines
// that don't match a known shape fall through to SummarizeSentence.
func SanitizeActivityLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "shift+tab"),
		strings.Contains(lower, "permissions"),
		strings.Contains(lower, "bypass"),
		strings.HasPrefix(line, "❯"),
		strings.HasPrefix(line, "─"),
		strings.HasPrefix(line, "━"):
		return ""
	case strings.Contains(lower, "rg "),
		strings.Contains(lower, "grep "),
		strings.Contains(lower, "search"):
		return "Searching the codebase"
	case strings.Contains(lower, "read "),
		strings.Contains(lower, "open "),
		strings.Contains(lower, "inspect"):
		return "Reading files"
	case strings.Contains(lower, "go test"),
		strings.Contains(lower, "npm test"),
		strings.Contains(lower, "pytest"):
		return "Running tests"
	case strings.Contains(lower, "go build"),
		strings.Contains(lower, "npm run build"),
		strings.Contains(lower, "bun run build"):
		return "Building the project"
	case strings.Contains(lower, "curl "),
		strings.Contains(lower, "http://"),
		strings.Contains(lower, "https://"):
		return "Calling an external system"
	}
	return SummarizeSentence(line)
}

// SummarizeSentence collapses newlines + surrounding quotes, then
// truncates to 88 characters with a "..." continuation.
func SummarizeSentence(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	text = strings.Trim(text, "\"")
	text = strings.TrimSpace(text)
	if len(text) <= 88 {
		return text
	}
	return text[:85] + "..."
}

// BlockedWorkTasks returns up to limit blocked tasks, optionally
// scoped to focusSlug as the owner. limit <= 0 keeps all matches.
func BlockedWorkTasks(tasks []Task, focusSlug string, limit int) []Task {
	filtered := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if !strings.EqualFold(strings.TrimSpace(task.Status), "blocked") {
			continue
		}
		if strings.TrimSpace(focusSlug) != "" && strings.TrimSpace(task.Owner) != strings.TrimSpace(focusSlug) {
			continue
		}
		filtered = append(filtered, task)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// RecentDirectExecutionActions returns up to limit external_*
// actions in newest-first order, optionally scoped to focusSlug
// (matching the action.Actor — actions actored by "scheduler" or
// with no actor always pass).
func RecentDirectExecutionActions(actions []Action, focusSlug string, limit int) []Action {
	var filtered []Action
	for _, action := range actions {
		if !strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
			continue
		}
		actor := strings.TrimSpace(action.Actor)
		if focusSlug != "" && actor != "" && actor != focusSlug && actor != "scheduler" {
			continue
		}
		filtered = append(filtered, action)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := append([]Action(nil), filtered...)
	reverseAny(out)
	return out
}

// ExecutionMetaLine joins source, actor, related ID, and a relative
// timestamp with " · " for the "Recent actions" strip body.
func ExecutionMetaLine(action Action) string {
	parts := []string{}
	if source := strings.TrimSpace(action.Source); source != "" {
		parts = append(parts, source)
	}
	if actor := strings.TrimSpace(action.Actor); actor != "" {
		parts = append(parts, "@"+actor)
	}
	if related := strings.TrimSpace(action.RelatedID); related != "" {
		parts = append(parts, related)
	}
	if when := strings.TrimSpace(action.CreatedAt); when != "" {
		parts = append(parts, PrettyRelativeTime(when))
	}
	return strings.Join(parts, " · ")
}

// LatestRelevantAction finds the most recent external_* action whose
// actor matches slug (or is "scheduler" / empty). Returns the action
// and true on hit, the zero Action and false on miss.
func LatestRelevantAction(actions []Action, slug string) (Action, bool) {
	slug = strings.TrimSpace(slug)
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		if !strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
			continue
		}
		actor := strings.TrimSpace(action.Actor)
		if actor != "" && actor != slug && actor != "scheduler" {
			continue
		}
		return action, true
	}
	return Action{}, false
}

// DescribeActionState renders a runtime-strip phrase summarizing
// where an external action sits in its lifecycle: failed / dry-run
// ready / scheduled / listening / completed / generic summary.
func DescribeActionState(action Action) string {
	switch {
	case strings.Contains(action.Kind, "failed"):
		return fmt.Sprintf("last action failed: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "planned"):
		return fmt.Sprintf("dry-run ready: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "scheduled"):
		return fmt.Sprintf("scheduled: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "registered"):
		return fmt.Sprintf("listening: %s", strings.TrimSpace(action.Summary))
	case strings.Contains(action.Kind, "executed"), strings.Contains(action.Kind, "created"):
		return fmt.Sprintf("completed: %s", strings.TrimSpace(action.Summary))
	default:
		return strings.TrimSpace(action.Summary)
	}
}

// ActivityPill maps a MemberActivity label to a colored pill — the
// "blocked" / "reviewing" / "queued" etc. badge shown next to a
// member's name in the runtime strip.
func ActivityPill(act MemberActivity) string {
	switch act.Label {
	case "working", "shipping":
		return AccentPill(act.Label, "#7C3AED")
	case "reviewing":
		return AccentPill(act.Label, "#2563EB")
	case "blocked":
		return AccentPill(act.Label, "#B91C1C")
	case "queued", "plotting":
		return AccentPill(act.Label, "#B45309")
	case "talking":
		return AccentPill(act.Label, "#15803D")
	case "away":
		return SubtlePill(act.Label, "#CBD5E1", "#475569")
	default:
		return SubtlePill(act.Label, "#CBD5E1", "#334155")
	}
}

// ActionStatePill maps an action's Kind to a colored pill — failed
// (red) / planned (blue) / listening (purple) / completed (green) /
// generic neutral pill for anything else (with underscores
// space-separated).
func ActionStatePill(kind string) string {
	switch {
	case strings.Contains(kind, "failed"):
		return AccentPill("failed", "#B91C1C")
	case strings.Contains(kind, "planned"):
		return AccentPill("planned", "#1D4ED8")
	case strings.Contains(kind, "registered"), strings.Contains(kind, "received"):
		return AccentPill("listening", "#7C3AED")
	case strings.Contains(kind, "executed"), strings.Contains(kind, "created"), strings.Contains(kind, "scheduled"):
		return AccentPill("completed", "#15803D")
	default:
		return SubtlePill(strings.ReplaceAll(kind, "_", " "), "#E2E8F0", "#334155")
	}
}
