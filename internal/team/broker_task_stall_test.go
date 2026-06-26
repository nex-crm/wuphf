package team

// Regression tests for the silent-stall honesty watchdog (Wave F2,
// broker_task_stall.go). The bug lived at the watchdog layer: a RUNNING
// task could sit silent for 21 minutes with no visible signal (ICP-eval v3
// [19:05:30]); these tests pin the stamp / one-line / clear contract.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStallTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	b.mu.Lock()
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}},
		{Slug: "task-office-1", Name: "task-office-1", Members: []string{"human", "ceo", "eng"}},
	}
	b.mu.Unlock()
	return b
}

func seedRunningTask(b *Broker, id, channel, owner, updatedAt string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, teamTask{
		ID:             id,
		Channel:        channel,
		Title:          "stall fixture " + id,
		Owner:          owner,
		status:         "in_progress",
		CreatedBy:      "ceo",
		LifecycleState: LifecycleStateRunning,
		CreatedAt:      updatedAt,
		UpdatedAt:      updatedAt,
	})
}

func stallSweep(b *Broker, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.markSilentRunningTasksLocked(now, taskStallThreshold)
}

func taskStalledSince(t *testing.T, b *Broker, id string) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	task := b.findTaskByIDLocked(id)
	if task == nil {
		t.Fatalf("task %s not found", id)
	}
	return task.StalledSince
}

func stallMessages(b *Broker, channel string) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []channelMessage
	for _, m := range b.messages {
		if m.Kind == taskStalledMessageKind && normalizeChannelSlug(m.Channel) == channel {
			out = append(out, m)
		}
	}
	return out
}

func TestStallWatchdogStampsSilentRunningTaskOnce(t *testing.T) {
	b := newStallTestBroker(t)
	base := time.Now().UTC().Add(-2 * taskStallThreshold)
	seedRunningTask(b, "OFFICE-1", "task-office-1", "eng", base.Format(time.RFC3339))

	now := base.Add(taskStallThreshold + time.Minute)
	if !stallSweep(b, now) {
		t.Fatal("expected the sweep to mark the silent task")
	}
	if got := taskStalledSince(t, b, "OFFICE-1"); got == "" {
		t.Fatal("expected StalledSince to be stamped")
	}
	msgs := stallMessages(b, "task-office-1")
	if len(msgs) != 1 {
		t.Fatalf("expected exactly one stall line, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "@eng") || !strings.Contains(msgs[0].Content, "OFFICE-1") {
		t.Fatalf("stall line must name the owner and the task, got %q", msgs[0].Content)
	}
	if msgs[0].From != "system" {
		t.Fatalf("stall line must come from system, got %q", msgs[0].From)
	}

	// A second sweep while still silent must not duplicate the line —
	// and crucially, the watchdog's own system message/action must not
	// count as fresh activity that clears the marker.
	stallSweep(b, now.Add(time.Minute))
	if got := taskStalledSince(t, b, "OFFICE-1"); got == "" {
		t.Fatal("marker must hold across sweeps while the task stays silent")
	}
	if msgs := stallMessages(b, "task-office-1"); len(msgs) != 1 {
		t.Fatalf("expected one stall line after repeat sweep, got %d", len(msgs))
	}
}

func TestStallWatchdogClearsMarkerOnFreshActivity(t *testing.T) {
	b := newStallTestBroker(t)
	base := time.Now().UTC().Add(-2 * taskStallThreshold)
	seedRunningTask(b, "OFFICE-1", "task-office-1", "eng", base.Format(time.RFC3339))
	stallSweep(b, base.Add(taskStallThreshold+time.Minute))
	if taskStalledSince(t, b, "OFFICE-1") == "" {
		t.Fatal("precondition: task should be stalled")
	}

	// An agent message in the task channel is an observable trace.
	freshAt := base.Add(taskStallThreshold + 2*time.Minute)
	b.mu.Lock()
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        "msg-fresh",
		From:      "eng",
		Channel:   "task-office-1",
		Content:   "progress: drafting the plan",
		Timestamp: freshAt.Format(time.RFC3339),
	})
	b.mu.Unlock()

	if !stallSweep(b, freshAt.Add(time.Minute)) {
		t.Fatal("expected the sweep to clear the marker")
	}
	if got := taskStalledSince(t, b, "OFFICE-1"); got != "" {
		t.Fatalf("expected marker cleared after fresh activity, got %q", got)
	}
}

func TestStallWatchdogIgnoresNonRunningAndSystemTasks(t *testing.T) {
	b := newStallTestBroker(t)
	base := time.Now().UTC().Add(-2 * taskStallThreshold)
	b.mu.Lock()
	b.tasks = append(b.tasks,
		teamTask{ID: "OFFICE-2", Channel: "general", Title: "done fixture", Owner: "eng",
			status: "done", LifecycleState: LifecycleStateApproved,
			CreatedAt: base.Format(time.RFC3339), UpdatedAt: base.Format(time.RFC3339)},
		teamTask{ID: "task-general", Channel: "general", Title: "system fixture", System: true,
			status: "in_progress", LifecycleState: LifecycleStateRunning,
			CreatedAt: base.Format(time.RFC3339), UpdatedAt: base.Format(time.RFC3339)},
	)
	b.mu.Unlock()

	stallSweep(b, base.Add(taskStallThreshold+time.Minute))
	if got := taskStalledSince(t, b, "OFFICE-2"); got != "" {
		t.Fatalf("non-running task must not be marked stalled, got %q", got)
	}
	if got := taskStalledSince(t, b, "task-general"); got != "" {
		t.Fatalf("system task must not be marked stalled, got %q", got)
	}
	if msgs := stallMessages(b, "general"); len(msgs) != 0 {
		t.Fatalf("expected no stall lines, got %d", len(msgs))
	}
}

func TestStallWatchdogClearsMarkerWhenTaskLeavesRunning(t *testing.T) {
	b := newStallTestBroker(t)
	base := time.Now().UTC().Add(-2 * taskStallThreshold)
	seedRunningTask(b, "OFFICE-1", "task-office-1", "eng", base.Format(time.RFC3339))
	stallSweep(b, base.Add(taskStallThreshold+time.Minute))

	b.mu.Lock()
	task := b.findTaskByIDLocked("OFFICE-1")
	task.LifecycleState = LifecycleStateBlocked
	task.status = "blocked"
	b.mu.Unlock()

	stallSweep(b, base.Add(taskStallThreshold+2*time.Minute))
	if got := taskStalledSince(t, b, "OFFICE-1"); got != "" {
		t.Fatalf("marker must clear when the task leaves running, got %q", got)
	}
}

func TestTeamTaskStalledSinceRoundTripsOnWire(t *testing.T) {
	in := teamTask{ID: "OFFICE-9", Title: "wire fixture", StalledSince: "2026-06-12T10:00:00Z",
		CreatedAt: "2026-06-12T09:00:00Z", UpdatedAt: "2026-06-12T09:00:00Z"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"stalled_since":"2026-06-12T10:00:00Z"`) {
		t.Fatalf("wire JSON missing stalled_since: %s", data)
	}
	var out teamTask
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.StalledSince != in.StalledSince {
		t.Fatalf("round trip lost stalled_since: %q", out.StalledSince)
	}
}
