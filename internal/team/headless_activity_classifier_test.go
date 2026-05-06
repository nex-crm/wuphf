package team

import "testing"

// TestClassifyActivityKind locks the seed table for the activity classifier
// that drives agent-event bubble Kind tagging. Stuck is intentionally not
// covered here — that path is owned by the reaper / watchdog, not the
// per-event classifier.
func TestClassifyActivityKind(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		status string
		detail string
		want   string
	}{
		{
			name:   "default plain text edit is routine",
			tool:   "text",
			status: "active",
			detail: "drafting response",
			want:   "routine",
		},
		{
			name:   "running tests via Bash is a milestone",
			tool:   "Bash",
			status: "active",
			detail: "running go test ./internal/team",
			want:   "milestone",
		},
		{
			name:   "running build via Bash is a milestone",
			tool:   "Bash",
			status: "active",
			detail: "running go build ./cmd/wuphf",
			want:   "milestone",
		},
		{
			name:   "any error status is a milestone regardless of tool",
			tool:   "tool",
			status: "error",
			detail: "transport failure",
			want:   "milestone",
		},
		{
			name:   "deploy keyword in detail is a milestone",
			tool:   "tool_use",
			status: "active",
			detail: "deploy to staging.acme.com",
			want:   "milestone",
		},
		{
			name:   "plain text-edit activity stays routine",
			tool:   "text",
			status: "active",
			detail: "writing changelog entry",
			want:   "routine",
		},
		{
			name:   "pure read tool (grep) is always routine",
			tool:   "Grep",
			status: "active",
			detail: "scanning for pattern",
			want:   "routine",
		},
		{
			name:   "headless-style activity tool with go test detail is milestone",
			tool:   "tool",
			status: "active",
			detail: "running go test ./...",
			want:   "milestone",
		},
		{
			name:   "merged PR detail is a milestone",
			tool:   "tool",
			status: "active",
			detail: "merged PR #642",
			want:   "milestone",
		},
		{
			name:   "thinking with no keyword is routine",
			tool:   "thinking",
			status: "active",
			detail: "reviewing work packet",
			want:   "routine",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyActivityKind(tc.tool, tc.status, tc.detail)
			if got != tc.want {
				t.Fatalf("classifyActivityKind(%q,%q,%q) = %q, want %q", tc.tool, tc.status, tc.detail, got, tc.want)
			}
			if got == "stuck" {
				t.Fatalf("classifier must never return stuck (owned by reaper/watchdog)")
			}
		})
	}
}
