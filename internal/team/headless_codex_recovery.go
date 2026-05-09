package team

// headless_codex_recovery.go owns the post-turn recovery layer of
// headless dispatch (PLAN.md §C18): durability checks (did the turn
// actually persist its work?), the retry-prompt builders (timeout vs
// failure shapes), the recovery dispatchers that re-enqueue with
// updated attempt counts, and the agent-posting heuristics used to
// decide whether a "final message" should be auto-posted on silent
// turns. Split out of headless_codex.go so the entry-point file
// stays focused on dispatch + types.

import (
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

func taskHasDurableCompletionState(task *teamTask) bool {
	if task == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.status))
	review := strings.ToLower(strings.TrimSpace(task.reviewState))
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
			status := strings.ToLower(strings.TrimSpace(task.status))
			switch status {
			case "done", "completed", "review":
				if executed {
					return true, ""
				}
				return false, fmt.Sprintf("external-action turn for #%s marked %s/%s without recorded external execution evidence", task.ID, strings.TrimSpace(task.status), strings.TrimSpace(task.reviewState))
			case "blocked", "cancelled", "canceled":
				if attempted {
					return true, ""
				}
				return false, fmt.Sprintf("external-action turn for #%s moved to %s without recorded external workflow evidence", task.ID, strings.TrimSpace(task.status))
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
				return false, fmt.Sprintf("coding turn for #%s changed workspace %s but left task %s/%s without durable completion evidence", task.ID, workspaceDir, strings.TrimSpace(task.status), strings.TrimSpace(task.reviewState))
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

// isSubstantiveAgentProgressMessage rejects messages that don't count
// as durable evidence the agent made progress. STATUS pings and
// agent_issue helpdesk pings are explicitly out — they're noise the
// human asks the agent to clarify, not turn output.
func isSubstantiveAgentProgressMessage(msg channelMessage) bool {
	if strings.TrimSpace(msg.Kind) == agentIssueMessageKind {
		return false
	}
	content := strings.TrimSpace(msg.Content)
	return content != "" && !strings.HasPrefix(content, "[STATUS]")
}

func (l *Launcher) agentPostedSubstantiveMessageSince(slug string, startedAt time.Time) bool {
	if l == nil || l.broker == nil {
		return false
	}
	for _, msg := range l.broker.AllMessages() {
		if msg.From != slug {
			continue
		}
		if !isSubstantiveAgentProgressMessage(msg) {
			continue
		}
		// parseBrokerTimestamp (broker.go) accepts RFC3339 + RFC3339Nano
		// so fractional-second timestamps from a high-res clock still
		// parse. Returns zero on parse failure.
		when := parseBrokerTimestamp(msg.Timestamp)
		if when.IsZero() {
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
		if !isSubstantiveAgentProgressMessage(msg) {
			continue
		}
		when := parseBrokerTimestamp(msg.Timestamp)
		if when.IsZero() {
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
		_, _, _ = l.requestSelfHealing(slug, task.ID, agent.EscalationStuck, reason)
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
		_, _, _ = l.requestSelfHealing(slug, task.ID, agent.EscalationMaxRetries, reason)
	}
}

func (l *Launcher) timedOutTurnAlreadyRecovered(task *teamTask, slug string, startedAt time.Time) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		status := strings.ToLower(strings.TrimSpace(task.status))
		review := strings.ToLower(strings.TrimSpace(task.reviewState))
		// Include canceled / cancelled so a user-canceled task isn't
		// requeued or blocked-again by the timeout recovery path.
		// Same semantics as a "done" or "blocked" terminal state for
		// recovery purposes.
		// Mirror taskHasDurableCompletionState's terminal-state set
		// (which includes "completed") so the fast path here doesn't
		// requeue or re-block a task that's already terminal-by-name
		// but in the "completed" branch.
		return status == "done" || status == "completed" || status == "review" || status == "blocked" ||
			status == "canceled" || status == "cancelled" ||
			review == "ready_for_review" || review == "approved"
	}
	return l.agentPostedSubstantiveMessageSince(slug, startedAt)
}
