package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func (b *Broker) BlockTask(taskID, actor, reason string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := strings.TrimSpace(taskID)
	if id == "" {
		return teamTask{}, false, fmt.Errorf("task id required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	reason = strings.TrimSpace(reason)
	now := time.Now().UTC().Format(time.RFC3339)

	for i := range b.tasks {
		task := &b.tasks[i]
		if task.ID != id {
			continue
		}
		if isTerminalTeamTaskStatus(task.status) {
			return *task, false, nil
		}
		if err := rejectFalseLocalWorktreeBlock(task, reason); err != nil {
			return *task, false, err
		}
		beforeStatus := task.status
		if reason != "" {
			switch existing := strings.TrimSpace(task.Details); {
			case existing == "":
				task.Details = reason
			case !strings.Contains(existing, reason):
				task.Details = existing + "\n\n" + reason
			}
		}
		// Route the legacy block path through the lifecycle transition
		// layer so derived fields, the indexed lookup, and the self-heal
		// gate (build-time gate #1) all stay in sync. The transition
		// stamps status/blocked/pipelineStage/reviewState atomically and
		// updates the lifecycleIndex bucket.
		if _, err := b.transitionLifecycleLocked(task.ID, LifecycleStateBlockedOnPRMerge, reason); err != nil {
			return *task, false, err
		}
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			return *task, false, err
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			return teamTask{}, false, err
		}
		b.appendActionLocked("task_updated", "office", normalizeChannelSlug(task.Channel), actor, truncateSummary(task.Title+" ["+task.status+"]", 140), task.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		b.emitTaskTransitionAutoNotebook(task, beforeStatus, actor)
		return *task, true, nil
	}

	return teamTask{}, false, fmt.Errorf("task not found")
}

func (b *Broker) ResumeTask(taskID, actor, reason string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := strings.TrimSpace(taskID)
	if id == "" {
		return teamTask{}, false, fmt.Errorf("task id required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	reason = strings.TrimSpace(reason)
	now := time.Now().UTC().Format(time.RFC3339)

	for i := range b.tasks {
		task := &b.tasks[i]
		if task.ID != id {
			continue
		}
		beforeStatus := task.status
		changed := false
		// Strip the stale rate-limit marker on every ResumeTask call, not
		// only when this call flips Blocked from true to false. A different
		// code path (e.g. unblockDependentsLocked, capability self-healing)
		// may have already cleared Blocked while leaving the marker in
		// Details; without the unconditional strip the watchdog's
		// externalWorkflowRetryAfter check would still detect the stale
		// timestamp on its next tick and re-enter the resume loop. The
		// strip is a no-op when no marker is present.
		if cleaned := stripExternalRetryMarker(task.Details); cleaned != task.Details {
			task.Details = cleaned
			changed = true
		}
		if task.blocked || strings.EqualFold(strings.TrimSpace(task.status), "blocked") {
			targetState := LifecycleStateReady
			if strings.TrimSpace(task.Owner) != "" {
				targetState = LifecycleStateRunning
			}
			transitioned, err := b.transitionLifecycleLocked(task.ID, targetState, "task resumed")
			if err != nil {
				return teamTask{}, false, err
			}
			task = transitioned
			changed = true
		}
		if !changed {
			return *task, false, nil
		}
		if reason != "" && !strings.Contains(task.Details, reason) {
			task.Details = strings.TrimSpace(task.Details)
			if task.Details != "" {
				task.Details += "\n\n"
			}
			task.Details += reason
		}
		b.ensureTaskOwnerChannelMembershipLocked(task.Channel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			return teamTask{}, false, err
		}
		b.appendActionLocked("task_unblocked", "office", normalizeChannelSlug(task.Channel), actor, truncateSummary(task.Title+" resumed", 140), task.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		b.emitTaskTransitionAutoNotebook(task, beforeStatus, actor)
		return *task, true, nil
	}

	return teamTask{}, false, fmt.Errorf("task not found")
}

func (b *Broker) handleTaskAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body TaskAckRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	result, err := b.AckTask(body)
	if errors.Is(err, errTaskAckInvalid) {
		http.Error(w, "id and slug required", http.StatusBadRequest)
		return
	}
	if errors.Is(err, errTaskAckOwnerOnly) {
		http.Error(w, "only the task owner can ack", http.StatusForbidden)
		return
	}
	if errors.Is(err, errTaskNotFound) {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, errTaskPersistFailed) {
		http.Error(w, "failed to persist", http.StatusInternalServerError)
		return
	}
	if err != nil {
		log.Printf("tasks ack: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (b *Broker) EnsureTask(channel, title, details, owner, createdBy, threadID string, dependsOn ...string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = b.preferredTaskChannelLocked(channel, createdBy, owner, title, details)
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(createdBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}
	title = strings.TrimSpace(title)
	if existing := b.findReusableTaskLocked(taskReuseMatch{
		Channel:  channel,
		Title:    title,
		ThreadID: strings.TrimSpace(threadID),
		Owner:    strings.TrimSpace(owner),
	}); existing != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		beforeStatus := existing.status
		if existing.Details == "" && strings.TrimSpace(details) != "" {
			existing.Details = strings.TrimSpace(details)
		}
		if existing.Owner == "" && strings.TrimSpace(owner) != "" {
			existing.Owner = strings.TrimSpace(owner)
			if !existing.blocked {
				existing.status = "in_progress"
			}
		}
		if existing.ThreadID == "" && strings.TrimSpace(threadID) != "" {
			existing.ThreadID = strings.TrimSpace(threadID)
		}
		b.reindexTaskLifecycleFromLegacyLocked(existing)
		syncTaskMemoryWorkflow(existing, now)
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = now
		b.queueTaskBehindActiveOwnerLaneLocked(existing)
		if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
			return teamTask{}, false, err
		}
		b.scheduleTaskLifecycleLocked(existing)
		if err := b.syncTaskWorktreeLocked(existing); err != nil {
			return teamTask{}, false, err
		}
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		b.emitTaskTransitionAutoNotebook(existing, beforeStatus, createdBy)
		return *existing, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	task := teamTask{
		ID:        fmt.Sprintf("task-%d", b.counter),
		Channel:   channel,
		Title:     title,
		Details:   strings.TrimSpace(details),
		Owner:     strings.TrimSpace(owner),
		status:    "open",
		CreatedBy: strings.TrimSpace(createdBy),
		ThreadID:  strings.TrimSpace(threadID),
		DependsOn: dependsOn,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
		task.blocked = true
	} else if task.Owner != "" {
		task.status = "in_progress"
	}
	syncTaskMemoryWorkflow(&task, now)
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		return teamTask{}, false, err
	}
	b.scheduleTaskLifecycleLocked(&task)
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		return teamTask{}, false, err
	}
	b.reindexTaskLifecycleFromLegacyLocked(&task)
	b.tasks = append(b.tasks, task)
	b.appendActionLocked("task_created", "office", channel, createdBy, truncateSummary(task.Title, 140), task.ID)
	if err := b.saveLocked(); err != nil {
		return teamTask{}, false, err
	}
	b.emitTaskTransitionAutoNotebook(&task, "", createdBy)
	return task, false, nil
}

// AppendTaskDetail appends non-duplicate detail text to an existing task without
// changing ownership or status.
func (b *Broker) AppendTaskDetail(taskID, actor, detail string) (teamTask, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := strings.TrimSpace(taskID)
	if id == "" {
		return teamTask{}, fmt.Errorf("task id required")
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return teamTask{}, fmt.Errorf("detail required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}

	for i := range b.tasks {
		task := &b.tasks[i]
		if task.ID != id {
			continue
		}
		_ = appendTaskDetailLocked(task, detail)
		task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.appendActionLocked("task_updated", "office", normalizeChannelSlug(task.Channel), actor, truncateSummary(task.Title+" [updated]", 140), task.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, err
		}
		return *task, nil
	}

	return teamTask{}, fmt.Errorf("task not found")
}

func appendTaskDetailLocked(task *teamTask, detail string) error {
	if task == nil {
		return fmt.Errorf("task required")
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return fmt.Errorf("detail required")
	}
	if strings.Contains(task.Details, detail) {
		return nil
	}
	task.Details = strings.TrimSpace(task.Details)
	if task.Details != "" {
		task.Details += "\n\n"
	}
	task.Details += detail
	return nil
}

// hasUnresolvedDepsLocked returns true if any of the task's dependencies
// are still active. Any terminal status — done, completed, canceled,
// cancelled — counts as resolved. This mirrors requestIsResolvedLocked's
// treatment of cancelled humanInterview deps so a parent's cancellation
// no longer permanently orphans every dependent task. Missing deps still
// count as unresolved (dependency doesn't exist yet).
func (b *Broker) hasUnresolvedDepsLocked(task *teamTask) bool {
	for _, depID := range task.DependsOn {
		if requestIsResolvedLocked(b.requests, depID) {
			continue
		}
		found := false
		for j := range b.tasks {
			if b.tasks[j].ID == depID {
				found = true
				if !isTerminalTeamTaskStatus(b.tasks[j].status) {
					return true
				}
				break
			}
		}
		if !found {
			return true // dependency doesn't exist yet — treat as unresolved
		}
	}
	return false
}

// unblockDependentsLocked checks all blocked tasks and unblocks those whose
// dependencies are now resolved. For each newly unblocked task, it appends a
// "task_unblocked" action so the launcher can deliver a notification to the owner.
//
// Lane A extension: the function sweeps the union of (DependsOn, BlockedOn)
// and branches on LifecycleState so harness tasks (blocked_on_pr_merge) move
// to review on resolution while legacy DependsOn-blocked tasks keep their
// pre-Lane-A in_progress / open behavior.
//
// Returns the list of cascade transitions that the caller must publish to the
// auto-notebook writer AFTER its own saveLocked succeeds. Emitting under
// b.mu before the persist would leak notebook entries for transitions the
// broker subsequently rolled back on save failure (CodeRabbit, major).
func (b *Broker) unblockDependentsLocked(completedTaskID string) []pendingTaskTransition {
	now := time.Now().UTC().Format(time.RFC3339)
	var pending []pendingTaskTransition
	for i := range b.tasks {
		task := &b.tasks[i]
		if !task.blocked {
			continue
		}
		// Sweep both legacy DependsOn and the new typed BlockedOn list so
		// the same code path resolves harness tasks and pre-Lane-A tasks.
		hasDep := false
		for _, depID := range task.DependsOn {
			if depID == completedTaskID {
				hasDep = true
				break
			}
		}
		if !hasDep {
			for _, depID := range task.BlockedOn {
				if depID == completedTaskID {
					hasDep = true
					break
				}
			}
		}
		if !hasDep {
			continue
		}
		// Remove the resolved entry from BlockedOn before checking
		// whether anything still blocks the task.
		if len(task.BlockedOn) > 0 {
			filtered := task.BlockedOn[:0]
			for _, depID := range task.BlockedOn {
				if depID == completedTaskID {
					continue
				}
				filtered = append(filtered, depID)
			}
			task.BlockedOn = filtered
		}
		if b.hasUnresolvedDepsLocked(task) || len(task.BlockedOn) > 0 {
			continue
		}
		beforeStatus := task.status
		// Mirror the strip in ResumeTask: if the dependent task was
		// also rate-limited at some earlier point, the stale marker
		// in Details would otherwise trigger the watchdog resume loop
		// even though the dependency completion already unblocked it.
		task.Details = stripExternalRetryMarker(task.Details)
		// Branch on LifecycleState: harness tasks move to review,
		// legacy tasks fall back to the pre-Lane-A behavior. The
		// transition layer stamps every derived field for both paths.
		if task.LifecycleState == LifecycleStateBlockedOnPRMerge {
			if _, err := b.transitionLifecycleLocked(task.ID, LifecycleStateReview, "blocker resolved by "+completedTaskID); err != nil {
				continue
			}
		} else {
			targetState := LifecycleStateReady
			if strings.TrimSpace(task.Owner) != "" {
				targetState = LifecycleStateRunning
			}
			transitioned, err := b.transitionLifecycleLocked(task.ID, targetState, "legacy blocker resolved by "+completedTaskID)
			if err != nil {
				continue
			}
			task = transitioned
		}
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		b.scheduleTaskLifecycleLocked(task)
		_ = b.syncTaskWorktreeLocked(task)
		b.appendActionLocked(
			"task_unblocked",
			"office",
			normalizeChannelSlug(task.Channel),
			"system",
			truncateSummary(task.Title+" unblocked by "+completedTaskID, 140),
			task.ID,
		)
		pending = append(pending, pendingTaskTransition{
			taskID:       task.ID,
			beforeStatus: beforeStatus,
		})
	}
	return pending
}

type taskReuseMatch struct {
	Channel          string
	Title            string
	ThreadID         string
	Owner            string
	PipelineID       string
	SourceSignalID   string
	SourceDecisionID string
}

func (m taskReuseMatch) hasScopedIdentity() bool {
	return strings.TrimSpace(m.PipelineID) != "" ||
		strings.TrimSpace(m.SourceSignalID) != "" ||
		strings.TrimSpace(m.SourceDecisionID) != ""
}

func hasScopedTaskIdentity(task *teamTask) bool {
	if task == nil {
		return false
	}
	return strings.TrimSpace(task.SourceSignalID) != "" ||
		strings.TrimSpace(task.SourceDecisionID) != ""
}

func taskOwnerMatches(task *teamTask, owner string) bool {
	if task == nil {
		return false
	}
	taskOwner := strings.TrimSpace(task.Owner)
	return owner == "" || taskOwner == owner || taskOwner == ""
}

func scopedTaskIdentityMatches(task *teamTask, match taskReuseMatch) bool {
	if task == nil {
		return false
	}
	if match.PipelineID != "" {
		if strings.TrimSpace(task.PipelineID) != match.PipelineID {
			return false
		}
	}
	if match.SourceSignalID != "" && strings.TrimSpace(task.SourceSignalID) != match.SourceSignalID {
		return false
	}
	if match.SourceDecisionID != "" && strings.TrimSpace(task.SourceDecisionID) != match.SourceDecisionID {
		return false
	}
	return true
}

func taskCanMatchScopedIdentity(task *teamTask, match taskReuseMatch) bool {
	if hasScopedTaskIdentity(task) {
		return true
	}
	return strings.TrimSpace(match.PipelineID) != "" && strings.TrimSpace(task.PipelineID) != ""
}

func (b *Broker) findReusableTaskLocked(match taskReuseMatch) *teamTask {
	channel := normalizeChannelSlug(match.Channel)
	title := strings.TrimSpace(match.Title)
	threadID := strings.TrimSpace(match.ThreadID)
	owner := strings.TrimSpace(match.Owner)
	scopedIdentity := match.hasScopedIdentity()
	for i := range b.tasks {
		task := &b.tasks[i]
		if normalizeChannelSlug(task.Channel) != channel {
			continue
		}
		if isTerminalTeamTaskStatus(task.status) {
			continue
		}
		sameTitle := title != "" && strings.EqualFold(strings.TrimSpace(task.Title), title)
		if threadID != "" && strings.TrimSpace(task.ThreadID) == threadID {
			if sameTitle && taskOwnerMatches(task, owner) {
				taskHasScopedIdentity := taskCanMatchScopedIdentity(task, match)
				if scopedIdentity || taskHasScopedIdentity {
					if !scopedIdentity || !taskHasScopedIdentity {
						continue
					}
					if scopedTaskIdentityMatches(task, match) {
						return task
					}
					continue
				}
				return task
			}
			continue
		}
		if !sameTitle || !taskOwnerMatches(task, owner) {
			continue
		}
		taskHasScopedIdentity := taskCanMatchScopedIdentity(task, match)
		if scopedIdentity || taskHasScopedIdentity {
			if !scopedIdentity || !taskHasScopedIdentity {
				continue
			}
			if scopedTaskIdentityMatches(task, match) {
				return task
			}
			continue
		}
		return task
	}
	return nil
}

func isTerminalTeamTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "completed", "canceled", "cancelled":
		return true
	default:
		return false
	}
}
