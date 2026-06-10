package team

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// HeadlessEvent is the canonical, provider-agnostic envelope for a single
// progress signal emitted from a headless agent turn. All four runners
// (Claude, Codex, Opencode, OpenAI-compatible) populate the same shape so
// the web UI can render a normalized timeline regardless of which
// provider is executing.
//
// Wire shape: emitted as one JSONL line on /agent-stream/{slug} with
// `kind` set to "headless_event" so the frontend can branch on a single
// discriminator without inspecting type-specific fields. The line lives
// alongside the raw provider chunks the runner already tees into the
// stream — additive for now so existing consumers keep working; future
// slices may replace the raw tee once the typed channel is the system
// of record.
//
// Field semantics:
//
//   - Kind: always "headless_event". Lets a JSON.parse-then-discriminate
//     consumer skip provider-native events without a structural sniff.
//   - Type: phase of the turn — "status", "text", "tool_use",
//     "tool_result", "idle", "error", "manifest". A2-MVP emitted only
//     "idle" and "error"; A3 wired the intermediate phases; A4 adds
//     "manifest" — a per-turn completion summary emitted after the
//     terminal idle/error event.
//   - Provider: "claude" | "codex" | "opencode" | "openai-compat".
//   - Agent: the speaker slug (the agent the turn belongs to).
//   - TurnID, TaskID, ParentID: correlation IDs. TurnID groups events
//     from one ReadXxxJSONStream call. TaskID is the broker task the
//     turn is servicing (already used for SSE scoping in /agent-stream
//     ?task=). ParentID is reserved for nested tool/sub-agent calls.
//   - ToolName, Detail: payload for tool_use / tool_result / error.
//   - Text: payload for text events (and the human-readable summary
//     for idle).
//   - Status: "active" | "idle" | "error" — mirrors the activity
//     snapshot status so a single event drives both the timeline and
//     status-pill subscribers.
//   - StartedAt: RFC3339 timestamp from the runner's clock so ordering
//     survives reordering at the SSE boundary and replay timing is
//     reconstructable.
//   - Metrics: turn-level latency and token totals. Populated on idle
//     and manifest.
//   - RawType: the underlying provider event type for debug tooling.
//     Empty for runner-synthesized events like idle and manifest.
//   - ToolCalls: populated only on manifest events. Sorted list of
//     distinct tools called during the turn with per-tool call counts.
//   - TextLen: populated only on manifest events. Total byte length of
//     all text chunks emitted during the turn.
type HeadlessEvent struct {
	Kind      string                  `json:"kind"`
	Type      string                  `json:"type"`
	Provider  string                  `json:"provider,omitempty"`
	Agent     string                  `json:"agent,omitempty"`
	TurnID    string                  `json:"turn_id,omitempty"`
	TaskID    string                  `json:"task_id,omitempty"`
	ParentID  string                  `json:"parent_id,omitempty"`
	ToolName  string                  `json:"tool_name,omitempty"`
	Text      string                  `json:"text,omitempty"`
	Detail    string                  `json:"detail,omitempty"`
	Status    string                  `json:"status,omitempty"`
	StartedAt string                  `json:"started_at,omitempty"`
	Metrics   *HeadlessEventMetrics   `json:"metrics,omitempty"`
	RawType   string                  `json:"raw_type,omitempty"`
	ToolCalls []HeadlessManifestEntry `json:"tool_calls,omitempty"`
	TextLen   *int                    `json:"text_len,omitempty"`
}

// HeadlessManifestEntry is one tool in a manifest event's ToolCalls list.
// Count is the number of times that tool was called during the turn.
type HeadlessManifestEntry struct {
	ToolName string `json:"tool_name"`
	Count    int    `json:"count"`
}

// HeadlessEventMetrics carries turn-level timing and token totals. All
// values are optional; zero is treated as "not measured".
type HeadlessEventMetrics struct {
	TotalMs      int64 `json:"total_ms,omitempty"`
	FirstEventMs int64 `json:"first_event_ms,omitempty"`
	FirstTextMs  int64 `json:"first_text_ms,omitempty"`
	FirstToolMs  int64 `json:"first_tool_ms,omitempty"`
	InputTokens  int   `json:"input_tokens,omitempty"`
	OutputTokens int   `json:"output_tokens,omitempty"`
}

// Constants for the discriminator and stable Type values. Wire-format
// strings — keep in lockstep with the frontend's HeadlessEventView.
const (
	HeadlessEventKind = "headless_event"

	HeadlessEventTypeStatus     = "status"
	HeadlessEventTypeText       = "text"
	HeadlessEventTypeToolUse    = "tool_use"
	HeadlessEventTypeToolResult = "tool_result"
	HeadlessEventTypeIdle       = "idle"
	HeadlessEventTypeError      = "error"
	HeadlessEventTypeManifest   = "manifest"

	HeadlessProviderClaude       = "claude"
	HeadlessProviderCodex        = "codex"
	HeadlessProviderOpencode     = "opencode"
	HeadlessProviderOpenAICompat = "openai-compat"
)

// pushHeadlessEvent serializes event as a single JSON line and writes it
// into stream's task-scoped buffer. Kind is forced to the canonical
// discriminator and StartedAt defaults to now() so callers cannot forget
// either. A nil stream is a no-op so callers do not need a guard around
// every test path that constructs a runner without a broker.
func pushHeadlessEvent(stream *agentStreamBuffer, event HeadlessEvent) {
	if stream == nil {
		return
	}
	event.Kind = HeadlessEventKind
	if strings.TrimSpace(event.StartedAt) == "" {
		event.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	// PushTask appends the line as-is; we add a trailing newline so the
	// SSE serializer in handleAgentStream can keep its `data: %s\n\n`
	// framing without special-casing event lines.
	stream.PushTask(strings.TrimSpace(event.TaskID), string(data)+"\n")
}

// headlessProgressEventMetrics adapts the runner-side
// headlessProgressMetrics into the wire-format HeadlessEventMetrics. The
// runner's `-1` sentinel for "not measured" maps to JSON `omitempty`
// zeros so the SSE payload stays compact and the frontend can treat
// missing fields the same way it treats zero.
func headlessProgressEventMetrics(m headlessProgressMetrics, usage *headlessTokenUsage) *HeadlessEventMetrics {
	out := HeadlessEventMetrics{}
	if m.TotalMs >= 0 {
		out.TotalMs = m.TotalMs
	}
	if m.FirstEventMs >= 0 {
		out.FirstEventMs = m.FirstEventMs
	}
	if m.FirstTextMs >= 0 {
		out.FirstTextMs = m.FirstTextMs
	}
	if m.FirstToolMs >= 0 {
		out.FirstToolMs = m.FirstToolMs
	}
	if usage != nil {
		out.InputTokens = usage.InputTokens
		out.OutputTokens = usage.OutputTokens
	}
	if out == (HeadlessEventMetrics{}) {
		return nil
	}
	return &out
}

// headlessTokenUsage is a runner-agnostic shape for the optional token
// totals attached to a terminal HeadlessEvent. Each runner already has a
// provider-specific usage struct; this is the smallest envelope all four
// can populate without leaking the provider type.
type headlessTokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// emitHeadlessTerminal pushes either an idle or error HeadlessEvent to
// the agent stream at the end of a turn. Callers pass the same status
// summary they fed to updateHeadlessProgress so the activity-pill text
// and the timeline event stay aligned. Provider is the wire-format
// constant (HeadlessProviderClaude, etc).
func emitHeadlessTerminal(stream *agentStreamBuffer, provider, slug, taskID, summary, errDetail string, metrics headlessProgressMetrics, usage *headlessTokenUsage) {
	emitHeadlessTerminalWithTurn(stream, "", provider, slug, taskID, summary, errDetail, metrics, usage)
}

// emitHeadlessTerminalWithTurn is the turn-aware variant of
// emitHeadlessTerminal. Pass the same turnID used for the per-phase
// emits so all events from one turn carry a stable correlation key.
func emitHeadlessTerminalWithTurn(stream *agentStreamBuffer, turnID, provider, slug, taskID, summary, errDetail string, metrics headlessProgressMetrics, usage *headlessTokenUsage) {
	if stream == nil {
		return
	}
	event := HeadlessEvent{
		Provider: provider,
		Agent:    slug,
		TurnID:   strings.TrimSpace(turnID),
		TaskID:   strings.TrimSpace(taskID),
		Metrics:  headlessProgressEventMetrics(metrics, usage),
	}
	if strings.TrimSpace(errDetail) != "" {
		event.Type = HeadlessEventTypeError
		event.Status = "error"
		event.Detail = errDetail
		event.Text = errDetail
	} else {
		event.Type = HeadlessEventTypeIdle
		event.Status = "idle"
		event.Text = summary
	}
	pushHeadlessEvent(stream, event)
}

// emitHeadlessText pushes a text-phase HeadlessEvent. text is the
// user-facing chunk the model just produced. rawType is the underlying
// provider event name (e.g. "content_block_delta", "response.output_text.delta")
// — empty when the runner cannot supply one.
//
// Empty text is dropped so trivially-empty text-deltas (provider noise
// during preamble) don't pollute the timeline.
func emitHeadlessText(stream *agentStreamBuffer, turnID, provider, slug, taskID, text, rawType string) {
	if stream == nil || strings.TrimSpace(text) == "" {
		return
	}
	pushHeadlessEvent(stream, HeadlessEvent{
		Type:     HeadlessEventTypeText,
		Provider: provider,
		Agent:    slug,
		TurnID:   strings.TrimSpace(turnID),
		TaskID:   strings.TrimSpace(taskID),
		Text:     text,
		Status:   "active",
		RawType:  rawType,
	})
}

// emitHeadlessToolUse pushes a tool_use-phase HeadlessEvent. toolInput is
// the JSON-serialized arguments string the runner already builds (kept as
// string so the wire shape is stable across providers that pre-stream
// arguments differently).
func emitHeadlessToolUse(stream *agentStreamBuffer, turnID, provider, slug, taskID, toolName, toolInput, rawType string) {
	if stream == nil || strings.TrimSpace(toolName) == "" {
		return
	}
	pushHeadlessEvent(stream, HeadlessEvent{
		Type:     HeadlessEventTypeToolUse,
		Provider: provider,
		Agent:    slug,
		TurnID:   strings.TrimSpace(turnID),
		TaskID:   strings.TrimSpace(taskID),
		ToolName: strings.TrimSpace(toolName),
		Detail:   toolInput,
		Status:   "active",
		RawType:  rawType,
	})
}

// emitHeadlessToolResult pushes a tool_result-phase HeadlessEvent. text is
// the truncated result summary the runner already prepares for logs.
func emitHeadlessToolResult(stream *agentStreamBuffer, turnID, provider, slug, taskID, toolName, text, rawType string) {
	if stream == nil || strings.TrimSpace(toolName) == "" {
		return
	}
	pushHeadlessEvent(stream, HeadlessEvent{
		Type:     HeadlessEventTypeToolResult,
		Provider: provider,
		Agent:    slug,
		TurnID:   strings.TrimSpace(turnID),
		TaskID:   strings.TrimSpace(taskID),
		ToolName: strings.TrimSpace(toolName),
		Text:     text,
		Status:   "active",
		RawType:  rawType,
	})
}

// emitHeadlessManifest pushes a manifest HeadlessEvent after the terminal
// idle/error event. It is the machine-readable per-turn completion record:
// which tools were called (deduplicated with counts), how many text bytes
// were produced, and the same metrics as the idle event. Downstream
// consumers — task details view, notebook auto-writer, cost analytics —
// can query a single event type rather than replaying the full turn stream.
//
// toolNames is the ordered sequence of tool names as called; duplicates are
// collapsed into a count. textLen is the sum of len(chunk) for every text
// event emitted during the turn. Both may be zero for turns that produced
// no tools or no text — the manifest is still emitted so consumers see a
// consistent turn boundary.
func emitHeadlessManifest(stream *agentStreamBuffer, turnID, provider, slug, taskID, errDetail string, toolNames []string, textLen int, metrics headlessProgressMetrics, usage *headlessTokenUsage) {
	if stream == nil {
		return
	}
	counts := make(map[string]int, len(toolNames))
	for _, name := range toolNames {
		if name = strings.TrimSpace(name); name != "" {
			counts[name]++
		}
	}
	calls := make([]HeadlessManifestEntry, 0, len(counts))
	for name, count := range counts {
		calls = append(calls, HeadlessManifestEntry{ToolName: name, Count: count})
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].ToolName < calls[j].ToolName })
	status := "idle"
	if strings.TrimSpace(errDetail) != "" {
		status = "error"
	}
	pushHeadlessEvent(stream, HeadlessEvent{
		Type:      HeadlessEventTypeManifest,
		Provider:  provider,
		Agent:     slug,
		TurnID:    strings.TrimSpace(turnID),
		TaskID:    strings.TrimSpace(taskID),
		Status:    status,
		ToolCalls: calls,
		TextLen:   &textLen,
		Metrics:   headlessProgressEventMetrics(metrics, usage),
	})
}

// turnIDCounter is a process-local fallback when crypto/rand fails (it
// almost never does). Combined with a per-process-start nanosecond it
// keeps generated IDs unique even when the system entropy source is
// briefly unavailable.
var turnIDCounter atomic.Uint64

// newHeadlessTurnID returns a short, opaque correlation ID for one
// runner turn. Callers attach this to every HeadlessEvent they emit so
// downstream consumers can group events from the same turn without
// pattern-matching on text content. Format is implementation-detail —
// hex string today; treat as opaque.
func newHeadlessTurnID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	// crypto/rand failure is rare on supported platforms; fall back to
	// the atomic counter so we never block a turn for a missing ID.
	return fmt.Sprintf("ctr-%d-%d", time.Now().UnixNano(), turnIDCounter.Add(1))
}
