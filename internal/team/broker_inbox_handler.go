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
		rawFilter = string(InboxFilterNeedsDecision)
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
	"intake":           {},
	"ack":              {},
	"memory-workflow":  {},
	"memory-workflows": {},
}

// handleTaskByID serves GET /tasks/{id} and POST /tasks/{id}/{verb}.
// Mounted via b.withAuth on the "/tasks/" prefix. ServeMux's longest-
// prefix matching means the existing exact paths /tasks, /tasks/ack,
// /tasks/memory-workflow, /tasks/memory-workflow/reconcile, /tasks/inbox,
// and /tasks/intake win over this prefix handler — so this path
// effectively only fires for /tasks/{id} (GET) or /tasks/{id}/{verb}
// (POST).
//
// Recognised verbs: block (Lane F block-on-PR-merge), transition (Lane F
// lifecycle advance). merge / request-changes / defer remain reserved
// for Lane G and return 404 here so a stray client cannot silently
// land on the wrong handler.
func (b *Broker) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	actor, ok := requestActorFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/tasks/")
	if strings.Contains(rest, "/") {
		segments := strings.SplitN(rest, "/", 2)
		if len(segments) == 2 {
			switch segments[1] {
			case "block":
				b.handleTaskBlock(w, r, actor, strings.TrimSpace(segments[0]))
				return
			case "transition":
				b.handleTaskTransition(w, r, actor, strings.TrimSpace(segments[0]))
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
	on := strings.TrimSpace(body.On)
	reason := strings.TrimSpace(body.Reason)
	if reason == "" && on != "" {
		reason = "blocked on " + on
	}
	actorSlug := strings.TrimSpace(body.Actor)
	if actorSlug == "" {
		actorSlug = "human"
	}
	task, ok, err := b.BlockTask(id, actorSlug, reason, on)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, task)
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

// intakeHTTPOutcome is the JSON wire shape returned by POST /tasks/intake.
// We do not return the AutoAssignCountdown handle (it is not serialisable
// and lives on the broker process); the CLI re-creates a local countdown
// when AutoAssign is non-empty so the user-facing keypress UX is identical
// to the in-process path.
type intakeHTTPOutcome struct {
	TaskID     string `json:"taskId"`
	Spec       Spec   `json:"spec"`
	AutoAssign string `json:"autoAssign,omitempty"`
}

// handleTasksIntake serves POST /tasks/intake. Body shape:
//
//	{"intent": "<free-text intent>"}
//
// Returns 200 with intakeHTTPOutcome on a clean intake → ready
// transition. The task lands in LifecycleStateReady; the caller is
// responsible for the ready → running advance via POST
// /tasks/{id}/transition.
//
// Auth: broker-only. Tunnel humans cannot trigger intake in v1; the
// founder/owner runs `wuphf task start` against their own broker.
func (b *Broker) handleTasksIntake(w http.ResponseWriter, r *http.Request) {
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
	if actor.Kind != requestActorKindBroker {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		Intent string `json:"intent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	intent := strings.TrimSpace(body.Intent)
	if intent == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "intent required"})
		return
	}
	provider := b.resolveIntakeProvider()
	if provider == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no intake provider configured"})
		return
	}
	outcome, err := b.StartIntake(r.Context(), intent, provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, intakeHTTPOutcome{
		TaskID:     outcome.TaskID,
		Spec:       outcome.Spec,
		AutoAssign: outcome.AutoAssign,
	})
}

// handleTaskTransition serves POST /tasks/{id}/transition. Body shape:
//
//	{"to": "<lifecycle-state>", "reason": "<text>"}
//
// On success returns 200 with the post-transition teamTask snapshot.
// Auth: broker/owner only — humans drive lifecycle advances through
// Lane G's UI verbs (merge / request-changes / defer), which post to
// dedicated endpoints rather than this raw transition primitive.
func (b *Broker) handleTaskTransition(w http.ResponseWriter, r *http.Request, actor requestActor, id string) {
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
		To     string `json:"to"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	to := strings.TrimSpace(body.To)
	if to == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target lifecycle state required"})
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "transition via api"
	}
	if err := b.TransitionLifecycle(id, LifecycleState(to), reason); err != nil {
		// Distinguish "task not found" from "transition rejected" so the
		// CLI surfaces a clearer error to the user.
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	b.mu.Lock()
	task := b.findTaskByIDLocked(id)
	var snapshot teamTask
	if task != nil {
		snapshot = *task
	}
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, snapshot)
}
