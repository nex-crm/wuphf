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
// Counts come from the existing indexed-bucket path (O(1) reads of
// b.lifecycleIndex) and intentionally remain broker-wide — auth filter
// applies to items only.

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

	// Counts reuse the indexed-bucket path so the badge math stays
	// O(1). Sums across the three kinds land in v1.1 alongside the
	// per-user cursor — for now the badge counts decisions only,
	// matching the existing inbox badge behavior.
	b.mu.Lock()
	counts := b.inboxCountsLocked(startOfTodayUTC())
	b.mu.Unlock()

	writeJSON(w, http.StatusOK, unifiedInboxResponse{
		Items:       items,
		Counts:      counts,
		RefreshedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// handleInboxCursor serves POST /inbox/cursor.
// Body: { "lastSeenAt": "<RFC3339>" }
// After any decision action (approve / request_changes / defer), the
// frontend calls this so the badge count recalculates from the new cursor
// on the next GET /inbox/items.
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
		LastSeenAt time.Time `json:"lastSeenAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LastSeenAt.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "lastSeenAt required"})
		return
	}

	b.SetInboxCursor(actor.Slug, InboxCursor{LastSeenAt: body.LastSeenAt})
	w.WriteHeader(http.StatusNoContent)
}
