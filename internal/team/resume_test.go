package team

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// agent is used by the routing tests to construct legacy compatibility packs.
var _ = agent.LookupLegacyPack

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

func TestFindUnansweredMessagesHumanThreadReplyDoesNotCountAsAgentAnswer(t *testing.T) {
	// Spec: only AGENT replies should mark a message as answered.
	// A human following up in a thread (ReplyTo pointing at another human message)
	// must NOT cause the original message to be treated as answered.
	humanMsgs := []channelMessage{
		{ID: "h1", From: "you", Content: "Can you build the login page?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	allMessages := []channelMessage{
		{ID: "h1", From: "you", Content: "Can you build the login page?", Timestamp: "2026-04-14T10:00:00Z"},
		// Human follow-up reply — NOT an agent answer
		{ID: "h2", From: "human", Content: "Adding more context here", ReplyTo: "h1", Timestamp: "2026-04-14T10:01:00Z"},
	}

	got := findUnansweredMessages(humanMsgs, allMessages)
	// h1 should still be unanswered — h2 is a human reply, not an agent reply.
	if len(got) != 1 {
		t.Fatalf("expected h1 to remain unanswered (human thread reply is not an agent answer), got %d: %+v", len(got), got)
	}
	if got[0].ID != "h1" {
		t.Errorf("expected unanswered message h1, got %q", got[0].ID)
	}
}

func TestFindUnansweredMessagesNexReplyDoesNotCountAsAgentAnswer(t *testing.T) {
	// Nex automation messages (kind=automation) are not agent replies.
	humanMsgs := []channelMessage{
		{ID: "h1", From: "you", Content: "What is the status?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	allMessages := []channelMessage{
		{ID: "h1", From: "you", Content: "What is the status?", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "n1", From: "nex", Content: "Here is context from Nex", ReplyTo: "h1", Timestamp: "2026-04-14T10:01:00Z"},
	}

	got := findUnansweredMessages(humanMsgs, allMessages)
	// h1 should still be unanswered — nex reply is not an agent answer.
	if len(got) != 1 {
		t.Fatalf("expected h1 to remain unanswered (nex reply is not an agent answer), got %d: %+v", len(got), got)
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

func TestBuildResumePacketsIncludesDynamicBrokerMembersOutsideLaunchPack(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "executor", Name: "Executor"},
		{Slug: "builder", Name: "Builder"},
	}
	b.channels = []teamChannel{{
		Slug:    "youtube-factory",
		Name:    "youtube-factory",
		Members: []string{"ceo", "executor", "builder"},
	}}
	b.tasks = []teamTask{{
		ID:        "task-44",
		Channel:   "youtube-factory",
		Title:     "Restore Remotion dependency path",
		Owner:     "builder",
		Status:    "in_progress",
		CreatedBy: "ceo",
	}}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "blank-slate",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "executor", Name: "Executor"},
			},
		},
	}

	packets := l.buildResumePackets()
	if _, ok := packets["builder"]; !ok {
		t.Fatalf("expected dynamic broker member outside launch pack to receive a resume packet, got %+v", packets)
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

func TestBuildResumePacketsSkipsAgentsNotInPack(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.tasks = []teamTask{
		// "designer" is NOT in the pack below — their task should be skipped.
		{ID: "t1", Title: "Design the landing page", Owner: "designer", Status: "in_progress"},
		// "fe" IS in the pack — their task should be included.
		{ID: "t2", Title: "Build the login form", Owner: "fe", Status: "in_progress"},
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "coding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	packets := l.buildResumePackets()

	// "designer" is not in the pack → no packet for them.
	if _, ok := packets["designer"]; ok {
		t.Error("expected no resume packet for 'designer' (not in current pack)")
	}
	// "fe" is in the pack → should have a packet.
	if _, ok := packets["fe"]; !ok {
		t.Fatal("expected resume packet for 'fe' (in current pack)")
	}
	if !strings.Contains(packets["fe"], "Build the login form") {
		t.Error("fe packet should contain their task title")
	}
}

func TestBuildResumePacketsSkipsTaggedAgentsNotInPack(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		// Tagged @old-agent who is no longer in the pack.
		{ID: "h1", From: "you", Content: "hey @old-agent can you help?", Tagged: []string{"old-agent"}, Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			Slug:     "coding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	packets := l.buildResumePackets()

	// "old-agent" is not in the pack → no packet should be generated for them.
	if _, ok := packets["old-agent"]; ok {
		t.Error("expected no resume packet for 'old-agent' (not in current pack)")
	}
}

func TestBuildResumePacketIncludesWorktreePath(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Title: "Build the API", Owner: "be", Status: "in_progress", WorktreePath: "/workspace/feat-api"},
		{ID: "t2", Title: "No worktree task", Owner: "be", Status: "in_progress", WorktreePath: ""},
	}
	packet := buildResumePacket("be", tasks, nil)

	// Task with worktree should include the working directory instruction.
	if !strings.Contains(packet, "/workspace/feat-api") {
		t.Error("expected packet to include WorktreePath for t1")
	}
	// Task without worktree should not add spurious path lines.
	if strings.Contains(packet, "working_directory") && !strings.Contains(packet, "/workspace/feat-api") {
		t.Error("unexpected working_directory reference for task without WorktreePath")
	}
}

func TestBuildResumePacketIncludesReplyToInstructions(t *testing.T) {
	msgs := []channelMessage{
		{ID: "h1", From: "you", Channel: "general", Content: "What is the plan?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	packet := buildResumePacket("ceo", nil, msgs)

	// Spec: packet must include channel and reply_to_id so agent knows how to thread their response.
	if !strings.Contains(packet, "general") {
		t.Error("expected packet to include channel 'general' for reply routing")
	}
	if !strings.Contains(packet, "h1") {
		t.Error("expected packet to include message ID 'h1' as reply_to_id")
	}
	if !strings.Contains(packet, "team_broadcast") {
		t.Error("expected packet to include team_broadcast instruction for routing response")
	}
}

func TestBuildResumePacketReplyInstructionsMentionsSlug(t *testing.T) {
	msgs := []channelMessage{
		{ID: "h2", From: "you", Channel: "engineering", Content: "Can you review this?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	packet := buildResumePacket("be", nil, msgs)

	// Agent slug must appear in the routing instructions so the agent knows my_slug.
	if !strings.Contains(packet, "be") {
		t.Error("expected packet to reference agent slug 'be' in reply instructions")
	}
	if !strings.Contains(packet, "engineering") {
		t.Error("expected packet to include channel 'engineering'")
	}
}

func TestResumeInFlightWorkHeadlessEnqueuesLeadEvenWhenSpecialistsPresent(t *testing.T) {
	// Spec: CEO's resume packet must not be silently dropped by the queue-hold
	// guard when specialists are also receiving resume packets.
	// Fix: enqueue the lead first so its queue entry is set before specialists'
	// queues are populated — the queue-hold check fires only when OTHER slugs
	// have non-empty queues at the time of the CEO enqueue.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	// fe has an in-flight task (specialist).
	b.tasks = []teamTask{
		{ID: "t1", Title: "Build login form", Owner: "fe", Status: "in_progress"},
	}
	// ceo has an unanswered message (lead).
	b.messages = []channelMessage{
		{ID: "h1", From: "you", Content: "what is the strategy?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	l := &Launcher{
		provider: "codex", // headless path
		broker:   b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	l.resumeInFlightWork()

	// Workers start goroutines that drain headlessQueues into headlessActive.
	// Check both queue and active entries to avoid a race where the goroutine
	// has already consumed the queue entry before we read it.
	ceoPresent := func() bool {
		l.headlessMu.Lock()
		defer l.headlessMu.Unlock()
		return len(l.headlessQueues["ceo"]) > 0 || l.headlessActive["ceo"] != nil
	}
	fePresent := func() bool {
		l.headlessMu.Lock()
		defer l.headlessMu.Unlock()
		return len(l.headlessQueues["fe"]) > 0 || l.headlessActive["fe"] != nil
	}

	if !ceoPresent() {
		t.Error("CEO resume packet was dropped by queue-hold guard — lead must be enqueued before specialists")
	}
	if !fePresent() {
		t.Error("fe specialist resume packet was not enqueued")
	}
}

func TestBuildResumePacketsUsesLimit50ForRecentHumanMessages(t *testing.T) {
	// Spec: buildResumePackets() must pass limit=50 to RecentHumanMessages().
	// The constant recentHumanMessageLimit must be 50.
	if recentHumanMessageLimit != 50 {
		t.Errorf("recentHumanMessageLimit = %d, want 50 (per spec)", recentHumanMessageLimit)
	}
}

func TestBuildResumePacketSpecHeader(t *testing.T) {
	// Spec: header must be "[Session resumed — picking up where you left off]"
	tasks := []teamTask{
		{ID: "t1", Title: "Build login", Owner: "fe", Status: "in_progress"},
	}
	packet := buildResumePacket("fe", tasks, nil)

	if !strings.Contains(packet, "[Session resumed — picking up where you left off]") {
		t.Errorf("expected spec header '[Session resumed — picking up where you left off]', got packet:\n%s", packet)
	}
	// Old header must not appear.
	if strings.Contains(packet, "You are @") {
		t.Error("old header 'You are @...' must not appear in spec-format packet")
	}
}

func TestBuildResumePacketSpecSectionTasksLabel(t *testing.T) {
	// Spec: tasks section label must be "Active tasks:" (not "## Your assigned tasks")
	tasks := []teamTask{
		{ID: "t1", Title: "Build login", Owner: "fe", Status: "in_progress"},
	}
	packet := buildResumePacket("fe", tasks, nil)

	if !strings.Contains(packet, "Active tasks:") {
		t.Errorf("expected section label 'Active tasks:', got packet:\n%s", packet)
	}
	if strings.Contains(packet, "## Your assigned tasks") {
		t.Error("old section label '## Your assigned tasks' must not appear")
	}
}

func TestBuildResumePacketSpecSectionMessagesLabel(t *testing.T) {
	// Spec: messages section label must be "Unanswered messages:" (not "## Unanswered messages awaiting your response")
	msgs := []channelMessage{
		{ID: "h1", From: "you", Channel: "general", Content: "What is the plan?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	packet := buildResumePacket("ceo", nil, msgs)

	if !strings.Contains(packet, "Unanswered messages:") {
		t.Errorf("expected section label 'Unanswered messages:', got packet:\n%s", packet)
	}
	if strings.Contains(packet, "## Unanswered messages awaiting your response") {
		t.Error("old section label '## Unanswered messages awaiting your response' must not appear")
	}
}

// TestResumeInFlightWorkTUIClaudeRoutesHeadless pins the invariant that TUI
// mode with claude-code runtime and no pane-backed agents routes resumption
// through the headless queue, not the tmux pane branch. Before this guard,
// resumeInFlightWork branched on webMode alone — TUI has webMode=false, so it
// fell through to agentPaneTargets() which computes pane addresses without
// verifying they exist, and the resulting tmux send-keys commands silently
// failed. Users restarting `wuphf --tui` with in-flight work lost resumption.
func TestResumeInFlightWorkTUIClaudeRoutesHeadless(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	oldWakeLead := headlessWakeLeadFn
	headlessWakeLeadFn = func(_ *Launcher, _ string) {}
	defer func() { headlessWakeLeadFn = oldWakeLead }()

	b := NewBroker()
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Build login form", Owner: "fe", Status: "in_progress"},
	}
	b.messages = []channelMessage{
		{ID: "h1", From: "you", Content: "what is the strategy?", Timestamp: "2026-04-14T10:00:00Z"},
	}
	b.mu.Unlock()

	l := &Launcher{
		// TUI mode: webMode=false, claude-code provider, no panes spawned.
		provider:         "claude-code",
		webMode:          false,
		paneBackedAgents: false,
		broker:           b,
		pack: &agent.PackDefinition{
			Slug:     "founding-team",
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	l.resumeInFlightWork()

	present := func(slug string) bool {
		l.headlessMu.Lock()
		defer l.headlessMu.Unlock()
		return len(l.headlessQueues[slug]) > 0 || l.headlessActive[slug] != nil
	}

	if !present("ceo") {
		t.Error("TUI+claude: CEO resume packet dropped — TUI must route through headless queue when paneBackedAgents=false")
	}
	if !present("fe") {
		t.Error("TUI+claude: fe specialist resume packet dropped — TUI must route through headless queue when paneBackedAgents=false")
	}
}
