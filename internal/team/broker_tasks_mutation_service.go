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

// checkTaskActionAuthLocked enforces the hybrid CEO-managed Issues
// model. Returns nil when the actor may perform the action on the
// (optional) target task, or a TaskMutationForbidden error with a
// human-readable steer when not.
//
// Actor classes:
//   - humanActors: "human", "you", "" (system) — the operator. Always allowed.
//   - "system" / "broker": internal recovery (auto-resolve safety net,
//     self-heal, intake driver) — always allowed.
//   - leadActor: whoever holds the lead/CEO slug. Always allowed.
//   - taskOwner: the current Owner of the target task. Allowed for
//     status-transition actions on their own task.
//   - everyone else: specialist — only `comment` is open.
//
// Caller holds b.mu.
func (b *Broker) checkTaskActionAuthLocked(action, actor, targetTaskID string) error {
	a := strings.ToLower(strings.TrimSpace(action))
	actorSlug := strings.ToLower(strings.TrimSpace(actor))

	// Comment is open to all — every agent should be able to leave a
	// note on any Issue they can see.
	if a == "comment" {
		return nil
	}

	// Human + internal recovery actors are unrestricted.
	switch actorSlug {
	case "", "human", "you", "system", "broker", "nex":
		return nil
	}

	leadSlug := strings.ToLower(strings.TrimSpace(officeLeadSlugFrom(b.members)))
	// Pre-onboarding / test fixtures have no members → no lead. In
	// that state the office isn't managed yet, so don't block — the
	// gate only kicks in once a CEO/lead is in place.
	if leadSlug == "" {
		return nil
	}
	if actorSlug == leadSlug {
		return nil
	}
	// The gate ONLY blocks slugs that are registered as specialist
	// agents in this office. Unregistered actors (test slugs, CLI
	// scripts, external callers that pass an arbitrary created_by)
	// fall through — we have no basis to treat them as a specialist
	// being managed by CEO. This keeps tests + ad-hoc tooling working
	// while still blocking actual specialist agents from scope-editing
	// Issues that should go through CEO.
	if b.findMemberLocked(actorSlug) == nil {
		return nil
	}

	// Owner-allowed actions: the task's current owner can move their
	// own work through status transitions without going through CEO.
	// Requires a target task id to check ownership.
	ownerAllowed := map[string]bool{
		"submit_for_review": true,
		"review":            true,
		"complete":          true,
		"resume":            true,
		"release":           true,
		"claim":             true,
		"assign":            true,
		// block is the owner saying "I can't move because of <reason>"
		// (sets blocked=true). Allowed for owner so they can pause
		// their own work without going through CEO.
		"block": true,
		// cancel kills a task. Owner can cancel their own work; CEO
		// can cancel anything via the lead path above. Specialists
		// can't cancel work they don't own.
		"cancel": true,
	}
	// Reviewer-allowed actions: an agent assigned as a reviewer on the
	// task can bounce work back with request_changes and (in PR-loop
	// usage) approve/reject the submission. These are not "scope" edits
	// — they're the reviewer fulfilling their assigned role.
	reviewerAllowed := map[string]bool{
		"request_changes": true,
		"approve":         true,
		"reject":          true,
	}
	if targetTaskID != "" {
		if task := b.findTaskByIDLocked(strings.TrimSpace(targetTaskID)); task != nil {
			if ownerAllowed[a] && strings.EqualFold(strings.TrimSpace(task.Owner), actorSlug) {
				return nil
			}
			if reviewerAllowed[a] {
				for _, r := range task.Reviewers {
					if strings.EqualFold(strings.TrimSpace(r), actorSlug) {
						return nil
					}
				}
			}
		}
	}

	// Everything else is CEO-only for specialists. Steer them at the
	// suggestion channel so they know how to escalate scope ideas
	// without being silently blocked.
	return taskMutationError(
		TaskMutationForbidden,
		fmt.Sprintf(
			"only @%s (or the human) can %s an Issue. To propose a change, post team_task action=comment with a [SUGGESTION] prefix on the parent Issue and @-mention %s.",
			leadSlug, a, leadSlug,
		),
		nil,
	)
}

// defaultTaskTypeForCreate is the broker safety net for RULE ZERO. When an
// agent creates a top-level task via team_task action=create, the Issues
// board only renders rows with task_type="issue" (see web IssuesList
// isIssueSpecTask). Pre-fix the team_task tool schema listed example
// values "research, feature, launch, follow_up, bugfix, incident" without
// mentioning "issue", so LLMs picked one of those for human-asked work —
// the task landed in broker state but never reached the user-visible
// Issues board, defeating RULE ZERO. The prompt was updated to instruct
// task_type="issue", and the schema description rewritten, but we also
// override here so a regressed prompt cannot silently break the surface.
//
// Override scope: empty input and the bare "follow_up" default (the value
// LLMs reach for when the schema example lists it first) become "issue".
// Real pipeline values picked deliberately (feature / research / launch /
// bugfix / incident / custom) pass through — sub-tasks INSIDE an Issue
// are allowed to carry those typed values per the canonical workflow,
// and tests asserting pipeline-specific behaviour rely on explicit types.
// Part 2 ships parent_issue_id; once that lands, this override should
// only fire when parent_issue_id is empty (top-level work).
func defaultTaskTypeForCreate(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" {
		return "issue"
	}
	switch s {
	case "follow_up", "follow-up", "followup":
		return "issue"
	}
	return strings.TrimSpace(in)
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
	if !strings.EqualFold(strings.TrimSpace(task.status), "done") && strings.TrimSpace(task.CompletedAt) == "" {
		task.CompletedAt = timestamp
	}
	task.status = "done"
}

func reconcileTaskReviewState(task *teamTask, action string) {
	if task == nil {
		return
	}
	// PR-style review-loop actions write reviewState directly; the
	// reconciler must not overwrite their authoritative value with a
	// status-derived guess.
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "request_changes", "submit_for_review", "comment", "reject":
		return
	}
	if !taskNeedsStructuredReview(task) {
		// For new create actions, leave task.reviewState empty so downstream logic
		// can detect an uninitialized state; other actions or pre-set values
		// normalize to not_required when taskNeedsStructuredReview is false.
		if strings.TrimSpace(task.reviewState) != "" || !strings.EqualFold(strings.TrimSpace(action), "create") {
			task.reviewState = "not_required"
		}
		return
	}

	switch strings.ToLower(strings.TrimSpace(task.status)) {
	case "review":
		task.reviewState = "ready_for_review"
	case "done":
		switch {
		case strings.EqualFold(strings.TrimSpace(action), "approve"),
			strings.EqualFold(strings.TrimSpace(action), "complete"),
			strings.EqualFold(strings.TrimSpace(task.reviewState), "approved"):
			task.reviewState = "approved"
		default:
			task.reviewState = "ready_for_review"
		}
	default:
		switch strings.TrimSpace(task.reviewState) {
		case "pending_review", "ready_for_review", "approved":
		default:
			task.reviewState = "pending_review"
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

	// CEO-managed Issues gate (hybrid model, Slice 7):
	//   - scope-shaping actions (create / reassign / approve / reject /
	//     reopen / block / cancel) are restricted to CEO + human. They
	//     change WHAT the Issue is or WHO owns it.
	//   - owner status-transition actions (submit_for_review / complete
	//     / request_changes / resume / release / claim / assign) are
	//     allowed for CEO + human OR the task's current owner. They
	//     report WHERE the owner's own work is.
	//   - comment is always open — every agent can leave a note.
	//
	// Specialists who try to scope-edit get a clear error pointing them
	// at the suggestion channel (team_task action=comment with
	// [SUGGESTION] prefix). The auto-resolve / broker-internal create
	// path passes actor="system" or the broker's own slug which we
	// allow-list below so safety-net Issue creation keeps working.
	if err := b.checkTaskActionAuthLocked(action, actor, body.ID); err != nil {
		return TaskResponse{}, err
	}

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
			beforeStatus := existing.status
			if details := strings.TrimSpace(body.Details); details != "" {
				existing.Details = details
			}
			if owner := strings.TrimSpace(body.Owner); owner != "" {
				existing.Owner = owner
				existing.status = "in_progress"
			}
			if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
				existing.TaskType = defaultTaskTypeForCreate(taskType)
			}
			if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
				existing.PipelineID = pipelineID
			}
			if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
				existing.ExecutionMode = executionMode
			}
			if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
				existing.reviewState = reviewState
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
			b.reindexTaskLifecycleFromLegacyLocked(existing)
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
			b.appendActionLocked("task_updated", "office", channel, actor, truncateSummary(existing.Title+" ["+existing.status+"]", 140), existing.ID)
			if err := b.saveLocked(); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
			}
			b.emitTaskTransitionAutoNotebook(existing, beforeStatus, actor)
			return TaskResponse{Task: *existing}, nil
		}
		b.counter++
		task := teamTask{
			ID:               b.allocateIssueIDLocked(),
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			Details:          strings.TrimSpace(body.Details),
			Owner:            strings.TrimSpace(body.Owner),
			status:           "open",
			CreatedBy:        actor,
			ThreadID:         strings.TrimSpace(body.ThreadID),
			TaskType:         defaultTaskTypeForCreate(body.TaskType),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			ExecutionMode:    strings.TrimSpace(body.ExecutionMode),
			reviewState:      strings.TrimSpace(body.ReviewState),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
			WorktreePath:     strings.TrimSpace(body.WorktreePath),
			WorktreeBranch:   strings.TrimSpace(body.WorktreeBranch),
			DependsOn:        trimTaskDependencies(body.DependsOn),
			ParentIssueID:    strings.TrimSpace(body.ParentIssueID),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		// Force task_type=issue when parent_issue_id is set so every
		// sub-issue is renderable on the Issue detail surface (same
		// lifecycle, same packet, same components). Agents may pass
		// task_type=research/feature on sub-issues; we override here
		// so the FE doesn't have to special-case those.
		if task.ParentIssueID != "" {
			task.TaskType = "issue"
			// Sub-issues nest one level deep only. If the proposed
			// parent is itself a sub-issue (has its own parent),
			// reject the create so we don't get sub-sub-sub-issue
			// cascades the UI can't render legibly. Agents can
			// always rescope a sub-issue under the top-level parent
			// instead. The check stays inside the locked section
			// since we're reading other tasks' state.
			if parent := b.findTaskByIDLocked(task.ParentIssueID); parent != nil {
				if strings.TrimSpace(parent.ParentIssueID) != "" {
					rollbackTask()
					return TaskResponse{}, taskMutationError(
						TaskMutationConflict,
						"sub-issues can only be one level deep; pick the top-level parent instead",
						nil,
					)
				}
			}
		}
		if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.blocked = true
		} else if task.Owner != "" {
			task.status = "in_progress"
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
		b.reindexTaskLifecycleFromLegacyLocked(&task)
		b.tasks = append(b.tasks, task)
		b.indexTaskLocked(task.ID, len(b.tasks)-1)
		// Issues start in `drafting` regardless of whether an owner is
		// set, so the human sees the new Issue in the Drafting column
		// with an Approve & Start button before the agent begins work.
		// Without this, owner-set Issues go straight to `in_progress`
		// / running and the agent ploughs ahead — defeating the
		// human-approval gate the user demanded for "any work getting
		// done has an Issue in place". applyLifecycleStateLocked sets
		// pipeline_stage=draft, review_state=pending_review, status=open,
		// blocked=false, and updates LifecycleState in-place. Errors
		// are ignored: the only failure mode is an unrecognized state,
		// and LifecycleStateDrafting is canonical.
		if strings.EqualFold(task.TaskType, "issue") {
			// Preserve the dependency-block flag across the drafting
			// transition. LifecycleStateDrafting's derived row has
			// blocked=false (drafting issues aren't in any dispatch
			// queue), but a freshly created issue with unresolved
			// deps was just marked blocked above (line ~281). Without
			// this restore, the block silently disappears when the
			// human clicks Approve & Start.
			depBlocked := b.tasks[len(b.tasks)-1].blocked
			// Blank the legacy-derived LifecycleState before applying
			// Drafting so the prev=="" guard in applyLifecycleStateLocked
			// suppresses the (otherwise) redundant issue_lifecycle chat
			// card on Issue creation — postIssueCreatedCardLocked below
			// is the one card we want for the create event.
			b.tasks[len(b.tasks)-1].LifecycleState = ""
			_ = b.applyLifecycleStateLocked(&b.tasks[len(b.tasks)-1], LifecycleStateDrafting)
			if depBlocked {
				b.tasks[len(b.tasks)-1].blocked = true
			}
			task = b.tasks[len(b.tasks)-1]
		}
		b.appendActionLocked("task_created", "office", channel, task.CreatedBy, truncateSummary(task.Title, 140), task.ID)
		// Seed a Decision Packet for Issues so the /tasks/{id} read path
		// returns 200 immediately instead of 404 "decision packet not yet
		// available". Pre-fix: tasks created via team_task action=create
		// had no packet until Lane B's intake driver ran SetSpec, so the
		// Issue detail surface failed to load and the user saw "Could not
		// load issue" even though the row was on the board. The packet
		// starts empty; intake fills spec.goal / context / approach /
		// acceptance as CEO streams them. Only seed for task_type=issue
		// so internal types (skill_review_nudge, incident self-heal) keep
		// their legacy no-packet behaviour.
		if strings.EqualFold(task.TaskType, "issue") {
			packet := b.getOrInitPacketLocked(task.ID)
			if packet != nil {
				b.stampLifecycleStateLocked(packet)
				b.persistDecisionPacketLocked(task.ID, *packet)
			}
			// Post the issue card into the channel so the human (and
			// other agents) see the new Issue land in chat with a
			// one-click link to the detail view. Independent of any
			// chat reply the creating agent posts itself.
			b.postIssueCreatedCardLocked(actor, &task)
		}
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
		}
		// Treat creation as a transition from "" → task.status so the owner's
		// shelf records the moment a task lands in their lane.
		b.emitTaskTransitionAutoNotebook(&task, "", actor)
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
		requestChangesTriggered := false
		rejectTriggered := false
		submitForReviewTriggered := false
		beforeStatus := task.status
		switch action {
		case "claim", "assign":
			if strings.TrimSpace(body.Owner) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "owner required", nil)
			}
			task.Owner = strings.TrimSpace(body.Owner)
			task.status = "in_progress"
			if taskNeedsStructuredReview(task) {
				task.reviewState = "pending_review"
			} else {
				task.reviewState = "not_required"
			}
		case "reassign":
			if strings.TrimSpace(body.Owner) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "owner required", nil)
			}
			reassignPrevOwner = strings.TrimSpace(task.Owner)
			newOwner := strings.TrimSpace(body.Owner)
			task.Owner = newOwner
			status := strings.ToLower(strings.TrimSpace(task.status))
			if status != "done" && status != "review" {
				task.status = "in_progress"
			}
			if taskNeedsStructuredReview(task) && strings.TrimSpace(task.reviewState) == "" {
				task.reviewState = "pending_review"
			}
			reassignTriggered = reassignPrevOwner != newOwner
		case "complete":
			if strings.EqualFold(strings.TrimSpace(task.status), "done") {
				if taskNeedsStructuredReview(task) {
					task.reviewState = "approved"
				}
				task.blocked = false
			} else if strings.EqualFold(strings.TrimSpace(task.status), "review") ||
				strings.EqualFold(strings.TrimSpace(task.reviewState), "ready_for_review") {
				markTaskDone(task, now)
				if taskNeedsStructuredReview(task) {
					task.reviewState = "approved"
				}
				task.blocked = false
			} else if taskNeedsStructuredReview(task) {
				task.status = "review"
				task.reviewState = "ready_for_review"
			} else {
				markTaskDone(task, now)
				task.blocked = false
			}
		case "review":
			task.status = "review"
			task.reviewState = "ready_for_review"
		case "approve":
			markTaskDone(task, now)
			task.blocked = false
			if taskNeedsStructuredReview(task) {
				task.reviewState = "approved"
			}
		case "block":
			if err := rejectFalseLocalWorktreeBlock(task, body.Details); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
			task.status = "blocked"
			task.blocked = true
		case "resume":
			if task.blocked {
				task.blocked = false
			}
			if strings.EqualFold(strings.TrimSpace(task.status), "blocked") {
				if strings.TrimSpace(task.Owner) != "" {
					task.status = "in_progress"
				} else {
					task.status = "open"
				}
			}
			appendDetails = true
		case "reopen":
			// Reopen a closed (rejected/cancelled/approved) Issue back
			// into drafting so the human can re-approve to restart work.
			// Clears the terminal state markers; applyLifecycleStateLocked
			// below restores the rest of the drafting tuple.
			task.status = "open"
			task.reviewState = "pending_review"
			task.pipelineStage = "draft"
			task.blocked = false
			task.CompletedAt = ""
			task.LifecycleState = LifecycleStateDrafting
			if err := b.applyLifecycleStateLocked(task, LifecycleStateDrafting); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationConflict, "could not reopen issue", err)
			}
			appendDetails = true
		case "release":
			task.Owner = ""
			task.status = "open"
			task.blocked = false
		case "cancel":
			cancelPrevOwner = strings.TrimSpace(task.Owner)
			task.status = "canceled"
			task.blocked = false
			task.FollowUpAt = ""
			task.ReminderAt = ""
			task.RecheckAt = ""
			cancelTriggered = true
		case "request_changes":
			// PR-like revision loop. Reviewer rejects the current
			// submission and bounces the task back to its existing
			// owner with feedback. Owner stays unchanged; status
			// resets so the owner picks up the rework.
			task.status = "in_progress"
			task.reviewState = "changes_requested"
			task.blocked = false
			appendDetails = true
			requestChangesTriggered = true
		case "submit_for_review":
			// Explicit "hand off to reviewer" action so executor
			// agents have a verb that matches PR-review intent
			// instead of overloading "complete". The Details field
			// (if present) carries the submitted artifact (code,
			// copy, plan) which we capture below as a FeedbackItem
			// so it shows up in the unified Inbox Discussion thread.
			// appendDetails preserves prior task details (planner
			// spec, earlier submission notes) instead of clobbering
			// them with each resubmit.
			task.status = "review"
			task.reviewState = "ready_for_review"
			appendDetails = true
			submitForReviewTriggered = true
		case "comment":
			// Append-only comment with no state change. Used by both
			// humans and agents to leave PR-style notes on a task
			// before anyone decides to approve / request changes /
			// reject. The actual append happens below via the
			// appendDetails branch.
			appendDetails = true
		case "reject":
			// Terminal "this work cannot land" outcome. Distinct from
			// block (recoverable, waiting on upstream) and from
			// request_changes (revise + resubmit). LifecycleStateRejected
			// keeps Blocked=true so unblockDependentsLocked treats the
			// upstream as unresolved and downstream tasks STAY blocked.
			//
			// Reject must carry a reason — a terminal "this won't land"
			// without context is hostile to the agent that has to
			// pivot. Enforce that contract at the API boundary so the
			// "@human reviewer rejected without saying why" failure
			// mode can't happen.
			if strings.TrimSpace(body.Details) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "reject reason required", nil)
			}
			if err := b.applyLifecycleStateLocked(task, LifecycleStateRejected); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, err.Error(), err)
			}
			appendDetails = true
			rejectTriggered = true
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
			task.reviewState = reviewState
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
		if !strings.EqualFold(strings.TrimSpace(task.status), "done") {
			task.CompletedAt = ""
		}
		reconcileTaskReviewState(task, action)
		b.reindexTaskLifecycleFromLegacyLocked(task)
		syncTaskMemoryWorkflow(task, now)
		if strings.EqualFold(strings.TrimSpace(task.status), "done") {
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
		var pendingCascade []pendingTaskTransition
		if isTerminalTeamTaskStatus(task.status) {
			pendingCascade = b.unblockDependentsLocked(task.ID)
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
		}
		b.appendActionLocked("task_updated", "office", taskChannel, actor, truncateSummary(task.Title+" ["+task.status+"]", 140), task.ID)
		if action == "block" {
			b.requestCapabilitySelfHealingLocked(task, actor, body.Details)
		}
		if reassignTriggered {
			b.postTaskReassignNotificationsLocked(actor, task, reassignPrevOwner)
		}
		if cancelTriggered {
			b.postTaskCancelNotificationsLocked(actor, task, cancelPrevOwner)
		}
		if requestChangesTriggered {
			feedback := strings.TrimSpace(body.Details)
			b.postTaskRequestChangesNotificationsLocked(actor, task, feedback)
			b.AppendPacketFeedbackLocked(strings.TrimSpace(task.ID), actor, feedback)
		}
		if action == "comment" {
			b.AppendPacketFeedbackLocked(strings.TrimSpace(task.ID), actor, strings.TrimSpace(body.Details))
		}
		if submitForReviewTriggered {
			// Capture the submitted artifact (code, copy, plan) into
			// the Decision Packet's feedback thread so reviewers see
			// the exact submission inline in the unified Inbox.
			artifact := strings.TrimSpace(body.Details)
			if artifact != "" {
				b.AppendPacketFeedbackLocked(strings.TrimSpace(task.ID), actor, "📤 Submitted for review:\n"+artifact)
			}
		}
		if rejectTriggered {
			feedback := strings.TrimSpace(body.Details)
			b.postTaskRejectedNotificationsLocked(actor, task, feedback)
			b.AppendPacketFeedbackLocked(strings.TrimSpace(task.ID), actor, feedback)
		}
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
		}
		b.emitTaskTransitionAutoNotebook(task, beforeStatus, actor)
		b.flushPendingAutoNotebookTransitionsLocked(pendingCascade, "system")
		return TaskResponse{Task: *task}, nil
	}

	return TaskResponse{}, taskMutationError(TaskMutationNotFound, "task not found", nil)
}
