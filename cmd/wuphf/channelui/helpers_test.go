package channelui

import (
	"strings"
	"testing"
)

func TestContainsStringMatchesTrimmedTarget(t *testing.T) {
	items := []string{" fe ", "be", "ceo"}
	if !ContainsString(items, "fe") {
		t.Fatalf("expected trimmed match for fe")
	}
	if !ContainsString(items, "be") {
		t.Fatalf("expected match for be")
	}
	if ContainsString(items, "pm") {
		t.Fatalf("unexpected match for pm")
	}
	if ContainsString(nil, "fe") {
		t.Fatalf("nil slice should not match")
	}
}

func TestRenderTimingSummaryJoinsParts(t *testing.T) {
	got := RenderTimingSummary("2030-01-01T10:00:00Z", "", "", "")
	if got == "" {
		t.Fatalf("expected non-empty timing summary, got empty")
	}
	if !strings.Contains(got, "due") {
		t.Fatalf("expected 'due' label in timing summary, got %q", got)
	}
}

func TestRenderTimingSummaryAllBlank(t *testing.T) {
	if got := RenderTimingSummary("", "", "", ""); got != "" {
		t.Fatalf("blank inputs should yield empty timing summary, got %q", got)
	}
}

func TestPrettyWhenUnparsable(t *testing.T) {
	got := PrettyWhen("not-a-time", "due")
	if !strings.Contains(got, "not-a-time") {
		t.Fatalf("unparsable timestamps should fall through, got %q", got)
	}
}
