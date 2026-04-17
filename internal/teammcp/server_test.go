package teammcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/team"
)

func ensureBrokerMembers(t *testing.T, ctx context.Context, slugs ...string) {
	t.Helper()
	for _, slug := range slugs {
		name := strings.ReplaceAll(slug, "-", " ")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		name = strings.ToUpper(name[:1]) + name[1:]
		err := brokerPostJSON(ctx, "/office-members", map[string]any{
			"action":     "create",
			"slug":       slug,
			"name":       name,
			"role":       name,
			"created_by": "ceo",
		}, nil)
		if err != nil && !strings.Contains(err.Error(), "member already exists") {
			t.Fatalf("ensure broker member %s: %v", slug, err)
		}
		err = brokerPostJSON(ctx, "/channel-members", map[string]any{
			"channel": "general",
			"action":  "add",
			"slug":    slug,
		}, nil)
		if err != nil {
			t.Fatalf("ensure broker member %s in #general: %v", slug, err)
		}
	}
}

func textFromResult(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected text result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}

func TestConfigureServerToolsExposesActionToolsToOfficeSpecialists(t *testing.T) {
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "wuphf-team-test", Version: "0.1.0"}, nil)
	configureServerTools(server, "workflow-architect", "general", false)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "team_action_connections") {
		t.Fatalf("expected team_action_connections for office specialist, got %v", names)
	}
	if !slices.Contains(names, "team_action_workflow_execute") {
		t.Fatalf("expected team_action_workflow_execute for office specialist, got %v", names)
	}
}

func TestConfigureServerToolsAnnotatesActionTools(t *testing.T) {
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "wuphf-team-test", Version: "0.1.0"}, nil)
	configureServerTools(server, "workflow-architect", "general", false)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var connections *mcp.Tool
	var execute *mcp.Tool
	for i := range tools.Tools {
		switch tools.Tools[i].Name {
		case "team_action_connections":
			connections = tools.Tools[i]
		case "team_action_execute":
			execute = tools.Tools[i]
		}
	}
	if connections == nil || connections.Annotations == nil || !connections.Annotations.ReadOnlyHint {
		t.Fatalf("expected team_action_connections to be read-only, got %+v", connections)
	}
	if execute == nil || execute.Annotations == nil || execute.Annotations.DestructiveHint == nil || *execute.Annotations.DestructiveHint {
		t.Fatalf("expected team_action_execute to be a non-destructive write tool, got %+v", execute)
	}
}

func TestSuppressBroadcastReasonBlocksOutOfDomainReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"Here is my thought.",
		"",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "We need better launch positioning and campaign messaging."},
		},
		nil,
	)
	if reason == "" {
		t.Fatal("expected FE reply to be suppressed for marketing-only work")
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

func TestSuppressBroadcastReasonBlocksAfterUntargetedCEOReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"I can take the UI piece.",
		"msg-1",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "What should we do here?"},
			{ID: "msg-2", From: "ceo", Content: "PM owns this. Let's keep scope tight.", ReplyTo: "msg-1"},
		},
		nil,
	)
	// CEO reply no longer suppresses specialists — agents collaborate, CEO takes final call
	if reason != "" {
		t.Fatalf("expected CEO reply to NOT block specialist, got %q", reason)
	}
}

func TestSuppressBroadcastReasonAllowsOperatorFollowUpInActiveTaskThread(t *testing.T) {
	reason := suppressBroadcastReason(
		"operator",
		"`#task-24` approved and moving to the next execution slice.",
		"msg-20",
		[]brokerMessage{
			{ID: "msg-16", From: "planner", Content: "Locked execution brief."},
			{ID: "msg-20", From: "executor", Content: "Completed `#task-7` and moved it to review.", ReplyTo: "msg-16"},
		},
		[]brokerTaskSummary{
			{ID: "task-24", Owner: "operator", Status: "in_progress", ThreadID: "msg-16", Title: "Approve script and open next execution lane"},
		},
	)
	if reason != "" {
		t.Fatalf("expected operator follow-up in active task thread to be allowed, got %q", reason)
	}
}

// TestSuppressBroadcastReasonAllowsMarketingCompetitorPricing verifies that a
// marketing agent can broadcast about "competitor pricing" without being suppressed.
// Before the fix, "pricing" was a sales-only keyword so "competitor pricing findings"
// classified as "sales" domain and a marketing agent got blocked ("outside your domain").
func TestSuppressBroadcastReasonAllowsMarketingCompetitorPricing(t *testing.T) {
	reason := suppressBroadcastReason(
		"marketing",
		"Here are our competitor pricing findings — Acme charges $50/seat, Bravo charges $45.",
		"",
		nil,
		nil,
	)
	if reason != "" {
		t.Errorf("marketing should not be suppressed for competitor pricing content, got %q", reason)
	}
}

// TestSuppressBroadcastReasonBlocksFEOnPureBackend ensures the suppression still
// fires for genuine hard domain mismatches (FE agent talking about DB schemas).
func TestSuppressBroadcastReasonBlocksFEOnPureBackend(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"The database migration adds a new index on the users table for faster auth queries.",
		"",
		nil,
		nil,
	)
	if reason == "" {
		t.Error("FE agent should be suppressed for pure backend/database content")
	}
}

func TestSuppressBroadcastReasonAllowsRecentlyCompletedOwnedTaskBroadcast(t *testing.T) {
	reason := suppressBroadcastReason(
		"gtm",
		"Here is the locked monetization ladder and CTA routing for the launch packet.",
		"",
		nil,
		[]brokerTaskSummary{
			{
				ID:        "task-4",
				Owner:     "gtm",
				Status:    "done",
				Title:     "Lock the launch monetization path for the flagship WUPHF episode",
				Details:   "Finalize monetization ladder and CTA routing for the first live upload.",
				UpdatedAt: time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			},
		},
	)
	if reason != "" {
		t.Fatalf("expected recently completed owned-task broadcast to be allowed, got %q", reason)
	}
}

func TestSuppressBroadcastReasonBlocksStaleCompletedOwnedTaskBroadcast(t *testing.T) {
	reason := suppressBroadcastReason(
		"gtm",
		"Here is the locked monetization ladder and CTA routing for the launch packet.",
		"",
		nil,
		[]brokerTaskSummary{
			{
				ID:        "task-4",
				Owner:     "gtm",
				Status:    "done",
				Title:     "Lock the launch monetization path for the flagship WUPHF episode",
				Details:   "Finalize monetization ladder and CTA routing for the first live upload.",
				UpdatedAt: time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
			},
		},
	)
	if reason == "" {
		t.Fatal("expected stale completed task to stop authorizing out-of-domain broadcast")
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
	ctx := context.Background()
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	called := 0
	prev := reconfigureOfficeSessionFn
	reconfigureOfficeSessionFn = func() error {
		called++
		return nil
	}
	defer func() { reconfigureOfficeSessionFn = prev }()

	if _, _, err := handleTeamChannel(ctx, nil, TeamChannelArgs{
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

func TestHandleTeamChannelCreateRequiresExplicitSlug(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_CHANNEL", "general")

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	result, _, err := handleTeamChannel(context.Background(), nil, TeamChannelArgs{
		Action:      "create",
		Name:        "launch",
		Description: "Launch execution channel",
		Members:     []string{"pm", "fe"},
		MySlug:      "ceo",
	})
	if err != nil {
		t.Fatalf("handleTeamChannel returned unexpected error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected tool error result when slug is omitted, got %+v", result)
	}
	if got := textFromResult(t, result); !strings.Contains(got, "channel slug is required") {
		t.Fatalf("expected explicit slug message, got %q", got)
	}

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/channels", b.Addr()), nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch channels: %v", err)
	}
	defer resp.Body.Close()

	var channelsResult struct {
		Channels []struct {
			Slug string `json:"slug"`
		} `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channelsResult); err != nil {
		t.Fatalf("decode channels: %v", err)
	}
	if len(channelsResult.Channels) != 1 || channelsResult.Channels[0].Slug != "general" {
		t.Fatalf("expected only general channel to remain, got %+v", channelsResult.Channels)
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

func TestHandleTeamMemoryWriteAndQueryPrivate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if _, _, err := handleTeamMemoryWrite(context.Background(), nil, TeamMemoryWriteArgs{
		Key:        "launch-brief",
		Title:      "Launch brief",
		Content:    "Customer Alpha needs the launch deck cut down to one page.",
		Visibility: "private",
		MySlug:     "pm",
	}); err != nil {
		t.Fatalf("handleTeamMemoryWrite: %v", err)
	}

	result, _, err := handleTeamMemoryQuery(context.Background(), nil, TeamMemoryQueryArgs{
		Query:  "launch deck",
		Scope:  "private",
		MySlug: "pm",
	})
	if err != nil {
		t.Fatalf("handleTeamMemoryQuery: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Private memory:") || !strings.Contains(text, "launch-brief") || !strings.Contains(text, "Launch brief") {
		t.Fatalf("expected private memory hit, got %q", text)
	}
}

func TestHandleTeamMemoryWriteHintsPromotionForDurableNote(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	result, _, err := handleTeamMemoryWrite(context.Background(), nil, TeamMemoryWriteArgs{
		Key:        "launch-brief",
		Title:      "Launch brief",
		Content:    "Approved final launch positioning for Customer Alpha. This is the canonical story for the rollout.",
		Visibility: "private",
		MySlug:     "pm",
	})
	if err != nil {
		t.Fatalf("handleTeamMemoryWrite: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Saved private note launch-brief.") {
		t.Fatalf("expected save confirmation, got %q", text)
	}
	if !strings.Contains(text, "team_memory_promote key=launch-brief") {
		t.Fatalf("expected promotion hint, got %q", text)
	}
}

func TestHandleTeamMemoryQueryAutoIncludesSharedNexMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", "nex")
	t.Setenv("WUPHF_API_KEY", "nex-test-key")
	t.Setenv("WUPHF_NO_NEX", "")

	var askedQuery string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/developers/v1/context/ask":
			askedQuery = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"answer":"Shared launch history from Nex."}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	t.Setenv("WUPHF_DEV_URL", apiServer.URL)

	binDir := t.TempDir()
	nexMCP := filepath.Join(binDir, "nex-mcp")
	if err := os.WriteFile(nexMCP, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake nex-mcp: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if _, _, err := handleTeamMemoryWrite(context.Background(), nil, TeamMemoryWriteArgs{
		Key:        "launch-brief",
		Title:      "Launch brief",
		Content:    "Private note for the PM.",
		Visibility: "private",
		MySlug:     "pm",
	}); err != nil {
		t.Fatalf("handleTeamMemoryWrite: %v", err)
	}

	result, _, err := handleTeamMemoryQuery(context.Background(), nil, TeamMemoryQueryArgs{
		Query:  "launch",
		Scope:  "auto",
		MySlug: "pm",
	})
	if err != nil {
		t.Fatalf("handleTeamMemoryQuery: %v", err)
	}
	text := textFromResult(t, result)
	if askedQuery == "" {
		t.Fatal("expected shared Nex query to be called")
	}
	if !strings.Contains(text, "Private memory:") || !strings.Contains(text, "Shared memory:") || !strings.Contains(text, "Shared launch history from Nex.") {
		t.Fatalf("expected both private and shared memory hits, got %q", text)
	}
}

func TestHandleTeamMemoryPromoteWritesSharedNexMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", "nex")
	t.Setenv("WUPHF_API_KEY", "nex-test-key")
	t.Setenv("WUPHF_NO_NEX", "")

	var postedBody string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/developers/v1/context/text":
			body := new(bytes.Buffer)
			_, _ = body.ReadFrom(r.Body)
			postedBody = body.String()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	t.Setenv("WUPHF_DEV_URL", apiServer.URL)

	binDir := t.TempDir()
	nexMCP := filepath.Join(binDir, "nex-mcp")
	if err := os.WriteFile(nexMCP, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake nex-mcp: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if _, _, err := handleTeamMemoryWrite(context.Background(), nil, TeamMemoryWriteArgs{
		Key:        "launch-brief",
		Title:      "Launch brief",
		Content:    "Approved final launch positioning for Customer Alpha.",
		Visibility: "private",
		MySlug:     "pm",
	}); err != nil {
		t.Fatalf("handleTeamMemoryWrite: %v", err)
	}

	result, _, err := handleTeamMemoryPromote(context.Background(), nil, TeamMemoryPromoteArgs{
		Key:    "launch-brief",
		MySlug: "pm",
	})
	if err != nil {
		t.Fatalf("handleTeamMemoryPromote: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Promoted private note launch-brief") {
		t.Fatalf("expected promote confirmation, got %q", text)
	}
	if !strings.Contains(postedBody, "Launch brief") || !strings.Contains(postedBody, "Approved final launch positioning") {
		t.Fatalf("expected promoted content in Nex write, got %q", postedBody)
	}
}

func TestHandleTeamMemoryQuerySharedSuggestsRoutingHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", "nex")
	t.Setenv("WUPHF_API_KEY", "nex-test-key")
	t.Setenv("WUPHF_NO_NEX", "")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/developers/v1/context/ask":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"answer":"Author: @pm\nApproved final launch positioning for Customer Alpha."}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	t.Setenv("WUPHF_DEV_URL", apiServer.URL)

	binDir := t.TempDir()
	nexMCP := filepath.Join(binDir, "nex-mcp")
	if err := os.WriteFile(nexMCP, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake nex-mcp: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ctx := context.Background()
	ensureBrokerMembers(t, ctx, "pm", "fe")

	result, _, err := handleTeamMemoryQuery(ctx, nil, TeamMemoryQueryArgs{
		Query:  "launch positioning",
		Scope:  "shared",
		MySlug: "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamMemoryQuery: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Shared Nex memory:") {
		t.Fatalf("expected shared-memory section, got %q", text)
	}
	if !strings.Contains(text, "Routing hints:") || !strings.Contains(text, "@pm") {
		t.Fatalf("expected routing hint toward @pm, got %q", text)
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

func TestHandleTeamPollScopesMessagesForNonCEO(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	for _, msg := range []map[string]any{
		{"channel": "general", "from": "you", "content": "Human wants a quick update."},
		{"channel": "general", "from": "pm", "content": "Unrelated PM planning note."},
		{"channel": "general", "from": "ceo", "content": "Frontend, tighten the CTA copy.", "tagged": []string{"fe"}},
		{"channel": "general", "from": "fe", "content": "I am on the CTA copy now."},
	} {
		if err := brokerPostJSON(ctx, "/messages", msg, nil); err != nil {
			t.Fatalf("post message: %v", err)
		}
	}

	result, _, err := handleTeamPoll(ctx, nil, TeamPollArgs{Channel: "general", MySlug: "fe"})
	if err != nil {
		t.Fatalf("handleTeamPoll: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Frontend, tighten the CTA copy.") {
		t.Fatalf("expected tagged CEO direction in %q", text)
	}
	if !strings.Contains(text, "I am on the CTA copy now.") {
		t.Fatalf("expected FE outbox message in %q", text)
	}
	if strings.Contains(text, "Unrelated PM planning note.") {
		t.Fatalf("did not expect unrelated PM note in scoped poll %q", text)
	}
}

func TestSummarizeTaskRuntimeIncludesIsolationCounts(t *testing.T) {
	summary := summarizeTaskRuntime("general", []brokerTaskSummary{
		{
			ID:             "task-1",
			Owner:          "fe",
			Status:         "in_progress",
			ExecutionMode:  "local_worktree",
			WorktreePath:   "/tmp/wuphf-task-1",
			WorktreeBranch: "feat/task-1",
			Title:          "Implement landing page",
		},
		{
			ID:          "task-2",
			Owner:       "pm",
			Status:      "review",
			ReviewState: "ready_for_review",
			Title:       "Review launch scope",
		},
	})

	if !strings.Contains(summary, "Running tasks: 2 of 2") {
		t.Fatalf("expected running count in %q", summary)
	}
	if !strings.Contains(summary, "Isolated worktrees: 1") {
		t.Fatalf("expected isolation count in %q", summary)
	}
	if !strings.Contains(summary, "branch feat/task-1") {
		t.Fatalf("expected worktree branch in %q", summary)
	}
	if !strings.Contains(summary, "/tmp/wuphf-task-1") {
		t.Fatalf("expected worktree path in %q", summary)
	}
	if !strings.Contains(summary, "working_directory") {
		t.Fatalf("expected working_directory guidance in %q", summary)
	}
}

func TestHandleTeamTaskStatusReportsWorktreeIsolation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "fe")

	payload := map[string]any{
		"action":          "create",
		"channel":         "general",
		"title":           "Implement worktree task",
		"owner":           "fe",
		"created_by":      "ceo",
		"execution_mode":  "local_worktree",
		"worktree_path":   "/tmp/wuphf-task-42",
		"worktree_branch": "task/42",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal task payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/tasks", b.Addr()), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating task, got %d", resp.StatusCode)
	}

	result, _, err := handleTeamTaskStatus(ctx, nil, TeamTasksArgs{
		Channel: "general",
		MySlug:  "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamTaskStatus: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Running tasks: 1 of 1") {
		t.Fatalf("expected runtime count in %q", text)
	}
	if !strings.Contains(text, "Isolated worktrees: 1") {
		t.Fatalf("expected isolation count in %q", text)
	}
	if !strings.Contains(text, "branch wuphf-") {
		t.Fatalf("expected worktree branch in %q", text)
	}
	if !strings.Contains(text, ".wuphf/task-worktrees/") {
		t.Fatalf("expected worktree path in %q", text)
	}
	if !strings.Contains(text, "working_directory") {
		t.Fatalf("expected working_directory guidance in %q", text)
	}

	tasksResult, _, err := handleTeamTasks(ctx, nil, TeamTasksArgs{
		Channel: "general",
		MySlug:  "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamTasks: %v", err)
	}
	tasksText := textFromResult(t, tasksResult)
	if !strings.Contains(tasksText, "Current team tasks:") {
		t.Fatalf("expected task listing header in %q", tasksText)
	}
	if !strings.Contains(tasksText, "branch wuphf-") {
		t.Fatalf("expected worktree branch in task listing %q", tasksText)
	}
	if !strings.Contains(tasksText, "working_directory ") || !strings.Contains(tasksText, ".wuphf/task-worktrees/") {
		t.Fatalf("expected working_directory path in task listing %q", tasksText)
	}
}

func TestHandleTeamTaskReturnsWorktreeGuidance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "fe")

	payload := map[string]any{
		"action":          "create",
		"channel":         "general",
		"title":           "Implement worktree task",
		"owner":           "fe",
		"created_by":      "ceo",
		"execution_mode":  "local_worktree",
		"worktree_path":   "/tmp/wuphf-task-99",
		"worktree_branch": "task/99",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal task payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/tasks", b.Addr()), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating task, got %d", resp.StatusCode)
	}

	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task: %v", err)
	}

	result, _, err := handleTeamTask(ctx, nil, TeamTaskArgs{
		Action:  "review",
		Channel: "general",
		ID:      created.Task.ID,
		MySlug:  "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamTask: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "branch wuphf-") {
		t.Fatalf("expected worktree branch in %q", text)
	}
	if !strings.Contains(text, "working_directory ") || !strings.Contains(text, ".wuphf/task-worktrees/") {
		t.Fatalf("expected working_directory guidance in %q", text)
	}
}

func TestHandleTeamRuntimeStateIncludesRecoveryAndCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "fe")

	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "general",
		"from":    "ceo",
		"content": "Need your approval before shipping.",
	}, nil); err != nil {
		t.Fatalf("post message: %v", err)
	}

	if err := brokerPostJSON(ctx, "/tasks", map[string]any{
		"action":          "create",
		"channel":         "general",
		"title":           "Ship release candidate",
		"owner":           "fe",
		"created_by":      "ceo",
		"execution_mode":  "local_worktree",
		"worktree_path":   "/tmp/wuphf-task-77",
		"worktree_branch": "task/77",
	}, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":     "approval",
		"channel":  "general",
		"from":     "ceo",
		"title":    "Approve release",
		"question": "Should we ship the release candidate?",
		"blocking": true,
		"required": true,
		"secret":   false,
		"reply_to": "",
		"options":  []map[string]any{{"id": "yes", "label": "Ship it"}},
	}, nil); err != nil {
		t.Fatalf("create request: %v", err)
	}

	result, structured, err := handleTeamRuntimeState(ctx, nil, TeamRuntimeStateArgs{
		Channel:      "general",
		MySlug:       "fe",
		MessageLimit: 10,
	})
	if err != nil {
		t.Fatalf("handleTeamRuntimeState: %v", err)
	}
	text := textFromResult(t, result)
	for _, want := range []string{
		"Runtime state for #general",
		"Pending human requests: 1",
		"Current focus: Approve release from @ceo.",
		"working_directory ",
		"Runtime capabilities:",
		"Memory backend [info]: Nex is disabled for this run, so the office is operating without an external memory backend.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in %q", want, text)
		}
	}

	snapshot, ok := structured.(team.RuntimeSnapshot)
	if !ok {
		t.Fatalf("expected structured runtime snapshot, got %T", structured)
	}
	if snapshot.Channel != "general" {
		t.Fatalf("expected general channel, got %q", snapshot.Channel)
	}
	if len(snapshot.Tasks) != 1 || !strings.Contains(snapshot.Tasks[0].WorktreePath, ".wuphf/task-worktrees/") {
		t.Fatalf("unexpected runtime tasks: %+v", snapshot.Tasks)
	}
	if len(snapshot.Requests) == 0 || snapshot.Requests[0].Title != "Approve release" {
		t.Fatalf("unexpected runtime requests: %+v", snapshot.Requests)
	}
	if _, ok := snapshot.Registry.Entry(team.CapabilityKeyConnections); !ok {
		t.Fatalf("expected connections readiness in runtime registry, got %+v", snapshot.Registry.Entries)
	}
	if _, ok := snapshot.Registry.Entry(team.CapabilityKeyOfficeActions); !ok {
		t.Fatalf("expected office actions readiness in runtime registry, got %+v", snapshot.Registry.Entries)
	}
}

func TestHandleTeamRequestDefaultsApprovalOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if _, _, err := handleTeamRequest(context.Background(), nil, TeamRequestArgs{
		Kind:     "approval",
		Channel:  "general",
		Question: "Ship this?",
		MySlug:   "ceo",
	}); err != nil {
		t.Fatalf("handleTeamRequest: %v", err)
	}

	var result brokerRequestsResponse
	if err := brokerGetJSON(context.Background(), "/requests?channel=general", &result); err != nil {
		t.Fatalf("fetch requests: %v", err)
	}
	if len(result.Requests) != 1 {
		t.Fatalf("expected one request, got %+v", result.Requests)
	}
	req := result.Requests[0]
	if req.RecommendedID != "approve" {
		t.Fatalf("expected recommended approval option, got %q", req.RecommendedID)
	}
	if len(req.Options) != 5 {
		t.Fatalf("expected default approval options, got %+v", req.Options)
	}
	found := false
	for _, option := range req.Options {
		if option.ID == "approve_with_note" {
			found = option.RequiresText && strings.TrimSpace(option.TextHint) != ""
		}
	}
	if !found {
		t.Fatalf("expected approve_with_note option with text guidance, got %+v", req.Options)
	}
}

func TestHandleTeamPollUsesAgentScopedTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	for _, msg := range []map[string]any{
		{"channel": "general", "from": "you", "content": "Frontend, should we ship this?", "tagged": []string{"fe"}},
		{"channel": "general", "from": "pm", "content": "Unrelated roadmap chatter."},
		{"channel": "general", "from": "ceo", "content": "Keep scope tight and focus on signup."},
		{"channel": "general", "from": "fe", "content": "I can take the signup work."},
	} {
		if err := brokerPostJSON(ctx, "/messages", msg, nil); err != nil {
			t.Fatalf("post message: %v", err)
		}
	}

	result, _, err := handleTeamPoll(ctx, nil, TeamPollArgs{
		Channel: "general",
		MySlug:  "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamPoll: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Keep scope tight and focus on signup.") {
		t.Fatalf("expected CEO context in scoped transcript, got %q", text)
	}
	if strings.Contains(text, "Unrelated roadmap chatter.") {
		t.Fatalf("did not expect unrelated PM chatter in scoped transcript, got %q", text)
	}
}

func TestHandleTeamBroadcastDefaultsToLatestTaggedChannelAndThread(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	if err := brokerPostJSON(ctx, "/channels", map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch work",
		"members":     []string{"fe", "pm"},
		"created_by":  "ceo",
	}, nil); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "launch",
		"from":    "ceo",
		"content": "Frontend, tighten the launch CTA in this thread.",
		"tagged":  []string{"fe"},
	}, nil); err != nil {
		t.Fatalf("post launch message: %v", err)
	}

	result, _, err := handleTeamBroadcast(ctx, nil, TeamBroadcastArgs{
		MySlug:  "fe",
		Content: "On it. I will keep this in the launch thread.",
	})
	if err != nil {
		t.Fatalf("handleTeamBroadcast: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Posted to #launch as @fe") {
		t.Fatalf("expected launch channel in %q", text)
	}
	if !strings.Contains(text, "in reply to msg-1") {
		t.Fatalf("expected reply target in %q", text)
	}

	var launch brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?channel=launch&limit=10", &launch); err != nil {
		t.Fatalf("fetch launch messages: %v", err)
	}
	if len(launch.Messages) != 2 {
		t.Fatalf("expected two launch messages, got %+v", launch.Messages)
	}
	got := launch.Messages[len(launch.Messages)-1]
	if got.From != "fe" || got.ReplyTo != "msg-1" {
		t.Fatalf("expected FE reply in launch thread, got %+v", got)
	}
}

func TestHandleTeamPollDefaultsToLatestTaggedChannel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	if err := brokerPostJSON(ctx, "/channels", map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch work",
		"members":     []string{"fe", "pm"},
		"created_by":  "ceo",
	}, nil); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "launch",
		"from":    "ceo",
		"content": "Frontend, review the launch thread.",
		"tagged":  []string{"fe"},
	}, nil); err != nil {
		t.Fatalf("post launch message: %v", err)
	}

	result, _, err := handleTeamPoll(ctx, nil, TeamPollArgs{MySlug: "fe"})
	if err != nil {
		t.Fatalf("handleTeamPoll: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "Channel #launch") {
		t.Fatalf("expected inferred launch channel in %q", text)
	}
	if !strings.Contains(text, "Frontend, review the launch thread.") {
		t.Fatalf("expected launch content in %q", text)
	}
}

func TestHandleTeamTaskUsesTaskChannelWhenIDGiven(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	if err := brokerPostJSON(ctx, "/channels", map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch work",
		"members":     []string{"fe", "pm"},
		"created_by":  "ceo",
	}, nil); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks", map[string]any{
		"action":     "create",
		"channel":    "launch",
		"title":      "Review launch CTA",
		"owner":      "fe",
		"created_by": "ceo",
		"thread_id":  "msg-launch",
	}, &created); err != nil {
		t.Fatalf("create task: %v", err)
	}

	result, _, err := handleTeamTask(ctx, nil, TeamTaskArgs{
		Action: "review",
		ID:     created.Task.ID,
		MySlug: "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamTask: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "in #launch") {
		t.Fatalf("expected task action to stay in launch, got %q", text)
	}
}

func TestHandleHumanMessageDefaultsToDirectReplyThreadInOneOnOneMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_ONE_ON_ONE", "1")
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm")
	if err := b.SetSessionMode(team.SessionModeOneOnOne, "pm"); err != nil {
		t.Fatalf("set session mode: %v", err)
	}

	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "general",
		"from":    "you",
		"content": "Can you send me the latest product answer?",
	}, nil); err != nil {
		t.Fatalf("post direct human message: %v", err)
	}

	result, _, err := handleHumanMessage(ctx, nil, HumanMessageArgs{
		MySlug:  "pm",
		Content: "Yes. Here is the latest product answer.",
	})
	if err != nil {
		t.Fatalf("handleHumanMessage: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "this direct session") {
		t.Fatalf("expected direct-session label in %q", text)
	}
	if !strings.Contains(text, "in reply to msg-1") {
		t.Fatalf("expected direct reply threading in %q", text)
	}
}

func TestHandleTeamInboxAndOutboxExposeOwnedTranscriptSlices(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	ensureBrokerMembers(t, ctx, "pm", "fe")

	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "general",
		"from":    "ceo",
		"content": "Frontend, take the signup thread.",
	}, nil); err != nil {
		t.Fatalf("post ceo message: %v", err)
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel":  "general",
		"from":     "fe",
		"content":  "I can own the signup thread.",
		"reply_to": "msg-1",
	}, nil); err != nil {
		t.Fatalf("post own message: %v", err)
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel":  "general",
		"from":     "pm",
		"content":  "Please include pricing copy in that thread.",
		"reply_to": "msg-2",
	}, nil); err != nil {
		t.Fatalf("post thread reply: %v", err)
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "general",
		"from":    "fe",
		"content": "Shipped the initial branch.",
	}, nil); err != nil {
		t.Fatalf("post own top-level message: %v", err)
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": "general",
		"from":    "pm",
		"content": "Unrelated roadmap chatter.",
	}, nil); err != nil {
		t.Fatalf("post unrelated message: %v", err)
	}

	inboxResult, _, err := handleTeamInbox(ctx, nil, TeamPollArgs{Channel: "general", MySlug: "fe"})
	if err != nil {
		t.Fatalf("handleTeamInbox: %v", err)
	}
	inboxText := textFromResult(t, inboxResult)
	if !strings.Contains(inboxText, "Inbox for @fe in #general") {
		t.Fatalf("expected inbox heading, got %q", inboxText)
	}
	if !strings.Contains(inboxText, "Please include pricing copy in that thread.") {
		t.Fatalf("expected thread reply in inbox, got %q", inboxText)
	}
	if strings.Contains(inboxText, "Shipped the initial branch.") || strings.Contains(inboxText, "Unrelated roadmap chatter.") {
		t.Fatalf("unexpected content in inbox slice: %q", inboxText)
	}

	outboxResult, _, err := handleTeamOutbox(context.Background(), nil, TeamPollArgs{Channel: "general", MySlug: "fe"})
	if err != nil {
		t.Fatalf("handleTeamOutbox: %v", err)
	}
	outboxText := textFromResult(t, outboxResult)
	if !strings.Contains(outboxText, "Outbox for @fe in #general") {
		t.Fatalf("expected outbox heading, got %q", outboxText)
	}
	if !strings.Contains(outboxText, "Shipped the initial branch.") {
		t.Fatalf("expected authored message in outbox, got %q", outboxText)
	}
	if strings.Contains(outboxText, "Frontend, take the signup thread.") || strings.Contains(outboxText, "Please include pricing copy in that thread.") {
		t.Fatalf("unexpected non-authored content in outbox slice: %q", outboxText)
	}
}

func TestDetectUntaggedMentions(t *testing.T) {
	// No @-mentions → nothing flagged
	if got := detectUntaggedMentions("Hello there, nice work!", nil); len(got) != 0 {
		t.Fatalf("expected no mentions, got %v", got)
	}

	// @-mention that IS in tagged → not flagged
	if got := detectUntaggedMentions("@engineering please write this", []string{"engineering"}); len(got) != 0 {
		t.Fatalf("expected no untagged, got %v", got)
	}

	// @-mention NOT in tagged → flagged
	got := detectUntaggedMentions("@engineering please write this", nil)
	if len(got) != 1 || got[0] != "@engineering" {
		t.Fatalf("expected @engineering flagged, got %v", got)
	}

	// Known non-agent @-references → not flagged
	nonAgents := []string{"you", "human", "nex", "team", "everyone"}
	for _, na := range nonAgents {
		content := fmt.Sprintf("@%s please reply", na)
		if got := detectUntaggedMentions(content, nil); len(got) != 0 {
			t.Fatalf("@%s should not be flagged, got %v", na, got)
		}
	}

	// Multiple @-mentions, one tagged → only untagged one flagged
	got = detectUntaggedMentions("@ceo @marketing please coordinate", []string{"ceo"})
	if len(got) != 1 || got[0] != "@marketing" {
		t.Fatalf("expected only @marketing untagged, got %v", got)
	}

	// Trailing punctuation stripped correctly
	got = detectUntaggedMentions("@marketing, please write a draft.", nil)
	if len(got) != 1 || got[0] != "@marketing" {
		t.Fatalf("expected @marketing after stripping punctuation, got %v", got)
	}
}

// TestHandleTeamPlanCreatesDependentBlockedTasks verifies the team_plan MCP tool
// round-trips through the HTTP broker, creates both tasks, and marks the dependent
// task as BLOCKED when its dependency is still open.
func TestHandleTeamPlanCreatesDependentBlockedTasks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	result, _, err := handleTeamPlan(context.Background(), nil, TeamPlanArgs{
		Channel: "general",
		MySlug:  "ceo",
		Tasks: []struct {
			Title         string   `json:"title" jsonschema:"Task title"`
			Assignee      string   `json:"assignee" jsonschema:"Agent slug to own this task"`
			Details       string   `json:"details,omitempty" jsonschema:"Optional task details"`
			TaskType      string   `json:"task_type,omitempty" jsonschema:"Optional task type such as research, feature, launch, follow_up, bugfix, or incident"`
			ExecutionMode string   `json:"execution_mode,omitempty" jsonschema:"Optional execution mode such as office or local_worktree"`
			DependsOn     []string `json:"depends_on,omitempty" jsonschema:"Titles or IDs of tasks this depends on"`
		}{
			{Title: "Research competitors", Assignee: "research"},
			{Title: "Write positioning copy", Assignee: "marketing", DependsOn: []string{"Research competitors"}},
		},
	})
	if err != nil {
		t.Fatalf("handleTeamPlan: %v", err)
	}
	text := textFromResult(t, result)

	if !strings.Contains(text, "Created 2 tasks") {
		t.Fatalf("expected 2 tasks created, got %q", text)
	}
	if !strings.Contains(text, "Research competitors") {
		t.Fatalf("expected research task in output, got %q", text)
	}
	if !strings.Contains(text, "Write positioning copy") {
		t.Fatalf("expected marketing task in output, got %q", text)
	}
	// The dependent task must be marked BLOCKED in the output.
	if !strings.Contains(text, "BLOCKED") {
		t.Fatalf("expected BLOCKED flag for dependent task, got %q", text)
	}
	// The first task (no deps) must NOT be blocked.
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Research competitors") && strings.Contains(line, "BLOCKED") {
			t.Fatalf("research task should not be BLOCKED: %q", line)
		}
	}
}

func TestHandleTeamPlanPreservesTaskMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	_, _, err := handleTeamPlan(context.Background(), nil, TeamPlanArgs{
		Channel: "general",
		MySlug:  "ceo",
		Tasks: []struct {
			Title         string   `json:"title" jsonschema:"Task title"`
			Assignee      string   `json:"assignee" jsonschema:"Agent slug to own this task"`
			Details       string   `json:"details,omitempty" jsonschema:"Optional task details"`
			TaskType      string   `json:"task_type,omitempty" jsonschema:"Optional task type such as research, feature, launch, follow_up, bugfix, or incident"`
			ExecutionMode string   `json:"execution_mode,omitempty" jsonschema:"Optional execution mode such as office or local_worktree"`
			DependsOn     []string `json:"depends_on,omitempty" jsonschema:"Titles or IDs of tasks this depends on"`
		}{
			{Title: "Build the studio control plane", Assignee: "eng", TaskType: "feature", ExecutionMode: "local_worktree"},
			{Title: "Package the launch slate", Assignee: "gtm", TaskType: "launch", ExecutionMode: "office"},
		},
	})
	if err != nil {
		t.Fatalf("handleTeamPlan: %v", err)
	}

	var result brokerTasksResponse
	if err := brokerGetJSON(context.Background(), "/tasks?channel=general&include_done=true", &result); err != nil {
		t.Fatalf("fetch tasks: %v", err)
	}

	found := map[string]brokerTaskSummary{}
	for _, task := range result.Tasks {
		found[task.Title] = task
	}
	if got := found["Build the studio control plane"]; got.TaskType != "feature" || got.ExecutionMode != "local_worktree" {
		t.Fatalf("expected feature/local_worktree metadata, got %+v", got)
	}
	if got := found["Package the launch slate"]; got.TaskType != "launch" || got.ExecutionMode != "office" {
		t.Fatalf("expected launch/office metadata, got %+v", got)
	}
}

func TestHandleTeamTaskCreatePreservesTaskMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	_, _, err := handleTeamTask(context.Background(), nil, TeamTaskArgs{
		Action:        "create",
		Channel:       "general",
		Title:         "Implement studio foundations",
		Owner:         "eng",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
		MySlug:        "ceo",
	})
	if err != nil {
		t.Fatalf("handleTeamTask: %v", err)
	}

	var result brokerTasksResponse
	if err := brokerGetJSON(context.Background(), "/tasks?channel=general&include_done=true", &result); err != nil {
		t.Fatalf("fetch tasks: %v", err)
	}

	for _, task := range result.Tasks {
		if task.Title != "Implement studio foundations" {
			continue
		}
		if task.TaskType != "feature" || task.ExecutionMode != "local_worktree" {
			t.Fatalf("expected feature/local_worktree metadata, got %+v", task)
		}
		return
	}
	t.Fatal("expected created task to be present")
}
