package team

// broker_system_tasks_test.go — acceptance tests for the "Backup & Migration"
// system task (task-general).
//
// Covered by these tests:
//   - The system task exists after broker init with the correct fields.
//   - It is excluded from default ListTasks (no include_done).
//   - It is included in ListTasks with include_done=true.
//   - The "general" channel still exists and is unchanged.
//   - The system task is idempotent: seeding twice does not duplicate it.
//   - AllTasks() and ChannelTasks() exclude the system task.

import (
	"testing"
)

// TestBackupMigrationSystemTaskExistsAfterBrokerInit asserts that booting a
// broker via NewBrokerAt seeds the "Backup & Migration" system task with the
// expected ID, channel, System flag, and LifecycleState.
func TestBackupMigrationSystemTaskExistsAfterBrokerInit(t *testing.T) {
	b := newTestBroker(t)

	// Find task-general directly from the raw task slice (not AllTasks,
	// which excludes system tasks by design).
	b.mu.Lock()
	defer b.mu.Unlock()

	var found *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == backupMigrationTaskID {
			found = &b.tasks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected %q system task to exist after broker init", backupMigrationTaskID)
	}

	if found.Channel != "general" {
		t.Errorf("Channel: got %q, want %q", found.Channel, "general")
	}
	if !found.System {
		t.Error("System: got false, want true")
	}
	if found.LifecycleState != LifecycleStateArchived {
		t.Errorf("LifecycleState: got %q, want %q", found.LifecycleState, LifecycleStateArchived)
	}
	if found.Title != "Backup & Migration" {
		t.Errorf("Title: got %q, want %q", found.Title, "Backup & Migration")
	}
	if found.CreatedBy != "system" {
		t.Errorf("CreatedBy: got %q, want %q", found.CreatedBy, "system")
	}
}

// TestBackupMigrationTaskExcludedFromDefaultListTasks asserts that the system
// task does NOT appear in default ListTasks responses (no include_done), but
// DOES appear when include_done=true is set.
func TestBackupMigrationTaskExcludedFromDefaultListTasks(t *testing.T) {
	b := newTestBroker(t)

	// Default list — system task must be absent (it's archived).
	res, err := b.ListTasks(TaskListRequest{
		Channel:     "general",
		ViewerSlug:  "ceo",
		IncludeDone: false,
	})
	if err != nil {
		t.Fatalf("ListTasks default: %v", err)
	}
	for _, task := range res.Tasks {
		if task.ID == backupMigrationTaskID {
			t.Errorf("system task %q should not appear in default ListTasks, got %+v", backupMigrationTaskID, task)
		}
	}

	// With include_done=true — system task must be present.
	resWithDone, err := b.ListTasks(TaskListRequest{
		Channel:     "general",
		ViewerSlug:  "ceo",
		IncludeDone: true,
	})
	if err != nil {
		t.Fatalf("ListTasks include_done=true: %v", err)
	}
	found := false
	for _, task := range resWithDone.Tasks {
		if task.ID == backupMigrationTaskID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("system task %q should appear in ListTasks with include_done=true", backupMigrationTaskID)
	}
}

// TestBackupMigrationTaskIdempotentSeed asserts that calling
// ensureBackupMigrationTaskLocked twice does not create duplicate entries.
func TestBackupMigrationTaskIdempotentSeed(t *testing.T) {
	b := newTestBroker(t)

	b.mu.Lock()
	// Call the seed again explicitly — it should be a no-op.
	b.ensureBackupMigrationTaskLocked()
	b.ensureBackupMigrationTaskLocked()

	count := 0
	for _, task := range b.tasks {
		if task.ID == backupMigrationTaskID {
			count++
		}
	}
	b.mu.Unlock()

	if count != 1 {
		t.Errorf("expected exactly 1 task-general, got %d", count)
	}
}

// TestGeneralChannelStillExistsAfterSystemTaskSeed asserts that the #general
// channel is unchanged after system task seeding — the 141 fallback call sites
// must keep working.
func TestGeneralChannelStillExistsAfterSystemTaskSeed(t *testing.T) {
	b := newTestBroker(t)

	b.mu.Lock()
	ch := b.findChannelLocked("general")
	b.mu.Unlock()

	if ch == nil {
		t.Fatal("#general channel must exist after broker init")
	}
	if ch.Slug != "general" {
		t.Errorf("#general slug: got %q, want %q", ch.Slug, "general")
	}
}

// TestAllTasksExcludesSystemTask asserts AllTasks() never returns system tasks.
func TestAllTasksExcludesSystemTask(t *testing.T) {
	b := newTestBroker(t)
	for _, task := range b.AllTasks() {
		if task.System {
			t.Errorf("AllTasks should not return system tasks, got %+v", task)
		}
		if task.ID == backupMigrationTaskID {
			t.Errorf("AllTasks should not return %q", backupMigrationTaskID)
		}
	}
}

// TestChannelTasksExcludesSystemTask asserts ChannelTasks("general") never
// returns the system task.
func TestChannelTasksExcludesSystemTask(t *testing.T) {
	b := newTestBroker(t)
	for _, task := range b.ChannelTasks("general") {
		if task.System {
			t.Errorf("ChannelTasks should not return system tasks, got %+v", task)
		}
		if task.ID == backupMigrationTaskID {
			t.Errorf("ChannelTasks should not return %q", backupMigrationTaskID)
		}
	}
}
