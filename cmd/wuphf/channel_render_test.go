package main

import (
	"strings"
	"testing"
)

func TestDefaultHumanMessageTitleByKind(t *testing.T) {
	cases := map[string]string{
		"human_decision": "needs your call",
		"human_action":   "wants you to do something",
		"human_report":   "has an update for you",
		"":               "has an update for you",
	}
	for kind, want := range cases {
		got := defaultHumanMessageTitle(kind, "ceo")
		if !strings.Contains(got, want) {
			t.Errorf("defaultHumanMessageTitle(%q, ceo) = %q, want substring %q", kind, got, want)
		}
	}
}

func TestSliceRenderedLinesScrollsFromBottom(t *testing.T) {
	lines := []renderedLine{
		{Text: "0"}, {Text: "1"}, {Text: "2"}, {Text: "3"}, {Text: "4"},
	}
	visible, scroll, start, end := sliceRenderedLines(lines, 3, 0)
	if scroll != 0 || start != 2 || end != 5 || len(visible) != 3 {
		t.Fatalf("expected bottom 3 lines, got start=%d end=%d scroll=%d len=%d", start, end, scroll, len(visible))
	}
	if visible[0].Text != "2" || visible[2].Text != "4" {
		t.Fatalf("expected lines [2..4], got %v", visible)
	}
}

func TestSliceRenderedLinesScrollClampsToBuffer(t *testing.T) {
	lines := []renderedLine{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}}
	visible, scroll, start, _ := sliceRenderedLines(lines, 2, 99)
	// scroll clamped: total - msgH = 4 - 2 = 2
	if scroll != 2 {
		t.Fatalf("expected scroll clamped to 2, got %d", scroll)
	}
	if start != 0 {
		t.Fatalf("expected start=0 when fully scrolled up, got %d", start)
	}
	if len(visible) != 2 || visible[0].Text != "a" {
		t.Fatalf("expected top slice, got %v", visible)
	}
}

func TestSliceRenderedLinesEmptyInput(t *testing.T) {
	visible, scroll, start, end := sliceRenderedLines(nil, 5, 0)
	if visible != nil || scroll != 0 || start != 0 || end != 0 {
		t.Fatalf("empty input should return zero values, got %v %d %d %d", visible, scroll, start, end)
	}
}

func TestBuildRequestLinesEmptyShowsCoachingCopy(t *testing.T) {
	lines := buildRequestLines(nil, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "No open requests") {
		t.Fatalf("expected empty-state message, got %q", plain)
	}
}

func TestBuildRequestLinesIncludesQuestionAndContext(t *testing.T) {
	requests := []channelInterview{{
		ID:            "req-1",
		Kind:          "decision",
		From:          "ceo",
		Status:        "open",
		Title:         "Launch decision",
		Question:      "Approve the rollout?",
		Context:       "The metrics look healthy and stable.",
		RecommendedID: "ship",
		Blocking:      true,
		Required:      true,
		CreatedAt:     "2026-04-29T09:00:00Z",
	}}
	lines := buildRequestLines(requests, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "Approve the rollout?") {
		t.Fatalf("expected question, got %q", plain)
	}
	if !strings.Contains(plain, "Launch decision") {
		t.Fatalf("expected title in meta, got %q", plain)
	}
	if !strings.Contains(plain, "Recommended: ship") {
		t.Fatalf("expected recommended id, got %q", plain)
	}
	if !strings.Contains(plain, "blocking") {
		t.Fatalf("expected blocking marker, got %q", plain)
	}
	if !strings.Contains(plain, "unblocks the team") {
		t.Fatalf("blocking requests should explain dismiss consequences, got %q", plain)
	}
}

func TestBuildPolicyLinesEmptyShowsGuidance(t *testing.T) {
	lines := buildPolicyLines(nil, nil, nil, nil, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "Insights") {
		t.Fatalf("expected Insights header, got %q", plain)
	}
	if !strings.Contains(plain, "No office insights yet") {
		t.Fatalf("expected empty-state guidance, got %q", plain)
	}
}

func TestBuildPolicyLinesShowsSignalsDecisionsWatchdogs(t *testing.T) {
	signals := []channelSignal{{ID: "s1", Title: "Spike", Content: "Latency rising", Owner: "be"}}
	decisions := []channelDecision{{ID: "d1", Summary: "Roll forward", Reason: "Risk acceptable", Owner: "ceo"}}
	watchdogs := []channelWatchdog{{ID: "w1", Summary: "Build flaking", Status: "active", Kind: "ci"}}
	lines := buildPolicyLines(signals, decisions, watchdogs, nil, 80)
	plain := stripANSI(joinRenderedLines(lines))
	if !strings.Contains(plain, "Spike") {
		t.Fatalf("expected signal title, got %q", plain)
	}
	if !strings.Contains(plain, "Roll forward") {
		t.Fatalf("expected decision summary, got %q", plain)
	}
	if !strings.Contains(plain, "Build flaking") {
		t.Fatalf("expected watchdog summary, got %q", plain)
	}
}
