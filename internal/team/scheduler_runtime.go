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
