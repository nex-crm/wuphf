package team

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
)

var (
	headlessCodexLookPath       = exec.LookPath
	headlessCodexCommandContext = exec.CommandContext
	headlessCodexExecutablePath = os.Executable
	headlessCodexRunTurn        = func(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error {
		if l != nil {
			switch l.memberEffectiveProviderKind(slug) {
			case provider.KindCodex:
				return l.runHeadlessCodexTurn(ctx, slug, notification, channel...)
			case provider.KindOpencode:
				return l.runHeadlessOpencodeTurn(ctx, slug, notification, channel...)
			default:
				return l.runHeadlessClaudeTurn(ctx, slug, notification, channel...)
			}
		}
		return l.runHeadlessCodexTurn(ctx, slug, notification, channel...)
	}
	// headlessWakeLeadFn is nil in production; override in tests to intercept
	// lead wake-ups. Always access via headlessWakeLeadFnMu to avoid races
	// with leaked goroutines from concurrent tests.
	headlessWakeLeadFn   func(l *Launcher, specialistSlug string)
	headlessWakeLeadFnMu sync.RWMutex
)

var (
	headlessCodexTurnTimeout              = 4 * time.Minute
	headlessCodexOfficeLaunchTurnTimeout  = 10 * time.Minute
	headlessCodexLocalWorktreeTurnTimeout = 12 * time.Minute
	headlessCodexStaleCancelAfter         = 90 * time.Second
	// Minimum age an active turn must have before an enqueue can preempt
	// it via stale-cancel. Floors out tight re-enqueue loops where two
	// near-simultaneous enqueues would otherwise cancel each other on
	// arrival. Seen in prod (April 2026): CEO codex queue logged dozens
	// of `stale-turn: cancelling active turn after 0s` over hours because
	// the enqueue path exhausted the 90s threshold via clock-skew or a
	// malformed turn, causing back-to-back cancels that never produced
	// real work. 2s is long enough to absorb any legitimate rapid-fire
	// wake without blocking genuine preemption.
	headlessCodexMinTurnAgeBeforeCancel = 2 * time.Second
	headlessCodexEnvVarsToStrip         = []string{
		"OLDPWD",
		"PWD",
		"CODEX_THREAD_ID",
		"CODEX_TUI_RECORD_SESSION",
		"CODEX_TUI_SESSION_LOG_PATH",
	}
)

const headlessCodexLocalWorktreeRetryLimit = 2
const headlessCodexExternalActionRetryLimit = 1

type headlessCodexTurn struct {
	Prompt     string
	Channel    string // channel slug (e.g. "dm-ceo", "general")
	TaskID     string
	Attempts   int
	EnqueuedAt time.Time
}

type headlessCodexActiveTurn struct {
	Turn              headlessCodexTurn
	StartedAt         time.Time
	Timeout           time.Duration
	Cancel            context.CancelFunc
	WorkspaceDir      string
	WorkspaceSnapshot string
}

var headlessCodexWorkspaceStatusSnapshot = func(path string) string {
	path = normalizeHeadlessWorkspaceDir(path)
	if path == "" {
		return ""
	}
	out, err := runGitOutput(path, "status", "--porcelain=v1", "-z")
	if err != nil {
		return ""
	}
	return string(out)
}

func (l *Launcher) launchHeadlessCodex() error {
	killStaleBroker()
	killStaleHeadlessTaskRunners()
	_ = exec.Command("tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	l.broker = NewBroker()
	l.broker.packSlug = l.packSlug
	l.broker.blankSlateLaunch = l.blankSlateLaunch
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}
	if err := writeOfficePIDFile(); err != nil {
		return fmt.Errorf("write office pid: %w", err)
	}

	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())

	l.resumeInFlightWork()
	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.notifyOfficeChangesLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}

func (l *Launcher) enqueueHeadlessCodexTurn(slug string, prompt string, channel ...string) {
	ch := ""
	if len(channel) > 0 {
		ch = channel[0]
	}
	slug = strings.TrimSpace(slug)
	prompt = strings.TrimSpace(prompt)
	if slug == "" || prompt == "" {
		return
	}
	l.enqueueHeadlessCodexTurnRecord(slug, headlessCodexTurn{
		Prompt:     prompt,
		Channel:    ch,
		TaskID:     headlessCodexTaskID(prompt),
		EnqueuedAt: time.Now(),
	})
}

func (l *Launcher) enqueueHeadlessCodexTurnRecord(slug string, turn headlessCodexTurn) {
	slug = strings.TrimSpace(slug)
	turn.Prompt = strings.TrimSpace(turn.Prompt)
	turn.Channel = strings.TrimSpace(turn.Channel)
	turn.TaskID = strings.TrimSpace(turn.TaskID)
	if slug == "" || turn.Prompt == "" {
		return
	}
	if turn.TaskID == "" {
		turn.TaskID = headlessCodexTaskID(turn.Prompt)
	}
	if turn.EnqueuedAt.IsZero() {
		turn.EnqueuedAt = time.Now()
	}

	var cancel context.CancelFunc
	var staleAge time.Duration
	startWorker := false

	l.headlessMu.Lock()
	if l.headlessQueues == nil {
		l.headlessQueues = make(map[string][]headlessCodexTurn)
	}
	if l.headlessActive == nil {
		l.headlessActive = make(map[string]*headlessCodexActiveTurn)
	}
	if l.headlessWorkers == nil {
		l.headlessWorkers = make(map[string]bool)
	}
	urgentLeadTurn := l.headlessLeadTurnNeedsImmediateWakeLocked(slug, turn.Prompt)
	if turn.TaskID != "" {
		if active := l.headlessActive[slug]; active != nil && strings.TrimSpace(active.Turn.TaskID) == turn.TaskID {
			if !(slug == l.officeLeadSlug() && urgentLeadTurn) && turn.Attempts <= active.Turn.Attempts {
				l.headlessMu.Unlock()
				if slug == l.officeLeadSlug() {
					appendHeadlessCodexLog(slug, "queue-drop: lead already handling same task")
				} else {
					appendHeadlessCodexLog(slug, "queue-drop: agent already handling same task")
				}
				return
			}
		}
		if pending := l.replaceDuplicateTaskTurnLocked(slug, turn); pending {
			if !l.headlessWorkers[slug] {
				l.headlessWorkers[slug] = true
				startWorker = true
			}
			l.headlessMu.Unlock()
			if slug == l.officeLeadSlug() {
				appendHeadlessCodexLog(slug, "queue-replace: refreshed pending lead turn for same task")
			} else {
				appendHeadlessCodexLog(slug, "queue-replace: refreshed pending turn for same task")
			}
			if startWorker {
				go l.runHeadlessCodexQueue(slug)
			}
			return
		}
	}
	// For the lead (CEO) agent, suppress the notification if any other specialist
	// is still active or has pending work. The lead should only step in when all
	// parallel work is done — not when one specialist finishes while others are
	// still running. This eliminates the race condition where CEO fires after the
	// first specialist completes and redundantly re-routes to still-running agents.
	if slug == l.officeLeadSlug() && !urgentLeadTurn {
		for workerSlug, queue := range l.headlessQueues {
			if workerSlug == slug {
				continue
			}
			if len(queue) > 0 {
				l.headlessDeferredLead = &turn
				l.headlessMu.Unlock()
				appendHeadlessCodexLog(slug, "queue-hold: specialist still queued, deferring lead notification until all work lands")
				return
			}
		}
		for workerSlug, active := range l.headlessActive {
			if workerSlug == slug {
				continue
			}
			if active != nil {
				l.headlessDeferredLead = &turn
				l.headlessMu.Unlock()
				appendHeadlessCodexLog(slug, "queue-hold: specialist still running, deferring lead notification until all work lands")
				return
			}
		}
	}
	// For the lead (CEO) agent, cap the pending queue at 1 turn.
	// Multiple rapid-fire notifications (agent completions, status pings) can
	// stack up redundant CEO turns that each re-route the same task. One pending
	// turn is enough to catch the latest state; extras are dropped.
	const leadMaxPending = 1
	if slug == l.officeLeadSlug() && len(l.headlessQueues[slug]) >= leadMaxPending {
		if urgentLeadTurn {
			l.headlessQueues[slug][len(l.headlessQueues[slug])-1] = turn
			if !l.headlessWorkers[slug] {
				l.headlessWorkers[slug] = true
				startWorker = true
			}
			l.headlessMu.Unlock()
			appendHeadlessCodexLog(slug, "queue-replace: lead queue at cap, replacing pending turn with urgent task notification")
			if startWorker {
				go l.runHeadlessCodexQueue(slug)
			}
			return
		}
		l.headlessMu.Unlock()
		appendHeadlessCodexLog(slug, "queue-drop: lead queue at cap, dropping redundant notification")
		return
	}
	l.headlessQueues[slug] = append(l.headlessQueues[slug], turn)
	if !l.headlessWorkers[slug] {
		l.headlessWorkers[slug] = true
		startWorker = true
	}
	if active := l.headlessActive[slug]; active != nil && active.Cancel != nil {
		age := time.Since(active.StartedAt)
		// Two conditions must hold to preempt: past the configured staleness
		// threshold AND past the minimum-turn-age floor. The floor breaks the
		// tight cancel-loop pattern observed in prod (see doc on the constant).
		if age >= headlessCodexMinTurnAgeBeforeCancel &&
			age >= l.headlessCodexStaleCancelAfterForTurn(active.Turn) {
			cancel = active.Cancel
			staleAge = age
		}
	}
	l.headlessMu.Unlock()

	if cancel != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("stale-turn: cancelling active turn after %s to process queued work", staleAge.Round(time.Second)))
		l.updateHeadlessProgress(slug, "active", "queued", "preempting stale work for newer request", headlessProgressMetrics{})
		cancel()
	}
	if startWorker {
		go l.runHeadlessCodexQueue(slug)
	}
}

func (l *Launcher) replaceDuplicateTaskTurnLocked(slug string, turn headlessCodexTurn) bool {
	for i := range l.headlessQueues[slug] {
		if strings.TrimSpace(l.headlessQueues[slug][i].TaskID) != turn.TaskID {
			continue
		}
		l.headlessQueues[slug][i] = turn
		return true
	}
	if slug == l.officeLeadSlug() && l.headlessDeferredLead != nil && strings.TrimSpace(l.headlessDeferredLead.TaskID) == turn.TaskID {
		cp := turn
		l.headlessDeferredLead = &cp
		return true
	}
	return false
}

func (l *Launcher) headlessLeadTurnNeedsImmediateWakeLocked(slug, prompt string) bool {
	if l == nil || l.broker == nil {
		return false
	}
	if strings.TrimSpace(slug) != l.officeLeadSlug() {
		return false
	}
	taskID := strings.TrimSpace(headlessCodexTaskID(prompt))
	if taskID == "" {
		return false
	}
	for _, task := range l.broker.AllTasks() {
		if task.ID != taskID {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		review := strings.ToLower(strings.TrimSpace(task.ReviewState))
		return status == "review" || review == "ready_for_review" || status == "blocked"
	}
	return false
}

func (l *Launcher) runHeadlessCodexQueue(slug string) {
	for {
		func() {
			defer recoverPanicTo("runHeadlessCodexQueue", fmt.Sprintf("slug=%s", slug))
			turn, turnCtx, startedAt, timeout, ok := l.beginHeadlessCodexTurn(slug)
			if !ok {
				l.updateHeadlessProgress(slug, "idle", "idle", "waiting for work", headlessProgressMetrics{})
				return
			}
			appendHeadlessCodexLatency(slug, fmt.Sprintf("stage=started queue_wait_ms=%d", time.Since(turn.EnqueuedAt).Milliseconds()))
			l.updateHeadlessProgress(slug, "active", "queued", "queued work packet received", headlessProgressMetrics{})

			err := headlessCodexRunTurn(l, turnCtx, slug, turn.Prompt, turn.Channel)
			ctxErr := turnCtx.Err()
			isDurabilityError := false
			if err == nil {
				l.headlessMu.Lock()
				active := l.headlessActive[slug]
				l.headlessMu.Unlock()
				if ok, reason := l.headlessTurnCompletedDurably(slug, active); !ok {
					appendHeadlessCodexLog(slug, "durability-error: "+reason)
					err = errors.New(reason)
					isDurabilityError = true
				}
			}
			switch {
			case err == nil:
			case errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
				appendHeadlessCodexLog(slug, fmt.Sprintf("error: headless codex turn timed out after %s", timeout))
				l.updateHeadlessProgress(slug, "error", "error", fmt.Sprintf("turn timed out after %s", timeout), headlessProgressMetrics{})
				l.recoverTimedOutHeadlessTurn(slug, turn, startedAt, timeout)
			case errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled):
				appendHeadlessCodexLog(slug, "error: headless codex turn cancelled so newer queued work can run")
				l.updateHeadlessProgress(slug, "active", "queued", "restarting on newer queued work", headlessProgressMetrics{})
			case isDurabilityError:
				// The provider returned successfully but left no durable task state.
				// Don't retry — the agent already had its turn. Block the task immediately.
				appendHeadlessCodexLog(slug, fmt.Sprintf("error: %v", err))
				l.updateHeadlessProgress(slug, "error", "error", truncate(err.Error(), 180), headlessProgressMetrics{})
				exhaustedTurn := turn
				exhaustedTurn.Attempts = headlessCodexLocalWorktreeRetryLimit
				l.recoverFailedHeadlessTurn(slug, exhaustedTurn, startedAt, err.Error())
			default:
				appendHeadlessCodexLog(slug, fmt.Sprintf("error: %v", err))
				l.updateHeadlessProgress(slug, "error", "error", truncate(err.Error(), 180), headlessProgressMetrics{})
				l.recoverFailedHeadlessTurn(slug, turn, startedAt, err.Error())
			}
			l.finishHeadlessTurn(slug)
		}()
		l.headlessMu.Lock()
		_, stillRunning := l.headlessWorkers[slug]
		l.headlessMu.Unlock()
		if !stillRunning {
			return
		}
	}
}

func taskHasDurableCompletionState(task *teamTask) bool {
	if task == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	review := strings.ToLower(strings.TrimSpace(task.ReviewState))
	switch status {
	case "done", "completed", "blocked", "cancelled", "canceled", "review":
		return true
	}
	switch review {
	case "ready_for_review", "approved":
		return true
	}
	return false
}

func (l *Launcher) headlessTurnCompletedDurably(slug string, active *headlessCodexActiveTurn) (bool, string) {
	if l == nil || l.broker == nil || active == nil {
		return true, ""
	}
	task := l.timedOutTaskForTurn(slug, active.Turn)
	requiresDurableGuard := codingAgentSlugs[slug]
	requiresExternalExecution := taskRequiresRealExternalExecution(task)
	if task != nil && strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		requiresDurableGuard = true
	}
	if requiresExternalExecution {
		requiresDurableGuard = true
	}
	if !requiresDurableGuard {
		return true, ""
	}
	if task != nil && requiresExternalExecution {
		executed, attempted := l.taskHasExternalWorkflowEvidenceSince(task, active.StartedAt)
		if taskHasDurableCompletionState(task) {
			status := strings.ToLower(strings.TrimSpace(task.Status))
			switch status {
			case "done", "completed", "review":
				if executed {
					return true, ""
				}
				return false, fmt.Sprintf("external-action turn for #%s marked %s/%s without recorded external execution evidence", task.ID, strings.TrimSpace(task.Status), strings.TrimSpace(task.ReviewState))
			case "blocked", "cancelled", "canceled":
				if attempted {
					return true, ""
				}
				return false, fmt.Sprintf("external-action turn for #%s moved to %s without recorded external workflow evidence", task.ID, strings.TrimSpace(task.Status))
			default:
				if executed {
					return true, ""
				}
			}
		}
		if executed {
			return true, ""
		}
	}
	if task != nil && taskHasDurableCompletionState(task) {
		return true, ""
	}
	if l.agentPostedSubstantiveMessageSince(slug, active.StartedAt) {
		return true, ""
	}
	if workspaceDir := strings.TrimSpace(active.WorkspaceDir); workspaceDir != "" {
		current := headlessCodexWorkspaceStatusSnapshot(workspaceDir)
		if strings.TrimSpace(active.WorkspaceSnapshot) != "" && current != active.WorkspaceSnapshot {
			if task != nil {
				return false, fmt.Sprintf("coding turn for #%s changed workspace %s but left task %s/%s without durable completion evidence", task.ID, workspaceDir, strings.TrimSpace(task.Status), strings.TrimSpace(task.ReviewState))
			}
			return false, fmt.Sprintf("coding turn changed workspace %s without durable completion evidence", workspaceDir)
		}
	}
	if task != nil {
		if requiresExternalExecution {
			return false, fmt.Sprintf("external-action turn for #%s completed without durable task state or external workflow evidence", task.ID)
		}
		return false, fmt.Sprintf("coding turn for #%s completed without durable task state or completion evidence", task.ID)
	}
	if requiresExternalExecution {
		return false, fmt.Sprintf("external-action turn by @%s completed without durable task state or external workflow evidence", slug)
	}
	return false, fmt.Sprintf("coding turn by @%s completed without durable task state or completion evidence", slug)
}

func (l *Launcher) taskHasExternalWorkflowEvidenceSince(task *teamTask, startedAt time.Time) (executed bool, attempted bool) {
	if l == nil || l.broker == nil || task == nil {
		return false, false
	}
	channel := normalizeChannelSlug(task.Channel)
	owner := strings.TrimSpace(task.Owner)
	for _, action := range l.broker.Actions() {
		kind := strings.ToLower(strings.TrimSpace(action.Kind))
		switch kind {
		case "external_workflow_executed",
			"external_workflow_failed",
			"external_workflow_rate_limited",
			"external_action_executed",
			"external_action_failed":
		default:
			continue
		}
		if channel != "" && normalizeChannelSlug(action.Channel) != channel {
			continue
		}
		if owner != "" {
			actor := strings.TrimSpace(action.Actor)
			if actor != "" && actor != owner && actor != "scheduler" {
				continue
			}
		}
		when, err := time.Parse(time.RFC3339, strings.TrimSpace(action.CreatedAt))
		if err != nil {
			when, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(action.CreatedAt))
		}
		if err == nil && !when.Add(time.Second).After(startedAt) {
			continue
		}
		attempted = true
		if kind == "external_workflow_executed" || kind == "external_action_executed" {
			executed = true
		}
	}
	return executed, attempted
}

func (l *Launcher) finishHeadlessTurn(slug string) {
	l.headlessMu.Lock()
	if active := l.headlessActive[slug]; active != nil && active.Cancel != nil {
		active.Cancel()
	}
	delete(l.headlessActive, slug)
	lead := l.officeLeadSlug()
	var deferredLead *headlessCodexTurn
	// Determine if this was a specialist finishing (not the lead), and if so whether
	// any other specialists are still active or queued. If the slate is clear, we
	// need to wake the lead so it can react to the specialist's completion messages.
	// Without this, the CEO misses completion broadcasts because the queue-hold
	// fires while the specialist is still "active" (process running), and after the
	// process exits there is nothing else to re-trigger the CEO.
	shouldWakeLead := slug != lead && lead != ""
	if shouldWakeLead {
		for workerSlug, queue := range l.headlessQueues {
			if workerSlug == lead {
				continue
			}
			if len(queue) > 0 {
				shouldWakeLead = false
				break
			}
		}
	}
	if shouldWakeLead {
		for workerSlug, active := range l.headlessActive {
			if workerSlug == lead {
				continue
			}
			if active != nil {
				shouldWakeLead = false
				break
			}
		}
	}
	// Check if the lead already has work queued — no need to wake it.
	if shouldWakeLead && len(l.headlessQueues[lead]) > 0 {
		shouldWakeLead = false
	}
	if shouldWakeLead && l.headlessDeferredLead != nil {
		turn := *l.headlessDeferredLead
		l.headlessDeferredLead = nil
		deferredLead = &turn
		shouldWakeLead = false
	}
	l.headlessMu.Unlock()

	if deferredLead != nil {
		l.enqueueHeadlessCodexTurn(lead, deferredLead.Prompt, deferredLead.Channel)
		return
	}
	if shouldWakeLead {
		headlessWakeLeadFnMu.RLock()
		fn := headlessWakeLeadFn
		headlessWakeLeadFnMu.RUnlock()
		if fn != nil {
			fn(l, slug)
		} else {
			l.wakeLeadAfterSpecialist(slug)
		}
	}
}

// wakeLeadAfterSpecialist re-queues the lead (CEO) with the most recent message
// posted by the finishing specialist. This is needed because the lead's queue-hold
// suppresses notifications while a specialist is running, so the lead never sees
// the completion broadcast. We only do this when no other specialists remain active.
func (l *Launcher) wakeLeadAfterSpecialist(specialistSlug string) {
	if l.broker == nil {
		return
	}
	lead := l.officeLeadSlug()
	if lead == "" {
		return
	}
	targets := l.agentNotificationTargets()
	target, ok := targets[lead]
	if !ok {
		return
	}
	// Find the most recent substantive message from the specialist across all
	// channels. A specialist may complete work on a non-general channel (e.g.
	// "engineering" or "marketing"), so scanning only "general" would miss those
	// completions and the lead would never react.
	msgs := l.broker.AllMessages()
	var lastMsg *channelMessage
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.From != specialistSlug {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if strings.HasPrefix(content, "[STATUS]") {
			continue
		}
		lastMsg = &msgs[i]
		break
	}
	if lastMsg == nil {
		if action, task, ok := l.latestLeadWakeTaskAction(specialistSlug); ok {
			content := l.taskNotificationContent(action, task)
			appendHeadlessCodexLog(lead, fmt.Sprintf("wake-lead: re-delivering task handoff from @%s (%s)", specialistSlug, task.ID))
			l.sendTaskUpdate(target, action, task, content)
		}
		return
	}
	appendHeadlessCodexLog(lead, fmt.Sprintf("wake-lead: re-delivering specialist completion from @%s (msg %s)", specialistSlug, lastMsg.ID))
	l.sendChannelUpdate(target, *lastMsg)
}

func (l *Launcher) latestLeadWakeTaskAction(specialistSlug string) (officeActionLog, teamTask, bool) {
	if l == nil || l.broker == nil {
		return officeActionLog{}, teamTask{}, false
	}
	actions := l.broker.Actions()
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		if strings.TrimSpace(action.Actor) != specialistSlug {
			continue
		}
		if action.Kind != "task_updated" && action.Kind != "task_unblocked" {
			continue
		}
		task, ok := l.taskForAction(action)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "done", "review", "blocked":
			return action, task, true
		}
	}
	return officeActionLog{}, teamTask{}, false
}

func headlessCodexTaskID(prompt string) string {
	prefixes := []string{"#task-", "#blank-slate-"}
	for _, prefix := range prefixes {
		idx := strings.Index(prompt, prefix)
		if idx == -1 {
			continue
		}
		start := idx + 1
		end := start
		for end < len(prompt) {
			ch := prompt[end]
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
				end++
				continue
			}
			break
		}
		return strings.TrimSpace(prompt[start:end])
	}
	return ""
}

func (l *Launcher) agentPostedSubstantiveMessageSince(slug string, startedAt time.Time) bool {
	if l == nil || l.broker == nil {
		return false
	}
	for _, msg := range l.broker.AllMessages() {
		if msg.From != slug {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(content, "[STATUS]") {
			continue
		}
		when, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err != nil {
			continue
		}
		if when.Add(time.Second).After(startedAt) {
			return true
		}
	}
	return false
}

func (l *Launcher) agentPostedSubstantiveMessageToChannelSince(slug string, targetChannel string, startedAt time.Time) bool {
	if l == nil || l.broker == nil {
		return false
	}
	targetChannel = normalizeChannelSlug(targetChannel)
	if IsDMSlug(targetChannel) {
		if targetAgent := DMTargetAgent(targetChannel); targetAgent != "" {
			targetChannel = DMSlugFor(targetAgent)
		}
	}
	for _, msg := range l.broker.AllMessages() {
		if msg.From != slug {
			continue
		}
		if targetChannel != "" && normalizeChannelSlug(msg.Channel) != targetChannel {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(content, "[STATUS]") {
			continue
		}
		when, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err != nil {
			continue
		}
		if when.Add(time.Second).After(startedAt) {
			return true
		}
	}
	return false
}

func (l *Launcher) postHeadlessFinalMessageIfSilent(slug string, targetChannel string, notification string, text string, startedAt time.Time) (channelMessage, bool, error) {
	if l == nil || l.broker == nil {
		return channelMessage{}, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return channelMessage{}, false, nil
	}
	targetChannel = normalizeChannelSlug(targetChannel)
	if targetChannel == "" {
		targetChannel = "general"
	}
	if IsDMSlug(targetChannel) {
		if targetAgent := DMTargetAgent(targetChannel); targetAgent != "" {
			targetChannel = DMSlugFor(targetAgent)
		}
	}
	if l.agentPostedSubstantiveMessageToChannelSince(slug, targetChannel, startedAt) {
		return channelMessage{}, false, nil
	}
	msg, err := l.broker.PostMessage(slug, targetChannel, text, nil, headlessReplyToID(notification))
	if err != nil {
		return channelMessage{}, false, err
	}
	return msg, true, nil
}

func headlessReplyToID(notification string) string {
	const marker = `reply_to_id "`
	idx := strings.LastIndex(notification, marker)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	end := strings.Index(notification[start:], `"`)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(notification[start : start+end])
}

func (l *Launcher) timedOutTaskForTurn(slug string, turn headlessCodexTurn) *teamTask {
	if l == nil || l.broker == nil {
		return nil
	}
	if id := strings.TrimSpace(turn.TaskID); id != "" {
		for _, task := range l.broker.AllTasks() {
			if task.ID == id {
				cp := task
				return &cp
			}
		}
	}
	return l.agentActiveTask(slug)
}

func (l *Launcher) shouldRetryTimedOutHeadlessTurn(task *teamTask, turn headlessCodexTurn) bool {
	if task == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	return turn.Attempts < headlessCodexLocalWorktreeRetryLimit
}

func headlessTimedOutRetryPrompt(slug string, prompt string, timeout time.Duration, attempt int, external bool) string {
	note := fmt.Sprintf("Previous attempt by @%s timed out after %s without a durable task handoff. Retry #%d.", strings.TrimSpace(slug), timeout, attempt)
	if external {
		note += " This is a live external-action task. Do the smallest useful live external step now. If Slack target discovery is already known, use it. If the first live Slack target fails, retry once against the resolved writable target; if that still fails, pivot immediately to the smallest useful live Notion or Drive action and report the exact blocker. Do not write repo docs or planning artifacts as substitutes."
	} else {
		note += " For this retry, move immediately from claim/status into targeted file reads and edits, then leave the task in review/done/blocked before you stop. If you cannot ship the whole slice, ship the smallest runnable sub-slice and mark that state explicitly."
	}
	if strings.TrimSpace(prompt) == "" {
		return note
	}
	return strings.TrimSpace(prompt) + "\n\n" + note
}

func headlessFailedRetryPrompt(slug string, prompt string, detail string, attempt int, external bool) string {
	note := fmt.Sprintf("Previous attempt by @%s failed before a durable task handoff. Retry #%d.", strings.TrimSpace(slug), attempt)
	if trimmed := strings.TrimSpace(detail); trimmed != "" {
		note += " Last error: " + truncate(trimmed, 180) + "."
	}
	if external {
		note += " This is a live external-action task. Do the smallest useful live external step now. Do not keep discovering or drafting repo substitutes. If the first live Slack target fails, retry once against the resolved writable target; if that still fails, pivot immediately to the smallest useful live Notion or Drive action and report the exact blocker."
	} else {
		note += " For this retry, move immediately from claim/status into targeted file reads and edits, then leave the task in review/done/blocked before you stop. If you cannot ship the whole slice, ship the smallest runnable sub-slice and mark that state explicitly."
	}
	if strings.TrimSpace(prompt) == "" {
		return note
	}
	return strings.TrimSpace(prompt) + "\n\n" + note
}

func shouldRetryHeadlessTurn(task *teamTask, turn headlessCodexTurn) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return turn.Attempts < headlessCodexLocalWorktreeRetryLimit
	}
	if taskRequiresRealExternalExecution(task) {
		return turn.Attempts < headlessCodexExternalActionRetryLimit
	}
	return false
}

func (l *Launcher) recoverTimedOutHeadlessTurn(slug string, turn headlessCodexTurn, startedAt time.Time, timeout time.Duration) {
	if l == nil || l.broker == nil {
		return
	}
	task := l.timedOutTaskForTurn(slug, turn)
	if task == nil || strings.TrimSpace(task.ID) == "" {
		appendHeadlessCodexLog(slug, "timeout-recovery: no matching task found to block")
		return
	}
	if l.timedOutTurnAlreadyRecovered(task, slug, startedAt) {
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery: %s already produced durable progress; leaving task state unchanged", task.ID))
		return
	}
	if shouldRetryHeadlessTurn(task, turn) {
		retryTurn := turn
		retryTurn.Attempts++
		retryTurn.EnqueuedAt = time.Now()
		retryTurn.Prompt = headlessTimedOutRetryPrompt(slug, turn.Prompt, timeout, retryTurn.Attempts, taskRequiresRealExternalExecution(task))
		limit := headlessCodexLocalWorktreeRetryLimit
		if taskRequiresRealExternalExecution(task) {
			limit = headlessCodexExternalActionRetryLimit
		}
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery: requeueing %s after silent timeout (attempt %d/%d)", task.ID, retryTurn.Attempts, limit))
		l.enqueueHeadlessCodexTurnRecord(slug, retryTurn)
		return
	}
	reason := fmt.Sprintf("Automatic timeout recovery: @%s timed out after %s before posting a substantive update. Requeue, retry, or reassign from here.", slug, timeout)
	if _, changed, err := l.broker.BlockTask(task.ID, slug, reason); err != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery-error: could not block %s: %v", task.ID, err))
		return
	} else if changed {
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery: blocked %s after empty timeout", task.ID))
	}
}

// isDurabilityFailure reports whether detail came from headlessTurnCompletedDurably
// ("completed without durable task state"). These failures mean the agent ran but did
// nothing observable — retrying produces the same result, so we block instead.
func isDurabilityFailure(detail string) bool {
	return strings.Contains(strings.TrimSpace(detail), "completed without durable task state")
}

func (l *Launcher) recoverFailedHeadlessTurn(slug string, turn headlessCodexTurn, startedAt time.Time, detail string) {
	if l == nil || l.broker == nil {
		return
	}
	task := l.timedOutTaskForTurn(slug, turn)
	if task == nil || strings.TrimSpace(task.ID) == "" {
		appendHeadlessCodexLog(slug, "error-recovery: no matching task found to recover")
		return
	}
	if l.timedOutTurnAlreadyRecovered(task, slug, startedAt) {
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery: %s already produced durable progress; leaving task state unchanged", task.ID))
		return
	}
	if shouldRetryHeadlessTurn(task, turn) && !isDurabilityFailure(detail) {
		retryTurn := turn
		retryTurn.Attempts++
		retryTurn.EnqueuedAt = time.Now()
		retryTurn.Prompt = headlessFailedRetryPrompt(slug, turn.Prompt, detail, retryTurn.Attempts, taskRequiresRealExternalExecution(task))
		limit := headlessCodexLocalWorktreeRetryLimit
		if taskRequiresRealExternalExecution(task) {
			limit = headlessCodexExternalActionRetryLimit
		}
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery: requeueing %s after failed turn (attempt %d/%d)", task.ID, retryTurn.Attempts, limit))
		l.enqueueHeadlessCodexTurnRecord(slug, retryTurn)
		return
	}
	trimmed := strings.TrimSpace(detail)
	if trimmed == "" {
		trimmed = "unknown headless codex failure"
	}
	reason := fmt.Sprintf("Automatic error recovery: @%s failed before a durable task handoff. Last error: %s. Requeue, retry, or reassign from here.", slug, truncate(trimmed, 220))
	if _, changed, err := l.broker.BlockTask(task.ID, slug, reason); err != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery-error: could not block %s: %v", task.ID, err))
		return
	} else if changed {
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery: blocked %s after failed turn", task.ID))
	}
}

func (l *Launcher) timedOutTurnAlreadyRecovered(task *teamTask, slug string, startedAt time.Time) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		review := strings.ToLower(strings.TrimSpace(task.ReviewState))
		return status == "done" || status == "review" || status == "blocked" ||
			review == "ready_for_review" || review == "approved"
	}
	return l.agentPostedSubstantiveMessageSince(slug, startedAt)
}

func (l *Launcher) beginHeadlessCodexTurn(slug string) (headlessCodexTurn, context.Context, time.Time, time.Duration, bool) {
	l.headlessMu.Lock()
	defer l.headlessMu.Unlock()

	queue := l.headlessQueues[slug]
	if len(queue) == 0 {
		// Atomically mark the worker as done. This must happen while the lock is
		// held so that any concurrent enqueueHeadlessCodexTurn will observe
		// headlessWorkers[slug] = false and start a new goroutine rather than
		// assuming the current one will pick up the new item.
		delete(l.headlessWorkers, slug)
		delete(l.headlessQueues, slug)
		return headlessCodexTurn{}, nil, time.Time{}, 0, false
	}

	turn := queue[0]
	if len(queue) == 1 {
		delete(l.headlessQueues, slug)
	} else {
		l.headlessQueues[slug] = queue[1:]
	}

	baseCtx := l.headlessCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	timeout := l.headlessCodexTurnTimeoutForTurn(turn)
	turnCtx, cancel := context.WithTimeout(baseCtx, timeout)
	startedAt := time.Now()
	workspaceDir := ""
	if worktreeDir := l.headlessTaskWorkspaceDir(slug); worktreeDir != "" {
		workspaceDir = worktreeDir
	} else if codingAgentSlugs[slug] {
		workspaceDir = normalizeHeadlessWorkspaceDir(l.cwd)
	}
	l.headlessActive[slug] = &headlessCodexActiveTurn{
		Turn:              turn,
		StartedAt:         startedAt,
		Timeout:           timeout,
		Cancel:            cancel,
		WorkspaceDir:      workspaceDir,
		WorkspaceSnapshot: headlessCodexWorkspaceStatusSnapshot(workspaceDir),
	}
	return turn, turnCtx, startedAt, timeout, true
}

func (l *Launcher) headlessCodexTurnTimeoutForTurn(turn headlessCodexTurn) time.Duration {
	if task := l.timedOutTaskForTurn("", turn); task != nil {
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
			return headlessCodexLocalWorktreeTurnTimeout
		}
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "office") &&
			strings.EqualFold(strings.TrimSpace(task.TaskType), "launch") {
			return headlessCodexOfficeLaunchTurnTimeout
		}
	}
	return headlessCodexTurnTimeout
}

func (l *Launcher) headlessCodexStaleCancelAfterForTurn(turn headlessCodexTurn) time.Duration {
	if task := l.timedOutTaskForTurn("", turn); task != nil {
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
			return l.headlessCodexTurnTimeoutForTurn(turn)
		}
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "office") &&
			strings.EqualFold(strings.TrimSpace(task.TaskType), "launch") {
			return l.headlessCodexTurnTimeoutForTurn(turn)
		}
	}
	return headlessCodexStaleCancelAfter
}

func (l *Launcher) runHeadlessCodexTurn(ctx context.Context, slug string, notification string, channel ...string) error {
	if _, err := headlessCodexLookPath("codex"); err != nil {
		return fmt.Errorf("codex not found: %w", err)
	}
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}
	if err := l.preflightHeadlessCodexAuth(slug, firstNonEmpty(channel...)); err != nil {
		return err
	}

	workspaceDir := strings.TrimSpace(l.cwd)
	if worktreeDir := l.headlessTaskWorkspaceDir(slug); worktreeDir != "" {
		workspaceDir = worktreeDir
	}
	workspaceDir = normalizeHeadlessWorkspaceDir(workspaceDir)
	if workspaceDir == "" {
		workspaceDir = "."
	}

	overrides, err := l.buildCodexOfficeConfigOverrides(slug)
	if err != nil {
		return err
	}

	args := make([]string, 0, 16+len(overrides)*2)
	// Nested Codex local-worktree turns need full bypass here. The child Codex
	// sandbox rejects both apply_patch and shell writes even with
	// workspace-write, which leaves coding tasks permanently unable to land
	// edits. Keep office/non-editing turns on workspace-write.
	if l.unsafe || l.headlessCodexNeedsDangerousBypass(slug) {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "-a", "never", "-s", "workspace-write")
	}
	args = append(args, "--disable", "plugins")
	args = append(args,
		"exec",
		"-C", workspaceDir,
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"--json",
	)
	if model := strings.TrimSpace(config.ResolveCodexModel(l.cwd)); model != "" {
		args = append(args, "--model", model)
	}
	for _, override := range overrides {
		args = append(args, "-c", override)
	}
	args = append(args, "-")

	cmd := headlessCodexCommandContext(ctx, "codex", args...)
	cmd.Dir = workspaceDir
	cmd.Env = l.buildHeadlessCodexEnv(slug, workspaceDir, firstNonEmpty(channel...))
	if workspaceDir != strings.TrimSpace(l.cwd) {
		cmd.Env = append(cmd.Env, "WUPHF_WORKTREE_PATH="+workspaceDir)
	}
	stdinText := buildHeadlessCodexPrompt(l.buildPrompt(slug), notification)
	cmd.Stdin = strings.NewReader(stdinText)
	configureHeadlessProcess(cmd)
	dumpHeadlessCodexInvocation(slug, workspaceDir, args, cmd.Env, stdinText)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach codex stdout: %w", err)
	}

	// Tee raw stdout to the agent stream so the web UI can display live output.
	// The ReadCodexJSONStream parser doesn't emit streaming events for exec mode's
	// item.started/item.completed format, so we pipe raw lines directly.
	var agentStream *agentStreamBuffer
	if l.broker != nil {
		agentStream = l.broker.AgentStream(slug)
	}
	pr, pw := io.Pipe()
	teedStdout := io.TeeReader(stdout, pw)
	// Pipe every raw line from the provider (codex/claude) to the web UI's live stream.
	// No filtering — the user sees everything the agent sees.
	go func() {
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if agentStream != nil && line != "" {
				agentStream.Push(line)
			}
		}
	}()

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			terminateHeadlessProcess(cmd)
			_ = stdout.Close()
			_ = pw.CloseWithError(ctx.Err())
		case <-done:
		}
	}()

	startedAt := time.Now()
	metrics := headlessProgressMetrics{
		TotalMs:      -1,
		FirstEventMs: -1,
		FirstTextMs:  -1,
		FirstToolMs:  -1,
	}
	l.updateHeadlessProgress(slug, "active", "thinking", "reviewing work packet", metrics)
	var firstEventAt time.Time
	var firstTextAt time.Time
	var firstToolAt time.Time
	textStarted := false
	result, parseErr := provider.ReadCodexJSONStream(teedStdout, func(event provider.CodexStreamEvent) {
		if firstEventAt.IsZero() {
			firstEventAt = time.Now()
			metrics.FirstEventMs = durationMillis(startedAt, firstEventAt)
		}
		switch event.Type {
		case "text":
			if firstTextAt.IsZero() && strings.TrimSpace(event.Text) != "" {
				firstTextAt = time.Now()
				metrics.FirstTextMs = durationMillis(startedAt, firstTextAt)
			}
			if !textStarted && strings.TrimSpace(event.Text) != "" {
				textStarted = true
				l.updateHeadlessProgress(slug, "active", "text", "drafting response", metrics)
			}
		case "tool_use":
			if firstToolAt.IsZero() {
				firstToolAt = time.Now()
				metrics.FirstToolMs = durationMillis(startedAt, firstToolAt)
			}
			line := fmt.Sprintf("tool_use: %s %s", event.ToolName, truncate(event.ToolInput, 120))
			appendHeadlessCodexLog(slug, line)
			l.updateHeadlessProgress(slug, "active", "tool_use", fmt.Sprintf("running %s", strings.TrimSpace(event.ToolName)), metrics)
		case "tool_result":
			line := "tool_result: " + truncate(event.Text, 140)
			appendHeadlessCodexLog(slug, line)
			l.updateHeadlessProgress(slug, "active", "tool_result", truncate(event.Text, 140), metrics)
		case "error":
			appendHeadlessCodexLog(slug, "stream_error: "+event.Detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(event.Detail, 180), metrics)
		}
	})
	_ = pw.Close() // signal scanner goroutine that stream is done (io.PipeWriter.Close always returns nil)
	if err := cmd.Wait(); err != nil {
		detail := firstNonEmpty(result.LastError, strings.TrimSpace(stderr.String()))
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		if detail != "" {
			appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
				metrics.TotalMs,
				durationMillis(startedAt, firstEventAt),
				durationMillis(startedAt, firstTextAt),
				durationMillis(startedAt, firstToolAt),
				detail,
			))
			appendHeadlessCodexLog(slug, "stderr: "+detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(detail, 180), metrics)
			if isCodexAuthError(detail) && l.broker != nil {
				target := firstNonEmpty(channel...)
				if strings.TrimSpace(target) == "" {
					target = "general"
				}
				l.broker.PostSystemMessage(target,
					fmt.Sprintf("@%s hit an auth error talking to the model (%s). Run `codex login` on this machine, or set OPENAI_API_KEY, then retry.", slug, truncate(detail, 180)),
					"error",
				)
			}
			return fmt.Errorf("%w: %s", err, detail)
		}
		appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
			metrics.TotalMs,
			durationMillis(startedAt, firstEventAt),
			durationMillis(startedAt, firstTextAt),
			durationMillis(startedAt, firstToolAt),
			err.Error(),
		))
		return err
	}
	if parseErr != nil {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		l.updateHeadlessProgress(slug, "error", "error", truncate(parseErr.Error(), 180), metrics)
		return parseErr
	}
	metrics.TotalMs = time.Since(startedAt).Milliseconds()
	appendHeadlessCodexLatency(slug, fmt.Sprintf("status=ok total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d final_chars=%d",
		metrics.TotalMs,
		durationMillis(startedAt, firstEventAt),
		durationMillis(startedAt, firstTextAt),
		durationMillis(startedAt, firstToolAt),
		len(strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine))),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metrics))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metrics)
	if l.broker != nil && (result.Usage.InputTokens != 0 || result.Usage.OutputTokens != 0 || result.Usage.CacheReadTokens != 0 || result.Usage.CacheCreationTokens != 0 || result.Usage.CostUSD != 0) {
		l.broker.RecordAgentUsage(slug, config.ResolveCodexModel(l.cwd), result.Usage)
	}
	if text := strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine)); text != "" {
		appendHeadlessCodexLog(slug, "result: "+text)
		target := firstNonEmpty(channel...)
		msg, posted, err := l.postHeadlessFinalMessageIfSilent(slug, target, notification, text, startedAt)
		if err != nil {
			appendHeadlessCodexLog(slug, "fallback-post-error: "+err.Error())
		} else if posted {
			appendHeadlessCodexLog(slug, fmt.Sprintf("fallback-post: posted final output to #%s as %s", msg.Channel, msg.ID))
		}
	}
	return nil
}

func (l *Launcher) headlessCodexNeedsDangerousBypass(slug string) bool {
	if l == nil || l.broker == nil {
		return false
	}
	task := l.agentActiveTask(slug)
	if task == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree")
}

func (l *Launcher) buildHeadlessCodexEnv(slug string, workspaceDir string, channel string) []string {
	// gitCleanEnv: codex agents run `git` subcommands inside their sandbox.
	// If wuphf inherited GIT_DIR / GIT_WORK_TREE / GIT_CONFIG_PARAMETERS from
	// a parent (git hook, nested wuphf call) every child git would retarget
	// the outer repo. Clean those first, then drop codex-specific noise.
	// stripEnvKeys is exact-match, gitCleanEnv is prefix-match — the
	// GIT_CONFIG_KEY_<n> family needs prefix-match, so we run gitCleanEnv
	// first and stripEnvKeys second.
	env := stripEnvKeys(gitCleanEnv(), headlessCodexEnvVarsToStrip)
	if workspaceDir = normalizeHeadlessWorkspaceDir(workspaceDir); workspaceDir != "" {
		env = setEnvValue(env, "PWD", workspaceDir)
	}
	if codexHome := prepareHeadlessCodexHome(); codexHome != "" {
		// Use the isolated runtime home for the headless Codex process so it
		// doesn't inherit user-global ~/.agents skills from the interactive shell.
		env = setEnvValue(env, "HOME", codexHome)
		_ = os.MkdirAll(filepath.Join(codexHome, "plugins", "cache"), 0o755)
		env = setEnvValue(env, "CODEX_HOME", codexHome)
	} else if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		env = setEnvValue(env, "HOME", home)
	}
	if base := l.headlessCodexWorkspaceCacheDir(workspaceDir); base != "" {
		goCache := filepath.Join(base, "go-build", strings.TrimSpace(slug))
		goTmp := filepath.Join(base, "go-tmp", strings.TrimSpace(slug))
		_ = os.MkdirAll(goCache, 0o755)
		_ = os.MkdirAll(goTmp, 0o755)
		env = setEnvValue(env, "GOCACHE", goCache)
		env = setEnvValue(env, "GOTMPDIR", goTmp)
	}
	env = setEnvValue(env, "WUPHF_AGENT_SLUG", slug)
	if channel = strings.TrimSpace(channel); channel != "" {
		env = setEnvValue(env, "WUPHF_CHANNEL", channel)
	}
	env = setEnvValue(env, "WUPHF_BROKER_TOKEN", l.broker.Token())
	env = setEnvValue(env, "WUPHF_BROKER_BASE_URL", l.BrokerBaseURL())
	env = setEnvValue(env, "WUPHF_HEADLESS_PROVIDER", "codex")
	if config.ResolveNoNex() {
		env = setEnvValue(env, "WUPHF_NO_NEX", "1")
	}
	if l.isOneOnOne() {
		env = setEnvValue(env, "WUPHF_ONE_ON_ONE", "1")
		env = setEnvValue(env, "WUPHF_ONE_ON_ONE_AGENT", l.oneOnOneAgent())
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		env = setEnvValue(env, "ONE_SECRET", secret)
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env = setEnvValue(env, "ONE_IDENTITY", identity)
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env = setEnvValue(env, "ONE_IDENTITY_TYPE", identityType)
		}
	}
	if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
		env = setEnvValue(env, "WUPHF_API_KEY", apiKey)
		env = setEnvValue(env, "NEX_API_KEY", apiKey)
	}
	if openAIKey := strings.TrimSpace(config.ResolveOpenAIAPIKey()); openAIKey != "" {
		env = setEnvValue(env, "WUPHF_OPENAI_API_KEY", openAIKey)
		env = setEnvValue(env, "OPENAI_API_KEY", openAIKey)
	}
	return env
}

func headlessCodexHomeDir() string {
	if raw := strings.TrimSpace(os.Getenv("CODEX_HOME")); raw != "" {
		if abs, err := filepath.Abs(raw); err == nil && strings.TrimSpace(abs) != "" {
			return abs
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func headlessCodexGlobalHomeDir() string {
	if raw := strings.TrimSpace(os.Getenv("WUPHF_GLOBAL_HOME")); raw != "" {
		if abs, err := filepath.Abs(raw); err == nil && strings.TrimSpace(abs) != "" {
			return abs
		}
		return raw
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(home)
}

func headlessCodexRuntimeHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "codex-headless")
}

func prepareHeadlessCodexHome() string {
	runtimeHome := normalizeHeadlessWorkspaceDir(headlessCodexRuntimeHomeDir())
	if runtimeHome == "" {
		return headlessCodexHomeDir()
	}
	if err := os.MkdirAll(runtimeHome, 0o755); err != nil {
		return headlessCodexHomeDir()
	}
	sourceHome := normalizeHeadlessWorkspaceDir(filepath.Join(headlessCodexGlobalHomeDir(), ".codex"))
	if sourceHome == "" {
		sourceHome = normalizeHeadlessWorkspaceDir(headlessCodexHomeDir())
	}
	if sourceHome != "" && sourceHome != runtimeHome {
		if err := copyHeadlessCodexHomeFile(sourceHome, runtimeHome, "auth.json", 0o600); err != nil {
			// Auth is load-bearing — without it codex dies with 401 after a 5s
			// reconnect dance and nothing surfaces to the user. Log loudly.
			// runHeadlessCodexTurn does the user-visible preflight; this log is
			// the trail we want when debugging why that preflight fired.
			appendHeadlessCodexLog("_setup", fmt.Sprintf(
				"auth-copy-failed: source=%s dest=%s err=%v (run `codex login` or set OPENAI_API_KEY)",
				filepath.Join(sourceHome, "auth.json"),
				filepath.Join(runtimeHome, "auth.json"),
				err,
			))
		}
	}
	if userHome := strings.TrimSpace(headlessCodexGlobalHomeDir()); userHome != "" {
		// Best-effort — these are optional overlays, silent on miss is fine.
		_ = copyHeadlessCodexHomeFile(userHome, runtimeHome, filepath.Join(".one", "config.json"), 0o600)
		_ = copyHeadlessCodexHomeFile(userHome, runtimeHome, filepath.Join(".one", "update-check.json"), 0o600)
	}
	return runtimeHome
}

// copyHeadlessCodexHomeFile copies rel from sourceHome into runtimeHome. Returns
// an error when the source exists but the copy failed, or when the source is
// missing. A wholly-empty path or rel is a no-op (nil). Callers that care about
// visibility (auth.json) check the error; best-effort overlays ignore it.
func copyHeadlessCodexHomeFile(sourceHome string, runtimeHome string, rel string, mode os.FileMode) error {
	if strings.TrimSpace(sourceHome) == "" || strings.TrimSpace(runtimeHome) == "" || strings.TrimSpace(rel) == "" {
		return nil
	}
	sourcePath := filepath.Join(sourceHome, filepath.FromSlash(rel))
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sourcePath, err)
	}
	destPath := filepath.Join(runtimeHome, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
	}
	if err := os.WriteFile(destPath, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

func normalizeHeadlessWorkspaceDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil && strings.TrimSpace(abs) != "" {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil && strings.TrimSpace(real) != "" {
		path = real
	}
	return path
}

func (l *Launcher) headlessCodexWorkspaceCacheDir(workspaceDir string) string {
	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = strings.TrimSpace(l.cwd)
	}
	if base == "" {
		if wd, err := os.Getwd(); err == nil {
			base = wd
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, ".wuphf", "cache")
}

func (l *Launcher) headlessTaskWorkspaceDir(slug string) string {
	if l == nil || l.broker == nil {
		return ""
	}
	task := l.agentActiveTask(slug)
	if task == nil {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return ""
	}
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		return path
	}
	if strings.TrimSpace(task.ID) == "" {
		return ""
	}
	path, _, err := prepareTaskWorktree(task.ID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(path)
}

func (l *Launcher) buildCodexOfficeConfigOverrides(slug string) ([]string, error) {
	wuphfBinary, err := headlessCodexExecutablePath()
	if err != nil {
		return nil, err
	}
	wuphfEnvVars := []string{
		"WUPHF_AGENT_SLUG",
		"WUPHF_BROKER_TOKEN",
		"WUPHF_BROKER_BASE_URL",
	}
	if config.ResolveNoNex() {
		wuphfEnvVars = append(wuphfEnvVars, "WUPHF_NO_NEX")
	}
	if l.isOneOnOne() {
		wuphfEnvVars = append(wuphfEnvVars,
			"WUPHF_ONE_ON_ONE",
			"WUPHF_ONE_ON_ONE_AGENT",
		)
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		wuphfEnvVars = append(wuphfEnvVars, "ONE_SECRET")
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		wuphfEnvVars = append(wuphfEnvVars, "ONE_IDENTITY")
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			wuphfEnvVars = append(wuphfEnvVars, "ONE_IDENTITY_TYPE")
		}
	}

	overrides := []string{
		fmt.Sprintf(`mcp_servers.wuphf-office.command=%s`, tomlQuote(wuphfBinary)),
		`mcp_servers.wuphf-office.args=["mcp-team"]`,
		fmt.Sprintf(`mcp_servers.wuphf-office.env_vars=%s`, tomlStringArray(wuphfEnvVars)),
	}

	if !config.ResolveNoNex() {
		if nexMCP, err := headlessCodexLookPath("nex-mcp"); err == nil {
			overrides = append(overrides, fmt.Sprintf(`mcp_servers.nex.command=%s`, tomlQuote(nexMCP)))
			if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
				overrides = append(overrides, fmt.Sprintf(`mcp_servers.nex.env_vars=%s`, tomlStringArray([]string{
					"WUPHF_API_KEY",
					"NEX_API_KEY",
				})))
			}
		}
	}

	return overrides, nil
}

func buildHeadlessCodexPrompt(systemPrompt string, prompt string) string {
	var parts []string
	if trimmed := strings.TrimSpace(systemPrompt); trimmed != "" {
		parts = append(parts, "<system>\n"+trimmed+"\n</system>")
	}
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, "\n\n")
}

func wuphfLogDir() string {
	// WUPHF_LOG_DIR lets tests redirect headless log writes to a
	// process-stable path. Headless-worker goroutines routinely outlive the
	// test that started them; if they wrote to the test's t.TempDir() they
	// would race with go's test-scoped RemoveAll ("directory not empty" on
	// unlinkat). Tests set this to a package-owned leaked dir so writes
	// from leaked goroutines land harmlessly. Unset in production → HOME.
	if override := strings.TrimSpace(os.Getenv("WUPHF_LOG_DIR")); override != "" {
		// Fail loudly on a broken override instead of silently falling
		// through — a misconfigured WUPHF_LOG_DIR path otherwise surfaces
		// as confusing "file open failed" errors far from the root cause.
		// Returning "" disables headless logging for this call (the
		// appendHeadless*Log helpers no-op on empty dir), which matches
		// the HOME-lookup graceful-degradation path below.
		if err := os.MkdirAll(override, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "wuphf: WUPHF_LOG_DIR=%q unwritable: %v — headless logging disabled\n", override, err)
			return ""
		}
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".wuphf", "logs")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func appendHeadlessCodexLog(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-codex-"+slug+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(line))
}

func appendHeadlessCodexLatency(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-codex-latency.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(slug), strings.TrimSpace(line))
}

// preflightHeadlessCodexAuth verifies codex has a way to authenticate before we
// spawn the process. Without this check, codex dies with a 401 after retrying
// for ~10s and WUPHF's error log reads "exit status 1: exit status 1" — totally
// undebuggable from the UI. We check the two valid auth paths: a copied
// auth.json in the isolated CODEX_HOME (ChatGPT plan or API-key file), or an
// OPENAI_API_KEY in the env we're about to hand codex. If neither, fail fast
// with a message the user will actually see in the channel.
func (l *Launcher) preflightHeadlessCodexAuth(slug string, channel string) error {
	// Check the source codex creds that WUPHF will later copy into the isolated
	// CODEX_HOME. If the source is missing AND there's no OPENAI_API_KEY to fall
	// back on, fail fast so the user sees a clear message rather than a silent
	// 5-second 401 loop.
	sourceHome := strings.TrimSpace(headlessCodexGlobalHomeDir())
	authPath := filepath.Join(sourceHome, ".codex", "auth.json")
	if sourceHome != "" {
		if _, err := os.Stat(authPath); err == nil {
			return nil
		}
	}
	// Also accept a previously-copied auth.json in the isolated runtime home —
	// codex would still work even if the source was since removed.
	isolatedAuth := filepath.Join(headlessCodexHomeDir(), "auth.json")
	if _, err := os.Stat(isolatedAuth); err == nil {
		return nil
	}
	if strings.TrimSpace(config.ResolveOpenAIAPIKey()) != "" {
		return nil
	}
	reason := fmt.Sprintf(
		"Codex cannot authenticate — no `auth.json` at %s and no OPENAI_API_KEY in env. Run `codex login` on this machine, or set OPENAI_API_KEY, then retry.",
		authPath,
	)
	appendHeadlessCodexLog(slug, "preflight: "+reason)
	if l != nil && l.broker != nil {
		target := channel
		if strings.TrimSpace(target) == "" {
			target = "general"
		}
		l.broker.PostSystemMessage(target,
			fmt.Sprintf("@%s can't run: %s", slug, reason),
			"error",
		)
	}
	return fmt.Errorf("codex auth missing: %s", reason)
}

// dumpHeadlessCodexInvocation writes the exact codex argv + env + stdin to a
// shell script in $WUPHF_DEBUG_CODEX_DUMP when that env var is set. Off by
// default. Used to reproduce a failing turn outside WUPHF in a few seconds.
func dumpHeadlessCodexInvocation(slug, workspaceDir string, args []string, env []string, stdinText string) {
	dumpDir := strings.TrimSpace(os.Getenv("WUPHF_DEBUG_CODEX_DUMP"))
	if dumpDir == "" {
		return
	}
	if err := os.MkdirAll(dumpDir, 0o700); err != nil {
		return
	}
	ts := time.Now().Format("20060102-150405.000")
	stub := filepath.Join(dumpDir, fmt.Sprintf("codex-%s-%s", slug, ts))
	if err := os.WriteFile(stub+".stdin", []byte(stdinText), 0o600); err != nil {
		return
	}
	var sh strings.Builder
	sh.WriteString("#!/bin/bash\n")
	fmt.Fprintf(&sh, "# Reproduces the exact codex invocation WUPHF builds for agent=%s\n", slug)
	sh.WriteString("set -e\n")
	fmt.Fprintf(&sh, "cd %q\n", workspaceDir)
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			fmt.Fprintf(&sh, "export %s=%q\n", kv[:i], kv[i+1:])
		}
	}
	sh.WriteString("codex")
	for _, a := range args {
		fmt.Fprintf(&sh, " %q", a)
	}
	fmt.Fprintf(&sh, " < %q\n", stub+".stdin")
	if err := os.WriteFile(stub+".sh", []byte(sh.String()), 0o700); err != nil {
		return
	}
	appendHeadlessCodexLog(slug, "debug-dump: wrote "+stub+".sh")
}

// isCodexAuthError reports whether the failure detail looks like an auth issue
// (expired OAuth, bad key, missing bearer). Used to surface a clear message to
// the channel instead of the raw "exit status 1: exit status 1" noise.
func isCodexAuthError(detail string) bool {
	d := strings.ToLower(detail)
	if d == "" {
		return false
	}
	if strings.Contains(d, "401") {
		return true
	}
	if strings.Contains(d, "unauthorized") {
		return true
	}
	if strings.Contains(d, "missing bearer") {
		return true
	}
	return false
}

func durationMillis(start, mark time.Time) int64 {
	if start.IsZero() || mark.IsZero() {
		return -1
	}
	return mark.Sub(start).Milliseconds()
}

func tomlQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func tomlStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, tomlQuote(value))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func setEnvValue(env []string, key string, value string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return env
	}
	prefix := key + "="
	filtered := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, prefix+value)
}

func stripEnvKeys(env []string, strip []string) []string {
	if len(strip) == 0 {
		return env
	}
	stripSet := make(map[string]struct{}, len(strip))
	for _, key := range strip {
		key = strings.TrimSpace(key)
		if key != "" {
			stripSet[key] = struct{}{}
		}
	}
	if len(stripSet) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if _, ok := stripSet[key]; ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}
