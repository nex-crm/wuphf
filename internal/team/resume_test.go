package team

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// agent is used by TestBuildResumePacketsRouting to construct a Launcher with a pack.
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

// --- Tests for Launcher.buildResumePackets ---

func TestBuildResumePacketsTaggedMessageRoutesToTaggedAgent(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "h1", From: "you", Content: "hey @fe please build the login page", Tagged: []string{"fe"}, Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	packets := l.buildResumePackets()

	// h1 is tagged @fe — only fe should receive a packet about this message.
	if _, ok := packets["fe"]; !ok {
		t.Fatal("expected 'fe' to receive a resume packet for tagged message")
	}
	if strings.Contains(packets["fe"], "ceo") {
		t.Error("fe packet should not route to ceo")
	}
	// ceo should not receive a packet for this message (it was tagged only @fe).
	if p, ok := packets["ceo"]; ok && strings.Contains(p, "login page") {
		t.Error("expected ceo NOT to receive the tagged message meant for fe")
	}
}

func TestBuildResumePacketsUntaggedMessageRoutesToLead(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "h1", From: "you", Content: "what should we build next?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	packets := l.buildResumePackets()

	// Untagged message with no reply → goes to pack lead (ceo).
	if _, ok := packets["ceo"]; !ok {
		t.Fatal("expected 'ceo' to receive a resume packet for untagged message")
	}
	if !strings.Contains(packets["ceo"], "build next") {
		t.Error("ceo packet should contain the untagged message content")
	}
}

func TestBuildResumePacketsInFlightTasksIncluded(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Build dashboard", Owner: "fe", Status: "in_progress"},
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	packets := l.buildResumePackets()

	if _, ok := packets["fe"]; !ok {
		t.Fatal("expected 'fe' to receive a resume packet for their in-flight task")
	}
	if !strings.Contains(packets["fe"], "Build dashboard") {
		t.Error("fe packet should contain their task title")
	}
}

func TestBuildResumePacketsEmptyWhenNothingInFlight(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	// No tasks, no messages.
	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
		},
	}

	packets := l.buildResumePackets()
	if len(packets) != 0 {
		t.Fatalf("expected empty packets when nothing in flight, got %d", len(packets))
	}
}

// --- Integration tests for edge cases ---

func TestResumeInFlightWorkNoBrokerNoPanic(t *testing.T) {
	// Launcher with nil broker must not panic.
	l := &Launcher{broker: nil}
	// Should complete without panicking.
	l.resumeInFlightWork()
}

func TestResumeInFlightWorkNoPackNoPanic(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "h1", From: "you", Content: "unanswered question", Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	// Launcher with broker but nil pack — officeLeadSlug() should handle gracefully.
	l := &Launcher{broker: b, pack: nil}
	// Should complete without panicking.
	l.resumeInFlightWork()
}

func TestBuildResumePacketsUnansweredRoutesToLead(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		// answered: has a reply
		{ID: "h1", From: "you", Content: "old answered question", Timestamp: "2026-04-14T09:00:00Z"},
		{ID: "a1", From: "ceo", Content: "Here is the answer", ReplyTo: "h1", Timestamp: "2026-04-14T09:01:00Z"},
		// unanswered: no reply
		{ID: "h2", From: "you", Content: "new unanswered question", Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	packets := l.buildResumePackets()

	// Only the unanswered message (h2) should be in the packet.
	// It is untagged → routes to ceo (lead).
	if _, ok := packets["ceo"]; !ok {
		t.Fatal("expected 'ceo' to receive a resume packet for unanswered message")
	}
	if !strings.Contains(packets["ceo"], "unanswered question") {
		t.Error("ceo packet should contain the unanswered message content")
	}
	if strings.Contains(packets["ceo"], "old answered question") {
		t.Error("ceo packet should NOT contain already-answered message content")
	}
}
