# Codex Streaming Notes from Hermes and OpenClaw

Date: 2026-04-09

## Question

How do Hermes and OpenClaw make Codex-backed agents feel smoother and more natural than WUPHF?

## Short answer

They do not wait for a final response blob.

- Hermes streams the OpenAI Responses API directly and treats stream events as the source of truth.
- OpenClaw keeps a provider-to-runtime-to-UI event pipeline and translates low-level provider events into stable runtime/UI events immediately.
- Both add fallback/backfill logic for Codex edge cases instead of dropping back to "wait for completion, then render".

## Hermes

### What Hermes does

Hermes uses `responses.stream(...)` for Codex and consumes events directly.

Source:
- `/tmp/hermes-agent/run_agent.py`
- `/tmp/hermes-agent/agent/auxiliary_client.py`

Key behaviors:

1. Stream text deltas as they arrive.
   - `run_agent.py` watches `response.output_text.delta` and immediately fires `_fire_stream_delta(delta_text)`.
   - It also fires an `on_first_delta` hook so the UI can switch from spinner to live output as soon as the first token lands.

2. Suppress plain-text streaming while tool calls are in progress.
   - Hermes tracks whether function-call events have appeared and avoids naively treating mixed tool output as plain assistant prose.

3. Backfill empty final responses from stream events.
   - Codex sometimes yields valid `response.output_item.done` events while `get_final_response().output` is empty.
   - Hermes collects completed output items during streaming and patches the final response object if needed.
   - If there were only text deltas and no function calls, Hermes synthesizes a final assistant message from the collected deltas.

4. Preserve readability across tool boundaries.
   - Hermes explicitly inserts a paragraph break before the first text delta after a tool iteration so text does not visually concatenate across tool phases.
   - Its stream consumer also creates a new message segment when it hits tool boundaries.

5. Keep a streaming fallback path.
   - If the primary stream path fails, Hermes falls back to `responses.create(..., stream=True)` and still reconstructs output from streamed events.

### Why it feels smooth

- First visible output appears on the first token, not after the full turn.
- Tool phases are visible but do not corrupt text rendering.
- Codex stream weirdness is repaired inside the streaming layer instead of leaking into UX as "nothing happened".

### Most relevant code

- `/tmp/hermes-agent/run_agent.py:3800`
- `/tmp/hermes-agent/run_agent.py:3836`
- `/tmp/hermes-agent/run_agent.py:3857`
- `/tmp/hermes-agent/run_agent.py:3892`
- `/tmp/hermes-agent/run_agent.py:4257`
- `/tmp/hermes-agent/agent/auxiliary_client.py:314`
- `/tmp/hermes-agent/agent/auxiliary_client.py:322`
- `/tmp/hermes-agent/agent/auxiliary_client.py:332`
- `/tmp/hermes-agent/tests/gateway/test_stream_consumer.py:284`

## OpenClaw

### What OpenClaw does

OpenClaw keeps a real event pipeline from the model transport up through Gateway and ACP/web clients.

Sources:
- `/tmp/openclaw/src/agents/openai-ws-connection.ts`
- `/tmp/openclaw/src/agents/openai-ws-stream.ts`
- `/tmp/openclaw/src/gateway/openresponses-http.ts`
- `/tmp/openclaw/docs.acp.md`

Key behaviors:

1. Use structured Responses API streaming events.
   - OpenClaw models Codex/OpenAI events explicitly:
     - `response.output_item.added`
     - `response.output_item.done`
     - `response.content_part.added`
     - `response.content_part.done`
     - `response.output_text.delta`
     - `response.output_text.done`
     - `response.function_call_arguments.delta`
     - `response.function_call_arguments.done`

2. Buffer by output item and content part, not just by raw text.
   - `openai-ws-stream.ts` stores text by `(item_id, content_index)`.
   - It waits until the output item phase is known, then emits buffered deltas in the correct logical place.
   - This prevents text from appearing in the wrong message bucket when the provider emits deltas before the surrounding item metadata settles.

3. Use a persistent streaming transport.
   - OpenClaw’s Codex/OpenAI path is built around a WebSocket manager and a streaming event stream, not one-shot CLI completion.
   - If the WebSocket path fails before any output, it can degrade to HTTP while preserving the streaming contract.

4. Translate runtime events into frontend-friendly SSE events.
   - `openresponses-http.ts` subscribes to runtime agent events.
   - Assistant deltas are immediately written as `response.output_text.delta` SSE events.
   - The endpoint also emits the proper `response.created`, `response.in_progress`, `response.output_item.added`, `response.content_part.added`, and `response.completed` scaffolding around the stream.

5. Keep a fallback when no deltas arrive.
   - If the run completes without streaming assistant deltas, OpenClaw still sends the full response text as a final delta rather than leaving the UI blank.

6. Keep protocol translation eventful.
   - The ACP bridge maps Gateway streaming events into ACP `message` and `tool_call` updates instead of waiting for a post-hoc transcript replay.

### Why it feels smooth

- The UI has a stable lifecycle to render immediately: response started, text streaming, tool call updates, response done.
- Provider quirks are normalized at the transport/runtime boundary.
- Streaming is not a UI hack; it is part of the core runtime contract.

### Most relevant code

- `/tmp/openclaw/src/agents/openai-ws-connection.ts:125`
- `/tmp/openclaw/src/agents/openai-ws-stream.ts:1019`
- `/tmp/openclaw/src/agents/openai-ws-stream.ts:1058`
- `/tmp/openclaw/src/agents/openai-ws-stream.ts:1085`
- `/tmp/openclaw/src/gateway/openresponses-http.ts:879`
- `/tmp/openclaw/src/gateway/openresponses-http.ts:911`
- `/tmp/openclaw/src/gateway/openresponses-http.ts:930`
- `/tmp/openclaw/src/gateway/openresponses-http.ts:1072`
- `/tmp/openclaw/docs.acp.md:206`

## What WUPHF is missing

Current WUPHF behavior is much coarser:

- `internal/provider/codex.go` waits for `runCodexOnce(...)` to fully finish before emitting any text.
- After completion, it fake-streams the final text with `streamTextChunks(...)`.
- The headless Codex path in `internal/team/headless_codex.go` similarly only keeps final message text, not intermediate events.
- Notification and UI paths are poll-based, which adds more delay even before rendering starts.

That means WUPHF has:

- no true time-to-first-token improvement
- no structured tool-call stream lifecycle
- no output-item-aware buffering
- no Codex empty-output backfill from stream events
- no runtime event contract that the UI can trust

## Concrete lessons for WUPHF

### 1. Treat Codex stream events as canonical

Do not run Codex to completion and then simulate streaming. Parse the actual event stream and emit internal events as they arrive.

### 2. Add output backfill logic

If the terminal/final response object is empty, reconstruct from:

- completed output items first
- text deltas second

Hermes does both.

### 3. Model streaming as lifecycle events, not raw text only

At minimum WUPHF should have:

- response_started
- output_item_added
- text_delta
- text_done
- tool_call_started
- tool_call_args_delta
- tool_call_done
- response_completed
- response_failed

### 4. Separate message segments across tool boundaries

Without this, streamed text and tool activity visually smear together and the conversation feels sloppy even when it is technically fast.

### 5. Push events to the UI directly

OpenClaw feels responsive because the provider stream becomes runtime events and then SSE/ACP events immediately. WUPHF currently adds polling between each stage.

### 6. Keep fallback streaming, not fallback blocking

If the preferred streaming transport fails, degrade to another streaming path before degrading to "wait for final text".

## Proposed WUPHF direction

If we port the same pattern, the minimal plan is:

1. Replace `runCodexOnce(...)` in `internal/provider/codex.go` with a real stream consumer.
2. Introduce a structured internal Codex event type instead of only `text/error`.
3. Teach `internal/agent/loop.go` to forward tool-use lifecycle and text deltas from Codex directly.
4. Add backfill logic for empty final output using collected stream events.
5. Replace the tmux/web poll-only rendering path with direct event delivery wherever possible.
6. Add message segmentation after tool boundaries so the stream stays readable.

## Bottom line

Hermes and OpenClaw feel smooth because they solved the transport and event-model problem, not because Codex itself is magically faster there.

They:

- stream real events
- normalize provider quirks
- preserve tool boundaries
- backfill missing final output
- expose a stable runtime event contract to the UI

WUPHF currently blocks on final completion and then imitates streaming afterward. That is the main gap.
