package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
