package team

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// Workflow-detection trace substrate. The per-turn manifest (event_sink.go)
// records only WHICH integration actions ran — enough for the cheap recurrence
// gate, but not enough to extract a real, parameterized workflow. This file
// persists the richer signal an LLM extractor needs to read AFTER a task
// completes: for each integration action, its action_id + the (masked) call
// arguments + a shape-preserving summary of the response. It is deliberately
// scoped to integration-proxy calls (team_action_execute) — the domain work —
// and never plain harness tools.
//
// PII discipline: this trace is read into an LLM prompt, so it must not carry
// raw email bodies, message text, or credentials. Arguments redact a denylist
// of body/content/secret keys and cap every string; the response is reduced
// (workflow.Reduce) to a size-bounded, structure-preserving view so the
// extractor can infer result_path/expose from field NAMES without seeing the
// values. Design: docs/specs/large-io-framework.md (expose-field masking).

// ActionTrace is one persisted integration action within a task: what ran, with
// what (masked) inputs, and a bounded view of what came back. A task's ordered
// ActionTraces are the corpus the completion-time extractor reads.
type ActionTrace struct {
	TaskID   string         `json:"task_id"`
	TurnID   string         `json:"turn_id,omitempty"`
	Agent    string         `json:"agent,omitempty"`
	Seq      int            `json:"seq"`
	Platform string         `json:"platform,omitempty"`
	ActionID string         `json:"action_id"`
	Args     map[string]any `json:"args,omitempty"`
	Result   string         `json:"result,omitempty"`
	At       string         `json:"at,omitempty"`
}

const traceSinkFile = "action_traces.jsonl"

// traceSinkMu serializes appends so concurrent turns never interleave a line.
var traceSinkMu sync.Mutex

// traceArgStringCap bounds any single argument string so a long body that
// slipped past the denylist still cannot bloat the prompt.
const traceArgStringCap = 240

// traceResultCap bounds the reduced response summary persisted per action.
const traceResultCap = 2000

// piiArgKeys are argument keys whose VALUES are redacted outright — free-text
// the human/agent authored (an email body, a message, raw HTML) that the
// extractor never needs and must not see. Structural config keys (query,
// channel, max_results, label_ids, …) are kept so the extractor can fill
// Params. Matched case-insensitively by substring so "htmlBody"/"body_html"
// both hit.
var piiArgKeys = []string{
	"body", "html", "content", "text", "message", "snippet",
	"raw", "attachment", "payload", "note", "comment", "description",
	"password", "token", "secret", "credential", "authorization",
}

// maskArgValue redacts PII-keyed values and caps everything else. Nested maps
// are masked recursively; arrays are capped so a 500-recipient list cannot
// bloat the prompt.
func maskArgValue(key string, v any) any {
	if isPIIArgKey(key) {
		return "[redacted]"
	}
	switch t := v.(type) {
	case string:
		if len(t) > traceArgStringCap {
			return t[:traceArgStringCap] + "…"
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = maskArgValue(k, vv)
		}
		return out
	case []any:
		const cap = 5
		n := len(t)
		out := make([]any, 0, min(n, cap))
		for i := 0; i < n && i < cap; i++ {
			out = append(out, maskArgValue(key, t[i]))
		}
		if n > cap {
			out = append(out, "…(+"+strconv.Itoa(n-cap)+" more)")
		}
		return out
	default:
		return v
	}
}

func isPIIArgKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, p := range piiArgKeys {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// traceFromToolUse parses an integration-proxy tool_use into the static part of
// an ActionTrace (everything except the result). Returns ok=false for
// non-proxy tools or unparseable input so the caller skips tracing.
func traceFromToolUse(taskID, turnID, agent, toolName, toolInput string, seq int) (ActionTrace, bool) {
	if !isActionProxyTool(toolName) {
		return ActionTrace{}, false
	}
	var in struct {
		Platform        string         `json:"platform"`
		ActionID        string         `json:"action_id"`
		Data            map[string]any `json:"data"`
		PathVariables   map[string]any `json:"path_variables"`
		QueryParameters map[string]any `json:"query_parameters"`
	}
	if err := json.Unmarshal([]byte(toolInput), &in); err != nil {
		return ActionTrace{}, false
	}
	if strings.TrimSpace(in.ActionID) == "" {
		return ActionTrace{}, false
	}
	args := map[string]any{}
	addMasked := func(name string, m map[string]any) {
		if len(m) == 0 {
			return
		}
		masked := make(map[string]any, len(m))
		for k, v := range m {
			masked[k] = maskArgValue(k, v)
		}
		args[name] = masked
	}
	addMasked("data", in.Data)
	addMasked("path_variables", in.PathVariables)
	addMasked("query_parameters", in.QueryParameters)
	return ActionTrace{
		TaskID:   strings.TrimSpace(taskID),
		TurnID:   strings.TrimSpace(turnID),
		Agent:    strings.TrimSpace(agent),
		Seq:      seq,
		Platform: strings.ToLower(strings.TrimSpace(in.Platform)),
		ActionID: strings.TrimSpace(in.ActionID),
		Args:     args,
	}, true
}

// summarizeResult reduces a tool_result payload to a bounded,
// structure-preserving string. JSON is run through workflow.Reduce (caps
// strings + arrays, keeps keys) so the extractor sees the response SHAPE; plain
// text is truncated. Never returns raw multi-KB bodies.
func summarizeResult(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	if json.Valid([]byte(t)) {
		reduced, _ := workflow.Reduce(json.RawMessage(t), workflow.DefaultPromptBudget)
		t = string(reduced)
	}
	if len(t) > traceResultCap {
		return t[:traceResultCap] + "…"
	}
	return t
}

// TraceSinkPath returns the durable trace file under the runtime home, or ""
// when no home is resolvable (then persistence is a no-op). Exported so the
// extractor reads the same path the broker writes.
func TraceSinkPath() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "office", traceSinkFile)
}

// appendActionTrace writes one record as a JSON line under O_APPEND.
func appendActionTrace(path string, tr ActionTrace) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("trace sink path is empty")
	}
	line, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	traceSinkMu.Lock()
	defer traceSinkMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// persistActionTrace is the production hook: best-effort, never surfaces an
// error to the turn. No-op when no home is resolvable.
func persistActionTrace(tr ActionTrace) {
	path := TraceSinkPath()
	if path == "" {
		return
	}
	_ = appendActionTrace(path, tr)
}

// ReadActionTraces parses the trace sink (file order, oldest first). Corrupt
// lines are skipped; an absent file yields an empty slice, not an error.
func ReadActionTraces(path string) ([]ActionTrace, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ActionTrace{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []ActionTrace
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var tr ActionTrace
		if err := json.Unmarshal(line, &tr); err != nil {
			continue
		}
		out = append(out, tr)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// canonicalTaskID normalizes a task identity so traces tagged with a task's
// CHANNEL slug ("task-office-443") and its task ID ("OFFICE-443") collapse to
// the same key. Headless turns are tagged with whichever the trigger carried
// (a channel kick vs a task create), so without this a task's traces split in
// two and the extractor sees only half. Lower-cased, "task-" prefix stripped.
func canonicalTaskID(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "task-"))
}

// ActionTracesForTask returns the ordered traces for one task from the sink,
// matching by canonical identity so channel-slug and task-id taggings merge.
func ActionTracesForTask(path, taskID string) ([]ActionTrace, error) {
	all, err := ReadActionTraces(path)
	if err != nil {
		return nil, err
	}
	want := canonicalTaskID(taskID)
	var out []ActionTrace
	for _, tr := range all {
		if canonicalTaskID(tr.TaskID) == want {
			out = append(out, tr)
		}
	}
	return out, nil
}
