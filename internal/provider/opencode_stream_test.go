package provider

import (
	"strings"
	"testing"
)

func TestReadOpencodeJSONStreamCollectsTextChunks(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"step_start"}`,
		`{"type":"text","part":{"text":"Hello "}}`,
		`{"type":"text","part":{"text":"world."}}`,
		`{"type":"step_finish"}`,
	}, "\n")

	var events []OpencodeStreamEvent
	res, err := ReadOpencodeJSONStream(strings.NewReader(stream), func(ev OpencodeStreamEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("ReadOpencodeJSONStream: %v", err)
	}
	if got, want := res.FinalMessage, "Hello world."; got != want {
		t.Fatalf("FinalMessage = %q, want %q", got, want)
	}
	if res.LastError != "" {
		t.Fatalf("unexpected LastError: %q", res.LastError)
	}

	textCount := 0
	for _, ev := range events {
		if ev.Type == "text" {
			textCount++
		}
	}
	if textCount != 2 {
		t.Fatalf("expected 2 text events, got %d (%+v)", textCount, events)
	}
}

func TestReadOpencodeJSONStreamSurfacesToolUseAndResult(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"tool_use","part":{"toolName":"team_wiki_write","callID":"abc123"}}`,
		`{"type":"tool_result","part":{"callID":"abc123","output":"wrote 1 file"}}`,
	}, "\n")

	var toolUse, toolResult *OpencodeStreamEvent
	if _, err := ReadOpencodeJSONStream(strings.NewReader(stream), func(ev OpencodeStreamEvent) {
		captured := ev
		switch ev.Type {
		case "tool_use":
			toolUse = &captured
		case "tool_result":
			toolResult = &captured
		}
	}); err != nil {
		t.Fatalf("ReadOpencodeJSONStream: %v", err)
	}
	if toolUse == nil || toolUse.ToolName != "team_wiki_write" || toolUse.ToolID != "abc123" {
		t.Fatalf("missing/wrong tool_use event: %+v", toolUse)
	}
	if toolResult == nil || toolResult.ToolID != "abc123" {
		t.Fatalf("missing/wrong tool_result event: %+v", toolResult)
	}
	if !strings.Contains(toolResult.Detail, "wrote 1 file") {
		t.Fatalf("tool_result detail %q does not include output", toolResult.Detail)
	}
}

func TestReadOpencodeJSONStreamCapturesError(t *testing.T) {
	stream := `{"type":"error","message":"rate limited (429)"}`
	res, err := ReadOpencodeJSONStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("ReadOpencodeJSONStream: %v", err)
	}
	if !strings.Contains(res.LastError, "rate limited") {
		t.Fatalf("LastError = %q, want substring 'rate limited'", res.LastError)
	}
}

func TestReadOpencodeJSONStreamFallsBackToPlainTextOnNonJSON(t *testing.T) {
	stream := strings.Join([]string{
		"opencode v1.14.25 — running",
		`{"type":"text","part":{"text":"ack"}}`,
	}, "\n")

	res, err := ReadOpencodeJSONStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("ReadOpencodeJSONStream: %v", err)
	}
	if !strings.Contains(res.FinalMessage, "opencode v1.14.25") {
		t.Fatalf("plain-text banner missing from FinalMessage: %q", res.FinalMessage)
	}
	if !strings.Contains(res.FinalMessage, "ack") {
		t.Fatalf("text event missing from FinalMessage: %q", res.FinalMessage)
	}
}

func TestReadOpencodeJSONStreamForwardsUnknownEventTypes(t *testing.T) {
	stream := `{"type":"future-event-from-opencode-2","part":{"text":"ignored"}}`
	var typesSeen []string
	if _, err := ReadOpencodeJSONStream(strings.NewReader(stream), func(ev OpencodeStreamEvent) {
		typesSeen = append(typesSeen, ev.Type)
	}); err != nil {
		t.Fatalf("ReadOpencodeJSONStream: %v", err)
	}
	if len(typesSeen) != 1 || typesSeen[0] != "future-event-from-opencode-2" {
		t.Fatalf("unknown event not forwarded as-is, got types=%v", typesSeen)
	}
}

func TestReadOpencodeJSONStreamSkipsMalformedJSON(t *testing.T) {
	// First line is malformed JSON (missing closing brace) — should be
	// surfaced as plain text rather than aborting the stream.
	stream := strings.Join([]string{
		`{"type":"text","part":{"text":"first"`, // truncated
		`{"type":"text","part":{"text":"second"}}`,
	}, "\n")

	res, err := ReadOpencodeJSONStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("ReadOpencodeJSONStream: %v", err)
	}
	if !strings.Contains(res.FinalMessage, "second") {
		t.Fatalf("second valid text event missing: %q", res.FinalMessage)
	}
}
