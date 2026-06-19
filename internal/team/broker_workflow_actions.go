package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// broker_workflow_actions.go makes a frozen workflow's run do REAL work and
// surface the artifact it produced, instead of only replaying the state machine.
//
// It is DOMAIN-AGNOSTIC. There is no Gmail-specific (or any provider-specific)
// Go here: an integration read is fully described by the SPEC the builder agent
// authored — Platform + ActionID + Params (call args) + ResultPath/Expose
// (response projection) — and one generic executor runs it. The platform owns
// the generic mechanics (allow-list enforcement, fail-loud oversize handling,
// projection, size reduction); the agent owns the integration-specific policy
// (which query, which flags, which fields). See docs/specs/large-io-framework.md.
//
// This file runs ONLY in the live exec path (makeWorkflowActionExecWithGate).
// shipcheck/replay uses the pure deterministic recordingExec, so the real model
// and integration calls here never touch the determinism/idempotency proofs.

// llmActionTimeout bounds a single in-run model call.
const llmActionTimeout = 90 * time.Second

// integrationReadCap overrides the provider response cap for a workflow read.
// Metadata-mode responses are small; this leaves headroom while still failing
// loud (not truncating) if a step pulls full payloads it should have trimmed.
const integrationReadCap int64 = 8 << 20

// execIntegrationAction runs a spec-declared integration read for real. The
// allow-list check (D6) is enforced by the caller-supplied allow predicate (the
// spec's AllowedReads) BEFORE any provider call. Oversize fails loud as a
// structured outcome (D5) rather than silently truncating. The response is
// projected to the spec's exposed fields and size-reduced before it threads into
// step data — so raw provider bodies never reach an LLM prompt or the run record.
func (b *Broker) execIntegrationAction(ctx context.Context, a workflow.Action, allow func(platform, actionID string) bool) workflow.ActionOutcome {
	if allow != nil && !allow(a.Platform, a.ActionID) {
		return workflow.ActionOutcome{OK: false, Err: fmt.Sprintf("integration read %s/%s is not allowed by this workflow", a.Platform, a.ActionID)}
	}
	composio := action.NewComposioFromEnv()
	if !composio.Configured() {
		return workflow.ActionOutcome{OK: false, Err: "integration provider not configured"}
	}
	key := activeConnectionKey(ctx, composio, a.Platform)
	if key == "" {
		return workflow.ActionOutcome{OK: false, Err: fmt.Sprintf("no active %s connection", a.Platform)}
	}
	res, err := composio.ExecuteAction(ctx, action.ExecuteRequest{
		Platform:         a.Platform,
		ActionID:         a.ActionID,
		ConnectionKey:    key,
		Data:             a.Params,
		MaxResponseBytes: integrationReadCap,
	})
	if err != nil {
		var tooLarge *action.ResultTooLargeError
		if errors.As(err, &tooLarge) {
			// Fail loud: the builder agent reacts (trim params / metadata mode /
			// paginate) and re-freezes — the platform does not silently retry.
			return workflow.ActionOutcome{OK: false, Err: tooLarge.Error(), Output: map[string]any{
				a.ID + "_error": "result_too_large",
				a.ID + "_limit": tooLarge.Limit,
			}}
		}
		return workflow.ActionOutcome{OK: false, Err: err.Error()}
	}
	projected, count := projectResult(res.Response, a.ResultPath, a.Expose)
	reduced, red := workflow.Reduce(projected, workflow.DefaultRunRecordBudget)
	out := map[string]any{
		a.ID:            reduced,
		a.ID + "_count": count,
	}
	if red.Truncated {
		out[a.ID+"_reduction"] = red // per-action key so multiple reads don't collide
	}
	return workflow.ActionOutcome{OK: true, Output: out}
}

// activeConnectionKey returns the first ACTIVE connection key for a platform.
// Generic across providers (no Gmail special-case).
func activeConnectionKey(ctx context.Context, composio *action.ComposioREST, platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return ""
	}
	conns, err := composio.ListConnections(ctx, action.ListConnectionsOptions{Search: platform, Limit: 50})
	if err != nil {
		return ""
	}
	for _, c := range conns.Connections {
		if strings.EqualFold(c.Platform, platform) && strings.EqualFold(c.State, "active") {
			return c.Key
		}
	}
	return ""
}

// projectResult extracts the array at resultPath (dot-path, e.g. "data.messages")
// and projects each item to the exposed field paths (e.g. "sender",
// "preview.body"), returning the projected array as JSON plus its length. When
// resultPath does not resolve to an array, the whole response is returned as-is
// (still size-reduced by the caller). Projection is what keeps RAW provider
// bodies — which may carry secrets/attachments — out of the data that flows into
// LLM prompts and the run record: only the exposed fields survive.
func projectResult(raw json.RawMessage, resultPath string, expose []string) (json.RawMessage, int) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return raw, 0
	}
	node := root
	if p := strings.TrimSpace(resultPath); p != "" {
		node = walkPath(root, p)
	}
	arr, ok := node.([]any)
	if !ok {
		// No array at the path — pass the whole (still-reduced) response through.
		return raw, 0
	}
	if len(expose) == 0 {
		out, _ := json.Marshal(arr)
		return out, len(arr)
	}
	projected := make([]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			projected = append(projected, item)
			continue
		}
		row := make(map[string]any, len(expose))
		for _, field := range expose {
			if v := walkPath(m, field); v != nil {
				row[leafKey(field)] = v
			}
		}
		projected = append(projected, row)
	}
	out, _ := json.Marshal(projected)
	return out, len(projected)
}

// walkPath follows a dot-separated path through nested maps and returns the
// value, or nil if any segment is missing.
func walkPath(v any, path string) any {
	for _, seg := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[seg]
		if v == nil {
			return nil
		}
	}
	return v
}

// leafKey is the last segment of a dot-path, used as the projected field's key
// (e.g. "preview.body" -> "body").
func leafKey(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// ── LLM step execution (generic; real model call) ──────────────────────────

// execLLMAction runs an llm-kind workflow step as a REAL model call against the
// office's configured default provider (the same one the agents use). The prompt
// is built from the action's description plus the data threaded in by earlier
// steps (the projected integration results). On any provider error or empty
// output it falls back to a labelled deterministic baseline so a run never
// hard-fails — but the happy path is genuine AI.
func (b *Broker) execLLMAction(ctx context.Context, a workflow.Action, data map[string]any) workflow.ActionOutcome {
	system, prompt := llmPromptFor(a, data)
	callCtx, cancel := context.WithTimeout(ctx, llmActionTimeout)
	defer cancel()
	out, err := provider.RunConfiguredOneShotCtx(callCtx, system, prompt, "")
	text := strings.TrimSpace(out)
	if err != nil || text == "" {
		note := "provider unavailable"
		if err != nil {
			note = err.Error()
		}
		baseline := fmt.Sprintf("[baseline — %s] %s", note, a.ID)
		return workflow.ActionOutcome{OK: true, Output: map[string]any{
			outputKeyFor(a): baseline,
			"draft":         baseline,
			"llm_used":      false,
		}}
	}
	return workflow.ActionOutcome{OK: true, Output: map[string]any{
		outputKeyFor(a): text,
		"draft":         text,
		"llm_used":      true,
	}}
}

// outputKeyFor names the primary output of an llm step for downstream + the UI:
// "digest" for a compose-style step, "summary" for a summarize step, else the
// action id. This is presentation/threading convention, not domain execution
// logic — the step still runs through the same generic model call.
func outputKeyFor(a workflow.Action) string {
	id := strings.ToLower(a.ID)
	switch {
	case strings.Contains(id, "digest") || strings.Contains(id, "compose"):
		return "digest"
	case strings.Contains(id, "summar"):
		return "summary"
	default:
		return a.ID
	}
}

// llmPromptFor builds the (system, user) prompt for an llm step from the action's
// description and the available step context.
func llmPromptFor(a workflow.Action, data map[string]any) (string, string) {
	ctx := renderContext(data)
	desc := strings.TrimSpace(a.Description)
	id := strings.ToLower(a.ID)
	switch {
	case strings.Contains(id, "summar"):
		if desc == "" {
			desc = "Summarize the items below concisely. Call out anything that clearly needs a reply. Do not invent content."
		}
		return "You summarize a busy operator's data concisely and honestly.", desc + "\n\nData:\n" + ctx
	case strings.Contains(id, "digest") || strings.Contains(id, "compose"):
		if desc == "" {
			desc = "Write a concise morning digest from the items below. Put anything that needs a reply first, marked ⚠️. End with a one-line count. Do not invent anything."
		}
		return "You write a short, friendly digest. Lead with anything that needs a reply, marked ⚠️.", desc + "\n\n" + ctx
	default:
		if desc == "" {
			desc = "Complete the step " + a.ID + " using the context below."
		}
		return "You are a workflow step executing for an operations team.", desc + "\n\nContext:\n" + ctx
	}
}

// renderContext renders the prior step outputs into readable prompt text. It
// skips internal/meta keys (counts, reductions, errors, drafts) and marshals each
// remaining value — which may be a projected array, a string, or json.RawMessage
// — uniformly. Only EXPOSED, REDUCED data reaches here (projectResult + Reduce),
// never raw provider bodies.
func renderContext(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if isInternalOutputKey(k) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "(no prior context)"
	}
	var b strings.Builder
	for _, k := range keys {
		v := data[k]
		if s, ok := v.(string); ok {
			fmt.Fprintf(&b, "%s:\n%s\n\n", k, s)
			continue
		}
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s:\n%s\n\n", k, string(raw))
	}
	return strings.TrimRight(b.String(), "\n")
}

// isInternalOutputKey reports whether an Output key is platform bookkeeping (not
// content to render into a prompt). Keeps reduction markers / counts / errors out
// of the model's context (also relevant to the self-heal miner ignoring them).
func isInternalOutputKey(k string) bool {
	if k == "llm_used" || k == "draft" {
		return true
	}
	for _, suffix := range []string{"_count", "_reduction", "_error", "_limit", "_sent", "_note", "_fallback"} {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// sendVerbs name steps that reach OUT of the system (a real side effect). A
// deterministic step matching one but lacking an integration target is reported
// as a draft, never a phantom send.
var sendVerbs = []string{"send", "email", "post", "notify", "announce", "publish", "deliver", "dm", "message", "slack", "tweet"}

func looksLikeSendAction(a workflow.Action) bool {
	hay := strings.ToLower(a.ID + " " + a.Description)
	for _, v := range sendVerbs {
		if strings.Contains(hay, v) {
			return true
		}
	}
	return false
}
