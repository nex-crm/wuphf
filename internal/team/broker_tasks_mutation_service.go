package team

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type TaskMutationErrorKind string

const (
	TaskMutationInvalid        TaskMutationErrorKind = "invalid"
	TaskMutationForbidden      TaskMutationErrorKind = "forbidden"
	TaskMutationNotFound       TaskMutationErrorKind = "not_found"
	TaskMutationConflict       TaskMutationErrorKind = "conflict"
	TaskMutationWorktreeFailed TaskMutationErrorKind = "worktree_failed"
	TaskMutationPersistFailed  TaskMutationErrorKind = "persist_failed"
)

type TaskMutationError struct {
	Kind    TaskMutationErrorKind
	Message string
	Cause   error
}

func (e *TaskMutationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *TaskMutationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func taskMutationError(kind TaskMutationErrorKind, message string, cause error) error {
	return &TaskMutationError{Kind: kind, Message: message, Cause: cause}
}

func writeTaskMutationHTTPError(w http.ResponseWriter, err error) {
	var mutationErr *TaskMutationError
	if !errors.As(err, &mutationErr) {
		log.Printf("task mutation: unexpected error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	status := http.StatusInternalServerError
	switch mutationErr.Kind {
	case TaskMutationInvalid:
		status = http.StatusBadRequest
	case TaskMutationForbidden:
		status = http.StatusForbidden
	case TaskMutationNotFound:
		status = http.StatusNotFound
	case TaskMutationConflict:
		status = http.StatusConflict
	case TaskMutationWorktreeFailed, TaskMutationPersistFailed:
		status = http.StatusInternalServerError
	}
	http.Error(w, mutationErr.Message, status)
}

func trimTaskDependencies(deps []string) []string {
	if len(deps) == 0 {
		return nil
	}
	trimmedDeps := make([]string, 0, len(deps))
	for _, dep := range deps {
		if trimmed := strings.TrimSpace(dep); trimmed != "" {
			trimmedDeps = append(trimmedDeps, trimmed)
		}
	}
	return trimmedDeps
}

func markTaskDone(task *teamTask, timestamp string) {
	if task == nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(task.Status), "done") && strings.TrimSpace(task.CompletedAt) == "" {
		task.CompletedAt = timestamp
	}
	task.Status = "done"
}

func reconcileTaskReviewState(task *teamTask, action string) {
	if task == nil {
		return
	}
	if !taskNeedsStructuredReview(task) {
		if strings.TrimSpace(task.ReviewState) != "" || !strings.EqualFold(strings.TrimSpace(action), "create") {
			task.ReviewState = "not_required"
		}
		return
	}

	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "review":
		task.ReviewState = "ready_for_review"
	case "done":
		switch {
		case strings.EqualFold(strings.TrimSpace(action), "approve"),
			strings.EqualFold(strings.TrimSpace(action), "complete"),
			strings.EqualFold(strings.TrimSpace(task.ReviewState), "approved"):
			task.ReviewState = "approved"
		default:
			task.ReviewState = "ready_for_review"
		}
	default:
		switch strings.TrimSpace(task.ReviewState) {
		case "pending_review", "ready_for_review", "approved":
		default:
			task.ReviewState = "pending_review"
		}
	}
}

func (b *Broker) MutateTask(body TaskPostRequest) (TaskResponse, error) {
	action := strings.TrimSpace(body.Action)
	actor := strings.TrimSpace(body.CreatedBy)
	now := time.Now().UTC().Format(time.RFC3339)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if action == "create" {
		if b.findChannelLocked(channel) == nil {
			return TaskResponse{}, taskMutationError(TaskMutationNotFound, "channel not found", nil)
		}
		if strings.TrimSpace(body.Title) == "" || actor == "" {
			return TaskResponse{}, taskMutationError(TaskMutationInvalid, "title and created_by required", nil)
		}
		if !b.canAccessChannelLocked(actor, channel) {
			return TaskResponse{}, taskMutationError(TaskMutationForbidden, "channel access denied", nil)
		}

		mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
		rollbackTask := func() {
			mutationSnapshot.restore(b)
		}
		if existing := b.findReusableTaskLocked(taskReuseMatch{
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			ThreadID:         strings.TrimSpace(body.ThreadID),
			Owner:            strings.TrimSpace(body.Owner),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
		}); existing != nil {
			if details := strings.TrimSpace(body.Details); details != "" {
				existing.Details = details
			}
			if owner := strings.TrimSpace(body.Owner); owner != "" {
				existing.Owner = owner
				existing.Status = "in_progress"
			}
			if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
				existing.TaskType = taskType
			}
			if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
				existing.PipelineID = pipelineID
			}
			if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
				existing.ExecutionMode = executionMode
			}
			if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
				existing.ReviewState = reviewState
			}
			if sourceSignalID := strings.TrimSpace(body.SourceSignalID); sourceSignalID != "" {
				existing.SourceSignalID = sourceSignalID
			}
			if sourceDecisionID := strings.TrimSpace(body.SourceDecisionID); sourceDecisionID != "" {
				existing.SourceDecisionID = sourceDecisionID
			}
			if worktreePath := strings.TrimSpace(body.WorktreePath); worktreePath != "" {
				existing.WorktreePath = worktreePath
			}
			if worktreeBranch := strings.TrimSpace(body.WorktreeBranch); worktreeBranch != "" {
				existing.WorktreeBranch = worktreeBranch
			}
			if existing.ThreadID == "" && strings.TrimSpace(body.ThreadID) != "" {
				existing.ThreadID = strings.TrimSpace(body.ThreadID)
			}
			reconcileTaskReviewState(existing, action)
			syncTaskMemoryWorkflow(existing, now)
			b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
			existing.UpdatedAt = now
			if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
			}
			b.appendActionLocked("task_updated", "office", channel, actor, truncateSummary(existing.Title+" ["+existing.Status+"]", 140), existing.ID)
			if err := b.saveLocked(); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
			}
			return TaskResponse{Task: *existing}, nil
		}
		b.counter++
		task := teamTask{
			ID:               fmt.Sprintf("task-%d", b.counter),
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			Details:          strings.TrimSpace(body.Details),
			Owner:            strings.TrimSpace(body.Owner),
			Status:           "open",
			CreatedBy:        actor,
			ThreadID:         strings.TrimSpace(body.ThreadID),
			TaskType:         strings.TrimSpace(body.TaskType),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			ExecutionMode:    strings.TrimSpace(body.ExecutionMode),
			ReviewState:      strings.TrimSpace(body.ReviewState),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
			WorktreePath:     strings.TrimSpace(body.WorktreePath),
			WorktreeBranch:   strings.TrimSpace(body.WorktreeBranch),
			DependsOn:        trimTaskDependencies(body.DependsOn),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.Blocked = true
		} else if task.Owner != "" {
			task.Status = "in_progress"
		}
		reconcileTaskReviewState(&task, action)
		syncTaskMemoryWorkflow(&task, now)
		b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&task)
		if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
		}
		b.scheduleTaskLifecycleLocked(&task)
		if err := b.syncTaskWorktreeLocked(&task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
		}
		b.tasks = append(b.tasks, task)
		b.appendActionLocked("task_created", "office", channel, task.CreatedBy, truncateSummary(task.Title, 140), task.ID)
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
		}
		return TaskResponse{Task: task}, nil
	}

	requestedID := strings.TrimSpace(body.ID)
	for i := range b.tasks {
		if b.tasks[i].ID != requestedID {
			continue
		}
		task := &b.tasks[i]
		mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
		rollbackTask := func() {
			mutationSnapshot.restore(b)
		}
		taskChannel := normalizeChannelSlug(task.Channel)
		if taskChannel == "" {
			taskChannel = channel
		}
		if b.findChannelLocked(taskChannel) == nil {
			return TaskResponse{}, taskMutationError(TaskMutationNotFound, "channel not found", nil)
		}
		// Authorize against the task's actual channel, not caller-supplied body.Channel.
		if !b.canAccessChannelLocked(actor, taskChannel) {
			return TaskResponse{}, taskMutationError(TaskMutationForbidden, "channel access denied", nil)
		}
		appendDetails := false
		reassignPrevOwner := ""
		reassignTriggered := false
		cancelTriggered := false
		cancelPrevOwner := ""
		switch action {
		case "claim", "assign":
			if strings.TrimSpace(body.Owner) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "owner required", nil)
			}
			task.Owner = strings.TrimSpace(body.Owner)
			task.Status = "in_progress"
			if taskNeedsStructuredReview(task) {
				task.ReviewState = "pending_review"
			} else {
				task.ReviewState = "not_required"
			}
		case "reassign":
			if strings.TrimSpace(body.Owner) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "owner required", nil)
			}
			reassignPrevOwner = strings.TrimSpace(task.Owner)
			newOwner := strings.TrimSpace(body.Owner)
			task.Owner = newOwner
			status := strings.ToLower(strings.TrimSpace(task.Status))
			if status != "done" && status != "review" {
				task.Status = "in_progress"
			}
			if taskNeedsStructuredReview(task) && strings.TrimSpace(task.ReviewState) == "" {
				task.ReviewState = "pending_review"
			}
			reassignTriggered = reassignPrevOwner != newOwner
		case "complete":
			if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
				if taskNeedsStructuredReview(task) {
					task.ReviewState = "approved"
				}
				task.Blocked = false
			} else if strings.EqualFold(strings.TrimSpace(task.Status), "review") ||
				strings.EqualFold(strings.TrimSpace(task.ReviewState), "ready_for_review") {
				markTaskDone(task, now)
				if taskNeedsStructuredReview(task) {
					task.ReviewState = "approved"
				}
				task.Blocked = false
			} else if taskNeedsStructuredReview(task) {
				task.Status = "review"
				task.ReviewState = "ready_for_review"
			} else {
				markTaskDone(task, now)
				task.Blocked = false
			}
		case "review":
			task.Status = "review"
			task.ReviewState = "ready_for_review"
		case "approve":
			markTaskDone(task, now)
			task.Blocked = false
			if taskNeedsStructuredReview(task) {
				task.ReviewState = "approved"
			}
		case "block":
			if err := rejectFalseLocalWorktreeBlock(task, body.Details); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
			task.Status = "blocked"
			task.Blocked = true
		case "resume":
			if task.Blocked {
				task.Blocked = false
			}
			if strings.EqualFold(strings.TrimSpace(task.Status), "blocked") {
				if strings.TrimSpace(task.Owner) != "" {
					task.Status = "in_progress"
				} else {
					task.Status = "open"
				}
			}
			appendDetails = true
		case "release":
			task.Owner = ""
			task.Status = "open"
			task.Blocked = false
		case "cancel":
			cancelPrevOwner = strings.TrimSpace(task.Owner)
			task.Status = "canceled"
			task.Blocked = false
			task.FollowUpAt = ""
			task.ReminderAt = ""
			task.RecheckAt = ""
			cancelTriggered = true
		default:
			return TaskResponse{}, taskMutationError(TaskMutationInvalid, "unknown action", nil)
		}
		if strings.TrimSpace(body.Details) != "" {
			if appendDetails {
				if err := appendTaskDetailLocked(task, body.Details); err != nil {
					rollbackTask()
					return TaskResponse{}, taskMutationError(TaskMutationInvalid, err.Error(), err)
				}
			} else {
				task.Details = strings.TrimSpace(body.Details)
			}
		}
		if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
			task.TaskType = taskType
		}
		if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
			task.PipelineID = pipelineID
		}
		if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
			task.ExecutionMode = executionMode
		}
		if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
			task.ReviewState = reviewState
		}
		if sourceSignalID := strings.TrimSpace(body.SourceSignalID); sourceSignalID != "" {
			task.SourceSignalID = sourceSignalID
		}
		if sourceDecisionID := strings.TrimSpace(body.SourceDecisionID); sourceDecisionID != "" {
			task.SourceDecisionID = sourceDecisionID
		}
		if worktreePath := strings.TrimSpace(body.WorktreePath); worktreePath != "" {
			task.WorktreePath = worktreePath
		}
		if worktreeBranch := strings.TrimSpace(body.WorktreeBranch); worktreeBranch != "" {
			task.WorktreeBranch = worktreeBranch
		}
		if !strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			task.CompletedAt = ""
		}
		reconcileTaskReviewState(task, action)
		syncTaskMemoryWorkflow(task, now)
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			overrideActor := strings.TrimSpace(body.MemoryWorkflowOverrideActor)
			if overrideActor == "" {
				overrideActor = actor
			}
			overrideReason := strings.TrimSpace(body.MemoryWorkflowOverrideReason)
			if overrideReason == "" {
				overrideReason = strings.TrimSpace(body.OverrideReason)
			}
			if err := applyMemoryWorkflowCompletionGate(task, overrideActor, overrideReason, body.MemoryWorkflowOverride, now); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
		}
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
		}
		// Any terminal status releases waiting dependents. isTerminalTeamTaskStatus
		// matches hasUnresolvedDepsLocked so cancelled parents do not orphan dependents.
		if isTerminalTeamTaskStatus(task.Status) {
			b.unblockDependentsLocked(task.ID)
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
		}
		b.appendActionLocked("task_updated", "office", taskChannel, actor, truncateSummary(task.Title+" ["+task.Status+"]", 140), task.ID)
		if action == "block" {
			b.requestCapabilitySelfHealingLocked(task, actor, body.Details)
		}
		if reassignTriggered {
			b.postTaskReassignNotificationsLocked(actor, task, reassignPrevOwner)
		}
		if cancelTriggered {
			b.postTaskCancelNotificationsLocked(actor, task, cancelPrevOwner)
		}
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
		}
		return TaskResponse{Task: *task}, nil
	}

	return TaskResponse{}, taskMutationError(TaskMutationNotFound, "task not found", nil)
}
