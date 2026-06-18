package team

import "strings"

// broker_slack_cards.go owns the persisted registry of Slack task lifecycle
// cards: one pinned Block Kit card per ongoing task, posted into the task's
// Slack-surfaced channel and updated in place as the lifecycle advances. The
// registry maps task ID → posted card (channel, message ts, last rendered
// state) and is persisted in broker state so a restart updates the EXISTING
// card instead of posting a duplicate. The card sync loop that reads/writes
// this registry lives transport-side in slack_task_cards.go.

// slackTaskCardRecord is one posted task card. State is the lifecycle state
// the card last rendered — the sync loop only touches Slack when the task's
// current state differs. Pinned tracks whether the message currently holds a
// pin so terminal transitions unpin exactly once.
type slackTaskCardRecord struct {
	ChannelID string `json:"channel_id"`
	Timestamp string `json:"timestamp"`
	State     string `json:"state"`
	Pinned    bool   `json:"pinned,omitempty"`
}

// SlackTaskCard returns the posted card record for a task, if any.
func (b *Broker) SlackTaskCard(taskID string) (slackTaskCardRecord, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rec, ok := b.slackTaskCards[taskID]
	return rec, ok
}

// SetSlackTaskCard upserts a task's card record and persists state so the
// card↔task binding survives restarts.
func (b *Broker) SetSlackTaskCard(taskID string, rec slackTaskCardRecord) {
	if taskID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.slackTaskCards == nil {
		b.slackTaskCards = make(map[string]slackTaskCardRecord)
	}
	b.slackTaskCards[taskID] = rec
	_ = b.saveLocked()
}

// SlackThreadIsTaskRoot reports whether ts is the Slack thread-root of a known
// task card, i.e. a reply carrying this thread_ts is continuing work WUPHF
// already owns. The inbound passivity gate uses it so task-thread replies (a
// foreign agent's delegation reply, a human's follow-up) keep flowing even
// though WUPHF was not re-tagged.
func (b *Broker) SlackThreadIsTaskRoot(ts string) bool {
	if strings.TrimSpace(ts) == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.slackTaskByRootTSLocked(ts) != nil
}

// slackTaskByRootTSLocked returns the task whose Slack thread-root card has
// the given message ts, or nil. This is the reverse of the card registry: an
// inbound Slack reply carries the root ts as its thread_ts, and this maps it
// back to the task so the reply can be folded into that task's thread.
// Caller must hold b.mu.
func (b *Broker) slackTaskByRootTSLocked(ts string) *teamTask {
	ts = strings.TrimSpace(ts)
	if ts == "" || len(b.slackTaskCards) == 0 {
		return nil
	}
	for taskID, rec := range b.slackTaskCards {
		if rec.Timestamp == ts {
			return b.findTaskByIDLocked(taskID)
		}
	}
	return nil
}

// cloneSlackTaskCardsLocked snapshots the registry for persistence.
// Caller must hold b.mu.
func (b *Broker) cloneSlackTaskCardsLocked() map[string]slackTaskCardRecord {
	if len(b.slackTaskCards) == 0 {
		return nil
	}
	out := make(map[string]slackTaskCardRecord, len(b.slackTaskCards))
	for id, rec := range b.slackTaskCards {
		out[id] = rec
	}
	return out
}
