package channelui

import (
	"strings"
	"testing"
)

func TestHumanMessageLabelByKind(t *testing.T) {
	cases := map[string]string{
		"human_decision": "decision",
		"human_action":   "action",
		"":               "report",
		"unknown":        "report",
	}
	for kind, want := range cases {
		if got := HumanMessageLabel(kind); got != want {
			t.Errorf("HumanMessageLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestRenderUnreadDividerIncludesCount(t *testing.T) {
	got := stripANSI(RenderUnreadDivider(60, 3))
	if !strings.Contains(got, "3 new since you looked") {
		t.Fatalf("expected count in divider, got %q", got)
	}
}

func TestRenderUnreadDividerNoCountFallback(t *testing.T) {
	got := stripANSI(RenderUnreadDivider(60, 0))
	if !strings.Contains(got, "New since you looked") {
		t.Fatalf("expected generic divider when count is zero, got %q", got)
	}
}

func TestRenderUnreadDividerSurvivesNarrowWidth(t *testing.T) {
	// Width below the label length must not panic and must still produce text.
	if got := RenderUnreadDivider(4, 1); got == "" {
		t.Fatalf("narrow divider should still render content")
	}
}
