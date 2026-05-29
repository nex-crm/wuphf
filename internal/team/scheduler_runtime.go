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
		now := time.Now().UTC()
		startedAt := b.scheduler[i].LastRun
		b.scheduler[i].Status = strings.TrimSpace(status)
		b.scheduler[i].LastRun = now.Format(time.RFC3339)
		if !nextRun.IsZero() {
			b.scheduler[i].NextRun = nextRun.UTC().Format(time.RFC3339)
			b.scheduler[i].DueAt = b.scheduler[i].NextRun
		} else {
			b.scheduler[i].NextRun = ""
			b.scheduler[i].DueAt = ""
		}
		// Persist a run entry only for terminal states emitted by the
		// scheduler ("scheduled" is just a re-arm tick after a fire, so a
		// single state transition produces one run entry — not two).
		if runStatusEmitsRecord(status) {
			// Resolve the run's status FIRST so a prior failure on the
			// row doesn't taint a fresh successful transition, then
			// stamp LastRunStatus so the list-view badge and the new
			// run entry agree on this fire's outcome.
			resolved := schedulerRunStatusFor(status, b.scheduler[i].LastRunStatus)
			b.scheduler[i].LastRunStatus = resolved
			run := schedulerRun{
				Slug:        b.scheduler[i].Slug,
				Status:      resolved,
				StartedAt:   firstNonEmptyStr(startedAt, now.Format(time.RFC3339)),
				FinishedAt:  now.Format(time.RFC3339),
				TriggeredBy: "scheduler",
				TargetType:  b.scheduler[i].TargetType,
				TargetID:    b.scheduler[i].TargetID,
			}
			b.recordSchedulerRunLocked(run)
		}
		return b.saveLocked()
	}
	return fmt.Errorf("scheduler job %q not found", slug)
}

// runStatusEmitsRecord returns true when the status string represents a
// fire that finished (success or failure). "scheduled" reflects the
// re-arming step and would double-count if we emitted on every call.
func runStatusEmitsRecord(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "done", "completed", "ok", "success", "failed", "error":
		return true
	}
	return false
}

// schedulerRunStatusFor normalises the status field on the persisted run.
// The current transition is authoritative; a prior LastRunStatus is
// consulted only when the caller passed no transition at all (which the
// scheduler doesn't, but defensive code paths might). Without this rule
// a stale "failed" badge from a previous fire would silently overwrite
// a freshly successful "done" transition as failed.
func schedulerRunStatusFor(transition, lastRunStatus string) string {
	t := strings.TrimSpace(strings.ToLower(transition))
	if t == "failed" || t == "error" {
		return "failed"
	}
	if t != "" {
		return t
	}
	ls := strings.TrimSpace(strings.ToLower(lastRunStatus))
	if ls == "failed" || ls == "error" {
		return "failed"
	}
	return "ok"
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// CompleteSchedulerRun records a rich run-history entry for one fire and
// re-arms the schedule for the next tick, atomically. Dispatchers that
// have richer state than the generic "done" transition (event trace,
// human-readable summary, structured error) should call this instead of
// UpdateSchedulerJobState so the Runs tab can show "what happened" on
// every fire instead of "no detail trace".
//
// statusForJob is what the job's Status column becomes; for a recurring
// routine pass "scheduled" so schedulerJobDue keeps picking it up. The
// run record's own Status field comes from run.Status — passing "ok" /
// "failed" / "completed" is conventional.
func (b *Broker) CompleteSchedulerRun(slug string, nextRun time.Time, statusForJob string, run schedulerRun) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.scheduler {
		if strings.TrimSpace(b.scheduler[i].Slug) != strings.TrimSpace(slug) {
			continue
		}
		now := time.Now().UTC()
		startedAt := b.scheduler[i].LastRun
		b.scheduler[i].Status = strings.TrimSpace(statusForJob)
		b.scheduler[i].LastRun = now.Format(time.RFC3339)
		// LastRunStatus drives the badge in the list view ("ok" / "failed"
		// / "ran"). Mirror the run's own status so the two surfaces never
		// disagree about the outcome of the same fire.
		if run.Status != "" {
			b.scheduler[i].LastRunStatus = strings.TrimSpace(run.Status)
		}
		if !nextRun.IsZero() {
			b.scheduler[i].NextRun = nextRun.UTC().Format(time.RFC3339)
			b.scheduler[i].DueAt = b.scheduler[i].NextRun
		} else {
			b.scheduler[i].NextRun = ""
			b.scheduler[i].DueAt = ""
		}
		// Run record. Caller supplies events/summary/etc; we just stamp
		// the slug and the started_at fallback so dispatchers don't have
		// to repeat that bookkeeping.
		if run.Slug == "" {
			run.Slug = b.scheduler[i].Slug
		}
		if run.StartedAt == "" {
			run.StartedAt = firstNonEmptyStr(startedAt, now.Format(time.RFC3339))
		}
		if run.FinishedAt == "" {
			run.FinishedAt = now.Format(time.RFC3339)
		}
		b.recordSchedulerRunLocked(run)
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
