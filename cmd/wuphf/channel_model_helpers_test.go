package main

import (
	"strings"
	"testing"
)

func TestFilterInsightMessagesKeepsAutomationAndNex(t *testing.T) {
	messages := []brokerMessage{
		{ID: "m1", From: "ceo", Content: "talk", Kind: "human_decision"},
		{ID: "m2", From: "nex", Content: "policy"},
		{ID: "m3", From: "fe", Content: "automation tick", Kind: "automation"},
		{ID: "m4", From: "fe", Content: "regular"},
	}
	got := filterInsightMessages(messages)
	if len(got) != 2 {
		t.Fatalf("expected 2 insight messages, got %d (%v)", len(got), got)
	}
	for _, m := range got {
		if m.From != "nex" && m.Kind != "automation" {
			t.Errorf("unexpected message %+v", m)
		}
	}
}

func TestPopupActionIndexParses(t *testing.T) {
	if idx, ok := popupActionIndex("3"); !ok || idx != 3 {
		t.Errorf("expected (3,true), got (%d,%v)", idx, ok)
	}
	if _, ok := popupActionIndex("not a number"); ok {
		t.Errorf("expected miss for non-numeric")
	}
	if _, ok := popupActionIndex("-5"); ok {
		t.Errorf("expected miss for negative")
	}
}

func TestCountUniqueAgentsExcludesYouNexAndAutomation(t *testing.T) {
	messages := []brokerMessage{
		{From: "fe"},
		{From: "be"},
		{From: "fe"}, // duplicate, should not double-count
		{From: "you"},
		{From: "nex"},
		{From: "fe", Kind: "automation"},
	}
	if got := countUniqueAgents(messages); got != 2 {
		t.Fatalf("expected 2 unique agents (fe, be), got %d", got)
	}
}

func TestCountUniqueAgentsEmpty(t *testing.T) {
	if got := countUniqueAgents(nil); got != 0 {
		t.Fatalf("expected 0 for empty input, got %d", got)
	}
}

func TestFormatUsdFormatsTwoDecimals(t *testing.T) {
	cases := map[float64]string{
		0:        "$0.00",
		0.5:      "$0.50",
		1.234:    "$1.23",
		1500.999: "$1501.00",
	}
	for in, want := range cases {
		if got := formatUsd(in); got != want {
			t.Errorf("formatUsd(%g) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTokenCountUnits(t *testing.T) {
	cases := map[int]string{
		0:         "0 tok",
		999:       "999 tok",
		1_500:     "1.5k tok",
		2_000_000: "2.0M tok",
	}
	for in, want := range cases {
		if got := formatTokenCount(in); got != want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRecommendedOptionIndexFindsMatch(t *testing.T) {
	m := channelModel{}
	m.pending = &channelInterview{
		Options: []channelInterviewOption{
			{ID: "yes"},
			{ID: "no"},
			{ID: "maybe"},
		},
		RecommendedID: "no",
	}
	if got := m.recommendedOptionIndex(); got != 1 {
		t.Errorf("expected index 1 for 'no', got %d", got)
	}

	m.pending.RecommendedID = "missing"
	if got := m.recommendedOptionIndex(); got != 0 {
		t.Errorf("missing recommendation should fall back to 0, got %d", got)
	}

	m.pending = nil
	if got := m.recommendedOptionIndex(); got != 0 {
		t.Errorf("nil pending should be 0, got %d", got)
	}
}

func TestInterviewOptionCountIncludesCustomAnswerSlot(t *testing.T) {
	m := channelModel{}
	if got := m.interviewOptionCount(); got != 0 {
		t.Errorf("nil pending should yield 0 option count, got %d", got)
	}
	m.pending = &channelInterview{Options: []channelInterviewOption{{ID: "a"}, {ID: "b"}}}
	if got := m.interviewOptionCount(); got != 3 {
		t.Errorf("expected len(options)+1 = 3, got %d", got)
	}
}

func TestSelectedInterviewOptionEdgeCases(t *testing.T) {
	m := channelModel{}
	if m.selectedInterviewOption() != nil {
		t.Fatalf("nil pending should yield nil option")
	}

	m.pending = &channelInterview{Options: []channelInterviewOption{{ID: "a"}, {ID: "b"}}}
	m.selectedOption = -1
	got := m.selectedInterviewOption()
	if got == nil || got.ID != "a" {
		t.Errorf("negative selectedOption should default to first option, got %#v", got)
	}

	m.selectedOption = 1
	got = m.selectedInterviewOption()
	if got == nil || got.ID != "b" {
		t.Errorf("expected second option, got %#v", got)
	}

	m.selectedOption = 99
	if m.selectedInterviewOption() != nil {
		t.Fatalf("out-of-range selectedOption should yield nil (custom slot)")
	}
}

func TestRenderUsageStripBuildsAgentColumn(t *testing.T) {
	usage := channelUsageState{
		Agents: map[string]channelUsageTotals{
			"fe":  {InputTokens: 100, OutputTokens: 50, TotalTokens: 150, CostUsd: 0.5},
			"ceo": {InputTokens: 5, OutputTokens: 1, TotalTokens: 6},
		},
	}
	members := []channelMember{{Slug: "fe", Name: "Frontend"}}
	got := stripANSI(renderUsageStrip(usage, members, 120))
	if got == "" {
		t.Fatalf("expected non-empty usage strip")
	}
	if !strings.Contains(got, "150 tok") {
		t.Fatalf("expected fe token total in strip, got %q", got)
	}
	if !strings.Contains(got, "$0.50") {
		t.Fatalf("expected fe cost in strip, got %q", got)
	}
}

func TestRenderUsageStripEmptyReturnsEmpty(t *testing.T) {
	if got := renderUsageStrip(channelUsageState{}, nil, 120); got != "" {
		t.Fatalf("empty usage should yield empty strip, got %q", got)
	}
	usage := channelUsageState{Agents: map[string]channelUsageTotals{"fe": {TotalTokens: 1}}}
	if got := renderUsageStrip(usage, nil, 30); got != "" {
		t.Fatalf("narrow width should yield empty strip, got %q", got)
	}
}

func TestVisiblePendingRequestNilWhenNoPending(t *testing.T) {
	m := channelModel{}
	if got := m.visiblePendingRequest(); got != nil {
		t.Fatalf("expected nil when no pending request, got %v", got)
	}
}

func TestComposerTargetLabelInDirect(t *testing.T) {
	m := channelModel{}
	m.sessionMode = "1o1"
	m.oneOnOneAgent = "fe"
	got := m.composerTargetLabel()
	if got == "" {
		t.Fatalf("expected non-empty composer target label in 1:1")
	}
}

func TestChannelModelInitReturnsPollCmd(t *testing.T) {
	m := newChannelModel(false)
	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("expected non-nil cmd from Init()")
	}
}

func TestCurrentAppLabelByApp(t *testing.T) {
	cases := map[officeApp]string{
		officeAppMessages:  "messages",
		officeAppRecovery:  "recovery",
		officeAppInbox:     "inbox",
		officeAppOutbox:    "outbox",
		officeAppTasks:     "tasks",
		officeAppRequests:  "requests",
		officeAppPolicies:  "policies",
		officeAppCalendar:  "calendar",
		officeAppArtifacts: "artifacts",
		officeAppSkills:    "skills",
	}
	for app, want := range cases {
		m := channelModel{activeApp: app}
		if got := m.currentAppLabel(); got != want {
			t.Errorf("currentAppLabel(%v) = %q, want %q", app, got, want)
		}
	}
}

func TestCurrentAppLabelOneOnOneOverridesMostApps(t *testing.T) {
	m := channelModel{}
	m.sessionMode = "1o1"
	m.oneOnOneAgent = "fe"
	m.activeApp = officeAppTasks
	if got := m.currentAppLabel(); got != "messages" {
		t.Errorf("1:1 mode should report 'messages' for non-mailbox apps, got %q", got)
	}
	// inbox/outbox/recovery still surface their own labels in 1:1 mode.
	m.activeApp = officeAppInbox
	if got := m.currentAppLabel(); got != "inbox" {
		t.Errorf("1:1 mode should still surface inbox label, got %q", got)
	}
}

func TestNextFocusCyclesAvailableAreas(t *testing.T) {
	m := channelModel{focus: focusMain, threadPanelOpen: false, sidebarCollapsed: false}
	if got := m.nextFocus(); got != focusSidebar {
		t.Errorf("expected main->sidebar, got %v", got)
	}
	m.focus = focusSidebar
	if got := m.nextFocus(); got != focusMain {
		t.Errorf("expected sidebar->main when no thread, got %v", got)
	}

	// With thread open, sidebar -> thread -> main -> sidebar...
	m.threadPanelOpen = true
	m.focus = focusSidebar
	if got := m.nextFocus(); got != focusThread {
		t.Errorf("expected sidebar->thread with thread open, got %v", got)
	}
	m.focus = focusThread
	if got := m.nextFocus(); got != focusMain {
		t.Errorf("expected thread->main, got %v", got)
	}
}

func TestNextFocusSkipsCollapsedSidebar(t *testing.T) {
	m := channelModel{focus: focusMain, sidebarCollapsed: true, threadPanelOpen: false}
	// Only focusMain in the cycle; should stay on main.
	if got := m.nextFocus(); got != focusMain {
		t.Errorf("expected main->main when sidebar collapsed and no thread, got %v", got)
	}
}

func TestLatestHumanFacingMessageScansFromEnd(t *testing.T) {
	messages := []brokerMessage{
		{ID: "1", Kind: "automation"},
		{ID: "2", Kind: "human_decision"},
		{ID: "3", Kind: "human_action"},
		{ID: "4", Kind: "automation"},
	}
	got := latestHumanFacingMessage(messages)
	if got == nil {
		t.Fatalf("expected to find latest human-facing message")
	}
	if got.ID != "3" {
		t.Errorf("expected last human message id=3, got %s", got.ID)
	}

	if got := latestHumanFacingMessage(nil); got != nil {
		t.Fatalf("nil input should yield nil")
	}
	if got := latestHumanFacingMessage([]brokerMessage{{Kind: "automation"}}); got != nil {
		t.Fatalf("no human-facing kinds should yield nil")
	}
}
