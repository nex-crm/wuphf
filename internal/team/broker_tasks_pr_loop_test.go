package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPRLoop_RequestChangesBouncesToOwnerWithFeedback mirrors scenario 1
// from the live ICP-style browser walk-through (slug/emoji helper):
// reviewer flags an issue, calls request_changes with concrete feedback,
// the task bounces back to the owner with reviewState=changes_requested,
// and a channel broadcast tags the owner with the feedback excerpt.
func TestPRLoop_RequestChangesBouncesToOwnerWithFeedback(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"executor", "reviewer", "ceo"}},
	}
	b.tasks = []teamTask{
		{
			ID:      "task-rc-1",
			Channel: "general",
			Title:   "Implement ToShortcode helper",
			Owner:   "executor",
			status:  "review",
		},
	}

	before := len(b.Messages())

	feedback := "Bullet 4 fails — :tada: is 6 runes vs 🎉's 1, so 'never longer than input' is violated for every mapping."
	got, err := b.MutateTask(TaskPostRequest{
		Action:    "request_changes",
		ID:        "task-rc-1",
		Channel:   "general",
		Details:   feedback,
		CreatedBy: "reviewer",
	})
	if err != nil {
		t.Fatalf("MutateTask request_changes: %v", err)
	}
	if got.Task.ReviewState() != "changes_requested" {
		t.Fatalf("reviewState: want changes_requested, got %q", got.Task.ReviewState())
	}
	if got.Task.Status() != "in_progress" {
		t.Fatalf("status: want in_progress, got %q", got.Task.Status())
	}
	if got.Task.Owner != "executor" {
		t.Fatalf("owner must stay executor for revision, got %q", got.Task.Owner)
	}

	msgs := b.Messages()[before:]
	var broadcast *channelMessage
	for i := range msgs {
		if msgs[i].Kind == "task_changes_requested" {
			broadcast = &msgs[i]
			break
		}
	}
	if broadcast == nil {
		t.Fatalf("expected task_changes_requested broadcast in %d new messages", len(msgs))
	}
	if broadcast.From != "reviewer" {
		t.Fatalf("broadcast author: want reviewer, got %q", broadcast.From)
	}
	if !containsSlug(broadcast.Tagged, "executor") {
		t.Fatalf("broadcast must tag owner @executor, tagged=%v", broadcast.Tagged)
	}
	if !strings.Contains(broadcast.Content, "Bullet 4") {
		t.Fatalf("broadcast must include feedback excerpt, got %q", broadcast.Content)
	}
	if !strings.Contains(broadcast.Content, "submit_for_review") {
		t.Fatalf("broadcast must instruct owner how to resubmit, got %q", broadcast.Content)
	}
}

// TestPRLoop_SubmitForReviewCapturesArtifact verifies the executor's
// submission text auto-appends to the DecisionPacket feedback thread so
// the unified Inbox Discussion section renders the artifact inline.
// Mirrors scenario 2 in the browser walk-through.
func TestPRLoop_SubmitForReviewCapturesArtifact(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"executor", "reviewer"}},
	}
	b.tasks = []teamTask{
		{
			ID:      "task-sub-1",
			Channel: "general",
			Title:   "Write Greet helper",
			Owner:   "executor",
			status:  "in_progress",
		},
	}

	artifact := "Implemented internal/hello.Greet — handles empty + whitespace trim. 3 table-driven tests pass."
	got, err := b.MutateTask(TaskPostRequest{
		Action:    "submit_for_review",
		ID:        "task-sub-1",
		Channel:   "general",
		Details:   artifact,
		CreatedBy: "executor",
	})
	if err != nil {
		t.Fatalf("MutateTask submit_for_review: %v", err)
	}
	if got.Task.Status() != "review" {
		t.Fatalf("status: want review, got %q", got.Task.Status())
	}

	b.mu.Lock()
	packet, perr := b.findDecisionPacketLocked("task-sub-1")
	b.mu.Unlock()
	if perr != nil {
		t.Fatalf("findDecisionPacketLocked: %v", perr)
	}
	if packet == nil {
		t.Fatalf("expected DecisionPacket with submission artifact after submit_for_review")
	}
	if len(packet.Spec.Feedback) == 0 {
		t.Fatalf("expected feedback array to contain submitted artifact, got empty")
	}
	last := packet.Spec.Feedback[len(packet.Spec.Feedback)-1]
	if last.Author != "executor" {
		t.Fatalf("submission author: want executor, got %q", last.Author)
	}
	if !strings.Contains(last.Body, artifact) {
		t.Fatalf("submission body must contain artifact text, got %q", last.Body)
	}
	if !strings.Contains(last.Body, "📤 Submitted") {
		t.Fatalf("submission body must be marked as a submit envelope, got %q", last.Body)
	}
}

// TestPRLoop_CommentDoesNotChangeState verifies the comment action only
// appends a FeedbackItem; lifecycle state remains untouched.
func TestPRLoop_CommentDoesNotChangeState(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"executor", "reviewer"}},
	}
	b.tasks = []teamTask{
		{
			ID:      "task-cmt-1",
			Channel: "general",
			Title:   "Stable task",
			Owner:   "executor",
			status:  "review",
		},
	}

	got, err := b.MutateTask(TaskPostRequest{
		Action:    "comment",
		ID:        "task-cmt-1",
		Channel:   "general",
		Details:   "Reviewer drive-by: nice catch on the edge case.",
		CreatedBy: "reviewer",
	})
	if err != nil {
		t.Fatalf("MutateTask comment: %v", err)
	}
	if got.Task.Status() != "review" {
		t.Fatalf("status must stay review after comment, got %q", got.Task.Status())
	}
	if got.Task.Owner != "executor" {
		t.Fatalf("owner must stay executor after comment, got %q", got.Task.Owner)
	}

	b.mu.Lock()
	packet, perr := b.findDecisionPacketLocked("task-cmt-1")
	b.mu.Unlock()
	if perr != nil {
		t.Fatalf("findDecisionPacketLocked: %v", perr)
	}
	if packet == nil || len(packet.Spec.Feedback) == 0 {
		t.Fatalf("expected comment to append a FeedbackItem")
	}
	last := packet.Spec.Feedback[len(packet.Spec.Feedback)-1]
	if last.Author != "reviewer" {
		t.Fatalf("comment author: want reviewer, got %q", last.Author)
	}
	if !strings.Contains(last.Body, "nice catch") {
		t.Fatalf("comment body lost: got %q", last.Body)
	}
}

// TestPRLoop_RejectIsTerminalAndDoesNotUnblockDependents mirrors the
// PR-rejection arm: reviewer rejects with a reason, lifecycle goes to
// LifecycleStateRejected, downstream task that depends on the rejected
// upstream STAYS blocked (terminal failure, work did not land).
func TestPRLoop_RejectIsTerminalAndDoesNotUnblockDependents(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"executor", "reviewer", "ceo"}},
	}
	b.tasks = []teamTask{
		{
			ID:      "task-rej-1",
			Channel: "general",
			Title:   "Doomed implementation",
			Owner:   "executor",
			status:  "review",
		},
		{
			ID:        "task-rej-2",
			Channel:   "general",
			Title:     "Downstream that depends on rejected upstream",
			Owner:     "executor",
			status:    "blocked",
			blocked:   true,
			BlockedOn: []string{"task-rej-1"},
		},
	}

	reason := "Out of scope without a customer-facing change log first."
	got, err := b.MutateTask(TaskPostRequest{
		Action:    "reject",
		ID:        "task-rej-1",
		Channel:   "general",
		Details:   reason,
		CreatedBy: "reviewer",
	})
	if err != nil {
		t.Fatalf("MutateTask reject: %v", err)
	}
	if got.Task.LifecycleState != LifecycleStateRejected {
		t.Fatalf("LifecycleState: want rejected, got %q", got.Task.LifecycleState)
	}
	if got.Task.Status() != "rejected" {
		t.Fatalf("status: want rejected, got %q", got.Task.Status())
	}
	if got.Task.ReviewState() != "rejected" {
		t.Fatalf("reviewState: want rejected, got %q", got.Task.ReviewState())
	}

	// Downstream must stay blocked — work did not land.
	b.mu.Lock()
	var downstream *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == "task-rej-2" {
			downstream = &b.tasks[i]
			break
		}
	}
	b.mu.Unlock()
	if downstream == nil {
		t.Fatalf("downstream task disappeared")
	}
	if !downstream.blocked {
		t.Fatalf("downstream must stay blocked after reject, got blocked=%v", downstream.blocked)
	}
}

// TestPRLoop_CommentEndpointResolvesActorFromAuth covers the HTTP layer:
// POST /tasks/{id}/comment authenticates the actor via the bearer token
// (not body.CreatedBy) so human comments show up with a real author,
// not "@unknown".
func TestPRLoop_CommentEndpointResolvesActorFromAuth(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	defer b.Stop()
	b.mu.Lock()
	b.channels = []teamChannel{{Slug: "general", Members: []string{"executor", "reviewer"}}}
	b.tasks = []teamTask{{
		ID: "task-http-1", Channel: "general", Title: "Auth me",
		Owner: "executor", status: "review",
	}}
	b.mu.Unlock()

	url := fmt.Sprintf("http://%s/tasks/task-http-1/comment", b.Addr())
	body, _ := json.Marshal(map[string]string{"body": "Looks good, ship it after the doc bump."})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST comment: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d body=%s", resp.StatusCode, raw)
	}
	var got struct {
		TaskID string `json:"taskId"`
		Status string `json:"status"`
		Author string `json:"author"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Author == "" || got.Author == "unknown" {
		t.Fatalf("expected non-empty resolved actor, got %q", got.Author)
	}
	if got.Status != "recorded" {
		t.Fatalf("status: want recorded, got %q", got.Status)
	}

	b.mu.Lock()
	packet, _ := b.findDecisionPacketLocked("task-http-1")
	b.mu.Unlock()
	if packet == nil || len(packet.Spec.Feedback) == 0 {
		t.Fatalf("expected FeedbackItem appended via /comment endpoint")
	}
	last := packet.Spec.Feedback[len(packet.Spec.Feedback)-1]
	if last.Author == "" || last.Author == "unknown" {
		t.Fatalf("FeedbackItem author must come from auth, got %q", last.Author)
	}
}

// TestPRLoop_RejectRequiresNonEmptyReason guards the "reject must
// carry a reason" contract at the broker layer (frontend is best
// effort; backend is authoritative).
func TestPRLoop_RejectRequiresNonEmptyReason(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.channels = []teamChannel{{Slug: "general", Members: []string{"reviewer", "executor"}}}
	b.tasks = []teamTask{{
		ID: "task-rej-empty-1", Channel: "general", Title: "Blank-reason reject",
		Owner: "executor", status: "review",
	}}

	_, err := b.MutateTask(TaskPostRequest{
		Action:    "reject",
		ID:        "task-rej-empty-1",
		Channel:   "general",
		Details:   "   ",
		CreatedBy: "reviewer",
	})
	if err == nil {
		t.Fatalf("expected error for empty reject reason, got nil")
	}
	if !strings.Contains(err.Error(), "reject reason required") {
		t.Fatalf("expected 'reject reason required' error, got %v", err)
	}
}

// TestPRLoop_CommentEndpointRequiresBody guards against silent empty
// comments being persisted as feedback noise.
func TestPRLoop_CommentEndpointRequiresBody(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	defer b.Stop()
	b.mu.Lock()
	b.channels = []teamChannel{{Slug: "general", Members: []string{"reviewer"}}}
	b.tasks = []teamTask{{ID: "task-empty-1", Channel: "general", Title: "x"}}
	b.mu.Unlock()

	url := fmt.Sprintf("http://%s/tasks/task-empty-1/comment", b.Addr())
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"body":"   "}`))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST comment: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400 for empty body, got %d", resp.StatusCode)
	}
}
