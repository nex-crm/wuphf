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
