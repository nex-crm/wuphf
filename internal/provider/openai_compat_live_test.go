package provider

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestOpenAICompatStreamFn_LiveMLXServer drives the registered mlx-lm
// provider against a real, running mlx_lm.server. Skipped unless the test
// is opted in with WUPHF_TEST_LIVE_MLX_LM=1 so CI / `go test ./...` stays
// hermetic.
//
// Run manually with:
//
//	mlx_lm.server --model mlx-community/Qwen2.5-Coder-32B-Instruct-4bit \
//	  --host 127.0.0.1 --port 8080 &
//	WUPHF_TEST_LIVE_MLX_LM=1 go test -v \
//	  -run TestOpenAICompatStreamFn_LiveMLXServer \
//	  ./internal/provider/
func TestOpenAICompatStreamFn_LiveMLXServer(t *testing.T) {
	if os.Getenv("WUPHF_TEST_LIVE_MLX_LM") != "1" {
		t.Skip("set WUPHF_TEST_LIVE_MLX_LM=1 to run against a live mlx_lm.server on :8080")
	}

	entry := Lookup(KindMLXLM)
	if entry == nil {
		t.Fatal("mlx-lm not registered — provider package init() did not run?")
	}

	stream := entry.StreamFn("live-smoke-agent")
	ch := stream(
		[]agent.Message{
			{Role: "system", Content: "You are terse. Reply in one short sentence."},
			{Role: "user", Content: "Reply with exactly the word: pong."},
		},
		nil,
	)

	var got strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case "text":
			got.WriteString(chunk.Content)
		case "error":
			t.Fatalf("provider error: %s", chunk.Content)
		}
	}
	out := strings.TrimSpace(got.String())
	if out == "" {
		t.Fatal("no text emitted by live mlx_lm.server — provider/server are not talking")
	}
	if !strings.Contains(strings.ToLower(out), "pong") {
		t.Logf("live response did not contain 'pong': %q (model picked another reply — non-fatal)", out)
	} else {
		t.Logf("live ok: %q", out)
	}
}

// TestOpenAICompatStreamFn_LiveMLXServerTools exercises the tool-calling
// path end-to-end: sends a single tool definition, asks the model to use
// it, asserts a tool_use chunk comes back with parsed parameters. Skipped
// unless WUPHF_TEST_LIVE_MLX_LM=1.
//
// Tool-calling reliability varies by model; this test asserts the wuphf
// integration parses whatever the model emits, not that the model always
// picks the tool. If the model replies in text instead, the test logs the
// reply and skips — the only hard failure is a malformed tool_use chunk
// that our parser fails to unmarshal.
func TestOpenAICompatStreamFn_LiveMLXServerTools(t *testing.T) {
	if os.Getenv("WUPHF_TEST_LIVE_MLX_LM") != "1" {
		t.Skip("set WUPHF_TEST_LIVE_MLX_LM=1 to run against a live mlx_lm.server on :8080")
	}

	entry := Lookup(KindMLXLM)
	if entry == nil {
		t.Fatal("mlx-lm not registered")
	}

	tool := agent.AgentTool{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "The city to look up, e.g. \"San Francisco\".",
				},
			},
			"required": []string{"city"},
		},
	}

	ch := entry.StreamFn("live-tool-agent")(
		[]agent.Message{
			{Role: "system", Content: "Use the available tool to answer weather questions. Do not answer from memory."},
			{Role: "user", Content: "What is the weather in Lisbon right now?"},
		},
		[]agent.AgentTool{tool},
	)

	var sawToolUse bool
	var textBuf strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case "text":
			textBuf.WriteString(chunk.Content)
		case "tool_use":
			sawToolUse = true
			if chunk.ToolName != "get_weather" {
				t.Errorf("tool_use ToolName = %q, want get_weather", chunk.ToolName)
			}
			if got, _ := chunk.ToolParams["city"].(string); got == "" {
				t.Errorf("tool_use ToolParams[city] missing or non-string: %+v", chunk.ToolParams)
			} else {
				t.Logf("live tool_use ok: name=%s city=%q raw=%s", chunk.ToolName, got, chunk.ToolInput)
			}
		case "error":
			t.Fatalf("live tool stream error: %s", chunk.Content)
		}
	}
	if !sawToolUse {
		t.Logf("model declined the tool and replied in text: %q (non-fatal — Qwen2.5-Coder is not deterministic on tool-use, the test verifies parsing not model policy)", strings.TrimSpace(textBuf.String()))
	}
}

// TestOpenAICompatStreamFn_LiveOllama exercises the registered ollama
// provider against a real ollama daemon on :11434. Skipped unless
// WUPHF_TEST_LIVE_OLLAMA=1; the model name comes from
// WUPHF_OLLAMA_MODEL (default: the registered package default, which
// must be `ollama pull`'d ahead of time).
//
// This is the parity check for the Ollama path: same code as mlx-lm,
// different daemon. Running it during local verification confirms the
// shared SSE streamer, /v1 normalizer, and ctx plumbing all work
// against a second OpenAI-compatible implementation.
func TestOpenAICompatStreamFn_LiveOllama(t *testing.T) {
	if os.Getenv("WUPHF_TEST_LIVE_OLLAMA") != "1" {
		t.Skip("set WUPHF_TEST_LIVE_OLLAMA=1 (and ensure WUPHF_OLLAMA_MODEL is pulled) to run against a live ollama daemon on :11434")
	}

	entry := Lookup(KindOllama)
	if entry == nil {
		t.Fatal("ollama not registered")
	}
	ch := entry.StreamFn("live-ollama-agent")(
		[]agent.Message{
			{Role: "system", Content: "Be terse."},
			{Role: "user", Content: "Reply with exactly: pong."},
		},
		nil,
	)
	var got strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case "text":
			got.WriteString(chunk.Content)
		case "error":
			t.Fatalf("provider error: %s", chunk.Content)
		}
	}
	out := strings.TrimSpace(got.String())
	if out == "" {
		t.Fatal("no text from live ollama")
	}
	t.Logf("live ollama ok: %q", out)
}

// TestOpenAICompatStreamFn_LiveOllama_CtxCancelAbortsHTTP exercises the
// WithCtx variant against a real ollama daemon to lock in the
// cancellation contract end-to-end. The unit test
// TestOpenAICompatToolLoop_ContextCancelStopsImmediately uses a fake
// StreamFn so it only verifies the loop's responsiveness; this confirms
// the *real HTTP request* aborts when ctx is cancelled, which is the
// load-bearing property for not pinning the server's inference slot.
func TestOpenAICompatStreamFn_LiveOllama_CtxCancelAbortsHTTP(t *testing.T) {
	if os.Getenv("WUPHF_TEST_LIVE_OLLAMA") != "1" {
		t.Skip("set WUPHF_TEST_LIVE_OLLAMA=1 to run against a live ollama daemon on :11434")
	}
	ctx, cancel := context.WithCancel(context.Background())
	fn := NewOpenAICompatStreamFnWithCtx(ctx, KindOllama)

	ch := fn(
		[]agent.Message{{Role: "user", Content: "Write a 200 word essay about clouds."}},
		nil,
	)
	// Cancel after we know at least one frame is in-flight.
	go func() {
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	closed := false
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-deadline.C:
			t.Fatal("channel still open 8s after ctx cancel — HTTP request was not aborted")
		}
	}
	t.Log("live ollama ctx cancel ok: channel closed within 8s of cancel")
}

// TestOpenAICompatStreamFn_LiveExo_ConnectRefused verifies the failure
// path is friendly: when no exo daemon is running on :52415 (the
// realistic state for users on a single Mac), the StreamFn must surface
// one clear error chunk including the URL and a hint about the local
// server, not crash or hang. This is the test that protects against
// regressions in `runOpenAICompatStream`'s early-error path.
//
// Always-on (no env gate) because it doesn't require any external
// process — connection refused is the expected reality.
func TestOpenAICompatStreamFn_LiveExo_ConnectRefused(t *testing.T) {
	entry := Lookup(KindExo)
	if entry == nil {
		t.Fatal("exo not registered")
	}
	// Force a base URL that nothing's listening on. Use an unlikely high
	// port to avoid colliding with a real daemon if the user has one.
	t.Setenv("WUPHF_EXO_BASE_URL", "http://127.0.0.1:1/v1")

	ch := entry.StreamFn("exo-down-agent")(
		[]agent.Message{{Role: "user", Content: "ping"}},
		nil,
	)
	var (
		errCount int
		lastErr  string
	)
	for chunk := range ch {
		if chunk.Type == "error" {
			errCount++
			lastErr = chunk.Content
		}
	}
	if errCount != 1 {
		t.Fatalf("expected exactly 1 error chunk on connect-refused, got %d", errCount)
	}
	if !strings.Contains(lastErr, "127.0.0.1:1") {
		t.Errorf("error did not mention the URL: %q", lastErr)
	}
	if !strings.Contains(lastErr, "Is the local server running") {
		t.Errorf("error missing the hint we ship for users: %q", lastErr)
	}
}
