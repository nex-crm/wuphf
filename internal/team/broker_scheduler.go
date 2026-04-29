package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

func (b *Broker) dueSchedulerJobsLocked(now time.Time) []schedulerJob {
	now = now.UTC()
	var out []schedulerJob
	for _, job := range b.scheduler {
		if strings.EqualFold(strings.TrimSpace(job.Status), "done") || strings.EqualFold(strings.TrimSpace(job.Status), "canceled") {
			continue
		}
		target := strings.TrimSpace(job.NextRun)
		if target == "" {
			continue
		}
		dueAt, err := time.Parse(time.RFC3339, target)
		if err != nil {
			continue
		}
		if !dueAt.After(now) {
			out = append(out, job)
		}
	}
	return out
}

func (b *Broker) SetSchedulerJob(job schedulerJob) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	job = normalizeSchedulerJob(job)
	if job.Slug == "" {
		return fmt.Errorf("job slug required")
	}
	if err := b.scheduleJobLocked(job); err != nil {
		return err
	}
	return b.saveLocked()
}

func (b *Broker) ScheduleTaskFollowUp(taskID, channel, owner, label, payload string, when time.Time) error {
	return b.scheduleJob(schedulerJob{
		Slug:            normalizeSchedulerSlug("task_follow_up", channel, taskID),
		Kind:            "task_follow_up",
		Label:           label,
		TargetType:      "task",
		TargetID:        strings.TrimSpace(taskID),
		Channel:         normalizeChannelSlug(channel),
		IntervalMinutes: 0,
		DueAt:           when.UTC().Format(time.RFC3339),
		NextRun:         when.UTC().Format(time.RFC3339),
		Status:          "scheduled",
		Payload:         payload,
	})
}

func (b *Broker) ScheduleRequestFollowUp(requestID, channel, label, payload string, when time.Time) error {
	return b.scheduleJob(schedulerJob{
		Slug:            normalizeSchedulerSlug("request_follow_up", channel, requestID),
		Kind:            "request_follow_up",
		Label:           label,
		TargetType:      "request",
		TargetID:        strings.TrimSpace(requestID),
		Channel:         normalizeChannelSlug(channel),
		IntervalMinutes: 0,
		DueAt:           when.UTC().Format(time.RFC3339),
		NextRun:         when.UTC().Format(time.RFC3339),
		Status:          "scheduled",
		Payload:         payload,
	})
}

func (b *Broker) ScheduleRecheck(channel, targetType, targetID, label, payload string, when time.Time) error {
	return b.scheduleJob(schedulerJob{
		Slug:            normalizeSchedulerSlug("recheck", channel, targetType, targetID),
		Kind:            "recheck",
		Label:           label,
		TargetType:      strings.TrimSpace(targetType),
		TargetID:        strings.TrimSpace(targetID),
		Channel:         normalizeChannelSlug(channel),
		IntervalMinutes: 0,
		DueAt:           when.UTC().Format(time.RFC3339),
		NextRun:         when.UTC().Format(time.RFC3339),
		Status:          "scheduled",
		Payload:         payload,
	})
}

func (b *Broker) scheduleJob(job schedulerJob) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	job = normalizeSchedulerJob(job)
	if job.Slug == "" {
		return fmt.Errorf("job slug required")
	}
	if job.Channel == "" {
		job.Channel = "general"
	}
	if err := b.scheduleJobLocked(job); err != nil {
		return err
	}
	return b.saveLocked()
}

func (b *Broker) scheduleJobLocked(job schedulerJob) error {
	for i := range b.scheduler {
		if !schedulerJobMatches(b.scheduler[i], job) {
			continue
		}
		b.scheduler[i] = job
		return nil
	}
	b.scheduler = append(b.scheduler, job)
	return nil
}

func normalizeSchedulerSlug(parts ...string) string {
	var filtered []string
	for _, part := range parts {
		part = normalizeSlugPart(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, ":")
}

func normalizeSlugPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

func normalizeSchedulerJob(job schedulerJob) schedulerJob {
	job.Slug = strings.TrimSpace(job.Slug)
	job.Kind = strings.TrimSpace(job.Kind)
	job.Label = strings.TrimSpace(job.Label)
	job.TargetType = strings.TrimSpace(job.TargetType)
	job.TargetID = strings.TrimSpace(job.TargetID)
	job.Channel = normalizeChannelSlug(job.Channel)
	job.Provider = strings.TrimSpace(job.Provider)
	job.ScheduleExpr = strings.TrimSpace(job.ScheduleExpr)
	job.WorkflowKey = strings.TrimSpace(job.WorkflowKey)
	job.SkillName = strings.TrimSpace(job.SkillName)
	if job.Channel == "" {
		job.Channel = "general"
	}
	job.Payload = strings.TrimSpace(job.Payload)
	job.Status = strings.TrimSpace(job.Status)
	if job.Status == "" {
		job.Status = "scheduled"
	}
	if job.IntervalMinutes < 0 {
		job.IntervalMinutes = 0
	}
	if job.DueAt == "" && job.NextRun != "" {
		job.DueAt = job.NextRun
	}
	if job.NextRun == "" && job.DueAt != "" {
		job.NextRun = job.DueAt
	}
	return job
}

func schedulerJobMatches(existing, candidate schedulerJob) bool {
	if existing.Slug != "" && candidate.Slug != "" && existing.Slug == candidate.Slug {
		return true
	}
	if existing.Kind != "" && candidate.Kind != "" && existing.Kind != candidate.Kind {
		return false
	}
	if existing.TargetType != "" && candidate.TargetType != "" && existing.TargetType != candidate.TargetType {
		return false
	}
	if existing.TargetID != "" && candidate.TargetID != "" && existing.TargetID != candidate.TargetID {
		return false
	}
	if existing.Channel != "" && candidate.Channel != "" && existing.Channel != candidate.Channel {
		return false
	}
	return existing.Kind != "" && existing.Kind == candidate.Kind && existing.TargetType == candidate.TargetType && existing.TargetID == candidate.TargetID && existing.Channel == candidate.Channel
}

func schedulerJobDue(job schedulerJob, now time.Time) bool {
	if strings.EqualFold(job.Status, "done") || strings.EqualFold(job.Status, "canceled") {
		return false
	}
	if job.DueAt != "" {
		if due, err := time.Parse(time.RFC3339, job.DueAt); err == nil && !due.After(now) {
			return true
		}
	}
	if job.NextRun != "" {
		if due, err := time.Parse(time.RFC3339, job.NextRun); err == nil && !due.After(now) {
			return true
		}
	}
	return false
}

func (b *Broker) completeSchedulerJobsLocked(targetType, targetID, channel string) {
	for i := range b.scheduler {
		job := &b.scheduler[i]
		if targetType != "" && job.TargetType != targetType {
			continue
		}
		if targetID != "" && job.TargetID != targetID {
			continue
		}
		if channel != "" && job.Channel != "" && normalizeChannelSlug(job.Channel) != normalizeChannelSlug(channel) {
			continue
		}
		job.Status = "done"
		job.DueAt = ""
		job.NextRun = ""
		job.LastRun = time.Now().UTC().Format(time.RFC3339)
	}
}

func (b *Broker) scheduleTaskLifecycleLocked(task *teamTask) {
	if task == nil {
		return
	}
	normalizeTaskPlan(task)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	followUpMinutes := config.ResolveTaskFollowUpInterval()
	recheckMinutes := config.ResolveTaskRecheckInterval()
	reminderMinutes := config.ResolveTaskReminderInterval()
	now := time.Now().UTC()
	if strings.EqualFold(task.Status, "done") || strings.EqualFold(task.Status, "canceled") || strings.EqualFold(task.Status, "cancelled") {
		task.FollowUpAt = ""
		task.ReminderAt = ""
		task.RecheckAt = ""
		task.DueAt = ""
		b.completeSchedulerJobsLocked("task", task.ID, taskChannel)
		b.resolveWatchdogAlertsLocked("task", task.ID, taskChannel)
		return
	}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		due := now.Add(time.Duration(followUpMinutes) * time.Minute)
		task.FollowUpAt = due.Format(time.RFC3339)
		task.ReminderAt = due.Add(time.Duration(reminderMinutes) * time.Minute).Format(time.RFC3339)
		task.RecheckAt = due.Add(time.Duration(recheckMinutes) * time.Minute).Format(time.RFC3339)
		task.DueAt = task.FollowUpAt
		_ = b.scheduleJobLocked(normalizeSchedulerJob(schedulerJob{
			Slug:       normalizeSchedulerSlug("task_follow_up", taskChannel, task.ID),
			Kind:       "task_follow_up",
			Label:      "Follow up on " + task.Title,
			TargetType: "task",
			TargetID:   task.ID,
			Channel:    taskChannel,
			DueAt:      task.FollowUpAt,
			NextRun:    task.FollowUpAt,
			Status:     "scheduled",
			Payload:    task.Details,
		}))
	default:
		due := now.Add(time.Duration(recheckMinutes) * time.Minute)
		task.RecheckAt = due.Format(time.RFC3339)
		task.ReminderAt = due.Add(time.Duration(reminderMinutes) * time.Minute).Format(time.RFC3339)
		task.FollowUpAt = task.RecheckAt
		task.DueAt = task.RecheckAt
		_ = b.scheduleJobLocked(normalizeSchedulerJob(schedulerJob{
			Slug:       normalizeSchedulerSlug("recheck", taskChannel, "task", task.ID),
			Kind:       "recheck",
			Label:      "Recheck task " + truncateSummary(task.Title, 48),
			TargetType: "task",
			TargetID:   task.ID,
			Channel:    taskChannel,
			DueAt:      task.RecheckAt,
			NextRun:    task.RecheckAt,
			Status:     "scheduled",
			Payload:    task.Details,
		}))
	}
}

func (b *Broker) scheduleRequestLifecycleLocked(req *humanInterview) {
	if req == nil {
		return
	}
	reqChannel := normalizeChannelSlug(req.Channel)
	if reqChannel == "" {
		reqChannel = "general"
	}
	reminderMinutes := config.ResolveTaskReminderInterval()
	followUpMinutes := config.ResolveTaskFollowUpInterval()
	now := time.Now().UTC()
	if strings.EqualFold(req.Status, "answered") || strings.EqualFold(req.Status, "canceled") {
		req.DueAt = ""
		req.ReminderAt = ""
		req.RecheckAt = ""
		req.FollowUpAt = ""
		b.completeSchedulerJobsLocked("request", req.ID, reqChannel)
		b.resolveWatchdogAlertsLocked("request", req.ID, reqChannel)
		return
	}
	due := now.Add(time.Duration(reminderMinutes) * time.Minute)
	req.ReminderAt = due.Format(time.RFC3339)
	req.FollowUpAt = due.Add(time.Duration(followUpMinutes) * time.Minute).Format(time.RFC3339)
	req.RecheckAt = req.ReminderAt
	req.DueAt = req.ReminderAt
	_ = b.scheduleJobLocked(normalizeSchedulerJob(schedulerJob{
		Slug:       normalizeSchedulerSlug("request_follow_up", reqChannel, req.ID),
		Kind:       "request_follow_up",
		Label:      "Follow up on " + req.Title,
		TargetType: "request",
		TargetID:   req.ID,
		Channel:    reqChannel,
		DueAt:      req.ReminderAt,
		NextRun:    req.ReminderAt,
		Status:     "scheduled",
		Payload:    req.Question,
	}))
}

func (b *Broker) handleScheduler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		jobs := make([]schedulerJob, 0, len(b.scheduler))
		dueOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("due_only")), "true")
		now := time.Now().UTC()
		for _, job := range b.scheduler {
			if dueOnly && !schedulerJobDue(job, now) {
				continue
			}
			jobs = append(jobs, job)
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
	case http.MethodPost:
		var body schedulerJob
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Slug) == "" || strings.TrimSpace(body.Label) == "" {
			http.Error(w, "slug and label required", http.StatusBadRequest)
			return
		}
		if err := b.SetSchedulerJob(body); err != nil {
			http.Error(w, "failed to persist scheduler job", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
