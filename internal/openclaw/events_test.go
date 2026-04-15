package openclaw

import (
	"encoding/json"
	"testing"
)

func TestParseSessionMessageAssistantPartsArray(t *testing.T) {
	// Real OpenClaw shape for an assistant reply: content is an array of
	// {type, text} parts. Verified 2026-04-15 against OpenClaw 2026.4.14.
	raw := []byte(`{"sessionKey":"agent:main:main","message":{"role":"assistant","content":[{"type":"text","text":"hello there"}],"timestamp":1776254522461},"messageSeq":7}`)
	evt, err := parseSessionMessage(raw)
	if err != nil {
		t.Fatalf("parseSessionMessage: %v", err)
	}
	if evt.SessionKey != "agent:main:main" {
		t.Fatalf("sessionKey: %q", evt.SessionKey)
	}
	if evt.MessageSeq == nil || *evt.MessageSeq != 7 {
		t.Fatalf("messageSeq: %v", evt.MessageSeq)
	}
	if evt.Role != "assistant" {
		t.Fatalf("Role: %q", evt.Role)
	}
	if evt.MessageText != "hello there" {
		t.Fatalf("MessageText: %q", evt.MessageText)
	}
}

func TestParseSessionMessageUserStringContent(t *testing.T) {
	// User-role messages in the real feed carry content as a plain string.
	raw := []byte(`{"sessionKey":"k","message":{"role":"user","content":"hi","timestamp":0},"messageSeq":0}`)
	evt, err := parseSessionMessage(raw)
	if err != nil {
		t.Fatalf("parseSessionMessage: %v", err)
	}
	if evt.Role != "user" || evt.MessageText != "hi" {
		t.Fatalf("Role/Text: %q/%q", evt.Role, evt.MessageText)
	}
}

func TestExtractMessageTextMultiplePartsJoined(t *testing.T) {
	parts := json.RawMessage(`[{"type":"text","text":"line 1"},{"type":"text","text":"line 2"}]`)
	got := extractMessageText(parts, "")
	if got != "line 1\nline 2" {
		t.Fatalf("joined text: %q", got)
	}
}

func TestParseSessionsChangedEvent(t *testing.T) {
	raw := []byte(`{"sessionKey":"k","reason":"ended","phase":"message"}`)
	evt, err := parseSessionsChanged(raw)
	if err != nil {
		t.Fatalf("parseSessionsChanged: %v", err)
	}
	if evt.SessionKey != "k" || evt.Reason != "ended" || evt.Phase != "message" {
		t.Fatalf("event: %+v", evt)
	}
}

func TestClientEventDiscriminator(t *testing.T) {
	seq := int64(5)
	e := ClientEvent{
		Kind:           EventKindMessage,
		SessionMessage: &SessionMessageEvent{SessionKey: "k", MessageSeq: &seq},
	}
	if e.Kind != EventKindMessage || e.SessionMessage == nil {
		t.Fatalf("union: %+v", e)
	}
}
