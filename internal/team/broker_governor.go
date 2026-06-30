package team

// broker_governor.go wires the session governor (governor.go) into the broker:
// the dispatch-loop hooks the launcher calls, the manual pause/stop/resume
// surface, the SSE fanout, and the GET/POST /governor HTTP handler.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// governorActionMaxBodyBytes caps the POST /governor body. The valid payload is
// a tiny action + slug + two numbers; anything larger is a bug or abuse. Mirrors
// the body caps on the other POST handlers (otlpLogsMaxBodyBytes etc).
const governorActionMaxBodyBytes = 1 << 10 // 1 KiB

// Bounds for the "Continue +budget" bump so a single call can't set the budget
// to +Inf (disabling the cost gate) or overflow int on a 32-bit build.
const (
	governorMaxAddTokens  = 10_000_000
	governorMaxAddCostUsd = 1000.0
)

// SetHeadlessDispatchController wires the launcher's cancel hook (used by Stop
// to interrupt in-flight turns). Deliberately separate from launcherDrainer
// (which fully drains + exits) so Stop is resumable — it cancels the current
// turn but leaves workers parked on the governor gate. nil means Stop still
// pauses dispatch but cannot cancel an already-running turn.
func (b *Broker) SetHeadlessDispatchController(c headlessDispatchController) {
	if b == nil || b.governor == nil {
		return
	}
	b.governor.setController(c)
}

// dispatchAgentTurn enqueues a single agent turn directly — one agent, one job,
// no CEO/lead orchestration hop. Returns false when no enqueuer is wired (unit
// tests, or before the launcher attaches). This is the pi-skeleton dispatch the
// App Builder edit path uses so a human change reaches the builder without
// bouncing through the lead (which can hit its own turn cap and drop the work).
func (b *Broker) dispatchAgentTurn(slug, prompt, channel string) bool {
	if b == nil || b.governor == nil {
		return false
	}
	e, ok := b.governor.enqueuer()
	if !ok {
		return false
	}
	e.EnqueueHeadlessTurn(slug, prompt, channel)
	return true
}

// initGovernor creates the session governor and baselines it to any usage
// restored from the state file, so a new session doesn't trip an instant budget
// pause on its first turn. Called once from NewBrokerAt after state load.
func (b *Broker) initGovernor() {
	b.governor = newGovernor(loadGovernorConfig(), 0, 0)
	tok, cost := b.sessionUsageSnapshot()
	b.governor.rebaseline(tok, cost)
}

// sessionUsageSnapshot returns the current session's cumulative tokens and cost
// from the usage accounting that every headless turn feeds via RecordAgentUsage.
func (b *Broker) sessionUsageSnapshot() (tokens int, cost float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usage.Session.TotalTokens, b.usage.Session.CostUsd
}

// governorGate is called by the launcher's dispatch worker before each turn.
// It blocks while dispatch is paused and returns false when the worker should
// exit (stop signalled while parked). Nil-safe for bare test launchers.
func (b *Broker) governorGate(stop <-chan struct{}) bool {
	if b == nil || b.governor == nil {
		return true
	}
	return b.governor.gate(stop)
}

// governorNoteTurn records one completed autonomous turn. If a budget or
// turn-count threshold trips, dispatch is now paused and we raise a review
// notice in #general plus an SSE update so the UI surfaces the checkpoint.
func (b *Broker) governorNoteTurn() {
	if b == nil || b.governor == nil {
		return
	}
	tok, cost := b.sessionUsageSnapshot()
	tripped, reason := b.governor.noteTurnComplete(tok, cost)
	if !tripped {
		return
	}
	// Report spend SINCE the last checkpoint, not lifetime session totals — a
	// rebaseline on resume/startup would otherwise make the notice overstate
	// "this stretch".
	st := b.governor.status(tok, cost)
	b.PostSystemMessage("general", governorPauseMessage(reason, st.TokensSinceCheckpoint, st.CostSinceCheckpoint), "governor")
	b.publishGovernorEvent()
}

// GovernorPause is the manual "Pause" action: stop starting new turns; let the
// in-flight turn finish so work lands in a reviewable state.
func (b *Broker) GovernorPause() {
	if b == nil || b.governor == nil {
		return
	}
	b.governor.pauseManual(pauseManual)
	b.publishGovernorEvent()
}

// GovernorStop is the manual "Stop" action: pause dispatch AND cancel in-flight
// turns now. slug == "" stops the whole team; a slug stops one agent. Queued
// work is preserved and drains on resume (clearing it would be harder to undo).
func (b *Broker) GovernorStop(slug string) {
	if b == nil || b.governor == nil {
		return
	}
	b.governor.pauseManual(pauseStop)
	b.governor.cancelInFlight(strings.TrimSpace(slug))
	b.publishGovernorEvent()
}

// GovernorResume clears the pause, wakes parked workers, and rebaselines the
// budget window to current usage so the next checkpoint measures fresh spend.
func (b *Broker) GovernorResume() {
	if b == nil || b.governor == nil {
		return
	}
	tok, cost := b.sessionUsageSnapshot()
	b.governor.resume(tok, cost)
	b.publishGovernorEvent()
}

// GovernorResumeMore is "Continue +budget": raise the thresholds for this
// session, then resume. Bounds are enforced here (not just in the HTTP handler)
// so every caller gets the same rejection for negative or oversized values.
func (b *Broker) GovernorResumeMore(addTokens int, addCost float64) error {
	if b == nil || b.governor == nil {
		return nil
	}
	if addTokens < 0 || addTokens > governorMaxAddTokens {
		return fmt.Errorf("addTokens out of range (0..%d)", governorMaxAddTokens)
	}
	if addCost < 0 || addCost > governorMaxAddCostUsd {
		return fmt.Errorf("addCostUsd out of range (0..%g)", governorMaxAddCostUsd)
	}
	b.governor.bumpBudget(addTokens, addCost)
	b.GovernorResume()
	return nil
}

// GovernorStatus snapshots the current control state for the HTTP/SSE surface.
func (b *Broker) GovernorStatus() governorStatus {
	if b == nil || b.governor == nil {
		return governorStatus{}
	}
	tok, cost := b.sessionUsageSnapshot()
	return b.governor.status(tok, cost)
}

// governorPauseMessage renders the #general review notice. WUPHF voice: honest,
// a little wry (The Office), no em-dashes.
func governorPauseMessage(reason pauseReason, tokens int, cost float64) string {
	switch reason {
	case pauseBudget:
		return fmt.Sprintf(
			"Pumping the brakes. The team has burned %s (about $%.2f) this stretch. Take a look at the work so far, then hit Continue to keep going or Stop to call it.",
			humanizeTokens(tokens), cost)
	case pauseTurns:
		return "Pumping the brakes for a checkpoint. The team has run a stretch of turns without a human in the loop. Review the work so far, then Continue or Stop."
	case pauseStop:
		return "Stopped. In-flight work was cancelled. Resume when you are ready."
	default:
		return "Paused. Review the work so far, then Continue or Stop."
	}
}

// humanizeTokens renders a token count as e.g. "152k tokens" / "980 tokens".
func humanizeTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk tokens", n/1000)
	}
	return fmt.Sprintf("%d tokens", n)
}

// publishGovernorEvent fans the current status out to SSE subscribers.
func (b *Broker) publishGovernorEvent() {
	st := b.GovernorStatus()
	b.mu.Lock()
	for _, ch := range b.governorSubscribers {
		select {
		case ch <- st:
		default:
		}
	}
	b.mu.Unlock()
}

// SubscribeGovernor returns a channel of governor status updates plus an
// unsubscribe func, mirroring SubscribeActions et al. The SSE loop uses it to
// push "governor" events to the browser.
func (b *Broker) SubscribeGovernor(buffer int) (<-chan governorStatus, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan governorStatus, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	if b.governorSubscribers == nil {
		b.governorSubscribers = make(map[int]chan governorStatus)
	}
	b.governorSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.governorSubscribers[id]; ok {
			delete(b.governorSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// governorActionRequest is the POST /governor body.
type governorActionRequest struct {
	Action     string  `json:"action"` // pause | stop | resume | resume_more
	Slug       string  `json:"slug,omitempty"`
	AddTokens  int     `json:"addTokens,omitempty"`
	AddCostUsd float64 `json:"addCostUsd,omitempty"`
}

// handleGovernor serves GET (status) and POST (control) on /governor.
func (b *Broker) handleGovernor(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, b.GovernorStatus())
	case http.MethodPost:
		var req governorActionRequest
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, governorActionMaxBodyBytes)
			defer r.Body.Close()
			// An empty body (io.EOF) is allowed for action-only callers; any
			// other decode error is a 400.
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}
		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "pause":
			b.GovernorPause()
		case "stop":
			b.GovernorStop(req.Slug)
		case "resume":
			b.GovernorResume()
		case "resume_more":
			if err := b.GovernorResumeMore(req.AddTokens, req.AddCostUsd); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, b.GovernorStatus())
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
