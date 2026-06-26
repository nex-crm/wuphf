package team

// task_thread_boundary_test.go pins the per-task context boundary: every
// task gets its own thread root at creation, and the thread-scoped context
// excludes other tasks' messages. This is the regression for the live
// hallucination where a fresh task's CEO turn pulled in a PRIOR task's
// rate-limit chatter from raw channel scrollback and invented that the agent
// was rate-limited.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/channel"
)

// A fresh /task-plan task is given a non-empty ThreadID anchored on a real
// root message in its channel.
func TestTaskPlanAnchorsThreadRoot(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "ceo",
		"tasks": []map[string]any{
			{"title": "Compare two plans", "details": "TESTING run.", "assignee": "ceo", "execution_mode": "office"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/task-plan", b.Addr()), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task plan request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(result.Tasks))
	}
	root := strings.TrimSpace(result.Tasks[0].ThreadID)
	if root == "" {
		t.Fatal("task created without a thread root — context would fall back to channel scrollback (the bleed)")
	}
	// A start-now task must carry the typed Running state, not just a bare
	// "in_progress" status — otherwise SourceTaskID stamping (which gates on
	// the typed pre-merge state) never fires and nothing threads under the task.
	if got := result.Tasks[0].LifecycleState; got != LifecycleStateRunning {
		t.Fatalf("start-now task lifecycle = %q, want running (else threading no-ops)", got)
	}

	// The owner's next channel message must get SourceTaskID stamped + chained
	// into the task thread — the live path that was silently broken.
	b.mu.Lock()
	stamped := b.appendMessageLocked(channelMessage{From: "ceo", Channel: result.Tasks[0].Channel, Content: "on it"})
	b.mu.Unlock()
	if stamped.SourceTaskID != result.Tasks[0].ID {
		t.Fatalf("owner message not stamped with the task: %q", stamped.SourceTaskID)
	}
	if stamped.ReplyTo != root {
		t.Fatalf("owner message not chained into the task thread: ReplyTo=%q want %q", stamped.ReplyTo, root)
	}

	// The root must be a real message in the channel, so the context builder
	// can anchor on it.
	b.mu.Lock()
	found := false
	for _, m := range b.messages {
		if m.ID == root {
			found = true
			break
		}
	}
	b.mu.Unlock()
	if !found {
		t.Fatalf("thread root %q is not a real channel message", root)
	}
}

// Thread-scoped context excludes a sibling task's messages: a turn for task A
// must not see task B's chatter (the bleed), while still seeing A's own work.
func TestNotificationContext_ThreadRootExcludesSiblingTaskBleed(t *testing.T) {
	msgs := []channelMessage{
		{ID: "a-root", Channel: "c", From: "ceo", Content: "Task A opened", ReplyTo: "", SourceTaskID: "A"},
		{ID: "a1", Channel: "c", From: "hermes", Content: "Plan B totals $5,220 over 3 years", ReplyTo: "a-root", SourceTaskID: "A"},
		{ID: "b-root", Channel: "c", From: "ceo", Content: "Task B opened", ReplyTo: "", SourceTaskID: "B"},
		{ID: "b1", Channel: "c", From: "hermes", Content: "Hermes hit the GitHub rate limit", ReplyTo: "b-root", SourceTaskID: "B"},
	}
	cb := &notificationContextBuilder{
		channelMessages: func(string) []channelMessage { return msgs },
		channelTasks:    func(string) []teamTask { return nil },
		allTasks:        func() []teamTask { return nil },
		channelStore:    func() *channel.Store { return nil },
	}

	// Scoped to task A's thread root.
	scoped := cb.NotificationContext("ceo", "c", "", "a-root", 20)
	if !strings.Contains(scoped, "Plan B totals") {
		t.Fatalf("scoped context should include task A's own work: %q", scoped)
	}
	if strings.Contains(scoped, "rate limit") {
		t.Fatalf("scoped context leaked task B's chatter (the bleed): %q", scoped)
	}

	// Contrast: with no thread root the old behavior bleeds (both tasks).
	bled := cb.NotificationContext("ceo", "c", "", "", 20)
	if !strings.Contains(bled, "rate limit") {
		t.Fatalf("unscoped context should show the bleed for contrast: %q", bled)
	}
}
