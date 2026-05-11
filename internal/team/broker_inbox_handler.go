package team

// broker_inbox_handler.go is the REST surface for Lane E.
//
// Two routes:
//
//   - GET /tasks/inbox?filter=<filter>  → InboxPayload
//     Defaults to filter=needs_decision when omitted; returns 400 on
//     unknown filter values. Auth filter applied per the table in the
//     design doc "Tunnel-human reviewer auth" section.
//
//   - GET /tasks/{id}  → DecisionPacket
//     Returns the on-disk packet shape verbatim. 404 when the task ID
//     does not exist; 403 when the human session is not in the task's
//     reviewer list; 200 for owner/broker token or for human sessions
//     in the reviewer list.
//
// Both routes sit behind b.withAuth (registered in broker.go alongside
// the existing /tasks routes). withAuth handles the unauthenticated
// case (401); this file owns the authorization layering on top of
// authenticated actors.
//
// Method gating: only GET is supported on both endpoints. The single
// /tasks/ subpath handler dispatches by the trimmed ID token; reserved
// suffixes ("inbox", "ack", "memory-workflow", "") fall through to a
// 404 so the broader /tasks-family routes (registered via
// taskBrokerRoutes) keep their meanings.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

// handleTasksInbox serves GET /tasks/inbox. Mounted via b.withAuth
// from broker.go; the actor identity is read from the request context.
func (b *Broker) handleTasksInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor, ok := requestActorFromContext(r.Context())
	if !ok {
		// Defensive: withAuth should already have rejected this. The
		// 401 here keeps the handler safe even if it is wired without
		// withAuth in a future refactor.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	rawFilter := strings.TrimSpace(r.URL.Query().Get("filter"))
	if rawFilter == "" {
		rawFilter = string(InboxFilterDecisionRequired)
	}
	payload, err := b.inboxForActor(InboxFilter(rawFilter), actor.Kind == requestActorKindBroker, actor.Slug)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// reservedTaskSubpath captures the exact-path /tasks/* routes already
// owned by other handlers. handleTaskByID returns 404 instead of
// returning a Decision Packet keyed by these literal IDs so that a
// future refactor that drops one of those routes does not silently
// expose the wrong document.
var reservedTaskSubpath = map[string]struct{}{
	"":                 {},
	"inbox":            {},
	"ack":              {},
	"memory-workflow":  {},
	"memory-workflows": {},
}

// handleTaskByID serves GET /tasks/{id} and POST /tasks/{id}/block.
// Mounted via b.withAuth on the "/tasks/" prefix. ServeMux's longest-
// prefix matching means the existing exact paths /tasks, /tasks/ack,
// /tasks/memory-workflow, /tasks/memory-workflow/reconcile, and
// /tasks/inbox win over this prefix handler — so this path effectively
// only fires for /tasks/{id} (GET) or /tasks/{id}/block (POST).
func (b *Broker) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	actor, ok := requestActorFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/tasks/")
	// /tasks/{id}/block is the Lane F block-on-PR-merge action. Other
	// verbs (merge / request-changes / defer) are still reserved for
	// Lane G; they return 404 here so a stray client cannot silently
	// land on the wrong handler.
	if strings.Contains(rest, "/") {
		segments := strings.SplitN(rest, "/", 2)
		taskID := strings.TrimSpace(segments[0])
		if !IsSafeTaskID(taskID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
			return
		}
		if len(segments) == 2 {
			switch segments[1] {
			case "block":
				b.handleTaskBlock(w, r, actor, taskID)
				return
			case "decision":
				b.handleTaskDecision(w, r, actor, taskID)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(rest)
	if _, reserved := reservedTaskSubpath[id]; reserved {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !IsSafeTaskID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}

	b.mu.Lock()
	task := b.findTaskByIDLocked(id)
	if task == nil {
		b.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	// Snapshot reviewer list under the lock so the auth check runs
	// against a stable view; releasing the lock before the auth
	// decision would race Lane D's reviewer-routing writes.
	reviewers := append([]string(nil), task.Reviewers...)
	packet := b.findDecisionPacketLocked(id)
	var packetCopy DecisionPacket
	if packet != nil {
		packetCopy = *packet
	}
	b.mu.Unlock()

	if !taskAccessAllowed(actor, reviewers) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if packet == nil {
		// Lane C has not yet stored a packet for this task. Return a
		// 404 in v1 so the frontend distinguishes "task exists, no
		// packet yet" from "task not found at all". When Lane C ships
		// a regenerate-from-memory fallback (per the persistence error
		// row in the failure-modes matrix), this branch flips to a
		// regenerated packet plus the missing-packet banner.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "decision packet not yet available"})
		return
	}
	writeJSON(w, http.StatusOK, packetCopy)
}

// handleTaskBlock serves POST /tasks/{id}/block. Body shape:
//
//	{"on": "<pr-or-task-id>", "actor": "<slug>", "reason": "<text>"}
//
// On success returns 200 with the post-block teamTask snapshot.
// Auth: broker/owner only — humans cannot block other reviewers' tasks
// in v1 to keep the action surface small.
func (b *Broker) handleTaskBlock(w http.ResponseWriter, r *http.Request, actor requestActor, id string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if actor.Kind != requestActorKindBroker {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task id required"})
		return
	}
	var body struct {
		On     string `json:"on"`
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" && strings.TrimSpace(body.On) != "" {
		reason = "blocked on " + strings.TrimSpace(body.On)
	}
	actorSlug := strings.TrimSpace(body.Actor)
	if actorSlug == "" {
		actorSlug = "human"
	}
	task, ok, err := b.BlockTask(id, actorSlug, reason, strings.TrimSpace(body.On))
	if err != nil {
		log.Printf("broker: block task %q: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleTaskDecision serves POST /tasks/{id}/decision. Body shape:
//
//	{"action": "merge" | "request_changes" | "defer", "actor": "<slug>"}
//
// Returns 200 with the recorded decision summary, 400 on invalid
// action / unknown task, 403 when the human session has no reviewer
// access. The buttons in the Decision Packet view post here.
func (b *Broker) handleTaskDecision(w http.ResponseWriter, r *http.Request, actor requestActor, id string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task id required"})
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	action := strings.TrimSpace(strings.ToLower(body.Action))
	if action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action required"})
		return
	}

	// Auth: broker token always; human session must be in the task's
	// reviewer list. Snapshot reviewers under the lock for a stable check.
	b.mu.Lock()
	task := b.findTaskByIDLocked(id)
	if task == nil {
		b.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	reviewers := append([]string(nil), task.Reviewers...)
	b.mu.Unlock()
	if !taskAccessAllowed(actor, reviewers) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if err := b.RecordTaskDecision(id, action); err != nil {
		if errors.Is(err, ErrUnknownDecisionAction) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("broker: record decision task=%q action=%q: %v", id, action, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"taskId": id,
		"action": action,
		"status": "recorded",
	})
}

// taskAccessAllowed encodes the auth matrix from the design doc:
// broker/owner token always allowed; human session allowed iff the
// human's slug matches at least one entry in the task's Reviewers.
func taskAccessAllowed(actor requestActor, reviewers []string) bool {
	if actor.Kind == requestActorKindBroker {
		return true
	}
	if actor.Kind != requestActorKindHuman {
		return false
	}
	slug := normalizeReviewerSlug(actor.Slug)
	if slug == "" {
		return false
	}
	for _, r := range reviewers {
		if normalizeReviewerSlug(r) == slug {
			return true
		}
	}
	return false
}
