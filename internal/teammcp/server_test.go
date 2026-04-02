package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nex-crm/wuphf/internal/team"
)

func TestSuppressBroadcastReasonAllowsViewpoints(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"Here is my thought.",
		"",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "We need better launch positioning and campaign messaging."},
		},
		nil,
	)
	if reason != "" {
		t.Fatalf("expected FE reply to be allowed (agents should share viewpoints), got %q", reason)
	}
}

func TestSuppressBroadcastReasonAllowsOwnedTaskReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"Shipping the signup work now.",
		"msg-1",
		[]brokerMessage{
			{ID: "msg-1", From: "ceo", Content: "Frontend, take the signup flow."},
		},
		[]brokerTaskSummary{
			{ID: "task-1", Owner: "fe", Status: "in_progress", ThreadID: "msg-1", Title: "Own signup flow"},
		},
	)
	if reason != "" {
		t.Fatalf("expected owned-task reply to be allowed, got %q", reason)
	}
}

func TestSuppressBroadcastReasonAllowsAfterCEOReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"I can take this too.",
		"msg-1",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "What should we do here?"},
			{ID: "msg-2", From: "ceo", Content: "PM owns this. Let's keep scope tight.", ReplyTo: "msg-1"},
		},
		nil,
	)
	if reason != "" {
		t.Fatalf("expected FE reply to be allowed after CEO (agents share viewpoints), got %q", reason)
	}
}

func TestIsOneOnOneModeFromEnv(t *testing.T) {
	t.Setenv("WUPHF_ONE_ON_ONE", "1")
	if !isOneOnOneMode() {
		t.Fatal("expected 1o1 env to enable direct mode")
	}
}

func TestHandleTeamMemberCreateTriggersReconfigure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	called := 0
	prev := reconfigureOfficeSessionFn
	reconfigureOfficeSessionFn = func() error {
		called++
		return nil
	}
	defer func() { reconfigureOfficeSessionFn = prev }()

	if _, _, err := handleTeamMember(context.Background(), nil, TeamMemberArgs{
		Action: "create",
		Slug:   "growthops",
		Name:   "Growth Ops",
		Role:   "Growth Ops",
		MySlug: "ceo",
	}); err != nil {
		t.Fatalf("handleTeamMember: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected one reconfigure call, got %d", called)
	}
	found := false
	for _, member := range b.OfficeMembers() {
		if member.Slug == "growthops" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected created office member to persist")
	}
}

func TestHandleTeamChannelCreateTriggersReconfigure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	called := 0
	prev := reconfigureOfficeSessionFn
	reconfigureOfficeSessionFn = func() error {
		called++
		return nil
	}
	defer func() { reconfigureOfficeSessionFn = prev }()

	if _, _, err := handleTeamChannel(context.Background(), nil, TeamChannelArgs{
		Action:      "create",
		Channel:     "launch",
		Name:        "launch",
		Description: "Launch execution channel",
		Members:     []string{"pm", "fe"},
		MySlug:      "ceo",
	}); err != nil {
		t.Fatalf("handleTeamChannel: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected one reconfigure call, got %d", called)
	}

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/channels", b.Addr()), nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch channels: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Channels []struct {
			Slug        string   `json:"slug"`
			Description string   `json:"description"`
			Members     []string `json:"members"`
		} `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode channels: %v", err)
	}

	found := false
	for _, ch := range result.Channels {
		if ch.Slug == "launch" {
			found = true
			if ch.Description != "Launch execution channel" {
				t.Fatalf("expected description to persist, got %+v", ch)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected created channel to persist")
	}
}

func TestHandleHumanMessageUsesDirectSessionLabelInOneOnOneMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_ONE_ON_ONE", "1")
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	result, _, err := handleHumanMessage(context.Background(), nil, HumanMessageArgs{
		Content: "Action complete.",
	})
	if err != nil {
		t.Fatalf("handleHumanMessage: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected text result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	if text.Text == "" {
		t.Fatal("expected non-empty text")
	}
	if want := "this direct session"; !strings.Contains(text.Text, want) {
		t.Fatalf("expected %q in %q", want, text.Text)
	}
	if strings.Contains(text.Text, "#general") {
		t.Fatalf("did not expect office channel label in %q", text.Text)
	}
}

func TestHandleTeamPollOneOnOneHighlightsLatestHumanRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_ONE_ON_ONE", "1")
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	for _, msg := range []map[string]any{
		{"channel": "general", "from": "you", "content": "Old unrelated ask."},
		{"channel": "general", "from": "ceo", "content": "Acknowledged."},
		{"channel": "general", "from": "you", "content": "Newest request wins."},
	} {
		if err := brokerPostJSON(context.Background(), "/messages", msg, nil); err != nil {
			t.Fatalf("post message: %v", err)
		}
	}

	result, _, err := handleTeamPoll(context.Background(), nil, TeamPollArgs{MySlug: "ceo"})
	if err != nil {
		t.Fatalf("handleTeamPoll: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected text result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "Latest human request to answer now:") {
		t.Fatalf("expected latest-request header, got %q", text.Text)
	}
	if !strings.Contains(text.Text, "Newest request wins.") {
		t.Fatalf("expected latest human message in %q", text.Text)
	}
}
