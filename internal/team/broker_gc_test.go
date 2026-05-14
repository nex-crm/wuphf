package team

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendMessageLocked_CapsAtMax(t *testing.T) {
	t.Setenv("WUPHF_MAX_MESSAGES", "10")
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))

	b.mu.Lock()
	for i := 0; i < 25; i++ {
		b.appendMessageLocked(channelMessage{
			ID:      "msg-" + time.Now().Format("150405.000000") + "-" + string(rune('a'+i%26)),
			Content: "hello",
			Channel: "general",
		})
	}
	count := len(b.messages)
	b.mu.Unlock()

	if count != 10 {
		t.Errorf("expected 10 messages after cap, got %d", count)
	}
}

func TestPruneCompletedTasksLocked_RemovesMergedOldTasks(t *testing.T) {
	t.Setenv("WUPHF_TASK_RETENTION_DAYS", "1")
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))

	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-1", LifecycleState: LifecycleStateMerged, UpdatedAt: old},
		{ID: "task-2", LifecycleState: LifecycleStateRunning, UpdatedAt: old},
		{ID: "task-3", LifecycleState: LifecycleStateMerged, UpdatedAt: recent},
		{ID: "task-4", LifecycleState: LifecycleStateIntake, CreatedAt: old},
	}
	pruned := b.pruneCompletedTasksLocked()
	remaining := len(b.tasks)
	b.mu.Unlock()

	if pruned != 1 {
		t.Errorf("expected 1 pruned (old merged), got %d", pruned)
	}
	if remaining != 3 {
		t.Errorf("expected 3 remaining, got %d", remaining)
	}
}

func TestPruneCompletedTasksLocked_KeepsAllWhenNoneExpired(t *testing.T) {
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))

	recent := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-1", LifecycleState: LifecycleStateMerged, UpdatedAt: recent},
		{ID: "task-2", LifecycleState: LifecycleStateRunning, UpdatedAt: recent},
	}
	pruned := b.pruneCompletedTasksLocked()
	remaining := len(b.tasks)
	b.mu.Unlock()

	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
	if remaining != 2 {
		t.Errorf("expected 2 remaining, got %d", remaining)
	}
}

func TestIsTerminalTask(t *testing.T) {
	if !isTerminalTask(teamTask{LifecycleState: LifecycleStateMerged}) {
		t.Error("expected merged task to be terminal")
	}
	if isTerminalTask(teamTask{LifecycleState: LifecycleStateRunning}) {
		t.Error("expected running task to not be terminal")
	}
	if isTerminalTask(teamTask{LifecycleState: LifecycleStateIntake}) {
		t.Error("expected intake task to not be terminal")
	}
}
