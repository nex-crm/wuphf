package team

// broker_inbox_handler_phase2.go wires the Phase 2 unified inbox over
// HTTP. The new endpoint is additive — the existing /tasks/inbox stays
// in place and continues to serve the task-only payload, so the legacy
// frontend keeps working through the transition. The new endpoint is:
//
//	GET /inbox/items?filter=<filter>&kind=<kind>
//
//	  filter — same set as /tasks/inbox: decision_required / running /
//	           blocked / approved / all. Applies to the task half only.
//	  kind   — optional task / request / review. Trims the union to one
//	           kind at the API boundary so per-kind frontend tabs do not
//	           pay the full merge cost.
//
// Response is { items: []InboxItem, counts: InboxCounts, refreshedAt }.
// Counts are derived from the caller's auth-filtered items so the badge
// math never reveals task volume the caller cannot see. The kind=<kind>
// query param trims the items response but NOT the counts — the badge
// totals stay accurate across tabs.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// unifiedInboxResponse is the wire shape for /inbox/items. Mirrors
// InboxPayload's shape but swaps Rows for Items so the frontend can
// distinguish the union response from the legacy task-only one.
type unifiedInboxResponse struct {
	Items       []InboxItem `json:"items"`
	Counts      InboxCounts `json:"counts"`
	RefreshedAt string      `json:"refreshedAt"`
}

// handleInboxItems serves GET /inbox/items.
func (b *Broker) handleInboxItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor, ok := requestActorFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	rawFilter := strings.TrimSpace(r.URL.Query().Get("filter"))
	if rawFilter == "" {
		rawFilter = string(InboxFilterAll)
	}
	// Always pull the unfiltered union for the caller so counts cover
	// every visible kind, regardless of the kind=<kind> trim below.
	allItems, err := b.inboxItemsForActor(actor, InboxFilterAll)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	counts := inboxCountsForItems(allItems, startOfTodayUTC())

	items, err := b.inboxItemsForActor(actor, InboxFilter(rawFilter))
	if err != nil {
		if errors.Is(err, ErrInboxFilterUnknown) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" {
		expected := InboxItemKind(kind)
		filtered := items[:0]
		for _, item := range items {
			if item.Kind == expected {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	writeJSON(w, http.StatusOK, unifiedInboxResponse{
		Items:       items,
		Counts:      counts,
		RefreshedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// inboxCountsForItems derives the badge counts from the caller's
// auth-filtered item list. The counts only cover task-kind items —
// requests and reviews live in their own counters and v1's
// InboxCounts shape doesn't yet expose them. Critical: this function
// is the only counts source on /inbox/items, so it must never reveal
// task volume the caller did not already see in `items`.
func inboxCountsForItems(items []InboxItem, todayCutoff time.Time) InboxCounts {
	var counts InboxCounts
	cutoffMs := todayCutoff.UnixMilli()
	for _, item := range items {
		if item.Kind != InboxItemKindTask || item.TaskRow == nil {
			continue
		}
		switch item.TaskRow.LifecycleState {
		case LifecycleStateDecision:
			counts.DecisionRequired++
		case LifecycleStateRunning:
			counts.Running++
		case LifecycleStateBlockedOnPRMerge, LifecycleStateQueuedBehindOwner:
			counts.Blocked++
		case LifecycleStateApproved:
			// ElapsedMs measures age, not absolute time, so derive
			// "today" from the row's createdAt + the cutoff. v1
			// keeps the InboxRow shape stable; richer "approvedAt"
			// timestamps are a v1.1 follow-up.
			if item.TaskRow.ElapsedMs > 0 && time.Now().UTC().UnixMilli()-item.TaskRow.ElapsedMs >= cutoffMs {
				counts.ApprovedToday++
			}
		}
	}
	return counts
}

// handleInboxCursor serves POST /inbox/cursor.
// Body: { "lastSeenAt": "<RFC3339>", "acknowledgedKinds": { "task": "<RFC3339>", ... } }
// After any decision action (approve / request_changes / defer), the
// frontend calls this so the badge count recalculates from the new cursor
// on the next GET /inbox/items. Per-kind acknowledgements stack onto the
// global lastSeenAt so a tab-specific clear does not reset the others.
func (b *Broker) handleInboxCursor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor, ok := requestActorFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		LastSeenAt        time.Time                   `json:"lastSeenAt"`
		AcknowledgedKinds map[InboxItemKind]time.Time `json:"acknowledgedKinds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.LastSeenAt.IsZero() && len(body.AcknowledgedKinds) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "lastSeenAt or acknowledgedKinds required"})
		return
	}

	b.SetInboxCursor(actor.Slug, InboxCursor{
		LastSeenAt:        body.LastSeenAt,
		AcknowledgedKinds: body.AcknowledgedKinds,
	})
	w.WriteHeader(http.StatusNoContent)
}
