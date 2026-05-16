package team

// broker_inbox_phase2.go is Phase 2 of the unified Inbox plan. It
// turns the Decision Inbox from a single-kind (lifecycle tasks only)
// surface into a fan-out merge across the three attention-bearing
// artifact kinds the broker tracks:
//
//   - task    — lifecycle items in decision / running / blocked / approved
//   - request — humanInterview pending human action
//   - review  — Promotion awaiting reviewer grade
//
// The plan from /plan-design-review + /plan-eng-review on 2026-05-11
// (artifact /tmp/wuphf-unified-inbox-plan.md) locks the contract:
//
//   - InboxItemKind is a closed string set; "task" / "request" / "review"
//   - InboxItem is the union row shape returned to the web UI / CLI
//   - inboxItemsForActor merges per-kind helpers under one auth boundary
//   - Per-kind helpers each enforce their own membership rule:
//        * tasksForInbox    — owner OR slug in task.Reviewers
//        * requestsForInbox — owner OR slug == req.From (channel-scope
//                              filter applied above; tagged/assigned
//                              fields land in a v1.1 follow-up)
//        * reviewsForInbox  — owner OR slug == promotion.ReviewerSlug
//   - SetInboxCursor / InboxCursor support per-user workflow-semantic
//     unread tracking (flips on action, not on open).

import (
	"sort"
	"strings"
	"time"
)

// InboxItemKind selects which artifact a row in the unified inbox came
// from. The set is closed; TS mirrors it as a discriminated-union tag.
type InboxItemKind string

const (
	InboxItemKindTask    InboxItemKind = "task"
	InboxItemKindRequest InboxItemKind = "request"
	InboxItemKindReview  InboxItemKind = "review"
)

// InboxItem is the unified row shape. Exactly one of TaskID /
// RequestID / ReviewID is set per row, determined by Kind. The web
// UI renders by switching on Kind; the never-default branch in TS
// enforces exhaustiveness at compile time.
//
// Fields that apply to more than one kind (Title, Channel, ElapsedMs)
// hoist to the top level so the inbox list renderer never has to
// peek inside per-kind payloads to draw the row.
type InboxItem struct {
	Kind      InboxItemKind `json:"kind"`
	TaskID    string        `json:"taskId,omitempty"`
	RequestID string        `json:"requestId,omitempty"`
	ReviewID  string        `json:"reviewId,omitempty"`
	Title     string        `json:"title"`
	Channel   string        `json:"channel,omitempty"`
	CreatedAt string        `json:"createdAt,omitempty"`
	ElapsedMs int64         `json:"elapsedMs,omitempty"`
	// AgentSlug is the agent who owns / sent / submitted this item.
	// Phase 3 uses this to group items into per-agent threads. Empty
	// when the item has no agent attribution (e.g. system-generated
	// tasks the harness can't trace back to a single agent).
	AgentSlug string `json:"agentSlug,omitempty"`
	// Per-kind enrichments. Populated only when Kind matches.
	TaskRow *InboxRow    `json:"task,omitempty"`
	Request *RequestPeek `json:"request,omitempty"`
	Review  *ReviewPeek  `json:"review,omitempty"`
}

// RequestPeek is the trimmed request shape the inbox row needs. Full
// request fetch happens lazily when the row opens.
type RequestPeek struct {
	Kind     string `json:"kind"`
	Question string `json:"question"`
	From     string `json:"from"`
	Blocking bool   `json:"blocking,omitempty"`
}

// ReviewPeek is the trimmed promotion shape the inbox row needs.
type ReviewPeek struct {
	State        string `json:"state"`
	ReviewerSlug string `json:"reviewerSlug"`
	SourceSlug   string `json:"sourceSlug"`
	TargetPath   string `json:"targetPath"`
}

// InboxCursor records the workflow-semantic read state for one human.
// LastSeenAt is the wall-clock time the user last took an action that
// would clear an item (approve, request changes, defer, dismiss).
// AcknowledgedKinds lets per-kind filters carry their own cursor so
// switching tabs does not reset the global one.
type InboxCursor struct {
	LastSeenAt        time.Time                   `json:"lastSeenAt"`
	AcknowledgedKinds map[InboxItemKind]time.Time `json:"acknowledgedKinds,omitempty"`
}

// IsZero reports whether the cursor has ever been written.
func (c InboxCursor) IsZero() bool {
	return c.LastSeenAt.IsZero() && len(c.AcknowledgedKinds) == 0
}

// inboxItemsForActor is the auth-aware fan-out merge entry point.
// Returns rows from every artifact kind the actor can see, sorted by
// most-recent-activity descending.
//
// The filter applies only to the task half (the lifecycle bucket).
// Requests and reviews always render regardless of filter because
// their states are not lifecycle-shaped; per-kind filtering on the
// frontend (kind=request etc.) hides them in the unified view.
func (b *Broker) inboxItemsForActor(actor requestActor, filter InboxFilter) ([]InboxItem, error) {
	if b == nil {
		return nil, nil
	}
	// Validate the filter up-front so a bad value short-circuits.
	if _, err := inboxFilterToStates(filter); err != nil {
		return nil, err
	}

	taskRows := b.tasksForInbox(actor)
	// When the caller asked for a single lifecycle bucket, trim tasks
	// to that bucket. InboxFilterAll keeps every row. This matches
	// the existing Inbox(filter) semantics; the new fan-out merges
	// requests + reviews on top.
	if filter != InboxFilterAll {
		states, _ := inboxFilterToStates(filter)
		allowed := make(map[LifecycleState]struct{}, len(states))
		for _, s := range states {
			allowed[s] = struct{}{}
		}
		filtered := taskRows[:0]
		for _, row := range taskRows {
			if row.TaskRow == nil {
				continue
			}
			if _, ok := allowed[row.TaskRow.LifecycleState]; ok {
				filtered = append(filtered, row)
			}
		}
		taskRows = filtered
	}

	rows := make([]InboxItem, 0, len(taskRows)+8)
	rows = append(rows, taskRows...)
	rows = append(rows, b.requestsForInbox(actor)...)
	rows = append(rows, b.reviewsForInbox(actor)...)

	sort.SliceStable(rows, func(i, j int) bool {
		// Attention-first: items the user can act on now (decision,
		// request, review) bubble to the top. Within the same priority
		// tier, sort by CreatedAt desc. This stops approved /
		// blocked-on-pr-merge tasks from burying the work that needs
		// the user's eyes.
		pi := inboxItemPriority(rows[i])
		pj := inboxItemPriority(rows[j])
		if pi != pj {
			return pi < pj
		}
		ti := parseBrokerTimestamp(rows[i].CreatedAt)
		tj := parseBrokerTimestamp(rows[j].CreatedAt)
		switch {
		case ti.IsZero() && tj.IsZero():
			return false
		case ti.IsZero():
			return false
		case tj.IsZero():
			return true
		default:
			return ti.After(tj)
		}
	})
	return rows, nil
}

// inboxItemPriority returns a sort key: lower = more attention-y.
// Tasks needing a decision and active requests/reviews tie at 0;
// running / blocked / approved-today tasks land below. Lifecycle
// states the user can't directly act on rank lowest.
func inboxItemPriority(item InboxItem) int {
	switch item.Kind {
	case InboxItemKindRequest, InboxItemKindReview:
		return 0
	case InboxItemKindTask:
		if item.TaskRow == nil {
			return 4
		}
		switch item.TaskRow.LifecycleState {
		case LifecycleStateDecision:
			return 0
		case LifecycleStateChangesRequested:
			return 1
		case LifecycleStateBlockedOnPRMerge:
			return 2
		case LifecycleStateReview, LifecycleStateRunning,
			LifecycleStateReady, LifecycleStateIntake:
			return 3
		case LifecycleStateApproved:
			return 4
		}
	}
	return 5
}

// tasksForInbox returns lifecycle-task rows visible to actor. Owner
// sees every task; a human session sees only tasks whose Reviewers
// list contains their slug. Counts/badges always reflect broker-wide
// state — the auth filter is on rows only.
func (b *Broker) tasksForInbox(actor requestActor) []InboxItem {
	if b == nil {
		return nil
	}
	owner := actor.Kind == requestActorKindBroker
	slug := normalizeReviewerSlug(actor.Slug)

	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]InboxItem, 0, len(b.tasks))
	for i := range b.tasks {
		task := &b.tasks[i]
		// Drop legacy tasks that the lifecycle migration could not
		// categorize. These are stale rows from pre-Lane-A sessions
		// (status=done, blocked=false, pipeline_stage=postmortem etc.)
		// and they have no packet on disk, so clicking them in the
		// inbox returns a "not yet available" cold error. Filtering
		// them at the source keeps the inbox focused on actionable
		// work.
		if task.LifecycleState == LifecycleStateUnknown {
			continue
		}
		if !owner {
			if slug == "" {
				continue
			}
			matched := false
			for _, r := range task.Reviewers {
				if normalizeReviewerSlug(r) == slug {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		row := b.buildInboxRowLocked(task)
		out = append(out, InboxItem{
			Kind:      InboxItemKindTask,
			TaskID:    task.ID,
			Title:     row.Title,
			Channel:   task.Channel,
			CreatedAt: task.CreatedAt,
			ElapsedMs: row.ElapsedMs,
			AgentSlug: normalizeReviewerSlug(task.Owner),
			TaskRow:   &row,
		})
	}
	return out
}

// requestsForInbox returns humanInterview rows visible to actor.
// Owner sees every active request; a human session sees requests
// whose From field matches their slug. Resolved requests are
// excluded — the inbox is exception-only.
//
// Phase 2 OSS-local scope: From is the proxy for "who needs to see
// this". Tagged / AssignedTo fields land in a v1.1 follow-up that
// extends the filter without changing this function's signature.
func (b *Broker) requestsForInbox(actor requestActor) []InboxItem {
	if b == nil {
		return nil
	}
	owner := actor.Kind == requestActorKindBroker
	slug := normalizeReviewerSlug(actor.Slug)

	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]InboxItem, 0, len(b.requests))
	for i := range b.requests {
		req := b.requests[i]
		if !requestIsActive(req) {
			continue
		}
		if !owner {
			if slug == "" || normalizeReviewerSlug(req.From) != slug {
				continue
			}
		}
		title := strings.TrimSpace(req.TitleOrDefault())
		if title == "" {
			title = strings.TrimSpace(req.Question)
		}
		out = append(out, InboxItem{
			Kind:      InboxItemKindRequest,
			RequestID: req.ID,
			Title:     title,
			Channel:   req.Channel,
			CreatedAt: req.CreatedAt,
			AgentSlug: normalizeReviewerSlug(req.From),
			Request: &RequestPeek{
				Kind:     req.Kind,
				Question: req.Question,
				From:     req.From,
				Blocking: req.Blocking,
			},
		})
	}
	return out
}

// reviewsForInbox returns promotion rows visible to actor. Owner sees
// every non-archived review; a human session sees only promotions
// where ReviewerSlug == their slug. Wires through the existing
// ReviewLog.List(scope) so the auth model stays consistent with the
// /review/list?scope=<slug> handler.
func (b *Broker) reviewsForInbox(actor requestActor) []InboxItem {
	if b == nil {
		return nil
	}
	rl := b.ReviewLog()
	if rl == nil {
		return nil
	}
	owner := actor.Kind == requestActorKindBroker
	slug := normalizeReviewerSlug(actor.Slug)
	var promotions []*Promotion
	if owner {
		promotions = rl.List("all")
	} else {
		if slug == "" {
			return nil
		}
		promotions = rl.List(slug)
	}
	out := make([]InboxItem, 0, len(promotions))
	for _, p := range promotions {
		if p == nil {
			continue
		}
		// Inbox is exception-only — once a review is approved /
		// rejected / archived / expired, it stops needing the
		// human's attention. Filter those terminal states out so
		// they don't pad the inbox after the work is done.
		switch p.State {
		case PromotionApproved, PromotionRejected, PromotionArchived, PromotionExpired:
			continue
		}
		title := notebookEntrySlug(p.SourcePath)
		if t := strings.TrimSpace(p.Rationale); t != "" {
			title = t
		}
		out = append(out, InboxItem{
			Kind:      InboxItemKindReview,
			ReviewID:  p.ID,
			Title:     title,
			CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
			AgentSlug: normalizeReviewerSlug(p.SourceSlug),
			Review: &ReviewPeek{
				State:        string(p.State),
				ReviewerSlug: p.ReviewerSlug,
				SourceSlug:   p.SourceSlug,
				TargetPath:   p.TargetPath,
			},
		})
	}
	return out
}

// SetInboxCursor records that the human at humanSlug acted on the
// inbox at the given wall-clock time. Used by the workflow-semantic
// unread badge. Concurrent writers are serialized on inboxCursorMu;
// the most recent LastSeenAt wins (clock skew assumption — broker
// is single-host so wall clock is monotonic enough for v1).
func (b *Broker) SetInboxCursor(humanSlug string, cursor InboxCursor) {
	if b == nil {
		return
	}
	slug := normalizeInboxHumanSlug(humanSlug)
	if slug == "" {
		return
	}
	b.inboxCursorMu.Lock()
	defer b.inboxCursorMu.Unlock()
	if b.userInboxCursors == nil {
		b.userInboxCursors = make(map[string]InboxCursor)
	}
	// Per-kind ack map is monotonic — a write that omits a previously
	// acknowledged kind must not wipe it. Seed from existing, then
	// overlay the caller's map (defensive-copy each value).
	existing, hasExisting := b.userInboxCursors[slug]
	stored := InboxCursor{LastSeenAt: cursor.LastSeenAt}
	mergedKinds := map[InboxItemKind]time.Time{}
	if hasExisting {
		for k, v := range existing.AcknowledgedKinds {
			mergedKinds[k] = v
		}
	}
	for k, v := range cursor.AcknowledgedKinds {
		mergedKinds[k] = v
	}
	if len(mergedKinds) > 0 {
		stored.AcknowledgedKinds = mergedKinds
	}
	// "Most recent write wins" — but never let a stale write
	// rewind LastSeenAt for the same slug.
	if hasExisting && stored.LastSeenAt.Before(existing.LastSeenAt) {
		stored.LastSeenAt = existing.LastSeenAt
	}
	b.userInboxCursors[slug] = stored
}

// InboxCursor returns the recorded cursor for humanSlug. Returns a
// zero value when the human has never acted.
func (b *Broker) InboxCursor(humanSlug string) InboxCursor {
	if b == nil {
		return InboxCursor{}
	}
	slug := normalizeInboxHumanSlug(humanSlug)
	if slug == "" {
		return InboxCursor{}
	}
	b.inboxCursorMu.RLock()
	defer b.inboxCursorMu.RUnlock()
	cursor, ok := b.userInboxCursors[slug]
	if !ok {
		return InboxCursor{}
	}
	// Defensive copy on the way out so the caller cannot mutate
	// stored state.
	out := InboxCursor{LastSeenAt: cursor.LastSeenAt}
	if len(cursor.AcknowledgedKinds) > 0 {
		out.AcknowledgedKinds = make(map[InboxItemKind]time.Time, len(cursor.AcknowledgedKinds))
		for k, v := range cursor.AcknowledgedKinds {
			out.AcknowledgedKinds[k] = v
		}
	}
	return out
}

// normalizeInboxHumanSlug lowercases and trims a slug used as a key
// in the per-user inbox cursor map. Mirrors normalizeReviewerSlug so
// equality checks across the inbox subsystem stay consistent.
func normalizeInboxHumanSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}
