package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/orchestration"
)

func stripStreamANSI(s string) string {
	return ansi.Strip(s)
}

func newTestStreamModel() StreamModel {
	agentSvc := agent.NewAgentService()
	msgRouter := orchestration.NewMessageRouter()
	events := make(chan tea.Msg, 256)
	delegator := orchestration.NewDelegator(3)

	rt := &Runtime{
		AgentService:  agentSvc,
		MessageRouter: msgRouter,
		Delegator:     delegator,
		TeamLeadSlug:  "team-lead",
		PackSlug:      "founding-team",
		Events:        events,
	}

	m := NewStreamModel(rt, events)
	m.width = 120
	m.height = 40
	m.statusBar.Width = 120
	return m
}

func newTestStreamModelWithQueues() (StreamModel, *agent.MessageQueues) {
	queues := agent.NewMessageQueues()
	agentSvc := agent.NewAgentService(agent.WithQueues(queues))
	msgRouter := orchestration.NewMessageRouter()
	events := make(chan tea.Msg, 256)
	delegator := orchestration.NewDelegator(3)

	rt := &Runtime{
		AgentService:  agentSvc,
		MessageRouter: msgRouter,
		Delegator:     delegator,
		TeamLeadSlug:  "team-lead",
		PackSlug:      "founding-team",
		Events:        events,
	}

	m := NewStreamModel(rt, events)
	m.width = 120
	m.height = 40
	m.statusBar.Width = 120
	return m, queues
}

func waitForQueuedOrStarted(t *testing.T, m StreamModel, queues *agent.MessageQueues, slug string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if queues.HasMessages(slug) {
			return
		}
		if ma, ok := m.runtime.AgentService.Get(slug); ok {
			if ma.Loop.GetState().Phase != agent.PhaseIdle {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	phase := agent.PhaseIdle
	if ma, ok := m.runtime.AgentService.Get(slug); ok {
		phase = ma.Loop.GetState().Phase
	}
	t.Fatalf("expected %s to have queued work or start processing; phase=%s", slug, phase)
}

// --- Slash command tests ---

func TestSlashHelp(t *testing.T) {
	m := newTestStreamModel()
	m.inputValue = []rune("/help")
	m.inputPos = 5

	m2, cmd := m.handleSubmit()
	if cmd != nil {
		t.Fatal("expected no cmd for /help")
	}

	found := false
	for _, msg := range m2.messages {
		if msg.Role == "system" && (strings.Contains(msg.Content, "Commands:") || strings.Contains(msg.Content, "Available commands")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected help output in messages")
	}
}

func TestSlashClear(t *testing.T) {
	m := newTestStreamModel()
	m.messages = append(m.messages, StreamMessage{
		Role: "user", Content: "hello", Timestamp: time.Now(),
	})
	m.inputValue = []rune("/clear")
	m.inputPos = 6

	m2, _ := m.handleSubmit()

	if len(m2.messages) != 1 {
		t.Fatalf("expected 1 message after clear, got %d", len(m2.messages))
	}
	if !strings.Contains(m2.messages[0].Content, "cleared") {
		t.Fatal("expected 'cleared' in message")
	}
}

func TestSlashQuit(t *testing.T) {
	m := newTestStreamModel()
	m.inputValue = []rune("/quit")
	m.inputPos = 5

	_, cmd := m.handleSubmit()
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestSlashQ(t *testing.T) {
	m := newTestStreamModel()
	m.inputValue = []rune("/q")
	m.inputPos = 2

	_, cmd := m.handleSubmit()
	if cmd == nil {
		t.Fatal("expected quit command for /q")
	}
}

func TestSlashUnknown(t *testing.T) {
	m := newTestStreamModel()
	m.inputValue = []rune("/foobar")
	m.inputPos = 7

	m2, _ := m.handleSubmit()

	found := false
	for _, msg := range m2.messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "Unknown command: /foobar") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected unknown command message")
	}
}

func TestSlashResetClearsClaudeSessionPersistence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionPath := filepath.Join(home, ".wuphf", "providers", "claude-sessions.json")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(sessionPath, []byte("{\"ceo\":{\"session_id\":\"sess-123\",\"cwd\":\"/tmp/test\"}}\n"), 0o600); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	m := newTestStreamModel()
	m.inputValue = []rune("/reset")
	m.inputPos = 6

	m2, cmd := m.handleSubmit()
	if cmd != nil {
		t.Fatal("expected no cmd for /reset")
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("expected session file removed, got err=%v", err)
	}
	found := false
	for _, msg := range m2.messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "Claude session persistence reset") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected reset confirmation message")
	}
}

func TestSlashThinkingTogglesVisibility(t *testing.T) {
	m := newTestStreamModel()
	m.messages = append(m.messages, StreamMessage{
		Role:      "thinking",
		AgentSlug: "ceo",
		AgentName: "CEO",
		Content:   "hidden reasoning",
		Timestamp: time.Now(),
	})

	collapsed := m.renderMessages(100, 10)
	if strings.Contains(collapsed, "hidden reasoning") {
		t.Fatal("expected thinking to be collapsed by default")
	}
	if !strings.Contains(collapsed, "thinking update(s) hidden") {
		t.Fatal("expected collapsed thinking summary")
	}

	m.inputValue = []rune("/thinking")
	m.inputPos = len(m.inputValue)
	m2, _ := m.handleSubmit()
	if !m2.showThinking {
		t.Fatal("expected /thinking to expand thinking messages")
	}

	expanded := m2.renderMessages(100, 10)
	if !strings.Contains(expanded, "hidden reasoning") {
		t.Fatal("expected thinking content when expanded")
	}
}

func TestRenderMessageKeepsShortToolResultsExpanded(t *testing.T) {
	m := newTestStreamModel()
	msg := StreamMessage{
		Role:      "tool_result",
		AgentSlug: "fe",
		AgentName: "Frontend Engineer",
		Content:   "ok",
	}

	rendered := stripStreamANSI(m.renderMessage(msg, 100))
	if !strings.Contains(rendered, "ok") {
		t.Fatalf("expected short tool output to remain visible, got %q", rendered)
	}
	if strings.Contains(rendered, "[folded output:") {
		t.Fatalf("expected short tool output not to fold, got %q", rendered)
	}
}

func TestRenderMessageFoldsLargeToolResults(t *testing.T) {
	m := newTestStreamModel()
	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines, strings.Repeat("line", 12)+strings.Repeat(string(rune('a'+i)), 8))
	}
	content := strings.Join(lines, "\n")
	msg := StreamMessage{
		Role:      "tool_result",
		AgentSlug: "fe",
		AgentName: "Frontend Engineer",
		Content:   content,
	}

	rendered := stripStreamANSI(m.renderMessage(msg, 100))
	if !strings.Contains(rendered, "[folded output: 12 lines") {
		t.Fatalf("expected folded summary, got %q", rendered)
	}
	if !strings.Contains(rendered, lines[0]) {
		t.Fatalf("expected first line in folded summary, got %q", rendered)
	}
	if !strings.Contains(rendered, lines[len(lines)-1]) {
		t.Fatalf("expected last line in folded summary, got %q", rendered)
	}
	if msg.Content != content {
		t.Fatal("expected folding to leave original tool content unchanged")
	}
}

func TestInsertModeAllowsMultiCharacterPaste(t *testing.T) {
	m := newTestStreamModel()

	m2, _ := m.updateInsertMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello world")})
	if got := string(m2.inputValue); got != "hello world" {
		t.Fatalf("expected pasted text preserved, got %q", got)
	}
	if m2.inputPos != len([]rune("hello world")) {
		t.Fatalf("expected cursor at end of pasted text, got %d", m2.inputPos)
	}
}

func TestInsertModeIgnoresControlKeyStrings(t *testing.T) {
	m := newTestStreamModel()

	m2, _ := m.updateInsertMode(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if got := string(m2.inputValue); got != "" {
		t.Fatalf("expected ctrl+j not to be inserted, got %q", got)
	}
}

func TestInsertModeAllowsSpaceKey(t *testing.T) {
	m := newTestStreamModel()

	m2, _ := m.updateInsertMode(tea.KeyMsg{Type: tea.KeySpace})
	if got := string(m2.inputValue); got != " " {
		t.Fatalf("expected space to be inserted, got %q", got)
	}
}

func TestInsertModeSanitizesPastedNewlines(t *testing.T) {
	m := newTestStreamModel()

	m2, _ := m.updateInsertMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello\nworld\r\nagain")})
	if got := string(m2.inputValue); got != "hello world again" {
		t.Fatalf("expected pasted newlines normalized, got %q", got)
	}
}

func TestEnsureSpinnerTickRestartsWhenLoadingBegins(t *testing.T) {
	m := newTestStreamModel()
	m.spinnerTicking = false
	m.spinner.SetActive(true)

	cmd := m.ensureSpinnerTick()
	if cmd == nil {
		t.Fatal("expected spinner tick to restart when loading begins")
	}
	if !m.spinnerTicking {
		t.Fatal("expected spinnerTicking to be marked true")
	}
}

// --- Submit routing test ---

func TestSubmitRoutesToAgent(t *testing.T) {
	m := newTestStreamModel()

	// Create and register team-lead
	_, _ = m.runtime.AgentService.CreateFromTemplate("team-lead", "team-lead")
	_ = m.runtime.AgentService.Start("team-lead")
	if tmpl, ok := m.runtime.AgentService.GetTemplate("team-lead"); ok {
		m.runtime.MessageRouter.RegisterAgent("team-lead", tmpl.Expertise)
	}

	m.inputValue = []rune("hello world")
	m.inputPos = 11

	m2, _ := m.handleSubmit()

	// Should have user message
	found := false
	for _, msg := range m2.messages {
		if msg.Role == "user" && msg.Content == "hello world" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected user message 'hello world' in stream")
	}

	if !m2.loading {
		t.Fatal("expected loading to be true after submit")
	}
}

func TestSubmitStartsPrimaryAgentWorkImmediately(t *testing.T) {
	m, queues := newTestStreamModelWithQueues()

	_, _ = m.runtime.AgentService.CreateFromTemplate("team-lead", "team-lead")
	_ = m.runtime.AgentService.Start("team-lead")
	if tmpl, ok := m.runtime.AgentService.GetTemplate("team-lead"); ok {
		m.runtime.MessageRouter.RegisterAgent("team-lead", tmpl.Expertise)
	}

	m.inputValue = []rune("hello world")
	m.inputPos = len(m.inputValue)

	_, _ = m.handleSubmit()

	waitForQueuedOrStarted(t, m, queues, "team-lead")
	if queues.HasSteer("team-lead") {
		t.Fatal("did not expect primary agent steer to be queued for user submit")
	}
}

func TestSubmitImmediatelyKicksOffCollaboratorsForBroadDirective(t *testing.T) {
	m, queues := newTestStreamModelWithQueues()
	m.runtime.TeamLeadSlug = "ceo"
	m.runtime.MessageRouter.SetTeamLeadSlug("ceo")

	for _, cfg := range []agent.AgentConfig{
		{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy", "delegation"}},
		{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React", "CSS", "UI-UX", "components"}},
		{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs", "databases"}},
		{Slug: "cmo", Name: "CMO", Expertise: []string{"positioning", "messaging", "go-to-market"}},
	} {
		_, _ = m.runtime.AgentService.Create(cfg)
		_ = m.runtime.AgentService.Start(cfg.Slug)
		m.runtime.MessageRouter.RegisterAgent(cfg.Slug, cfg.Expertise)
	}

	m.inputValue = []rune("Build a new landing page, backend API, and positioning brief for Nex.")
	m.inputPos = len(m.inputValue)

	_, _ = m.handleSubmit()

	waitForQueuedOrStarted(t, m, queues, "ceo")
	waitForQueuedOrStarted(t, m, queues, "fe")
	waitForQueuedOrStarted(t, m, queues, "be")
}

func TestEmptySubmitDoesNothing(t *testing.T) {
	m := newTestStreamModel()
	m.inputValue = nil
	m.inputPos = 0

	initialCount := len(m.messages)
	m2, cmd := m.handleSubmit()

	if cmd != nil {
		t.Fatal("expected no cmd for empty submit")
	}
	if len(m2.messages) != initialCount {
		t.Fatal("expected no new messages for empty submit")
	}
}

// --- Message rendering tests ---

func TestViewContainsUserMessage(t *testing.T) {
	m := newTestStreamModel()
	m.messages = []StreamMessage{
		{Role: "user", Content: "hello", Timestamp: time.Now()},
	}

	view := m.View()
	if !strings.Contains(view, "You:") {
		t.Error("expected 'You:' in view")
	}
	if !strings.Contains(view, "hello") {
		t.Error("expected 'hello' in view")
	}
}

func TestViewContainsAgentMessage(t *testing.T) {
	m := newTestStreamModel()
	m.messages = []StreamMessage{
		{Role: "agent", AgentSlug: "team-lead", AgentName: "Team Lead", Content: "hi there", Timestamp: time.Now()},
	}

	view := m.View()
	if !strings.Contains(view, "Team Lead:") {
		t.Error("expected 'Team Lead:' in view")
	}
	if !strings.Contains(view, "hi there") {
		t.Error("expected 'hi there' in view")
	}
}

func TestViewContainsSystemMessage(t *testing.T) {
	m := newTestStreamModel()
	m.messages = []StreamMessage{
		{Role: "system", Content: "system msg", Timestamp: time.Now()},
	}

	view := m.View()
	if !strings.Contains(view, "system msg") {
		t.Error("expected 'system msg' in view")
	}
}

func TestViewShowsTitle(t *testing.T) {
	m := newTestStreamModel()
	view := m.View()
	if !strings.Contains(view, "wuphf v0.1.0") {
		t.Error("expected title 'wuphf v0.1.0' in view")
	}
}

func TestViewShowsRoster(t *testing.T) {
	m := newTestStreamModel()
	view := m.View()
	if !strings.Contains(view, "TEAM") {
		t.Error("expected 'TEAM' roster header in view")
	}
}

// --- Mode tests ---

func TestInitialModeIsInsert(t *testing.T) {
	m := newTestStreamModel()
	if m.mode != "insert" {
		t.Fatalf("expected initial mode 'insert', got %q", m.mode)
	}
	if m.statusBar.Mode != "INSERT" {
		t.Fatalf("expected status bar mode 'INSERT', got %q", m.statusBar.Mode)
	}
}

func TestWelcomeMessage(t *testing.T) {
	m := newTestStreamModel()
	if len(m.messages) == 0 {
		t.Fatal("expected welcome message")
	}
	if !strings.Contains(m.messages[0].Content, "No active agents") && !strings.Contains(m.messages[0].Content, "ready with") {
		t.Fatal("expected runtime summary welcome message content")
	}
}

func TestSlashAgentsShowsRuntimeRoster(t *testing.T) {
	m := newTestStreamModel()
	for _, cfg := range []agent.AgentConfig{
		{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy"}},
		{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend"}},
	} {
		_, _ = m.runtime.AgentService.Create(cfg)
		_ = m.runtime.AgentService.Start(cfg.Slug)
	}
	m.runtime.TeamLeadSlug = "ceo"
	m.runtime.PackSlug = "founding-team"

	m.inputValue = []rune("/agents")
	m.inputPos = len(m.inputValue)

	m2, _ := m.handleSubmit()

	last := m2.messages[len(m2.messages)-1].Content
	if !strings.Contains(last, "Founding Team roster:") {
		t.Fatal("expected /agents output to include pack roster heading")
	}
	if !strings.Contains(last, "@ceo CEO [lead]") {
		t.Fatal("expected /agents output to include lead entry")
	}
	if !strings.Contains(last, "@fe Frontend Engineer [specialist]") {
		t.Fatal("expected /agents output to include specialist entry")
	}
}

// --- Agent event tests ---

func TestAgentTextMsgUpdatesStreaming(t *testing.T) {
	m := newTestStreamModel()
	m2, _ := m.Update(AgentTextMsg{AgentSlug: "test-agent", Text: "hello "})

	if m2.streaming["test-agent"] != "hello " {
		t.Fatalf("expected streaming text 'hello ', got %q", m2.streaming["test-agent"])
	}

	m3, _ := m2.Update(AgentTextMsg{AgentSlug: "test-agent", Text: "world"})
	if m3.streaming["test-agent"] != "hello world" {
		t.Fatalf("expected streaming text 'hello world', got %q", m3.streaming["test-agent"])
	}
}

func TestPhaseChangeAddsVisibleProgressMessage(t *testing.T) {
	m := newTestStreamModel()
	_, _ = m.runtime.AgentService.Create(agent.AgentConfig{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend"}})

	m2, _ := m.Update(PhaseChangeMsg{AgentSlug: "fe", From: "idle", To: "build_context"})

	found := false
	for _, msg := range m2.messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "Frontend Engineer is preparing context") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected phase change to add visible progress message")
	}
}

func TestProgressPulseTextIncludesTaskSummary(t *testing.T) {
	got := progressPulseText("Frontend Engineer", false, agent.PhaseStreamLLM, "Design the CRM landing page hero and pricing section.")
	if !strings.Contains(got, "Frontend Engineer is still working on") {
		t.Fatal("expected progress pulse to describe ongoing work")
	}
	if !strings.Contains(got, "Design the CRM landing page hero") {
		t.Fatal("expected progress pulse to include summarized task")
	}
}

func TestSpinnerLabelSummarizesActiveWork(t *testing.T) {
	m := newTestStreamModel()
	for _, cfg := range []agent.AgentConfig{
		{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy"}},
		{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend"}},
	} {
		_, _ = m.runtime.AgentService.Create(cfg)
		_ = m.runtime.AgentService.Start(cfg.Slug)
	}
	m.runtime.TeamLeadSlug = "ceo"
	if ma, ok := m.runtime.AgentService.Get("ceo"); ok {
		ma.State.Phase = agent.PhaseStreamLLM
	}
	if ma, ok := m.runtime.AgentService.Get("fe"); ok {
		ma.State.Phase = agent.PhaseBuildContext
	}

	m.updateSpinnerLabel()

	if !strings.Contains(m.spinner.label, "CEO coordinating") {
		t.Fatal("expected spinner label to include CEO coordinating")
	}
	if !strings.Contains(m.spinner.label, "Frontend Engineer preparing") {
		t.Fatal("expected spinner label to include Frontend Engineer preparing")
	}
}

func TestAgentDoneMsgFinalizesMessage(t *testing.T) {
	m := newTestStreamModel()
	m.streaming["test-agent"] = "final text"

	// Need the agent in service for name lookup — create one
	_, _ = m.runtime.AgentService.CreateFromTemplate("team-lead", "team-lead")

	m2, _ := m.Update(AgentDoneMsg{AgentSlug: "test-agent"})

	// Streaming should be cleared
	if _, ok := m2.streaming["test-agent"]; ok {
		t.Fatal("expected streaming to be cleared after done")
	}

	// Should have finalized message
	found := false
	for _, msg := range m2.messages {
		if msg.Role == "agent" && msg.Content == "final text" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected finalized agent message")
	}
}

func TestStartDelegationsQueuesSteerAndFollowUp(t *testing.T) {
	m, queues := newTestStreamModelWithQueues()
	_, _ = m.runtime.AgentService.Create(agent.AgentConfig{
		Slug:      "fe",
		Name:      "Frontend Engineer",
		Expertise: []string{"frontend"},
	})
	_ = m.runtime.AgentService.Start("fe")

	m.startDelegations([]orchestration.Delegation{{
		AgentSlug: "fe",
		Task:      "@fe tighten the hero spacing.",
	}})

	waitForQueuedOrStarted(t, m, queues, "fe")
}

func TestTeamLeadDoneFallsBackToRoutingHints(t *testing.T) {
	m, queues := newTestStreamModelWithQueues()
	m.runtime.TeamLeadSlug = "team-lead"

	_, _ = m.runtime.AgentService.Create(agent.AgentConfig{Slug: "team-lead", Name: "Team Lead", Expertise: []string{"strategy"}})
	_, _ = m.runtime.AgentService.Create(agent.AgentConfig{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend"}})
	_ = m.runtime.AgentService.Start("team-lead")
	_ = m.runtime.AgentService.Start("fe")

	m.streaming["team-lead"] = "I'll coordinate this."
	m.pendingLeadTask = "Build the landing page."
	m.pendingLeadHints = []string{"fe"}

	m2, _ := m.Update(AgentDoneMsg{AgentSlug: "team-lead"})

	waitForQueuedOrStarted(t, m2, queues, "fe")
	if m2.pendingLeadTask != "" || len(m2.pendingLeadHints) != 0 {
		t.Fatal("expected pending lead hints to be cleared after fallback delegation")
	}
}

func TestSanitizeAgentOutputRemovesDebugMetadata(t *testing.T) {
	raw := "Here is the answer.\n\nsession_id: abc123\nmetadata: model: chat_agent"
	got := sanitizeAgentOutput(raw)
	if got != "Here is the answer." {
		t.Fatalf("expected debug metadata to be stripped, got %q", got)
	}
}

func TestDisplayAgentTextKeepsFullSanitizedContent(t *testing.T) {
	m := newTestStreamModel()
	raw := "Plan intro.\n\n@fe build the landing page hero.\n@be define the CRM API.\nExtra paragraph should remain."
	got := m.displayAgentText("ceo", raw)
	if !strings.Contains(got, "Plan intro.") {
		t.Fatal("expected first paragraph to remain")
	}
	if !strings.Contains(got, "@fe build the landing page hero.") {
		t.Fatal("expected FE delegation to remain")
	}
	if !strings.Contains(got, "@be define the CRM API.") {
		t.Fatal("expected BE delegation to remain")
	}
	if !strings.Contains(got, "Extra paragraph should remain.") {
		t.Fatal("expected later content to remain")
	}
}
