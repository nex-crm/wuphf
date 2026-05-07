package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// OpencodeStreamEvent is a normalized event emitted while parsing
// `opencode run --format json` output.
//
// Opencode's documented event schema (1.14.x at time of writing) frames each
// SSE-style event as a JSON object with at least a `type` field plus an
// optional `part` payload. The exact shape has shifted across opencode minors
// and is not formally specified, so this reader is intentionally lenient:
// any line that does not decode as JSON is forwarded as a plain-text event,
// which keeps wuphf working against older opencode builds and preserves the
// pre-#313 behavior as a safety net.
type OpencodeStreamEvent struct {
	Type     string
	Text     string
	ToolName string
	ToolID   string
	Detail   string
	Raw      string
}

// OpencodeStreamResult captures the final outcome of a streamed Opencode turn.
type OpencodeStreamResult struct {
	FinalMessage string
	LastError    string
}

type opencodeRawEvent struct {
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
	Part    opencodeRawPart `json:"part"`
}

type opencodeRawPart struct {
	Text     string          `json:"text"`
	Message  string          `json:"message"`
	Tool     string          `json:"tool"`
	ToolName string          `json:"toolName"`
	Name     string          `json:"name"`
	ID       string          `json:"id"`
	CallID   string          `json:"callID"`
	Output   json.RawMessage `json:"output"`
	Result   json.RawMessage `json:"result"`
}

// ReadOpencodeJSONStream consumes `opencode run --format json` output and
// normalizes it into text / tool-use / tool-result / error events, mirroring
// the shape of ReadClaudeJSONStream so callers can swap providers without
// reshaping their event handlers.
//
// Lines that do not decode as JSON (banners, warnings printed before the
// stream opens, fallbacks from older opencode builds) are surfaced as plain
// "text" events so no agent output is silently dropped.
//
// Drained via DrainStreamLines so a single oversized JSONL line never wedges
// the cmd's stdout pipe and never aborts parsing of subsequent lines.
func ReadOpencodeJSONStream(r io.Reader, onEvent func(OpencodeStreamEvent)) (OpencodeStreamResult, error) {
	var result OpencodeStreamResult
	var text strings.Builder

	emit := func(ev OpencodeStreamEvent) {
		if onEvent != nil {
			onEvent(ev)
		}
	}

	err := DrainStreamLines(r, func(raw string) {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return
		}

		// Cheap JSON-object detector before we pay for json.Unmarshal —
		// avoids decoding banners or stray plain-text lines.
		appendPlain := func() {
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(line)
			emit(OpencodeStreamEvent{Type: "text", Text: line, Detail: line, Raw: line})
		}

		if !strings.HasPrefix(trimmed, "{") {
			appendPlain()
			return
		}

		var ev opencodeRawEvent
		if jsonErr := json.Unmarshal([]byte(trimmed), &ev); jsonErr != nil {
			// Forward malformed/unrecognised JSON as plain text rather than
			// dropping it on the floor. Mirrors the shim behavior in #313.
			appendPlain()
			return
		}

		switch ev.Type {
		case "text":
			chunk := ev.Part.Text
			if chunk == "" {
				return
			}
			text.WriteString(chunk)
			emit(OpencodeStreamEvent{Type: "text", Text: chunk, Detail: chunk, Raw: line})
		case "tool_use", "tool_call":
			name := firstNonEmpty(ev.Part.ToolName, ev.Part.Tool, ev.Part.Name)
			id := firstNonEmpty(ev.Part.CallID, ev.Part.ID)
			emit(OpencodeStreamEvent{Type: "tool_use", ToolName: name, ToolID: id, Detail: name, Raw: line})
		case "tool_result":
			detail := strings.TrimSpace(string(ev.Part.Output))
			if detail == "" {
				detail = strings.TrimSpace(string(ev.Part.Result))
			}
			id := firstNonEmpty(ev.Part.CallID, ev.Part.ID)
			emit(OpencodeStreamEvent{Type: "tool_result", ToolID: id, Text: detail, Detail: detail, Raw: line})
		case "step_start", "step_finish":
			emit(OpencodeStreamEvent{Type: ev.Type, Raw: line})
		case "error":
			msg := firstNonEmpty(ev.Message, ev.Error, ev.Part.Message)
			if msg != "" {
				result.LastError = msg
			}
			emit(OpencodeStreamEvent{Type: "error", Text: msg, Detail: msg, Raw: line})
		default:
			// Unknown event types are forwarded raw so callers can log/
			// diagnose without us having to enumerate every minor's schema.
			emit(OpencodeStreamEvent{Type: ev.Type, Raw: line})
		}
	})
	if err != nil {
		return result, fmt.Errorf("read opencode json stream: %w", err)
	}

	result.FinalMessage = strings.TrimSpace(text.String())
	return result, nil
}
