package render

import (
	"strings"
	"testing"
)

func TestRenderInsightsBasic(t *testing.T) {
	insights := []Insight{
		{
			Priority: "critical",
			Category: "security",
			Title:    "Token expiring soon",
			Body:     "API token expires in 2 days",
			Target:   "settings/tokens",
			Time:     "2h ago",
		},
		{
			Priority: "high",
			Category: "pipeline",
			Title:    "Build failure rate increasing",
			Body:     "Failure rate up 15% this week",
			Target:   "ci/builds",
			Time:     "5h ago",
		},
	}

	out := RenderInsights(insights)

	// Check priority badges present.
	if !strings.Contains(out, "CRIT") {
		t.Error("expected [CRIT] badge")
	}
	if !strings.Contains(out, "HIGH") {
		t.Error("expected [HIGH] badge")
	}

	// Check categories.
	if !strings.Contains(out, "security") {
		t.Error("expected security category")
	}
	if !strings.Contains(out, "pipeline") {
		t.Error("expected pipeline category")
	}

	// Check titles.
	if !strings.Contains(out, "Token expiring soon") {
		t.Error("expected title 'Token expiring soon'")
	}
	if !strings.Contains(out, "Build failure rate increasing") {
		t.Error("expected title 'Build failure rate increasing'")
	}

	// Check body text.
	if !strings.Contains(out, "API token expires in 2 days") {
		t.Error("expected body text")
	}

	// Check target hints.
	if !strings.Contains(out, "settings/tokens") {
		t.Error("expected target hint")
	}

	// Check timestamps.
	if !strings.Contains(out, "2h ago") {
		t.Error("expected timestamp")
	}

	// Check separator between insights.
	if !strings.Contains(out, "───") {
		t.Error("expected separator between insights")
	}
}

func TestRenderInsightsEmpty(t *testing.T) {
	out := RenderInsights(nil)
	if !strings.Contains(out, "(no insights)") {
		t.Errorf("expected '(no insights)', got %q", out)
	}

	out2 := RenderInsights([]Insight{})
	if !strings.Contains(out2, "(no insights)") {
		t.Errorf("expected '(no insights)', got %q", out2)
	}
}

func TestRenderInsightsTruncation(t *testing.T) {
	longBody := strings.Repeat("x", 200)
	insights := []Insight{
		{
			Priority: "medium",
			Category: "data",
			Title:    "Long insight",
			Body:     longBody,
		},
	}

	out := RenderInsights(insights)

	// The body should be truncated. The rendered body should not contain
	// the full 200-char string, and should end with "...".
	if strings.Contains(out, longBody) {
		t.Error("expected body to be truncated, but full body found")
	}
	if !strings.Contains(out, "...") {
		t.Error("expected truncated body to end with '...'")
	}
	if !strings.Contains(out, "MED") {
		t.Error("expected [MED] badge for medium priority")
	}
}
