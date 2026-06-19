package team

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// maxOpenAICompatToolIterations bounds the inner tool-call loop so a model
// stuck in a self-referential tool-use cycle can't burn the office. Eight
// is a comfortable ceiling for the broker's normal "claim_task → do work →
// post message" sequences while still terminating runaway loops fast.
const maxOpenAICompatToolIterations = 8

// runHeadlessOpenAICompatTurn executes a single broker-driven turn for an
// agent bound to an OpenAI-compatible runtime (mlx-lm, ollama, exo, Hermes,
// or OpenClaw Gateway HTTP).
//
// Flow:
//  1. Spawn `wuphf mcp-team` and connect an MCP client → list of tools.
//  2. Stream the model with the agent prompt, the user notification, and
//     the discovered tools.
//  3. On each tool_use chunk, dispatch through the MCP session and append
//     a synthetic user message describing the result so the next turn can
//     react. Loop up to maxOpenAICompatToolIterations.
//  4. When the model returns text without firing more tools, post that
//     text to the channel via the standard "if-silent" hook.
//
// If the MCP bridge fails to start (e.g. wuphf binary path is unresolvable)
// the runner falls back to text-only chat so the user gets a visible reply
// rather than a silent failure. This is intentionally noisy in the agent
// log so an operator can see why their tools aren't firing.
func (l *Launcher) runHeadlessOpenAICompatTurn(ctx context.Context, slug string, notification string, channel ...string) error {
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}

	// Use the turn's task-aware kind (the same resolution the dispatch switch
	// used to route here) so Lookup and the model override below agree with the
	// per-task provider, not just the agent binding.
	kind := l.effectiveProviderKindForAgent(ctx, slug)
	entry := provider.Lookup(kind)
	if entry == nil || entry.StreamFn == nil {
		return fmt.Errorf("openai-compat runtime %q is not registered", kind)
	}

	systemPrompt := strings.TrimSpace(l.buildPrompt(slug))
	userPrompt := strings.TrimSpace(notification)
	if userPrompt == "" {
		userPrompt = "Proceed with the task."
	}

	startedAt := time.Now()
	metrics := headlessProgressMetrics{
		TotalMs:      -1,
		FirstEventMs: -1,
		FirstTextMs:  -1,
		FirstToolMs:  -1,
	}
	l.updateHeadlessProgress(slug, "active", "thinking", "reviewing work packet", metrics)

	target := firstNonEmpty(channel...)

	// Broker.AgentStream is keyed by slug and returns the same buffer on
	// repeat calls, so a single lookup is sufficient for both the bridge-
	// failure notice and the per-chunk pushes in the loop callbacks below.
	var agentStream *agentStreamBuffer
	activeTaskID := l.turnTaskIDForCtx(ctx, slug)
	if l.broker != nil {
		agentStream = l.broker.AgentStream(slug)
	}

	// Bridge MCP tools so the model can drive the broker. Failure is non-
	// fatal; we degrade to text-only chat so the user still sees a reply.
	// We push the failure into agentStream too so it shows up in the live
	// UI, not only in the on-disk log.
	tools, cleanup, bridgeErr := l.connectOpenAICompatMCPBridge(ctx, slug, target)
	if cleanup != nil {
		defer cleanup()
	}
	if bridgeErr != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("openai_compat_mcp-bridge-failed: %v (falling back to text-only)", bridgeErr))
		if agentStream != nil {
			agentStream.PushTask(activeTaskID, fmt.Sprintf("[bridge unavailable: %v — replying without tools]", bridgeErr))
		}
		tools = nil
	}

	// Stable tool order for the prompt. Slightly more cache-friendly across
	// turns and easier for humans reading agent logs.
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	toolByName := make(map[string]agent.AgentTool, len(tools))
	for _, t := range tools {
		toolByName[t.Name] = t
	}

	// Some OpenAI-compat upstreams (Hermes api_server, OpenClaw HTTP) ignore
	// the caller's tools[] field and run their own agent loop. We can still
	// make tool calls work by prompting the model to emit
	// `<tools>{"name":"X","arguments":{...}}</tools>` in its text — WUPHF's
	// existing parseJSONInContentToolCall parser picks those up and the loop
	// dispatches them through toolByName the same way a native tool_call
	// would. The bridge tools are still registered for dispatch even though
	// the upstream never sees them in the tools[] field.
	if kind == provider.KindHermesAgent || kind == provider.KindOpenclawHTTP {
		if len(tools) > 0 {
			systemPrompt = openAICompatPromptedToolsPrompt(slug, systemPrompt, tools)
		} else {
			systemPrompt = openAICompatTextOnlyPrompt(slug, systemPrompt)
		}
	} else if len(tools) == 0 {
		// Native OpenAI-compat (mlx-lm, ollama, exo) with no tools — bridge
		// failed; degrade to text-only with the same preamble.
		systemPrompt = openAICompatTextOnlyPrompt(slug, systemPrompt)
	}

	msgs := make([]agent.Message, 0, 4)
	if systemPrompt != "" {
		msgs = append(msgs, agent.Message{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, agent.Message{Role: "user", Content: userPrompt})

	// Use the ctx-aware StreamFn so cancellation propagates all the way
	// to the HTTP request — without this a cancelled turn would pin the
	// local server's inference slot until the model finishes generating.
	// Per-task model wins over the agent binding (model lives on the task);
	// the binding then wins over env/config/default — this is what makes
	// agents like ceo run kimi-k2.6 while planner runs deepseek-v4-pro on the
	// same install-wide ollama endpoint.
	modelOverride := l.taskModelForKind(ctx, slug, kind)
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(l.broker.MemberProviderBinding(slug).Model)
	}
	streamFn := provider.NewOpenAICompatStreamFnWithCtxModelAndAgent(ctx, kind, modelOverride, slug)

	// Live-chat relay streams the model's user-facing text to the channel
	// at sentence/paragraph boundaries, so the room sees the agent's reply
	// taking shape rather than waiting for the final summary post. The
	// state machine's pushLiveText / onToolUseChunk paths drive
	// relay.OnText / relay.Flush internally; without a relay attached
	// those calls are no-ops and only the final post lands in the channel.
	relay := newHeadlessLiveChatRelay(l, slug, target, notification, func(line string) {
		appendHeadlessCodexLog(slug, line)
	})

	// All policy-bearing per-turn state is owned by openAICompatTurnState
	// (see openai_compat_turn_state.go). The state machine is
	// independently unit-tested via openai_compat_turn_state_test.go.
	state := newOpenAICompatTurnState(&runtimeTurnSinks{
		l:      l,
		slug:   slug,
		taskID: activeTaskID,
		// agentStream may be nil during edge-case launcher setups; the
		// sink's pushAgentStream short-circuits on nil so the state
		// machine stays oblivious.
		stream:  agentStream,
		metrics: &metrics,
	}, relay)
	// Defer the flush so the loop's `if err != nil { return err }` and
	// other early exits still surface the trailing buffered sentence.
	// The explicit flushes before the partial-post and final-post hooks
	// stay — they're idempotent once the buffer is drained.
	defer state.flushLiveChat()

	// taskID is unique per turn so the Receipts panel groups each
	// agent's turn into its own row. Format mirrors agent.nextTaskID.
	taskID := fmt.Sprintf("%s-%d", slug, time.Now().UnixMilli())
	turnID := newHeadlessTurnID()
	var turnToolNames []string
	var turnTextLen int
	// Workflow-detection trace capture (mirrors the claude runner): correlate
	// each integration tool_use with its result so the extractor sees real args
	// + response shape. See trace_sink.go.
	var pendingTrace *ActionTrace
	traceSeq := 0
	flushTrace := func() {
		if pendingTrace != nil {
			persistActionTrace(*pendingTrace)
			pendingTrace = nil
		}
	}

	loop := openAICompatToolLoop{
		streamFn:    streamFn,
		tools:       tools,
		toolByName:  toolByName,
		maxIters:    maxOpenAICompatToolIterations,
		toolTimeout: 90 * time.Second,
		taskLogRoot: agent.DefaultTaskLogRoot(),
		taskID:      taskID,
		agentSlug:   slug,
		onText: func(chunk string) {
			state.onText(chunk)
			turnTextLen += len(chunk)
			emitHeadlessText(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, chunk, kind+".text")
		},
		onToolUse: func(name, rawInput string) {
			state.onToolUseChunk(name, rawInput)
			l.updateHeadlessProgress(slug, "active", "tool", "running "+name, metrics)
			turnToolNames = append(turnToolNames, manifestToolToken(name, rawInput))
			flushTrace()
			if tr, ok := traceFromToolUse(activeTaskID, turnID, slug, name, rawInput, traceSeq); ok {
				traceSeq++
				pendingTrace = &tr
			}
			emitHeadlessToolUse(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, name, rawInput, kind+".tool_use")
		},
		onError: state.onError,
		onToolResult: func(name, result string, err error) {
			state.onToolResult(name, result, err)
			text := result
			if err != nil {
				text = err.Error()
			}
			if pendingTrace != nil {
				pendingTrace.Result = summarizeResult(text)
				flushTrace()
			}
			emitHeadlessToolResult(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, name, text, kind+".tool_result")
		},
		onFirstEvent: func() {
			metrics.FirstEventMs = durationMillis(startedAt, time.Now())
		},
		onFirstText: func() {
			metrics.FirstTextMs = durationMillis(startedAt, time.Now())
			l.updateHeadlessProgress(slug, "active", "text", "drafting response", metrics)
		},
		onFirstTool: func() {
			metrics.FirstToolMs = durationMillis(startedAt, time.Now())
		},
	}

	finalText, iterationsUsed, turnUsage, streamErr, err := loop.run(ctx, msgs)
	flushTrace() // persist a trailing integration call whose result closed the turn
	// Record token counts even on partial / errored turns: a failed turn
	// can still have generated thousands of tokens we want surfaced in the
	// usage panel. CostUSD stays at zero — local runtimes have no marginal
	// $ cost so the broker's cost_usd column correctly remains untouched.
	if (turnUsage.InputTokens > 0 || turnUsage.OutputTokens > 0) && l.broker != nil {
		l.broker.RecordAgentUsage(slug, kind, turnUsage)
	}
	if err != nil {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		l.updateHeadlessProgress(slug, "error", "error", truncate(err.Error(), 180), metrics)
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, "", err.Error(), metrics, claudeUsageToTokenUsage(turnUsage))
		emitHeadlessManifest(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, err.Error(), turnToolNames, turnTextLen, metrics, claudeUsageToTokenUsage(turnUsage))
		return err
	}
	if streamErr != "" {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error provider=%s total_ms=%d first_event_ms=%d first_text_ms=%d iterations=%d detail=%q",
			kind, metrics.TotalMs,
			metrics.FirstEventMs, metrics.FirstTextMs,
			iterationsUsed, streamErr,
		))
		l.updateHeadlessProgress(slug, "error", "error", truncate(streamErr, 180), metrics)
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, "", streamErr, metrics, claudeUsageToTokenUsage(turnUsage))
		emitHeadlessManifest(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, streamErr, turnToolNames, turnTextLen, metrics, claudeUsageToTokenUsage(turnUsage))
		// Post any partial output (e.g. the cap-hit marker the loop
		// produced when maxIters tripped) before propagating the error,
		// so the user sees something on-channel rather than a silent
		// failure. Without this, finalText is computed and discarded.
		state.flushLiveChat()
		if text := strings.TrimSpace(finalText); text != "" {
			if msg, posted, postErr := l.postHeadlessFinalMessageIfSilent(slug, target, notification, text, startedAt); postErr != nil {
				appendHeadlessCodexLog(slug, kind+"_partial-post-error: "+postErr.Error())
			} else if posted {
				appendHeadlessCodexLog(slug, fmt.Sprintf("%s_partial-post: posted partial output to #%s as %s", kind, msg.Channel, msg.ID))
			}
		}
		return fmt.Errorf("%s: %s", kind, streamErr)
	}

	metrics.TotalMs = time.Since(startedAt).Milliseconds()
	text := strings.TrimSpace(finalText)
	// Backstop: if the loop's final reply LOOKS like a tool-call shape
	// the parser couldn't disambiguate, don't post the raw JSON to the
	// channel as the agent's message — replace it with a short hint
	// pointing at the latency log. This is a recoverable case: the
	// model emitted something tool-call-shaped that didn't match any
	// of our supported dialects (legacy bug: posted the JSON verbatim).
	if looksUnparsedToolCall(text) {
		appendHeadlessCodexLog(slug, kind+"_unparsed_tool_call: "+truncate(text, 480))
		text = "(local model emitted a tool-call shape we couldn't parse — see the agent log for the raw output and try a different model or prompt)"
	}
	appendHeadlessCodexLatency(slug, fmt.Sprintf("status=ok provider=%s total_ms=%d first_event_ms=%d first_text_ms=%d iterations=%d final_chars=%d",
		kind, metrics.TotalMs,
		metrics.FirstEventMs, metrics.FirstTextMs,
		iterationsUsed, len(text),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metrics))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metrics)
	emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, summary, "", metrics, claudeUsageToTokenUsage(turnUsage))
	emitHeadlessManifest(agentStream, turnID, HeadlessProviderOpenAICompat, slug, activeTaskID, "", turnToolNames, turnTextLen, metrics, claudeUsageToTokenUsage(turnUsage))

	state.flushLiveChat()
	if text != "" && state.shouldPostFinalText() {
		// Suppress when the agent already posted via team_broadcast /
		// reply / etc this turn. Posting finalText here would
		// double-up the user-visible reply AND fan out through the
		// broker's channel-dispatch back to the agent itself.
		appendHeadlessCodexLog(slug, kind+"_result: "+text)
		msg, posted, err := l.postHeadlessFinalMessageIfSilent(slug, target, notification, text, startedAt)
		if err != nil {
			appendHeadlessCodexLog(slug, kind+"_fallback-post-error: "+err.Error())
		} else if posted {
			appendHeadlessCodexLog(slug, fmt.Sprintf("%s_fallback-post: posted final output to #%s as %s", kind, msg.Channel, msg.ID))
		}
	}
	return nil
}

// isUserVisiblePostTool reports whether name is one of the MCP tools
// that itself posts a message to a channel the user is reading. When
// the agent invokes one of these in a turn, the post-loop final-text
// hook should NOT also post finalText (which would be the model's
// "I've done it" follow-up summary), or the user sees double-replies
// per turn AND triggers a fan-out loop because each post re-fires
// the agent through the broker's channel-dispatch path.
func isUserVisiblePostTool(name string) bool {
	switch name {
	case
		"team_broadcast",
		"team_reply",
		"reply",
		"direct_message",
		"broker_post_message":
		return true
	}
	return false
}

// looksUnparsedToolCall reports whether text resembles a tool-call
// JSON shape we'd have wanted to dispatch but couldn't. Used as a
// last-ditch backstop in runHeadlessOpenAICompatTurn so a model that
// emits tool-call-shaped output in an unsupported dialect doesn't
// have its raw JSON posted to the channel as if it were the agent's
// reply. Conservative: only fires when the trimmed content starts
// with `{` and contains both `"name"` and `"arguments"` substrings.
func looksUnparsedToolCall(text string) bool {
	t := strings.TrimSpace(text)
	// `<tools>{...}</tools>` is the prompted-tools dialect emitted by
	// Hermes/OpenClaw-HTTP when they use the schema-in-prompt protocol.
	// It's structurally a tool invocation, never legitimate prose, and we
	// don't want the live-chat-relay leaking the raw JSON into the channel
	// before the openAICompatToolLoop dispatches it.
	if strings.HasPrefix(t, "<tools>") && strings.Contains(t, "</tools>") {
		return true
	}
	if !strings.HasPrefix(t, "{") {
		return false
	}
	return strings.Contains(t, `"name"`) && strings.Contains(t, `"arguments"`)
}

// isOpenAICompatKind reports whether kind is one of the local OpenAI-
// compatible runtimes that route through runHeadlessOpenAICompatTurn.
// Centralised here so the dispatcher in headless_codex.go and any future
// caller stay in sync without duplicating the list.
func isOpenAICompatKind(kind string) bool {
	switch kind {
	case provider.KindMLXLM, provider.KindOllama, provider.KindExo, provider.KindHermesAgent, provider.KindOpenclawHTTP:
		return true
	default:
		return false
	}
}

// openAICompatTextOnlyPrompt wraps the standard system prompt with a leading
// directive telling the model that no tools are callable in this session and
// that its reply text will be posted to the channel verbatim by WUPHF. We do
// not strip the existing prompt body — its persona / team / voice content is
// still useful — we just override the parts that promise team_broadcast,
// team_task, human_message, etc. are dispatchable.
func openAICompatTextOnlyPrompt(slug, originalPrompt string) string {
	header := strings.Builder{}
	header.WriteString("== TOOL-LESS REPLY MODE ==\n")
	header.WriteString("This API session does NOT support tool calls. Tools like team_broadcast, team_task, team_skill_run, human_message, mcp__wuphf-office__*, query_context, etc. are NOT callable here, even if the section below mentions them.\n")
	header.WriteString("Reply with plain assistant text ONLY. WUPHF will automatically post that text to the channel for you — no tool invocation is needed or possible.\n")
	header.WriteString("Do NOT say things like \"I can't execute team_broadcast\" or \"the tool isn't available\" — just answer in prose as if you were replying in chat. Do NOT propose creating tasks, channels, skills, or wiki entries unless the human explicitly asked for narrative-only suggestions.\n")
	if slug != "" {
		header.WriteString(fmt.Sprintf("You are @%s. Speak in first person and stay in role.\n", slug))
	}
	header.WriteString("\n--- background context (treat tool sections below as descriptive, not callable) ---\n\n")
	header.WriteString(originalPrompt)
	return header.String()
}

// openAICompatPromptedToolsPrompt wraps the standard system prompt with a
// tool-calling preamble that teaches the model to emit text-encoded tool
// invocations as `<tools>{...}</tools>` blocks. WUPHF's existing
// parseJSONInContentToolCall (internal/provider/openai_compat.go) parses
// those blocks back into tool_use stream chunks, and openAICompatToolLoop
// dispatches them through the registered tool map and feeds results back
// as the next user turn — the same multi-turn flow native tool_calls take.
//
// This path exists for OpenAI-compat upstreams that ignore the request's
// tools[] field (Hermes api_server runs its own internal agent loop;
// OpenClaw HTTP behaves similarly). For those backends, sending tools in
// the request body is a no-op — the prompt is the only place we can teach
// the model what's callable.
func openAICompatPromptedToolsPrompt(slug, originalPrompt string, tools []agent.AgentTool) string {
	header := strings.Builder{}
	header.WriteString("== TOOL CALLING (PROMPT-ENCODED) ==\n")
	header.WriteString("This API session does NOT deliver tools through the OpenAI `tool_calls` field. Instead, you invoke tools by emitting a `<tools>{...}</tools>` block in your reply text. WUPHF parses these blocks, executes the tool, and feeds the result back into the conversation as the next user turn.\n\n")
	header.WriteString("To call a tool, emit exactly:\n")
	header.WriteString("    <tools>{\"name\":\"<tool-name>\",\"arguments\":{...}}</tools>\n")
	header.WriteString("Rules:\n")
	header.WriteString("- The block must be the ENTIRE reply (with optional surrounding whitespace). Do NOT mix prose and a tools block in the same turn — the parser only fires when the JSON dominates the message.\n")
	header.WriteString("- Use exactly one tools block per turn. If you need a second tool, wait for the result and call it next turn.\n")
	header.WriteString("- `arguments` must be a JSON object matching the tool's schema. Strings must be JSON-escaped; do NOT wrap arguments in extra quotes.\n")
	header.WriteString("- If you do NOT need to call a tool this turn, reply with plain prose — WUPHF will post it to the channel automatically.\n")
	header.WriteString("- Tool RESULTS arrive as the next user message prefixed with `Tool \"<name>\" returned:`. Read them and decide whether to call another tool or write a final user-facing reply.\n")
	header.WriteString("- Do NOT mention this protocol to the human (no \"let me call team_broadcast\" narration). Either emit the block or write the reply directly.\n\n")
	if slug != "" {
		header.WriteString(fmt.Sprintf("You are @%s. Speak in first person and stay in role.\n\n", slug))
	}
	header.WriteString("Available tools (name, description, JSON schema):\n")
	for _, t := range tools {
		// Tools without schemas get an empty object so the model emits
		// `"arguments":{}` rather than guessing. Note `json.Marshal(nil)`
		// returns `[]byte("null"), nil` — checking only `err == nil` would
		// emit `schema: null` for nil-schema tools, which is confusing to the
		// model. Detect nil first.
		schemaStr := "{}"
		if t.Schema != nil {
			if schemaBytes, err := json.Marshal(t.Schema); err == nil && len(schemaBytes) > 0 {
				schemaStr = string(schemaBytes)
			}
		}
		desc := strings.TrimSpace(t.Description)
		if desc == "" {
			desc = "(no description)"
		}
		// Single-line description keeps the prompt compact; schemas are
		// already JSON.
		header.WriteString(fmt.Sprintf("- %s: %s\n  schema: %s\n", t.Name, desc, schemaStr))
	}
	header.WriteString("\nExamples:\n")
	header.WriteString("To post to #general:\n")
	header.WriteString("    <tools>{\"name\":\"team_broadcast\",\"arguments\":{\"channel\":\"general\",\"content\":\"Hello team\",\"tagged\":[]}}</tools>\n")
	header.WriteString("To answer the human without calling any tool:\n")
	header.WriteString("    Just reply in plain prose. No tools block.\n\n")
	header.WriteString("--- background context (the section below describes tool names + intent; ignore any language that implies tool_calls are dispatched natively — use the <tools>{...}</tools> protocol above instead) ---\n\n")
	header.WriteString(originalPrompt)
	return header.String()
}
