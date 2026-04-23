package team

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDuplicateAgentBroadcastIsSuppressed verifies that when the same agent
// posts a near-identical broadcast to the same channel+thread within the
// dedup window, the broker silently drops the duplicate. This is the
// broker-side safety net for the "CEO emits 3 broadcasts in one turn"
// pattern — the prompt rule telling agents not to do that is routinely
// ignored, so this enforces it at the persistence layer.
func TestDuplicateAgentBroadcastIsSuppressed(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.messages = []channelMessage{
		{
			ID:        "msg-1",
			From:      "ceo",
			Channel:   "general",
			Content:   "Ball is in reviewer's court, shipping the PR now.",
			ReplyTo:   "",
			Timestamp: now,
		},
	}
	b.mu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Byte-identical → drop.
	if !b.isDuplicateAgentBroadcastLocked("ceo", "general", "", "Ball is in reviewer's court, shipping the PR now.") {
		t.Error("exact duplicate should be detected")
	}
	// Paraphrased but same semantic content → drop (Jaccard over word set).
	if !b.isDuplicateAgentBroadcastLocked("ceo", "general", "", "Ball is in the reviewer's court — shipping the PR now.") {
		t.Error("near-duplicate with trivial punctuation drift should be detected")
	}
	// Truly different content → allow.
	if b.isDuplicateAgentBroadcastLocked("ceo", "general", "", "Planner is blocked on a missing spec.") {
		t.Error("distinct content must not be flagged duplicate")
	}
	// Different agent → allow.
	if b.isDuplicateAgentBroadcastLocked("planner", "general", "", "Ball is in reviewer's court, shipping the PR now.") {
		t.Error("duplicate detection must scope to the sender")
	}
	// Different thread → allow.
	if b.isDuplicateAgentBroadcastLocked("ceo", "general", "msg-99", "Ball is in reviewer's court, shipping the PR now.") {
		t.Error("duplicate detection must scope to (channel, thread)")
	}
}

// TestDuplicateAgentBroadcastWindowExpires verifies the dedup window is time
// bounded — a follow-up beyond the window posts normally.
func TestDuplicateAgentBroadcastWindowExpires(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	old := time.Now().UTC().Add(-2 * duplicateBroadcastWindow).Format(time.RFC3339)
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "msg-1", From: "ceo", Channel: "general", Content: "same content", Timestamp: old},
	}
	defer b.mu.Unlock()

	if b.isDuplicateAgentBroadcastLocked("ceo", "general", "", "same content") {
		t.Error("messages older than duplicateBroadcastWindow must not trigger dedup")
	}
}

// TestStaleUnansweredFilteredOnResume verifies resume packets drop
// unanswered human messages older than staleUnansweredThreshold. Without
// this, a broker crash/restart replays zombie work (observed symptom: an
// old "@planner say hi" from hours before the restart wakes planner with
// the wrong intent).
func TestStaleUnansweredFilteredOnResume(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	// This test runs with the production-like threshold, not the test-suite
	// override from TestMain. Swap locally so production semantics win here.
	origThreshold := staleUnansweredThreshold
	staleUnansweredThreshold = time.Hour
	defer func() { staleUnansweredThreshold = origThreshold }()

	b := NewBroker()
	stale := time.Now().UTC().Add(-90 * time.Minute).Format(time.RFC3339)
	fresh := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "h1", From: "you", Channel: "general", Content: "stale — ignore", Tagged: []string{"planner"}, Timestamp: stale},
		{ID: "h2", From: "you", Channel: "general", Content: "fresh — answer", Tagged: []string{"planner"}, Timestamp: fresh},
		{ID: "h3", From: "you", Channel: "general", Content: "malformed — ignore", Tagged: []string{"planner"}, Timestamp: "not-a-time"},
	}
	b.mu.Unlock()

	l := Launcher{
		broker: b,
	}

	packets := l.buildResumePackets()
	if p, ok := packets["planner"]; !ok {
		t.Fatal("planner should receive a packet for the fresh unanswered message")
	} else {
		if strings.Contains(p, "stale — ignore") {
			t.Error("stale message must not appear in resume packet")
		}
		if strings.Contains(p, "malformed — ignore") {
			t.Error("messages with malformed timestamps must not appear in resume packet")
		}
		if !strings.Contains(p, "fresh — answer") {
			t.Error("fresh message must appear in resume packet")
		}
	}
}
