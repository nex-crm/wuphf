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

// TestReadClaudeJSONStream_OversizedTextBlock is the wedge regression. A
// single assistant message with a >2 MiB text block must parse cleanly,
// and a normal result event after it must still be observed. Under the
// prior bufio.Scanner with a 1 MiB token limit this would have aborted
// at the oversized line and silently lost the result event behind it.
func TestReadClaudeJSONStream_OversizedTextBlock(t *testing.T) {
	const huge = 2*1024*1024 + 17
	body := strings.Repeat("x", huge)

	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + body + `"}]}}`,
		`{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":42,"output_tokens":7,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`,
	}
	input := strings.Join(lines, "\n")

	var sawText bool
	result, err := ReadClaudeJSONStream(strings.NewReader(input), func(evt ClaudeStreamEvent) {
		if evt.Type == "text" && len(evt.Text) == huge {
			sawText = true
		}
	})
	if err != nil {
		t.Fatalf("oversized-line ReadClaudeJSONStream: %v", err)
	}
	if !sawText {
		t.Fatal("oversized text block was not delivered to onEvent")
	}
	if result.Usage.InputTokens != 42 || result.Usage.OutputTokens != 7 {
		t.Fatalf("post-oversized result not parsed: %+v", result.Usage)
	}
	if result.FinalMessage != "ok" {
		t.Fatalf("FinalMessage: got %q, want %q", result.FinalMessage, "ok")
	}
}
