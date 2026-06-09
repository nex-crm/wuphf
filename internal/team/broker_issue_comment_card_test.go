package team

// Coverage for the Issue chat-surface fixes that landed together:
//
//  1. Creating an Issue (task_type=issue) emits exactly one chat card
//     of kind=issue_created and zero issue_lifecycle cards. Pre-fix,
//     the legacy-derived LifecycleState read off the task at create
//     time leaked into the apply call, which bypassed the prev=="" guard
//     and emitted a redundant "<legacy> → drafting" lifecycle card on
//     top of the proper issue_created card.
//
//  2. POST /tasks/{id}/comment no longer creates a packet-feedback
//     "comment" + an issue_comment card. Task comments are retired:
//     the endpoint posts the body as a normal chat message into the
//     task's channel (visible in the feed, wakes the owner via the
//     standard path). See TestPostTaskCommentPostsChatMessage.
//
//  3. Issue chat cards (created / lifecycle) carry ReplyTo =
//     task.ThreadID so they fold into the originating thread instead
//     of seeding new top-level messages that visually float above
//     subsequent chat replies.
//
//  4. Multi-step lifecycle transitions on a single user action (e.g.
//     Drafting→Approved→Running on Approve & Start) coalesce so the
//     channel only shows ONE card representing the final state.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCreateIssueEmitsOnlyOneCardKind(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "create",
		"title":      "Ship the Inbox needs-action filter",
		"details":    "Add a new default tab in the Decision Inbox.",
		"created_by": "ceo",
		"owner":      "builder",
		"channel":    "general",
		"task_type":  "issue",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task post failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}
	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	taskID := strings.TrimSpace(result.Task.ID)
	if taskID == "" {
		t.Fatalf("missing task id in response")
	}

	var (
		createdCards   int
		lifecycleCards int
	)
	b.mu.Lock()
	for _, msg := range b.messages {
		if msg.SourceTaskID != taskID {
			continue
		}
		switch msg.Kind {
		case "issue_created":
			createdCards++
		case "issue_lifecycle":
			lifecycleCards++
		}
	}
	b.mu.Unlock()

	if createdCards != 1 {
		t.Fatalf("expected exactly one issue_created card, got %d", createdCards)
	}
	if lifecycleCards != 0 {
		t.Fatalf("expected zero issue_lifecycle cards at create time, got %d", lifecycleCards)
	}
}

func TestPostTaskCommentPostsChatMessage(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	// Create the parent Issue first so the comment endpoint resolves.
	createBody, _ := json.Marshal(map[string]any{
		"action":     "create",
		"title":      "Pick a Postgres major version",
		"details":    "Need to align with platform before staging cutover.",
		"created_by": "ceo",
		"owner":      "builder",
		"channel":    "general",
		"task_type":  "issue",
	})
	createReq, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+b.Token())
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if createResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("unexpected create status %d: %s", createResp.StatusCode, raw)
	}
	var createResult struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createResult); err != nil {
		createResp.Body.Close()
		t.Fatalf("decode create response: %v", err)
	}
	createResp.Body.Close()
	taskID := strings.TrimSpace(createResult.Task.ID)

	commentText := "Should we go with PG16 or wait for 17 GA?"
	commentBody, _ := json.Marshal(map[string]string{"body": commentText})
	commentReq, _ := http.NewRequest(
		http.MethodPost,
		base+"/tasks/"+taskID+"/comment",
		bytes.NewReader(commentBody),
	)
	commentReq.Header.Set("Authorization", "Bearer "+b.Token())
	commentReq.Header.Set("Content-Type", "application/json")
	commentResp, err := http.DefaultClient.Do(commentReq)
	if err != nil {
		t.Fatalf("post comment: %v", err)
	}
	defer commentResp.Body.Close()
	if commentResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(commentResp.Body)
		t.Fatalf("unexpected comment status %d: %s", commentResp.StatusCode, raw)
	}

	// Resolve the task's channel, then assert the comment body landed there
	// as a plain human chat message — not a packet-feedback "issue_comment"
	// card. Task comments are retired: human input on a task is just chat.
	b.mu.Lock()
	taskChannel := ""
	if tk := b.findTaskByIDLocked(taskID); tk != nil {
		taskChannel = normalizeChannelSlug(tk.Channel)
	}
	var (
		chatMsg channelMessage
		found   bool
	)
	for i := range b.messages {
		msg := &b.messages[i]
		if normalizeChannelSlug(msg.Channel) == taskChannel &&
			strings.TrimSpace(msg.Content) == commentText {
			chatMsg = *msg
			found = true
			break
		}
	}
	sawCommentCard := false
	for i := range b.messages {
		if b.messages[i].Kind == "issue_comment" {
			sawCommentCard = true
			break
		}
	}
	b.mu.Unlock()

	if !found {
		t.Fatalf("expected the comment body to be posted as a chat message in channel %q", taskChannel)
	}
	// A human-shaped sender so it renders as a normal chat bubble, not a
	// system card.
	if !isHumanMessageSender(chatMsg.From) {
		t.Fatalf("expected chat message From to be a human sender, got %q", chatMsg.From)
	}
	if chatMsg.Kind == "issue_comment" {
		t.Fatalf("expected a plain chat message, got an issue_comment card")
	}
	if sawCommentCard {
		t.Fatalf("comments are retired: expected NO issue_comment card to be created")
	}
}

// TestIssueCardsReplyInOriginatingThread verifies that the Issue chat
// cards (created / lifecycle) all set ReplyTo to the originating thread
// id stored on the task. Pre-fix, cards seeded their own top-level chat
// slot, which left subsequent chat replies appearing visually below
// them as if the card were "newer".
func TestIssueCardsReplyInOriginatingThread(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	const threadRoot = "msg-thread-root-1"

	createBody, _ := json.Marshal(map[string]any{
		"action":     "create",
		"title":      "Wire ReplyTo on Issue cards",
		"details":    "Cards should fold into the originating thread.",
		"created_by": "ceo",
		"owner":      "builder",
		"channel":    "general",
		"task_type":  "issue",
		"thread_id":  threadRoot,
	})
	createReq, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+b.Token())
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if createResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("unexpected create status %d: %s", createResp.StatusCode, raw)
	}
	var createResult struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createResult); err != nil {
		createResp.Body.Close()
		t.Fatalf("decode create: %v", err)
	}
	createResp.Body.Close()
	taskID := strings.TrimSpace(createResult.Task.ID)

	// Force a lifecycle transition (Drafting → Review) to emit an
	// issue_lifecycle card. transitionLifecycleLocked is the public
	// chokepoint and only requires the lock to be held by the caller.
	b.mu.Lock()
	_, lerr := b.transitionLifecycleLocked(taskID, LifecycleStateReview, "test-trigger")
	b.mu.Unlock()
	if lerr != nil {
		t.Fatalf("lifecycle transition: %v", lerr)
	}

	wantKinds := map[string]bool{
		"issue_created":   false,
		"issue_lifecycle": false,
	}
	b.mu.Lock()
	for _, msg := range b.messages {
		if strings.TrimSpace(msg.SourceTaskID) != taskID {
			continue
		}
		if _, ok := wantKinds[msg.Kind]; !ok {
			continue
		}
		if msg.ReplyTo != threadRoot {
			b.mu.Unlock()
			t.Fatalf(
				"kind=%s ReplyTo mismatch: got %q want %q",
				msg.Kind, msg.ReplyTo, threadRoot,
			)
		}
		wantKinds[msg.Kind] = true
	}
	b.mu.Unlock()
	for kind, seen := range wantKinds {
		if !seen {
			t.Fatalf("expected to see a %s card for task %s, missing", kind, taskID)
		}
	}
}

// TestLifecycleCardsCoalesceWithinShortWindow verifies that two
// lifecycle transitions firing within the coalesce window leave only
// the most recent card in the channel. Mirrors the user-visible burst
// of Drafting→Approved→Running on a single Approve & Start click.
func TestLifecycleCardsCoalesceWithinShortWindow(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"action":     "create",
		"title":      "Coalesce lifecycle bursts",
		"details":    "Drive two transitions to verify dedup.",
		"created_by": "ceo",
		"owner":      "builder",
		"channel":    "general",
		"task_type":  "issue",
	})
	createReq, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+b.Token())
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if createResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("unexpected create status %d: %s", createResp.StatusCode, raw)
	}
	var createResult struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createResult); err != nil {
		createResp.Body.Close()
		t.Fatalf("decode create: %v", err)
	}
	createResp.Body.Close()
	taskID := strings.TrimSpace(createResult.Task.ID)

	// Two transitions back-to-back. Both will emit lifecycle cards.
	// Coalesce should leave only one card (the most recent kind).
	b.mu.Lock()
	if _, err := b.transitionLifecycleLocked(taskID, LifecycleStateApproved, "approve"); err != nil {
		b.mu.Unlock()
		t.Fatalf("transition to approved: %v", err)
	}
	if _, err := b.transitionLifecycleLocked(taskID, LifecycleStateRunning, "start"); err != nil {
		b.mu.Unlock()
		t.Fatalf("transition to running: %v", err)
	}
	b.mu.Unlock()

	var (
		lifecycleCards []channelMessage
	)
	b.mu.Lock()
	for _, msg := range b.messages {
		if strings.TrimSpace(msg.SourceTaskID) != taskID {
			continue
		}
		if msg.Kind != "issue_lifecycle" {
			continue
		}
		lifecycleCards = append(lifecycleCards, msg)
	}
	b.mu.Unlock()

	if len(lifecycleCards) != 1 {
		t.Fatalf(
			"expected exactly 1 issue_lifecycle card after coalesce, got %d (%v)",
			len(lifecycleCards), lifecycleCards,
		)
	}
	// The surviving card must represent the LATEST transition.
	var payload map[string]string
	if err := json.Unmarshal(lifecycleCards[0].Payload, &payload); err != nil {
		t.Fatalf("decode lifecycle payload: %v", err)
	}
	if payload["to_state"] != string(LifecycleStateRunning) {
		t.Fatalf("expected to_state=running on surviving card, got %q", payload["to_state"])
	}
}

// TestLifecycleCardsDoNotCoalesceAcrossTasks ensures the dedup window
// is per-task — a transition on task A must not drop a recent card
// for an unrelated task B in the same channel.
func TestLifecycleCardsDoNotCoalesceAcrossTasks(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	mkTask := func(title string) string {
		body, _ := json.Marshal(map[string]any{
			"action":     "create",
			"title":      title,
			"details":    "n/a",
			"created_by": "ceo",
			"owner":      "builder",
			"channel":    "general",
			"task_type":  "issue",
		})
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected create status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode %q: %v", title, err)
		}
		return strings.TrimSpace(result.Task.ID)
	}

	taskA := mkTask("Task A")
	taskB := mkTask("Task B")

	b.mu.Lock()
	if _, err := b.transitionLifecycleLocked(taskA, LifecycleStateApproved, "approve"); err != nil {
		b.mu.Unlock()
		t.Fatalf("A→approved: %v", err)
	}
	if _, err := b.transitionLifecycleLocked(taskB, LifecycleStateApproved, "approve"); err != nil {
		b.mu.Unlock()
		t.Fatalf("B→approved: %v", err)
	}
	b.mu.Unlock()

	countA := 0
	countB := 0
	b.mu.Lock()
	for _, msg := range b.messages {
		if msg.Kind != "issue_lifecycle" {
			continue
		}
		switch strings.TrimSpace(msg.SourceTaskID) {
		case taskA:
			countA++
		case taskB:
			countB++
		}
	}
	b.mu.Unlock()

	if countA != 1 || countB != 1 {
		t.Fatalf(
			"expected one lifecycle card per task (A=%d, B=%d)",
			countA, countB,
		)
	}
}

// TestCoalesceWindowDoesNotRemoveOlderCards verifies that cards older
// than the coalesce window stay put. Without this, a freshly emitted
// transition would silently swallow a card from minutes earlier and
// destroy chat history.
func TestCoalesceWindowDoesNotRemoveOlderCards(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	defer b.mu.Unlock()
	// Inject a synthetic "old" card directly via appendMessageLocked
	// so we control its timestamp.
	oldTimestamp := time.Now().UTC().Add(-30 * time.Second).Format(time.RFC3339)
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:           fmt.Sprintf("msg-%d", b.counter),
		From:         "system",
		Channel:      "general",
		Kind:         "issue_lifecycle",
		Title:        "Old card",
		Content:      "Old lifecycle event",
		Timestamp:    oldTimestamp,
		SourceTaskID: "task-old",
	})

	removed := b.coalesceRecentLifecycleCardLocked("task-old", 10*time.Second)
	if removed != 0 {
		t.Fatalf("expected coalesce to skip cards older than the window, removed=%d", removed)
	}
}
