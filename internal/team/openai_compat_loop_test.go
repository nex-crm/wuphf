package team

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// scriptedStreamFn is a programmable agent.StreamFn for tests. Each call to
// the returned function pops the next script entry; the loop expects them
// in order.
type scriptedStreamFn struct {
	turns []scriptedTurn
	calls int
}

type scriptedTurn struct {
	chunks []agent.StreamChunk
	// expectMessages, when non-nil, runs the supplied predicate against the
	// msgs the loop passed in. Lets each turn assert the running history.
	expectMessages func(*testing.T, []agent.Message)
	// recordedTools holds the tools slice the loop passed in for this turn.
	// Populated for the test to inspect after run().
	recordedTools []agent.AgentTool
}

func (s *scriptedStreamFn) fn(t *testing.T) agent.StreamFn {
	return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
		idx := s.calls
		s.calls++
		if idx >= len(s.turns) {
			t.Fatalf("scripted stream over-called: turn=%d, only %d scripted", idx+1, len(s.turns))
		}
		turn := &s.turns[idx]
		if turn.expectMessages != nil {
			turn.expectMessages(t, msgs)
		}
		turn.recordedTools = append([]agent.AgentTool(nil), tools...)
		ch := make(chan agent.StreamChunk, len(turn.chunks)+1)
		go func() {
			defer close(ch)
			for _, c := range turn.chunks {
				ch <- c
			}
		}()
		return ch
	}
}

// TestOpenAICompatToolLoop_TextOnlyOneShot is the simplest possible
// happy path: model emits a single text chunk and finishes. Verifies the
// loop returns finalText after exactly one iteration with no tool churn.
func TestOpenAICompatToolLoop_TextOnlyOneShot(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "text", Content: "hello there."},
			}},
		},
	}

	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		maxIters:    8,
		toolTimeout: time.Second,
	}
	final, iters, _, streamErr, err := loop.run(context.Background(), []agent.Message{
		{Role: "user", Content: "say hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamErr != "" {
		t.Fatalf("unexpected stream error: %s", streamErr)
	}
	if iters != 1 {
		t.Errorf("iterations = %d, want 1", iters)
	}
	if final != "hello there." {
		t.Errorf("finalText = %q, want %q", final, "hello there.")
	}
}

// TestOpenAICompatToolLoop_AccumulatesUsageAcrossIterations confirms the
// loop's per-field accumulation policy:
//   - OutputTokens SUMs across iterations (each iteration generates new
//     tokens, so the turn cost is the sum).
//   - InputTokens MAXes across iterations (each iteration's prompt is
//     cumulative — it already contains every prior iteration's history —
//     so summing would charge the system prompt N times for an N-
//     iteration turn).
//
// In this scripted scenario iteration 1 sends 100 prompt tokens and
// generates 20; iteration 2 sees the iter-1 history echoed back, so its
// prompt grows to 150 (the larger value), while it generates a new 5.
// Correct totals: {input:150, output:25} — NOT {input:250, output:25}.
func TestOpenAICompatToolLoop_AccumulatesUsageAcrossIterations(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "tool_use", ToolName: "echo", ToolParams: map[string]any{"x": "1"}, ToolInput: `{"x":"1"}`},
				{Type: "usage", InputTokens: 100, OutputTokens: 20},
			}},
			{chunks: []agent.StreamChunk{
				{Type: "text", Content: "done"},
				{Type: "usage", InputTokens: 150, OutputTokens: 5},
			}},
		},
	}
	tools := []agent.AgentTool{{
		Name:    "echo",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) { return "ok", nil },
	}}
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       tools,
		toolByName:  map[string]agent.AgentTool{"echo": tools[0]},
		maxIters:    4,
		toolTimeout: time.Second,
	}
	final, iters, usage, streamErr, err := loop.run(context.Background(), []agent.Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamErr != "" {
		t.Fatalf("unexpected stream error: %s", streamErr)
	}
	if iters != 2 {
		t.Errorf("iterations = %d, want 2", iters)
	}
	if final != "done" {
		t.Errorf("finalText = %q, want %q", final, "done")
	}
	if usage.InputTokens != 150 || usage.OutputTokens != 25 {
		t.Errorf("usage = {input:%d output:%d}, want {150, 25}", usage.InputTokens, usage.OutputTokens)
	}
}

// TestOpenAICompatToolLoop_NoUsageStaysZero verifies that turns with no
// usage chunks return a zero-valued ClaudeUsage so the headless runner can
// gate its broker.RecordAgentUsage() call on input/output > 0 and avoid
// recording empty rows when the server doesn't emit usage frames.
func TestOpenAICompatToolLoop_NoUsageStaysZero(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{{Type: "text", Content: "hi"}}},
		},
	}
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		maxIters:    4,
		toolTimeout: time.Second,
	}
	_, _, usage, _, err := loop.run(context.Background(), []agent.Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("usage = {input:%d output:%d}, want {0, 0}", usage.InputTokens, usage.OutputTokens)
	}
}

// TestOpenAICompatToolLoop_FullToolRoundTrip is the core e2e test for the
// MCP bridge: model fires a tool, the loop dispatches to AgentTool.Execute,
// the synthetic user-message containing the result is appended, and the
// next streamfn turn sees the tool output and finalizes with text.
//
// This is the kind of test that would have caught regressions in any of:
//   - tool dispatch routing by name
//   - parameter pass-through to Execute
//   - tool-result formatting in the next-turn prompt
//   - the "stop when no tools fired" termination condition
func TestOpenAICompatToolLoop_FullToolRoundTrip(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "Looking it up..."},
					{Type: "tool_use", ToolName: "lookup_weather", ToolParams: map[string]any{"city": "Lisbon"}, ToolInput: `{"city":"Lisbon"}`, ToolUseID: "c1"},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					if len(msgs) != 1 || msgs[0].Role != "user" {
						t.Errorf("turn 1 expected single user msg, got %+v", msgs)
					}
				},
			},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "It's 22°C and sunny in Lisbon."},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					if len(msgs) != 3 {
						t.Fatalf("turn 2 expected 3 msgs (user, assistant, user-with-tool-result), got %d: %+v", len(msgs), msgs)
					}
					if msgs[1].Role != "assistant" {
						t.Errorf("turn 2 msg[1].Role = %q, want assistant", msgs[1].Role)
					}
					if !strings.Contains(msgs[1].Content, "[tool_call lookup_weather") {
						t.Errorf("turn 2 msg[1] missing tool_call marker: %q", msgs[1].Content)
					}
					if msgs[2].Role != "user" {
						t.Errorf("turn 2 msg[2].Role = %q, want user (tool result)", msgs[2].Role)
					}
					if !strings.Contains(msgs[2].Content, "22°C") {
						t.Errorf("turn 2 msg[2] missing tool output: %q", msgs[2].Content)
					}
				},
			},
		},
	}

	var capturedParams map[string]any
	weatherTool := agent.AgentTool{
		Name:        "lookup_weather",
		Description: "Look up the current weather for a city.",
		Schema:      map[string]any{"type": "object"},
		Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
			capturedParams = params
			return "22°C, sunny", nil
		},
	}

	var (
		toolUseHook    int
		toolResultHook int
	)
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{weatherTool},
		toolByName:  map[string]agent.AgentTool{"lookup_weather": weatherTool},
		maxIters:    4,
		toolTimeout: 2 * time.Second,
		onToolUse:   func(name, raw string) { toolUseHook++ },
		onToolResult: func(name, result string, err error) {
			if err != nil {
				t.Errorf("tool reported error: %v", err)
			}
			toolResultHook++
		},
	}

	final, iters, _, streamErr, err := loop.run(context.Background(), []agent.Message{
		{Role: "user", Content: "What's the weather in Lisbon?"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamErr != "" {
		t.Fatalf("unexpected stream error: %s", streamErr)
	}
	if iters != 2 {
		t.Errorf("iterations = %d, want 2", iters)
	}
	if final != "It's 22°C and sunny in Lisbon." {
		t.Errorf("finalText = %q", final)
	}
	if got, _ := capturedParams["city"].(string); got != "Lisbon" {
		t.Errorf("Execute params[city] = %v, want Lisbon", capturedParams["city"])
	}
	if toolUseHook != 1 {
		t.Errorf("onToolUse fired %d times, want 1", toolUseHook)
	}
	if toolResultHook != 1 {
		t.Errorf("onToolResult fired %d times, want 1", toolResultHook)
	}
	// Tools must be visible to the model on every iteration, not just the
	// first — otherwise the model can't follow up its own tool call.
	if got := stream.turns[1].recordedTools; len(got) != 1 || got[0].Name != "lookup_weather" {
		t.Errorf("turn 2 tools = %+v, want lookup_weather still visible", got)
	}
}

// TestOpenAICompatToolLoop_UnknownToolDoesNotPanic verifies the loop
// returns a graceful "Tool X not available" message into the next turn
// rather than panicking when the model hallucinates a tool name. This is
// the realistic case for smaller open models that try to call tools they
// don't have.
func TestOpenAICompatToolLoop_UnknownToolDoesNotPanic(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "tool_use", ToolName: "made_up_tool", ToolParams: map[string]any{}, ToolInput: "{}"},
			}},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "Sorry — I can't actually do that."},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					if got := msgs[len(msgs)-1].Content; !strings.Contains(got, "is not available") {
						t.Errorf("unavailable tool message missing: %q", got)
					}
				},
			},
		},
	}
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		toolByName:  map[string]agent.AgentTool{},
		maxIters:    4,
		toolTimeout: time.Second,
	}
	final, _, _, streamErr, err := loop.run(context.Background(), []agent.Message{{Role: "user", Content: "do it"}})
	if err != nil || streamErr != "" {
		t.Fatalf("unexpected: err=%v streamErr=%q", err, streamErr)
	}
	if !strings.Contains(final, "I can't actually do that") {
		t.Errorf("finalText = %q", final)
	}
}

// TestOpenAICompatToolLoop_ToolErrorPropagatesAsResult exercises the
// failure path: a tool that returns an error must surface its message in
// the next-turn prompt as a "Tool X failed: ..." block, not crash the
// loop. The model can then choose to apologize or try a different tool.
func TestOpenAICompatToolLoop_ToolErrorPropagatesAsResult(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "tool_use", ToolName: "broken", ToolParams: map[string]any{}, ToolInput: "{}"},
			}},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "It looks like that tool broke."},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					if got := msgs[len(msgs)-1].Content; !strings.Contains(got, "broken") || !strings.Contains(got, "kaboom") {
						t.Errorf("error trailer missing: %q", got)
					}
				},
			},
		},
	}
	broken := agent.AgentTool{
		Name: "broken",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) {
			return "", errors.New("kaboom")
		},
	}
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{broken},
		toolByName:  map[string]agent.AgentTool{"broken": broken},
		maxIters:    3,
		toolTimeout: time.Second,
	}
	final, _, _, streamErr, err := loop.run(context.Background(), []agent.Message{{Role: "user", Content: "go"}})
	if err != nil || streamErr != "" {
		t.Fatalf("unexpected: err=%v streamErr=%q", err, streamErr)
	}
	if !strings.Contains(final, "broke") {
		t.Errorf("finalText = %q", final)
	}
}

// TestOpenAICompatToolLoop_StreamErrorBreaksOut verifies a model-emitted
// error chunk halts the loop and is returned as streamErr, not as an
// invocation that keeps going.
func TestOpenAICompatToolLoop_StreamErrorBreaksOut(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "error", Content: "rate limited"},
			}},
		},
	}
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		maxIters:    4,
		toolTimeout: time.Second,
	}
	final, _, _, streamErr, err := loop.run(context.Background(), []agent.Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if streamErr != "rate limited" {
		t.Errorf("streamErr = %q, want rate limited", streamErr)
	}
	if final != "" {
		t.Errorf("finalText = %q on error path, want empty", final)
	}
}

// TestOpenAICompatToolLoop_MaxIterationsCap protects against runaway
// tool-only loops. The loop must cap and surface a clear error string
// instead of looping forever.
func TestOpenAICompatToolLoop_MaxIterationsCap(t *testing.T) {
	infiniteToolTurn := scriptedTurn{chunks: []agent.StreamChunk{
		{Type: "tool_use", ToolName: "echo", ToolParams: map[string]any{}, ToolInput: "{}"},
	}}
	stream := &scriptedStreamFn{turns: []scriptedTurn{
		infiniteToolTurn, infiniteToolTurn, infiniteToolTurn, infiniteToolTurn,
	}}

	echo := agent.AgentTool{
		Name: "echo",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) {
			return "ok", nil
		},
	}
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{echo},
		toolByName:  map[string]agent.AgentTool{"echo": echo},
		maxIters:    4,
		toolTimeout: time.Second,
	}
	finalText, iters, _, streamErr, _ := loop.run(context.Background(), []agent.Message{{Role: "user", Content: "loop"}})
	if iters != 4 {
		t.Errorf("iterations = %d, want 4 (the cap)", iters)
	}
	if !strings.Contains(streamErr, "exceeded") {
		t.Errorf("streamErr = %q, expected exceeded-iterations message", streamErr)
	}
	// finalText must carry the cap-hit marker so the headless runner has
	// something to post to the channel — runHeadlessOpenAICompatTurn
	// returns early on streamErr but posts finalText first, and that
	// post is the only path the user ever sees on this failure mode.
	if finalText == "" {
		t.Fatal("finalText empty on cap-hit; user gets a silent failure")
	}
	if !strings.Contains(finalText, "tool loop hit") || !strings.Contains(finalText, "iterations") {
		t.Errorf("finalText missing marker: %q", finalText)
	}
}

// TestOpenAICompatToolLoop_BridgeDiesMidCallSurfacesAsToolError simulates
// the realistic failure mode where the `wuphf mcp-team` subprocess (or
// any MCP transport) goes away between turns: the OOM-killer takes mlx
// out, a panic in the broker subprocess unwinds, etc. The loop must
// surface the failure to the model as a "Tool X failed" trailer (so the
// next turn can apologize) rather than panicking or hanging.
//
// The MCP bridge's NewInMemoryTransports tests verify normal call/reply
// paths; this test fills the gap by ripping the transport out from
// underneath an Execute callback that's already been bound.
func TestOpenAICompatToolLoop_BridgeDiesMidCallSurfacesAsToolError(t *testing.T) {
	// brokenTool simulates a tool whose underlying transport has died:
	// it returns a Go error instead of a result. The loop must treat
	// this as "Tool X failed: ...".
	brokenTool := agent.AgentTool{
		Name: "broker_post_message",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) {
			return "", &transportClosedErr{op: "broker_post_message"}
		},
	}

	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "tool_use", ToolName: "broker_post_message", ToolParams: map[string]any{"channel": "general"}, ToolInput: `{"channel":"general"}`},
			}},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "Sorry, the broker is offline right now."},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					last := msgs[len(msgs)-1]
					if !strings.Contains(last.Content, "broker_post_message") {
						t.Errorf("tool name missing from failure trailer: %q", last.Content)
					}
					if !strings.Contains(last.Content, "transport closed") {
						t.Errorf("transport error not surfaced to model: %q", last.Content)
					}
				},
			},
		},
	}

	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{brokenTool},
		toolByName:  map[string]agent.AgentTool{"broker_post_message": brokenTool},
		maxIters:    4,
		toolTimeout: time.Second,
	}
	final, _, _, streamErr, err := loop.run(context.Background(), []agent.Message{
		{Role: "user", Content: "post hi to #general"},
	})
	if err != nil {
		t.Fatalf("loop.run returned Go error (should have surfaced via prompt instead): %v", err)
	}
	if streamErr != "" {
		t.Fatalf("unexpected stream error: %s", streamErr)
	}
	if !strings.Contains(final, "broker is offline") {
		t.Errorf("model did not get a chance to apologize: %q", final)
	}
}

type transportClosedErr struct{ op string }

func (e *transportClosedErr) Error() string { return e.op + ": transport closed (subprocess exited)" }

// TestOpenAICompatToolLoop_ContextCancelStopsImmediately verifies a
// cancelled context aborts cleanly even mid-stream AND that the loop
// drains the underlying StreamFn channel — without the drain
// goroutine, the streamer would block forever on a full channel
// after the loop returned, leaking a goroutine per cancelled turn.
//
// The reviewer flagged the original test for not actually exercising
// the drain path: removing the `go func() { for range ch {} }()`
// line in openai_compat_loop.go used to leave this test green. The
// new assertion uses an unbuffered channel and a goroutine that
// blocks on send forever — if the drain doesn't run, the streamer
// goroutine never returns and the test's GoroutineLeakTracker fails.
func TestOpenAICompatToolLoop_ContextCancelStopsImmediately(t *testing.T) {
	streamerExited := make(chan struct{})
	streamFn := func(_ []agent.Message, _ []agent.AgentTool) <-chan agent.StreamChunk {
		ch := make(chan agent.StreamChunk) // unbuffered: send blocks until received
		go func() {
			defer close(ch)
			defer close(streamerExited)
			// First send blocks indefinitely unless the loop's drain
			// goroutine reads it. If the drain is removed, this
			// streamer goroutine wedges and the test's deadline below
			// trips.
			select {
			case ch <- agent.StreamChunk{Type: "text", Content: "after-cancel-1"}:
			case <-time.After(5 * time.Second):
				return
			}
			select {
			case ch <- agent.StreamChunk{Type: "text", Content: "after-cancel-2"}:
			case <-time.After(5 * time.Second):
				return
			}
		}()
		return ch
	}

	loop := openAICompatToolLoop{
		streamFn:    streamFn,
		maxIters:    1,
		toolTimeout: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before run
	_, _, _, _, err := loop.run(ctx, []agent.Message{{Role: "user", Content: "x"}})
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}

	// The streamer goroutine MUST exit — meaning the drain goroutine
	// the loop spawns successfully read past the blocking sends. If
	// this trips we've reintroduced a per-cancel goroutine leak.
	select {
	case <-streamerExited:
		// drain worked; streamer ran to close(ch)
	case <-time.After(2 * time.Second):
		t.Fatal("streamer goroutine did not exit within 2s of ctx cancel — drain goroutine missing or broken; production runs would leak a goroutine + an open channel per cancelled turn")
	}
}
