package team

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

func (b *Broker) dueSchedulerJobsLocked(now time.Time) []schedulerJob {
	now = now.UTC()
	var out []schedulerJob
	for _, job := range b.scheduler {
		// Delegate to the canonical due predicate so jobs persisted with
		// only DueAt set (no NextRun) become visible here too. The earlier
		// inline NextRun-only check silently dropped DueAt-only jobs even
		// though /scheduler?due_only=true accepted them.
		if schedulerJobDue(job, now) {
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
	// Clear any previously-scheduled lifecycle jobs that don't match the
	// kind we're about to enqueue. The slugs differ between "task_follow_up"
	// and "recheck", so without this scheduleJobLocked won't find the old
	// entry to update — it stays around and keeps firing across active-state
	// transitions like queued → in_progress → blocked.
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		b.cancelSupersededTaskJobsLocked(task.ID, taskChannel, "task_follow_up")
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
		b.cancelSupersededTaskJobsLocked(task.ID, taskChannel, "recheck")
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

// cancelSupersededTaskJobsLocked marks any scheduled lifecycle job for the
// given task whose Kind differs from keepKind as done. Used during state
// transitions so a stale recheck doesn't keep firing alongside a fresh
// task_follow_up (or vice versa).
func (b *Broker) cancelSupersededTaskJobsLocked(taskID, channel, keepKind string) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range b.scheduler {
		job := &b.scheduler[i]
		if job.TargetType != "task" || job.TargetID != taskID {
			continue
		}
		if normalizeChannelSlug(job.Channel) != normalizeChannelSlug(channel) {
			continue
		}
		if job.Kind == keepKind {
			continue
		}
		if strings.EqualFold(job.Status, "done") || strings.EqualFold(job.Status, "canceled") {
			continue
		}
		job.Status = "done"
		job.DueAt = ""
		job.NextRun = ""
		job.LastRun = now
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

// systemCronSpec describes a self-registered cron's identity + default
// interval (PR 8 Lane G). Read-only crons (one-relay-events for v1) refuse
// PATCH; everything else can be throttled within its floor.
type systemCronSpec struct {
	Slug            string
	Label           string
	DefaultInterval func() int // minutes; resolved fresh at registration
	MinFloor        int        // minutes; minimum interval_override accepted
	ReadOnly        bool       // one-relay-events: hardcoded for v1
}

// systemCronSpecs is the v1 system cron registry. Order is alphabetical
// for deterministic startup. Each entry SHOULD be invisible in /scheduler
// today (or surface only sporadically); registration makes them
// configurable from the Calendar app.
func systemCronSpecs() []systemCronSpec {
	return []systemCronSpec{
		{
			Slug:            "nex-insights",
			Label:           "Nex insights",
			DefaultInterval: func() int { return config.ResolveInsightsPollInterval() },
			MinFloor:        30, // LLM-touching, quota burn — keep the floor high
		},
		{
			Slug:            "nex-notifications",
			Label:           "Nex notifications",
			DefaultInterval: func() int { return int(notificationPollInterval() / time.Minute) },
			MinFloor:        5,
		},
		{
			Slug:            "one-relay-events",
			Label:           "One relay events",
			DefaultInterval: func() int { return 1 },
			MinFloor:        1,
			ReadOnly:        true, // hardcoded for v1 — surface only
		},
		{
			Slug:            "request_follow_up",
			Label:           "Request follow-up reminders",
			DefaultInterval: func() int { return config.ResolveTaskFollowUpInterval() },
			MinFloor:        5,
		},
		{
			Slug:            "review-expiry",
			Label:           "Review expiry sweep",
			DefaultInterval: func() int { return 10 },
			MinFloor:        5,
		},
		{
			Slug:            "task_follow_up",
			Label:           "Task follow-up reminders",
			DefaultInterval: func() int { return config.ResolveTaskFollowUpInterval() },
			MinFloor:        5,
		},
		{
			Slug:            "task_recheck",
			Label:           "Task recheck cadence",
			DefaultInterval: func() int { return config.ResolveTaskRecheckInterval() },
			MinFloor:        5,
		},
		{
			Slug:            "task_reminder",
			Label:           "Task reminder cadence",
			DefaultInterval: func() int { return config.ResolveTaskReminderInterval() },
			MinFloor:        5,
		},
	}
}

// registerSystemCrons self-registers every system cron from
// systemCronSpecs. Idempotent: existing entries keep their Enabled and
// IntervalOverride values; only the Label / SystemManaged / IntervalMinutes
// (default) fields refresh, so a config change to the env-resolved default
// shows up the next time the broker starts.
//
// Takes b.mu internally — DO NOT call while holding the lock.
func (b *Broker) registerSystemCrons() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, spec := range systemCronSpecs() {
		defaultInterval := spec.DefaultInterval()
		if defaultInterval <= 0 {
			defaultInterval = 1
		}
		// Find existing — preserve user-controlled fields.
		var existing *schedulerJob
		for i := range b.scheduler {
			if b.scheduler[i].Slug == spec.Slug {
				existing = &b.scheduler[i]
				break
			}
		}
		if existing != nil {
			existing.Label = spec.Label
			existing.SystemManaged = true
			existing.IntervalMinutes = defaultInterval
			continue
		}
		b.scheduler = append(b.scheduler, schedulerJob{
			Slug:            spec.Slug,
			Label:           spec.Label,
			IntervalMinutes: defaultInterval,
			Status:          "scheduled",
			SystemManaged:   true,
			Enabled:         true,
		})
	}
	if err := b.saveLocked(); err != nil {
		log.Printf("registerSystemCronsLocked: saveLocked failed: %v", err)
	}
}

// updateSchedulerHeartbeat refreshes the in-memory scheduler entry for slug
// with the latest interval / next-run / status fields (PR 8 Lane G). When
// the slug isn't yet registered we fall through to a fresh entry so legacy
// subsystems that surface a cron without going through registerSystemCrons
// still appear in the Calendar app's System Schedules panel.
func (b *Broker) updateSchedulerHeartbeat(slug, label string, intervalMinutes int, nextRun time.Time, status string, runStatus string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range b.scheduler {
		if b.scheduler[i].Slug != slug {
			continue
		}
		b.scheduler[i].Label = label
		b.scheduler[i].IntervalMinutes = intervalMinutes
		if !nextRun.IsZero() {
			b.scheduler[i].NextRun = nextRun.UTC().Format(time.RFC3339)
		}
		if status != "" {
			b.scheduler[i].Status = status
		}
		if status == "sleeping" || runStatus != "" {
			b.scheduler[i].LastRun = now
		}
		if runStatus != "" {
			b.scheduler[i].LastRunStatus = runStatus
		}
		_ = b.saveLocked()
		return
	}
	// Slug missing — fall back to a fresh entry so legacy subsystems that
	// surface a cron without registerSystemCrons still appear.
	job := schedulerJob{
		Slug:            slug,
		Label:           label,
		IntervalMinutes: intervalMinutes,
		Status:          status,
		Enabled:         true,
	}
	if !nextRun.IsZero() {
		job.NextRun = nextRun.UTC().Format(time.RFC3339)
	}
	if status == "sleeping" || runStatus != "" {
		job.LastRun = now
	}
	if runStatus != "" {
		job.LastRunStatus = runStatus
	}
	job = normalizeSchedulerJob(job)
	b.scheduler = append(b.scheduler, job)
	_ = b.saveLocked()
}

// handleSchedulerSubpath dispatches /scheduler/{slug} requests. Currently
// only PATCH is supported (PR 8 Lane G); future verbs (delete, run-now)
// would land here too.
func (b *Broker) handleSchedulerSubpath(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/scheduler/")
	slug = strings.TrimSpace(slug)
	if slug == "" {
		http.Error(w, "scheduler slug required in path", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		b.handlePatchSchedulerJob(w, r, slug)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePatchSchedulerJob updates the Enabled flag and / or
// IntervalOverride on a registered cron. System-managed read-only crons
// (one-relay-events for v1) reject any change with 400.
//
//	PATCH /scheduler/{slug}
//	{ "enabled"?: bool, "interval_override"?: int }
//
// Validation:
//   - interval_override of 0 clears the override (fall back to default).
//   - interval_override > 0 must be >= the spec's MinFloor; otherwise 400.
//   - Unknown slug → 404.
//   - Read-only spec (one-relay-events) → 400 with reason.
func (b *Broker) handlePatchSchedulerJob(w http.ResponseWriter, r *http.Request, slug string) {
	var body struct {
		Enabled          *bool `json:"enabled,omitempty"`
		IntervalOverride *int  `json:"interval_override,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Enabled == nil && body.IntervalOverride == nil {
		http.Error(w, "at least one of enabled or interval_override required", http.StatusBadRequest)
		return
	}

	// Look up the spec for floor + read-only enforcement. System-cron specs
	// are the source of truth; non-system jobs (workflow / task follow-ups
	// scheduled per-instance) inherit a generic 5-minute floor.
	var spec *systemCronSpec
	for _, s := range systemCronSpecs() {
		if s.Slug == slug {
			cp := s
			spec = &cp
			break
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	var job *schedulerJob
	for i := range b.scheduler {
		if b.scheduler[i].Slug == slug {
			job = &b.scheduler[i]
			break
		}
	}
	if job == nil {
		http.Error(w, "scheduler job not found", http.StatusNotFound)
		return
	}
	if spec != nil && spec.ReadOnly {
		http.Error(w, fmt.Sprintf("scheduler job %q is read-only in v1", slug), http.StatusBadRequest)
		return
	}

	if body.IntervalOverride != nil {
		override := *body.IntervalOverride
		if override < 0 {
			http.Error(w, "interval_override must be >= 0", http.StatusBadRequest)
			return
		}
		floor := 5
		if spec != nil {
			floor = spec.MinFloor
		}
		if override > 0 && override < floor {
			http.Error(w, fmt.Sprintf("interval_override below floor (%d minutes)", floor), http.StatusBadRequest)
			return
		}
		job.IntervalOverride = override
	}
	if body.Enabled != nil {
		job.Enabled = *body.Enabled
		// Reflect in Status so /scheduler GET surfaces the disabled state
		// without requiring callers to read Enabled separately.
		if !job.Enabled {
			job.Status = "disabled"
		} else if strings.EqualFold(job.Status, "disabled") {
			job.Status = "scheduled"
		}
	}
	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist scheduler update", http.StatusInternalServerError)
		return
	}

	updated := *job
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"job": updated})
}
