package team

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTurnSinks records every interaction so tests can assert the
// state machine pushed the right things in the right order.
type fakeTurnSinks struct {
	pushed []string
	labels []string
	logs   []string
}

func (f *fakeTurnSinks) pushAgentStream(s string) { f.pushed = append(f.pushed, s) }
func (f *fakeTurnSinks) updateProgressLabel(s string) {
	f.labels = append(f.labels, s)
}
func (f *fakeTurnSinks) appendLog(s string) { f.logs = append(f.logs, s) }

// TestTurnState_NonJSONStreamFlushesLiveImmediately covers the
// happy path where the model emits prose: once the state machine has
// seen ~8 chars of non-`{` content, it commits to "this is text" and
// every chunk thereafter (including the buffered prefix) flushes
// straight to the live-output panel.
func TestTurnState_NonJSONStreamFlushesLiveImmediately(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	for _, c := range []string{"Hello,", " I'm ", "the planner."} {
		st.onText(c)
	}
	if !st.decided {
		t.Fatal("state never made the looksJSON decision after >8 chars")
	}
	if st.looksJSON {
		t.Fatal("state classified prose stream as JSON-shaped")
	}
	full := strings.Join(sinks.pushed, "")
	if !strings.Contains(full, "Hello, I'm the planner.") {
		t.Errorf("live-output panel didn't receive the streamed text: %q", full)
	}
}

// TestTurnState_JSONShapedStreamIsSuppressed covers the regression
// the user reported: model streams a markdown-fenced JSON tool call
// chunk-by-chunk, the state machine MUST hold the buffer back so the
// user doesn't watch raw JSON type itself out before the parser
// converts it to a tool_use.
func TestTurnState_JSONShapedStreamIsSuppressed(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	for _, c := range []string{`{"name":`, `"team_broadcast","arguments":`, `{"channel":"x"}}`} {
		st.onText(c)
	}
	if !st.decided {
		t.Fatal("state never decided")
	}
	if !st.looksJSON {
		t.Fatal("state failed to classify {...} stream as JSON-shaped")
	}
	if len(sinks.pushed) != 0 {
		t.Errorf("buffer was flushed despite looksJSON=true: pushed=%+v", sinks.pushed)
	}
}

// TestTurnState_ToolUseChunkResetsState mirrors the iteration
// boundary in production: a tool_use chunk arrives, the buffered
// JSON is the tool's invocation (NOT the user-visible reply), so the
// buffer must be discarded and per-iteration state reset for the
// next streaming turn.
func TestTurnState_ToolUseChunkResetsState(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	st.onText(`{"name":"team_broadcast","arguments":{}}`)
	if !st.looksJSON || st.liveBuf.Len() == 0 {
		t.Fatal("state should be holding JSON in liveBuf")
	}
	st.onToolUseChunk("team_broadcast", `{"channel":"x"}`)
	if st.liveBuf.Len() != 0 {
		t.Error("liveBuf not cleared after tool_use")
	}
	if st.looksJSON || st.decided {
		t.Error("classification flags not reset for next iteration")
	}
	if st.streamedChars != 0 || !st.tpsAnchorAt.IsZero() {
		t.Error("tps state not reset for next iteration — fresh tok/s readout would be wrong")
	}
	if len(sinks.pushed) != 1 ||
		!strings.HasPrefix(sinks.pushed[0], "[tool_use team_broadcast]") {
		t.Errorf("expected one [tool_use ...] marker, got %+v", sinks.pushed)
	}
}

// TestTurnState_BroadcastedGateFiresOnUserVisibleTool is the
// load-bearing fix from cc14d6a6: a successful user-visible post
// tool flips broadcastedThisTurn so the runner's post-loop final
// text post is suppressed (preventing double-replies AND breaking
// the broker fan-out cascade).
func TestTurnState_BroadcastedGateFiresOnUserVisibleTool(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	if !st.shouldPostFinalText() {
		t.Fatal("initial state should allow final-text post")
	}
	st.onToolResult("team_broadcast", "Posted to #general", nil)
	if st.shouldPostFinalText() {
		t.Error("broadcastedThisTurn did not flip after team_broadcast — runner will double-post")
	}
}

// TestTurnState_BroadcastedGateIgnoresReadOnlyTool: a wiki-read or
// run_lint MUST NOT flip the gate — otherwise a turn that only ran
// a read-only tool would silently drop the model's reply.
func TestTurnState_BroadcastedGateIgnoresReadOnlyTool(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	st.onToolResult("team_wiki_read", "wiki content", nil)
	if !st.shouldPostFinalText() {
		t.Error("read-only tool wrongly flipped the broadcasted gate")
	}
}

// TestTurnState_BroadcastedGateIgnoresErroredTool: if the tool
// errored out, finalText must still be posted so the user sees an
// explanation rather than silence.
func TestTurnState_BroadcastedGateIgnoresErroredTool(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	st.onToolResult("team_broadcast", "", errors.New("broker offline"))
	if !st.shouldPostFinalText() {
		t.Error("tool error wrongly flipped the broadcasted gate — user sees no reply")
	}
}

// TestTurnState_TpsLabelThrottledTo750ms: the rolling tps readout
// must update at most every 750ms. A test that sends chunks faster
// than that and asserts only one label fires within the window
// catches a regression where someone "simplifies" the throttle and
// floods the broker SSE with label updates.
func TestTurnState_TpsLabelThrottledTo750ms(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	// Anchor at t0.
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	st.streamedChars = 0
	st.maybeUpdateTpsLabel(t0)
	st.streamedChars = 32
	// 200ms later — too soon, should not fire.
	st.maybeUpdateTpsLabel(t0.Add(200 * time.Millisecond))
	if len(sinks.labels) != 0 {
		t.Fatalf("label fired before 750ms anchor window: %+v", sinks.labels)
	}
	// 800ms later — fires.
	st.streamedChars = 80
	st.maybeUpdateTpsLabel(t0.Add(800 * time.Millisecond))
	if len(sinks.labels) != 1 {
		t.Fatalf("label did not fire at 800ms: %+v", sinks.labels)
	}
	if !strings.Contains(sinks.labels[0], "tok/s") {
		t.Errorf("label format = %q, want tok/s readout", sinks.labels[0])
	}
}

// TestTurnState_OnErrorPushesIntoLive surfaces the model-side error
// chunk into the live-output panel so users see the failure inline
// rather than a silent stall.
func TestTurnState_OnErrorPushesIntoLive(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	st.onError("rate limited")
	if len(sinks.pushed) != 1 || !strings.Contains(sinks.pushed[0], "[error]") ||
		!strings.Contains(sinks.pushed[0], "rate limited") {
		t.Errorf("error push = %+v, want [error]-marked line", sinks.pushed)
	}
}

// TestTurnState_EmptyChunksAreNoOps: the runner sometimes feeds the
// state machine empty strings (e.g. SSE keep-alive frames the loop
// already filtered to "no-content"). They must NOT be counted in
// streamedChars or trigger label updates.
func TestTurnState_EmptyChunksAreNoOps(t *testing.T) {
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks)
	st.onText("")
	if st.streamedChars != 0 {
		t.Error("empty chunk counted in streamedChars")
	}
	if !st.tpsAnchorAt.IsZero() {
		t.Error("empty chunk anchored the tps window")
	}
	if len(sinks.pushed) != 0 || len(sinks.labels) != 0 {
		t.Error("empty chunk produced sink output")
	}
}
