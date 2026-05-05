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

// TestReadCodexJSONStreamOversizedLine is the wedge regression. A single
// JSONL event larger than the prior 4 MiB scanner buffer must drain
// cleanly and the trailing usage event must still parse. Under the prior
// bufio.Scanner this would have aborted at the oversized line.
func TestReadCodexJSONStreamOversizedLine(t *testing.T) {
	const huge = 5*1024*1024 + 41
	body := strings.Repeat("x", huge)

	raw := strings.Join([]string{
		`{"type":"response.output_text.delta","delta":"` + body + `"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":3}}}`,
	}, "\n")

	var sawDelta bool
	result, err := ReadCodexJSONStream(strings.NewReader(raw), func(evt CodexStreamEvent) {
		if evt.Type == "text" && len(evt.Text) == huge {
			sawDelta = true
		}
	})
	if err != nil {
		t.Fatalf("oversized-line ReadCodexJSONStream: %v", err)
	}
	if !sawDelta {
		t.Fatal("oversized text delta was not delivered to onEvent")
	}
	if result.Usage.InputTokens != 7 || result.Usage.OutputTokens != 3 {
		t.Fatalf("post-oversized usage not parsed: %+v", result.Usage)
	}
}
