package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty_allowed", "", false},
		{"claude_code", "claude-code", false},
		{"codex", "codex", false},
		{"opencode", "opencode", false},
		{"openclaw", "openclaw", false},
		{"unknown", "gemini", true},
		{"typo", "claud-code", true},
		{"uppercase_rejected", "Codex", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKind(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateKind(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "claude-code") {
				t.Fatalf("ValidateKind(%q) err=%v should list valid values", tt.in, err)
			}
		})
	}
}

func TestBindingJSONRoundTrip_Empty(t *testing.T) {
	t.Parallel()
	var b ProviderBinding
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("empty binding should marshal to {}, got %s", data)
	}
	var got ProviderBinding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != "" || got.Model != "" || got.Openclaw != nil {
		t.Fatalf("empty round-trip lost zero value: %+v", got)
	}
}

func TestBindingJSONRoundTrip_Claude(t *testing.T) {
	t.Parallel()
	in := ProviderBinding{Kind: "claude-code", Model: "claude-sonnet-4.6"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "openclaw") {
		t.Fatalf("claude binding should not emit openclaw field, got %s", data)
	}
	var got ProviderBinding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != in {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, in)
	}
}

func TestBindingJSONRoundTrip_Openclaw(t *testing.T) {
	t.Parallel()
	in := ProviderBinding{
		Kind:     "openclaw",
		Model:    "openai-codex/gpt-5.4",
		Openclaw: &OpenclawProviderBinding{SessionKey: "agent:foo:demo", AgentID: "main"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProviderBinding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != in.Kind || got.Model != in.Model {
		t.Fatalf("round-trip lost core fields: got=%+v want=%+v", got, in)
	}
	if got.Openclaw == nil || *got.Openclaw != *in.Openclaw {
		t.Fatalf("round-trip lost openclaw block: got=%+v want=%+v", got.Openclaw, in.Openclaw)
	}
}

func TestResolveKindFallsBackToGlobal(t *testing.T) {
	t.Parallel()
	// Caller-provided global resolver — tests the shape without depending on config.
	global := func() string { return "codex" }
	if got := ResolveKind(ProviderBinding{Kind: ""}, global); got != "codex" {
		t.Fatalf("empty Kind should fall back to global, got %q", got)
	}
	if got := ResolveKind(ProviderBinding{Kind: "claude-code"}, global); got != "claude-code" {
		t.Fatalf("explicit Kind should win, got %q", got)
	}
}
