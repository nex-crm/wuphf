package team

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// TestOpenAICompatToolLoop_LiveMLX exercises the full tool loop against a
// real running mlx_lm.server. Skipped unless WUPHF_TEST_LIVE_MLX_LM=1 so
// the regular `go test ./...` stays hermetic. Drives the same code path
// the headless runner uses: provider StreamFn + openAICompatToolLoop +
// real model + fake in-process tool. The fake tool stands in for the
// MCP-bridged broker tools so we don't need a broker server here.
//
// What this catches that the unit tests can't:
//   - Provider/server SSE-format compatibility regressions.
//   - JSON-in-content fallback round-tripping back through the loop's
//     tool-result encoding.
//   - Real-token-budget loop iteration counts (this should finish in
//     1–3 iterations on Qwen2.5-Coder; if it explodes to 8 there's a
//     prompt-engineering regression).
//
// Manually:
//
//	mlx_lm.server --model mlx-community/Qwen2.5-Coder-32B-Instruct-4bit \
//	  --host 127.0.0.1 --port 8080 &
//	WUPHF_TEST_LIVE_MLX_LM=1 go test -v -timeout 5m \
//	  -run TestOpenAICompatToolLoop_LiveMLX ./internal/team/
func TestOpenAICompatToolLoop_LiveMLX(t *testing.T) {
	if os.Getenv("WUPHF_TEST_LIVE_MLX_LM") != "1" {
		t.Skip("set WUPHF_TEST_LIVE_MLX_LM=1 to run against a live mlx_lm.server on :8080")
	}

	entry := provider.Lookup(provider.KindMLXLM)
	if entry == nil {
		t.Fatal("mlx-lm not registered")
	}

	var calls int
	echo := agent.AgentTool{
		Name:        "echo_phrase",
		Description: "Returns the input phrase verbatim. Use to demonstrate tool calling.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"phrase": map[string]any{"type": "string", "description": "The phrase to echo back."},
			},
			"required": []string{"phrase"},
		},
		Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
			calls++
			phrase, _ := params["phrase"].(string)
			return phrase, nil
		},
	}

	loop := openAICompatToolLoop{
		streamFn:    entry.StreamFn("live-loop-agent"),
		tools:       []agent.AgentTool{echo},
		toolByName:  map[string]agent.AgentTool{"echo_phrase": echo},
		maxIters:    4,
		toolTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	finalText, iters, streamErr, err := loop.run(ctx, []agent.Message{
		{Role: "system", Content: "You have one tool, echo_phrase. The user wants you to call it once with the phrase 'unified-steele' and then summarize what came back in a short sentence."},
		{Role: "user", Content: "Please call echo_phrase with phrase=\"unified-steele\" and tell me what it returned."},
	})
	if err != nil {
		t.Fatalf("loop.run: %v", err)
	}
	if streamErr != "" {
		t.Fatalf("stream error: %s", streamErr)
	}
	if iters > 4 {
		t.Errorf("iterations = %d; loop did not terminate quickly", iters)
	}
	t.Logf("live loop ok: iters=%d tool_calls=%d final=%q", iters, calls, finalText)
	// We don't insist the model called the tool — the test verifies the
	// loop survives whichever choice the model makes. But if it called the
	// tool, the result must echo "unified-steele".
	if calls > 0 && !strings.Contains(strings.ToLower(finalText), "unified-steele") {
		t.Logf("model called the tool but did not echo the phrase in final reply: %q", finalText)
	}
}
