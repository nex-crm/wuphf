package team

import (
	"net/http"
	"strconv"
	"strings"
)

// broker_observe.go backs the OBSERVE half of the demo call: while the operator
// demonstrates a workflow, cua-driver reads the REAL page (the AX component tree
// + visible text) instead of the AI guessing from screenshots. POST /observe
// runs runner/cua_observe.py and proxies its snapshot/navigate events to the FE
// as SSE; the FE accumulates them and folds them into the build handoff.
//
// No OpenAI key is needed — the observer only reads via cua-driver. With no
// runner on the host it returns 503 and the call simply proceeds without the
// structured capture. The request context cancels the observer when the call
// ends. See docs/specs/operator-cua-migration.md §7 (C5).

// handleObserveBrowser starts the observe capture loop and streams its events.
func (b *Broker) handleObserveBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runner := cuaRunnerPath("cua_observe.py", "WUPHF_CUA_OBSERVE_RUNNER")
	if runner == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "cua observer not available",
		})
		return
	}
	args := []string{runner}
	// Optional ?interval=<seconds> to tune the poll cadence; bounded so a tiny
	// value can't turn the capture into a busy loop.
	if iv := strings.TrimSpace(r.URL.Query().Get("interval")); iv != "" {
		if f, err := strconv.ParseFloat(iv, 64); err == nil && f >= 1 && f <= 30 {
			args = append(args, "--interval", strconv.FormatFloat(f, 'f', -1, 64))
		}
	}
	spawnRunnerSSE(w, r, args, nil)
}
