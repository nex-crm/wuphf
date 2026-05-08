package team

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPushHeadlessEventEncodesDiscriminatorAndPushes pins the wire shape
// the frontend depends on: every emitted line carries kind:"headless_event"
// (so the React StreamLineView's branch-by-discriminator can short-circuit
// without inspecting type-specific fields), and the line lands in the
// agentStream task buffer with the canonical JSON encoding.
func TestPushHeadlessEventEncodesDiscriminatorAndPushes(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}

	pushHeadlessEvent(stream, HeadlessEvent{
		Type:     HeadlessEventTypeIdle,
		Provider: HeadlessProviderClaude,
		Agent:    "ceo",
		TaskID:   "task-42",
		Text:     "reply ready · ttft 120ms",
		Status:   "idle",
		Metrics: &HeadlessEventMetrics{
			TotalMs:      1500,
			FirstTextMs:  120,
			InputTokens:  300,
			OutputTokens: 200,
		},
	})

	got := stream.recentTask("task-42")
	if len(got) != 1 {
		t.Fatalf("expected 1 line in task buffer, got %d (%v)", len(got), got)
	}
	if !strings.HasSuffix(got[0], "\n") {
		t.Fatalf("expected trailing newline so SSE framing stays intact, got %q", got[0])
	}
	var decoded HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(got[0], "\n")), &decoded); err != nil {
		t.Fatalf("emitted line is not valid HeadlessEvent JSON: %v\nline=%q", err, got[0])
	}
	if decoded.Kind != HeadlessEventKind {
		t.Fatalf("kind discriminator: want %q, got %q", HeadlessEventKind, decoded.Kind)
	}
	if decoded.Type != HeadlessEventTypeIdle {
		t.Fatalf("type: want idle, got %q", decoded.Type)
	}
	if decoded.Provider != HeadlessProviderClaude || decoded.Agent != "ceo" {
		t.Fatalf("provider/agent: %+v", decoded)
	}
	if decoded.StartedAt == "" {
		t.Fatal("StartedAt must be populated by pushHeadlessEvent default")
	}
	if decoded.Metrics == nil || decoded.Metrics.TotalMs != 1500 || decoded.Metrics.InputTokens != 300 {
		t.Fatalf("metrics not encoded faithfully: %+v", decoded.Metrics)
	}
}

// TestPushHeadlessEventNilStreamIsSafe pins the no-op contract for
// callers that construct a runner without a broker (every test of the
// runners would otherwise need a guard at every emit site).
func TestPushHeadlessEventNilStreamIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("pushHeadlessEvent on nil stream panicked: %v", r)
		}
	}()
	pushHeadlessEvent(nil, HeadlessEvent{Type: HeadlessEventTypeIdle})
}

// TestEmitHeadlessTerminalIdleAndError exercises the runner-side helper
// that all four headless runners call at their terminal exit points. The
// idle path must encode status="idle" + the human-readable summary; the
// error path must encode status="error" + the error detail. Both shapes
// flow into the same wire envelope so a single React component can
// render both.
func TestEmitHeadlessTerminalIdleAndError(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}

	metrics := headlessProgressMetrics{TotalMs: 800, FirstTextMs: 90}
	emitHeadlessTerminal(stream, HeadlessProviderCodex, "eng", "task-7", "reply ready · ttft 90ms", "", metrics, &headlessTokenUsage{InputTokens: 100, OutputTokens: 60})
	emitHeadlessTerminal(stream, HeadlessProviderCodex, "eng", "task-7", "", "auth: 401 unauthorized", metrics, nil)

	lines := stream.recentTask("task-7")
	if len(lines) != 2 {
		t.Fatalf("expected idle + error, got %d lines: %v", len(lines), lines)
	}
	var idle HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &idle); err != nil {
		t.Fatalf("idle decode: %v", err)
	}
	if idle.Type != HeadlessEventTypeIdle || idle.Status != "idle" {
		t.Fatalf("idle envelope: %+v", idle)
	}
	if idle.Text == "" {
		t.Fatal("idle Text must carry the latency summary")
	}
	if idle.Metrics == nil || idle.Metrics.InputTokens != 100 {
		t.Fatalf("idle metrics: %+v", idle.Metrics)
	}

	var errEvt HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[1], "\n")), &errEvt); err != nil {
		t.Fatalf("error decode: %v", err)
	}
	if errEvt.Type != HeadlessEventTypeError || errEvt.Status != "error" {
		t.Fatalf("error envelope: %+v", errEvt)
	}
	if errEvt.Detail != "auth: 401 unauthorized" || errEvt.Text != "auth: 401 unauthorized" {
		t.Fatalf("error detail/text: %+v", errEvt)
	}
}

// TestEmitHeadlessTextDropsEmptyAndCarriesTurn pins the per-phase text
// emit contract: empty text is silently dropped (so trivially-empty
// stream noise during preamble doesn't pollute the timeline), but a
// text chunk carries kind="headless_event", type="text", the turn
// correlation key, and the runner-supplied raw_type for debug tooling.
func TestEmitHeadlessTextDropsEmptyAndCarriesTurn(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}

	// Empty / whitespace-only text must not produce an event.
	emitHeadlessText(stream, "turn-1", HeadlessProviderClaude, "ceo", "task-1", "", "claude.text")
	emitHeadlessText(stream, "turn-1", HeadlessProviderClaude, "ceo", "task-1", "   ", "claude.text")
	if got := stream.recentTask("task-1"); len(got) != 0 {
		t.Fatalf("empty text must be dropped, got %d lines: %v", len(got), got)
	}

	emitHeadlessText(stream, "turn-1", HeadlessProviderClaude, "ceo", "task-1", "shipping the update", "claude.text")
	got := stream.recentTask("task-1")
	if len(got) != 1 {
		t.Fatalf("expected 1 text event, got %d: %v", len(got), got)
	}
	var event HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(got[0], "\n")), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if event.Type != HeadlessEventTypeText {
		t.Fatalf("type: want text, got %q", event.Type)
	}
	if event.TurnID != "turn-1" {
		t.Fatalf("turn id missing: %+v", event)
	}
	if event.RawType != "claude.text" {
		t.Fatalf("raw_type missing: %+v", event)
	}
	if event.Status != "active" {
		t.Fatalf("status: want active, got %q", event.Status)
	}
}

// TestEmitHeadlessToolUseAndToolResult pins the tool-phase wire shape:
// tool_use carries the tool name + serialized arguments; tool_result
// carries the truncated summary text. Both variants share the same
// turn id so a downstream consumer can correlate call -> result.
func TestEmitHeadlessToolUseAndToolResult(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}

	emitHeadlessToolUse(stream, "turn-2", HeadlessProviderCodex, "eng", "task-7", "team_broadcast", `{"channel":"general","content":"shipped"}`, "response.function_call.delta")
	emitHeadlessToolResult(stream, "turn-2", HeadlessProviderCodex, "eng", "task-7", "team_broadcast", "Posted to #general as @eng", "response.function_call_result")

	// Empty tool name must be silently dropped — the runner's stream
	// callback can't always tag a name (Codex pre-streams arguments
	// before the name lands in the same chunk), and we don't want a
	// nameless tool_use polluting the timeline.
	emitHeadlessToolUse(stream, "turn-2", HeadlessProviderCodex, "eng", "task-7", "", "{}", "")

	lines := stream.recentTask("task-7")
	if len(lines) != 2 {
		t.Fatalf("expected 2 events (tool_use + tool_result), got %d: %v", len(lines), lines)
	}
	var use, res HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &use); err != nil {
		t.Fatalf("tool_use decode: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[1], "\n")), &res); err != nil {
		t.Fatalf("tool_result decode: %v", err)
	}
	if use.Type != HeadlessEventTypeToolUse || res.Type != HeadlessEventTypeToolResult {
		t.Fatalf("types: %+v %+v", use, res)
	}
	if use.TurnID != "turn-2" || res.TurnID != "turn-2" {
		t.Fatalf("correlation lost: %+v %+v", use, res)
	}
	if use.ToolName != "team_broadcast" || res.ToolName != "team_broadcast" {
		t.Fatalf("tool name: %+v %+v", use, res)
	}
	if use.Detail == "" {
		t.Fatalf("tool_use Detail must carry serialized arguments: %+v", use)
	}
	if res.Text == "" {
		t.Fatalf("tool_result Text must carry summary: %+v", res)
	}
}

// TestNewHeadlessTurnIDIsUnique pins the contract: every call returns a
// distinct opaque ID. The runner uses this to correlate text/tool events
// from the same turn — two turns colliding would silently merge their
// timelines. crypto/rand-backed; collision is astronomically unlikely.
func TestNewHeadlessTurnIDIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := newHeadlessTurnID()
		if id == "" {
			t.Fatalf("turn id %d empty", i)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("turn id %d collided: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestEmitHeadlessManifestAggregatesAndSorts pins the per-turn manifest
// emit contract: tool names are deduplicated with per-tool counts, the
// list is sorted alphabetically so wire output is deterministic, and the
// event carries the same turn/task correlation IDs as per-phase events.
func TestEmitHeadlessManifestAggregatesAndSorts(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	metrics := headlessProgressMetrics{TotalMs: 2000, FirstTextMs: 80}

	// Read called 3 times, Edit called 1 time, Bash called 2 times — expect
	// Bash×2, Edit×1, Read×3 in alphabetical order.
	toolNames := []string{"Read", "Bash", "Edit", "Read", "Bash", "Read"}
	emitHeadlessManifest(stream, "turn-m1", HeadlessProviderClaude, "ceo", "task-99", "",
		toolNames, 1420, metrics, &headlessTokenUsage{InputTokens: 500, OutputTokens: 300})

	lines := stream.recentTask("task-99")
	if len(lines) != 1 {
		t.Fatalf("expected 1 manifest event, got %d: %v", len(lines), lines)
	}
	var ev HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Kind != HeadlessEventKind {
		t.Fatalf("kind: want %q, got %q", HeadlessEventKind, ev.Kind)
	}
	if ev.Type != HeadlessEventTypeManifest {
		t.Fatalf("type: want manifest, got %q", ev.Type)
	}
	if ev.TurnID != "turn-m1" {
		t.Fatalf("turn_id: want turn-m1, got %q", ev.TurnID)
	}
	if ev.Status != "idle" {
		t.Fatalf("status: want idle, got %q", ev.Status)
	}
	if ev.TextLen == nil || *ev.TextLen != 1420 {
		v := 0
		if ev.TextLen != nil {
			v = *ev.TextLen
		}
		t.Fatalf("text_len: want 1420, got %d", v)
	}
	if ev.Metrics == nil || ev.Metrics.TotalMs != 2000 || ev.Metrics.InputTokens != 500 {
		t.Fatalf("metrics: %+v", ev.Metrics)
	}

	// Verify dedup + sort
	want := []HeadlessManifestEntry{
		{ToolName: "Bash", Count: 2},
		{ToolName: "Edit", Count: 1},
		{ToolName: "Read", Count: 3},
	}
	if len(ev.ToolCalls) != len(want) {
		t.Fatalf("tool_calls length: want %d, got %d: %+v", len(want), len(ev.ToolCalls), ev.ToolCalls)
	}
	for i, w := range want {
		g := ev.ToolCalls[i]
		if g.ToolName != w.ToolName || g.Count != w.Count {
			t.Fatalf("tool_calls[%d]: want %+v, got %+v", i, w, g)
		}
	}
}

// TestEmitHeadlessManifestErrorTurnStatus verifies that a non-empty errDetail
// flips the manifest status to "error" so consumers can distinguish failed
// turns without also reading the preceding idle/error event.
func TestEmitHeadlessManifestErrorTurnStatus(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	emitHeadlessManifest(stream, "turn-err", HeadlessProviderCodex, "eng", "task-e", "auth: 401",
		nil, 0, headlessProgressMetrics{TotalMs: 300}, nil)
	lines := stream.recentTask("task-e")
	if len(lines) != 1 {
		t.Fatalf("expected 1 manifest event, got %d", len(lines))
	}
	var ev HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Status != "error" {
		t.Fatalf("status: want error on non-empty errDetail, got %q", ev.Status)
	}
}

// TestEmitHeadlessManifestEmptyTools verifies a turn with no tool calls
// still emits a manifest with zero-length ToolCalls (omitted from wire
// JSON due to omitempty) but still carries TextLen and Metrics.
func TestEmitHeadlessManifestEmptyTools(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	metrics := headlessProgressMetrics{TotalMs: 500}

	emitHeadlessManifest(stream, "turn-m2", HeadlessProviderCodex, "eng", "task-1", "",
		nil, 850, metrics, nil)

	lines := stream.recentTask("task-1")
	if len(lines) != 1 {
		t.Fatalf("expected 1 manifest event even with no tools, got %d", len(lines))
	}
	var ev HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Type != HeadlessEventTypeManifest {
		t.Fatalf("type: want manifest, got %q", ev.Type)
	}
	if len(ev.ToolCalls) != 0 {
		t.Fatalf("ToolCalls must be empty for no-tool turn, got %+v", ev.ToolCalls)
	}
	if ev.TextLen == nil || *ev.TextLen != 850 {
		v := 0
		if ev.TextLen != nil {
			v = *ev.TextLen
		}
		t.Fatalf("text_len: want 850, got %d", v)
	}
	// Verify omitempty strips tool_calls from JSON
	raw := strings.TrimRight(lines[0], "\n")
	if strings.Contains(raw, `"tool_calls"`) {
		t.Fatalf("tool_calls must be omitted from JSON when empty: %s", raw)
	}
}

// TestEmitHeadlessManifestNilStreamIsSafe verifies the nil-stream guard
// so runners don't need a conditional around every emit call.
func TestEmitHeadlessManifestNilStreamIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitHeadlessManifest on nil stream panicked: %v", r)
		}
	}()
	emitHeadlessManifest(nil, "t", HeadlessProviderClaude, "ceo", "task-1", "",
		[]string{"Read"}, 100, headlessProgressMetrics{}, nil)
}

// TestEmitHeadlessManifestFiltersEmptyToolNames verifies that blank or
// whitespace-only tool names in the accumulator slice are silently
// dropped before counting. Opencode normalises missing names to "tool"
// before appending, but other callers could pass raw provider names
// that arrive empty; the filter ensures they don't inflate the count.
func TestEmitHeadlessManifestFiltersEmptyToolNames(t *testing.T) {
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	toolNames := []string{"Read", "", "  ", "Edit", ""}
	emitHeadlessManifest(stream, "turn-f1", HeadlessProviderClaude, "ceo", "task-f", "",
		toolNames, 0, headlessProgressMetrics{}, nil)
	lines := stream.recentTask("task-f")
	if len(lines) != 1 {
		t.Fatalf("expected 1 manifest event, got %d", len(lines))
	}
	var ev HeadlessEvent
	if err := json.Unmarshal([]byte(strings.TrimRight(lines[0], "\n")), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ev.ToolCalls) != 2 {
		t.Fatalf("want 2 tool entries (Read, Edit), got %d: %+v", len(ev.ToolCalls), ev.ToolCalls)
	}
	if ev.ToolCalls[0].ToolName != "Edit" || ev.ToolCalls[1].ToolName != "Read" {
		t.Fatalf("expected alphabetical [Edit, Read], got %+v", ev.ToolCalls)
	}
}

// TestHeadlessProgressEventMetricsDropsSentinels pins the "-1 means
// not measured" sentinel handling. Sentinels must not leak onto the
// wire as negative numbers — JSON omitempty zeros are how the frontend
// distinguishes "absent" from a real measured zero.
func TestHeadlessProgressEventMetricsDropsSentinels(t *testing.T) {
	out := headlessProgressEventMetrics(headlessProgressMetrics{
		TotalMs:      -1,
		FirstEventMs: -1,
		FirstTextMs:  -1,
		FirstToolMs:  -1,
	}, nil)
	if out != nil {
		t.Fatalf("all-sentinel metrics must produce nil envelope, got %+v", out)
	}
	out = headlessProgressEventMetrics(headlessProgressMetrics{
		TotalMs: 1500, FirstEventMs: -1, FirstTextMs: 90, FirstToolMs: -1,
	}, &headlessTokenUsage{InputTokens: 5})
	if out == nil {
		t.Fatal("partial metrics must produce envelope")
	}
	if out.TotalMs != 1500 || out.FirstTextMs != 90 || out.InputTokens != 5 {
		t.Fatalf("populated fields wrong: %+v", out)
	}
	if out.FirstEventMs != 0 || out.FirstToolMs != 0 {
		t.Fatalf("sentinel-marked fields must zero out, got %+v", out)
	}
}
