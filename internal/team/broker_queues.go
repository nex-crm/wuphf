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
	out := make([]officeActionLog, len(b.actions))
	copy(out, b.actions)
	b.mu.Unlock()
	for i, action := range out {
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
	out := make([]officeSignalRecord, len(b.signals))
	copy(out, b.signals)
	b.mu.Unlock()
	for i, sig := range out {
		out[i] = sanitizeOfficeSignalRecord(sig)
	}
	return out
}

func (b *Broker) Decisions() []officeDecisionRecord {
	b.mu.Lock()
	out := make([]officeDecisionRecord, len(b.decisions))
	copy(out, b.decisions)
	b.mu.Unlock()
	for i, dec := range out {
		out[i] = sanitizeOfficeDecisionRecord(dec)
	}
	return out
}

func (b *Broker) Watchdogs() []watchdogAlert {
	b.mu.Lock()
	out := make([]watchdogAlert, len(b.watchdogs))
	copy(out, b.watchdogs)
	b.mu.Unlock()
	for i, alert := range out {
		out[i] = sanitizeWatchdogAlert(alert)
	}
	return out
}

func (b *Broker) Scheduler() []schedulerJob {
	b.mu.Lock()
	out := make([]schedulerJob, len(b.scheduler))
	copy(out, b.scheduler)
	b.mu.Unlock()
	for i, job := range out {
		out[i] = sanitizeSchedulerJob(job)
	}
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
	actions := make([]officeActionLog, len(b.actions))
	copy(actions, b.actions)
	signals := make([]officeSignalRecord, len(b.signals))
	copy(signals, b.signals)
	decisions := make([]officeDecisionRecord, len(b.decisions))
	copy(decisions, b.decisions)
	watchdogs := make([]watchdogAlert, len(b.watchdogs))
	copy(watchdogs, b.watchdogs)
	scheduler := make([]schedulerJob, len(b.scheduler))
	copy(scheduler, b.scheduler)
	due := b.dueSchedulerJobsLocked(time.Now().UTC())
	dueJobs := make([]schedulerJob, len(due))
	copy(dueJobs, due)
	b.mu.Unlock()
	for i, action := range actions {
		actions[i] = sanitizeOfficeActionLog(action)
	}
	for i, sig := range signals {
		signals[i] = sanitizeOfficeSignalRecord(sig)
	}
	for i, dec := range decisions {
		decisions[i] = sanitizeOfficeDecisionRecord(dec)
	}
	for i, alert := range watchdogs {
		watchdogs[i] = sanitizeWatchdogAlert(alert)
	}
	for i, job := range scheduler {
		scheduler[i] = sanitizeSchedulerJob(job)
	}
	for i, job := range dueJobs {
		dueJobs[i] = sanitizeSchedulerJob(job)
	}
	return queueSnapshot{
		Actions:   actions,
		Signals:   signals,
		Decisions: decisions,
		Watchdogs: watchdogs,
		Scheduler: scheduler,
		Due:       dueJobs,
	}
}
