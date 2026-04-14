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
	_ = json.RawMessage(raw)
}

func TestParseSessionsChangedEvent(t *testing.T) {
	raw := []byte(`{"sessionKey":"k","reason":"ended","phase":"message"}`)
	evt, err := parseSessionsChanged(raw)
	if err != nil {
		t.Fatalf("parseSessionsChanged: %v", err)
	}
	if evt.SessionKey != "k" || evt.Reason != "ended" {
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
