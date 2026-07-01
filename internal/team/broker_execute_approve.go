package team

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

// broker_execute_approve.go is the back-channel for send-gating: a live runner
// that is about to perform an EXTERNAL SEND (Slack/Gmail/post) pauses and emits
// an approval_request, blocking on its stdin. The operator's decision arrives as
// a SEPARATE request (POST /execute/approve), which we forward to that runner's
// stdin. A run is addressed by the run_id the broker emits as the first SSE
// event of the stream. See docs/specs/operator-cua-migration.md (send-gating).

// runStdinRegistry maps a run_id to the live runner's stdin so an approval
// request can reach the right paused run.
type runStdinRegistry struct {
	mu sync.Mutex
	m  map[string]io.Writer
}

var activeRuns = &runStdinRegistry{m: map[string]io.Writer{}}

func (g *runStdinRegistry) add(id string, w io.Writer) {
	g.mu.Lock()
	g.m[id] = w
	g.mu.Unlock()
}

func (g *runStdinRegistry) remove(id string) {
	g.mu.Lock()
	delete(g.m, id)
	g.mu.Unlock()
}

// writeLine forwards one decision line to the run's stdin. False if unknown.
func (g *runStdinRegistry) writeLine(id, line string) bool {
	g.mu.Lock()
	w, ok := g.m[id]
	g.mu.Unlock()
	if !ok {
		return false
	}
	_, err := io.WriteString(w, line+"\n")
	return err == nil
}

func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type executeApproveRequest struct {
	RunID string `json:"run_id"`
	// "approve" sends; anything else (incl. "deny") does NOT.
	Decision string `json:"decision"`
}

// handleExecuteApprove forwards the operator's send decision to a paused runner.
func (b *Broker) handleExecuteApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req executeApproveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.RunID == "" {
		http.Error(w, "missing run_id", http.StatusBadRequest)
		return
	}
	decision := "deny"
	if req.Decision == "approve" {
		decision = "approve"
	}
	if !activeRuns.writeLine(req.RunID, decision) {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
