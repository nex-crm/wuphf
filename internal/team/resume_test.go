package team

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// Silence the agent import for now — it's used in Task 3 for buildResumePackets.
// This reference ensures the import is used across the test file.
var _ = agent.Packs

func TestFindUnansweredMessagesAllAnswered(t *testing.T) {
	humanMsgs := []channelMessage{
		{ID: "h1", From: "you", Content: "Can you build the login page?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	allMessages := []channelMessage{
		{ID: "h1", From: "you", Content: "Can you build the login page?", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "a1", From: "fe", Content: "On it!", ReplyTo: "h1", Timestamp: "2026-04-14T10:01:00Z"},
	}

	got := findUnansweredMessages(humanMsgs, allMessages)
	if len(got) != 0 {
		t.Fatalf("expected 0 unanswered messages, got %d: %+v", len(got), got)
	}
}

func TestFindUnansweredMessagesNoneAnswered(t *testing.T) {
	humanMsgs := []channelMessage{
		{ID: "h1", From: "you", Content: "First question", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "h2", From: "human", Content: "Second question", Timestamp: "2026-04-14T10:02:00Z"},
	}
	allMessages := []channelMessage{
		{ID: "h1", From: "you", Content: "First question", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "a1", From: "fe", Content: "Working on it...", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "h2", From: "human", Content: "Second question", Timestamp: "2026-04-14T10:02:00Z"},
	}

	// h1 has no ReplyTo pointing to it, a1 is a new message not a reply
	// h2 has no reply at all
	got := findUnansweredMessages(humanMsgs, allMessages)
	if len(got) != 2 {
		t.Fatalf("expected 2 unanswered messages, got %d: %+v", len(got), got)
	}
}

func TestFindUnansweredMessagesPartialAnswers(t *testing.T) {
	humanMsgs := []channelMessage{
		{ID: "h1", From: "you", Content: "Question one", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "h2", From: "you", Content: "Question two", Timestamp: "2026-04-14T10:02:00Z"},
	}
	allMessages := []channelMessage{
		{ID: "h1", From: "you", Content: "Question one", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "a1", From: "be", Content: "Here's my answer", ReplyTo: "h1", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "h2", From: "you", Content: "Question two", Timestamp: "2026-04-14T10:02:00Z"},
	}

	got := findUnansweredMessages(humanMsgs, allMessages)
	if len(got) != 1 {
		t.Fatalf("expected 1 unanswered message, got %d: %+v", len(got), got)
	}
	if got[0].ID != "h2" {
		t.Errorf("expected unanswered message h2, got %q", got[0].ID)
	}
}

func TestFindUnansweredMessagesEmptyInputs(t *testing.T) {
	got := findUnansweredMessages(nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected 0 unanswered messages for empty inputs, got %d", len(got))
	}
}

func TestBuildResumePacketWithTasksAndMessages(t *testing.T) {
	// Suppress broker state path for this test.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	tasks := []teamTask{
		{ID: "t1", Title: "Build the login page", Owner: "fe", Status: "in_progress"},
		{ID: "t2", Title: "Design API schema", Owner: "fe", Status: "pending"},
	}
	msgs := []channelMessage{
		{ID: "h1", From: "you", Content: "Can you also add a logout button?", Timestamp: "2026-04-14T10:05:00Z"},
	}

	packet := buildResumePacket("fe", tasks, msgs)

	// Should contain the agent slug.
	if !strings.Contains(packet, "fe") {
		t.Error("expected packet to reference agent slug 'fe'")
	}
	// Should contain task titles.
	if !strings.Contains(packet, "Build the login page") {
		t.Error("expected packet to contain task title 'Build the login page'")
	}
	if !strings.Contains(packet, "Design API schema") {
		t.Error("expected packet to contain task title 'Design API schema'")
	}
	// Should contain unanswered message content.
	if !strings.Contains(packet, "logout button") {
		t.Error("expected packet to contain unanswered message content")
	}
}

func TestBuildResumePacketNoTasksNoMessages(t *testing.T) {
	packet := buildResumePacket("ceo", nil, nil)
	// An empty packet should be empty string (no work to resume).
	if packet != "" {
		t.Errorf("expected empty packet when no tasks and no messages, got %q", packet)
	}
}

func TestBuildResumePacketTasksOnly(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Title: "Finalize roadmap", Owner: "ceo", Status: "in_progress"},
	}
	packet := buildResumePacket("ceo", tasks, nil)
	if packet == "" {
		t.Fatal("expected non-empty packet when tasks exist")
	}
	if !strings.Contains(packet, "Finalize roadmap") {
		t.Error("expected packet to contain task title")
	}
}

func TestBuildResumePacketMessagesOnly(t *testing.T) {
	msgs := []channelMessage{
		{ID: "h1", From: "you", Content: "What's the sprint plan?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	packet := buildResumePacket("ceo", nil, msgs)
	if packet == "" {
		t.Fatal("expected non-empty packet when messages exist")
	}
	if !strings.Contains(packet, "sprint plan") {
		t.Error("expected packet to contain message content")
	}
}
