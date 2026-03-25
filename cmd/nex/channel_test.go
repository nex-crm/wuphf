package main

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestNewBrokerRequestUsesEnvTokenAtRequestTime(t *testing.T) {
	oldEnv := os.Getenv("WUPHF_BROKER_TOKEN")
	oldPath := brokerTokenPath
	t.Cleanup(func() {
		_ = os.Setenv("WUPHF_BROKER_TOKEN", oldEnv)
		brokerTokenPath = oldPath
	})

	brokerTokenPath = "/tmp/non-existent-nex-broker-token"
	if err := os.Setenv("WUPHF_BROKER_TOKEN", "token-from-env"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	req, err := newBrokerRequest("GET", "http://127.0.0.1:7890/messages", nil)
	if err != nil {
		t.Fatalf("newBrokerRequest: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-from-env" {
		t.Fatalf("expected env auth token, got %q", got)
	}
}

func TestNewBrokerRequestFallsBackToTokenFile(t *testing.T) {
	oldEnv := os.Getenv("WUPHF_BROKER_TOKEN")
	oldPath := brokerTokenPath
	t.Cleanup(func() {
		_ = os.Setenv("WUPHF_BROKER_TOKEN", oldEnv)
		brokerTokenPath = oldPath
	})

	if err := os.Unsetenv("WUPHF_BROKER_TOKEN"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	tokenFile, err := os.CreateTemp(t.TempDir(), "broker-token")
	if err != nil {
		t.Fatalf("create temp token file: %v", err)
	}
	if _, err := tokenFile.WriteString("token-from-file\n"); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if err := tokenFile.Close(); err != nil {
		t.Fatalf("close token file: %v", err)
	}
	brokerTokenPath = tokenFile.Name()

	req, err := newBrokerRequest("GET", "http://127.0.0.1:7890/messages", nil)
	if err != nil {
		t.Fatalf("newBrokerRequest: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-from-file" {
		t.Fatalf("expected file auth token, got %q", got)
	}
}

func TestRenderA2UIBlocksPromptExampleRendersListItems(t *testing.T) {
	content := "```a2ui\n" +
		`{"type":"card","props":{"title":"Sprint Plan"},"children":[{"type":"list","props":{"items":["Build auth","Design UI"]}}]}` +
		"\n```"

	textPart, rendered := renderA2UIBlocks(content, 48)
	if textPart != "" {
		t.Fatalf("expected no plain text, got %q", textPart)
	}
	if !strings.Contains(rendered, "Sprint Plan") {
		t.Fatalf("expected rendered card title, got %q", rendered)
	}
	if !strings.Contains(rendered, "Build auth") || !strings.Contains(rendered, "Design UI") {
		t.Fatalf("expected prompt-example list items to render, got %q", rendered)
	}
}

func TestRenderA2UIBlocksInlineJSONPreservesTrailingText(t *testing.T) {
	content := `Before {"type":"text","props":{"content":"Hello"}} after`

	textPart, rendered := renderA2UIBlocks(content, 40)
	if !strings.Contains(textPart, "Before") || !strings.Contains(textPart, "after") {
		t.Fatalf("expected leading and trailing prose to survive, got %q", textPart)
	}
	if !strings.Contains(rendered, "Hello") {
		t.Fatalf("expected inline A2UI JSON to render, got %q", rendered)
	}
}

func TestRenderA2UIBlocksInvalidNestedSchemaFallsBackToFence(t *testing.T) {
	content := "```a2ui\n" +
		`{"type":"card","children":[{"type":"bogus"}]}` +
		"\n```"

	textPart, rendered := renderA2UIBlocks(content, 40)
	if rendered != "" {
		t.Fatalf("expected invalid schema not to render, got %q", rendered)
	}
	if !strings.Contains(textPart, "```a2ui") || !strings.Contains(textPart, `"type":"bogus"`) {
		t.Fatalf("expected original fenced JSON fallback, got %q", textPart)
	}
}

func TestRenderA2UIBlocksRespectsRequestedWidth(t *testing.T) {
	content := "```a2ui\n" +
		`{"type":"card","props":{"title":"Width Test"},"children":[{"type":"text","props":{"content":"This is a long line that should wrap inside a narrow card instead of rendering at the default width."}}]}` +
		"\n```"

	_, rendered := renderA2UIBlocks(content, 28)
	for _, line := range strings.Split(stripANSI(rendered), "\n") {
		if len([]rune(line)) > 32 {
			t.Fatalf("expected rendered output to fit narrow width, got line %q (%d chars)", line, len([]rune(line)))
		}
	}
}

func TestAppendWrappedWrapsLongLines(t *testing.T) {
	lines := appendWrapped(nil, 12, "this is a long line that should wrap")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output to span multiple lines, got %q", lines)
	}
}

func TestClampScrollCapsToBufferHeight(t *testing.T) {
	if got := clampScroll(10, 4, 99); got != 6 {
		t.Fatalf("expected scroll to clamp to 6, got %d", got)
	}
	if got := clampScroll(3, 10, 5); got != 0 {
		t.Fatalf("expected scroll to clamp to 0 for short buffer, got %d", got)
	}
}

func TestFlattenThreadMessagesNestsRepliesUnderParent(t *testing.T) {
	messages := []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root"},
		{ID: "msg-2", From: "fe", Content: "Reply", ReplyTo: "msg-1"},
		{ID: "msg-3", From: "pm", Content: "Second root"},
		{ID: "msg-4", From: "be", Content: "Nested", ReplyTo: "msg-2"},
	}

	got := flattenThreadMessages(messages, map[string]bool{"msg-1": true, "msg-2": true})
	if len(got) != 4 {
		t.Fatalf("expected 4 flattened messages, got %d", len(got))
	}
	if got[0].Message.ID != "msg-1" || got[0].Depth != 0 {
		t.Fatalf("expected first root msg-1 depth 0, got %#v", got[0])
	}
	if got[1].Message.ID != "msg-2" || got[1].Depth != 1 || got[1].ParentLabel != "@ceo" {
		t.Fatalf("expected msg-2 nested under @ceo, got %#v", got[1])
	}
	if got[2].Message.ID != "msg-4" || got[2].Depth != 2 || got[2].ParentLabel != "@fe" {
		t.Fatalf("expected msg-4 nested under @fe, got %#v", got[2])
	}
	if got[3].Message.ID != "msg-3" || got[3].Depth != 0 {
		t.Fatalf("expected final root msg-3 depth 0, got %#v", got[3])
	}
}

func TestChannelViewShowsThreadReplyLabel(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.expandedThreads["msg-1"] = true
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Should we target founders first?", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "cmo", Content: "Yes, wedge is stronger there.", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "thread reply to @ceo") {
		t.Fatalf("expected threaded reply label in view, got %q", view)
	}
	if !strings.Contains(view, "↳ CMO") {
		t.Fatalf("expected threaded reply header marker, got %q", view)
	}
}

func TestThreadsStartCollapsedByDefault(t *testing.T) {
	m := newChannelModel(true) // explicit collapsed mode
	m.width = 120
	m.height = 30
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "Reply one", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
		{ID: "msg-3", From: "be", Content: "Reply two", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:02:00Z"},
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "↩ 2 replies") {
		t.Fatalf("expected collapsed-thread summary, got %q", view)
	}
	if strings.Contains(view, "Reply one") || strings.Contains(view, "Reply two") {
		t.Fatalf("expected replies to stay hidden by default, got %q", view)
	}
}

func TestCountRepliesCountsNestedDescendants(t *testing.T) {
	messages := []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root"},
		{ID: "msg-2", From: "fe", Content: "Reply", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
		{ID: "msg-3", From: "be", Content: "Nested", ReplyTo: "msg-2", Timestamp: "2026-03-24T10:02:00Z"},
	}

	count, lastReply := countReplies(messages, "msg-1")
	if count != 2 {
		t.Fatalf("expected nested reply count 2, got %d", count)
	}
	if lastReply == "" {
		t.Fatal("expected last reply time for nested reply")
	}
}

func TestRenderThreadPanelShowsNestedReplies(t *testing.T) {
	messages := []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "First reply", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
		{ID: "msg-3", From: "be", Content: "Nested reply", ReplyTo: "msg-2", Timestamp: "2026-03-24T10:02:00Z"},
	}

	view := stripANSI(renderThreadPanel(messages, "msg-1", 44, 18, nil, 0, 0, "", true))
	if !strings.Contains(view, "2 replies") {
		t.Fatalf("expected thread panel to count nested replies, got %q", view)
	}
	if !strings.Contains(view, "Nested reply") {
		t.Fatalf("expected nested reply to render in thread panel, got %q", view)
	}
}

func TestChannelViewUsesOfficeHeaderAndComposer(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30

	view := stripANSI(m.View())
	if !strings.Contains(view, "The WUPHF Office") || !strings.Contains(view, "Message #general") {
		t.Fatalf("expected office chrome, got %q", view)
	}
}

func TestChannelViewRendersNexAutomationMessage(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.messages = []brokerMessage{
		{
			ID:          "msg-1",
			From:        "nex",
			Kind:        "automation",
			Source:      "context_graph",
			SourceLabel: "Nex",
			Title:       "Context alert",
			Content:     "Important: Acme mentioned budget pressure",
			Timestamp:   "2026-03-24T10:00:00Z",
		},
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "Nex") || !strings.Contains(view, "automated") || !strings.Contains(view, "Context alert") {
		t.Fatalf("expected Nex automation rendering, got %q", view)
	}
}

func TestReplyCommandEntersReplyMode(t *testing.T) {
	m := newChannelModel(false)
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic"},
	}
	m.input = []rune("/reply msg-1")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if got.replyToID != "msg-1" {
		t.Fatalf("expected replyToID msg-1, got %q", got.replyToID)
	}
	if !strings.Contains(got.notice, "Replying in thread msg-1") {
		t.Fatalf("expected reply notice, got %q", got.notice)
	}
}

func TestExpandCommandExpandsThread(t *testing.T) {
	m := newChannelModel(false)
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic"},
		{ID: "msg-2", From: "fe", Content: "Reply", ReplyTo: "msg-1"},
	}
	m.input = []rune("/expand msg-1")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if !got.expandedThreads["msg-1"] {
		t.Fatalf("expected msg-1 to be expanded")
	}
}

func TestCancelCommandClearsReplyMode(t *testing.T) {
	m := newChannelModel(false)
	m.replyToID = "msg-1"
	m.input = []rune("/cancel")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if got.replyToID != "" {
		t.Fatalf("expected reply mode to clear, got %q", got.replyToID)
	}
	if !strings.Contains(got.notice, "Reply mode cleared") {
		t.Fatalf("expected cancel notice, got %q", got.notice)
	}
}

func TestInitCommandStartsSetupFlow(t *testing.T) {
	m := newChannelModel(false)
	m.input = []rune("/init")
	m.inputPos = len(m.input)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if cmd == nil {
		t.Fatal("expected /init to emit a follow-up command")
	}
	if got.notice != "Starting setup..." {
		t.Fatalf("expected setup notice, got %q", got.notice)
	}
}

func TestNewChannelModelAutoStartsInitWithoutAPIKey(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "")
	defer os.Setenv("HOME", origHome)

	m := newChannelModel(false)

	if !m.initFlow.IsActive() && m.initFlow.Phase() != "api_key" {
		t.Fatalf("expected init flow to auto-start without API key, got phase %q", m.initFlow.Phase())
	}
	if !strings.Contains(m.notice, "Starting setup") {
		t.Fatalf("expected setup notice, got %q", m.notice)
	}
}

func TestSlashAutocompleteShowsAllCommandsOnSlash(t *testing.T) {
	m := newChannelModel(false)
	m.input = []rune("/")
	m.inputPos = len(m.input)
	m.updateInputOverlays()

	if !m.autocomplete.IsVisible() {
		t.Fatal("expected slash autocomplete to be visible")
	}
	view := stripANSI(m.autocomplete.View())
	if !strings.Contains(view, "/init") || !strings.Contains(view, "/reply") {
		t.Fatalf("expected command list in autocomplete, got %q", view)
	}
}

func TestSlashAutocompleteEnterSubmitsSelectedCommand(t *testing.T) {
	m := newChannelModel(false)
	m.input = []rune("/in")
	m.inputPos = len(m.input)
	m.updateInputOverlays()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if cmd == nil {
		t.Fatal("expected enter to submit the selected slash command")
	}
	if got.notice != "Starting setup..." {
		t.Fatalf("expected /init to run from partial autocomplete, got %q", got.notice)
	}
}

func TestThreadSlashAutocompleteEnterSubmitsSelectedCommand(t *testing.T) {
	m := newChannelModel(false)
	m.threadPanelOpen = true
	m.threadPanelID = "msg-1"
	m.focus = focusThread
	m.threadInput = []rune("/in")
	m.threadInputPos = len(m.threadInput)
	m.updateThreadOverlays()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if cmd == nil {
		t.Fatal("expected enter to submit the selected slash command from thread input")
	}
	if got.notice != "Starting setup..." {
		t.Fatalf("expected /init to run from thread autocomplete, got %q", got.notice)
	}
}

func TestSlashAutocompleteEnterSubmitsQuitCommand(t *testing.T) {
	m := newChannelModel(false)
	m.input = []rune("/qui")
	m.inputPos = len(m.input)
	m.updateInputOverlays()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command to emit tea.Quit")
	}
	if _, ok := next.(channelModel); !ok {
		t.Fatal("expected channel model back from update")
	}
}

func TestMentionAutocompleteFiltersAgents(t *testing.T) {
	m := newChannelModel(false)
	m.members = []channelMember{{Slug: "designer"}, {Slug: "cmo"}}
	m.input = []rune("@de")
	m.inputPos = len(m.input)
	m.updateInputOverlays()

	if !m.mention.IsVisible() {
		t.Fatal("expected mention autocomplete to be visible")
	}
	view := stripANSI(m.mention.View())
	if !strings.Contains(view, "@designer") {
		t.Fatalf("expected filtered mention result, got %q", view)
	}
	if strings.Contains(view, "@cmo") {
		t.Fatalf("expected non-matching mention to be filtered out, got %q", view)
	}
}

func TestIntegrateCommandOpensPicker(t *testing.T) {
	m := newChannelModel(false)
	m.notice = ""
	t.Setenv("WUPHF_API_KEY", "test-key")
	m.input = []rune("/integrate")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if !got.picker.IsActive() {
		t.Fatal("expected integration picker to be active")
	}
	if got.pickerMode != channelPickerIntegrations {
		t.Fatalf("expected integration picker mode, got %q", got.pickerMode)
	}
	if !strings.Contains(got.notice, "Choose an integration") {
		t.Fatalf("expected integration notice, got %q", got.notice)
	}
}

func TestChannelMsgKeepsScrollWhenReadingHistory(t *testing.T) {
	m := newChannelModel(false)
	m.scroll = 3

	next, _ := m.Update(channelMsg{messages: []brokerMessage{{ID: "msg-1", From: "ceo", Content: "new"}}})
	got := next.(channelModel)

	if got.scroll != 4 {
		t.Fatalf("expected scroll to preserve reader position, got %d", got.scroll)
	}
	if got.unreadCount != 1 {
		t.Fatalf("expected unread count to increase, got %d", got.unreadCount)
	}
}

func TestMouseClickJumpLatestClearsUnread(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 32
	m.scroll = 3
	m.unreadCount = 2
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "Reply", Timestamp: "2026-03-24T10:01:00Z"},
		{ID: "msg-3", From: "be", Content: "Reply", Timestamp: "2026-03-24T10:02:00Z"},
		{ID: "msg-4", From: "pm", Content: "Reply", Timestamp: "2026-03-24T10:03:00Z"},
		{ID: "msg-5", From: "cmo", Content: "Reply", Timestamp: "2026-03-24T10:04:00Z"},
	}

	layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
	mainX := layout.SidebarW + 1
	headerH, _, _ := m.mainPanelGeometry(layout.MainW, layout.ContentH)

	next, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, X: mainX + 4, Y: headerH})
	got := next.(channelModel)
	if got.scroll != 0 || got.unreadCount != 0 {
		t.Fatalf("expected jump latest to clear unread and scroll, got scroll=%d unread=%d", got.scroll, got.unreadCount)
	}
}

func TestMouseClickCollapsedThreadOpensThreadPanel(t *testing.T) {
	m := newChannelModel(true)
	m.width = 120
	m.height = 32
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "Reply one", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
	}

	layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
	headerH, msgH, _ := m.mainPanelGeometry(layout.MainW, layout.ContentH)
	contentWidth := layout.MainW - 2
	lines := buildOfficeMessageLines(m.messages, m.expandedThreads, contentWidth, m.threadsDefaultExpand)
	visible, _, _, _ := sliceRenderedLines(lines, msgH, m.scroll)
	row := -1
	for i, line := range visible {
		if line.ThreadID == "msg-1" {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("expected collapsed thread summary row")
	}

	next, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, X: layout.SidebarW + 5, Y: headerH + row})
	got := next.(channelModel)
	if !got.threadPanelOpen || got.threadPanelID != "msg-1" {
		t.Fatalf("expected click to open thread panel for msg-1, got open=%v id=%q", got.threadPanelOpen, got.threadPanelID)
	}
}

func TestChannelErrorsSurfaceInNotice(t *testing.T) {
	m := newChannelModel(false)
	errBoom := errors.New("boom")

	next, _ := m.Update(channelResetDoneMsg{err: errBoom})
	got := next.(channelModel)
	if !strings.Contains(got.notice, "Reset failed") {
		t.Fatalf("expected reset error notice, got %q", got.notice)
	}

	next, _ = got.Update(channelPostDoneMsg{err: errBoom})
	got = next.(channelModel)
	if !strings.Contains(got.notice, "Send failed") {
		t.Fatalf("expected post error notice, got %q", got.notice)
	}
}

func TestChannelViewShowsMessageIDInMeta(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.messages = []brokerMessage{
		{ID: "msg-12", From: "ceo", Content: "We should choose a sharper wedge.", Timestamp: "2026-03-24T10:00:00Z"},
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "msg-12") {
		t.Fatalf("expected message ID to be visible in view, got %q", view)
	}
}

func TestChannelViewShowsUsageTotals(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.usage = channelUsageState{
		Total: channelUsageTotals{TotalTokens: 12500, CostUsd: 1.23},
		Agents: map[string]channelUsageTotals{
			"ceo": {TotalTokens: 5000, CostUsd: 0.62},
			"fe":  {TotalTokens: 7500, CostUsd: 0.61},
		},
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "Spend to date $1.23") {
		t.Fatalf("expected overall spend summary, got %q", view)
	}
	if !strings.Contains(view, "CEO 5.0k tok · $0.62") {
		t.Fatalf("expected per-agent usage pill, got %q", view)
	}
	if !strings.Contains(view, "Frontend Engineer 7.5k tok · $0.61") {
		t.Fatalf("expected frontend usage pill, got %q", view)
	}
}

func TestRenderInterviewCardShowsCustomAnswerAsFinalOption(t *testing.T) {
	card := renderInterviewCard(channelInterview{
		From:     "ceo",
		Question: "What should we optimize for?",
		Options: []channelInterviewOption{
			{ID: "speed", Label: "Ship fast", Description: "Bias toward launch speed."},
			{ID: "quality", Label: "Higher polish", Description: "Bias toward experience quality."},
		},
		RecommendedID: "speed",
	}, 2, 60)

	plain := stripANSI(card)
	if !strings.Contains(plain, "Something else") {
		t.Fatalf("expected custom answer option in card, got %q", plain)
	}
	if strings.LastIndex(plain, "Something else") <= strings.LastIndex(plain, "Higher polish") {
		t.Fatalf("expected Something else to appear after predefined options, got %q", plain)
	}
}

func TestEscSnoozesPendingInterviewWithoutAnswering(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.pending = &channelInterview{
		ID:       "interview-1",
		From:     "ceo",
		Question: "Which segment should we prioritize?",
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(channelModel)

	if got.snoozedInterview != "interview-1" {
		t.Fatalf("expected interview to be snoozed, got %q", got.snoozedInterview)
	}
	if !strings.Contains(got.notice, "Interview snoozed") {
		t.Fatalf("expected snooze notice, got %q", got.notice)
	}
	view := stripANSI(got.View())
	if strings.Contains(view, "Human Interview") {
		t.Fatalf("expected interview card to be hidden after snooze, got %q", view)
	}
	if !strings.Contains(view, "Interview paused") {
		t.Fatalf("expected paused status bar after snooze, got %q", view)
	}
}

func TestNewInterviewUnsnoozesCard(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.snoozedInterview = "interview-1"
	m.pending = &channelInterview{
		ID:       "interview-1",
		From:     "ceo",
		Question: "Old question",
	}

	next, _ := m.Update(channelInterviewMsg{pending: &channelInterview{
		ID:       "interview-2",
		From:     "pm",
		Question: "New question",
	}})
	got := next.(channelModel)

	if got.snoozedInterview != "" {
		t.Fatalf("expected new interview to clear snoozed state, got %q", got.snoozedInterview)
	}
	view := stripANSI(got.View())
	if !strings.Contains(view, "Human Interview") {
		t.Fatalf("expected new interview card to be visible, got %q", view)
	}
}
