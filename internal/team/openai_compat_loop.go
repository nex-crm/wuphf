package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// openAICompatToolLoop is the broker-agnostic core of
// runHeadlessOpenAICompatTurn. It drives the streaming chat-completions
// loop: stream → collect text + tool_uses → execute tools → restream until
// the model emits text without firing more tools (or maxIters trips).
//
// Pulled out as its own type so it can be tested without a Launcher,
// broker, or live server: a fake StreamFn + fake AgentTool.Execute is all
// it takes. See openai_compat_loop_test.go.
type openAICompatToolLoop struct {
	// streamFn is the per-agent provider StreamFn (returned by
	// entry.StreamFn(slug)). Called once per iteration with the
	// accumulated message history.
	streamFn agent.StreamFn

	// tools are passed verbatim into streamFn; toolByName routes tool_use
	// chunks back to their Execute callbacks.
	tools      []agent.AgentTool
	toolByName map[string]agent.AgentTool

	// maxIters caps the inner loop. Required (zero means "no iterations").
	maxIters int

	// toolTimeout bounds each individual tool execution. Required.
	toolTimeout time.Duration

	// Observability hooks. Any may be nil.
	onText       func(text string)
	onToolUse    func(name, rawInput string)
	onToolResult func(name, result string, err error)
	onError      func(msg string)
	onFirstEvent func()
	onFirstText  func()
	onFirstTool  func()
}

// run executes the loop and returns:
//   - finalText: the model's last text reply (the one shown to the user).
//     Only the LAST iteration's text counts: the final iteration is the one
//     that didn't fire any tools.
//   - iterations: how many streaming turns ran. Useful for latency logs.
//   - usage: summed token counts across every iteration this turn fired.
//     Populated only for usage chunks the SSE parser surfaces; servers
//     that don't honour stream_options.include_usage produce a zero value.
//   - streamErr: a model-side or transport error reported via an "error"
//     stream chunk. Returned as a string (not error) because it represents
//     the model failing, not the loop itself; the caller decides whether
//     to escalate.
//   - err: a context cancellation or other Go-side error.
func (lp *openAICompatToolLoop) run(ctx context.Context, msgs []agent.Message) (finalText string, iterations int, usage provider.ClaudeUsage, streamErr string, err error) {
	if lp.maxIters <= 0 {
		return "", 0, provider.ClaudeUsage{}, "", fmt.Errorf("openAICompatToolLoop: maxIters must be > 0")
	}
	if lp.toolTimeout <= 0 {
		return "", 0, provider.ClaudeUsage{}, "", fmt.Errorf("openAICompatToolLoop: toolTimeout must be > 0")
	}

	firstEventFired := false
	firstTextFired := false
	firstToolFired := false

	for iter := 0; iter < lp.maxIters; iter++ {
		iterations = iter + 1
		var (
			turnText     strings.Builder
			turnToolUses []agent.StreamChunk
			turnErr      string
		)

		ch := lp.streamFn(msgs, lp.tools)

	DRAIN:
		for {
			select {
			case <-ctx.Done():
				go func() {
					for range ch {
					}
				}()
				return "", iterations, usage, "", ctx.Err()
			case chunk, ok := <-ch:
				if !ok {
					break DRAIN
				}
				if !firstEventFired {
					firstEventFired = true
					if lp.onFirstEvent != nil {
						lp.onFirstEvent()
					}
				}
				switch chunk.Type {
				case "text":
					if strings.TrimSpace(chunk.Content) != "" && !firstTextFired {
						firstTextFired = true
						if lp.onFirstText != nil {
							lp.onFirstText()
						}
					}
					if lp.onText != nil {
						lp.onText(chunk.Content)
					}
					turnText.WriteString(chunk.Content)
				case "tool_use":
					if !firstToolFired {
						firstToolFired = true
						if lp.onFirstTool != nil {
							lp.onFirstTool()
						}
					}
					if lp.onToolUse != nil {
						lp.onToolUse(chunk.ToolName, chunk.ToolInput)
					}
					turnToolUses = append(turnToolUses, chunk)
				case "usage":
					// Output tokens are per-iteration (the tokens the
					// model just generated this round), so summing
					// across iterations gives the turn's total
					// generation cost.
					usage.OutputTokens += chunk.OutputTokens
					usage.CacheReadTokens += chunk.CacheReadTokens
					usage.CacheCreationTokens += chunk.CacheCreationTokens
					// Input tokens are cumulative WITHIN each iteration
					// (each request body includes the full prior
					// conversation: system + user + asst_iter1 +
					// tool_result_iter1 + ... + asst_iterN-1 +
					// tool_result_iterN-1). Summing across iterations
					// would double-count the system prompt N times for
					// an N-iteration turn, making mlx-lm's panel totals
					// 2-5× larger than equivalent Claude/Codex turns
					// for no real reason. The last iteration's
					// prompt_tokens is the turn's true input footprint.
					// Use max() rather than overwrite so a server that
					// emits a degenerate later frame can't shrink it.
					if chunk.InputTokens > usage.InputTokens {
						usage.InputTokens = chunk.InputTokens
					}
				case "error":
					turnErr = chunk.Content
					if lp.onError != nil {
						lp.onError(turnErr)
					}
				}
			}
		}

		if turnErr != "" {
			return "", iterations, usage, turnErr, nil
		}

		// The final reply is whichever iteration's text we last saw.
		// Keeping the running text means: even if the model fires a tool
		// AFTER emitting some text, the next iteration's text overwrites,
		// matching how OpenAI client libs surface the post-tool summary.
		if t := strings.TrimSpace(turnText.String()); t != "" {
			finalText = t
		}

		if len(turnToolUses) == 0 {
			return finalText, iterations, usage, "", nil
		}

		// Encode the assistant's tool intent as a plain text turn so the
		// next iteration's prompt has the same conversation prefix.
		// agent.Message lacks structured tool_calls, so we serialize as
		// `[tool_call <name> <args-json>]` lines — practical for any
		// OpenAI-compatible model since they all parse mixed-format
		// transcripts fine.
		msgs = append(msgs, agent.Message{
			Role:    "assistant",
			Content: encodeAssistantToolIntent(turnText.String(), turnToolUses),
		})

		// Execute each tool and append the consolidated results. We do
		// this serially: parallel tool execution would need either OpenAI's
		// structured tool_call_id round-trip (which agent.Message doesn't
		// carry) or risk the model getting confused about which result
		// belongs to which call.
		var resultBlocks []string
		for _, tc := range turnToolUses {
			tool, ok := lp.toolByName[tc.ToolName]
			if !ok || tool.Execute == nil {
				resultBlocks = append(resultBlocks, fmt.Sprintf(
					"Tool %q is not available — only call tools listed in the function schema.", tc.ToolName))
				if lp.onToolResult != nil {
					lp.onToolResult(tc.ToolName, "", fmt.Errorf("not available"))
				}
				continue
			}
			execCtx, execCancel := context.WithTimeout(ctx, lp.toolTimeout)
			out, execErr := tool.Execute(tc.ToolParams, execCtx, nil)
			execCancel()
			if execErr != nil {
				resultBlocks = append(resultBlocks, fmt.Sprintf("Tool %q failed: %v", tc.ToolName, execErr))
				if lp.onToolResult != nil {
					lp.onToolResult(tc.ToolName, "", execErr)
				}
				continue
			}
			resultBlocks = append(resultBlocks, fmt.Sprintf("Tool %q returned:\n%s", tc.ToolName, strings.TrimSpace(out)))
			if lp.onToolResult != nil {
				lp.onToolResult(tc.ToolName, out, nil)
			}
		}
		// TODO: localize the trailer — non-English-speaking deployments may
		// want this hook configurable. Hardcoded English for v1 because all
		// agent prompts in wuphf today assume English.
		msgs = append(msgs, agent.Message{
			Role:    "user",
			Content: strings.Join(resultBlocks, "\n\n") + "\n\nIf the tool results answer the user's request, reply with a final message. Only call another tool if it's strictly required.",
		})
	}

	// Loop exhausted. Surface a marker as the final text so the broker's
	// "post if silent" hook still fires — without this the user sees a
	// silent failure (streamErr propagates up but `text != ""` gates the
	// channel post in runHeadlessOpenAICompatTurn).
	if finalText == "" {
		finalText = fmt.Sprintf("(tool loop hit %d iterations without resolving — see latency log for details)", lp.maxIters)
	}
	return finalText, iterations, usage, fmt.Sprintf("openai-compat: tool loop exceeded %d iterations without resolving", lp.maxIters), nil
}

// encodeAssistantToolIntent serializes the model's tool intent as the
// content of a synthetic assistant message. The wuphf agent.Message shape
// doesn't carry structured tool_calls, so we use a `[tool_call name args]`
// trailer that any chat-completion model parses fine in subsequent turns.
//
// ToolUseID is intentionally not serialized: this loop forces serial tool
// execution (one call per iteration) so there is no ambiguity to resolve
// in the prompt history. If a future extension allows parallel tool
// execution, the IDs will need to thread through here so the model can
// disambiguate which result belongs to which call.
func encodeAssistantToolIntent(prefixText string, toolUses []agent.StreamChunk) string {
	var b strings.Builder
	if t := strings.TrimSpace(prefixText); t != "" {
		b.WriteString(t)
		b.WriteString("\n\n")
	}
	for _, tc := range toolUses {
		input := strings.TrimSpace(tc.ToolInput)
		if input == "" {
			if data, err := json.Marshal(tc.ToolParams); err == nil {
				input = string(data)
			}
		}
		fmt.Fprintf(&b, "[tool_call %s %s]\n", tc.ToolName, input)
	}
	return strings.TrimSpace(b.String())
}
