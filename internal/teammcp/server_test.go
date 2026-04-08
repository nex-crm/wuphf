package teammcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nex-crm/wuphf/internal/team"
)

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

func TestHandleTeamPollScopesMessagesForNonCEO(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	for _, msg := range []map[string]any{
		{"channel": "general", "from": "you", "content": "Human wants a quick update."},
		{"channel": "general", "from": "pm", "content": "Unrelated PM planning note."},
		{"channel": "general", "from": "ceo", "content": "Frontend, tighten the CTA copy.", "tagged": []string{"fe"}},
		{"channel": "general", "from": "fe", "content": "I am on the CTA copy now."},
	} {
		if err := brokerPostJSON(context.Background(), "/messages", msg, nil); err != nil {
			t.Fatalf("post message: %v", err)
		}
	}

	result, _, err := handleTeamPoll(context.Background(), nil, TeamPollArgs{Channel: "general", MySlug: "fe"})
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

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

	result, _, err := handleTeamTaskStatus(context.Background(), nil, TeamTasksArgs{
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
	if !strings.Contains(text, "branch task/42") {
		t.Fatalf("expected worktree branch in %q", text)
	}
	if !strings.Contains(text, "/tmp/wuphf-task-42") {
		t.Fatalf("expected worktree path in %q", text)
	}
	if !strings.Contains(text, "working_directory") {
		t.Fatalf("expected working_directory guidance in %q", text)
	}

	tasksResult, _, err := handleTeamTasks(context.Background(), nil, TeamTasksArgs{
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
	if !strings.Contains(tasksText, "branch task/42") {
		t.Fatalf("expected worktree branch in task listing %q", tasksText)
	}
	if !strings.Contains(tasksText, "working_directory /tmp/wuphf-task-42") {
		t.Fatalf("expected working_directory path in task listing %q", tasksText)
	}
}

func TestHandleTeamTaskReturnsWorktreeGuidance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

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

	result, _, err := handleTeamTask(context.Background(), nil, TeamTaskArgs{
		Action:  "review",
		Channel: "general",
		ID:      created.Task.ID,
		MySlug:  "fe",
	})
	if err != nil {
		t.Fatalf("handleTeamTask: %v", err)
	}
	text := textFromResult(t, result)
	if !strings.Contains(text, "branch task/99") {
		t.Fatalf("expected worktree branch in %q", text)
	}
	if !strings.Contains(text, "working_directory /tmp/wuphf-task-99") {
		t.Fatalf("expected working_directory guidance in %q", text)
	}
}

func TestHandleTeamRuntimeStateIncludesRecoveryAndCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "general",
		"from":    "ceo",
		"content": "Need your approval before shipping.",
	}, nil); err != nil {
		t.Fatalf("post message: %v", err)
	}

	if err := brokerPostJSON(context.Background(), "/tasks", map[string]any{
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

	if err := brokerPostJSON(context.Background(), "/requests", map[string]any{
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

	result, structured, err := handleTeamRuntimeState(context.Background(), nil, TeamRuntimeStateArgs{
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
		"working_directory /tmp/wuphf-task-77",
		"Runtime capabilities:",
		"nex [info]: Disabled for this session with --no-nex.",
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
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].WorktreePath != "/tmp/wuphf-task-77" {
		t.Fatalf("unexpected runtime tasks: %+v", snapshot.Tasks)
	}
	if len(snapshot.Requests) == 0 || snapshot.Requests[0].Title != "Approve release" {
		t.Fatalf("unexpected runtime requests: %+v", snapshot.Requests)
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	for _, msg := range []map[string]any{
		{"channel": "general", "from": "you", "content": "Frontend, should we ship this?", "tagged": []string{"fe"}},
		{"channel": "general", "from": "pm", "content": "Unrelated roadmap chatter."},
		{"channel": "general", "from": "ceo", "content": "Keep scope tight and focus on signup."},
		{"channel": "general", "from": "fe", "content": "I can take the signup work."},
	} {
		if err := brokerPostJSON(context.Background(), "/messages", msg, nil); err != nil {
			t.Fatalf("post message: %v", err)
		}
	}

	result, _, err := handleTeamPoll(context.Background(), nil, TeamPollArgs{
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if err := brokerPostJSON(context.Background(), "/channels", map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch work",
		"members":     []string{"fe", "pm"},
		"created_by":  "ceo",
	}, nil); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "launch",
		"from":    "ceo",
		"content": "Frontend, tighten the launch CTA in this thread.",
		"tagged":  []string{"fe"},
	}, nil); err != nil {
		t.Fatalf("post launch message: %v", err)
	}

	result, _, err := handleTeamBroadcast(context.Background(), nil, TeamBroadcastArgs{
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
	if err := brokerGetJSON(context.Background(), "/messages?channel=launch&limit=10", &launch); err != nil {
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if err := brokerPostJSON(context.Background(), "/channels", map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch work",
		"members":     []string{"fe", "pm"},
		"created_by":  "ceo",
	}, nil); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "launch",
		"from":    "ceo",
		"content": "Frontend, review the launch thread.",
		"tagged":  []string{"fe"},
	}, nil); err != nil {
		t.Fatalf("post launch message: %v", err)
	}

	result, _, err := handleTeamPoll(context.Background(), nil, TeamPollArgs{MySlug: "fe"})
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if err := brokerPostJSON(context.Background(), "/channels", map[string]any{
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
	if err := brokerPostJSON(context.Background(), "/tasks", map[string]any{
		"action":     "create",
		"channel":    "launch",
		"title":      "Review launch CTA",
		"owner":      "fe",
		"created_by": "ceo",
		"thread_id":  "msg-launch",
	}, &created); err != nil {
		t.Fatalf("create task: %v", err)
	}

	result, _, err := handleTeamTask(context.Background(), nil, TeamTaskArgs{
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	if err := b.SetSessionMode(team.SessionModeOneOnOne, "pm"); err != nil {
		t.Fatalf("set session mode: %v", err)
	}

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "general",
		"from":    "you",
		"content": "Can you send me the latest product answer?",
	}, nil); err != nil {
		t.Fatalf("post direct human message: %v", err)
	}

	result, _, err := handleHumanMessage(context.Background(), nil, HumanMessageArgs{
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

	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "general",
		"from":    "ceo",
		"content": "Frontend, take the signup thread.",
	}, nil); err != nil {
		t.Fatalf("post ceo message: %v", err)
	}
	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel":  "general",
		"from":     "fe",
		"content":  "I can own the signup thread.",
		"reply_to": "msg-1",
	}, nil); err != nil {
		t.Fatalf("post own message: %v", err)
	}
	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel":  "general",
		"from":     "pm",
		"content":  "Please include pricing copy in that thread.",
		"reply_to": "msg-2",
	}, nil); err != nil {
		t.Fatalf("post thread reply: %v", err)
	}
	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "general",
		"from":    "fe",
		"content": "Shipped the initial branch.",
	}, nil); err != nil {
		t.Fatalf("post own top-level message: %v", err)
	}
	if err := brokerPostJSON(context.Background(), "/messages", map[string]any{
		"channel": "general",
		"from":    "pm",
		"content": "Unrelated roadmap chatter.",
	}, nil); err != nil {
		t.Fatalf("post unrelated message: %v", err)
	}

	inboxResult, _, err := handleTeamInbox(context.Background(), nil, TeamPollArgs{Channel: "general", MySlug: "fe"})
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
