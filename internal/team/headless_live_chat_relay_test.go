package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHeadlessLiveChatRelayPostsStreamedTextToChannel(t *testing.T) {
	b := newTestBroker(t)
	root, err := b.PostMessage("you", "general", "What is happening?", nil, "")
	if err != nil {
		t.Fatalf("post human message: %v", err)
	}
	l := &Launcher{broker: b}
	startedAt := time.Now().UTC().Add(-1 * time.Second)
	var logs []string
	relay := newHeadlessLiveChatRelay(
		l,
		"ceo",
		"general",
		fmt.Sprintf(`Reply using team_broadcast with reply_to_id "%s".`, root.ID),
		func(line string) { logs = append(logs, line) },
	)

	relay.OnText("I will check the live stream now.")

	msgs := b.ChannelMessages("general")
	if len(msgs) != 2 {
		t.Fatalf("expected human root + streamed agent message, got %d: %+v", len(msgs), msgs)
	}
	got := msgs[1]
	if got.From != "ceo" || got.Content != "I will check the live stream now." || got.ReplyTo != root.ID {
		t.Fatalf("unexpected streamed message: %+v", got)
	}
	if len(logs) != 1 {
		t.Fatalf("expected relay log entry, got %+v", logs)
	}

	_, posted, err := l.postHeadlessFinalMessageIfSilent("ceo", "general", "", "late summary", startedAt)
	if err != nil {
		t.Fatalf("fallback post: %v", err)
	}
	if posted {
		t.Fatal("expected final fallback to skip after streamed text was posted")
	}
}

func TestOpenAICompatLiveChatRelayDoesNotPostJSONToolShape(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.PostMessage("you", "general", "Please do the task.", nil, ""); err != nil {
		t.Fatalf("post human message: %v", err)
	}
	l := &Launcher{broker: b}
	relay := newHeadlessLiveChatRelay(l, "ceo", "general", "", nil)
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks, relay)

	st.onText(`{"name":`)
	st.onText(`"team_broadcast","arguments":`)
	st.onText(`{"channel":"general","content":"hello"}}`)
	st.onToolUseChunk("team_broadcast", `{"channel":"general"}`)

	msgs := b.ChannelMessages("general")
	if len(msgs) != 1 {
		t.Fatalf("expected only the human root; JSON tool stream leaked to chat: %+v", msgs)
	}
}

func TestHeadlessLiveChatRelayReportsIssueImmediately(t *testing.T) {
	b := newTestBroker(t)
	root, err := b.PostMessage("you", "general", "Open the browser.", nil, "")
	if err != nil {
		t.Fatalf("post human message: %v", err)
	}
	l := &Launcher{broker: b}
	relay := newHeadlessLiveChatRelay(
		l,
		"ceo",
		"general",
		fmt.Sprintf(`Reply using team_broadcast with reply_to_id "%s".`, root.ID),
		nil,
	)

	relay.ReportIssue("browser access is not available")

	msgs := b.ChannelMessages("general")
	if len(msgs) != 3 {
		t.Fatalf("expected issue to post immediately, got %+v", msgs)
	}
	got := msgs[1]
	if got.From != "ceo" || got.Kind != agentIssueMessageKind || got.ReplyTo != root.ID || got.Content != "Issue: browser access is not available" {
		t.Fatalf("unexpected issue message: %+v", got)
	}
	if approval := msgs[2]; approval.From != "system" || approval.Kind != "approval" || approval.EventID == "" || approval.Content == "" {
		t.Fatalf("expected inline approval recommendation, got %+v", approval)
	}
	if tasks := b.AllTasks(); len(tasks) != 0 {
		t.Fatalf("expected issue report to ask before creating self-heal task, got %+v", tasks)
	}
	requests := b.Requests("general", false)
	if len(requests) != 1 || requests[0].RecommendedID != "approve" {
		t.Fatalf("expected recommended approval request, got %+v", requests)
	}
}

func TestHeadlessLiveChatRelayFlushesBufferedTextBeforeIssue(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}
	relay := newHeadlessLiveChatRelay(l, "ceo", "general", "", nil)

	relay.OnText("I found context and will continue")
	relay.ReportIssue("browser access is not available")

	msgs := b.ChannelMessages("general")
	if len(msgs) != 3 {
		t.Fatalf("expected prose, issue, and approval messages, got %+v", msgs)
	}
	if got := msgs[0].Content; got != "I found context and will continue" {
		t.Fatalf("expected buffered prose to post first, got %q", got)
	}
	if msgs[1].Kind != agentIssueMessageKind {
		t.Fatalf("expected issue second, got %+v", msgs)
	}
}

func TestHeadlessLiveChatRelayPreservesWhitespaceChunks(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}
	relay := newHeadlessLiveChatRelay(l, "ceo", "general", "", nil)

	relay.OnText("Starting live")
	relay.OnText(" ")
	relay.OnText("now.")

	msgs := b.ChannelMessages("general")
	if len(msgs) != 1 {
		t.Fatalf("expected one flushed prose message, got %+v", msgs)
	}
	if got := msgs[0].Content; got != "Starting live now." {
		t.Fatalf("expected whitespace chunk to be preserved, got %q", got)
	}
}

func TestOpenAICompatToolErrorReportsIssueToChat(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.PostMessage("you", "general", "Use the browser.", nil, ""); err != nil {
		t.Fatalf("post human message: %v", err)
	}
	l := &Launcher{broker: b}
	relay := newHeadlessLiveChatRelay(l, "ceo", "general", "", nil)
	sinks := &fakeTurnSinks{}
	st := newOpenAICompatTurnState(sinks, relay)

	st.onToolResult("browser_open", "ERROR: browser access is not available", nil)

	msgs := b.ChannelMessages("general")
	if len(msgs) != 3 {
		t.Fatalf("expected tool error to post to chat, got %+v", msgs)
	}
	if got := msgs[1].Content; got != "Issue: ERROR: browser access is not available" {
		t.Fatalf("unexpected issue content: %q", got)
	}
	if msgs[1].Kind != agentIssueMessageKind {
		t.Fatalf("expected agent_issue kind, got %+v", msgs[1])
	}
}

func TestReportAgentIssueSuppressesStructuredPayloads(t *testing.T) {
	b := newTestBroker(t)

	_, _, posted, err := b.ReportAgentIssue("ceo", "general", "", `{"error":"browser access is not available"}`)
	if err != nil {
		t.Fatalf("report issue: %v", err)
	}
	if posted {
		t.Fatal("expected structured JSON payload to be suppressed")
	}
	if len(b.ChannelMessages("general")) != 0 {
		t.Fatalf("expected no chat messages, got %+v", b.ChannelMessages("general"))
	}
	if len(b.AgentIssues()) != 0 {
		t.Fatalf("expected no agent issues, got %+v", b.AgentIssues())
	}
}

func TestReportAgentIssueDedupesRepeatedStreamIssue(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}
	relay := newHeadlessLiveChatRelay(l, "ceo", "general", "", nil)

	relay.ReportIssue("browser access is not available")
	relay.ReportIssue("ERROR: browser access is not available")

	msgs := b.ChannelMessages("general")
	if len(msgs) != 2 {
		t.Fatalf("expected one issue message, got %+v", msgs)
	}
	issues := b.AgentIssues()
	if len(issues) != 1 || issues[0].Count != 2 {
		t.Fatalf("expected one counted issue, got %+v", issues)
	}
	requests := b.Requests("general", false)
	if len(requests) != 1 {
		t.Fatalf("expected one approval request, got %+v", requests)
	}
}

func TestReportAgentIssueAttachesActiveTaskAndWaitsForApproval(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Use the browser",
		Owner:     "eng",
		CreatedBy: "ceo",
		TaskType:  "feature",
	})
	if err != nil || reused {
		t.Fatalf("ensure task: %v reused=%v", err, reused)
	}

	if _, _, posted, err := b.ReportAgentIssue("eng", "general", "", "browser access is not available"); err != nil || !posted {
		t.Fatalf("report issue: posted=%v err=%v", posted, err)
	}

	issues := b.AgentIssues()
	if len(issues) != 1 || issues[0].TaskID != task.ID {
		t.Fatalf("expected issue attached to active task %s, got %+v", task.ID, issues)
	}
	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.Blocked || updated.Status != "in_progress" {
		t.Fatalf("expected active task not to be blocked before approval, got %+v", updated)
	}
	if len(b.AllTasks()) != 1 {
		t.Fatalf("expected no self-heal task before approval, got %+v", b.AllTasks())
	}
}

func TestApprovedAgentIssueCreatesSelfHealTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	if _, _, posted, err := b.ReportAgentIssue("eng", "general", "", "browser access is not available"); err != nil || !posted {
		t.Fatalf("report issue: posted=%v err=%v", posted, err)
	}
	requests := b.Requests("general", false)
	if len(requests) != 1 {
		t.Fatalf("expected approval request, got %+v", requests)
	}
	body, err := json.Marshal(map[string]any{
		"id":        requests[0].ID,
		"choice_id": "approve",
	})
	if err != nil {
		t.Fatalf("marshal request answer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/requests/answer", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request answer: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("answer approval: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected approval answer 200, got %d", resp.StatusCode)
	}

	var found bool
	for _, task := range b.AllTasks() {
		if task.Title == "Self-heal @eng runtime failure" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected approved self-heal task, got %+v", b.AllTasks())
	}
	issues := b.AgentIssues()
	if len(issues) != 1 || issues[0].SelfHealTaskID == "" {
		t.Fatalf("expected issue to record self-heal task, got %+v", issues)
	}
}

func TestAnsweredAgentIssueApprovalDoesNotCreateDuplicateRequest(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.requests = append(b.requests, humanInterview{
		ID:        "request-issue-1",
		Kind:      "approval",
		Status:    "answered",
		From:      "system",
		Channel:   "general",
		Title:     "Approve self-heal",
		Question:  "Proceed?",
		CreatedAt: now,
		UpdatedAt: now,
		Answered:  &interviewAnswer{ChoiceID: "approve", AnsweredAt: now},
	})
	b.agentIssues = append(b.agentIssues, agentIssueRecord{
		ID:                "issue-1",
		Agent:             "eng",
		Channel:           "general",
		Detail:            "browser access is not available",
		NormalizedKey:     normalizedAgentIssueKey("eng", "general", "browser access is not available"),
		ApprovalRequestID: "request-issue-1",
		Count:             1,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	b.ensureSelfHealApprovalRequestLocked(&b.agentIssues[0], agentIssueClassification{
		Visible:       true,
		CapabilityGap: true,
		Severity:      "warning",
	}, "browser access is not available")
	b.mu.Unlock()

	requests := b.Requests("general", true)
	if got := len(requests); got != 1 {
		t.Fatalf("expected answered approval to be reused, got %d requests: %+v", got, requests)
	}
	issues := b.AgentIssues()
	if len(issues) != 1 || issues[0].SelfHealTaskID == "" {
		t.Fatalf("expected answered approval to create one self-heal task, got issues=%+v tasks=%+v", issues, b.AllTasks())
	}
}

func TestApprovedAgentIssueSelfHealFailureSurfacesToChat(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.channels = nil
	b.requests = append(b.requests, humanInterview{
		ID:        "request-issue-1",
		Kind:      "approval",
		Status:    "answered",
		From:      "system",
		Channel:   "general",
		Title:     "Approve self-heal",
		Question:  "Proceed?",
		CreatedAt: now,
		UpdatedAt: now,
		Answered:  &interviewAnswer{ChoiceID: "approve", AnsweredAt: now},
	})
	b.agentIssues = append(b.agentIssues, agentIssueRecord{
		ID:                "issue-1",
		Agent:             "eng",
		Channel:           "general",
		Detail:            "browser access is not available",
		NormalizedKey:     normalizedAgentIssueKey("eng", "general", "browser access is not available"),
		ApprovalRequestID: "request-issue-1",
		Count:             1,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	b.maybeCreateApprovedSelfHealTaskLocked(b.requests[0])
	b.maybeCreateApprovedSelfHealTaskLocked(b.requests[0])
	b.mu.Unlock()

	issues := b.AgentIssues()
	if len(issues) != 1 || issues[0].SelfHealError == "" {
		t.Fatalf("expected self-heal creation error to be recorded, got %+v", issues)
	}
	msgs := b.ChannelMessages("general")
	if len(msgs) != 1 {
		t.Fatalf("expected one surfaced failure message, got %+v", msgs)
	}
	if got := msgs[0]; got.From != "system" || got.Kind != agentIssueMessageKind || !strings.Contains(got.Content, "could not be created") {
		t.Fatalf("unexpected surfaced failure message: %+v", got)
	}
}

func TestAgentIssueDoesNotCountAsSubstantiveProgress(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b}
	startedAt := time.Now().UTC().Add(-1 * time.Second)

	if _, _, posted, err := b.ReportAgentIssue("ceo", "general", "", "browser access is not available"); err != nil || !posted {
		t.Fatalf("report issue: posted=%v err=%v", posted, err)
	}
	if l.agentPostedSubstantiveMessageSince("ceo", startedAt) {
		t.Fatal("agent_issue should not count as substantive progress")
	}

	if _, err := b.PostMessage("ceo", "general", "I can continue with the code inspection.", nil, ""); err != nil {
		t.Fatalf("post normal message: %v", err)
	}
	if !l.agentPostedSubstantiveMessageSince("ceo", startedAt) {
		t.Fatal("normal streamed prose should count as substantive progress")
	}
}
