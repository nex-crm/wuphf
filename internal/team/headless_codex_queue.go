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

// taskRunsInIsolatedWorktree reports whether a task executes in its own git
// worktree (a distinct directory), making it safe to run concurrently with the
// agent's other lanes. Office-mode and external tasks share the office cwd, so
// they are NOT isolated and must stay on the serialized default lane.
func taskRunsInIsolatedWorktree(task *teamTask) bool {
	if task == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	return strings.TrimSpace(task.WorktreePath) != ""
}

// laneForTurn resolves the dispatch lane a turn runs in. The guiding rule:
// non-dependent tasks run concurrently, so every task turn gets its OWN lane
// unless two turns would collide on a shared resource. Callers hold
// l.headless.mu; the broker lookup respects the established headless.mu →
// broker.mu order (see headlessLeadTurnNeedsImmediateWakeLocked).
//
//   - chat / channel-triage turns (no task) → the agent's default lane (""):
//     conversational coherence, and the lead's triage never forks. This is the
//     ONLY lane the lead (CEO) uses for non-task turns.
//   - worktree tasks → keyed by worktree PATH: distinct worktrees run in
//     parallel; a shared worktree (a dependent reusing its parent's tree)
//     collapses to one serialized lane. A worktree task with no path assigned
//     yet serializes on the default lane until it's prepared.
//   - office / live_external / other → keyed by TASK id: no shared worktree to
//     collide on (concurrent office turns share cwd exactly as different agents'
//     turns already do; the broker mediates shared state), so each task runs in
//     its own lane and non-dependent tasks of one agent run at once. This now
//     applies to the LEAD too: a CEO turn carrying a task id gets its own
//     per-task lane so the CEO can work several tasks concurrently. A lead turn
//     with no task id still serializes on the default lane (channel triage).
func (l *Launcher) laneForTurn(slug string, turn headlessCodexTurn) headlessLane {
	slug = strings.TrimSpace(slug)
	taskID := strings.TrimSpace(turn.TaskID)
	if taskID == "" || l == nil || l.broker == nil {
		return slugLane(slug)
	}
	task := l.broker.TaskByID(taskID)
	if task == nil {
		return slugLane(slug)
	}
	if taskRunsInIsolatedWorktree(task) {
		return worktreeLane(slug, task.WorktreePath)
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		// Worktree mode but no path assigned yet — serialize on the default lane
		// until syncTaskWorktreeLocked prepares the tree, so it can't race a
		// sibling turn in cwd before its workspace exists.
		return slugLane(slug)
	}
	return taskLane(slug, taskID)
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
	// B4 pre-task bookend: the FIRST headless-turn enqueue for an
	// (agent, task) pair queues the agent's pre-task research note
	// (task_notebook_bookends.go). Only the in-memory dedupe runs on this
	// path; the broker reads + notebook write happen in a queued goroutine
	// and ride the wiki worker queue.
	if turn.TaskID != "" {
		l.queueTaskNotebookPreBookend(slug, turn.TaskID)
	}

	var cancel context.CancelFunc
	var staleAge time.Duration
	startWorker := false

	l.headless.mu.Lock()
	if l.headless.queues == nil {
		l.headless.queues = make(map[headlessLane][]headlessCodexTurn)
	}
	if l.headless.active == nil {
		l.headless.active = make(map[headlessLane]*headlessCodexActiveTurn)
	}
	if l.headless.workers == nil {
		l.headless.workers = make(map[headlessLane]bool)
	}
	// Resolve the lane this turn runs in. Lead turns and non-isolated-worktree
	// turns share the agent's default lane (serialized as before); isolated
	// worktree turns get their own lane keyed by worktree path so they can run
	// concurrently with the agent's other lanes.
	lane := l.laneForTurn(slug, turn)
	isLead := slug == l.targeter().LeadSlug()
	urgentLeadTurn := l.headlessLeadTurnNeedsImmediateWakeLocked(slug, turn.TaskID)
	humanPriority := turn.FromHuman
	if turn.TaskID != "" {
		if active := l.headless.active[lane]; active != nil && strings.TrimSpace(active.Turn.TaskID) == turn.TaskID {
			// Human turns bypass the same-task drop: a person sending a
			// follow-up message during an in-flight turn must always be
			// absorbed, never silently coalesced into the existing work.
			if !humanPriority && !(isLead && urgentLeadTurn) && turn.Attempts <= active.Turn.Attempts {
				l.headless.mu.Unlock()
				if isLead {
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
			if pending := l.replaceDuplicateTaskTurnLocked(lane, turn); pending {
				if !l.headless.workers[lane] {
					l.headless.workers[lane] = true
					startWorker = true
				}
				l.headless.mu.Unlock()
				if isLead {
					appendHeadlessCodexLog(slug, "queue-replace: refreshed pending lead turn for same task")
				} else {
					appendHeadlessCodexLog(slug, "queue-replace: refreshed pending turn for same task")
				}
				if startWorker {
					l.spawnHeadlessWorker(lane)
				}
				return
			}
		}
	}
	// For the lead (CEO) agent, suppress a NO-TASK triage notification if any
	// other specialist is still active or has pending work. The lead should only
	// step in on general channel chatter when all parallel work is done — not
	// when one specialist finishes while others are still running. This is where
	// the re-route race lives (the CEO reacting to chatter mid-flight and
	// redundantly re-routing to still-running agents). Scans every non-lead lane
	// (an agent may now hold several).
	//
	// A TASK-carrying lead turn is NOT held here: the CEO runs it on its own
	// per-task lane so non-dependent tasks proceed concurrently (CEO
	// multitasking). The same-task drop/replace above already prevents
	// double-dispatching the same task, and the concurrency cap bounds the total.
	//
	// Human turns bypass this hold: a person addressing the lead must be absorbed
	// immediately even if specialists are still working — the lead can decide
	// whether to stop, give a status update, or queue the request for later.
	if isLead && !urgentLeadTurn && !humanPriority && strings.TrimSpace(turn.TaskID) == "" {
		for workerLane, queue := range l.headless.queues {
			if workerLane.slug == slug {
				continue
			}
			if len(queue) > 0 {
				l.headless.deferredLead = &turn
				l.headless.mu.Unlock()
				appendHeadlessCodexLog(slug, "queue-hold: specialist still queued, deferring lead notification until all work lands")
				return
			}
		}
		for workerLane, active := range l.headless.active {
			if workerLane.slug == slug {
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
	// For the lead (CEO) agent, cap the pending queue at 1 turn PER LANE.
	// Multiple rapid-fire notifications (agent completions, status pings) can
	// stack up redundant CEO turns that each re-route the same task. One pending
	// turn is enough to catch the latest state; extras are dropped — except for
	// urgent task wakes and human-originated messages, which replace the pending
	// turn so the freshest signal wins instead of being silently dropped.
	// The lead now runs several lanes (one per task + the default triage lane),
	// and the cap is checked against THIS turn's lane (l.headless.queues[lane]),
	// so each task lane keeps its own single-pending budget independently.
	const leadMaxPending = 1
	if isLead && len(l.headless.queues[lane]) >= leadMaxPending {
		if urgentLeadTurn || humanPriority {
			l.headless.queues[lane][len(l.headless.queues[lane])-1] = turn
			if !l.headless.workers[lane] {
				l.headless.workers[lane] = true
				startWorker = true
			}
			l.headless.mu.Unlock()
			if humanPriority {
				appendHeadlessCodexLog(slug, "queue-replace: lead queue at cap, replacing pending turn with human-priority message")
			} else {
				appendHeadlessCodexLog(slug, "queue-replace: lead queue at cap, replacing pending turn with urgent task notification")
			}
			if startWorker {
				l.spawnHeadlessWorker(lane)
			}
			return
		}
		l.headless.mu.Unlock()
		appendHeadlessCodexLog(slug, "queue-drop: lead queue at cap, dropping redundant notification")
		return
	}
	l.headless.queues[lane] = append(l.headless.queues[lane], turn)
	if !l.headless.workers[lane] {
		l.headless.workers[lane] = true
		startWorker = true
	}
	if active := l.headless.active[lane]; active != nil && active.Cancel != nil {
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
			age >= l.headlessCodexStaleCancelAfterForTurn(slug, active.Turn):
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
		l.spawnHeadlessWorker(lane)
	}
}

func (l *Launcher) replaceDuplicateTaskTurnLocked(lane headlessLane, turn headlessCodexTurn) bool {
	for i := range l.headless.queues[lane] {
		if strings.TrimSpace(l.headless.queues[lane][i].TaskID) != turn.TaskID {
			continue
		}
		l.headless.queues[lane][i] = turn
		return true
	}
	if lane.slug == l.targeter().LeadSlug() && l.headless.deferredLead != nil && strings.TrimSpace(l.headless.deferredLead.TaskID) == turn.TaskID {
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
	// TaskByID does the same single-task lookup without AllTasks()'s full
	// slice copy — this runs on every enqueue (every dispatch/wake/message),
	// and the channel-per-task model makes the task set grow with the office.
	task := l.broker.TaskByID(taskID)
	if task == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.status))
	review := strings.ToLower(strings.TrimSpace(task.reviewState))
	return status == "review" || review == "ready_for_review" || status == "blocked"
}

// spawnHeadlessWorker starts a runHeadlessCodexQueue goroutine and registers
// it with l.headless.workerWg so stopHeadlessWorkers can drain it. Lazily
// initialises l.headless.stopCh; safe for any Launcher (including bare
// `&Launcher{}` literals that tests construct). All `go runHeadlessCodexQueue`
// sites must funnel through here so no worker escapes the WaitGroup.
func (l *Launcher) spawnHeadlessWorker(lane headlessLane) {
	l.headless.mu.Lock()
	if l.headless.stopCh == nil {
		l.headless.stopCh = make(chan struct{})
	}
	stop := l.headless.stopCh
	l.headless.workerWg.Add(1)
	l.headless.mu.Unlock()
	go l.runHeadlessCodexQueue(lane, stop)
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

func (l *Launcher) runHeadlessCodexQueue(lane headlessLane, stop <-chan struct{}) {
	defer l.headless.workerWg.Done()
	slug := lane.slug
	for {
		// Stop signal short-circuits the loop before grabbing more work.
		// The check is cheap (non-blocking select) and only fires on test
		// cleanup or graceful shutdown — production traffic never closes
		// stop while the worker is in steady state.
		select {
		case <-stop:
			l.headless.mu.Lock()
			delete(l.headless.workers, lane)
			l.headless.mu.Unlock()
			return
		default:
		}
		func() {
			defer recoverPanicTo("runHeadlessCodexQueue", fmt.Sprintf("slug=%s", slug))
			turn, turnCtx, startedAt, timeout, ok := l.beginHeadlessCodexTurn(lane)
			if !ok {
				l.updateHeadlessProgress(slug, "idle", "idle", "waiting for work", headlessProgressMetrics{})
				return
			}
			// Guarantee the active slot is released even if the turn body
			// panics. finishHeadlessTurn clears active[lane] (freeing the
			// concurrency-cap slot), respawns parked lanes, and wakes the
			// lead/CEO after a specialist completes. If a panic in
			// headlessCodexRunTurn, the recovery helpers, or
			// recordTaskLedgerEntry skipped it, the slot leaked forever:
			// under a cap every other agent's lane stayed parked and the CEO
			// never reacted to completions — agents silently stalled with no
			// reply. recoverPanicTo (deferred above, so it runs last) still
			// swallows and logs the panic after this cleanup runs.
			defer l.finishHeadlessTurn(lane)
			appendHeadlessCodexLatency(slug, fmt.Sprintf("stage=started queue_wait_ms=%d", time.Since(turn.EnqueuedAt).Milliseconds()))
			l.updateHeadlessProgress(slug, "active", "queued", "queued work packet received", headlessProgressMetrics{})

			err := headlessCodexRunTurn(l, turnCtx, slug, turn.Prompt, turn.Channel)
			ctxErr := turnCtx.Err()
			isDurabilityError := false
			if err == nil {
				l.headless.mu.Lock()
				active := l.headless.active[lane]
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
				detail := err.Error()
				if isTurnKilledError(err) {
					// Killed turn (SIGKILL/SIGTERM): post one honest
					// system note and humanize the detail that flows
					// into progress + recovery, so the user never has
					// to parse raw `signal: killed` exhaust (Wave F2).
					detail = turnKilledHumanDetail(slug)
					l.postTurnKilledNote(slug, turn.Channel)
				}
				l.updateHeadlessProgress(slug, "error", "error", truncate(detail, 180), headlessProgressMetrics{})
				l.recoverFailedHeadlessTurn(slug, turn, startedAt, detail)
			}
			l.recordTaskLedgerEntry(slug, turn, startedAt, err)
		}()
		l.headless.mu.Lock()
		_, stillRunning := l.headless.workers[lane]
		l.headless.mu.Unlock()
		if !stillRunning {
			return
		}
	}
}

func (l *Launcher) finishHeadlessTurn(lane headlessLane) {
	slug := lane.slug
	l.headless.mu.Lock()
	if active := l.headless.active[lane]; active != nil && active.Cancel != nil {
		active.Cancel()
	}
	delete(l.headless.active, lane)
	lead := l.targeter().LeadSlug()
	var deferredLead *headlessCodexTurn
	// Determine if this was a specialist finishing (not the lead), and if so whether
	// any other specialists are still active or queued. If the slate is clear, we
	// need to wake the lead so it can react to the specialist's completion messages.
	// Without this, the CEO misses completion broadcasts because the queue-hold
	// fires while the specialist is still "active" (process running), and after the
	// process exits there is nothing else to re-trigger the CEO. Scans by lane
	// grouped on lane.slug — an agent may finish one lane while others run on.
	shouldWakeLead := slug != lead && lead != ""
	if shouldWakeLead {
		for workerLane, queue := range l.headless.queues {
			if workerLane.slug == lead {
				continue
			}
			if len(queue) > 0 {
				shouldWakeLead = false
				break
			}
		}
	}
	if shouldWakeLead {
		for workerLane, active := range l.headless.active {
			if workerLane.slug == lead {
				continue
			}
			if active != nil {
				shouldWakeLead = false
				break
			}
		}
	}
	// Check if the lead already has work queued — no need to wake it. The lead
	// now runs several lanes (its default triage lane + one per task), so scan
	// every lead lane; any queued lead work means it'll process state on its own.
	if shouldWakeLead {
		for workerLane, queue := range l.headless.queues {
			if workerLane.slug == lead && len(queue) > 0 {
				shouldWakeLead = false
				break
			}
		}
	}
	if shouldWakeLead && l.headless.deferredLead != nil {
		turn := *l.headless.deferredLead
		l.headless.deferredLead = nil
		deferredLead = &turn
		shouldWakeLead = false
	}
	// A turn just finished, freeing an active slot. Re-spawn the lanes the
	// concurrency cap parked (queued work but no running worker). Only the
	// subset that fits the newly-available global/per-agent slots is woken:
	// we simulate admission under the lock — seed counts from the lanes still
	// active, then admit parked lanes one at a time, incrementing the running
	// tallies as we go — so a single completion never spawns the whole herd
	// only to have almost all of them immediately re-park and exit (O(n)
	// goroutine/log churn per finished turn under a tight cap). Collect under
	// the lock and mark workers=true so a concurrent enqueue won't also spawn;
	// the actual spawn happens after unlocking. Only meaningful when a cap is
	// active — with no cap every queued lane already has a worker.
	var parkedLanes []headlessLane
	if global, perAgent := l.headlessConcurrencyCaps(); global > 0 || perAgent > 0 {
		total := 0
		byAgent := map[string]int{}
		for activeLane, active := range l.headless.active {
			if active == nil {
				continue
			}
			total++
			byAgent[activeLane.slug]++
		}
		for parkedLane, queue := range l.headless.queues {
			if len(queue) == 0 || l.headless.workers[parkedLane] {
				continue
			}
			if global > 0 && total >= global {
				break
			}
			if perAgent > 0 && byAgent[parkedLane.slug] >= perAgent {
				continue
			}
			l.headless.workers[parkedLane] = true
			parkedLanes = append(parkedLanes, parkedLane)
			total++
			byAgent[parkedLane.slug]++
		}
	}
	l.headless.mu.Unlock()

	for _, parked := range parkedLanes {
		l.spawnHeadlessWorker(parked)
	}

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

func (l *Launcher) beginHeadlessCodexTurn(lane headlessLane) (headlessCodexTurn, context.Context, time.Time, time.Duration, bool) {
	slug := lane.slug
	l.headless.mu.Lock()
	defer l.headless.mu.Unlock()

	// If stopHeadlessWorkers already fired, don't start a new turn. This closes
	// the race where a worker goroutine passed the outer stop-channel check
	// just before stopHeadlessWorkers closed headlessStopCh, causing the worker
	// to block in headlessCodexRunTurn with no cancel registered in headlessActive.
	if l.headless.stopCh != nil {
		select {
		case <-l.headless.stopCh:
			delete(l.headless.workers, lane)
			return headlessCodexTurn{}, nil, time.Time{}, 0, false
		default:
		}
	}

	queue := l.headless.queues[lane]
	if len(queue) == 0 {
		// Atomically mark the worker as done. This must happen while the lock is
		// held so that any concurrent enqueueHeadlessCodexTurn will observe
		// workers[lane] = false and start a new goroutine rather than
		// assuming the current one will pick up the new item.
		delete(l.headless.workers, lane)
		delete(l.headless.queues, lane)
		return headlessCodexTurn{}, nil, time.Time{}, 0, false
	}

	turn := queue[0]
	// Concurrency cap (cost guard for CEO multitasking): if starting this lane
	// would exceed the global or per-agent in-flight cap, PARK the worker without
	// consuming the queued turn — the lane keeps its queued work and
	// finishHeadlessTurn re-spawns parked lanes as active slots free. A
	// human-priority turn bypasses the cap so a real person is never starved
	// behind agent work.
	if !turn.FromHuman && !l.headlessLaneMayStartLocked(lane) {
		delete(l.headless.workers, lane)
		appendHeadlessCodexLog(slug, "queue-park: at concurrency cap, deferring lane until a slot frees")
		return headlessCodexTurn{}, nil, time.Time{}, 0, false
	}
	if len(queue) == 1 {
		delete(l.headless.queues, lane)
	} else {
		l.headless.queues[lane] = queue[1:]
	}

	baseCtx := l.headless.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	timeout := l.headlessCodexTurnTimeoutForTurn(slug, turn)
	turnCtx, cancel := context.WithTimeout(baseCtx, timeout)
	// Tag the turn context with its task id so the runner helpers
	// (model / effort / provider / workspace) resolve THIS turn's task even when
	// the agent has several tasks in flight at once. See headless_runtime.go.
	turnCtx = withHeadlessTurnTaskID(turnCtx, turn.TaskID)
	startedAt := time.Now()
	// Launch-param record (V3-N5): the active turn carries the working
	// directory the runner will execute in — the task worktree when this
	// turn's task has one, else the agent's scratch dir inside the office
	// runtime home. Never the broker process launch cwd. The recovery
	// durability guard keys off the snapshot delta; a non-git scratch dir
	// snapshots to "" so it never trips that guard.
	workspaceDir, _ := l.headlessTurnWorkspace(slug, turn.TaskID)
	l.headless.active[lane] = &headlessCodexActiveTurn{
		Turn:              turn,
		StartedAt:         startedAt,
		Timeout:           timeout,
		Cancel:            cancel,
		WorkspaceDir:      workspaceDir,
		WorkspaceSnapshot: headlessCodexWorkspaceStatusSnapshot(workspaceDir),
	}
	return turn, turnCtx, startedAt, timeout, true
}

// headlessCodexTurnTimeoutForTurn resolves the hard wall-clock budget for a
// turn. slug is the lane owner; it is threaded so the task resolves via the
// same path the recovery layer uses (turn.TaskID first, then the owner's
// active in-progress task) — message-driven turns carry a channel-derived
// TaskID that does not match the real task ID, so without the slug fallback
// an office orchestration turn would silently drop to the tight default.
func (l *Launcher) headlessCodexTurnTimeoutForTurn(slug string, turn headlessCodexTurn) time.Duration {
	if task := l.timedOutTaskForTurn(slug, turn); task != nil {
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
			return headlessCodexLocalWorktreeTurnTimeout
		}
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "office") {
			// Launch turns keep their dedicated budget; every other office
			// turn (CEO orchestration, specialist office work) gets the
			// office budget instead of the tight 4m default that used to
			// force-kill multi-step orchestration mid-flight.
			if strings.EqualFold(strings.TrimSpace(task.TaskType), "launch") {
				return headlessCodexOfficeLaunchTurnTimeout
			}
			return headlessCodexOfficeTurnTimeout
		}
	}
	return headlessCodexTurnTimeout
}

// headlessCodexStaleCancelAfterForTurn returns how long an active turn must run
// before a newer non-human enqueue may preempt it. Worktree builds and one-time
// office-launch turns are made effectively un-preemptable (threshold == their
// own hard timeout) because they are long, single-shot, and restarting them
// wastes the most work. Every other turn — including routine office turns —
// keeps the short default so an urgent same-task wake (e.g. a specialist
// handoff) can still cancel a stale lead turn and restart it with fresh
// context; that preemption re-enqueues the work, it never blocks the task, so
// it is not what falsely blocked tasks in prod (the 4m hard timeout was). slug
// is threaded only to resolve the task identically to the timeout path.
func (l *Launcher) headlessCodexStaleCancelAfterForTurn(slug string, turn headlessCodexTurn) time.Duration {
	if task := l.timedOutTaskForTurn(slug, turn); task != nil {
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
			return l.headlessCodexTurnTimeoutForTurn(slug, turn)
		}
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "office") &&
			strings.EqualFold(strings.TrimSpace(task.TaskType), "launch") {
			return l.headlessCodexTurnTimeoutForTurn(slug, turn)
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
