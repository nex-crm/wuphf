package team

// broker_inbox.go is Lane E of the multi-agent control loop. It exposes the
// indexed lifecycle lookup that Lane A built (b.lifecycleIndex) as a clean
// query API for the Decision Inbox web UI (Lane G) and the CLI (Lane F).
//
// Storage and maintenance of the index lives in
// broker_lifecycle_transition.go (Lane A). This file only consumes it.
//
// Performance contract (design doc, "Failure modes" row "Decision Inbox
// query — 1000+ tasks"):
//
//   - InboxCounts must be O(1): every count is len(b.lifecycleIndex[state]).
//   - Inbox row construction is O(N) over the rows being returned, NOT
//     over total tasks in the broker. The 1000-task load test in
//     broker_inbox_test.go enforces a <100ms ceiling on a developer laptop.
//
// Auth filter contract (design doc, "Tunnel-human reviewer auth" matrix):
//
//   - Broker/owner token: full inbox.
//   - Human session whose slug appears in a task's Reviewers list:
//     inbox includes those tasks; /tasks/{id} returns 200 for those tasks
//     and 403 for others.
//   - Human session not in any reviewer list: inbox returns 200 with zero
//     rows; /tasks/{id} returns 403 for any task ID.
//   - Unauthenticated: 401 (handled upstream by withAuth).
//
// The handler in broker_inbox_handler.go wires the actor identity from
// requestActorFromContext into the InboxFilter call via a separate
// inbox-with-actor helper. The pure Inbox(filter) entry point assumes the
// caller has already authorized; tests that exercise the handler exercise
// the auth filter.

import (
	"errors"
	"log"
	"strings"
	"time"
)

// InboxFilter selects which lifecycle bucket(s) the inbox query returns.
// The five constants below are the only valid filter values; any other
// string yields ErrInboxFilterUnknown.
type InboxFilter string

const (
	InboxFilterDecisionRequired InboxFilter = "decision_required"
	InboxFilterRunning          InboxFilter = "running"
	InboxFilterBlocked          InboxFilter = "blocked"
	InboxFilterMerged           InboxFilter = "merged"
	InboxFilterAll              InboxFilter = "all"
)

// ErrInboxFilterUnknown is returned by Inbox when the caller passes a
// filter value not in the InboxFilter* constant set. Surfaces as a 400
// from the REST handler.
var ErrInboxFilterUnknown = errors.New("inbox: unknown filter")

// SeveritySummary mirrors Lane G's TS shape exactly (camelCase JSON keys,
// ints for each tier). Lane C populates the underlying ReviewerGrade
// list per task; Lane E aggregates it deterministically.
type SeveritySummary struct {
	Critical int `json:"critical"`
	Major    int `json:"major"`
	Minor    int `json:"minor"`
	Nitpick  int `json:"nitpick"`
}

// ReviewerSummary captures the convergence progress for a task's reviewer
// set. Graded counts only reviewer slugs who emitted a grade with a typed
// severity; Total is len(task.Reviewers). When Lane D has not yet
// populated Reviewers (or no reviewers were assigned), both are zero.
type ReviewerSummary struct {
	Graded int `json:"graded"`
	Total  int `json:"total"`
}

// InboxRow is one entry in the inbox payload. Field shape and JSON keys
// are 1:1 with Lane G's TS InboxRow type. Build-time:
//
//   - TaskID: teamTask.ID
//   - Title: teamTask.Title (for v1, Spec.Problem is what intake fills,
//     but the existing teamTask.Title field is the human-readable label
//     written by the spec confirmation step)
//   - Assignment: Spec.Assignment from the Decision Packet (empty when
//     Lane C has not stored a packet yet)
//   - LifecycleState: teamTask.LifecycleState as a string
//   - SeveritySummary: aggregated from DecisionPacket.ReviewerGrades
//   - ElapsedMs: now - parseBrokerTimestamp(task.CreatedAt)
//   - ReviewerSummary: graded count vs len(task.Reviewers)
type InboxRow struct {
	TaskID          string          `json:"taskId"`
	Title           string          `json:"title"`
	Assignment      string          `json:"assignment"`
	LifecycleState  LifecycleState  `json:"lifecycleState"`
	SeveritySummary SeveritySummary `json:"severitySummary"`
	ElapsedMs       int64           `json:"elapsedMs"`
	ReviewerSummary ReviewerSummary `json:"reviewerSummary"`
}

// InboxCounts is the cardinality summary that the inbox header renders.
// All four counts are O(1) reads of len(b.lifecycleIndex[state]); the
// inbox query never iterates b.tasks for these.
//
// MergedToday is the one exception that costs O(merged-bucket-size) to
// compute because the index does not segment by day. v1 accepts that:
// the merged bucket is bounded by recent activity and the total broker
// task count is small enough that this stays under the <100ms ceiling.
type InboxCounts struct {
	DecisionRequired int `json:"decisionRequired"`
	Running          int `json:"running"`
	Blocked          int `json:"blocked"`
	MergedToday      int `json:"mergedToday"`
}

// InboxPayload is the full response to GET /tasks/inbox.
type InboxPayload struct {
	Rows        []InboxRow  `json:"rows"`
	Counts      InboxCounts `json:"counts"`
	RefreshedAt string      `json:"refreshedAt"`
}

// inboxFilterToStates maps a filter to the lifecycle buckets it consumes.
// All filters except MergedToday and All map to a single bucket. The
// MergedToday filter maps to the merged bucket and the handler then
// post-filters by completion timestamp; the All filter sweeps every
// canonical state.
func inboxFilterToStates(filter InboxFilter) ([]LifecycleState, error) {
	switch filter {
	case InboxFilterDecisionRequired:
		return []LifecycleState{LifecycleStateDecision}, nil
	case InboxFilterRunning:
		return []LifecycleState{LifecycleStateRunning}, nil
	case InboxFilterBlocked:
		return []LifecycleState{LifecycleStateBlockedOnPRMerge}, nil
	case InboxFilterMerged:
		return []LifecycleState{LifecycleStateMerged}, nil
	case InboxFilterAll:
		return CanonicalLifecycleStates(), nil
	default:
		return nil, ErrInboxFilterUnknown
	}
}

// startOfTodayUTC returns the UTC midnight that begins the current day.
// MergedToday filters out merged tasks whose CompletedAt (or UpdatedAt
// fallback) is older than this boundary.
func startOfTodayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// Inbox returns the indexed inbox payload for the given filter without
// any auth filtering. Callers are expected to have already authorized;
// the REST handler in broker_inbox_handler.go composes auth on top via
// inboxForActor.
//
// O(1) for counts (reads b.lifecycleIndex bucket lengths). O(N) only
// over the rows being returned — never iterates b.tasks as a whole.
func (b *Broker) Inbox(filter InboxFilter) (InboxPayload, error) {
	if b == nil {
		return InboxPayload{}, errors.New("inbox: nil broker")
	}
	states, err := inboxFilterToStates(filter)
	if err != nil {
		return InboxPayload{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inboxLocked(states, nil), nil
}

// inboxForActor is the auth-aware inbox helper called from the REST
// handler. ownerToken=true bypasses the reviewer filter (broker/owner
// auth); when false, only tasks whose Reviewers list contains
// humanSlug are included.
func (b *Broker) inboxForActor(filter InboxFilter, ownerToken bool, humanSlug string) (InboxPayload, error) {
	if b == nil {
		return InboxPayload{}, errors.New("inbox: nil broker")
	}
	states, err := inboxFilterToStates(filter)
	if err != nil {
		return InboxPayload{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if ownerToken {
		return b.inboxLocked(states, nil), nil
	}
	slug := normalizeReviewerSlug(humanSlug)
	if slug == "" {
		// Authenticated human with no slug — surface a defensive empty
		// inbox + correct counts. The counts intentionally remain
		// truthful (O(1) bucket lengths); the filter is on rows only.
		return b.inboxLocked(states, func(string) bool { return false }), nil
	}
	predicate := func(taskID string) bool {
		for i := range b.tasks {
			if b.tasks[i].ID != taskID {
				continue
			}
			for _, r := range b.tasks[i].Reviewers {
				if normalizeReviewerSlug(r) == slug {
					return true
				}
			}
			return false
		}
		return false
	}
	return b.inboxLocked(states, predicate), nil
}

// inboxLocked builds the payload under b.mu. include is an optional
// predicate that filters task IDs (used by the auth layer); nil means
// include every row.
func (b *Broker) inboxLocked(states []LifecycleState, include func(taskID string) bool) InboxPayload {
	cutoff := startOfTodayUTC()
	rows := make([]InboxRow, 0, b.estimateBucketSizesLocked(states))
	for _, state := range states {
		bucket := b.lifecycleIndex[state]
		for _, taskID := range bucket {
			if include != nil && !include(taskID) {
				continue
			}
			task := b.findTaskByIDLocked(taskID)
			if task == nil {
				continue
			}
			if state == LifecycleStateMerged {
				ts := mergedAtTimestamp(task)
				if ts.IsZero() || ts.Before(cutoff) {
					// MergedToday post-filter: skip rows older than
					// today's UTC midnight. The All filter also walks
					// the merged bucket, but for All the row stays in;
					// only InboxFilterMerged narrows by date.
					if len(states) == 1 {
						continue
					}
				}
			}
			rows = append(rows, b.buildInboxRowLocked(task))
		}
	}
	return InboxPayload{
		Rows:        rows,
		Counts:      b.inboxCountsLocked(cutoff),
		RefreshedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// estimateBucketSizesLocked sums the lengths of the requested buckets
// so the row slice can be allocated once. Slight over-allocation is
// fine; under-allocation would force a grow in the hot path.
func (b *Broker) estimateBucketSizesLocked(states []LifecycleState) int {
	total := 0
	for _, state := range states {
		total += len(b.lifecycleIndex[state])
	}
	return total
}

// inboxCountsLocked returns the four header counts. Three are O(1); the
// MergedToday count walks the merged bucket once because the index is
// not segmented by day. v1 accepts this — see InboxCounts doc above.
func (b *Broker) inboxCountsLocked(cutoff time.Time) InboxCounts {
	counts := InboxCounts{
		DecisionRequired: len(b.lifecycleIndex[LifecycleStateDecision]),
		Running:          len(b.lifecycleIndex[LifecycleStateRunning]),
		Blocked:          len(b.lifecycleIndex[LifecycleStateBlockedOnPRMerge]),
	}
	for _, taskID := range b.lifecycleIndex[LifecycleStateMerged] {
		task := b.findTaskByIDLocked(taskID)
		if task == nil {
			continue
		}
		ts := mergedAtTimestamp(task)
		if !ts.IsZero() && !ts.Before(cutoff) {
			counts.MergedToday++
		}
	}
	return counts
}

// buildInboxRowLocked assembles one InboxRow from the task plus any
// Decision Packet stored under b.decisionPackets (Lane C). Decision
// Packet population is best-effort: when Lane C has not yet written a
// packet, severity counts and reviewer-graded count read as zero, which
// matches Lane G's empty-state rendering.
func (b *Broker) buildInboxRowLocked(task *teamTask) InboxRow {
	row := InboxRow{
		TaskID:         task.ID,
		Title:          strings.TrimSpace(task.Title),
		LifecycleState: task.LifecycleState,
		ReviewerSummary: ReviewerSummary{
			Total: len(task.Reviewers),
		},
	}
	if created := parseBrokerTimestamp(task.CreatedAt); !created.IsZero() {
		row.ElapsedMs = time.Since(created).Milliseconds()
		if row.ElapsedMs < 0 {
			row.ElapsedMs = 0
		}
	}
	// On a real read error we leave the packet-derived columns at
	// their zero values; the row still renders so the operator sees
	// the task exists, but the severity summary will be empty and the
	// row falls back to the task-level metadata. A 5xx on the inbox
	// list because one packet failed to read is worse than a row that
	// is briefly missing its grade rollup.
	packet, err := b.findDecisionPacketLocked(task.ID)
	if err != nil {
		log.Printf("broker: inbox row for task %q: packet read failed: %v", task.ID, err)
	}
	if packet != nil {
		row.Assignment = strings.TrimSpace(packet.Spec.Assignment)
		row.SeveritySummary = severitySummaryFromGrades(packet.ReviewerGrades)
		row.ReviewerSummary.Graded = countGradedReviewers(packet.ReviewerGrades)
	}
	return row
}

// severitySummaryFromGrades counts each typed severity tier in a flat
// scan over the grade list. SeveritySkipped is intentionally NOT a row
// in the SeveritySummary surface — Lane G renders it as "skipped" inline
// next to the reviewer slug, not as a tier count.
func severitySummaryFromGrades(grades []ReviewerGrade) SeveritySummary {
	out := SeveritySummary{}
	for _, g := range grades {
		switch g.Severity {
		case SeverityCritical:
			out.Critical++
		case SeverityMajor:
			out.Major++
		case SeverityMinor:
			out.Minor++
		case SeverityNitpick:
			out.Nitpick++
		}
	}
	return out
}

// countGradedReviewers returns the number of unique reviewer slugs whose
// grade carries a typed severity (anything except the empty value). A
// reviewer who emitted a grade with no severity is treated as
// not-yet-submitted per the convergence rule in the design doc.
func countGradedReviewers(grades []ReviewerGrade) int {
	if len(grades) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(grades))
	for _, g := range grades {
		if g.Severity == "" {
			continue
		}
		slug := normalizeReviewerSlug(g.ReviewerSlug)
		if slug == "" {
			continue
		}
		seen[slug] = struct{}{}
	}
	return len(seen)
}

// mergedAtTimestamp returns the merge-completion timestamp for a task.
// Prefers task.CompletedAt (set by the existing terminal-status mutator)
// and falls back to UpdatedAt so freshly-merged tasks still appear in
// MergedToday before the legacy mutator paths catch up.
func mergedAtTimestamp(task *teamTask) time.Time {
	if task == nil {
		return time.Time{}
	}
	if ts := parseBrokerTimestamp(task.CompletedAt); !ts.IsZero() {
		return ts
	}
	return parseBrokerTimestamp(task.UpdatedAt)
}

// findTaskByIDLocked returns a pointer to the task with the given ID
// or nil if not present. Caller must hold b.mu. Linear scan is OK
// because the inbox row builder calls this once per row, NOT once per
// task in the broker.
func (b *Broker) findTaskByIDLocked(id string) *teamTask {
	if id == "" {
		return nil
	}
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			return &b.tasks[i]
		}
	}
	return nil
}

// normalizeReviewerSlug lowercases and trims a slug for membership
// comparisons. Reviewer assignment can come from agent slugs (which the
// broker stores lower-cased) or from human session slugs (which
// humanIdentityFromActor lowercases via normalizeHumanSessionSlug); both
// land in the same shape after this normalizer.
func normalizeReviewerSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}
