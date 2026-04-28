package team

import "testing"

// TestHasSkillBodyShape locks in the body-shape gate that protects the
// explicit-frontmatter fast path from FAST_PATH_TRAP articles (D9).
// Articles with valid Anthropic frontmatter still need a recognisable
// skill body shape — section header + list/numbered steps — before the
// scanner promotes them without LLM judgment.
func TestHasSkillBodyShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "steps + numbered list",
			body: "## Steps\n\n1. Do thing.\n2. Do other thing.",
			want: true,
		},
		{
			name: "how to + bullets",
			body: "## How to\n\n- Step one.\n- Step two.",
			want: true,
		},
		{
			name: "procedure + numbered",
			body: "## Procedure\n\n1. First.\n2. Second.",
			want: true,
		},
		{
			name: "runbook + numbered",
			body: "## Runbook\n\n1. Run.\n2. Verify.",
			want: true,
		},
		{
			name: "header but no list (bio prose)",
			body: "## Steps\n\nSome prose without numbered or bulleted items.",
			want: false,
		},
		{
			name: "list but no skill header (random notes)",
			body: "## Notes\n\n1. We talked about Q3.\n2. We talked about Q4.",
			want: false,
		},
		{
			name: "FAST_PATH_TRAP bio (D9)",
			body: "Jane joined the team in 2026 after a decade at Stripe.\nFavourite project: shipping Stripe's first multi-currency rail.",
			want: false,
		},
		{
			name: "FAST_PATH_TRAP decision log (D9)",
			body: "**Context.** Internal-only consumers.\n**Decision.** Ship REST.\n**Consequences.** Slower client iteration.",
			want: false,
		},
		{
			name: "marketing copy with Steps header but no list (D9)",
			body: "## Steps to better collaboration\n\nWUPHF helps your team move faster. Modern teams deserve modern tools.",
			want: false,
		},
		{
			name: "case-insensitive header match",
			body: "## STEPS\n\n1. Yell.",
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSkillBodyShape(tc.body); got != tc.want {
				t.Errorf("hasSkillBodyShape(%q): got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
