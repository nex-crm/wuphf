package team

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// broker_execute.go backs the EXECUTE half: the operator's "Run" drives their
// REAL browser via cua-driver. The browser computer-use loop lives in a Python
// runner (runner/cua_exec.py) — it reads the window's accessibility tree, lets
// OpenAI pick an action, and executes it through cua-driver. This endpoint
// spawns that runner and proxies its newline-JSON event stream to the FE as SSE.
//
// Two security invariants mirror the realtime call:
//   - The long-lived OpenAI key is passed via the ENVIRONMENT only — never on the
//     argv (where `ps` would leak it) and never to the browser.
//   - The operator-supplied goal is passed as an argv element to exec.Command (no
//     shell), so it cannot inject a command.
//
// With no key or no runner on disk it returns 503 so the FE falls back to the
// scripted mock. See docs/specs/operator-cua-migration.md.

// cuaRunnerPath resolves runner/cua_exec.py: WUPHF_CUA_RUNNER env override, else
// the repo cwd. Empty when not found so the handler can 503.
func cuaRunnerPath() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_CUA_RUNNER")); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
		return ""
	}
	if cwd, err := os.Getwd(); err == nil {
		p := filepath.Join(cwd, "runner", "cua_exec.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// cuaPython is the interpreter that runs the runner. Overridable so the test can
// point it at a fake runner and so a packaged build can ship a pinned Python.
func cuaPython() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_CUA_PYTHON")); v != "" {
		return v
	}
	return "python3"
}

type executeBrowserRequest struct {
	Goal     string `json:"goal"`
	App      string `json:"app,omitempty"`
	WindowID int    `json:"window_id,omitempty"`
}

// decodeExecuteBrowserRequest parses + validates the body. The goal is required;
// it is capped so a runaway prompt cannot bloat the argv.
func decodeExecuteBrowserRequest(r *http.Request) (executeBrowserRequest, error) {
	var req executeBrowserRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		return req, fmt.Errorf("invalid json")
	}
	req.Goal = strings.TrimSpace(req.Goal)
	if req.Goal == "" {
		return req, fmt.Errorf("missing goal")
	}
	if len(req.Goal) > 2000 {
		req.Goal = req.Goal[:2000]
	}
	req.App = strings.TrimSpace(req.App)
	return req, nil
}

// handleExecuteBrowser spawns the cua runner for one goal and streams its events
// as SSE. The request context cancels the subprocess on client disconnect (or
// Stop), so a run never outlives the modal that started it.
func (b *Broker) handleExecuteBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeExecuteBrowserRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(config.ResolveOpenAIAPIKey())
	if key == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "no OpenAI key configured",
		})
		return
	}
	runner := cuaRunnerPath()
	if runner == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "cua runner not available",
		})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	cmd := exec.CommandContext(r.Context(), cuaPython(), executeBrowserArgs(runner, req)...)
	// Key ONLY via env — never argv, never the browser.
	cmd.Env = append(os.Environ(), "OPENAI_API_KEY="+key)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "runner pipe failed"})
		return
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "runner start failed"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Each runner line is already a JSON event ({type:status|action|done|error});
	// forward verbatim as one SSE data frame.
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			break
		}
		flusher.Flush()
	}
	_ = cmd.Wait()
	// Closing boundary so the client always learns the run ended, even if the
	// runner died without a terminal done/error line.
	fmt.Fprint(w, "event: end\ndata: {}\n\n")
	flusher.Flush()
}

// executeBrowserArgs builds the runner argv. The goal/app are argv elements (no
// shell) and window_id is an int, so none of them can inject a command.
func executeBrowserArgs(runner string, req executeBrowserRequest) []string {
	args := []string{runner, "--goal", req.Goal}
	if app := strings.TrimSpace(req.App); app != "" {
		args = append(args, "--app", app)
	}
	if req.WindowID > 0 {
		args = append(args, "--window-id", strconv.Itoa(req.WindowID))
	}
	return args
}
