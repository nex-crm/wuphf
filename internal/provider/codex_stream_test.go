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
	// FinalMessage is the runner's authoritative output. A regression that
	// drops state.deltaText during final assembly would still pass the
	// onEvent check above, so assert the assembled length explicitly.
	if len(result.FinalMessage) != huge {
		t.Fatalf("FinalMessage length: got %d, want %d", len(result.FinalMessage), huge)
	}
	if result.Usage.InputTokens != 7 || result.Usage.OutputTokens != 3 {
		t.Fatalf("post-oversized usage not parsed: %+v", result.Usage)
	}
}

// TestReadCodexJSONStreamItemCompletedDrivesProgress is the D3 regression:
// a codex exec turn that reports the assistant message only via
// item.completed (the newer exec shape, no response.output_text.delta)
// must still drive the runner's progress surface — reasoning, tool_use,
// and a text event for the completed assistant message. Before the fix the
// completed message item was collected into FinalMessage but never emitted
// as an onEvent, so the runner saw zero text events (first_text_ms stayed
// -1) and the live-chat relay never got the spoken output.
func TestReadCodexJSONStreamItemCompletedDrivesProgress(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"item.started","item":{"id":"r1","type":"reasoning"}}`,
		`{"type":"item.completed","item":{"id":"r1","type":"reasoning"}}`,
		`{"type":"item.started","item":{"id":"t1","type":"function_call","name":"notebook_visual_artifact_create","arguments":"{\"title\":\"chart\"}"}}`,
		`{"type":"item.completed","item":{"id":"t1","type":"function_call","name":"notebook_visual_artifact_create","arguments":"{\"title\":\"chart\"}"}}`,
		`{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"Shipped the figure."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":4}}`,
	}, "\n")

	var types []string
	var toolNames []string
	var textPayloads []string
	result, err := ReadCodexJSONStream(strings.NewReader(stream), func(evt CodexStreamEvent) {
		types = append(types, evt.Type)
		switch evt.Type {
		case "tool_use":
			toolNames = append(toolNames, evt.ToolName)
		case "text":
			textPayloads = append(textPayloads, evt.Text)
		}
	})
	if err != nil {
		t.Fatalf("ReadCodexJSONStream: %v", err)
	}

	// The runner classifies in order: reasoning (→ "planning next step"),
	// tool_use (→ "running <tool>"), text (→ "drafting response"). Assert
	// each fired at least once so updateHeadlessProgress would step through
	// thinking → tool_use → text.
	if !containsString(types, "reasoning") {
		t.Fatalf("expected a reasoning event, got %v", types)
	}
	if !containsString(types, "tool_use") {
		t.Fatalf("expected a tool_use event, got %v", types)
	}
	if !containsString(types, "text") {
		t.Fatalf("expected a text event for the item.completed message, got %v", types)
	}

	// Ordering: reasoning must precede tool_use must precede text, matching
	// the codex turn shape the runner narrates.
	if got := firstIndex(types, "reasoning"); got < 0 || got > firstIndex(types, "tool_use") {
		t.Fatalf("reasoning should precede tool_use: %v", types)
	}
	if got := firstIndex(types, "tool_use"); got < 0 || got > firstIndex(types, "text") {
		t.Fatalf("tool_use should precede text: %v", types)
	}

	if !containsString(toolNames, "notebook_visual_artifact_create") {
		t.Fatalf("expected the visual-artifact tool name, got %v", toolNames)
	}
	if !containsString(textPayloads, "Shipped the figure.") {
		t.Fatalf("expected the completed message text, got %v", textPayloads)
	}
	if result.FinalMessage != "Shipped the figure." {
		t.Fatalf("FinalMessage = %q, want %q", result.FinalMessage, "Shipped the figure.")
	}
}

// TestReadCodexJSONStreamCompletedMessageNotDoubledWithDeltas guards the
// dedup: when a turn DOES stream response.output_text.delta AND closes
// with a matching completed message item, the completed item must not
// re-emit a duplicate text event into the relay.
func TestReadCodexJSONStreamCompletedMessageNotDoubledWithDeltas(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"response.output_text.delta","delta":"Shipped "}`,
		`{"type":"response.output_text.delta","delta":"it."}`,
		`{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"Shipped it."}]}}`,
	}, "\n")

	var textEvents int
	if _, err := ReadCodexJSONStream(strings.NewReader(stream), func(evt CodexStreamEvent) {
		if evt.Type == "text" {
			textEvents++
		}
	}); err != nil {
		t.Fatalf("ReadCodexJSONStream: %v", err)
	}
	// Two delta events; the completed item must NOT add a third because the
	// streaming deltas already carried the message to the relay.
	if textEvents != 2 {
		t.Fatalf("expected exactly 2 text events (no completed-item double), got %d", textEvents)
	}
}

// TestReadCodexJSONStreamMixedShapeEmitsBothMessages guards the
// message-scoped dedup: a turn that streams response.output_text.delta for
// one message (id m1) AND later reports a SECOND message only as a completed
// item (id m2) must emit text events for BOTH. A turn-wide sawTextDelta flag
// would suppress the completed-only second message because a delta had been
// seen earlier in the turn — that is the missed-relay regression this test
// pins. The IDs differ, so per-message dedup must let m2 through.
func TestReadCodexJSONStreamMixedShapeEmitsBothMessages(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"response.output_text.delta","item_id":"m1","delta":"First "}`,
		`{"type":"response.output_text.delta","item_id":"m1","delta":"message."}`,
		`{"type":"response.output_item.done","item":{"id":"m1","type":"message","content":[{"type":"output_text","text":"First message."}]}}`,
		`{"type":"item.completed","item":{"id":"m2","type":"agent_message","text":"Second message."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":4}}`,
	}, "\n")

	var textPayloads []string
	result, err := ReadCodexJSONStream(strings.NewReader(stream), func(evt CodexStreamEvent) {
		if evt.Type == "text" {
			textPayloads = append(textPayloads, evt.Text)
		}
	})
	if err != nil {
		t.Fatalf("ReadCodexJSONStream: %v", err)
	}
	// m1: two deltas (no completed double, same item_id). m2: one completed
	// emission (no deltas for m2, so it must not be suppressed). Delta text is
	// trimmed by the parser, so "First " arrives as "First".
	wantText := []string{"First", "message.", "Second message."}
	if len(textPayloads) != len(wantText) {
		t.Fatalf("expected %d text events %v, got %d: %v", len(wantText), wantText, len(textPayloads), textPayloads)
	}
	for i, want := range wantText {
		if textPayloads[i] != want {
			t.Fatalf("text event %d = %q, want %q (all: %v)", i, textPayloads[i], want, textPayloads)
		}
	}
	if !containsString(textPayloads, "Second message.") {
		t.Fatalf("completed-only second message was suppressed: %v", textPayloads)
	}
	if result.FinalMessage != "First message.\n\nSecond message." {
		t.Fatalf("FinalMessage = %q, want %q", result.FinalMessage, "First message.\n\nSecond message.")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func firstIndex(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}
