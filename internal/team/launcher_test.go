package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Channel: "general",
		Content: "Need a product call here.",
	})

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

func TestNotificationTargetsForHumanMessageBroadcastAll(t *testing.T) {
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
		From:    "you",
		Content: "Build the landing page",
		Tagged:  []string{"fe", "be"},
	})

	// Broadcast model: CEO immediate, everyone else delayed
	if len(immediate) != 1 || immediate[0].Slug != "ceo" {
		t.Fatalf("expected CEO as only immediate target, got %+v", immediate)
	}
	if len(delayed) == 0 {
		t.Fatal("expected delayed targets for other agents, got none")
	}
	// Verify fe and be are in delayed targets
	slugs := make(map[string]bool)
	for _, d := range delayed {
		slugs[d.Slug] = true
	}
	if !slugs["fe"] || !slugs["be"] {
		t.Fatalf("expected fe and be in delayed targets, got %+v", delayed)
	}
}

func TestNotificationTargetsPreferMatchingDomainOverWrongTags(t *testing.T) {
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

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Content: "We need a positioning shift and launch campaign.",
		Tagged:  []string{"fe", "cmo"},
	})

	// Direct routing: tagged agents go immediate
	if len(immediate) < 1 {
		t.Fatalf("expected at least 1 immediate target, got %+v", immediate)
	}
	
}

func TestNotificationTargetsForCEOMessageBroadcastsToAll(t *testing.T) {
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

	// CEO sends a message — everyone else gets it (CEO excluded as sender)
	immediate, delayed := l.notificationTargetsForMessage(channelMessage{
		From:    "ceo",
		Content: "Frontend take this",
		Tagged:  []string{"fe"},
	})

	// No immediate (CEO is lead but also sender), all others delayed
	if len(immediate) != 0 {
		t.Fatalf("expected no immediate targets (CEO is sender), got %+v", immediate)
	}
	if len(delayed) == 0 {
		t.Fatal("expected delayed targets for other agents, got none")
	}
	slugs := make(map[string]bool)
	for _, d := range delayed {
		slugs[d.Slug] = true
	}
	if !slugs["fe"] || !slugs["be"] || !slugs["cmo"] {
		t.Fatalf("expected fe, be, cmo in delayed targets, got %+v", delayed)
	}
}

func TestShouldDeliverDelayedNotificationSkipsIfAgentAlreadyReplied(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	msg1, err := b.PostMessage("you", "general", "Build the landing page", []string{"fe"}, "")
	if err != nil {
		t.Fatalf("post human message: %v", err)
	}
	// FE already replied in the same thread
	if _, err := b.PostMessage("fe", "general", "On it!", nil, msg1.ID); err != nil {
		t.Fatalf("post fe message: %v", err)
	}

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}
	if l.shouldDeliverDelayedNotification("fe", msg1) {
		t.Fatal("expected FE delayed notification to be skipped — FE already replied")
	}
}

func TestShouldDeliverDelayedNotificationAllowsAllEnabledMembers(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()

	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "cmo", Name: "CMO"},
			},
		},
	}
	msg := channelMessage{From: "you", Channel: "general", Content: "We need a positioning shift.", ID: "msg-1"}
	// All enabled members should receive the delayed notification
	if !l.shouldDeliverDelayedNotification("fe", msg) {
		t.Fatal("expected FE delayed notification to be allowed")
	}
	if !l.shouldDeliverDelayedNotification("cmo", msg) {
		t.Fatal("expected CMO delayed notification to be allowed")
	}
}

func TestTaskNotificationTargetsFollowOwnerAndCEOHeadStart(t *testing.T) {
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
	l := &Launcher{
		broker: &Broker{
			channels: []teamChannel{{
				Slug:    "general",
				Name:    "general",
				Members: []string{"ceo", "fe"},
			}},
		},
	}
	l.broker.ensureDefaultOfficeMembersLocked()
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
	l := &Launcher{
		broker: &Broker{
			channels: []teamChannel{{
				Slug:    "general",
				Name:    "general",
				Members: []string{"ceo", "fe"},
			}},
		},
	}
	l.broker.ensureDefaultOfficeMembersLocked()
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

func TestPersistHumanDirectiveRecordsLedger(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	l := &Launcher{broker: b}
	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "CEO, give me the office state and ask PM for a v1 scope.",
		Tagged:  []string{"ceo", "pm"},
	}

	l.persistHumanDirective(msg)

	if got := len(b.Signals()); got != 1 {
		t.Fatalf("expected 1 signal, got %d", got)
	}
	if got := len(b.Decisions()); got != 1 {
		t.Fatalf("expected 1 decision, got %d", got)
	}
	if got := len(b.Actions()); got != 1 {
		t.Fatalf("expected 1 action, got %d", got)
	}
	if sig := b.Signals()[0]; sig.Source != "human_directive" || sig.SourceRef != "msg-1" {
		t.Fatalf("unexpected human directive signal: %+v", sig)
	}
	if decision := b.Decisions()[0]; decision.Kind == "" || decision.Owner != "ceo" {
		t.Fatalf("unexpected human directive decision: %+v", decision)
	}
	if action := b.Actions()[0]; action.Kind != "human_directive" || action.DecisionID == "" || len(action.SignalIDs) == 0 {
		t.Fatalf("unexpected human directive action: %+v", action)
	}

	l.persistHumanDirective(msg)
	if got := len(b.Signals()); got != 1 {
		t.Fatalf("expected deduped signal count to stay 1, got %d", got)
	}
	if got := len(b.Decisions()); got != 1 {
		t.Fatalf("expected deduped decision count to stay 1, got %d", got)
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

func TestPersistOfficeSignalsCreatesOwnedTaskAndLedger(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	l := &Launcher{broker: b}

	l.persistOfficeSignals("general", []officeSignal{{
		ID:         "nex-1",
		Source:     "nex_insights",
		Kind:       "activity",
		Title:      "Nex insight",
		Content:    "Paul Williams is speaking at a product event and we should follow up on the opportunity.",
		Channel:    "general",
		Owner:      "ai",
		Confidence: "very_high",
		Urgency:    "normal",
	}})

	if got := len(b.Signals()); got != 1 {
		t.Fatalf("expected 1 signal, got %d", got)
	}
	if got := len(b.Decisions()); got != 1 {
		t.Fatalf("expected 1 decision, got %d", got)
	}
	if got := len(b.Messages()); got != 1 {
		t.Fatalf("expected 1 automation message, got %d", got)
	}
	tasks := b.ChannelTasks("general")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 planned task, got %d", len(tasks))
	}
	if tasks[0].Owner != "ai" || tasks[0].SourceSignalID == "" || tasks[0].SourceDecisionID == "" {
		t.Fatalf("unexpected planned task: %+v", tasks[0])
	}
	actions := b.Actions()
	hasTaskCreated := false
	for _, a := range actions {
		if a.Kind == "task_created" {
			hasTaskCreated = true
			break
		}
	}
	if !hasTaskCreated {
		t.Fatalf("expected task_created action, got %+v", actions)
	}
}
