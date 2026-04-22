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

// pamSubscribersMu guards the side-registry below. Same side-table pattern
// as broker_playbook.go — avoids touching broker.go's already-long struct.
var (
	pamSubscribersMu       sync.Mutex
	pamStartedSubsByBroker = map[*Broker]map[int]chan PamActionStartedEvent{}
	pamDoneSubsByBroker    = map[*Broker]map[int]chan PamActionDoneEvent{}
	pamFailedSubsByBroker  = map[*Broker]map[int]chan PamActionFailedEvent{}
	pamDispatcherByBroker  = map[*Broker]*PamDispatcher{}
)

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
	for _, ch := range pamStartedSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
		}
	}
}

// PublishPamActionDone implements pamEventPublisher.
func (b *Broker) PublishPamActionDone(evt PamActionDoneEvent) {
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()
	for _, ch := range pamDoneSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
		}
	}
}

// PublishPamActionFailed implements pamEventPublisher.
func (b *Broker) PublishPamActionFailed(evt PamActionFailedEvent) {
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()
	for _, ch := range pamFailedSubsByBroker[b] {
		select {
		case ch <- evt:
		default:
		}
	}
}

// ensurePamDispatcher lazily constructs and starts the Pam dispatcher the
// first time an endpoint needs it. Mirrors ensurePlaybookSynthesizer.
func (b *Broker) ensurePamDispatcher() {
	pamSubscribersMu.Lock()
	if _, ok := pamDispatcherByBroker[b]; ok {
		pamSubscribersMu.Unlock()
		return
	}
	pamSubscribersMu.Unlock()

	b.mu.Lock()
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}

	disp := NewPamDispatcher(worker, b, PamDispatcherConfig{})
	disp.Start(context.Background())

	pamSubscribersMu.Lock()
	if _, ok := pamDispatcherByBroker[b]; ok {
		// Lost the race. Shut ours down and keep the existing one.
		pamSubscribersMu.Unlock()
		disp.Stop()
		return
	}
	pamDispatcherByBroker[b] = disp
	pamSubscribersMu.Unlock()
}

// PamDispatcher returns the live dispatcher or nil.
func (b *Broker) PamDispatcher() *PamDispatcher {
	pamSubscribersMu.Lock()
	defer pamSubscribersMu.Unlock()
	return pamDispatcherByBroker[b]
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
	b.ensurePamDispatcher()
	disp := b.PamDispatcher()
	if disp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "pam dispatcher is not active"})
		return
	}

	var body struct {
		Action    string `json:"action"`
		Path      string `json:"path"`
		ActorSlug string `json:"actor_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	actionID := PamActionID(strings.TrimSpace(body.Action))
	path := strings.TrimSpace(body.Path)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	actor := strings.TrimSpace(body.ActorSlug)
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if actor == "" {
		actor = "human"
	}

	id, err := disp.Enqueue(actionID, path, actor)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownPamAction):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrPamQueueSaturated):
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrPamStopped):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		default:
			log.Printf("pam: enqueue %s on %s by %s: %v", actionID, path, actor, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job_id":    id,
		"queued_at": time.Now().UTC().Format(time.RFC3339),
	})
}
