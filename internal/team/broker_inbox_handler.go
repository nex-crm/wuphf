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
	"io"
	"log"
	"net/http"
	"strings"
	"time"
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
			case "resume":
				b.handleTaskResume(w, r, actor, taskID)
				return
			case "decision":
				b.handleTaskDecision(w, r, actor, taskID)
				return
			case "comment":
				b.handleTaskComment(w, r, actor, taskID)
				return
			case "activity":
				b.handleTaskActivity(w, r, actor, taskID)
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
	// Snapshot the task summary alongside the reviewer list so the FE
	// detail view does not depend on a separate /tasks list query to
	// resolve display fields (title, channel, owner). Without this, a
	// freshly-created Issue renders "(untitled)" because the parallel
	// /tasks fetch is filtered by viewer_slug and may omit the row.
	reviewers := append([]string(nil), task.Reviewers...)
	taskSnapshot := *task
	packet, packetErr := b.findDecisionPacketLocked(id)
	var packetCopy DecisionPacket
	if packet != nil {
		packetCopy = *packet
	}
	b.mu.Unlock()

	if !taskAccessAllowed(actor, reviewers) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	// Note (Slice 7): /tasks/{id} stays open to broker-token holders.
	// Agent identity isn't carried on this endpoint (no my_slug header),
	// so we can't enforce per-agent visibility here without breaking
	// legitimate cross-agent reads (e.g. an agent looking up the
	// parent of a sub-issue it owns). The visibility filter on the
	// /tasks LIST endpoint (which DOES take viewer_slug) is what
	// shapes the agent's "which Issues exist" view and drives behavior.

	if packetErr != nil {
		// findDecisionPacketLocked returns (nil, nil) for "not yet
		// stored". A non-nil error means the on-disk store could not
		// be read — likely corruption or a permissions issue.
		// Surface that as 500 so the UI shows a real error banner
		// instead of the benign "not yet available" 404.
		log.Printf("broker: get decision packet task=%q: %v", id, packetErr)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "decision packet read failed"})
		return
	}
	if packet == nil {
		// Lazy-seed: any task can reach the Issue detail surface, but
		// only task_type=issue creates seed a packet on the
		// MutateTask create path. Sub-issues created by agents with
		// a non-issue task_type (or any pre-Slice-1 task) hit this
		// branch and would 404 with "decision packet not yet
		// available", breaking the detail view. Seed one on read so
		// the FE always gets a real packet to render.
		b.mu.Lock()
		seeded := b.getOrInitPacketLocked(id)
		if seeded != nil {
			b.stampLifecycleStateLocked(seeded)
			b.persistDecisionPacketLocked(id, *seeded)
			packetCopy = *seeded
		}
		b.mu.Unlock()
		if seeded == nil {
			// Defensive: getOrInitPacketLocked returns nil only on
			// invalid task id, which we've already validated above.
			// If this somehow fires, surface a real error so the FE
			// can show a banner rather than spin forever.
			log.Printf("broker: lazy-seed decision packet failed for task=%q", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not seed decision packet"})
			return
		}
	}
	// Decision packet response shape: packet fields at the top level
	// (taskId, lifecycleState, spec, ...) plus a "task" field carrying
	// the teamTask snapshot. The FE's normalizeIssueDocument reads
	// taskRecord = recordValue(r.task) for display fields.
	writeJSON(w, http.StatusOK, taskDetailResponse{
		TaskID:         packetCopy.TaskID,
		LifecycleState: packetCopy.LifecycleState,
		Spec:           packetCopy.Spec,
		SessionReport:  packetCopy.SessionReport,
		ChangedFiles:   packetCopy.ChangedFiles,
		ReviewerGrades: packetCopy.ReviewerGrades,
		Dependencies:   packetCopy.Dependencies,
		UpdatedAt:      packetCopy.UpdatedAt,
		Task:           &taskSnapshot,
	})
}

// taskDetailResponse mirrors DecisionPacket but adds the source
// teamTask so display fields (title, channel, owner) reach the FE
// without a second fetch. Defined here (not in
// broker_decision_packet_types.go) because it is purely a transport
// shape for GET /tasks/{id}; the persisted packet stays unchanged.
type taskDetailResponse struct {
	TaskID         string          `json:"taskId"`
	LifecycleState LifecycleState  `json:"lifecycleState"`
	Spec           Spec            `json:"spec"`
	SessionReport  SessionReport   `json:"sessionReport"`
	ChangedFiles   []DiffSummary   `json:"changedFiles"`
	ReviewerGrades []ReviewerGrade `json:"reviewerGrades"`
	Dependencies   Dependencies    `json:"dependencies"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	Task           *teamTask       `json:"task,omitempty"`
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

// handleTaskResume serves POST /tasks/{id}/resume. Body shape:
//
//	{"actor": "<slug>", "reason": "<text>"}
//
// Manually clears a blocked_on_pr_merge (or otherwise paused) task so
// the owner agent picks it up again. Wraps Broker.ResumeTask, which the
// watchdog scheduler also calls on retry. Humans with reviewer access on
// the task may resume it; everyone else gets 403. The action is
// idempotent — a re-issued resume on an already-running task returns
// 200 with changed=false in the response body.
func (b *Broker) handleTaskResume(w http.ResponseWriter, r *http.Request, actor requestActor, id string) {
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
		Reason string `json:"reason"`
	}
	// Body is optional — the UI button posts with no payload when no
	// reason is provided. Treat EOF as a valid empty body; reject any
	// other decode error so malformed JSON cannot silently mutate state.
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}

	// Auth: snapshot reviewers under the lock so the check races no
	// reviewer-routing write. Broker token bypasses the reviewer set.
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

	// Derive actor identity from auth context only. The body used to
	// accept an "actor" field; that would let any caller spoof the
	// audit trail on a task they otherwise have permission to resume,
	// so drop the field outright.
	actorSlug := strings.TrimSpace(actor.Slug)
	if actor.Kind == requestActorKindBroker {
		actorSlug = "owner"
	}
	if actorSlug == "" {
		actorSlug = "human"
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "Manual resume from inbox."
	}
	resumed, changed, err := b.ResumeTask(id, actorSlug, reason)
	if err != nil {
		log.Printf("broker: resume task %q: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":    resumed,
		"changed": changed,
	})
}

// handleTaskDecision serves POST /tasks/{id}/decision. Body shape:
//
//	{"action": "approve" | "request_changes" | "defer", "comment": "<optional reviewer note>"}
//
// When comment is non-empty, it's appended to the Decision Packet's
// spec.feedback log so the human's review note becomes part of the
// packet's durable history alongside the action.
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
		Action  string `json:"action"`
		Comment string `json:"comment,omitempty"`
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
	comment := strings.TrimSpace(body.Comment)

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

	authorSlug := strings.TrimSpace(actor.Slug)
	if actor.Kind == requestActorKindBroker {
		authorSlug = "owner"
	}
	if err := b.RecordTaskDecisionWithComment(id, action, comment, authorSlug); err != nil {
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

// handleTaskComment serves POST /tasks/{id}/comment. Body shape:
//
//	{"body": "<PR-style comment, no state change>"}
//
// Uses the auth-resolved actor so humans appear under their session
// slug ("human" by default) instead of the broker default "unknown".
// The append is purely additive — no lifecycle transition, no broadcast.
// Mirrors a GitHub PR review comment that does not approve or request
// changes.
func (b *Broker) handleTaskComment(w http.ResponseWriter, r *http.Request, actor requestActor, id string) {
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
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	trimmed := strings.TrimSpace(body.Body)
	if trimmed == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body required"})
		return
	}
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
	authorSlug := strings.TrimSpace(actor.Slug)
	if actor.Kind == requestActorKindBroker {
		authorSlug = "owner"
	}
	if authorSlug == "" {
		authorSlug = "human"
	}
	b.mu.Lock()
	b.AppendPacketFeedbackLocked(id, authorSlug, trimmed)
	// Wake the agents whose attention this comment needs. Parse any
	// @slug mentions; always add the channel leader (ceo) so an untagged
	// comment is picked up by CEO. The channel message is what triggers
	// agent loops to fetch new context — without it the feedback sits on
	// the packet but no one ever notices. Mirrors how
	// postTaskReassignNotificationsLocked works for ownership changes.
	if task != nil {
		b.postIssueCommentBroadcastLocked(authorSlug, task, trimmed)
	}
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{
		"taskId": id,
		"status": "recorded",
		"author": authorSlug,
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
