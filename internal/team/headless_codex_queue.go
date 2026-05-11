package team

// headless_codex_queue.go owns per-slug headless dispatch queue
// management (PLAN.md §C10): enqueue/replace, lead-wake heuristics,
// worker lifecycle (spawn/stop/runQueue), per-turn lifecycle
// (begin/finish), and the timeout/stale-cancel helpers. Split out of
// headless_codex.go so the file boundaries follow the layered
// responsibilities (dispatch entry -> queue -> turn execution ->
// recovery).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

	l.headless.mu.Lock()
	if l.headless.queues == nil {
		l.headless.queues = make(map[string][]headlessCodexTurn)
	}
	if l.headless.active == nil {
		l.headless.active = make(map[string]*headlessCodexActiveTurn)
	}
	if l.headless.workers == nil {
		l.headless.workers = make(map[string]bool)
	}
	urgentLeadTurn := l.headlessLeadTurnNeedsImmediateWakeLocked(slug, turn.TaskID)
	humanPriority := turn.FromHuman
	if turn.TaskID != "" {
		if active := l.headless.active[slug]; active != nil && strings.TrimSpace(active.Turn.TaskID) == turn.TaskID {
			// Human turns bypass the same-task drop: a person sending a
			// follow-up message during an in-flight turn must always be
			// absorbed, never silently coalesced into the existing work.
			if !humanPriority && !(slug == l.targeter().LeadSlug() && urgentLeadTurn) && turn.Attempts <= active.Turn.Attempts {
				l.headless.mu.Unlock()
				if slug == l.targeter().LeadSlug() {
					appendHeadlessCodexLog(slug, "queue-drop: lead already handling same task")
				} else {
					appendHeadlessCodexLog(slug, "queue-drop: agent already handling same task")
				}
				return
			}
		}
		// Skip the same-task replace for human turns: each human message is
		// distinct content and must not stomp on a previously-queued one.
		if !humanPriority {
			if pending := l.replaceDuplicateTaskTurnLocked(slug, turn); pending {
				if !l.headless.workers[slug] {
					l.headless.workers[slug] = true
					startWorker = true
				}
				l.headless.mu.Unlock()
				if slug == l.targeter().LeadSlug() {
					appendHeadlessCodexLog(slug, "queue-replace: refreshed pending lead turn for same task")
				} else {
					appendHeadlessCodexLog(slug, "queue-replace: refreshed pending turn for same task")
				}
				if startWorker {
					l.spawnHeadlessWorker(slug)
				}
				return
			}
		}
	}
	// For the lead (CEO) agent, suppress the notification if any other specialist
	// is still active or has pending work. The lead should only step in when all
	// parallel work is done — not when one specialist finishes while others are
	// still running. This eliminates the race condition where CEO fires after the
	// first specialist completes and redundantly re-routes to still-running agents.
	//
	// Human turns bypass this hold: a person addressing the lead must be absorbed
	// immediately even if specialists are still working — the lead can decide
	// whether to stop, give a status update, or queue the request for later.
	if slug == l.targeter().LeadSlug() && !urgentLeadTurn && !humanPriority {
		for workerSlug, queue := range l.headless.queues {
			if workerSlug == slug {
				continue
			}
			if len(queue) > 0 {
				l.headless.deferredLead = &turn
				l.headless.mu.Unlock()
				appendHeadlessCodexLog(slug, "queue-hold: specialist still queued, deferring lead notification until all work lands")
				return
			}
		}
		for workerSlug, active := range l.headless.active {
			if workerSlug == slug {
				continue
			}
			if active != nil {
				l.headless.deferredLead = &turn
				l.headless.mu.Unlock()
				appendHeadlessCodexLog(slug, "queue-hold: specialist still running, deferring lead notification until all work lands")
				return
			}
		}
	}
	// For the lead (CEO) agent, cap the pending queue at 1 turn.
	// Multiple rapid-fire notifications (agent completions, status pings) can
	// stack up redundant CEO turns that each re-route the same task. One pending
	// turn is enough to catch the latest state; extras are dropped — except for
	// urgent task wakes and human-originated messages, which replace the pending
	// turn so the freshest signal wins instead of being silently dropped.
	const leadMaxPending = 1
	if slug == l.targeter().LeadSlug() && len(l.headless.queues[slug]) >= leadMaxPending {
		if urgentLeadTurn || humanPriority {
			l.headless.queues[slug][len(l.headless.queues[slug])-1] = turn
			if !l.headless.workers[slug] {
				l.headless.workers[slug] = true
				startWorker = true
			}
			l.headless.mu.Unlock()
			if humanPriority {
				appendHeadlessCodexLog(slug, "queue-replace: lead queue at cap, replacing pending turn with human-priority message")
			} else {
				appendHeadlessCodexLog(slug, "queue-replace: lead queue at cap, replacing pending turn with urgent task notification")
			}
			if startWorker {
				l.spawnHeadlessWorker(slug)
			}
			return
		}
		l.headless.mu.Unlock()
		appendHeadlessCodexLog(slug, "queue-drop: lead queue at cap, dropping redundant notification")
		return
	}
	l.headless.queues[slug] = append(l.headless.queues[slug], turn)
	if !l.headless.workers[slug] {
		l.headless.workers[slug] = true
		startWorker = true
	}
	if active := l.headless.active[slug]; active != nil && active.Cancel != nil {
		age := time.Since(active.StartedAt)
		// Human turns preempt unconditionally. The staleness/min-age floors
		// exist to break tight agent-to-agent cancel loops, but a real person
		// chatting must never wait behind an in-flight turn. For non-human
		// turns both floors must hold: past the configured staleness threshold
		// AND past the minimum-turn-age floor.
		switch {
		case humanPriority:
			cancel = active.Cancel
			staleAge = age
		case age >= headlessCodexMinTurnAgeBeforeCancel &&
			age >= l.headlessCodexStaleCancelAfterForTurn(active.Turn):
			cancel = active.Cancel
			staleAge = age
		}
	}
	l.headless.mu.Unlock()

	if cancel != nil {
		if humanPriority {
			appendHeadlessCodexLog(slug, fmt.Sprintf("human-priority: cancelling active turn after %s so the agent absorbs the human message", staleAge.Round(time.Millisecond)))
			l.updateHeadlessProgress(slug, "active", "queued", "preempting in-flight turn for human message", headlessProgressMetrics{})
		} else {
			appendHeadlessCodexLog(slug, fmt.Sprintf("stale-turn: cancelling active turn after %s to process queued work", staleAge.Round(time.Second)))
			l.updateHeadlessProgress(slug, "active", "queued", "preempting stale work for newer request", headlessProgressMetrics{})
		}
		cancel()
	}
	if startWorker {
		l.spawnHeadlessWorker(slug)
	}
}

func (l *Launcher) replaceDuplicateTaskTurnLocked(slug string, turn headlessCodexTurn) bool {
	for i := range l.headless.queues[slug] {
		if strings.TrimSpace(l.headless.queues[slug][i].TaskID) != turn.TaskID {
			continue
		}
		l.headless.queues[slug][i] = turn
		return true
	}
	if slug == l.targeter().LeadSlug() && l.headless.deferredLead != nil && strings.TrimSpace(l.headless.deferredLead.TaskID) == turn.TaskID {
		cp := turn
		l.headless.deferredLead = &cp
		return true
	}
	return false
}

// headlessLeadTurnNeedsImmediateWakeLocked decides whether a lead-
// agent enqueue should bypass the "wait for specialists to finish"
// queue-hold. taskID is the already-normalized turn.TaskID — we
// must NOT re-parse the prompt here. The original implementation
// did `headlessCodexTaskID(prompt)`, which broke any enqueue path
// that set TaskID without embedding `#task-...` in the prompt
// (e.g. recovery dispatchers that build the prompt fresh).
func (l *Launcher) headlessLeadTurnNeedsImmediateWakeLocked(slug, taskID string) bool {
	if l == nil || l.broker == nil {
		return false
	}
	if strings.TrimSpace(slug) != l.targeter().LeadSlug() {
		return false
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	for _, task := range l.broker.AllTasks() {
		if task.ID != taskID {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(task.status))
		review := strings.ToLower(strings.TrimSpace(task.reviewState))
		return status == "review" || review == "ready_for_review" || status == "blocked"
	}
	return false
}

// spawnHeadlessWorker starts a runHeadlessCodexQueue goroutine and registers
// it with l.headless.workerWg so stopHeadlessWorkers can drain it. Lazily
// initialises l.headless.stopCh; safe for any Launcher (including bare
// `&Launcher{}` literals that tests construct). All `go runHeadlessCodexQueue`
// sites must funnel through here so no worker escapes the WaitGroup.
func (l *Launcher) spawnHeadlessWorker(slug string) {
	l.headless.mu.Lock()
	if l.headless.stopCh == nil {
		l.headless.stopCh = make(chan struct{})
	}
	stop := l.headless.stopCh
	l.headless.workerWg.Add(1)
	l.headless.mu.Unlock()
	go l.runHeadlessCodexQueue(slug, stop)
}

// stopHeadlessWorkers signals every live runHeadlessCodexQueue goroutine to
// exit at its next outer-loop tick and waits for them to drain. Idempotent —
// safe to call multiple times. Used by tests via t.Cleanup so a queue worker
// spawned by the current test can't outlive the test and race the next test's
// setup of headlessActive / headlessQueues / the test t.TempDir cleanup.
func (l *Launcher) stopHeadlessWorkers() {
	l.headless.mu.Lock()
	if l.headless.stopCh == nil {
		l.headless.stopCh = make(chan struct{})
	}
	select {
	case <-l.headless.stopCh:
		// already closed; idempotent re-entry
	default:
		close(l.headless.stopCh)
	}
	cancel := l.headless.cancel
	l.headless.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Cancel any in-flight turns and keep cancelling until workers drain.
	// Polling closes a TOCTOU window: a worker that was past its top-of-loop
	// stop check but had not yet called beginHeadlessCodexTurn at first
	// snapshot time would otherwise register a fresh active turn after we
	// scanned, and Wait() would block on a stub that's parked on ctx.Done().
	// Production launchers cancel via headlessCancel above; this loop is the
	// safety net for bare &Launcher{} test fixtures that don't seed one.
	done := make(chan struct{})
	go func() {
		l.headless.workerWg.Wait()
		close(done)
	}()
	cancelActive := func() {
		l.headless.mu.Lock()
		for _, active := range l.headless.active {
			if active != nil && active.Cancel != nil {
				active.Cancel()
			}
		}
		l.headless.mu.Unlock()
	}
	cancelActive()
	if cancel != nil {
		// Production path: parent ctx already cancelled; just wait.
		<-done
		return
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			cancelActive()
		}
	}
}

func (l *Launcher) runHeadlessCodexQueue(slug string, stop <-chan struct{}) {
	defer l.headless.workerWg.Done()
	for {
		// Stop signal short-circuits the loop before grabbing more work.
		// The check is cheap (non-blocking select) and only fires on test
		// cleanup or graceful shutdown — production traffic never closes
		// stop while the worker is in steady state.
		select {
		case <-stop:
			l.headless.mu.Lock()
			delete(l.headless.workers, slug)
			l.headless.mu.Unlock()
			return
		default:
		}
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
				l.headless.mu.Lock()
				active := l.headless.active[slug]
				l.headless.mu.Unlock()
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
		l.headless.mu.Lock()
		_, stillRunning := l.headless.workers[slug]
		l.headless.mu.Unlock()
		if !stillRunning {
			return
		}
	}
}

func (l *Launcher) finishHeadlessTurn(slug string) {
	l.headless.mu.Lock()
	if active := l.headless.active[slug]; active != nil && active.Cancel != nil {
		active.Cancel()
	}
	delete(l.headless.active, slug)
	lead := l.targeter().LeadSlug()
	var deferredLead *headlessCodexTurn
	// Determine if this was a specialist finishing (not the lead), and if so whether
	// any other specialists are still active or queued. If the slate is clear, we
	// need to wake the lead so it can react to the specialist's completion messages.
	// Without this, the CEO misses completion broadcasts because the queue-hold
	// fires while the specialist is still "active" (process running), and after the
	// process exits there is nothing else to re-trigger the CEO.
	shouldWakeLead := slug != lead && lead != ""
	if shouldWakeLead {
		for workerSlug, queue := range l.headless.queues {
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
		for workerSlug, active := range l.headless.active {
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
	if shouldWakeLead && len(l.headless.queues[lead]) > 0 {
		shouldWakeLead = false
	}
	if shouldWakeLead && l.headless.deferredLead != nil {
		turn := *l.headless.deferredLead
		l.headless.deferredLead = nil
		deferredLead = &turn
		shouldWakeLead = false
	}
	l.headless.mu.Unlock()

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
	lead := l.targeter().LeadSlug()
	if lead == "" {
		return
	}
	targets := l.targeter().NotificationTargets()
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
		// Reuse the substantive-message predicate so agent_issue
		// helpdesk pings (and other non-progress kinds) don't get
		// treated as a completion handoff and wake the lead unnecessarily.
		if !isSubstantiveAgentProgressMessage(m) {
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
		switch strings.ToLower(strings.TrimSpace(task.status)) {
		case "done", "completed", "review", "blocked":
			return action, task, true
		}
	}
	return officeActionLog{}, teamTask{}, false
}

func (l *Launcher) beginHeadlessCodexTurn(slug string) (headlessCodexTurn, context.Context, time.Time, time.Duration, bool) {
	l.headless.mu.Lock()
	defer l.headless.mu.Unlock()

	// If stopHeadlessWorkers already fired, don't start a new turn. This closes
	// the race where a worker goroutine passed the outer stop-channel check
	// just before stopHeadlessWorkers closed headlessStopCh, causing the worker
	// to block in headlessCodexRunTurn with no cancel registered in headlessActive.
	if l.headless.stopCh != nil {
		select {
		case <-l.headless.stopCh:
			delete(l.headless.workers, slug)
			return headlessCodexTurn{}, nil, time.Time{}, 0, false
		default:
		}
	}

	queue := l.headless.queues[slug]
	if len(queue) == 0 {
		// Atomically mark the worker as done. This must happen while the lock is
		// held so that any concurrent enqueueHeadlessCodexTurn will observe
		// headlessWorkers[slug] = false and start a new goroutine rather than
		// assuming the current one will pick up the new item.
		delete(l.headless.workers, slug)
		delete(l.headless.queues, slug)
		return headlessCodexTurn{}, nil, time.Time{}, 0, false
	}

	turn := queue[0]
	if len(queue) == 1 {
		delete(l.headless.queues, slug)
	} else {
		l.headless.queues[slug] = queue[1:]
	}

	baseCtx := l.headless.ctx
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
	l.headless.active[slug] = &headlessCodexActiveTurn{
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
