package team

import "time"

// Queue snapshots and the per-queue read-only accessors. These are the
// office's cross-entity workflow surfaces — Actions / Signals /
// Decisions / Watchdogs / Scheduler — that don't have a single owning
// entity file. The QueueSnapshot type is the wire shape the broker
// returns from /office/snapshot.
//
// Per-entity queries (Messages, ChannelMessages, Requests, etc.) live
// next to their entity siblings: see broker_messages.go,
// broker_human.go, broker_tasks.go, broker_office_members.go,
// broker_office_channels.go, broker_dm.go.

func (b *Broker) Actions() []officeActionLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeActionLog, len(b.actions))
	for i, action := range b.actions {
		out[i] = sanitizeOfficeActionLog(action)
	}
	return out
}

func sanitizeOfficeActionLog(action officeActionLog) officeActionLog {
	action.Summary = redactSecretsInText(action.Summary)
	if len(action.SignalIDs) > 0 {
		action.SignalIDs = append([]string(nil), action.SignalIDs...)
	}
	return action
}

func (b *Broker) Signals() []officeSignalRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeSignalRecord, len(b.signals))
	copy(out, b.signals)
	return out
}

func (b *Broker) Decisions() []officeDecisionRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeDecisionRecord, len(b.decisions))
	copy(out, b.decisions)
	return out
}

func (b *Broker) Watchdogs() []watchdogAlert {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]watchdogAlert, len(b.watchdogs))
	copy(out, b.watchdogs)
	return out
}

func (b *Broker) Scheduler() []schedulerJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]schedulerJob, len(b.scheduler))
	copy(out, b.scheduler)
	return out
}

type queueSnapshot struct {
	Actions   []officeActionLog      `json:"actions"`
	Signals   []officeSignalRecord   `json:"signals,omitempty"`
	Decisions []officeDecisionRecord `json:"decisions,omitempty"`
	Watchdogs []watchdogAlert        `json:"watchdogs,omitempty"`
	Scheduler []schedulerJob         `json:"scheduler"`
	Due       []schedulerJob         `json:"due,omitempty"`
}

func (b *Broker) QueueSnapshot() queueSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return queueSnapshot{
		Actions:   append([]officeActionLog(nil), b.actions...),
		Signals:   append([]officeSignalRecord(nil), b.signals...),
		Decisions: append([]officeDecisionRecord(nil), b.decisions...),
		Watchdogs: append([]watchdogAlert(nil), b.watchdogs...),
		Scheduler: append([]schedulerJob(nil), b.scheduler...),
		Due:       append([]schedulerJob(nil), b.dueSchedulerJobsLocked(time.Now().UTC())...),
	}
}
