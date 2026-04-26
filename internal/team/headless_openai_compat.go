package team

import (
	"context"
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
// agent bound to a local OpenAI-compatible runtime (mlx-lm, ollama, exo).
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

	kind := l.memberEffectiveProviderKind(slug)
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
			agentStream.Push(fmt.Sprintf("[bridge unavailable: %v — replying without tools]", bridgeErr))
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

	msgs := make([]agent.Message, 0, 4)
	if systemPrompt != "" {
		msgs = append(msgs, agent.Message{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, agent.Message{Role: "user", Content: userPrompt})

	// Use the ctx-aware StreamFn so cancellation propagates all the way
	// to the HTTP request — without this a cancelled turn would pin the
	// local server's inference slot until the model finishes generating.
	streamFn := provider.NewOpenAICompatStreamFnWithCtx(ctx, kind)

	// All policy-bearing per-turn state is owned by openAICompatTurnState
	// (see openai_compat_turn_state.go). The state machine is
	// independently unit-tested via openai_compat_turn_state_test.go.
	state := newOpenAICompatTurnState(&runtimeTurnSinks{
		l:    l,
		slug: slug,
		// agentStream may be nil during edge-case launcher setups; the
		// sink's pushAgentStream short-circuits on nil so the state
		// machine stays oblivious.
		stream:  agentStream,
		metrics: &metrics,
	})

	loop := openAICompatToolLoop{
		streamFn:    streamFn,
		tools:       tools,
		toolByName:  toolByName,
		maxIters:    maxOpenAICompatToolIterations,
		toolTimeout: 90 * time.Second,
		onText:      state.onText,
		onToolUse: func(name, rawInput string) {
			state.onToolUseChunk(name, rawInput)
			l.updateHeadlessProgress(slug, "active", "tool", "running "+name, metrics)
		},
		onError:      state.onError,
		onToolResult: state.onToolResult,
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
		// Post any partial output (e.g. the cap-hit marker the loop
		// produced when maxIters tripped) before propagating the error,
		// so the user sees something on-channel rather than a silent
		// failure. Without this, finalText is computed and discarded.
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
	case provider.KindMLXLM, provider.KindOllama, provider.KindExo:
		return true
	default:
		return false
	}
}
