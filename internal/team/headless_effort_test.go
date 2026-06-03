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
