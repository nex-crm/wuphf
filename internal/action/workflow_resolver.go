package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The real WorkflowActionResolver: it binds a semantic plan step to a concrete
// Composio action by searching the catalog and asking an LLM to pick the action,
// map its params, and (for a gated step) author the deterministic run_if gate.
// This is the one place AI lives in build->run — everything it produces is baked
// into the definition and then executed deterministically.
//
// Dependencies are func types, not concrete clients, so this file imports neither
// the Composio HTTP client nor the provider package (no import cycle): the broker
// wires SearchActions and provider.RunConfiguredOneShotCtx in. That also makes
// the resolver unit-testable with fakes — no network, no model.

// ActionSearchFunc returns candidate Composio actions for a platform + intent.
type ActionSearchFunc func(ctx context.Context, platform, query string) ([]Action, error)

// LLMCompleteFunc runs one synchronous system+user prompt and returns the text.
type LLMCompleteFunc func(ctx context.Context, system, user string) (string, error)

// ComposioActionResolver implements WorkflowActionResolver against the real
// catalog + model. It fails SAFE: if the catalog or model is unavailable or the
// output is unusable, it narrates the step as a template (no external mutation)
// so a build never breaks and nothing is sent on a guess.
type ComposioActionResolver struct {
	search ActionSearchFunc
	llm    LLMCompleteFunc
}

// NewComposioActionResolver builds the resolver from its two injected deps.
func NewComposioActionResolver(search ActionSearchFunc, llm LLMCompleteFunc) *ComposioActionResolver {
	return &ComposioActionResolver{search: search, llm: llm}
}

const resolverSystemPrompt = `You bind ONE step of an automation to a Composio action.
Return ONLY a JSON object, no prose, with this shape:
{"action_id": "<one of the candidate ids>", "params": { ... }, "run_if": "<gate or empty>"}
Rules:
- action_id MUST be exactly one of the candidate action ids provided.
- params maps the action's inputs. Reference earlier data with template refs like
  "{{ steps.<step_id>.result.<field> }}" or "{{ inputs.<name> }}". Use {} if none.
- run_if is a SINGLE deterministic comparison that gates this step, e.g.
  "steps.<step_id>.result.fit >= 80", or "" when the step always runs. Only set it
  when the plan says this step should be conditional (a threshold or a decision).`

func (r *ComposioActionResolver) Resolve(ctx context.Context, plan Plan, step PlanStep) (BoundStep, error) {
	kind := strings.ToLower(strings.TrimSpace(step.Kind))
	if kind == "trigger" {
		return BoundStep{Skip: true}, nil
	}
	if kind == "browser" {
		// No integration for this step → Nex drives the browser. The sub-goal
		// rides in the template; the broker's browser-step executor runs it via
		// cua (there is no Composio action to bind).
		goal := strings.TrimSpace(step.Detail)
		if goal == "" {
			goal = stepLabel(step)
		}
		return BoundStep{Type: "browser", Template: goal}, nil
	}

	integration := strings.TrimSpace(step.Integration)
	if integration == "" {
		// No external system: judgment becomes an AI nex_ask step; everything else
		// is a narration template (decision/branch gating rides on action run_if).
		if kind == "ai" || kind == "decision" {
			return BoundStep{Type: "nex_ask", QueryTemplate: stepQuery(step)}, nil
		}
		return BoundStep{Type: "template", Template: stepLabel(step)}, nil
	}

	platform := resolverPlatformSlug(integration)
	fallback := BoundStep{Type: "template", Template: stepLabel(step) + " (via " + integration + ")"}

	if r.search == nil || r.llm == nil {
		return fallback, nil
	}
	// Search/LLM failures degrade to the template fallback rather than failing
	// the whole bind: collapse an error into the empty result so the fallback
	// decision is driven by "no usable candidate/output", never by returning a
	// swallowed error as nil.
	candidates, err := r.search(ctx, platform, stepQuery(step))
	if err != nil {
		candidates = nil
	}
	if len(candidates) == 0 {
		return fallback, nil
	}

	out, err := r.llm(ctx, resolverSystemPrompt, resolverUserPrompt(plan, step, platform, candidates))
	if err != nil {
		out = ""
	}
	bound, ok := parseResolverOutput(out, candidates, platform)
	if !ok {
		return fallback, nil
	}
	return bound, nil
}

func resolverUserPrompt(plan Plan, step PlanStep, platform string, candidates []Action) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow: %s\n", strings.TrimSpace(plan.Name))
	b.WriteString("Steps (in order):\n")
	for _, s := range plan.Steps {
		fmt.Fprintf(&b, "- id=%s kind=%s: %s\n", strings.TrimSpace(s.ID), strings.TrimSpace(s.Kind), strings.TrimSpace(s.Title))
	}
	fmt.Fprintf(&b, "\nBind this step: id=%s, %q\n", strings.TrimSpace(step.ID), stepLabel(step))
	fmt.Fprintf(&b, "Platform: %s. Gated (needs a conditional gate): %v\n", platform, step.Gated)
	b.WriteString("Candidate actions:\n")
	for _, c := range candidates {
		fmt.Fprintf(&b, "- %s: %s\n", strings.TrimSpace(c.ActionID), strings.TrimSpace(c.Title))
	}
	return b.String()
}

// parseResolverOutput extracts the JSON verdict and validates the chosen action
// against the candidate set (so the model cannot invent an action id). Returns
// ok=false on any problem, which routes the caller to its safe fallback.
func parseResolverOutput(raw string, candidates []Action, platform string) (BoundStep, bool) {
	obj := extractJSONObject(raw)
	if obj == "" {
		return BoundStep{}, false
	}
	var parsed struct {
		ActionID string         `json:"action_id"`
		Params   map[string]any `json:"params"`
		RunIf    string         `json:"run_if"`
	}
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return BoundStep{}, false
	}
	actionID := strings.TrimSpace(parsed.ActionID)
	if !candidateHasAction(candidates, actionID) {
		return BoundStep{}, false
	}
	runIf := strings.TrimSpace(parsed.RunIf)
	if runIf != "" {
		if _, err := parseRunIf(runIf); err != nil {
			// A malformed gate is dropped rather than failing the whole bind; the
			// step then always runs (the gated mutation is still approval-protected
			// upstream). Keeping the action is better than discarding it.
			runIf = ""
		}
	}
	return BoundStep{
		Type:     "action",
		Platform: platform,
		ActionID: actionID,
		Params:   parsed.Params,
		RunIf:    runIf,
	}, true
}

func candidateHasAction(candidates []Action, actionID string) bool {
	for _, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c.ActionID), actionID) {
			return true
		}
	}
	return false
}

// extractJSONObject returns the first {...} block in s, tolerating code fences
// and surrounding prose the model may add despite the instruction.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func stepQuery(step PlanStep) string {
	parts := []string{strings.TrimSpace(step.Title), strings.TrimSpace(step.Detail)}
	out := strings.TrimSpace(strings.Join(parts, " "))
	if out == "" {
		return strings.TrimSpace(step.Kind)
	}
	return out
}

func stepLabel(step PlanStep) string {
	if title := strings.TrimSpace(step.Title); title != "" {
		return title
	}
	if detail := strings.TrimSpace(step.Detail); detail != "" {
		return detail
	}
	return "step"
}

// resolverPlatformSlug turns a display integration name ("HubSpot CRM") into a
// lowercase toolkit slug ("hubspot_crm").
func resolverPlatformSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return "integration"
	}
	return slug
}
