package agent

import (
	"context"
	"testing"
)

func TestAgentPhaseConstants(t *testing.T) {
	phases := []AgentPhase{PhaseIdle, PhaseBuildContext, PhaseStreamLLM, PhaseExecuteTool, PhaseDone, PhaseError}
	expected := []string{"idle", "build_context", "stream_llm", "execute_tool", "done", "error"}
	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("phase[%d]: got %q, want %q", i, p, expected[i])
		}
	}
}

func TestAgentToolExecute(t *testing.T) {
	tool := AgentTool{
		Name:        "test_tool",
		Description: "a test tool",
		Schema:      map[string]any{"type": "object"},
		Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
			return "ok", nil
		},
	}
	result, err := tool.Execute(nil, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("got %q, want %q", result, "ok")
	}
}

func TestStreamChunkTypes(t *testing.T) {
	text := StreamChunk{Type: "text", Content: "hello"}
	call := StreamChunk{Type: "tool_call", ToolName: "search", ToolParams: map[string]any{"q": "test"}}

	if text.Type != "text" || text.Content != "hello" {
		t.Errorf("unexpected text chunk: %+v", text)
	}
	if call.Type != "tool_call" || call.ToolName != "search" {
		t.Errorf("unexpected tool_call chunk: %+v", call)
	}
}
