package team

// scheduler.go owns the watchdog scheduler — the goroutine that wakes up
// periodically, asks the broker for due jobs, and dispatches each one to
// a per-target-type processor (PLAN.md §C4). First goroutine extraction
// in the launcher decomposition.
//
// Test seams (PLAN.md §3):
//   - clock interface with realClock (production) and manualClock (tests).
//     The loop's two time.Sleep calls (initialDelay + pollEvery) become
//     clock.After channel reads that the manual clock can release on
//     command. Kills the user's hard "no time.Sleep in tests" rule.
//   - onTickDone signal channel. The Start() loop sends after each
//     processOnce so tests can synchronously assert on the recorded side
//     effects without polling.
//   - schedulerBroker interface, declared on the consumer side per Go
//     convention. The real *Broker satisfies it implicitly; tests pass a
//     recording stub.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/calendar"
	"github.com/nex-crm/wuphf/internal/config"
)

// clock is the small time interface the scheduler uses. Production wires
// realClock (delegating to time); tests wire manualClock (advance-driven).
type clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// schedulerBroker is the narrow consumer-side interface the scheduler
// requires from *Broker. Declared here (not on Broker) so the broker
// surface stays free of refactor-driven interfaces.
type schedulerBroker interface {
	DueSchedulerJobs() []schedulerJob
	FindTask(channel, id string) (teamTask, bool)
	FindRequest(channel, id string) (humanInterview, bool)
	UpdateSchedulerJobState(slug string, nextRun time.Time, status string) error
	CompleteSchedulerRun(slug string, nextRun time.Time, statusForJob string, run schedulerRun) error
	EnsureDirectChannel(agentSlug string) (string, error)
	CreateWatchdogAlert(kind, channel, targetType, targetID, owner, summary string) (watchdogAlert, bool, error)
	RecordSignals([]officeSignal) ([]officeSignalRecord, error)
	RecordDecision(kind, channel, summary, reason, owner string, signalIDs []string, requiresHuman, blocking bool) (officeDecisionRecord, error)
	RecordAction(kind, source, channel, actor, summary, related string, signalIDs []string, decisionID string) error
	ResumeTask(id, by, note string) (teamTask, bool, error)
	PostAutomationMessage(from, channel, title, body, alertID, source, displayName string, ccSlugs []string, replyTo string) (channelMessage, bool, error)
	UpdateSkillExecutionByWorkflowKey(key, status string, when time.Time) error
	SetSchedulerJob(job schedulerJob) error
	// PR 8 Lane G additions for cron registry support.
	SchedulerJobControl(slug string, defaultInterval time.Duration) (bool, time.Duration)
	updateSchedulerHeartbeat(slug, label string, intervalMinutes int, nextRun time.Time, status string, runStatus string)
}

// watchdogScheduler runs the periodic broker-driven watchdog loop. One
// goroutine started by Start; drained by Stop via stopCh + done WaitGroup.
type watchdogScheduler struct {
	broker      schedulerBroker
	clock       clock
	deliverTask func(officeActionLog, teamTask)

	initialDelay time.Duration
	pollEvery    time.Duration

	// mu coordinates Start/Stop so Stop-before-Start can't consume its
	// signal and orphan a later Start's goroutine. started/stopped are
	// the actual state; sync.Once isn't enough because the two Onces
	// don't observe each other's outcome.
	mu      sync.Mutex
	started bool
	stopped bool
	stopCh  chan struct{}
	done    sync.WaitGroup

	// runCtx is a cancelable derivation of the ctx passed to Start. Stop
	// cancels it so any in-flight downstream call (e.g. a workflow provider
	// blocked on a network request) returns promptly instead of pinning
	// done.Wait — which would otherwise pin Launcher.Kill.
	runCtx    context.Context
	runCancel context.CancelFunc

	// onTickDone, when non-nil, receives one struct after every
	// processOnce call. Tests use it to wait deterministically; production
	// leaves it nil so the loop has zero overhead.
	onTickDone chan<- struct{}

	// resolveWorkflowProvider, when non-nil, replaces the live registry
	// lookup in processWorkflowJob. Tests inject a stub returning a fake
	// action.Provider so the lookup-failure, execute-failure, success, and
	// cancellation branches are all reachable without spinning up a real
	// registry. Production leaves this nil and uses action.NewRegistryFromEnv.
	resolveWorkflowProvider func(name string) (action.Provider, error)
}

// Start spawns the scheduler goroutine. Idempotent — multiple calls are
// no-ops after the first. If Stop ran before Start, Start is a no-op so
// the goroutine never spawns (the alternative would leak it). The
// returned scheduler keeps running until Stop is called or ctx is
// cancelled.
func (w *watchdogScheduler) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started || w.stopped {
		w.mu.Unlock()
		return
	}
	w.started = true
	if ctx == nil {
		ctx = context.Background()
	}
	w.runCtx, w.runCancel = context.WithCancel(ctx)
	w.stopCh = make(chan struct{})
	if w.initialDelay <= 0 {
		w.initialDelay = 15 * time.Second
	}
	if w.pollEvery <= 0 {
		w.pollEvery = 20 * time.Second
	}
	w.done.Add(1)
	runCtx := w.runCtx
	w.mu.Unlock()
	go w.run(runCtx)
}

func (w *watchdogScheduler) run(ctx context.Context) {
	defer w.done.Done()
	if w.broker == nil {
		return
	}
	if !w.wait(ctx, w.initialDelay) {
		return
	}
	for {
		w.processOnce()
		w.signalTick()
		if !w.wait(ctx, w.pollEvery) {
			return
		}
	}
}

// wait blocks for d, returning false when stopCh closes or ctx cancels
// (the loop should exit) and true when the deadline elapses normally.
func (w *watchdogScheduler) wait(ctx context.Context, d time.Duration) bool {
	select {
	case <-w.clock.After(d):
		return true
	case <-w.stopCh:
		return false
	case <-ctx.Done():
		return false
	}
}

func (w *watchdogScheduler) signalTick() {
	if w.onTickDone == nil {
		return
	}
	// Non-blocking when the test isn't reading. Tests that care about
	// every tick provide a buffered channel sized for the expected count.
	select {
	case w.onTickDone <- struct{}{}:
	default:
	}
}

// Stop signals the goroutine to exit and waits for it. Idempotent.
// Calling Stop before Start is supported: it disables a later Start
// from ever spawning the goroutine.
func (w *watchdogScheduler) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		w.done.Wait()
		return
	}
	w.stopped = true
	// Cancel runCtx first so any downstream blocking call (workflow
	// provider, etc.) unblocks before we sit on done.Wait.
	if w.runCancel != nil {
		w.runCancel()
	}
	if w.stopCh != nil {
		close(w.stopCh)
	}
	w.mu.Unlock()
	w.done.Wait()
}

// processOnce processes every currently-due job. Exposed (lowercase
// method, same package) so tests can drive a single tick deterministically
// without going through Start.
func (w *watchdogScheduler) processOnce() {
	if w.broker == nil {
		return
	}
	jobs := w.broker.DueSchedulerJobs()
	if len(jobs) == 0 {
		return
	}
	for _, job := range jobs {
		// PR 8 Lane G: per-instance jobs (task_follow_up, request_follow_up,
		// task_reminder, task_recheck) inherit the Enabled state of their
		// class-level system cron. If a human disables "task_reminder" from
		// the Calendar app, every existing per-task reminder skips its
		// dispatch this tick. The job is rescheduled (not killed) so flipping
		// Enabled back on resumes naturally without orphaning state.
		if classSlug := strings.TrimSpace(job.Kind); classSlug != "" {
			if classEnabled, _ := w.broker.SchedulerJobControl(classSlug, time.Duration(job.IntervalMinutes)*time.Minute); !classEnabled {
				next := w.clock.Now().UTC().Add(5 * time.Minute)
				_ = w.broker.UpdateSchedulerJobState(job.Slug, next, "disabled")
				continue
			}
		}
		switch strings.TrimSpace(job.TargetType) {
		case "task":
			w.processTaskJob(job)
		case "request":
			w.processRequestJob(job)
		case "workflow":
			w.processWorkflowJob(job)
		case "agent":
			w.processAgentJob(job)
		default:
			// Unknown target type — keep the row alive at its scheduled
			// cadence so the user can still pause / edit it from the UI,
			// but don't fire anything until a dispatcher claims this kind.
			nextRun := nextRoutineRun(job, w.clock.Now().UTC())
			_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		}
	}
}

func (w *watchdogScheduler) processTaskJob(job schedulerJob) {
	task, ok := w.broker.FindTask(job.Channel, job.TargetID)
	if !ok || strings.EqualFold(strings.TrimSpace(task.status), "done") {
		_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		return
	}
	now := w.clock.Now().UTC()
	if task.blocked {
		// Blocked tasks are legitimately waiting on dependencies — skip
		// the watchdog reminder. Owner cannot act until blockers resolve.
		// External-workflow rate-limit retry path stays here so a
		// throttled live-execute task auto-resumes when the cooldown ends.
		if retryAt, rateLimited := externalWorkflowRetryAfter(errors.New(task.Details), now); rateLimited && !retryAt.After(now) {
			resumeNote := "Retry window passed; resuming live external lane automatically."
			resumed, changed, err := w.broker.ResumeTask(task.ID, "watchdog", resumeNote)
			if err == nil && changed {
				_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
				if w.deliverTask != nil {
					w.deliverTask(officeActionLog{
						Kind:      "task_unblocked",
						Source:    "watchdog",
						Channel:   resumed.Channel,
						Actor:     "watchdog",
						RelatedID: resumed.ID,
					}, resumed)
				}
				return
			}
		}
		nextRun := w.clock.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
		_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		return
	}
	alertKind := "task_stalled"
	var summary string
	if strings.TrimSpace(task.Owner) == "" {
		alertKind = "task_unclaimed"
		summary = fmt.Sprintf("Task %s in #%s still has no owner.", task.Title, normalizeChannelSlug(task.Channel))
	} else {
		summary = fmt.Sprintf("@%s still needs to move %s in #%s.", task.Owner, task.Title, normalizeChannelSlug(task.Channel))
	}
	_, _, _ = w.broker.CreateWatchdogAlert(alertKind, task.Channel, "task", task.ID, task.Owner, summary)
	signalIDs, decisionID := w.recordLedger(task.Channel, alertKind, task.ID, task.Owner, summary, task.SourceSignalID)
	_ = w.broker.RecordAction("watchdog_alert", "watchdog", task.Channel, "watchdog", truncate(summary, 140), task.ID, signalIDs, decisionID)
	if w.deliverTask != nil {
		w.deliverTask(officeActionLog{
			Kind:      "watchdog_alert",
			Source:    "watchdog",
			Channel:   task.Channel,
			Actor:     "watchdog",
			RelatedID: task.ID,
		}, task)
	}
	nextRun := w.clock.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
	_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
}

func (w *watchdogScheduler) processRequestJob(job schedulerJob) {
	req, ok := w.broker.FindRequest(job.Channel, job.TargetID)
	if !ok || !requestIsActive(req) {
		_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		return
	}
	summary := fmt.Sprintf("Still waiting on %s in #%s: %s", req.TitleOrDefault(), normalizeChannelSlug(req.Channel), truncate(req.Question, 120))
	alert, existing, _ := w.broker.CreateWatchdogAlert("request_waiting", req.Channel, "request", req.ID, req.From, summary)
	signalIDs, decisionID := w.recordLedger(req.Channel, "request_waiting", req.ID, req.From, summary, "")
	_ = w.broker.RecordAction("watchdog_alert", "watchdog", req.Channel, "watchdog", truncate(summary, 140), req.ID, signalIDs, decisionID)
	if req.Blocking && !existing {
		_, _, _ = w.broker.PostAutomationMessage(
			"wuphf",
			req.Channel,
			"Waiting on human decision",
			summary,
			alert.ID,
			"watchdog",
			"Office watchdog",
			[]string{"ceo"},
			req.ReplyTo,
		)
	}
	nextRun := w.clock.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
	_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
}

func (w *watchdogScheduler) processWorkflowJob(job schedulerJob) {
	if w.broker == nil {
		return
	}
	type workflowSchedulePayload struct {
		Provider     string         `json:"provider"`
		WorkflowKey  string         `json:"workflow_key"`
		Inputs       map[string]any `json:"inputs"`
		ScheduleExpr string         `json:"schedule_expr"`
		CreatedBy    string         `json:"created_by"`
		Channel      string         `json:"channel"`
		SkillName    string         `json:"skill_name"`
	}
	var payload workflowSchedulePayload
	if strings.TrimSpace(job.Payload) != "" {
		_ = json.Unmarshal([]byte(job.Payload), &payload)
	}
	workflowKey := strings.TrimSpace(payload.WorkflowKey)
	if workflowKey == "" {
		workflowKey = strings.TrimSpace(job.WorkflowKey)
	}
	if workflowKey == "" {
		_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		return
	}
	channel := normalizeChannelSlug(payload.Channel)
	if channel == "" {
		channel = normalizeChannelSlug(job.Channel)
	}
	if channel == "" {
		channel = "general"
	}
	providerName := strings.TrimSpace(payload.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(job.Provider)
	}
	resolve := w.resolveWorkflowProvider
	if resolve == nil {
		resolve = func(name string) (action.Provider, error) {
			return action.NewRegistryFromEnv().ProviderNamed(name, action.CapabilityWorkflowExecute)
		}
	}
	provider, err := resolve(providerName)
	if err != nil {
		source := providerName
		if strings.TrimSpace(source) == "" {
			source = "workflow"
		}
		summary := fmt.Sprintf("Scheduled workflow %s could not start: %v", workflowKey, err)
		_ = w.broker.RecordAction("external_workflow_failed", source, channel, "scheduler", truncate(summary, 140), workflowKey, nil, "")
		_ = w.broker.UpdateSkillExecutionByWorkflowKey(workflowKey, "failed", w.clock.Now().UTC())
		if nextRun, hasNext := nextWorkflowRun(strings.TrimSpace(payload.ScheduleExpr), w.clock.Now().UTC()); hasNext {
			_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		} else {
			_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		}
		return
	}
	execCtx := w.runCtx
	if execCtx == nil {
		execCtx = context.Background()
	}
	result, err := provider.ExecuteWorkflow(execCtx, action.WorkflowExecuteRequest{
		KeyOrPath: workflowKey,
		Inputs:    payload.Inputs,
	})
	// Shutdown cancellation is not a failure — the workflow was pre-empted,
	// not rejected. Recording it as "failed" would corrupt persisted skill
	// state; bail out and let the job remain scheduled for the next run.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	now := w.clock.Now().UTC()
	nextRun, hasNext := nextWorkflowRun(strings.TrimSpace(payload.ScheduleExpr), now)
	if err != nil {
		summary := fmt.Sprintf("Scheduled workflow %s failed via %s", workflowKey, titleCaser.String(provider.Name()))
		_ = w.broker.RecordAction("external_workflow_failed", provider.Name(), channel, "scheduler", summary, workflowKey, nil, "")
		_ = w.broker.UpdateSkillExecutionByWorkflowKey(workflowKey, "failed", now)
		if hasNext {
			_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		} else {
			_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		}
		return
	}
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "completed"
	}
	summary := fmt.Sprintf("Scheduled workflow %s ran via %s", workflowKey, titleCaser.String(provider.Name()))
	_ = w.broker.RecordAction("external_workflow_executed", provider.Name(), channel, "scheduler", summary, workflowKey, nil, "")
	_ = w.broker.UpdateSkillExecutionByWorkflowKey(workflowKey, status, now)
	if hasNext {
		_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
	} else {
		_ = w.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
	}
}

// recordLedger writes a watchdog signal + a routing decision to the
// broker's audit trail. Returns the union of source signal IDs and any
// freshly-recorded ones, plus the decision ID for action correlation.
func (w *watchdogScheduler) recordLedger(channel, kind, targetID, owner, summary, sourceSignalID string) ([]string, string) {
	if w.broker == nil {
		return nil, ""
	}
	signal, err := w.broker.RecordSignals([]officeSignal{{
		ID:         strings.TrimSpace(kind) + "::" + strings.TrimSpace(targetID),
		Source:     "watchdog",
		Kind:       strings.TrimSpace(kind),
		Title:      "Office watchdog",
		Content:    strings.TrimSpace(summary),
		Channel:    channel,
		Owner:      strings.TrimSpace(owner),
		Confidence: "high",
		Urgency:    "high",
	}})
	if err != nil || len(signal) == 0 {
		return compactStringList([]string{sourceSignalID}), ""
	}
	signalIDs := make([]string, 0, len(signal)+1)
	signalIDs = append(signalIDs, compactStringList([]string{sourceSignalID})...)
	for _, record := range signal {
		signalIDs = append(signalIDs, record.ID)
	}
	decisionKind := "remind_owner"
	decisionReason := "The watchdog detected owned work with no visible movement, so the office should remind the current owner."
	decisionOwner := strings.TrimSpace(owner)
	requiresHuman := false
	blocking := false
	if decisionOwner == "" {
		decisionKind = "escalate_to_ceo"
		decisionReason = "The watchdog detected work without a live owner, so the CEO should re-triage it."
		decisionOwner = "ceo"
	}
	if kind == "request_waiting" {
		decisionKind = "ask_human"
		decisionReason = "The watchdog detected a pending human decision that is still blocking the office."
		decisionOwner = "ceo"
		requiresHuman = true
		blocking = true
	}
	decision, err := w.broker.RecordDecision(decisionKind, channel, summary, decisionReason, decisionOwner, signalIDs, requiresHuman, blocking)
	if err != nil {
		return signalIDs, ""
	}
	return signalIDs, decision.ID
}

// updateJob is a small helper still used by the launcher's Launch() path
// to seed the persisted job state on startup. PR 8 Lane G: routes through
// updateSchedulerHeartbeat so the user-controlled cron-registry fields
// (Enabled, IntervalOverride, SystemManaged, LastRunStatus) survive each
// tick instead of being clobbered by SetSchedulerJob's full-row replace.
func (w *watchdogScheduler) updateJob(slug, label string, interval time.Duration, nextRun time.Time, status string) {
	if w.broker == nil {
		return
	}
	w.broker.updateSchedulerHeartbeat(slug, label, int(interval/time.Minute), nextRun, status, "")
}

// processAgentJob fires an agent-targeted routine. The routine's payload
// (or label, when payload is empty) is posted as an automation message
// into the routine's channel, tagged at the owning agent so the agent's
// loop picks it up. Re-arms via CompleteSchedulerRun so the job stays
// schedulable for the next tick AND the Runs tab gets a detailed trace.
func (w *watchdogScheduler) processAgentJob(job schedulerJob) {
	if w.broker == nil {
		return
	}
	now := w.clock.Now().UTC()
	agentSlug := strings.TrimSpace(job.TargetID)
	nextRun := nextRoutineRun(job, now)
	startedAt := now.Format(time.RFC3339)

	if agentSlug == "" {
		// Misconfigured — keep the schedule alive but record a failed run
		// so the user can see why nothing landed in the channel.
		_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", schedulerRun{
			Slug:        job.Slug,
			StartedAt:   startedAt,
			Status:      "failed",
			Message:     "Routine has no owning agent — set target_id in Edit",
			TriggeredBy: "scheduler",
			TargetType:  job.TargetType,
			TargetID:    job.TargetID,
			Events:      []string{"Skipped: no owner assigned"},
		})
		return
	}

	// Resolve the destination channel. An explicit job.Channel always
	// wins. Otherwise we route to the owner's DM. We must NOT fall back
	// to #general when DM resolution fails — routines can carry
	// owner-specific payloads (drafts, prompts, status pings) that
	// should never leak into a shared channel because of a transient
	// channel-store error. Record the fire as failed and skip posting.
	channel := normalizeChannelSlug(job.Channel)
	if channel == "" {
		dm, err := w.broker.EnsureDirectChannel(agentSlug)
		if err != nil {
			_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", schedulerRun{
				Slug:        job.Slug,
				StartedAt:   startedAt,
				Status:      "failed",
				Message:     fmt.Sprintf("Could not resolve DM channel for @%s: %v", agentSlug, err),
				ErrorDetail: err.Error(),
				TriggeredBy: "scheduler",
				TargetType:  job.TargetType,
				TargetID:    job.TargetID,
				Events: []string{
					"EnsureDirectChannel failed — refusing to fall back to #general for an owner-targeted routine",
				},
			})
			return
		}
		channel = normalizeChannelSlug(dm)
	}
	if channel == "" {
		// Explicit empty job.Channel + a DM that came back blank. Treat
		// the same way as DM failure — never silently route to general.
		_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", schedulerRun{
			Slug:        job.Slug,
			StartedAt:   startedAt,
			Status:      "failed",
			Message:     "Routine has no destination channel — set channel or owner DM",
			TriggeredBy: "scheduler",
			TargetType:  job.TargetType,
			TargetID:    job.TargetID,
			Events:      []string{"Skipped: no channel resolved"},
		})
		return
	}

	body := strings.TrimSpace(job.Payload)
	if body == "" {
		body = strings.TrimSpace(job.Label)
	}
	if body == "" {
		body = fmt.Sprintf("Routine %s scheduled run", job.Slug)
	}
	// In a DM there's nobody else to disambiguate against, so an
	// @mention is just noise. In a shared channel we prefix with the
	// owner's slug so the assignment is obvious AND any downstream
	// text-scanning fallback picks it up (the Tagged[] field is the
	// load-bearing structural routing).
	isDM := isDirectChannelSlug(channel)
	if !isDM {
		mention := "@" + agentSlug
		if !strings.Contains(body, mention) {
			body = mention + " " + body
		}
	}
	title := strings.TrimSpace(job.Label)
	if title == "" {
		title = job.Slug
	}

	events := []string{
		fmt.Sprintf("Scheduler tick at %s UTC", now.Format(time.RFC3339)),
		fmt.Sprintf("Posting message to #%s tagging @%s", channel, agentSlug),
	}

	_, _, err := w.broker.PostAutomationMessage(
		"routine",
		channel,
		title,
		body,
		fmt.Sprintf("routine:%s:%d", job.Slug, now.Unix()),
		"routine-scheduler",
		"Routine scheduler",
		[]string{agentSlug},
		"",
	)
	if err != nil {
		events = append(events, fmt.Sprintf("PostAutomationMessage failed: %v", err))
		_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", schedulerRun{
			Slug:        job.Slug,
			StartedAt:   startedAt,
			Status:      "failed",
			Message:     fmt.Sprintf("Failed to post routine message: %v", err),
			ErrorDetail: err.Error(),
			TriggeredBy: "scheduler",
			TargetType:  job.TargetType,
			TargetID:    job.TargetID,
			Events:      events,
		})
		return
	}

	events = append(events, fmt.Sprintf("Posted; @%s notified", agentSlug))
	summary := fmt.Sprintf("Posted to #%s and notified @%s", channel, agentSlug)

	// Status MUST be "scheduled" (not "done") so schedulerJobDue keeps
	// picking up the routine on subsequent ticks. "done" terminates the
	// schedule, which is correct for one-shot jobs but wrong for crons.
	_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", schedulerRun{
		Slug:          job.Slug,
		StartedAt:     startedAt,
		Status:        "ok",
		OutputSummary: summary,
		TriggeredBy:   "scheduler",
		TargetType:    job.TargetType,
		TargetID:      job.TargetID,
		Events:        events,
	})
}

// isDirectChannelSlug returns true for the broker's DM channel-naming
// convention (`<agent>__human` or the migration shape `dm-<agent>`).
// Used by processAgentJob to decide whether to prefix the routine body
// with an @mention — DMs are 1:1 so the prefix is redundant noise.
func isDirectChannelSlug(slug string) bool {
	s := strings.TrimSpace(slug)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "dm-") {
		return true
	}
	if strings.Contains(s, "__") {
		return true
	}
	return false
}

// nextRoutineRun computes the next fire time for an agent-targeted
// routine. Prefers cron when present, falls back to interval_override
// or interval_minutes, and ultimately a 5-minute heartbeat so the row
// never goes silent — the caller can flip Enabled to actually pause it.
func nextRoutineRun(job schedulerJob, now time.Time) time.Time {
	if expr := strings.TrimSpace(job.ScheduleExpr); expr != "" {
		if next, ok := nextWorkflowRun(expr, now); ok {
			return next
		}
	}
	interval := job.IntervalOverride
	if interval <= 0 {
		interval = job.IntervalMinutes
	}
	if interval > 0 {
		return now.Add(time.Duration(interval) * time.Minute)
	}
	return now.Add(5 * time.Minute)
}

// nextWorkflowRun parses a cron expression and returns the next scheduled
// run after `after`. Returns ok=false for empty / malformed expressions
// or when the cron has no future occurrences.
func nextWorkflowRun(scheduleExpr string, after time.Time) (time.Time, bool) {
	scheduleExpr = strings.TrimSpace(scheduleExpr)
	if scheduleExpr == "" {
		return time.Time{}, false
	}
	sched, err := calendar.ParseCron(scheduleExpr)
	if err != nil {
		return time.Time{}, false
	}
	next := sched.Next(after)
	if next.IsZero() {
		return time.Time{}, false
	}
	return next, true
}
