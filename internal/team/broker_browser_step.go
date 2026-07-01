package team

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
)

// broker_browser_step.go wires the workflow engine's browser-step hook
// (action.BrowserStepRunner) to cua: when a frozen workflow reaches a `browser`
// step on a REAL (non-dry) run, the broker drives the browser to accomplish the
// step's goal via the cua runner.
//
// Slice 3b — the ask is now conversational. Before driving, the run PAUSES and
// asks the operator IN THE APP CHAT for permission to control the browser; a
// send inside the step pauses and asks again. Both asks go through
// browserApprovals (surfaced by GET/POST .../workflow/browser/pending|approve).
// The app id is threaded on the run context; with no app id (scheduler/cron/
// headless run) there is no operator to ask, so the step is skipped, never
// driven. With no key/runner on the host it degrades to a "skipped" marker so a
// frozen run still completes. See docs/specs/operator-browser-step.md.

func init() {
	action.BrowserStepRunner = runBrowserStepViaCua
}

func runBrowserStepViaCua(ctx context.Context, goal string) (map[string]any, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return map[string]any{"skipped": "empty goal"}, nil
	}
	// No app id → no operator chat to ask → do not seize the browser.
	appID, _ := ctx.Value(browserStepAppIDKey).(string)
	if strings.TrimSpace(appID) == "" {
		return map[string]any{"skipped": "browser control needs operator approval"}, nil
	}
	// Ask permission to control the browser for this step, in chat. The run stays
	// paused until the operator replies (or the ask times out → deny).
	if !browserApprovals.ask(ctx, appID, browserApprovalControl, goal) {
		return map[string]any{"skipped": "browser control not approved"}, nil
	}

	key := strings.TrimSpace(config.ResolveOpenAIAPIKey())
	runner := cuaRunnerPath("cua_exec.py", "WUPHF_CUA_RUNNER")
	if key == "" || runner == "" {
		// No key/runner on this host → tolerate; the frozen run still completes.
		return map[string]any{"skipped": "browser execution unavailable on this host"}, nil
	}

	cmd := exec.CommandContext(ctx, cuaPython(), runner, "--goal", goal)
	cmd.Env = append(os.Environ(), "OPENAI_API_KEY="+key)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	actions := []string{}
	var result, errMsg string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var e map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &e) != nil {
			continue
		}
		switch e["type"] {
		case "approval_request":
			// An external send inside the step: ask the operator in chat, then
			// forward the decision to the runner's stdin send-gate.
			label, _ := e["label"].(string)
			if browserApprovals.ask(ctx, appID, browserApprovalSend, label) {
				_, _ = stdin.Write([]byte("approve\n"))
			} else {
				_, _ = stdin.Write([]byte("deny\n"))
			}
		case "action":
			if l, _ := e["label"].(string); l != "" {
				actions = append(actions, l)
			}
		case "done":
			result, _ = e["result"].(string)
		case "error":
			errMsg, _ = e["message"].(string)
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	// Surface a crash / kill / truncated-scan as an error so the caller does not
	// treat an aborted step as a clean success. A runner-emitted "error" event
	// wins (it is more specific); ctx cancellation is an intentional stop, not a
	// failure.
	if scanErr := scanner.Err(); errMsg == "" && scanErr != nil {
		errMsg = "runner output error: " + scanErr.Error()
	}
	if errMsg == "" && waitErr != nil && ctx.Err() == nil {
		errMsg = "runner exited abnormally: " + waitErr.Error()
	}

	out := map[string]any{"actions": actions, "actions_count": len(actions)}
	if result != "" {
		out["result"] = result
	}
	if errMsg != "" {
		out["error"] = errMsg
	}
	return out, nil
}
