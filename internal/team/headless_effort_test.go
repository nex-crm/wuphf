package team

import (
	"strings"
	"testing"
)

func TestNormalizeClaudeEffort(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"high", "high"},
		{"HIGH", "high"},
		{" Medium ", "medium"},
		{"low", "low"},
		{"xhigh", "xhigh"},
		{"max", "max"},
		// codex-only level — not valid for claude
		{"minimal", ""},
		{"bogus", ""},
	}
	for _, tc := range cases {
		if got := normalizeClaudeEffort(tc.in); got != tc.want {
			t.Errorf("normalizeClaudeEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeCodexEffort(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"minimal", "minimal"},
		{"MINIMAL", "minimal"},
		{" High ", "high"},
		{"medium", "medium"},
		{"low", "low"},
		{"xhigh", "xhigh"},
		// claude-only level — not valid for codex
		{"max", ""},
		{"bogus", ""},
	}
	for _, tc := range cases {
		if got := normalizeCodexEffort(tc.in); got != tc.want {
			t.Errorf("normalizeCodexEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateTaskRuntimeFields(t *testing.T) {
	longModel := strings.Repeat("m", maxTaskModelLen+1)
	cases := []struct {
		name        string
		provider    string
		model       string
		effort      string
		wantErr     bool
		errContains string
	}{
		{name: "all empty falls back to defaults"},
		{name: "valid claude-code + model + effort", provider: "claude-code", model: "claude-opus-4-8", effort: "max"},
		{name: "valid codex + minimal effort", provider: "codex", model: "gpt-5.5", effort: "minimal"},
		{name: "effort case-insensitive", provider: "codex", effort: " High "},
		{name: "claude-only level accepted at boundary (union)", provider: "codex", effort: "max"},
		{name: "unknown provider rejected", provider: "made-up", wantErr: true, errContains: "provider"},
		{name: "unknown effort rejected", provider: "claude-code", effort: "turbo", wantErr: true, errContains: "effort"},
		{name: "oversized model rejected", model: longModel, wantErr: true, errContains: "too long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTaskRuntimeFields(tc.provider, tc.model, tc.effort)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateTaskRuntimeFields(%q,%q,%q) = nil, want error", tc.provider, tc.model, tc.effort)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("validateTaskRuntimeFields(%q,%q,%q) = %v, want nil", tc.provider, tc.model, tc.effort, err)
			}
		})
	}
}

// TestEffortRoundTripsThroughWire confirms the new effort field survives a
// marshal/unmarshal cycle with the stable wire key "effort".
func TestEffortRoundTripsThroughWire(t *testing.T) {
	original := teamTask{ID: "task-1", Title: "demo", Effort: "high"}
	data, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if want := `"effort":"high"`; !strings.Contains(string(data), want) {
		t.Fatalf("marshalled task missing %s; got %s", want, string(data))
	}
	var decoded teamTask
	if err := decoded.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if decoded.Effort != "high" {
		t.Errorf("round-trip Effort = %q, want %q", decoded.Effort, "high")
	}
}
