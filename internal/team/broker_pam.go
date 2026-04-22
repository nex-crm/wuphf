package team

// broker_pam.go wires Pam the Archivist onto the broker — dispatcher
// lifecycle, HTTP handlers, SSE fanout.
//
// Route map (registered in broker.go):
//
//	GET  /pam/actions   — list Pam's action registry (id + label, menu order)
//	POST /pam/action    — trigger a Pam action on an article
//
// SSE events fanned out via /events:
//
//	pam:action_started  — { job_id, action, article_path, request_by, started_at }
//	pam:action_done     — { job_id, action, article_path, commit_sha, finished_at }
//	pam:action_failed   — { job_id, action, article_path, error, failed_at }

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// pamSubscribersMu guards the SSE subscriber side-registry below. Same
// side-table pattern as broker_playbook.go — avoids touching broker.go's
// already-long struct.
//
// TODO: move these subscriber maps onto the Broker struct for cleaner
// shutdown semantics (they currently leak across broker lifetimes if a
// process ever hosts more than one Broker). Kept as globals for this PR
// to match the existing broker_playbook.go pattern; the Pam dispatcher
// itself has been moved onto Broker (see Broker.pamDispatcher).
var (
	pamSubscribersMu       sync.Mutex
	pamStartedSubsByBroker = map[*Broker]map[int]chan PamActionStartedEvent{}
	pamDoneSubsByBroker    = map[*Broker]map[int]chan PamActionDoneEvent{}
	pamFailedSubsByBroker  = map[*Broker]map[int]chan PamActionFailedEvent{}
)

// maxPamActionBodyBytes caps request bodies on POST /pam/action. The
// handler only needs a tiny JSON object; anything over this is either
// a malformed client or an abuse attempt.
const maxPamActionBodyBytes = 1 << 16 // 64 KiB

// maxPamActorSlugLen caps the actor_slug we will accept from the client.
// Also used for the action slug cap. Path is capped separately because
// article paths can be longer.
const (
	maxPamActorSlugLen = 64
	maxPamActionIDLen  = 64
	maxPamArticlePath  = 512
)

// pamActorSlugValid reports whether slug is composed only of the safe
// character set we allow for actor identities — alphanumerics plus the
// three separators `.`, `_`, and `-`.
func pamActorSlugValid(slug string) bool {
	if slug == "" || len(slug) > maxPamActorSlugLen {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// SubscribePamActionEvents returns three channels (started/done/failed) plus
// a single unsubscribe func. The /events SSE handler uses this.
func (b *Broker) SubscribePamActionEvents(buffer int) (<-chan PamActionStartedEvent, <-chan PamActionDoneEvent, <-chan PamActionFailedEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()

	started := pamStartedSubsByBroker[b]
	if started == nil {
		started = make(map[int]chan PamActionStartedEvent)
		pamStartedSubsByBroker[b] = started
	}
	done := pamDoneSubsByBroker[b]
	if done == nil {
		done = make(map[int]chan PamActionDoneEvent)
		pamDoneSubsByBroker[b] = done
	}
	failed := pamFailedSubsByBroker[b]
	if failed == nil {
		failed = make(map[int]chan PamActionFailedEvent)
		pamFailedSubsByBroker[b] = failed
	}

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.mu.Unlock()

	startedCh := make(chan PamActionStartedEvent, buffer)
	doneCh := make(chan PamActionDoneEvent, buffer)
	failedCh := make(chan PamActionFailedEvent, buffer)
	started[id] = startedCh
	done[id] = doneCh
	failed[id] = failedCh

	return startedCh, doneCh, failedCh, func() {
		pamSubscribersMu.Lock()
		defer pamSubscribersMu.Unlock()
		if m := pamStartedSubsByBroker[b]; m != nil {
			if ch, ok := m[id]; ok {
				delete(m, id)
				close(ch)
			}
		}
		if m := pamDoneSubsByBroker[b]; m != nil {
			if ch, ok := m[id]; ok {
				delete(m, id)
				close(ch)
			}
		}
		if m := pamFailedSubsByBroker[b]; m != nil {
			if ch, ok := m[id]; ok {
				delete(m, id)
				close(ch)
			}
		}
	}
}

// PublishPamActionStarted implements pamEventPublisher.
func (b *Broker) PublishPamActionStarted(evt PamActionStartedEvent) {
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()
	for key, ch := range pamStartedSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
			log.Printf("pam: dropping action_started event for subscriber %d (buffer full)", key)
		}
	}
}

// PublishPamActionDone implements pamEventPublisher.
func (b *Broker) PublishPamActionDone(evt PamActionDoneEvent) {
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()
	for key, ch := range pamDoneSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
			log.Printf("pam: dropping action_done event for subscriber %d (buffer full)", key)
		}
	}
}

// PublishPamActionFailed implements pamEventPublisher.
func (b *Broker) PublishPamActionFailed(evt PamActionFailedEvent) {
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()
	for key, ch := range pamFailedSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
			log.Printf("pam: dropping action_failed event for subscriber %d (buffer full)", key)
		}
	}
}

// ensurePamDispatcher lazily constructs and starts the Pam dispatcher the
// first time an endpoint needs it. Mirrors ensurePlaybookSynthesizer.
// Returns the live dispatcher or nil if the wiki worker isn't ready yet.
//
// Idempotent: safe to call repeatedly. The dispatcher is stored on the
// Broker so Broker.Stop() can tear it down cleanly.
func (b *Broker) ensurePamDispatcher() *PamDispatcher {
	b.mu.Lock()
	if disp := b.pamDispatcher; disp != nil {
		b.mu.Unlock()
		return disp
	}
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		log.Printf("pam: wiki worker not ready; dispatcher not started")
		return nil
	}

	disp := NewPamDispatcher(worker, b, PamDispatcherConfig{})
	disp.Start(context.Background())

	b.mu.Lock()
	if existing := b.pamDispatcher; existing != nil {
		// Lost the race. Shut ours down and keep the existing one.
		b.mu.Unlock()
		disp.Stop()
		return existing
	}
	b.pamDispatcher = disp
	b.mu.Unlock()
	return disp
}

// PamDispatcher returns the live dispatcher or nil. Kept for the SSE
// subscribe path and other callers that want to check liveness without
// triggering lazy construction.
func (b *Broker) PamDispatcher() *PamDispatcher {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pamDispatcher
}

// handlePamActions is GET /pam/actions — returns the action registry so the
// UI can render the desk menu without hard-coding labels.
//
//	resp: { "actions": [ { "id": "enrich_article", "label": "..." }, ... ] }
func (b *Broker) handlePamActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items := PamActions()
	out := make([]map[string]string, 0, len(items))
	for _, a := range items {
		out = append(out, map[string]string{
			"id":    string(a.ID),
			"label": a.Label,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": out})
}

// handlePamAction is POST /pam/action — enqueue a Pam job.
//
//	body: { "action": "enrich_article", "path": "team/companies/foo.md", "actor_slug"?: "..." }
//	resp: { "job_id", "queued_at" }
func (b *Broker) handlePamAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	disp := b.ensurePamDispatcher()
	if disp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "pam dispatcher is not active"})
		return
	}

	// Defense-in-depth: cap the request body before decoding. The decoder
	// below will surface MaxBytesError as a normal decode failure, which
	// we map to 400. That's intentional — we do not want to leak an
	// internal error class to the client.
	r.Body = http.MaxBytesReader(w, r.Body, maxPamActionBodyBytes)

	var body struct {
		Action    string `json:"action"`
		Path      string `json:"path"`
		ActorSlug string `json:"actor_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		// http.MaxBytesError → 413 so clients get the canonical "body too
		// large" signal instead of a generic 400.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	actionRaw := strings.TrimSpace(body.Action)
	if actionRaw == "" || len(actionRaw) > maxPamActionIDLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action"})
		return
	}
	actionID := PamActionID(actionRaw)

	path := strings.TrimSpace(body.Path)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if len(path) > maxPamArticlePath {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path too long"})
		return
	}

	actor := strings.TrimSpace(body.ActorSlug)
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if actor == "" {
		actor = "human"
	}
	if !pamActorSlugValid(actor) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid actor_slug"})
		return
	}

	id, err := disp.Enqueue(actionID, path, actor)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownPamAction):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrPamArticleMissing):
			// Don't echo the article path — log the full error server-side
			// and return a fixed message to the client.
			log.Printf("pam: article missing for %s on %s by %s: %v", actionID, path, actor, err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "article not found"})
		case errors.Is(err, ErrPamQueueSaturated):
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrPamStopped):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		default:
			// Avoid leaking internal error text to callers.
			log.Printf("pam: action dispatch failed for %s on %s by %s: %v", actionID, path, actor, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job_id":    id,
		"queued_at": time.Now().UTC().Format(time.RFC3339),
	})
}
