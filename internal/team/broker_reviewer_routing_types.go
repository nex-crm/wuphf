package team

// broker_reviewer_routing_types.go declares the type surface for Lane D's
// reviewer-routing layer: the per-officeMember Watching set, the typed
// signals the broker extracts from a task before intersecting with that
// set, and the ReviewerGrade / Severity wire types Lane D needs to call
// into Lane C's Decision Packet mutator.
//
// Coordination with Lane C (parallel build): Lane C owns the Decision
// Packet model and persistence. Lane D needs only two contract points
// from Lane C — the Severity constant set (so the timeout filler can
// stamp SeveritySkipped) and the AppendReviewerGrade mutator signature
// (so the timeout filler can populate a missing slot). Both live here
// as the Lane-D-side contract until Lane C lands; Lane C must match
// these types exactly when it merges, or the convergence path breaks.
//
// Why these live in their own file: keeping them out of broker_types.go
// preserves the "broker_types.go is the persisted-wire-shape file" rule.
// Watching and ReviewerRoutingSignals are not persisted as standalone
// records (Watching is a sub-field of officeMember; signals are
// transient). The grade types are persisted by Lane C, but Lane C will
// move them into broker_decision_packet.go when it ships, leaving this
// file as the watching-set + signals carrier.

// Watching captures the four glob/tag categories an officeMember can
// declare interest in. Used by the broker's reviewer-routing logic to
// auto-assign agents to a task entering review.
//
// Each list is matched independently — an empty list means "do not
// match on this category", not "match everything". An agent is
// auto-assigned when ANY of its non-empty categories intersects with
// the task's signals; this matches CODEOWNERS-style "any line touching
// my path" semantics rather than requiring all categories to overlap
// (which would make multi-category Watching sets effectively unusable).
type Watching struct {
	// Files is the list of glob patterns matched against the output of
	// `git diff --name-only` between the task's worktree and the task's
	// parent branch. Patterns use Go's path.Match semantics
	// (forward-slash separator across all platforms; no globstar — `**`
	// is NOT expanded recursively). Each entry must be a valid glob —
	// invalid entries are dropped at match time and logged.
	Files []string `json:"files,omitempty"`

	// WikiPaths is the list of glob patterns matched against the
	// wiki-relative paths that appear in the diff (i.e. the same
	// `git diff --name-only` output, filtered to entries under the
	// repo's wiki root). Same path.Match semantics as Files.
	WikiPaths []string `json:"wiki_paths,omitempty"`

	// ToolNames is matched against the union of HeadlessEvent.ToolCalls
	// observed for the task across all manifest events on its agent
	// stream. Comparison is exact-match on the tool name (no globs);
	// tool names are short canonical identifiers.
	//
	// ToolNames was substituted for an earlier SkillDomains proposal
	// because skill-domain tags are not currently recorded in the
	// HeadlessEvent pipeline. Adding skill-domain recording is v1.1.
	ToolNames []string `json:"tool_names,omitempty"`

	// TaskTags is matched against teamTask.Tags. Comparison is
	// exact-match (no globs); tags are short canonical identifiers
	// that the spec author or owner agent attaches to a task.
	TaskTags []string `json:"task_tags,omitempty"`
}

// IsEmpty reports whether the watching set has no patterns at all. Used
// to skip agents that haven't declared any interest.
func (w Watching) IsEmpty() bool {
	return len(w.Files) == 0 && len(w.WikiPaths) == 0 && len(w.ToolNames) == 0 && len(w.TaskTags) == 0
}

// ReviewerRoutingSignals is the deterministic snapshot of a task's
// state that the routing logic intersects against each agent's
// Watching set. Built by extractRoutingSignalsLocked from on-disk diff
// output, agent-stream manifest events, and the task's own Tags field.
//
// Kept as an explicit struct (rather than passing four slices) so the
// test surface can construct a synthetic signal set without spinning up
// a worktree or stream buffer.
type ReviewerRoutingSignals struct {
	Files     []string // diff paths from `git diff --name-only`
	WikiPaths []string // subset of Files under the wiki root
	ToolNames []string // unique tool names observed across the task's manifest events
	TaskTags  []string // verbatim copy of teamTask.Tags
}

// Severity and ReviewerGrade live in broker_decision_packet_types.go
// (Lane C). Lane D consumes them as-is for the timeout filler and
// convergence rule. SubmittedAt is time.Time per the design doc.

// reviewConvergenceTickInterval is the cadence at which the broker's
// background sweeper re-evaluates every review-state task. 30s matches
// the design doc; the sweeper is intentionally cheap (only iterates
// the review bucket of the lifecycle index, never the full task list).
const reviewConvergenceTickInterval = 30 // seconds

// reviewConvergenceDefaultTimeoutSeconds is the default cap on how
// long the broker waits for all assigned reviewers before filling
// missing slots with SeveritySkipped and transitioning to decision.
// teamTask.ReviewTimeoutSeconds overrides this on a per-task basis.
const reviewConvergenceDefaultTimeoutSeconds = 600 // 10 minutes
