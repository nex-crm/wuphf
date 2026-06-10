package team

// task_ledger.go — U2.3 per-(agent,task) state ledger + U3.3 living task
// brief (docs/specs/sota-uplift.md).
//
// Every headless turn on a task leaves a distilled journal entry: who acted,
// what they said last, which task mutations they made, and how the turn
// ended. The journal is stored on the task and rendered into every
// participant's execution packet, so turn N+1 — by the same agent or a
// teammate — starts from what turn N tried instead of from amnesia. This is
// the deterministic continuity substrate: fresh sessions stay (crash-safe,
// provider-agnostic), but working memory survives between wakes.
//
// Entries are assembled from broker-observable facts (messages posted,
// action log records, turn outcome), not from asking the model to summarize
// itself — no extra LLM call, no self-report to trust.

import (
	"fmt"
	"strings"
	"time"
)

// TaskLedgerEntry is one turn's distilled record on a task.
type TaskLedgerEntry struct {
	Agent   string   `json:"agent"`
	At      string   `json:"at"`
	Outcome string   `json:"outcome"`           // "ok" or a short error reason
	Said    string   `json:"said,omitempty"`    // tail of the agent's last message this turn
	Actions []string `json:"actions,omitempty"` // task/action-log mutations made this turn
	// ContextUsed is the manifest of knowledge items the turn's work packet
	// injected ("learning:<id>", "wiki:<ref>", "upstream:<task>",
	// "journal:<task>"). Recorded deterministically at packet-build time —
	// never model-self-reported — so the human can audit exactly what
	// context the agent was handed (B4). Additive wire field.
	ContextUsed []string `json:"context_used,omitempty"`
}

const (
	// taskLedgerMaxEntries bounds the persisted journal per task.
	taskLedgerMaxEntries = 20
	// taskLedgerRenderEntries is how many recent entries packets carry.
	taskLedgerRenderEntries = 6
	// taskLedgerSaidClip bounds the per-entry message tail.
	taskLedgerSaidClip = 600
)

// AppendTaskLedgerEntry stamps a journal entry onto the task and persists.
func (b *Broker) AppendTaskLedgerEntry(taskID string, entry TaskLedgerEntry) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || strings.TrimSpace(entry.Agent) == "" {
		return
	}
	if entry.At == "" {
		entry.At = time.Now().UTC().Format(time.RFC3339)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	task := b.taskByIDLocked(taskID)
	if task == nil {
		return
	}
	task.Ledger = append(task.Ledger, entry)
	if len(task.Ledger) > taskLedgerMaxEntries {
		task.Ledger = task.Ledger[len(task.Ledger)-taskLedgerMaxEntries:]
	}
	task.UpdatedAt = entry.At
	_ = b.saveLocked()
}

// recordTaskLedgerEntry assembles the journal entry for a finished headless
// turn from broker-observable facts. Called from the queue worker after the
// turn settles (no broker lock held on entry).
func (l *Launcher) recordTaskLedgerEntry(slug string, turn headlessCodexTurn, startedAt time.Time, turnErr error) {
	if l == nil || l.broker == nil {
		return
	}
	taskID := strings.TrimSpace(turn.TaskID)
	if taskID == "" {
		return
	}
	entry := TaskLedgerEntry{
		Agent:       slug,
		At:          time.Now().UTC().Format(time.RFC3339),
		Outcome:     "ok",
		ContextUsed: append([]string(nil), turn.ContextUsed...),
	}
	if turnErr != nil {
		entry.Outcome = truncate(turnErr.Error(), 200)
	}
	since := startedAt.UTC().Format(time.RFC3339)

	// Last thing the agent said in the task's channel this turn.
	if ch := normalizeChannelSlug(turn.Channel); ch != "" {
		msgs := l.broker.ChannelMessages(ch)
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.From != slug || m.Timestamp < since {
				continue
			}
			entry.Said = tailClip(m.Content, taskLedgerSaidClip)
			break
		}
	}

	// Task mutations the agent made this turn (action log is append-only;
	// RelatedID carries the task id for task_* records).
	for _, a := range l.broker.ActionsSince(since) {
		if a.Actor != slug || strings.TrimSpace(a.RelatedID) != taskID {
			continue
		}
		entry.Actions = append(entry.Actions, a.Kind+": "+truncate(a.Summary, 160))
	}

	if entry.Said == "" && len(entry.Actions) == 0 && entry.Outcome == "ok" {
		// A turn that left no observable trace records only its outcome —
		// still worth a line: "agent woke and produced nothing durable" is
		// exactly the signal reviewers and the next turn need.
		entry.Outcome = "ok (no durable trace: no message, no task mutation)"
	}
	l.broker.AppendTaskLedgerEntry(taskID, entry)
}

// ActionsSince returns action-log records stamped at or after the RFC3339
// timestamp. Snapshot copy; safe for concurrent use.
func (b *Broker) ActionsSince(since string) []officeActionLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []officeActionLog
	for _, a := range b.actions {
		if a.CreatedAt >= since {
			out = append(out, a)
		}
	}
	return out
}

// taskLedgerContext renders the living task brief for packets (U3.3): the
// recent journal so every participant wakes knowing what was already tried.
func taskLedgerContext(task teamTask) string {
	if len(task.Ledger) == 0 {
		return ""
	}
	entries := task.Ledger
	if len(entries) > taskLedgerRenderEntries {
		entries = entries[len(entries)-taskLedgerRenderEntries:]
	}
	var lines []string
	for _, e := range entries {
		line := fmt.Sprintf("- [%s @%s] %s", e.At, e.Agent, e.Outcome)
		if len(e.Actions) > 0 {
			line += " | " + strings.Join(e.Actions, "; ")
		}
		if e.Said != "" {
			line += " | said: " + e.Said
		}
		lines = append(lines, line)
	}
	return "TASK JOURNAL (what previous turns already did — continue from here, do not restart or re-discover):\n" + strings.Join(lines, "\n")
}
