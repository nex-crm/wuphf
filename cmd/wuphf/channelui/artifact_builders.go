package channelui

import (
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// RuntimeArtifactSnapshot is a thin holder for the recent execution
// artifacts surfaced by the artifacts view. Snapshot carries the
// items in newest-first display order; Filter / Count select by
// kind without mutating the underlying slice.
type RuntimeArtifactSnapshot struct {
	Items []team.RuntimeArtifact
}

// Count returns the number of items in the snapshot whose Kind
// matches one of the supplied kinds. Empty kinds slice returns
// the total length.
func (s RuntimeArtifactSnapshot) Count(kinds ...team.RuntimeArtifactKind) int {
	return len(s.Filter(kinds...))
}

// Filter returns a copy of the snapshot's items whose Kind matches
// one of the supplied kinds. Empty kinds slice returns a defensive
// copy of every item.
func (s RuntimeArtifactSnapshot) Filter(kinds ...team.RuntimeArtifactKind) []team.RuntimeArtifact {
	if len(kinds) == 0 {
		return append([]team.RuntimeArtifact(nil), s.Items...)
	}
	set := make(map[team.RuntimeArtifactKind]struct{}, len(kinds))
	for _, kind := range kinds {
		set[kind] = struct{}{}
	}
	out := make([]team.RuntimeArtifact, 0, len(s.Items))
	for _, artifact := range s.Items {
		if _, ok := set[artifact.Kind]; ok {
			out = append(out, artifact)
		}
	}
	return out
}

// RecentArtifactTasks filters out empty rows, sorts by latest
// updated/created timestamp (newest first, with a stable ID
// tiebreaker for unparseable timestamps), and clamps to limit.
// Limit <= 0 keeps all rows.
func RecentArtifactTasks(tasks []Task, limit int) []Task {
	filtered := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) == "" && strings.TrimSpace(task.Title) == "" {
			continue
		}
		filtered = append(filtered, task)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left := ParseArtifactTimestamp(filtered[i].UpdatedAt, filtered[i].CreatedAt)
		right := ParseArtifactTimestamp(filtered[j].UpdatedAt, filtered[j].CreatedAt)
		switch {
		case !left.IsZero() && !right.IsZero():
			return left.After(right)
		case !left.IsZero():
			return true
		case !right.IsZero():
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

// BuildRequestRuntimeArtifact projects a pending Interview into
// the team.RuntimeArtifact shape used by the artifacts view.
// Title falls through to question; the artifact is marked Blocking
// when the request is blocking or required.
func BuildRequestRuntimeArtifact(req Interview) team.RuntimeArtifact {
	state := NormalizeRequestArtifactState(req.Status)
	return team.RuntimeArtifact{
		ID:         strings.TrimSpace(req.ID),
		Kind:       team.RuntimeArtifactRequest,
		Title:      req.TitleOrQuestion(),
		Summary:    FallbackString(strings.TrimSpace(req.Context), strings.TrimSpace(req.Question)),
		State:      state,
		Progress:   RequestArtifactProgress(req),
		Owner:      strings.TrimSpace(req.From),
		Channel:    strings.TrimSpace(req.Channel),
		RelatedID:  strings.TrimSpace(req.ReplyTo),
		StartedAt:  strings.TrimSpace(req.CreatedAt),
		UpdatedAt:  LatestArtifactTimestamp(req.FollowUpAt, req.ReminderAt, req.RecheckAt, req.DueAt, req.CreatedAt),
		ResumeHint: "Answer the request or reopen it from Recovery.",
		ReviewHint: RequestArtifactReviewHint(req),
		Blocking:   req.Blocking || req.Required,
	}
}

// BuildActionRuntimeArtifact projects an Action into the
// team.RuntimeArtifact shape, classifying external_*-prefixed
// kinds as RuntimeArtifactExternalAction (vs the default
// RuntimeArtifactHumanAction).
func BuildActionRuntimeArtifact(action Action) team.RuntimeArtifact {
	kind := team.RuntimeArtifactHumanAction
	if strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
		kind = team.RuntimeArtifactExternalAction
	}
	title := strings.TrimSpace(action.Summary)
	if title == "" {
		title = strings.ReplaceAll(strings.TrimSpace(action.Kind), "_", " ")
	}
	return team.RuntimeArtifact{
		ID:         strings.TrimSpace(action.ID),
		Kind:       kind,
		Title:      title,
		Summary:    ActionArtifactSummary(action),
		State:      NormalizeActionArtifactState(action.Kind),
		Progress:   ActionArtifactProgress(action),
		Owner:      strings.TrimSpace(action.Actor),
		Channel:    strings.TrimSpace(action.Channel),
		RelatedID:  FallbackString(strings.TrimSpace(action.RelatedID), strings.TrimSpace(action.DecisionID)),
		StartedAt:  strings.TrimSpace(action.CreatedAt),
		UpdatedAt:  strings.TrimSpace(action.CreatedAt),
		ResumeHint: ActionArtifactResumeHint(action),
		ReviewHint: strings.TrimSpace(action.Source),
	}
}

// RequestArtifactProgress builds the optional progress strip for a
// request: recommended option + due / follow-up timestamps. Each
// component is gated on a non-empty trimmed value.
func RequestArtifactProgress(req Interview) string {
	parts := make([]string, 0, 3)
	if choice := strings.TrimSpace(req.RecommendedID); choice != "" {
		parts = append(parts, "Recommended: "+choice)
	}
	if due := strings.TrimSpace(req.DueAt); due != "" {
		parts = append(parts, "Due "+PrettyRelativeTime(due))
	}
	if followUp := strings.TrimSpace(req.FollowUpAt); followUp != "" {
		parts = append(parts, "Follow-up "+PrettyRelativeTime(followUp))
	}
	return strings.Join(parts, " · ")
}

// RequestArtifactReviewHint surfaces the most actionable review
// hint for a request: the recommendation when present, otherwise
// the due-date hint.
func RequestArtifactReviewHint(req Interview) string {
	if recommended := strings.TrimSpace(req.RecommendedID); recommended != "" {
		return "Review recommendation " + recommended + " before answering."
	}
	if due := strings.TrimSpace(req.DueAt); due != "" {
		return "Due " + PrettyRelativeTime(due)
	}
	return ""
}

// NormalizeRequestArtifactState canonicalizes a request status into
// one of the artifact-state strings: pending / completed /
// canceled. Unrecognized statuses pass through (lower-cased).
func NormalizeRequestArtifactState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "open":
		return "pending"
	case "answered", "complete", "completed":
		return "completed"
	case "canceled", "cancelled":
		return "canceled"
	default:
		return strings.TrimSpace(strings.ToLower(status))
	}
}

// ActionArtifactSummary renders the channel + actor + relative
// timestamp summary for an action artifact, falling back to a
// generic message when none of the parts are populated.
func ActionArtifactSummary(action Action) string {
	parts := make([]string, 0, 4)
	if channel := strings.TrimSpace(action.Channel); channel != "" {
		parts = append(parts, "#"+channel)
	}
	if actor := strings.TrimSpace(action.Actor); actor != "" {
		parts = append(parts, "@"+actor)
	}
	if when := strings.TrimSpace(PrettyRelativeTime(action.CreatedAt)); when != "" {
		parts = append(parts, when)
	}
	if len(parts) == 0 {
		return "Retained action trace."
	}
	return strings.Join(parts, " · ")
}

// ActionArtifactProgress surfaces the action's source (when set)
// as a "Source: …" progress line, or "" when no source is
// available.
func ActionArtifactProgress(action Action) string {
	if source := strings.TrimSpace(action.Source); source != "" {
		return "Source: " + source
	}
	return ""
}

// ActionArtifactResumeHint picks the most useful resume hint for
// an action: a related artifact when set, then a related decision,
// otherwise a generic prompt to review the thread / provider.
func ActionArtifactResumeHint(action Action) string {
	if related := strings.TrimSpace(action.RelatedID); related != "" {
		return "Review the related artifact or thread " + related + "."
	}
	if decision := strings.TrimSpace(action.DecisionID); decision != "" {
		return "Review decision " + decision + " or reopen the related thread."
	}
	return "Review the related thread or action provider details."
}

// NormalizeActionArtifactState classifies an action's kind into
// one of failed / canceled / blocked / running / completed via
// substring matching on common kind suffixes.
func NormalizeActionArtifactState(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(kind, "failed"), strings.Contains(kind, "error"):
		return "failed"
	case strings.Contains(kind, "canceled"), strings.Contains(kind, "cancelled"):
		return "canceled"
	case strings.Contains(kind, "blocked"), strings.Contains(kind, "waiting"), strings.Contains(kind, "follow_up"):
		return "blocked"
	case strings.Contains(kind, "planned"), strings.Contains(kind, "created"), strings.Contains(kind, "received"), strings.Contains(kind, "started"):
		return "running"
	case strings.Contains(kind, "answered"), strings.Contains(kind, "executed"), strings.Contains(kind, "completed"), strings.Contains(kind, "sent"):
		return "completed"
	default:
		return FallbackString(kind, "running")
	}
}

// LatestArtifactTimestamp picks the latest parseable timestamp
// from the supplied candidates and returns it as RFC3339. Returns
// "" when none of the candidates parse.
func LatestArtifactTimestamp(candidates ...string) string {
	var latest time.Time
	for _, candidate := range candidates {
		if ts, ok := ParseChannelTime(candidate); ok && ts.After(latest) {
			latest = ts
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.Format(time.RFC3339)
}
