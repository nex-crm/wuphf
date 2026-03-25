package team

import (
	"net/http"
	"net/http/httptest"
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
