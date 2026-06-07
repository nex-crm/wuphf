package team

// broker_system_tasks.go — system task seeding for permanent broker-owned tasks.
//
// System task — "Backup & Migration":
//   The task with ID "task-general" OWNS the #general channel. Every one of
//   the ~141 broker call sites that fall back to "general" as the default
//   channel keeps working because the channel slug is unchanged. The system
//   task gives #general a proper task ownership entry so the UI can surface
//   it in the Archive column (include_done=true) without exposing it on the
//   active board.
//
// Hard rules:
//   - The task ID "task-general" is stable and reserved; it is never
//     minted by the counter (allocateIssueIDLocked).
//   - The task is archived on creation so it is excluded from default
//     ListTasks responses (status="archived") and only appears when the
//     caller passes include_done=true.
//   - Seeding is idempotent: skip creation when a task with that ID already
//     exists (handles re-seed + restart).
//   - System tasks may not be removed. If a delete path is added later,
//     it must check task.System == true and refuse.

import "time"

// backupMigrationTaskID is the stable reserved ID for the system task
// that owns the #general channel.  Never produced by the counter.
const backupMigrationTaskID = "task-general"

// ensureBackupMigrationTaskLocked idempotently creates the "Backup &
// Migration" system task and links it to the #general channel.
//
// Caller MUST hold b.mu.  Safe to call multiple times — skips creation
// when the task already exists.  Also skips when the #general channel
// does not yet exist (caller must ensure the channel is seeded first).
func (b *Broker) ensureBackupMigrationTaskLocked() {
	// Idempotency guard: skip if already present.
	for i := range b.tasks {
		if b.tasks[i].ID == backupMigrationTaskID {
			return
		}
	}

	// Guard: #general must exist before we link a task to it.
	if b.findChannelLocked("general") == nil {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	task := teamTask{
		ID:        backupMigrationTaskID,
		Channel:   "general",
		Title:     "Backup & Migration",
		Details:   "Holds migrated #general history and uncategorized system messages.",
		Owner:     "",
		CreatedBy: "system",
		System:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Append first, then apply state to the slice element so the lifecycle
	// index and b.tasks never diverge mid-call (same pattern as the channel
	// migration). Apply LifecycleStateArchived so derived fields
	// (status="archived", pipeline_stage="archived", review_state="approved",
	// blocked=false) are set and the lifecycle index is updated. Errors are
	// intentionally ignored: LifecycleStateArchived is canonical and
	// derivedFieldsFor will always find it.
	b.tasks = append(b.tasks, task)
	_ = b.applyLifecycleStateLocked(&b.tasks[len(b.tasks)-1], LifecycleStateArchived)
}
