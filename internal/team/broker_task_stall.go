package team

// broker_task_stall.go — silent-stall honesty for RUNNING tasks
// (ten-out-of-ten Wave F2). ICP-eval v3 [19:05:30]: a task sat
// running-but-silent for 21 minutes with a raw `signal: killed` exhaust as
// the only trace. The activity watchdog now stamps a visible stall marker
// (teamTask.StalledSince, rendered as a chip by the FE) plus ONE honest chat
// line into the task's channel when a running task has produced no
// observable trace for taskStallThreshold. The marker clears itself the
// moment fresh activity lands or the task leaves running.
//
// "Observable trace" is broker-observable agent output only:
//   - the task's UpdatedAt (bumped by every mutation and ledger append),
//   - the newest ledger entry,
//   - the newest NON-system message in the task's channel,
//   - the newest NON-system action related to the task.
//
// System bookkeeping (including the stall line itself) deliberately does not
// count — otherwise the watchdog's own post would clear its own marker.

import (
	"fmt"
	"strings"
	"time"
)

// taskStallThreshold is how long a RUNNING task may go without any
// observable trace before it is marked visibly stalled.
const taskStallThreshold = 10 * time.Minute

// taskStalledMessageKind is the chat kind on the one honest stall line.
const taskStalledMessageKind = "task_stalled"

// taskIsObservablyRunning mirrors the "an execution turn can naturally carry
// activity" predicate used by markHumanNoteOnChannelTasksLocked: the typed
// Running state, or a legacy task whose only signal is status=in_progress.
func taskIsObservablyRunning(task *teamTask) bool {
	if task == nil {
		return false
	}
	return task.LifecycleState == LifecycleStateRunning ||
		(task.LifecycleState == "" && strings.EqualFold(strings.TrimSpace(task.status), "in_progress"))
}

// taskTraceIndex holds per-channel / per-task latest-trace timestamps built
// from one pass over the message and action logs.
type taskTraceIndex struct {
	lastChannelMessage map[string]time.Time // channel slug -> newest non-system message
	lastTaskAction     map[string]time.Time // task id -> newest non-system related action
}

// buildTaskTraceIndexLocked scans b.messages (rolling cap) and b.actions
// once. Caller holds b.mu.
func (b *Broker) buildTaskTraceIndexLocked() taskTraceIndex {
	idx := taskTraceIndex{
		lastChannelMessage: make(map[string]time.Time),
		lastTaskAction:     make(map[string]time.Time),
	}
	for i := range b.messages {
		msg := &b.messages[i]
		if msg.From == "system" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err != nil {
			continue
		}
		ch := normalizeChannelSlug(msg.Channel)
		if ts.After(idx.lastChannelMessage[ch]) {
			idx.lastChannelMessage[ch] = ts
		}
	}
	for i := range b.actions {
		act := &b.actions[i]
		if act.Actor == "system" || strings.TrimSpace(act.RelatedID) == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, act.CreatedAt)
		if err != nil {
			continue
		}
		if ts.After(idx.lastTaskAction[act.RelatedID]) {
			idx.lastTaskAction[act.RelatedID] = ts
		}
	}
	return idx
}

// lastObservableTraceTime returns the newest broker-observable trace for the
// task, or the zero time when nothing is parseable (in which case the
// watchdog leaves the task alone — it cannot age it safely).
func lastObservableTraceTime(task *teamTask, idx taskTraceIndex) time.Time {
	var latest time.Time
	bump := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil && ts.After(latest) {
			latest = ts
		}
	}
	bump(task.UpdatedAt)
	bump(task.CreatedAt)
	if n := len(task.Ledger); n > 0 {
		bump(task.Ledger[n-1].At)
	}
	if ts := idx.lastChannelMessage[normalizeChannelSlug(task.Channel)]; ts.After(latest) {
		latest = ts
	}
	if ts := idx.lastTaskAction[task.ID]; ts.After(latest) {
		latest = ts
	}
	return latest
}

// markSilentRunningTasksLocked stamps StalledSince + posts the one honest
// chat line for every running task whose newest observable trace is older
// than threshold, and clears stale markers when activity resumed or the task
// left running. Returns true when any task changed (caller persists).
// Caller holds b.mu. The threshold is a parameter so evals and tests can
// drive it with a fixture clock; production passes taskStallThreshold.
func (b *Broker) markSilentRunningTasksLocked(now time.Time, threshold time.Duration) bool {
	changed := false
	var idx taskTraceIndex
	indexed := false
	for i := range b.tasks {
		task := &b.tasks[i]
		if task.System {
			continue
		}
		if !taskIsObservablyRunning(task) {
			if task.StalledSince != "" {
				task.StalledSince = ""
				changed = true
			}
			continue
		}
		if !indexed {
			idx = b.buildTaskTraceIndexLocked()
			indexed = true
		}
		trace := lastObservableTraceTime(task, idx)
		if trace.IsZero() {
			// No parseable trace at all — cannot age safely; leave it.
			continue
		}
		if now.Sub(trace) < threshold {
			if task.StalledSince != "" {
				// Activity resumed: retire the marker silently — the
				// fresh trace is itself the honest signal.
				task.StalledSince = ""
				changed = true
			}
			continue
		}
		if task.StalledSince != "" {
			// Already marked: exactly one chat line per stall episode.
			continue
		}
		task.StalledSince = now.UTC().Format(time.RFC3339)
		changed = true
		owner := strings.TrimSpace(task.Owner)
		ownerRef := "the owner"
		if owner != "" {
			ownerRef = "@" + owner
		}
		channel := normalizeChannelSlug(task.Channel)
		if channel == "" {
			channel = "general"
		}
		silentFor := now.Sub(trace).Round(time.Minute)
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:      fmt.Sprintf("msg-%d", b.counter),
			From:    "system",
			Channel: channel,
			Kind:    taskStalledMessageKind,
			Content: fmt.Sprintf("%s has produced no visible activity on %s for %s — investigating or stalled.", ownerRef, task.ID, silentFor),
			// Stamp with the watchdog's now (not wall clock) so a
			// fixture-clock eval produces a consistent record.
			Timestamp: now.UTC().Format(time.RFC3339),
		})
		// Audit-trail entry. Actor "system" keeps it out of the
		// observable-trace index, so the watchdog cannot clear its own
		// marker with its own bookkeeping.
		b.appendActionLocked(taskStalledMessageKind, "office", channel, "system",
			truncateSummary(fmt.Sprintf("no visible activity on %s for %s — marked stalled", task.ID, silentFor), 140), task.ID)
	}
	return changed
}
