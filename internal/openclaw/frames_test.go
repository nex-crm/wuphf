package openclaw

import (
	"encoding/json"
	"testing"
)

func TestRequestFrameEncode(t *testing.T) {
	f := RequestFrame{Type: "req", ID: "id-1", Method: "sessions.list", Params: map[string]any{"limit": 5}}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Roundtrip
	var got RequestFrame
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Method != "sessions.list" || got.ID != "id-1" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestResponseFrameOkAndError(t *testing.T) {
	raw := `{"type":"res","id":"id-1","ok":true,"payload":{"sessions":[]}}`
	var r ResponseFrame
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("Unmarshal ok: %v", err)
	}
	if !r.OK || r.Error != nil {
		t.Fatalf("ok frame: %+v", r)
	}
	rawErr := `{"type":"res","id":"id-2","ok":false,"error":{"code":"UNAVAILABLE","message":"gateway busy","retryable":true}}`
	var e ResponseFrame
	if err := json.Unmarshal([]byte(rawErr), &e); err != nil {
		t.Fatalf("Unmarshal err: %v", err)
	}
	if e.OK || e.Error == nil || e.Error.Code != "UNAVAILABLE" {
		t.Fatalf("err frame: %+v", e)
	}
}

func TestEventFrameWithSeq(t *testing.T) {
	raw := `{"type":"event","event":"session.message","payload":{"sessionKey":"k"},"seq":42}`
	var f EventFrame
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if f.Event != "session.message" || f.Seq == nil || *f.Seq != 42 {
		t.Fatalf("event frame: %+v", f)
	}
}

func TestDecodeFrameDispatchesByType(t *testing.T) {
	cases := []struct {
		raw  string
		kind string
	}{
		{`{"type":"req","id":"1","method":"x"}`, "req"},
		{`{"type":"res","id":"1","ok":true}`, "res"},
		{`{"type":"event","event":"x"}`, "event"},
	}
	for _, c := range cases {
		kind, _, err := DecodeFrame([]byte(c.raw))
		if err != nil {
			t.Fatalf("DecodeFrame(%q): %v", c.raw, err)
		}
		if kind != c.kind {
			t.Fatalf("DecodeFrame(%q): got kind=%q want %q", c.raw, kind, c.kind)
		}
	}
}
