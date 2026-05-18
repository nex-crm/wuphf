package team

// broker_ceo_draft.go — Phase 4 CEO draft writer.
//
// This file implements draftIssueLocked, the ONLY LLM call in the
// onboarding-into-office spec. It runs at the `draft` phase transition when
// the user describes a first issue (e.g. "Get Stripe webhooks working").
//
// Hard rules (from spec "## Eng review decisions → Phase 4"):
//   - THIS IS THE ONLY LLM CALL in the spec for onboarding. Do not add others.
//   - CEO voice: no "Welcome", no "I'm your AI", no preamble. Declarative,
//     low word count. CEO does not introduce himself.
//   - Output: four sections (Goal, Context, Approach, Acceptance) streamed
//     as individual CEO messages via appendMessageLocked so the SSE fan-out
//     reaches the frontend immediately per section.
//   - Idempotent: if IssueDraftSpec.DraftedAt is already set, return nil.
//   - Approval gate: do NOT call the LLM if the task is not in Drafting state.
//   - Phase 6 (sub-issues, wiki mirror) is deferred; do NOT touch it here.
//
// # Transport selection
//
// The draft writer uses a dual-transport strategy so it works out-of-the-box
// for both local developers and CI/headless servers.
//
// Preferred — claude CLI subprocess: when `claude` is on PATH the writer
// shells out to `claude -p "<prompt>" --output-format json`. This path uses
// the user's existing Claude OAuth session (set up once via `claude login`)
// so no API key is required. This matches the ICP persona (Sam the founder
// running wuphf locally after a single OAuth login) and avoids a separate
// ANTHROPIC_API_KEY configuration step.
//
// Fallback — HTTP direct: when the `claude` binary is not found (CI,
// headless servers, Docker) the writer falls through to the existing
// callAnthropicCEODraft / callOpenAICEODraft HTTP path gated on API key
// availability. If neither transport is viable, errCEODraftLLMDisabled is
// returned so the caller can surface a human-friendly nudge.
//
// Provider routing (HTTP path): re-uses callAnthropic / callOpenAI from
// skill_synth_provider.go (same package). Picks Anthropic key first;
// falls back to OpenAI. When neither is configured, returns a
// sentinel error so the caller can surface a user-visible nudge.
//
// Execution lineup card: after Approve & Start (Drafting→Running), the broker
// emits a ceo_execution_lineup card to the CEO DM channel proposing which
// agents to spin up. For blueprint-path issues the roster comes from the
// blueprint's picked agents. For scratch-path issues a separate small LLM
// call returns a JSON agent list.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// ceoDraftLookPath and ceoDraftCommandContext are package-level vars so tests
// can inject a fake claude binary without modifying PATH globally.
var (
	ceoDraftLookPath       = exec.LookPath
	ceoDraftCommandContext = exec.CommandContext
)

// errCEODraftLLMDisabled is returned when no LLM key is configured so the
// caller can surface a "configure a provider key to draft issues" hint.
var errCEODraftLLMDisabled = errors.New("ceo_draft: no Anthropic/OpenAI API key configured")

// ceoDraftSystemPrompt is the CEO voice system prompt. Hard-wired per spec:
// no "Welcome", no "I'm your AI", no preamble. Declarative, low word count.
// The CEO never introduces himself. This prompt is immutable — the spec
// treats it as a load-bearing regression target.
const ceoDraftSystemPrompt = `You are the CEO of a small AI-powered software team. Your role is to draft clear, concise issue specifications based on what the user wants to build.

Rules (non-negotiable):
- Do NOT start with "Welcome", "I'm your", "I am your", "Hello", "Hi", or any greeting.
- Do NOT introduce yourself or explain your role.
- Do NOT use preamble or filler phrases.
- Write declaratively. Short sentences. Low word count.
- Output ONLY valid JSON in the exact schema provided.
- Reflect the user's intent precisely. Do not invent requirements they didn't ask for.
- If wiki context is provided, use it to ground the spec. Do not make up facts.`

// ceoDraftUserPromptTpl is the user-turn template. Slots: userRequest, agentRoster, wikiContext.
const ceoDraftUserPromptTpl = `User request: %s

Available agents in this office: %s

%sReturn a JSON object with exactly these four string keys:
{
  "goal": "<one-sentence goal>",
  "context": "<2-4 sentences of relevant background>",
  "approach": "<3-5 bullet points of implementation steps>",
  "acceptance": "<3-5 testable acceptance criteria>"
}

Write as if you are filing a Linear issue. Be specific, not generic.`

// issueDraftLLMResponse is the expected JSON shape from the LLM.
type issueDraftLLMResponse struct {
	Goal       string `json:"goal"`
	Context    string `json:"context"`
	Approach   string `json:"approach"`
	Acceptance string `json:"acceptance"`
}

// draftIssueLocked drafts an Issue spec for taskID using an LLM call.
// It emits each section as a CEO message to the CEO DM channel so the
// SSE fan-out delivers them to the frontend in order.
//
// Idempotency: if task.IssueDraftSpec.DraftedAt is already set the
// function returns nil immediately (no second LLM call, no second emit).
//
// Caller must hold b.mu. The function releases b.mu briefly to make
// the HTTP call, then re-acquires it to write back.
//
// Returns errCEODraftLLMDisabled when no API key is configured so the
// caller can surface a human-friendly error without logging a stack.
func (b *Broker) draftIssueLocked(
	ctx context.Context,
	taskID string,
	userPrompt string,
	wikiContext []string,
) error {
	if b == nil {
		return fmt.Errorf("ceo_draft: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("ceo_draft: task id required")
	}
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return fmt.Errorf("ceo_draft: user prompt required")
	}

	// Find the task and guard on state + idempotency.
	var task *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			task = &b.tasks[i]
			break
		}
	}
	if task == nil {
		return fmt.Errorf("ceo_draft: task %q not found", taskID)
	}
	// Only draft for Drafting state tasks.
	if task.LifecycleState != LifecycleStateDrafting {
		return fmt.Errorf("ceo_draft: task %q is in state %q, not drafting — LLM call skipped",
			taskID, task.LifecycleState)
	}
	// Idempotency: already drafted.
	if task.IssueDraftSpec != nil && task.IssueDraftSpec.DraftedAt != "" {
		return nil
	}

	// Snapshot the agent roster while holding the lock.
	agentSlugs := make([]string, 0, len(b.members))
	for _, m := range b.members {
		if s := strings.TrimSpace(m.Slug); s != "" && s != "ceo" {
			agentSlugs = append(agentSlugs, s)
		}
	}
	agentRoster := strings.Join(agentSlugs, ", ")
	if agentRoster == "" {
		agentRoster = "none configured yet"
	}

	// Snapshot the CEO DM channel slug for message emission.
	ceoDMSlug := strings.TrimSpace(b.onboardingCEODMSlug())

	// Release the lock for the blocking HTTP round-trip.
	b.mu.Unlock()
	spec, llmErr := callCEODraftLLM(ctx, userPrompt, agentRoster, wikiContext)
	b.mu.Lock()

	if llmErr != nil {
		return fmt.Errorf("ceo_draft: llm call: %w", llmErr)
	}

	// Re-find the task after re-acquiring the lock (slice may have grown).
	task = nil
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			task = &b.tasks[i]
			break
		}
	}
	if task == nil {
		return fmt.Errorf("ceo_draft: task %q disappeared while drafting", taskID)
	}

	// Write the spec and stamp DraftedAt.
	now := time.Now().UTC().Format(time.RFC3339)
	task.IssueDraftSpec = &IssueDraftSpec{
		Goal:       spec.Goal,
		Context:    spec.Context,
		Approach:   spec.Approach,
		Acceptance: spec.Acceptance,
		DraftedAt:  now,
	}
	task.UpdatedAt = now

	// Emit each section as a CEO message into the DM so the SSE fan-out
	// delivers them to the frontend section-by-section. Message kind
	// "issue_draft_section" carries a structured payload the frontend
	// uses to render Goal → Context → Approach → Acceptance in order.
	if ceoDMSlug != "" {
		for _, section := range []struct {
			key   string
			value string
		}{
			{"goal", spec.Goal},
			{"context", spec.Context},
			{"approach", spec.Approach},
			{"acceptance", spec.Acceptance},
		} {
			if strings.TrimSpace(section.value) == "" {
				continue
			}
			sectionPayload, _ := json.Marshal(map[string]string{
				"task_id": taskID,
				"section": section.key,
				"content": section.value,
			})
			b.counter++
			b.appendMessageLocked(channelMessage{
				ID:        fmt.Sprintf("msg-%d", b.counter),
				From:      "ceo",
				Channel:   ceoDMSlug,
				Kind:      "issue_draft_section",
				Content:   string(sectionPayload),
				Timestamp: now,
			})
		}
	}

	return nil
}

// onboardingCEODMSlug returns the CEO DM channel slug from the onboarding
// state. Falls back to "dm:ceo:onboarding" when state is unavailable.
// Must be called while b.mu is held.
func (b *Broker) onboardingCEODMSlug() string {
	// Preferred: look for the reserved onboarding DM slug in the channel list.
	const reserved = "dm:ceo:onboarding"
	for _, ch := range b.channels {
		if ch.Slug == reserved {
			return reserved
		}
	}
	return reserved
}

// ceoDraftTimeout is the per-call deadline for the CEO draft LLM request,
// whether via claude CLI subprocess or HTTP direct.
const ceoDraftTimeout = 90 * time.Second

// callCEODraftLLM makes the LLM call for the CEO draft writer.
//
// Transport selection (see file header for full rationale):
//  1. claude CLI subprocess — preferred when `claude` is on PATH. Uses the
//     user's OAuth session; no API key required.
//  2. HTTP direct (Anthropic then OpenAI) — fallback when claude is not found.
//
// Returns errCEODraftLLMDisabled when neither transport is viable.
func callCEODraftLLM(ctx context.Context, userRequest, agentRoster string, wikiContext []string) (issueDraftLLMResponse, error) {
	wikiSection := buildCEODraftWikiSection(wikiContext)
	userPrompt := fmt.Sprintf(ceoDraftUserPromptTpl, userRequest, agentRoster, wikiSection)

	callCtx, cancel := context.WithTimeout(ctx, ceoDraftTimeout)
	defer cancel()

	// Preferred: claude CLI subprocess (uses OAuth, no API key needed).
	if claudePath, err := ceoDraftLookPath("claude"); err == nil {
		log.Printf("ceo_draft: transport=claude_cli path=%q", claudePath)
		raw, cliErr := callClaudeCLICEODraft(callCtx, userPrompt)
		if cliErr != nil {
			return issueDraftLLMResponse{}, cliErr
		}
		return parseCEODraftResponse(raw)
	}

	// Fallback: HTTP direct to Anthropic / OpenAI.
	anthroKey := strings.TrimSpace(config.ResolveAnthropicAPIKey())
	openaiKey := strings.TrimSpace(config.ResolveOpenAIAPIKey())
	if anthroKey == "" && openaiKey == "" {
		return issueDraftLLMResponse{}, errCEODraftLLMDisabled
	}

	log.Printf("ceo_draft: transport=http_direct anthropic=%t openai=%t", anthroKey != "", openaiKey != "")
	client := &http.Client{Timeout: ceoDraftTimeout}
	var raw string
	var err error
	if anthroKey != "" {
		raw, err = callAnthropicCEODraft(callCtx, client, anthroKey, userPrompt)
	} else {
		raw, err = callOpenAICEODraft(callCtx, client, openaiKey, userPrompt)
	}
	if err != nil {
		return issueDraftLLMResponse{}, err
	}
	return parseCEODraftResponse(raw)
}

// buildCEODraftWikiSection formats the wiki context slice into the prompt
// section that grounds the spec. Returns an empty string when no context is
// provided.
func buildCEODraftWikiSection(wikiContext []string) string {
	if len(wikiContext) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Wiki context (use this to ground the spec):\n")
	for _, w := range wikiContext {
		if t := strings.TrimSpace(w); t != "" {
			sb.WriteString("- ")
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// claudeCLIJSONOutput is the top-level envelope emitted by
// `claude -p "<prompt>" --output-format json`.
// The CLI writes a single JSON object to stdout on success.
type claudeCLIJSONOutput struct {
	// Result contains the assistant's final text response.
	Result string `json:"result"`
	// Type is "result" for a successful completion.
	Type string `json:"type"`
	// Subtype is "success" for a normal completion, "error" for failures.
	Subtype string `json:"subtype"`
}

// callClaudeCLICEODraft runs `claude -p "<combinedPrompt>" --output-format json`
// as a subprocess and returns the assistant text from the JSON envelope.
//
// The full prompt (system prompt + user turn) is passed as a single -p argument
// because the non-interactive claude CLI treats the first positional as the
// initial user message. The system prompt is prepended inline to keep the CEO
// voice rules in scope without needing a separate --system-prompt flag (which
// would be visible in ps output).
//
// Context cancellation is wired through exec.CommandContext so the subprocess
// is killed when the parent context is cancelled or times out.
func callClaudeCLICEODraft(ctx context.Context, userPrompt string) (string, error) {
	// Embed the system prompt inline so the CEO voice rules are always applied.
	// Format: "<system>\n\n<user_turn>" — the model treats everything before the
	// first blank line as context when no role markers are present.
	combined := ceoDraftSystemPrompt + "\n\n" + userPrompt

	cmd := ceoDraftCommandContext(ctx, "claude", "-p", combined, "--output-format", "json")
	// Strip Claude Code session env vars to prevent recursive-invocation
	// detection, matching the pattern used by internal/provider/claude.go.
	cmd.Env = filteredEnvForCEODraft()

	// WaitDelay ensures cmd.Wait() returns promptly after context cancellation
	// even when child processes (spawned by a wrapper shell) keep the stdout
	// pipe open. Without WaitDelay, cmd.Wait() blocks until all file descriptors
	// are closed, which can be the full subprocess lifetime for shell scripts.
	// For the real `claude` binary this is never an issue (single process, no
	// children), but test harness scripts and edge cases benefit from the cap.
	cmd.WaitDelay = 2 * time.Second

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ceo_draft cli: %w", ctx.Err())
		}
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("ceo_draft cli: subprocess exited: %w: %s", err, detail)
	}

	raw := strings.TrimSpace(stdoutBuf.String())
	if raw == "" {
		return "", fmt.Errorf("ceo_draft cli: empty stdout from claude subprocess")
	}

	// Parse the JSON envelope. The CLI emits a single object.
	var out claudeCLIJSONOutput
	if jsonErr := json.Unmarshal([]byte(raw), &out); jsonErr != nil {
		return "", fmt.Errorf("ceo_draft cli: parse envelope: %w (raw: %.120s)", jsonErr, raw)
	}
	if strings.TrimSpace(out.Result) == "" {
		return "", fmt.Errorf("ceo_draft cli: empty result in envelope (subtype=%q raw: %.120s)", out.Subtype, raw)
	}
	return out.Result, nil
}

// claudeDraftEnvVarsToStrip are the env vars set by Claude Code that must be
// removed so the child `claude` process does not detect a recursive invocation.
// Mirrors internal/provider.claudeEnvVarsToStrip.
var claudeDraftEnvVarsToStrip = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_SESSION",
	"CLAUDE_CODE_PARENT_SESSION",
}

// filteredEnvForCEODraft returns os.Environ() with Claude Code session vars
// removed. Does NOT do full git-env scrubbing (only full agent launches need
// that; a one-shot prompt invocation is fine with the inherited env).
func filteredEnvForCEODraft() []string {
	stripSet := make(map[string]struct{}, len(claudeDraftEnvVarsToStrip))
	for _, k := range claudeDraftEnvVarsToStrip {
		stripSet[k] = struct{}{}
	}
	all := ceoDraftOsEnviron()
	out := make([]string, 0, len(all))
	for _, kv := range all {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		if _, skip := stripSet[key]; !skip {
			out = append(out, kv)
		}
	}
	return out
}

// ceoDraftOsEnviron is a seam for testing: returns the current process
// environment. Tests can swap this to inject a controlled env.
var ceoDraftOsEnviron = func() []string {
	return os.Environ()
}

// callAnthropicCEODraft calls the Anthropic API for the CEO draft writer.
func callAnthropicCEODraft(ctx context.Context, client *http.Client, key, userPrompt string) (string, error) {
	const endpoint = "https://api.anthropic.com/v1/messages"
	const model = "claude-haiku-4-5-20251001"

	payload := map[string]any{
		"model":      model,
		"max_tokens": 1024,
		"system":     ceoDraftSystemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("ceo_draft anthropic: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ceo_draft anthropic: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ceo_draft anthropic: do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("ceo_draft anthropic: read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ceo_draft anthropic: status %d: %.240s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("ceo_draft anthropic: decode: %w", err)
	}
	var sb strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

// callOpenAICEODraft calls the OpenAI API for the CEO draft writer.
func callOpenAICEODraft(ctx context.Context, client *http.Client, key, userPrompt string) (string, error) {
	const endpoint = "https://api.openai.com/v1/chat/completions"
	const model = "gpt-4o-mini"

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": ceoDraftSystemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens": 1024,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("ceo_draft openai: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ceo_draft openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ceo_draft openai: do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("ceo_draft openai: read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ceo_draft openai: status %d: %.240s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("ceo_draft openai: decode: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("ceo_draft openai: no choices in response")
	}
	return parsed.Choices[0].Message.Content, nil
}

// parseCEODraftResponse parses the raw LLM output into an issueDraftLLMResponse.
// Strips markdown code fences if the model wraps the JSON.
func parseCEODraftResponse(raw string) (issueDraftLLMResponse, error) {
	raw = strings.TrimSpace(raw)
	// Strip common markdown code fence wrappers.
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) == 2 {
			raw = lines[1]
		}
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}
	var resp issueDraftLLMResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return issueDraftLLMResponse{}, fmt.Errorf("ceo_draft: parse response: %w (raw: %.120s)", err, raw)
	}
	return resp, nil
}

// emitExecutionLineupCardLocked emits a ceo_execution_lineup suggestion card
// to the CEO DM channel after Approve & Start (Drafting → Running). The
// agents proposal comes from the blueprint's picked agents (if any) or from
// a small LLM call (scratch path). Sanitized via sanitizeCEOPayload path.
//
// Caller must hold b.mu. The function may release and re-acquire b.mu
// for the scratch-path LLM call.
func (b *Broker) emitExecutionLineupCardLocked(ctx context.Context, taskID string) {
	if b == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}

	var task *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			task = &b.tasks[i]
			break
		}
	}
	if task == nil {
		log.Printf("ceo_draft: emitExecutionLineupCard: task %q not found", taskID)
		return
	}

	ceoDMSlug := b.onboardingCEODMSlug()

	// Collect agents: office roster first, then infer from spec if empty.
	var agents []lineupAgentEntry

	// Blueprint / any-path: walk the office member list and include
	// non-CEO agents. Caps at 4 to keep the card readable.
	for _, m := range b.members {
		slug := strings.TrimSpace(m.Slug)
		if slug == "" || slug == "ceo" {
			continue
		}
		agents = append(agents, lineupAgentEntry{
			Slug:   slug,
			Role:   strings.TrimSpace(m.Name),
			Reason: "member of this office",
		})
		if len(agents) >= 4 {
			break
		}
	}

	// Scratch path: if no roster agents found and we have a draft spec,
	// make a small LLM call to infer suitable agents from the issue.
	if len(agents) == 0 && task.IssueDraftSpec != nil {
		b.mu.Unlock()
		inferred := inferAgentsFromSpec(ctx, task.IssueDraftSpec)
		b.mu.Lock()
		agents = append(agents, inferred...)
	}

	if len(agents) == 0 {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	suggestionID := fmt.Sprintf("lineup-%s-%d", taskID, time.Now().UnixNano())

	lineupPayload, err := json.Marshal(map[string]any{
		"kind":          "ceo_execution_lineup",
		"suggestion_id": suggestionID,
		"task_id":       taskID,
		"agents":        agents,
	})
	if err != nil {
		log.Printf("ceo_draft: marshal lineup payload: %v", err)
		return
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "ceo",
		Channel:   ceoDMSlug,
		Kind:      "ceo_execution_lineup",
		Content:   string(lineupPayload),
		Timestamp: now,
	})
}

// lineupAgentEntry is the per-agent entry in the execution lineup card.
type lineupAgentEntry struct {
	Slug   string `json:"slug"`
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

// inferAgentsFromSpec calls a small LLM to suggest 2-4 agents for a
// scratch-path issue based on its spec sections. Returns a minimal list
// on any error rather than propagating.
func inferAgentsFromSpec(ctx context.Context, spec *IssueDraftSpec) []lineupAgentEntry {
	anthroKey := strings.TrimSpace(config.ResolveAnthropicAPIKey())
	openaiKey := strings.TrimSpace(config.ResolveOpenAIAPIKey())
	if anthroKey == "" && openaiKey == "" {
		return nil
	}

	specText := strings.Join([]string{spec.Goal, spec.Context, spec.Approach, spec.Acceptance}, "\n")
	userP := fmt.Sprintf(`Issue spec:
%s

Available agent roles in a typical startup engineering team: founding-engineer, pm, designer, qa, devops.

Return ONLY a JSON array of 2-4 objects with no preamble:
[{"slug":"<role-slug>","role":"<readable role>","reason":"<one sentence why this agent fits>"}]`, specText)

	timeout := 15 * time.Second
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: timeout}
	var raw string
	var err error
	if anthroKey != "" {
		raw, err = callAnthropicCEODraft(callCtx, client, anthroKey, userP)
	} else {
		raw, err = callOpenAICEODraft(callCtx, client, openaiKey, userP)
	}
	if err != nil {
		log.Printf("ceo_draft: infer agents from spec: %v", err)
		return nil
	}

	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) == 2 {
			raw = lines[1]
		}
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}

	var entries []lineupAgentEntry
	if jsonErr := json.Unmarshal([]byte(raw), &entries); jsonErr != nil {
		log.Printf("ceo_draft: parse inferred agents: %v (raw: %.120s)", jsonErr, raw)
		return nil
	}

	result := make([]lineupAgentEntry, 0, len(entries))
	for _, e := range entries {
		if len(result) >= 4 {
			break
		}
		slug := strings.TrimSpace(e.Slug)
		if slug == "" {
			continue
		}
		result = append(result, lineupAgentEntry{
			Slug:   slug,
			Role:   strings.TrimSpace(e.Role),
			Reason: strings.TrimSpace(e.Reason),
		})
	}
	return result
}
