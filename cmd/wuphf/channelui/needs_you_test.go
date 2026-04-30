package channelui

import (
	"strings"
	"testing"
)

func TestBuildNeedsYouLinesPrefersBlockingRequests(t *testing.T) {
	requests := []Interview{
		{ID: "req-1", Kind: "approval", Status: "pending", Title: "Optional note", Question: "Optional note?", From: "pm"},
		{ID: "req-2", Kind: "approval", Status: "pending", Title: "Ship launch copy", Question: "Ship launch copy?", Context: "Need approval before publishing.", From: "ceo", Blocking: true, RecommendedID: "approve"},
	}

	lines := BuildNeedsYouLines(requests, 96)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Needs attention") {
		t.Fatalf("expected needs-attention separator, got %q", plain)
	}
	if !strings.Contains(plain, "Ship launch copy") {
		t.Fatalf("expected blocking request title, got %q", plain)
	}
	if !strings.Contains(plain, "The team is paused until you answer.") {
		t.Fatalf("expected blocking request guidance, got %q", plain)
	}
}
