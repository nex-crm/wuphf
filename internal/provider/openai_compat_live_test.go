package provider

import (
	"os"
	"strings"
	"testing"

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
