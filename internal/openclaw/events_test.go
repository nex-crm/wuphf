package openclaw

import (
	"encoding/json"
	"testing"
)

func TestParseSessionMessageEvent(t *testing.T) {
	raw := []byte(`{"sessionKey":"agent:main:main","message":{"id":"m1","role":"assistant","content":"hello"},"messageSeq":7}`)
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
	if evt.MessageText != "hello" {
		t.Fatalf("MessageText from nested content: %q", evt.MessageText)
	}

	// state extraction + text fallback
	raw2 := []byte(`{"sessionKey":"k","message":{"state":"delta","text":"partial"}}`)
	evt2, err := parseSessionMessage(raw2)
	if err != nil {
		t.Fatalf("parseSessionMessage state: %v", err)
	}
	if evt2.MessageState != "delta" {
		t.Fatalf("MessageState: %q", evt2.MessageState)
	}
	if evt2.MessageText != "partial" {
		t.Fatalf("MessageText from text fallback: %q", evt2.MessageText)
	}
	_ = json.RawMessage(raw)
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
