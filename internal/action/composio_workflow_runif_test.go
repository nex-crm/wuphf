package action

import "testing"

func runIfScope() map[string]any {
	return map[string]any{
		"inputs": map[string]any{
			"channel": "sales",
			"enabled": true,
		},
		"steps": map[string]any{
			"score": map[string]any{
				"result": map[string]any{
					"fit":  float64(82),
					"tier": "A",
				},
			},
		},
	}
}

func TestParseRunIf(t *testing.T) {
	cases := []struct {
		expr    string
		wantErr bool
	}{
		{"steps.score.result.fit >= 80", false},
		{"{{ steps.score.result.fit >= 80 }}", false},
		{`inputs.channel == "sales"`, false},
		{"inputs.enabled == true", false},
		{"steps.score.result.fit", true},       // no operator
		{"steps.score.result.fit >= ", true},   // missing operand
		{"steps.score.result.fit ~~ 80", true}, // unknown operator
		{"steps.score; drop >= 80", true},      // illegal path chars
	}
	for _, tc := range cases {
		_, err := parseRunIf(tc.expr)
		if tc.wantErr && err == nil {
			t.Errorf("parseRunIf(%q): expected error, got nil", tc.expr)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("parseRunIf(%q): unexpected error: %v", tc.expr, err)
		}
	}
}

func TestEvaluateRunIf(t *testing.T) {
	scope := runIfScope()
	cases := []struct {
		expr string
		want bool
	}{
		{"steps.score.result.fit >= 80", true},
		{"steps.score.result.fit > 82", false},
		{"steps.score.result.fit == 82", true},
		{"steps.score.result.fit < 100", true},
		{"{{ steps.score.result.fit <= 82 }}", true},
		{`steps.score.result.tier == "A"`, true},
		{`steps.score.result.tier != "B"`, true},
		{`inputs.channel == "sales"`, true},
		{"inputs.enabled == true", true},
		{"inputs.enabled == false", false},
	}
	for _, tc := range cases {
		got, err := evaluateRunIf(tc.expr, scope)
		if err != nil {
			t.Errorf("evaluateRunIf(%q): unexpected error: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("evaluateRunIf(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestEvaluateRunIfErrors(t *testing.T) {
	scope := runIfScope()
	// Unresolved path must surface as an error, never a silent skip.
	if _, err := evaluateRunIf("steps.missing.value >= 1", scope); err == nil {
		t.Error("expected error for unresolved path, got nil")
	}
	// Ordering comparison on non-numbers is an error.
	if _, err := evaluateRunIf(`steps.score.result.tier >= 80`, scope); err == nil {
		t.Error("expected error comparing a string with >=, got nil")
	}
}
