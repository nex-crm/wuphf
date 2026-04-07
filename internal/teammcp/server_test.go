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

func TestSuppressBroadcastReasonAllowsAfterCEOReply(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"I can help with the frontend piece.",
		"msg-1",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "What should we do here?"},
			{ID: "msg-2", From: "ceo", Content: "PM owns this. Let's keep scope tight.", ReplyTo: "msg-1"},
		},
		nil,
	)
	if reason != "" {
		t.Fatalf("expected CEO reply to NOT suppress specialist, got %q", reason)
	}
}

func TestSuppressBroadcastReasonAllowsNoClearNeed(t *testing.T) {
	reason := suppressBroadcastReason(
		"fe",
		"I have thoughts on this.",
		"",
		[]brokerMessage{
			{ID: "msg-1", From: "you", Content: "What do you all think about our strategy?"},
		},
		nil,
	)
	if reason != "" {
		t.Fatalf("expected 'no clear need' to NOT suppress, got %q", reason)
	}
}
