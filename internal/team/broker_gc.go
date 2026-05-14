package team

// broker_gc.go provides garbage-collection helpers that cap the growth of
// in-memory broker collections. Without these, long-running brokers
// accumulate messages and completed tasks indefinitely, bloating the state
// file and slowing startup.

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// ── Messages ─────────────────────────────────────────────────────────────────

// maxMessagesFromEnv returns the rolling cap on in-memory channel messages.
// Defaults to 500; overridable via WUPHF_MAX_MESSAGES.
func maxMessagesFromEnv() int {
	if v, err := strconv.Atoi(os.Getenv("WUPHF_MAX_MESSAGES")); err == nil && v > 0 {
		return v
	}
	return defaultMaxMessages
}

// ── Tasks ────────────────────────────────────────────────────────────────────

// defaultTaskRetentionDays is how long completed (merged) tasks are kept
// before pruning. 7 days gives operators enough time to review outcomes
// while preventing unbounded growth.
const defaultTaskRetentionDays = 7

// taskRetentionFromEnv returns the retention duration for completed tasks.
// Overridable via WUPHF_TASK_RETENTION_DAYS.
func taskRetentionFromEnv() time.Duration {
	if v, err := strconv.Atoi(os.Getenv("WUPHF_TASK_RETENTION_DAYS")); err == nil && v > 0 {
		return time.Duration(v) * 24 * time.Hour
	}
	return defaultTaskRetentionDays * 24 * time.Hour
}

// pruneCompletedTasksLocked removes tasks in terminal lifecycle states
// (merged) that are older than the retention window. Caller MUST hold b.mu.
//
// Returns the number of tasks pruned. Called from saveLocked so pruning
// piggybacks on the existing persistence cadence without adding a separate
// timer.
func (b *Broker) pruneCompletedTasksLocked() int {
	retention := taskRetentionFromEnv()
	cutoff := time.Now().Add(-retention)
	pruned := 0

	kept := make([]teamTask, 0, len(b.tasks))
	for _, t := range b.tasks {
		if isTerminalTask(t) && taskCompletedBefore(t, cutoff) {
			pruned++
			continue
		}
		kept = append(kept, t)
	}

	if pruned > 0 {
		b.tasks = kept
		slog.Info("broker_gc: pruned completed tasks",
			"pruned", pruned, "remaining", len(b.tasks),
			"retention_days", int(retention.Hours()/24))
	}
	return pruned
}

// isTerminalTask returns true for tasks that are in a terminal lifecycle
// state and safe to prune.
func isTerminalTask(t teamTask) bool {
	return t.LifecycleState == LifecycleStateMerged
}

// taskCompletedBefore checks whether a task's UpdatedAt is before the cutoff.
// Falls back to CreatedAt if UpdatedAt is empty.
func taskCompletedBefore(t teamTask, cutoff time.Time) bool {
	ts := t.UpdatedAt
	if ts == "" {
		ts = t.CreatedAt
	}
	if ts == "" {
		return false // No timestamp — keep the task.
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false // Unparseable — keep the task.
	}
	return parsed.Before(cutoff)
}
