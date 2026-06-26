package team

import (
	"encoding/json"
	"sort"
	"testing"
)

// These golden tests pin the JSON wire shapes the broker EMITS for the
// integration resolve response and the structured approval payload. The teammcp
// gate hand-mirrors these shapes (internal/teammcp/action_resolve_gate.go:
// actionResolveResponse / actionResolveEnvelope / actionResolveAccount /
// actionCardPayload) and the web mirrors them again (web/src/api/client.ts).
// There is no shared source of truth across the three copies, so a renamed json
// tag would silently drop a field on the consumer side. This test fails the
// moment the broker's emitted key set changes, forcing the mirrors to be
// updated in lockstep (the consumer side is pinned in
// internal/teammcp/action_resolve_gate_test.go TestActionResolveWireDecode).

func topLevelKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertKeys(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s keys = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s keys = %v, want %v", label, got, want)
		}
	}
}

func TestIntegrationResolveWireShape(t *testing.T) {
	resp := integrationResolveResponse{
		Decision: "approve", State: "connected", Provider: "composio",
		Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", Name: "Gmail",
		LogoURL: "logo", ReadOnly: false,
		Account:     &integrationResolveAccount{Name: "Founder Gmail", Key: "ca_1"},
		RawEnvelope: &integrationResolveEnvelope{Method: "POST", URL: "u", Headers: map[string]any{"a": 1}, Data: map[string]any{"b": 2}},
		Detail:      "d", RequestID: "request-1",
	}
	assertKeys(t, "resolve response", topLevelKeys(t, resp), []string{
		"account", "action_id", "decision", "detail", "logo_url", "name",
		"platform", "provider", "raw_envelope", "read_only", "request_id", "state",
	})
	assertKeys(t, "resolve account", topLevelKeys(t, resp.Account), []string{"key", "name"})
	assertKeys(t, "resolve envelope", topLevelKeys(t, resp.RawEnvelope), []string{"data", "headers", "method", "url"})
}

func TestApprovalActionPayloadWireShape(t *testing.T) {
	p := approvalActionPayload{
		Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", Verb: "Send Email",
		Name: "Gmail", LogoURL: "logo",
		Account:     &approvalActionAccount{Name: "Founder Gmail", Key: "ca_1"},
		RawEnvelope: &approvalActionEnvelope{Method: "POST", URL: "u", Headers: map[string]any{"a": 1}, Data: map[string]any{"b": 2}},
	}
	assertKeys(t, "approval payload", topLevelKeys(t, p), []string{
		"account", "action_id", "logo_url", "name", "platform", "raw_envelope", "verb",
	})
	assertKeys(t, "approval account", topLevelKeys(t, p.Account), []string{"key", "name"})
	assertKeys(t, "approval envelope", topLevelKeys(t, p.RawEnvelope), []string{"data", "headers", "method", "url"})
}
