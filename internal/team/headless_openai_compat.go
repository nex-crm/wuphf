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

	// Live-output suppression for JSON-in-content tool calls. Open
	// models (Qwen2.5-Coder via mlx_lm.server, in particular) emit
	// tool calls as `{"name":"...","arguments":{...}}<|im_end|>` —
	// streamed character-by-character. Without this gate the user
	// would watch the raw JSON type itself out in the live-output
	// panel before the orchestrator silently swaps it for a tool_use
	// chunk. We detect "looks like a JSON tool call" by checking
	// whether the cumulative text starts with `{` (whitespace-
	// trimmed). If it does, we hold off pushing chunks to agentStream
	// until either the parser fires (in which case the JSON never
	// appears) or the stream ends with no tool-call match.
	var (
		liveBuf   strings.Builder
		looksJSON bool
		decided   bool
	)

	// broadcastedThisTurn flips true the first time the agent uses a
	// user-visible posting tool (team_broadcast, reply, etc.). Used at
	// the end of the turn to suppress a duplicate text post — without
	// it, every turn produces both the tool's content + an iteration-2
	// "Great, I've sent that!" summary, which compounds into a fan-out
	// loop when the broker dispatches each post back to the agent.
	var broadcastedThisTurn bool

	// Throughput readout. Local LLMs are an order of magnitude slower
	// than cloud APIs, so users want feedback that the machine is
	// actually doing something — not just a static "drafting response"
	// label that could mean "stuck". We track cumulative chars over a
	// rolling 1s window and surface ~tok/s in the progress label
	// (~4 chars per token, English; close enough for a vibe readout).
	// Updates capped at one-per-750ms so the broker SSE stream isn't
	// pummeled on fast models.
	var (
		streamedChars int
		tpsAnchorAt   time.Time
		tpsAnchorChrs int
	)
	flushLiveBuf := func() {
		if liveBuf.Len() > 0 && agentStream != nil {
			agentStream.Push(liveBuf.String())
		}
		liveBuf.Reset()
	}

	loop := openAICompatToolLoop{
		streamFn:    streamFn,
		tools:       tools,
		toolByName:  toolByName,
		maxIters:    maxOpenAICompatToolIterations,
		toolTimeout: 90 * time.Second,
		onText: func(s string) {
			if s == "" {
				return
			}
			streamedChars += len(s)
			// Rolling tok/s readout in the progress label.
			now := time.Now()
			if tpsAnchorAt.IsZero() {
				tpsAnchorAt = now
				tpsAnchorChrs = streamedChars
			} else if now.Sub(tpsAnchorAt) >= 750*time.Millisecond {
				delta := streamedChars - tpsAnchorChrs
				elapsed := now.Sub(tpsAnchorAt).Seconds()
				if delta > 0 && elapsed > 0 {
					// Standard ~4 chars per token; close enough for a
					// vibe readout. Round to nearest int so the label
					// doesn't flicker on every chunk.
					tps := float64(delta) / 4.0 / elapsed
					l.updateHeadlessProgress(slug, "active", "text",
						fmt.Sprintf("drafting response · ~%.0f tok/s", tps),
						metrics)
				}
				tpsAnchorAt = now
				tpsAnchorChrs = streamedChars
			}
			if agentStream == nil {
				return
			}
			if !decided {
				// Buffer until we've seen enough non-whitespace to
				// decide; ~8 chars is enough to disambiguate `{ ` vs
				// real prose.
				liveBuf.WriteString(s)
				trimmed := strings.TrimLeft(liveBuf.String(), " \t\r\n")
				if len(trimmed) >= 8 || strings.ContainsAny(trimmed, "\n") {
					if strings.HasPrefix(trimmed, "{") {
						looksJSON = true
					}
					decided = true
					if !looksJSON {
						flushLiveBuf()
					}
				}
				return
			}
			if looksJSON {
				// Hold back; the parser at end-of-stream may convert
				// this entire buffer to a tool_use. If it doesn't,
				// the post-loop unparsed-tool-call backstop replaces
				// the JSON with a clearer message.
				liveBuf.WriteString(s)
				return
			}
			agentStream.Push(s)
		},
		onToolUse: func(name, rawInput string) {
			// Tool firing means the streamed JSON we held back was a
			// tool-call invocation — discard the buffer so the next
			// iteration starts fresh, and reset the throughput +
			// JSON-detection state so iteration 2 doesn't inherit
			// stale flags from iteration 1.
			liveBuf.Reset()
			looksJSON = false
			decided = false
			tpsAnchorAt = time.Time{}
			tpsAnchorChrs = 0
			streamedChars = 0
			if agentStream != nil {
				agentStream.Push(fmt.Sprintf("[tool_use %s] %s", name, rawInput))
			}
			l.updateHeadlessProgress(slug, "active", "tool", "running "+name, metrics)
		},
		onError: func(msg string) {
			if agentStream != nil {
				agentStream.Push("[error] " + msg)
			}
		},
		onToolResult: func(name, result string, err error) {
			if err != nil {
				appendHeadlessCodexLog(slug, fmt.Sprintf("openai_compat_tool_error: %s -> %v", name, err))
				return
			}
			appendHeadlessCodexLog(slug, fmt.Sprintf("openai_compat_tool_ok: %s -> %s", name, truncate(result, 240)))
			// Track when the agent already posted a user-visible message
			// in this turn via team_broadcast / reply / direct message
			// tools. The post-loop "post finalText to channel" hook would
			// otherwise double-post: once from the tool, again from
			// iteration-2's "I've sent that" summary. We suppress the
			// second post so the channel sees one reply per user message.
			if isUserVisiblePostTool(name) {
				broadcastedThisTurn = true
			}
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

	finalText, iterationsUsed, streamErr, err := loop.run(ctx, msgs)
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

	if text != "" && !broadcastedThisTurn {
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
