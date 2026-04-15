package provider

import (
	"strings"
	"testing"
)

func TestReadCodexJSONStreamParsesUsage(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"response.output_text.delta","delta":"Shipped "}`,
		`{"type":"response.output_text.delta","delta":"it."}`,
		`{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"Shipped it."}]}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":120,"output_tokens":45,"cached_input_tokens":30,"cache_creation_input_tokens":10}}}`,
	}, "\n")

	result, err := ReadCodexJSONStream(strings.NewReader(raw), nil)
	if err != nil {
		t.Fatalf("ReadCodexJSONStream: %v", err)
	}
	if result.FinalMessage != "Shipped it." {
		t.Fatalf("FinalMessage = %q, want %q", result.FinalMessage, "Shipped it.")
	}
	if result.Usage.InputTokens != 120 || result.Usage.OutputTokens != 45 {
		t.Fatalf("unexpected usage counts: %+v", result.Usage)
	}
	if result.Usage.CacheReadTokens != 30 || result.Usage.CacheCreationTokens != 10 {
		t.Fatalf("unexpected cache usage: %+v", result.Usage)
	}
}
