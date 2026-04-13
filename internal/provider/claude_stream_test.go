package provider

import (
	"strings"
	"testing"
)

func TestReadClaudeJSONStream_ParsesUsageFromResultEvent(t *testing.T) {
	// This is a real result event from Claude CLI 2.1.x stream-json output.
	// Token counts are nested under "usage", cost is "total_cost_usd".
	lines := []string{
		`{"type":"assistant","message":{"model":"claude-sonnet-4-20250514","id":"msg_abc","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":500,"output_tokens":50}}}`,
		`{"type":"result","subtype":"success","is_error":false,"duration_ms":2000,"num_turns":1,"result":"hello","total_cost_usd":0.003750,"usage":{"input_tokens":100,"cache_creation_input_tokens":2000,"cache_read_input_tokens":500,"output_tokens":50}}`,
	}
	input := strings.Join(lines, "\n")

	result, err := ReadClaudeJSONStream(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Usage.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", result.Usage.OutputTokens)
	}
	if result.Usage.CacheReadTokens != 500 {
		t.Errorf("CacheReadTokens: got %d, want 500", result.Usage.CacheReadTokens)
	}
	if result.Usage.CacheCreationTokens != 2000 {
		t.Errorf("CacheCreationTokens: got %d, want 2000", result.Usage.CacheCreationTokens)
	}
	if result.Usage.CostUSD != 0.003750 {
		t.Errorf("CostUSD: got %f, want 0.003750", result.Usage.CostUSD)
	}
	if result.FinalMessage != "hello" {
		t.Errorf("FinalMessage: got %q, want %q", result.FinalMessage, "hello")
	}
}

func TestReadClaudeJSONStream_ZeroCostStillParsesTokens(t *testing.T) {
	// Even if total_cost_usd is missing, tokens should still parse.
	line := `{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":200,"output_tokens":30,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`

	result, err := ReadClaudeJSONStream(strings.NewReader(line), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Usage.InputTokens != 200 {
		t.Errorf("InputTokens: got %d, want 200", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 30 {
		t.Errorf("OutputTokens: got %d, want 30", result.Usage.OutputTokens)
	}
	if result.Usage.CostUSD != 0 {
		t.Errorf("CostUSD: got %f, want 0", result.Usage.CostUSD)
	}
}
