package team

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// broker_browser_approval.go is slice 3b of the browser-step reframe: a live
// workflow run that reaches a `browser` step no longer auto-denies. Instead it
// PAUSES and asks the operator IN THE APP CHAT — first for permission to control
// the browser for the step, then again before any external send inside it. The
// operator's reply resumes (or skips) the paused run.
//
// The pause is an in-process rendezvous: the browser step blocks on a channel in
// browserApprovals; the app chat polls GET .../workflow/browser/pending to render
// the ask, and POST .../workflow/browser/approve resolves the channel. The run's
// HTTP request stays open across the wait — the same held-open model the
// send-gate already uses on the standalone execute stream. Default is DENY: a
// disconnect, a timeout, or an unresolved ask never sends and never seizes the
// browser. (Durable cross-restart run state is a later hardening; a broker
// restart mid-pause simply denies, which is safe.) See
// docs/specs/operator-browser-step.md.

// ctxKey is a private type for context values this package threads through a run.
type ctxKey string

// browserStepAppIDKey carries the operator app id into ExecuteWorkflow so a
// browser step knows which app's chat to ask. Absent (scheduler/cron/headless
// runs) means there is no operator to ask — the step is skipped, never driven.
const browserStepAppIDKey ctxKey = "operator_app_id"

// browserApprovalKind distinguishes the two asks a browser step can raise.
const (
	browserApprovalControl = "control" // permission to drive the browser for the step
	browserApprovalSend    = "send"    // an external send inside the step
)

// browserApprovalTimeout bounds how long a paused step waits for a chat reply
// before defaulting to deny, so a forgotten run cannot hold a browser hostage.
const browserApprovalTimeout = 3 * time.Minute

// browserApproval is one pending in-chat ask. The channel is buffered so resolve
// never blocks even if the waiting step has already given up (timeout/cancel).
type browserApproval struct {
	ID      string `json:"id"`
	AppID   string `json:"app_id"`
	Kind    string `json:"kind"`
	Goal    string `json:"goal"`
	seq     uint64
	ch      chan bool
	settled bool // guarded by browserApprovalRegistry.mu: exactly one of {expire, resolve} wins
}

type browserApprovalRegistry struct {
	mu  sync.Mutex
	m   map[string]*browserApproval
	seq uint64
}

var browserApprovals = &browserApprovalRegistry{m: map[string]*browserApproval{}}

func newApprovalID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ask registers a pending approval for the app and BLOCKS until the operator
// resolves it in chat, the run's context is cancelled, or the timeout elapses.
// It returns false (deny) on cancel or timeout — the safe default.
func (g *browserApprovalRegistry) ask(ctx context.Context, appID, kind, goal string) bool {
	g.mu.Lock()
	g.seq++
	a := &browserApproval{ID: newApprovalID(), AppID: appID, Kind: kind, Goal: goal, seq: g.seq, ch: make(chan bool, 1)}
	g.m[a.ID] = a
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.m, a.ID)
		g.mu.Unlock()
	}()

	timer := time.NewTimer(browserApprovalTimeout)
	defer timer.Stop()
	select {
	case allow := <-a.ch:
		return allow
	case <-ctx.Done():
		return g.expire(a)
	case <-timer.C:
		return g.expire(a)
	}
}

// expire claims the approval for the timeout/cancel path and denies. If resolve
// already claimed it first (a narrow window where the operator replied just as
// the step gave up), drain the decision it delivered instead of overriding it,
// so exactly one of {expire, resolve} wins.
func (g *browserApprovalRegistry) expire(a *browserApproval) bool {
	g.mu.Lock()
	if a.settled {
		g.mu.Unlock()
		return <-a.ch
	}
	a.settled = true
	delete(g.m, a.ID)
	g.mu.Unlock()
	return false
}

// resolve delivers the operator's decision to a waiting step. False when the id
// is unknown (already resolved, timed out, expired, or from another run).
func (g *browserApprovalRegistry) resolve(id string, allow bool) bool {
	return g.resolveForApp("", id, allow)
}

// resolveForApp is resolve scoped to an app: it refuses an approval that belongs
// to a different app so one app's endpoint cannot resolve another app's ask. An
// empty appID skips the ownership check (in-process callers that already trust
// the id). It claims the approval under the lock (settle-once), then delivers
// the decision into the buffered channel.
func (g *browserApprovalRegistry) resolveForApp(appID, id string, allow bool) bool {
	g.mu.Lock()
	a, ok := g.m[id]
	if !ok || a.settled || (appID != "" && a.AppID != appID) {
		g.mu.Unlock()
		return false
	}
	a.settled = true
	delete(g.m, a.ID)
	g.mu.Unlock()
	a.ch <- allow // buffered (cap 1); the waiting step or expire drains it
	return true
}

// pendingFor returns the app's currently paused asks, oldest first, so the chat
// renders them in the order they were raised.
func (g *browserApprovalRegistry) pendingFor(appID string) []browserApproval {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []browserApproval{}
	for _, a := range g.m {
		if a.AppID == appID {
			out = append(out, browserApproval{ID: a.ID, AppID: a.AppID, Kind: a.Kind, Goal: a.Goal, seq: a.seq})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// getOperatorAppBrowserPending lists the app's paused browser-step asks so the
// app chat can render them (poll while a live run is in flight).
func (b *Broker) getOperatorAppBrowserPending(w http.ResponseWriter, _ *http.Request, appID string) {
	writeJSON(w, http.StatusOK, map[string]any{"pending": browserApprovals.pendingFor(appID)})
}

// The two decision literals the FE (resolveBrowserApproval) ever sends. Kept as a
// closed set so a malformed decision fails fast instead of silently denying.
const (
	browserDecisionApprove = "approve" // resume the step (drive / send)
	browserDecisionDeny    = "deny"    // skip the step
)

type browserApproveRequest struct {
	ApprovalID string `json:"approval_id"`
	// One of browserDecisionApprove / browserDecisionDeny; anything else is a 400.
	Decision string `json:"decision"`
}

// resolveOperatorAppBrowserApproval forwards the operator's in-chat decision to
// the paused browser step. 404 when the ask is unknown (already resolved or the
// run moved on), so the UI can drop a stale card.
func (b *Broker) resolveOperatorAppBrowserApproval(w http.ResponseWriter, r *http.Request, appID string) {
	var req browserApproveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ApprovalID == "" {
		http.Error(w, "missing approval_id", http.StatusBadRequest)
		return
	}
	// Validate the decision BEFORE resolving: a malformed value must fail fast with
	// a 400 rather than fall through as a silent deny that consumes the pending ask.
	if req.Decision != browserDecisionApprove && req.Decision != browserDecisionDeny {
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	// Scope the resolve to the app in the URL: an approval id from another app
	// must not be resolvable through this app's endpoint.
	if !browserApprovals.resolveForApp(appID, req.ApprovalID, req.Decision == browserDecisionApprove) {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
