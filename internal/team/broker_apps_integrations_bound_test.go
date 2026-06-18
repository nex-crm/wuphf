package team

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBoundIntegrationResult pins the fix for the "Unexpected end of JSON input"
// crash: a large/unencodable upstream integration result (e.g. a 25-message
// Gmail read with full bodies) must be re-encoded to valid, size-bounded JSON
// instead of streamed raw — which previously failed mid-write and left the App
// with an empty body.
func TestBoundIntegrationResult(t *testing.T) {
	big := strings.Repeat("x", appIntegrationMaxFieldChars+5000)
	raw := json.RawMessage(`{"messages":[{"subject":"hi","body":"` + big + `"}]}`)

	out, ok := boundIntegrationResult(raw)
	if !ok {
		t.Fatal("valid input should bound ok")
	}
	if len(out) >= len(raw) {
		t.Fatalf("oversized field should have been truncated: got %d, raw %d", len(out), len(raw))
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("bounded output must be valid JSON: %v", err)
	}
	msg := v["messages"].([]any)[0].(map[string]any)
	body := msg["body"].(string)
	if len(body) > appIntegrationMaxFieldChars+len("…[truncated]")+4 {
		t.Fatalf("body not truncated: %d chars", len(body))
	}
	if !strings.Contains(body, "truncated") {
		t.Fatal("expected a truncation marker on the oversized field")
	}
	if msg["subject"].(string) != "hi" {
		t.Fatal("short field must be left intact")
	}

	if _, ok := boundIntegrationResult(json.RawMessage(`{not valid json`)); ok {
		t.Fatal("unparseable upstream JSON must return ok=false (graceful error, not empty body)")
	}

	if r, ok := boundIntegrationResult(nil); !ok || len(r) != 0 {
		t.Fatal("empty input should pass through unchanged")
	}
}
