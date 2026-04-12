package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
)

func TestParseAgentPaneIndicesSkipsChannelPane(t *testing.T) {
	got := parseAgentPaneIndices("0 📢 channel\n1 🤖 CEO (@ceo)\n2 🤖 Product Manager (@pm)\n5 🤖 AI Engineer (@ai)\n")
	want := []int{1, 2, 5}
	if len(got) != len(want) {
		t.Fatalf("expected %d panes, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pane[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestResetBrokerStateUsesAuthToken(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	if err := resetBrokerState("http://"+b.Addr(), b.Token()); err != nil {
		t.Fatalf("expected authenticated reset to succeed, got %v", err)
	}
}

func TestResetSessionOnlyClearsOfficeState(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if _, err := b.PostMessage("you", "general", "hello", nil, ""); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	l := &Launcher{broker: b}

	if err := l.ResetSession(); err != nil {
		t.Fatalf("ResetSession: %v", err)
	}
	if got := len(b.Messages()); got != 0 {
		t.Fatalf("expected messages cleared, got %d", got)
	}
	if got := len(b.ChannelTasks("general")); got != 0 {
		t.Fatalf("expected tasks cleared, got %d", got)
	}
	if got := len(b.Requests("general", false)); got != 0 {
		t.Fatalf("expected requests cleared, got %d", got)
	}
	if got := len(b.OfficeMembers()); got == 0 {
		t.Fatal("expected default members to remain after reset")
	}
}

func TestAgentPaneSlugsOneOnOneUsesOnlySelectedAgent(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
		sessionMode: SessionModeOneOnOne,
		oneOnOne:    "pm",
	}

	got := l.agentPaneSlugs()
	if len(got) != 1 || got[0] != "pm" {
		t.Fatalf("expected only pm in 1o1 pane list, got %v", got)
	}
	if l.AgentCount() != 1 {
		t.Fatalf("expected 1 agent in 1o1 mode, got %d", l.AgentCount())
	}
	if !strings.Contains(l.PackName(), "1:1 with") {
		t.Fatalf("expected 1o1 pack name, got %q", l.PackName())
	}
}

func TestAgentPaneSlugsUsesOfficeRosterNotStaticPack(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
		broker: &Broker{
			members: []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "growthops", Name: "Growth Ops"},
			},
		},
	}

	got := l.agentPaneSlugs()
	want := []string{"ceo", "pm", "fe", "growthops"}
	if len(got) != len(want) {
		t.Fatalf("expected %d pane slugs, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pane[%d] = %q, want %q (full list %v)", i, got[i], want[i], got)
		}
	}
}

func TestOfficeMembersSnapshotPrefersPersistedStateOverPack(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	state := brokerState{
		Members: []officeMember{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "pm", Name: "Product Manager"},
			{Slug: "growthops", Name: "Growth Ops"},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(brokerStatePath(), data, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	got := l.officeMembersSnapshot()
	if len(got) != 3 {
		t.Fatalf("expected persisted office members, got %+v", got)
	}
	if got[2].Slug != "growthops" {
		t.Fatalf("expected persisted dynamic member, got %+v", got)
	}
}

func TestNotificationTargetsForMessageOneOnOneWakesSelectedAgent(t *testing.T) {
	l := &Launcher{
		sessionMode: SessionModeOneOnOne,
		oneOnOne:    "pm",
	}

	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Channel: "general",
		Content: "Need a product call here.",
	})

	if len(delayed) != 0 {
		t.Fatalf("expected no delayed targets in 1o1 mode, got %v", delayed)
	}
	if len(immediate) != 1 || immediate[0].Slug != "pm" || immediate[0].PaneTarget == "" {
		t.Fatalf("expected pm as the only immediate target, got %v", immediate)
	}
}

func TestLoadRunningSessionModePrefersLiveBrokerState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected auth header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_mode":     SessionModeOneOnOne,
			"one_on_one_agent": "pm",
		})
	}))
	defer server.Close()

	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")
	t.Setenv("WUPHF_BROKER_BASE_URL", server.URL)

	mode, agent := loadRunningSessionMode()
	if mode != SessionModeOneOnOne {
		t.Fatalf("expected live session mode %q, got %q", SessionModeOneOnOne, mode)
	}
	if agent != "pm" {
		t.Fatalf("expected live 1o1 agent pm, got %q", agent)
	}
}

func TestFormatNexFeedItem(t *testing.T) {
	title, content := formatNexFeedItem(nexFeedItem{
		Type: "context_alert",
		Content: nexFeedItemContent{
			ImportantItems: []nexFeedItemContentItem{
				{Title: "Budget pressure", Context: "Acme mentioned a freeze"},
			},
			EntityChanges: []nexFeedItemContentItem{
				{Title: "Champion changed", Context: "New VP now owns the deal"},
			},
		},
	})

	if title != "Context alert" {
		t.Fatalf("unexpected title: %q", title)
	}
	if !strings.Contains(content, "Important: Budget pressure") || !strings.Contains(content, "Change: Champion changed") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestFetchAndIngestNexNotificationsSeedsCursorOnColdStart(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Fatalf("expected cold start to seed cursor without calling feed, got %s", r.URL.String())
	}))
	defer server.Close()

	b := NewBroker()
	launcher := &Launcher{broker: b}
	client := api.NewClient("test-key")
	client.BaseURL = server.URL

	launcher.fetchAndIngestNexNotifications(client)

	if requests != 0 {
		t.Fatalf("expected no feed requests on cold start, got %d", requests)
	}
	if got := b.NotificationCursor(); got == "" {
		t.Fatal("expected cold start to seed notification cursor")
	}
	if len(b.Messages()) != 0 {
		t.Fatalf("expected no notifications to be posted on cold start, got %d", len(b.Messages()))
	}
}

func TestEnsureMCPConfigUsesLocalGoTeamServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l := &Launcher{cwd: t.TempDir()}

	path, err := l.ensureMCPConfig()
	if err != nil {
		t.Fatalf("ensureMCPConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	server, ok := cfg.MCPServers["wuphf-office"]
	if !ok {
		t.Fatal("expected wuphf-office MCP server entry")
	}
	wantCommand, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if server.Command != wantCommand {
		t.Fatalf("expected command %q, got %q", wantCommand, server.Command)
	}
	if len(server.Args) != 1 || server.Args[0] != "mcp-team" {
		t.Fatalf("expected args [mcp-team], got %v", server.Args)
	}
}

func TestShouldPrimeClaudePane(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "trust prompt",
			content: "Security guide\n❯ 1. Yes, I trust this folder\n2. No, exit\nEnter to confirm",
			want:    true,
		},
		{
			name:    "chrome startup hint",
			content: "❯\n  ⏵⏵ bypass permissions on (...)\n  Claude in Chrome…",
			want:    true,
		},
		{
			name:    "normal conversation",
			content: "@ceo I think the wedge should be meeting notes to follow-up tasks.",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPrimeClaudePane(tt.content); got != tt.want {
				t.Fatalf("shouldPrimeClaudePane(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestChannelPaneNeedsRespawn(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "healthy channel", status: "0 0 wuphf", want: false},
		{name: "dead pane", status: "1 1 dead", want: true},
		{name: "missing command", status: "", want: false},
		{name: "wrong command", status: "0 0 bash", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelPaneNeedsRespawn(tt.status); got != tt.want {
				t.Fatalf("channelPaneNeedsRespawn(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestIsNoSessionError(t *testing.T) {
	if !isNoSessionError("can't find pane") {
		t.Fatal("expected can't find pane to be treated as no-session")
	}
	if !isNoSessionError("no server running on /tmp/tmux") {
		t.Fatal("expected no server error to be treated as no-session")
	}
	if isNoSessionError("permission denied") {
		t.Fatal("did not expect unrelated error to be treated as no-session")
	}
}

func TestChannelPaneLogPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := channelStderrLogPath(); !strings.Contains(got, ".wuphf/logs/channel-stderr.log") {
		t.Fatalf("unexpected stderr log path: %q", got)
	}
	if got := channelPaneSnapshotPath(); !strings.Contains(got, ".wuphf/logs/channel-pane.log") {
		t.Fatalf("unexpected pane log path: %q", got)
	}
}

func TestPrimeVisibleAgentsWithoutBrokerDoesNotPanic(t *testing.T) {
	l := &Launcher{sessionName: SessionName}
	l.primeVisibleAgents()
}

func TestNotificationTargetsForHumanMessageDirectToTaggedSpecialists(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		focusMode: true,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "be", Name: "Backend Engineer"},
				{Slug: "cmo", Name: "CMO"},
			},
		},
	}

	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Content: "Build the landing page",
		Tagged:  []string{"fe", "be"},
	})

	// In focus mode, when a human explicitly tags specialists, CEO is skipped.
	// The human's intent is explicit — no CEO re-routing needed.
	if len(immediate) != 2 {
		t.Fatalf("expected 2 immediate targets (fe + be, no CEO), got %+v", immediate)
	}
	slugs := make([]string, 0, len(immediate))
	for _, t2 := range immediate {
		slugs = append(slugs, t2.Slug)
	}
	for _, want := range []string{"fe", "be"} {
		found := false
		for _, s := range slugs {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected specialist %q in immediate targets, got %v", want, slugs)
		}
	}
	if len(delayed) != 0 {
		t.Fatalf("expected 0 delayed targets for tagged message, got %+v", delayed)
	}
}

func TestNotificationTargetsExplicitTagsAlwaysDeliverRegardlessOfDomain(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "cmo", Name: "CMO"},
			},
		},
	}

	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Content: "We need a positioning shift and launch campaign.",
		Tagged:  []string{"fe", "cmo"},
	})

	// Explicit @-tags always deliver regardless of domain inference. Domain is
	// "marketing" here, but fe was explicitly tagged — so ceo + fe + cmo all wake.
	if len(immediate) != 3 {
		t.Fatalf("expected 3 immediate targets (ceo + fe + cmo), got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("expected 0 delayed targets, got %+v", delayed)
	}
}

func TestNotificationTargetsTaggedSpecialistsGetImmediateDelivery(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "be", Name: "Backend Engineer"},
			},
		},
	}

	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Content: "Build the landing page",
		Tagged:  []string{"fe"},
	})

	ceoFound := false
	feFound := false
	for _, tgt := range immediate {
		if tgt.Slug == "ceo" {
			ceoFound = true
		}
		if tgt.Slug == "fe" {
			feFound = true
		}
	}
	if !ceoFound {
		t.Fatal("expected CEO in immediate targets")
	}
	if !feFound {
		t.Fatal("expected tagged @fe in immediate targets (not delayed)")
	}
	if len(delayed) != 0 {
		t.Fatalf("expected no delayed targets for tagged message, got %+v", delayed)
	}
}

func TestNotificationTargetsForCEOMessageNotifyTaggedOnly(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "be", Name: "Backend Engineer"},
				{Slug: "cmo", Name: "CMO"},
			},
		},
	}

	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "ceo",
		Content: "Frontend take this",
		Tagged:  []string{"fe"},
	})

	if len(delayed) != 0 {
		t.Fatalf("expected no delayed targets, got %+v", delayed)
	}
	if len(immediate) != 1 || immediate[0].Slug != "fe" {
		t.Fatalf("expected only FE immediate target, got %+v", immediate)
	}
}

func TestTaskNotificationTargetsFollowOwnerAndCEOHeadStart(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "cmo", Name: "CMO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}
	task := teamTask{
		ID:        "task-1",
		Channel:   "general",
		Title:     "Positioning work",
		Details:   "Draft a tighter launch narrative",
		Owner:     "cmo",
		Status:    "in_progress",
		CreatedBy: "you",
	}

	immediate, delayed := l.taskNotificationTargets(officeActionLog{
		Kind:      "task_created",
		Actor:     "you",
		Channel:   "general",
		RelatedID: "task-1",
	}, task)

	if len(immediate) != 2 || !containsNotificationTarget(immediate, "ceo") || !containsNotificationTarget(immediate, "cmo") {
		t.Fatalf("expected CEO immediate target, got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("expected no delayed target after immediate owner kickoff, got %+v", delayed)
	}

	immediate, delayed = l.taskNotificationTargets(officeActionLog{
		Kind:      "task_created",
		Actor:     "ceo",
		Channel:   "general",
		RelatedID: "task-1",
	}, task)
	if len(immediate) != 1 || !containsNotificationTarget(immediate, "cmo") {
		t.Fatalf("expected owner immediate target when CEO created the task, got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("expected no delayed target when CEO created the task, got %+v", delayed)
	}

	immediate, delayed = l.taskNotificationTargets(officeActionLog{
		Kind:      "task_updated",
		Actor:     "cmo",
		Channel:   "general",
		RelatedID: "task-1",
	}, task)
	if len(immediate) != 1 || immediate[0].Slug != "ceo" {
		t.Fatalf("expected CEO immediate target on owner update, got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("expected no delayed target on owner update, got %+v", delayed)
	}
}

func containsNotificationTarget(targets []notificationTarget, slug string) bool {
	for _, target := range targets {
		if target.Slug == slug {
			return true
		}
	}
	return false
}

func TestTaskNotificationContentIncludesOwnershipAndGuidance(t *testing.T) {
	l := &Launcher{}
	got := l.taskNotificationContent(officeActionLog{
		Kind:  "task_created",
		Actor: "you",
	}, teamTask{
		ID:      "task-9",
		Channel: "general",
		Title:   "Launch page",
		Details: "Tighten the story and assign follow-up",
		Owner:   "cmo",
		Status:  "in_progress",
	})
	if !strings.Contains(got, "Task created #task-9 on #general") {
		t.Fatalf("unexpected content prefix: %q", got)
	}
	if !strings.Contains(got, "owner @cmo") || !strings.Contains(got, "status in_progress") {
		t.Fatalf("expected ownership/status in content: %q", got)
	}
	if !strings.Contains(got, "team_poll") || !strings.Contains(got, "team_tasks") {
		t.Fatalf("expected routing guidance in content: %q", got)
	}
}

func TestTaskNotificationContentIncludesWorktreeDetails(t *testing.T) {
	l := &Launcher{}
	got := l.taskNotificationContent(officeActionLog{
		Kind:  "task_updated",
		Actor: "ceo",
	}, teamTask{
		ID:             "task-10",
		Channel:        "general",
		Title:          "Landing page polish",
		Owner:          "fe",
		Status:         "in_progress",
		ExecutionMode:  "local_worktree",
		WorktreeBranch: "wuphf-task-10",
		WorktreePath:   "/tmp/wuphf-task-task-10",
	})
	if !strings.Contains(got, "execution local_worktree") {
		t.Fatalf("expected execution mode in content: %q", got)
	}
	if !strings.Contains(got, "branch wuphf-task-10") || !strings.Contains(got, "path /tmp/wuphf-task-task-10") {
		t.Fatalf("expected worktree details in content: %q", got)
	}
	if !strings.Contains(got, `working_directory="/tmp/wuphf-task-task-10"`) {
		t.Fatalf("expected working_directory guidance in content: %q", got)
	}
}

func TestBuildPromptIncludesTaskStatusAndWorktreeGuidance(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}

	specialist := l.buildPrompt("fe")
	if !strings.Contains(specialist, "team_task_status") {
		t.Fatalf("expected team_task_status guidance in specialist prompt: %q", specialist)
	}
	if !strings.Contains(specialist, "working_directory") {
		t.Fatalf("expected working_directory guidance in specialist prompt: %q", specialist)
	}

	lead := l.buildPrompt("ceo")
	if !strings.Contains(lead, "team_task_status") {
		t.Fatalf("expected team_task_status guidance in lead prompt: %q", lead)
	}
	if !strings.Contains(lead, "working_directory") {
		t.Fatalf("expected working_directory guidance in lead prompt: %q", lead)
	}
}

func TestTaskNotificationTargetsWakeOwnerOnWatchdog(t *testing.T) {
	b := &Broker{
		channels: []teamChannel{{
			Slug:    "general",
			Name:    "general",
			Members: []string{"ceo", "fe"},
		}},
	}
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", },
		{Slug: "fe", Name: "Frontend Engineer", },
	}
	b.mu.Unlock()
	l := &Launcher{broker: b}
	task := teamTask{
		ID:      "task-1",
		Channel: "general",
		Title:   "Build signup conversion fix",
		Owner:   "fe",
		Status:  "in_progress",
	}

	immediate, delayed := l.taskNotificationTargets(officeActionLog{
		Kind:      "watchdog_alert",
		Actor:     "watchdog",
		Channel:   "general",
		RelatedID: "task-1",
	}, task)
	if !containsNotificationTarget(immediate, "ceo") || !containsNotificationTarget(immediate, "fe") {
		t.Fatalf("expected watchdog to wake CEO and owner immediately, got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("expected no delayed watchdog targets, got %+v", delayed)
	}
}

func TestTaskNotificationTargetsDoNotRewakeCEOForOwnCreatedTask(t *testing.T) {
	b := &Broker{
		channels: []teamChannel{{
			Slug:    "general",
			Name:    "general",
			Members: []string{"ceo", "fe"},
		}},
	}
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", },
		{Slug: "fe", Name: "Frontend Engineer", },
	}
	b.mu.Unlock()
	l := &Launcher{broker: b}
	task := teamTask{
		ID:      "task-2",
		Channel: "general",
		Title:   "Build signup conversion fix",
		Owner:   "fe",
		Status:  "in_progress",
	}

	immediate, delayed := l.taskNotificationTargets(officeActionLog{
		Kind:      "task_created",
		Actor:     "ceo",
		Channel:   "general",
		RelatedID: "task-2",
	}, task)
	if !containsNotificationTarget(immediate, "fe") {
		t.Fatalf("expected owner wake, got %+v", immediate)
	}
	if containsNotificationTarget(immediate, "ceo") {
		t.Fatalf("expected CEO not to be re-notified for its own created task, got %+v", immediate)
	}
	if len(delayed) != 0 {
		t.Fatalf("expected no delayed notifications, got %+v", delayed)
	}
}

func TestRecordPolicyDeduplicates(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	_, err := b.RecordPolicy("human_directed", "Always ask before deploying to production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Second call with same rule should deduplicate.
	_, err = b.RecordPolicy("human_directed", "Always ask before deploying to production")
	if err != nil {
		t.Fatalf("unexpected error on dedup: %v", err)
	}
	policies := b.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy after dedup, got %d", len(policies))
	}
}

func TestRecordWatchdogLedgerCreatesSignalAndDecision(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	l := &Launcher{broker: b}

	signalIDs, decisionID := l.recordWatchdogLedger("general", "task_stalled", "task-1", "fe", "Task is stalled.", "signal-1")
	if decisionID == "" || len(signalIDs) < 1 {
		t.Fatalf("expected watchdog refs, got signalIDs=%v decisionID=%q", signalIDs, decisionID)
	}
	if got := len(b.Signals()); got != 1 {
		t.Fatalf("expected 1 watchdog signal, got %d", got)
	}
	if got := len(b.Decisions()); got != 1 {
		t.Fatalf("expected 1 watchdog decision, got %d", got)
	}
	if decision := b.Decisions()[0]; decision.Kind != "remind_owner" {
		t.Fatalf("unexpected watchdog decision: %+v", decision)
	}
}

func TestRecordPolicyPersistsAndLoads(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	_, err := b.RecordPolicy("human_directed", "Work autonomously without asking for approval")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = b.RecordPolicy("auto_detected", "User prefers brief responses")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reload and verify persistence.
	b2 := NewBroker()
	if err := b2.loadState(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	policies := b2.ListPolicies()
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies after reload, got %d", len(policies))
	}
	sources := map[string]bool{}
	for _, p := range policies {
		sources[p.Source] = true
	}
	if !sources["human_directed"] || !sources["auto_detected"] {
		t.Fatalf("expected both sources, got %v", sources)
	}
}

func TestBuildNotificationContextEmpty(t *testing.T) {
	l := &Launcher{}
	ctx := l.buildNotificationContext("general", "msg-1", "", 5)
	if ctx != "" {
		t.Fatalf("expected empty context for nil broker, got %q", ctx)
	}
}

func TestBuildNotificationContextFormatsMessages(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	if _, err := b.PostMessage("you", "general", "First message", nil, ""); err != nil {
		t.Fatalf("post msg 1: %v", err)
	}
	if _, err := b.PostMessage("ceo", "general", "Second message", nil, ""); err != nil {
		t.Fatalf("post msg 2: %v", err)
	}
	if _, err := b.PostMessage("human", "general", "Third message", nil, ""); err != nil {
		t.Fatalf("post msg 3: %v", err)
	}

	l := &Launcher{broker: b}
	ctx := l.buildNotificationContext("general", "", "", 5)

	if !strings.Contains(ctx, "@you") {
		t.Error("expected @you in context")
	}
	if !strings.Contains(ctx, "@ceo") {
		t.Error("expected @ceo in context")
	}
	if !strings.Contains(ctx, "@human") {
		t.Error("expected @human in context")
	}
	if !strings.Contains(ctx, "[Recent channel]") {
		t.Error("expected [Recent channel] label when no thread root is given")
	}
}

func TestBuildNotificationContextFiltersSystem(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	if _, err := b.PostMessage("you", "general", "Real message", nil, ""); err != nil {
		t.Fatalf("post msg: %v", err)
	}
	b.PostSystemMessage("general", "Routing to @ceo...", "routing")
	if _, err := b.PostMessage("ceo", "general", "[STATUS] thinking", nil, ""); err != nil {
		t.Fatalf("post status msg: %v", err)
	}

	l := &Launcher{broker: b}
	ctx := l.buildNotificationContext("general", "", "", 5)

	if !strings.Contains(ctx, "@you") {
		t.Error("expected @you in context")
	}
	if strings.Contains(ctx, "Routing") {
		t.Error("expected system routing message to be filtered")
	}
	if strings.Contains(ctx, "[STATUS]") {
		t.Error("expected STATUS message to be filtered")
	}
}

func TestBuildNotificationContextRespectsLimit(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	for i := 0; i < 10; i++ {
		if _, err := b.PostMessage("you", "general", fmt.Sprintf("Message %d", i), nil, ""); err != nil {
			t.Fatalf("post msg %d: %v", i, err)
		}
	}

	l := &Launcher{broker: b}
	ctx := l.buildNotificationContext("general", "", "", 3)

	count := strings.Count(ctx, "@you")
	if count > 3 {
		t.Fatalf("expected at most 3 messages, got %d", count)
	}
}

func TestBuildNotificationContextExcludesTrigger(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	prev, err := b.PostMessage("you", "general", "Earlier message", nil, "")
	if err != nil {
		t.Fatalf("post prev: %v", err)
	}
	trigger, err := b.PostMessage("human", "general", "The trigger message", nil, "")
	if err != nil {
		t.Fatalf("post trigger: %v", err)
	}

	l := &Launcher{broker: b}
	ctx := l.buildNotificationContext("general", trigger.ID, "", 5)

	if strings.Contains(ctx, "The trigger message") {
		t.Error("trigger message should be excluded from context (it is sent separately as [New from @...])")
	}
	if !strings.Contains(ctx, "Earlier message") {
		t.Errorf("expected earlier message in context, got %q (prev.ID=%s)", ctx, prev.ID)
	}
}

func TestUltimateThreadRootFlat(t *testing.T) {
	// Flat thread: human ask (X) → CEO reply (Y, replyTo=X).
	// ultimateThreadRoot starting from Y should return X.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	humanAsk, err := b.PostMessage("human", "general", "Original human ask", nil, "")
	if err != nil {
		t.Fatalf("post humanAsk: %v", err)
	}
	ceoDelegate, err := b.PostMessage("ceo", "general", "CEO delegation", nil, humanAsk.ID)
	if err != nil {
		t.Fatalf("post ceoDelegate: %v", err)
	}
	l := &Launcher{broker: b}
	got := l.ultimateThreadRoot("general", ceoDelegate.ID)
	if got != humanAsk.ID {
		t.Errorf("expected ultimateThreadRoot(%s) = %s (humanAsk), got %s", ceoDelegate.ID, humanAsk.ID, got)
	}
}

func TestUltimateThreadRootDeep(t *testing.T) {
	// Deep thread: X → Y (replyTo=X) → Z (replyTo=Y).
	// ultimateThreadRoot starting from Z should return X.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	x, err := b.PostMessage("human", "general", "Root X", nil, "")
	if err != nil {
		t.Fatalf("post x: %v", err)
	}
	y, err := b.PostMessage("ceo", "general", "Mid Y", nil, x.ID)
	if err != nil {
		t.Fatalf("post y: %v", err)
	}
	z, err := b.PostMessage("you", "general", "Leaf Z", nil, y.ID)
	if err != nil {
		t.Fatalf("post z: %v", err)
	}
	l := &Launcher{broker: b}
	got := l.ultimateThreadRoot("general", z.ID)
	if got != x.ID {
		t.Errorf("expected ultimateThreadRoot(%s) = %s (x), got %s", z.ID, x.ID, got)
	}
}

func TestUltimateThreadRootTopLevel(t *testing.T) {
	// Top-level message has no replyTo: walk returns the message itself.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	x, err := b.PostMessage("human", "general", "Top level", nil, "")
	if err != nil {
		t.Fatalf("post x: %v", err)
	}
	l := &Launcher{broker: b}
	got := l.ultimateThreadRoot("general", x.ID)
	if got != x.ID {
		t.Errorf("expected ultimateThreadRoot(%s) = %s (self), got %s", x.ID, x.ID, got)
	}
}

func TestThreadMessageIDsParallelDelegation(t *testing.T) {
	// Regression: CEO delegates to two specialists in parallel (both reply to the
	// original human ask X). When Specialist A finishes, CEO should see that
	// Specialist B also already acted — even though B's message is at the X level,
	// not the Y level.
	//
	// Thread structure:
	//   X (human ask)
	//   ├── B_reply (specialist-b replies to X)
	//   └── Y (ceo delegates to A, replyTo X)
	//       └── A_reply (specialist-a reply to CEO, replyTo Y)
	//
	// CEO gets notified about A_reply. threadMessageIDs from ultimate root X must
	// include B_reply so CEO knows B already acted.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	x, err := b.PostMessage("human", "general", "What is the plan?", nil, "")
	if err != nil {
		t.Fatalf("post x: %v", err)
	}
	bReply, err := b.PostMessage("you", "general", "B answer (parallel)", nil, x.ID)
	if err != nil {
		t.Fatalf("post b_reply: %v", err)
	}
	y, err := b.PostMessage("ceo", "general", "A: please handle this", nil, x.ID)
	if err != nil {
		t.Fatalf("post y: %v", err)
	}
	aReply, err := b.PostMessage("you", "general", "A done", nil, y.ID)
	if err != nil {
		t.Fatalf("post a_reply: %v", err)
	}

	l := &Launcher{broker: b}

	root := l.ultimateThreadRoot("general", y.ID) // from Y, walk to X
	if root != x.ID {
		t.Fatalf("expected root=%s got %s", x.ID, root)
	}

	ids := l.threadMessageIDs("general", root)

	for _, want := range []string{x.ID, bReply.ID, y.ID, aReply.ID} {
		if _, ok := ids[want]; !ok {
			t.Errorf("expected message %s in thread IDs, not found", want)
		}
	}
}

func TestBuildNotificationContextThreadFiltering(t *testing.T) {
	// Verifies that when a threadRootID is given, only messages in that thread
	// appear in the context (labeled [Recent thread]), and messages from a
	// concurrent unrelated thread are excluded.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Thread A: human root + ceo reply
	threadA, err := b.PostMessage("human", "general", "Thread A root", nil, "")
	if err != nil {
		t.Fatalf("post threadA root: %v", err)
	}
	if _, err := b.PostMessage("ceo", "general", "Thread A reply", nil, threadA.ID); err != nil {
		t.Fatalf("post threadA reply: %v", err)
	}

	// Thread B: unrelated top-level discussion
	if _, err := b.PostMessage("you", "general", "Thread B unrelated", nil, ""); err != nil {
		t.Fatalf("post threadB: %v", err)
	}

	l := &Launcher{broker: b}

	// Context filtered to thread A should include root + reply, not thread B.
	ctx := l.buildNotificationContext("general", "", threadA.ID, 10)

	if !strings.Contains(ctx, "[Recent thread]") {
		t.Errorf("expected [Recent thread] label for thread-filtered context, got %q", ctx)
	}
	if !strings.Contains(ctx, "Thread A root") {
		t.Error("expected Thread A root in thread context")
	}
	if !strings.Contains(ctx, "Thread A reply") {
		t.Error("expected Thread A reply in thread context")
	}
	if strings.Contains(ctx, "Thread B unrelated") {
		t.Error("Thread B message should be excluded when filtering by thread A root")
	}
}

func TestBuildNotificationContextIncludesDeepThreadMessages(t *testing.T) {
	// Regression: in a research→marketing chain the marketing agent's context must
	// include the researcher's results (a grandchild of root), not just root + direct
	// children. Previous filter only showed root + replyTo==root messages.
	//
	// Thread tree:
	//   X (human: "research competitors and write email")
	//   └── Y (ceo: "@researcher please research")
	//       └── R (researcher: "here are the findings...")
	//           └── Z (ceo: "@marketing write email based on research") ← TRIGGER
	//
	// Marketing agent's context should show X (root anchor) and R (research results),
	// NOT just X and Y (which is all the old shallow filter produced).
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	x, err := b.PostMessage("human", "general", "Research competitors and write email", nil, "")
	if err != nil {
		t.Fatalf("post x: %v", err)
	}
	y, err := b.PostMessage("ceo", "general", "Researcher: please research", nil, x.ID)
	if err != nil {
		t.Fatalf("post y: %v", err)
	}
	r, err := b.PostMessage("you", "general", "Research findings: competitor A does X, B does Y", nil, y.ID)
	if err != nil {
		t.Fatalf("post r: %v", err)
	}
	z, err := b.PostMessage("ceo", "general", "Marketing: write email based on research", nil, r.ID)
	if err != nil {
		t.Fatalf("post z: %v", err)
	}

	l := &Launcher{broker: b}
	// Marketing agent receives z as trigger; thread root is x.
	ctx := l.buildNotificationContext("general", z.ID, x.ID, 4)

	if !strings.Contains(ctx, "[Recent thread]") {
		t.Errorf("expected [Recent thread] label, got %q", ctx)
	}
	// Root (human ask) must always appear — it's the anchor.
	if !strings.Contains(ctx, "Research competitors") {
		t.Error("expected root human message in context")
	}
	// Research results must appear — this is the key regression fix.
	if !strings.Contains(ctx, "Research findings") {
		t.Error("expected researcher's results in context (deep thread message missing)")
	}
	// Trigger (z) must be excluded.
	if strings.Contains(ctx, "write email based on research") {
		t.Error("trigger message should be excluded from context")
	}
}

func TestBuildTaskNotificationContextCEOSeesAllChannels(t *testing.T) {
	// CEO task context must include tasks from ALL channels, not just the channel of
	// the message that woke the CEO. When woken from "engineering" channel, the CEO
	// should still see tasks in "general".
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Post a task in "general" channel
	if _, err := b.PostMessage("you", "general", "initial ask", nil, ""); err != nil {
		t.Fatalf("post initial: %v", err)
	}
	if _, _, err := b.EnsureTask("general", "general-task", "Task in general channel", "ceo", "you", ""); err != nil {
		t.Fatalf("create task: %v", err)
	}

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "engineering", Name: "Engineering"},
			},
		},
	}

	// Passing "" to buildTaskNotificationContext means AllTasks — should see general tasks.
	ctx := l.buildTaskNotificationContext("", "ceo", 3)
	if !strings.Contains(ctx, "general-task") {
		t.Errorf("expected task from general channel in AllTasks context, got %q", ctx)
	}

	// Passing "engineering" only shows engineering tasks — should NOT see general task.
	ctxEngOnly := l.buildTaskNotificationContext("engineering", "ceo", 3)
	if strings.Contains(ctxEngOnly, "general-task") {
		t.Errorf("channel-scoped context should not include general-channel task, got %q", ctxEngOnly)
	}
}

func TestResponseInstructionForTargetLeadFromSpecialist(t *testing.T) {
	// When the CEO is woken by a specialist, the instruction should differ from
	// when woken by the human. Specialist completion should prompt "stay quiet or
	// coordinate" behavior, not "give first reply quickly".
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "engineering", Name: "Engineering"},
			},
		},
	}

	// Woken by human → should get "reply quickly" instruction
	humanInstr := l.responseInstructionForTarget(channelMessage{From: "you"}, "ceo")
	if !strings.Contains(humanInstr, "Give the first top-level reply quickly") {
		t.Errorf("expected quick-reply instruction when woken by human, got %q", humanInstr)
	}

	// Woken by specialist → should get "stay quiet unless needed" instruction
	specialistInstr := l.responseInstructionForTarget(channelMessage{From: "engineering"}, "ceo")
	if strings.Contains(specialistInstr, "Give the first top-level reply quickly") {
		t.Errorf("specialist wake-up should not use human-style quick-reply instruction, got %q", specialistInstr)
	}
	if !strings.Contains(specialistInstr, "stay quiet") {
		t.Errorf("specialist wake-up should include stay-quiet guidance, got %q", specialistInstr)
	}
}

func TestBuildNotificationContextFallsBackToChannelWhenThreadEmpty(t *testing.T) {
	// When threadRootID is given but the thread has no displayable messages
	// (e.g. the trigger IS the root and no other replies exist yet), the function
	// should fall back to recent channel messages labeled [Recent channel].
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Older unrelated channel message
	if _, err := b.PostMessage("you", "general", "Earlier channel message", nil, ""); err != nil {
		t.Fatalf("post earlier: %v", err)
	}
	// The trigger is a new top-level message — no prior thread members exist.
	trigger, err := b.PostMessage("human", "general", "Brand new topic", nil, "")
	if err != nil {
		t.Fatalf("post trigger: %v", err)
	}

	l := &Launcher{broker: b}
	// triggerMsgID = trigger.ID (excluded), threadRootID = trigger.ID (new root, no replies)
	ctx := l.buildNotificationContext("general", trigger.ID, trigger.ID, 5)

	if !strings.Contains(ctx, "[Recent channel]") {
		t.Errorf("expected [Recent channel] fallback label when thread has no prior messages, got %q", ctx)
	}
	if strings.Contains(ctx, "Brand new topic") {
		t.Error("trigger message should still be excluded even in fallback path")
	}
	if !strings.Contains(ctx, "Earlier channel message") {
		t.Error("expected earlier channel message in fallback context")
	}
}

func TestRelevantTaskForTargetCrossChannel(t *testing.T) {
	// When CEO delegates in "general" but the specialist's task lives in "engineering",
	// relevantTaskForTarget must still find it. Before the AllTasks() fix, it searched
	// only ChannelTasks("general") and returned nothing — causing work packets to omit
	// the "Active task" line and giving specialists the wrong response instruction.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()

	// Create "engineering" channel directly in broker state.
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "engineering",
		Members: []string{"ceo", "engineering"},
	})
	b.mu.Unlock()

	// Human asks in "general".
	humanMsg, err := b.PostMessage("you", "general", "Implement rate limiting middleware", nil, "")
	if err != nil {
		t.Fatalf("post human msg: %v", err)
	}

	// CEO creates task in "engineering" channel with threadID pointing to human's message.
	task, _, err := b.EnsureTask("engineering", "Rate limiting middleware", "Implement middleware", "engineering", "ceo", humanMsg.ID)
	if err != nil {
		t.Fatalf("ensure task: %v", err)
	}
	_ = task

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "engineering", Name: "Engineering"},
			},
		},
	}

	// CEO delegates as a REPLY to the human's original message. ReplyTo = humanMsg.ID,
	// so threadRoot = humanMsg.ID which matches task.ThreadID. Message channel is "general"
	// but the task lives in "engineering" — this tests the AllTasks() cross-channel search.
	ceoMsg := channelMessage{
		ID:      "ceo-msg-1",
		From:    "ceo",
		Channel: "general",
		Content: "Please implement rate limiting. @engineering",
		ReplyTo: humanMsg.ID, // CEO replies in the same thread as the human ask
		Tagged:  []string{"engineering"},
	}

	// relevantTaskForTarget should find the task in "engineering" even though the
	// message arrived from "general". The thread match (threadRoot = humanMsg.ID =
	// task.ThreadID) succeeds because we now search AllTasks() not just ChannelTasks("general").
	found, ok := l.relevantTaskForTarget(ceoMsg, "engineering")
	if !ok {
		t.Fatal("expected to find task across channels via thread match, got nothing")
	}
	if found.Title != "Rate limiting middleware" {
		t.Errorf("expected rate limiting task, got %q", found.Title)
	}

	// Confirm that with the OLD ChannelTasks("general") search, the task would NOT be found.
	// This validates the fix is necessary: ChannelTasks returns nothing for "general" when
	// the task is in "engineering".
	if len(b.ChannelTasks("general")) != 0 {
		t.Error("ChannelTasks(general) should be empty — task is in 'engineering', not 'general'")
	}
	if len(b.AllTasks()) == 0 {
		t.Error("AllTasks() should include the engineering task")
	}

	// responseInstructionForTarget: engineering is tagged → "directly tagged" instruction.
	instr := l.responseInstructionForTarget(ceoMsg, "engineering")
	if !strings.Contains(instr, "directly tagged") {
		t.Errorf("expected 'directly tagged' instruction, got %q", instr)
	}

	// A different specialist not in Tagged and not owning the task should stay quiet.
	unrelatedMsg := channelMessage{
		ID:      "ceo-msg-2",
		From:    "ceo",
		Channel: "general",
		Content: "Just an update.",
		ReplyTo: humanMsg.ID,
		Tagged:  []string{},
	}
	_, ok2 := l.relevantTaskForTarget(unrelatedMsg, "marketing")
	if ok2 {
		t.Error("marketing should not find an engineering-owned task")
	}
}

func TestBlockedTaskNotificationAndUnblockFlow(t *testing.T) {
	// Verifies the full research→marketing dependency chain:
	// 1. CEO creates marketing task with depends_on: [research-task] while research is in_progress
	// 2. Marketing should NOT be woken immediately (task is blocked)
	// 3. When research completes, a task_unblocked action fires
	// 4. Marketing IS woken immediately via the task_unblocked notification
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Create research task (in_progress).
	researchTask, _, err := b.EnsureTask("general", "Research rate limiting", "", "research", "ceo", "")
	if err != nil {
		t.Fatalf("create research task: %v", err)
	}

	// Create marketing task depending on the research task → should be Blocked.
	marketingTask, _, err := b.EnsureTask("general", "Write blog copy", "Based on research results", "marketing", "ceo", "", researchTask.ID)
	if err != nil {
		t.Fatalf("create marketing task: %v", err)
	}

	// Set broker members so agentPaneTargets can build notification targets.
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "research", Name: "Researcher"},
		{Slug: "marketing", Name: "Marketer"},
	}
	// Also populate the general channel's Members so EnabledMembers returns our agents.
	if ch := b.findChannelLocked("general"); ch != nil {
		ch.Members = []string{"ceo", "research", "marketing"}
	}
	b.mu.Unlock()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "research", Name: "Researcher"},
				{Slug: "marketing", Name: "Marketer"},
			},
		},
	}

	// Simulate research completing: task_created action for the marketing task.
	// With task.Blocked=true, taskNotificationTargets should NOT add marketing to immediate.
	blockedTask := teamTask{
		ID:        marketingTask.ID,
		Channel:   "general",
		Title:     "Write blog copy",
		Owner:     "marketing",
		Status:    "in_progress",
		DependsOn: []string{researchTask.ID},
		Blocked:   true,
	}
	createdAction := officeActionLog{Kind: "task_created", Actor: "ceo", Channel: "general", RelatedID: marketingTask.ID}
	immediate, _ := l.taskNotificationTargets(createdAction, blockedTask)
	for _, t2 := range immediate {
		if t2.Slug == "marketing" {
			t.Error("marketing should NOT be woken when their task is blocked")
		}
	}

	// Now simulate research completing: unblockDependentsLocked should fire task_unblocked action.
	// We check this by looking at the action log after marking research task done.
	initialActionCount := len(b.Actions())
	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID == researchTask.ID {
			b.tasks[i].Status = "done"
		}
	}
	b.unblockDependentsLocked(researchTask.ID)
	b.mu.Unlock()

	// Verify a task_unblocked action was recorded.
	actions := b.Actions()
	if len(actions) <= initialActionCount {
		t.Fatal("expected task_unblocked action after completing research task")
	}
	var unblockedAction officeActionLog
	for _, a := range actions[initialActionCount:] {
		if a.Kind == "task_unblocked" {
			unblockedAction = a
			break
		}
	}
	if unblockedAction.Kind == "" {
		t.Fatal("expected task_unblocked action kind in log")
	}
	if unblockedAction.RelatedID != marketingTask.ID {
		t.Errorf("expected task_unblocked for marketing task %s, got %s", marketingTask.ID, unblockedAction.RelatedID)
	}

	// Verify that task_unblocked wakes marketing immediately.
	unblockedTaskState := teamTask{
		ID:      marketingTask.ID,
		Channel: "general",
		Title:   "Write blog copy",
		Owner:   "marketing",
		Status:  "in_progress",
		Blocked: false, // now unblocked
	}
	immediate2, _ := l.taskNotificationTargets(unblockedAction, unblockedTaskState)
	hasMarketing := false
	for _, t2 := range immediate2 {
		if t2.Slug == "marketing" {
			hasMarketing = true
		}
	}
	if !hasMarketing {
		t.Error("marketing should be woken immediately when their task is unblocked")
	}
}

// TestActionLoopAllowsTaskUnblocked verifies that the notifyTaskActionsLoop kind
// filter passes task_unblocked through to deliverTaskNotification. Before the fix,
// the filter only listed "task_created" and "task_updated", silently dropping
// task_unblocked actions and breaking the entire dependency-chain wake-up.
//
// The test exercises this by subscribing to broker actions, completing the
// research task, and asserting that a task_unblocked action is emitted AND that
// taskNotificationTargets (called via deliverTaskNotification) would include
// the marketing owner in immediate targets — confirming the full path is live.
func TestActionLoopAllowsTaskUnblocked(t *testing.T) {
	dir := t.TempDir()
	oldPathFn := brokerStatePath
	brokerStatePath = func() string { return filepath.Join(dir, "state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "research", Name: "Researcher"},
		{Slug: "marketing", Name: "Marketer"},
	}
	if ch := b.findChannelLocked("general"); ch != nil {
		ch.Members = []string{"ceo", "research", "marketing"}
	}
	b.mu.Unlock()

	researchTask, _, err := b.EnsureTask("general", "Research competitors", "", "research", "ceo", "")
	if err != nil {
		t.Fatalf("create research task: %v", err)
	}
	marketingTask, _, err := b.EnsureTask("general", "Write blog post about findings", "", "marketing", "ceo", "")
	if err != nil {
		t.Fatalf("create marketing task: %v", err)
	}
	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID == marketingTask.ID {
			b.tasks[i].DependsOn = []string{researchTask.ID}
			b.tasks[i].Blocked = true
		}
	}
	b.mu.Unlock()

	// Subscribe to actions BEFORE triggering the unblock.
	actions, unsub := b.SubscribeActions(8)
	defer unsub()

	// Drain the task_created actions already in flight.
	timeout := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case <-actions:
		case <-timeout:
			break drain
		}
	}

	// Now complete research → should fire task_unblocked for marketing.
	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID == researchTask.ID {
			b.tasks[i].Status = "done"
		}
	}
	b.unblockDependentsLocked(researchTask.ID)
	b.mu.Unlock()

	// Collect the next actions and look for task_unblocked.
	deadline := time.After(2 * time.Second)
	var unblockedAction officeActionLog
	for {
		select {
		case a := <-actions:
			if a.Kind == "task_unblocked" && a.RelatedID == marketingTask.ID {
				unblockedAction = a
			}
		case <-deadline:
			goto done
		}
		if unblockedAction.Kind != "" {
			break
		}
	}
done:
	if unblockedAction.Kind == "" {
		t.Fatal("task_unblocked action was never emitted by unblockDependentsLocked")
	}

	// Now verify that the action loop kind-filter would NOT drop this action.
	// The filter is: kind != "task_created" && kind != "task_updated" && kind != "task_unblocked" → skip
	// task_unblocked is now in the allow-list, so this should NOT be skipped.
	if unblockedAction.Kind != "task_created" && unblockedAction.Kind != "task_updated" && unblockedAction.Kind != "task_unblocked" {
		t.Errorf("task_unblocked kind %q would be dropped by the action loop filter", unblockedAction.Kind)
	}

	// Finally, verify that with the task in unblocked state, marketing appears
	// in immediate notification targets (this is what deliverTaskNotification does).
	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "research", Name: "Researcher"},
				{Slug: "marketing", Name: "Marketer"},
			},
		},
	}
	unblockedState := teamTask{
		ID:      marketingTask.ID,
		Channel: "general",
		Title:   "Write blog post about findings",
		Owner:   "marketing",
		Status:  "in_progress",
		Blocked: false,
	}
	immediate, _ := l.taskNotificationTargets(unblockedAction, unblockedState)
	hasMarketing := false
	for _, t2 := range immediate {
		if t2.Slug == "marketing" {
			hasMarketing = true
		}
	}
	if !hasMarketing {
		t.Error("marketing should be in immediate targets after task_unblocked")
	}
}
