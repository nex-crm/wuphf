package team

import (
	"fmt"
	"strings"
	"time"
)

func (b *Broker) DueSchedulerJobs() []schedulerJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]schedulerJob(nil), b.dueSchedulerJobsLocked(time.Now().UTC())...)
}

// SchedulerJobControl returns (enabled, effective interval) for the named
// cron slug. effectiveInterval is IntervalOverride when non-zero, else the
// caller's defaultInterval. Run-loops call this once per tick (PR 8 Lane G):
//
//	enabled, interval := l.broker.SchedulerJobControl("nex-insights", config-default)
//	if !enabled { time.Sleep(interval); continue }
//	... do work ...
//	time.Sleep(interval)
//
// When the slug isn't registered (e.g. broker not yet seeded), returns
// (true, defaultInterval) so callers fall back to legacy behavior. ok=false
// is reserved for "slug found but caller passed an invalid default".
func (b *Broker) SchedulerJobControl(slug string, defaultInterval time.Duration) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, job := range b.scheduler {
		if job.Slug != slug {
			continue
		}
		interval := defaultInterval
		if job.IntervalOverride > 0 {
			interval = time.Duration(job.IntervalOverride) * time.Minute
		}
		return job.Enabled, interval
	}
	return true, defaultInterval
}

func (b *Broker) UpdateSchedulerJobState(slug string, nextRun time.Time, status string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.scheduler {
		if strings.TrimSpace(b.scheduler[i].Slug) != strings.TrimSpace(slug) {
			continue
		}
		b.scheduler[i].Status = strings.TrimSpace(status)
		b.scheduler[i].LastRun = time.Now().UTC().Format(time.RFC3339)
		if !nextRun.IsZero() {
			b.scheduler[i].NextRun = nextRun.UTC().Format(time.RFC3339)
			b.scheduler[i].DueAt = b.scheduler[i].NextRun
		} else {
			b.scheduler[i].NextRun = ""
			b.scheduler[i].DueAt = ""
		}
		return b.saveLocked()
	}
	return fmt.Errorf("scheduler job %q not found", slug)
}

func (b *Broker) FindTask(channel, taskID string) (teamTask, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	for _, task := range b.tasks {
		if normalizeChannelSlug(task.Channel) != channel {
			continue
		}
		if strings.TrimSpace(task.ID) == strings.TrimSpace(taskID) {
			return task, true
		}
	}
	return teamTask{}, false
}

func (b *Broker) FindRequest(channel, requestID string) (humanInterview, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	for _, req := range b.requests {
		reqChannel := normalizeChannelSlug(req.Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if reqChannel != channel {
			continue
		}
		if strings.TrimSpace(req.ID) == strings.TrimSpace(requestID) {
			return req, true
		}
	}
	return humanInterview{}, false
}

func (b *Broker) UpdateSkillExecutionByWorkflowKey(workflowKey, status string, when time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.skills {
		if strings.TrimSpace(b.skills[i].WorkflowKey) != strings.TrimSpace(workflowKey) {
			continue
		}
		if !when.IsZero() {
			b.skills[i].LastExecutionAt = when.UTC().Format(time.RFC3339)
		}
		b.skills[i].LastExecutionStatus = strings.TrimSpace(status)
		b.skills[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return b.saveLocked()
	}
	return nil
}
