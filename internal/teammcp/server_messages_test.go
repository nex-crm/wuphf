package teammcp

import (
	"strings"
	"testing"
)

// Regression guard (ten-out-of-ten Wave E handoff-2): the 800-char poll
// clip silently dropped the tail of long human messages — agents absorbed
// half a redline and re-asked for the other half (ICP-eval v3
// [18:05–18:10]). Human-authored content must reach agents IN FULL; only
// agent/system chatter keeps the clip as a token-overflow guard.
func TestFormatMessagesNeverClipsHumanContent(t *testing.T) {
	tail := "FINAL-REDLINE-OMEGA: sender name is Maya."
	long := strings.Repeat("redline detail ", 80) + tail // ~1.2k chars, past the 800 clip
	if len(long) <= 800 {
		t.Fatalf("fixture message too short: %d chars", len(long))
	}

	for _, from := range []string{"you", "human", "human:maya"} {
		out := formatMessages([]brokerMessage{
			{ID: "msg-1", From: from, Content: long, Timestamp: "2026-06-12T10:00:00Z"},
		}, "eng")
		if !strings.Contains(out, tail) {
			t.Errorf("human message from %q was clipped: tail marker missing from poll output", from)
		}
		if strings.Contains(out, "…") {
			t.Errorf("human message from %q carries the clip ellipsis", from)
		}
	}
}

// Agent and automation content keeps the 800-char clip — the exemption is
// scoped to humans only.
func TestFormatMessagesStillClipsAgentContent(t *testing.T) {
	tail := "AGENT-TAIL-MARKER"
	long := strings.Repeat("agent report detail ", 60) + tail // ~1.2k chars

	out := formatMessages([]brokerMessage{
		{ID: "msg-2", From: "eng", Content: long, Timestamp: "2026-06-12T10:00:00Z"},
	}, "ceo")
	if strings.Contains(out, tail) {
		t.Errorf("agent message was not clipped: tail marker should be truncated away")
	}
	if !strings.Contains(out, "…") {
		t.Errorf("agent message missing the clip ellipsis")
	}

	auto := formatMessages([]brokerMessage{
		{ID: "msg-3", From: "wuphf", Kind: "automation", Content: long, Timestamp: "2026-06-12T10:00:00Z"},
	}, "ceo")
	if strings.Contains(auto, tail) {
		t.Errorf("automation message was not clipped")
	}
}
