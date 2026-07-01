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
// Slice 3a runs cua synchronously and AUTO-DENIES any external send — a send
// inside a browser step needs the operator's in-chat approval, which is slice 3b
// (the browser-control permission + send-approval asked in the app chat). With
// no key/runner on the host it degrades to a "skipped" marker so the frozen run
// still completes. See docs/specs/operator-browser-step.md.

func init() {
	action.BrowserStepRunner = runBrowserStepViaCua
}

func runBrowserStepViaCua(ctx context.Context, goal string) (map[string]any, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return map[string]any{"skipped": "empty goal"}, nil
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
			// 3a: an external send inside a browser step is DENIED here — it needs
			// the in-chat approval that 3b will wire.
			_, _ = stdin.Write([]byte("deny\n"))
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
	_ = cmd.Wait()

	out := map[string]any{"actions": actions, "actions_count": len(actions)}
	if result != "" {
		out["result"] = result
	}
	if errMsg != "" {
		out["error"] = errMsg
	}
	return out, nil
}
