package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (b *Broker) handleTaskPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body TaskPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	createdBy := strings.TrimSpace(body.CreatedBy)
	if createdBy == "" || len(body.Tasks) == 0 {
		http.Error(w, "created_by and tasks required", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findChannelLocked(channel) == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
	rollbackPlan := func() {
		mutationSnapshot.restore(b)
	}

	// Map title → task ID for resolving depends_on by title
	titleToID := map[string]string{}
	now := time.Now().UTC().Format(time.RFC3339)
	created := make([]teamTask, 0, len(body.Tasks))

	for _, item := range body.Tasks {
		taskChannel := b.preferredTaskChannelLocked(channel, createdBy, item.Assignee, item.Title, item.Details)
		if b.findChannelLocked(taskChannel) == nil {
			rollbackPlan()
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		// Authorize on the resolved task channel, not body.Channel — the
		// body channel is just a default and the planner may route the
		// task to a different channel where the assignee actually lives.
		// Without this gate any authenticated caller could plant tasks in
		// channels they aren't a member of by spoofing the body channel.
		if !b.canAccessChannelLocked(createdBy, taskChannel) {
			rollbackPlan()
			http.Error(w, "channel access denied", http.StatusForbidden)
			return
		}

		// Validate the per-task LLM runtime override at the boundary (covers
		// both the reuse-merge and fresh-create branches below) so a malformed
		// provider/effort never lands in broker-state.json.
		if err := validateTaskRuntimeFields(item.Provider, item.Model, item.Effort); err != nil {
			rollbackPlan()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Resolve depends_on: accept both task IDs and titles
		resolvedDeps := make([]string, 0, len(item.DependsOn))
		for _, dep := range item.DependsOn {
			dep = strings.TrimSpace(dep)
			if id, ok := titleToID[dep]; ok {
				resolvedDeps = append(resolvedDeps, id)
			} else {
				resolvedDeps = append(resolvedDeps, dep) // assume it's a task ID
			}
		}
		if existing := b.findReusableTaskLocked(taskReuseMatch{
			Channel: taskChannel,
			Title:   strings.TrimSpace(item.Title),
			Owner:   strings.TrimSpace(item.Assignee),
		}); existing != nil {
			titleToID[strings.TrimSpace(item.Title)] = existing.ID
			if details := strings.TrimSpace(item.Details); details != "" {
				existing.Details = details
			}
			if taskType := strings.TrimSpace(item.TaskType); taskType != "" {
				existing.TaskType = taskType
			}
			if executionMode := strings.TrimSpace(item.ExecutionMode); executionMode != "" {
				existing.ExecutionMode = executionMode
			}
			if effort := strings.TrimSpace(item.Effort); effort != "" {
				existing.Effort = effort
			}
			if prov := strings.TrimSpace(item.Provider); prov != "" {
				existing.Provider = prov
			}
			if model := strings.TrimSpace(item.Model); model != "" {
				existing.Model = model
			}
			existing.DependsOn = resolvedDeps
			b.refreshPlannedTaskBlockStateLocked(existing)
			syncTaskMemoryWorkflow(existing, now)
			b.ensureTaskOwnerChannelMembershipLocked(taskChannel, existing.Owner)
			b.queueTaskBehindActiveOwnerLaneLocked(existing)
			existing.UpdatedAt = now
			if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
				rollbackPlan()
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				rollbackPlan()
				http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
				return
			}
			b.appendActionLocked("task_updated", "office", taskChannel, createdBy, truncateSummary(existing.Title+" ["+existing.status+"]", 140), existing.ID)
			created = append(created, *existing)
			continue
		}

		// Effective Plan-first: honor an explicit per-task choice; otherwise
		// default from the owner agent's autonomy (PermissionMode "plan" →
		// plan-first). This is what makes a "plan" agent's delegated work run
		// the owner through the provider's native plan mode before executing.
		planFirst := item.PlanFirstEnabled()
		if item.PlanFirst == nil {
			planFirst = b.ownerDefaultsToPlanFirstLocked(strings.TrimSpace(item.Assignee))
		}

		b.counter++
		taskID := b.allocateIssueIDLocked()
		titleToID[strings.TrimSpace(item.Title)] = taskID
		// Mint a dedicated channel for new business-objective tasks
		// that defaulted to "general".
		if shouldMintPerTaskChannel(taskChannel, &teamTask{
			Title:         strings.TrimSpace(item.Title),
			Details:       strings.TrimSpace(item.Details),
			Owner:         strings.TrimSpace(item.Assignee),
			TaskType:      strings.TrimSpace(item.TaskType),
			ExecutionMode: strings.TrimSpace(item.ExecutionMode),
		}) {
			if ch := b.createPerTaskChannelLocked(taskID, strings.TrimSpace(item.Title), strings.TrimSpace(item.Assignee), createdBy); ch != nil {
				taskChannel = ch.Slug
			}
		}

		task := teamTask{
			ID:            taskID,
			Channel:       taskChannel,
			Title:         strings.TrimSpace(item.Title),
			Details:       strings.TrimSpace(item.Details),
			Owner:         strings.TrimSpace(item.Assignee),
			status:        "open",
			CreatedBy:     createdBy,
			TaskType:      strings.TrimSpace(item.TaskType),
			ExecutionMode: strings.TrimSpace(item.ExecutionMode),
			Effort:        strings.TrimSpace(item.Effort),
			Provider:      strings.TrimSpace(item.Provider),
			Model:         strings.TrimSpace(item.Model),
			PlanFirst:     planFirst,
			DependsOn:     resolvedDeps,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		b.refreshPlannedTaskBlockStateLocked(&task)
		// Lifecycle routing (Phase 5 Plan mode + Phase 3 Backlog):
		//   - Backlog (Park): assigned but parked in Drafting — non-executable,
		//     shows in Backlog, dispatches nobody. Activated via "Approve &
		//     Start", which routes through PlanFirst (Drafting→Planning or
		//     Drafting→Running) in the decision handler.
		//   - Plan first + real owner (start now): land in Planning so the owner
		//     is dispatched to write a plan first (plan-only packet), then
		//     "Approve & Start" → Running. Overrides the in_progress promotion.
		//   - Plan first OFF (start now): leave the in_progress promotion in
		//     place → runs immediately, no plan/approval gate.
		// (Auto-owner Plan-first tasks plan after the CEO assigns a specialist;
		// the reassign path routes them into Planning.)
		switch {
		case item.Park:
			if err := b.applyLifecycleStateLocked(&task, LifecycleStateDrafting); err != nil {
				rollbackPlan()
				http.Error(w, "failed to park task", http.StatusInternalServerError)
				return
			}
		case task.PlanFirst && task.status == "in_progress":
			if err := b.applyLifecycleStateLocked(&task, LifecycleStatePlanning); err != nil {
				rollbackPlan()
				http.Error(w, "failed to start planning", http.StatusInternalServerError)
				return
			}
		}
		syncTaskMemoryWorkflow(&task, now)
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&task)
		if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
			rollbackPlan()
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		b.scheduleTaskLifecycleLocked(&task)
		if err := b.syncTaskWorktreeLocked(&task); err != nil {
			rollbackPlan()
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.tasks = append(b.tasks, task)
		b.appendActionLocked("task_created", "office", taskChannel, createdBy, truncateSummary(task.Title, 140), task.ID)
		// Start-now + Auto: no real owner to dispatch, so ask the CEO to triage
		// (pick a specialist + start). Parked Auto tasks defer this to activate.
		if isAutoOwner(task.Owner) && !item.Park {
			b.requestAutoAssignmentLocked(&task, createdBy)
		}
		created = append(created, task)
	}

	if err := b.saveLocked(); err != nil {
		rollbackPlan()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TaskListResponse{Tasks: created})
}

type plannedTaskInput struct {
	Channel          string
	Title            string
	Details          string
	Owner            string
	CreatedBy        string
	ThreadID         string
	TaskType         string
	PipelineID       string
	ExecutionMode    string
	Effort           string
	Provider         string
	Model            string
	ReviewState      string
	SourceSignalID   string
	SourceDecisionID string
	DependsOn        []string
}

func (b *Broker) refreshPlannedTaskBlockStateLocked(task *teamTask) {
	if task == nil {
		return
	}
	if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(task) {
		task.blocked = true
		task.status = "open"
		return
	}
	task.blocked = false
	// An "auto" owner is a triage sentinel, not a real agent — it must not
	// promote the task to in_progress (there is no @auto to dispatch). The CEO
	// resolves it to a real specialist first (see requestAutoAssignmentLocked).
	if strings.TrimSpace(task.Owner) != "" && !isAutoOwner(task.Owner) {
		task.status = "in_progress"
	} else if strings.EqualFold(strings.TrimSpace(task.status), "blocked") {
		task.status = "open"
	}
}

func (b *Broker) EnsurePlannedTask(input plannedTaskInput) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := b.preferredTaskChannelLocked(input.Channel, input.CreatedBy, input.Owner, input.Title, input.Details)
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(input.CreatedBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}
	// Validate the per-task LLM runtime override at the boundary (covers both
	// the reuse-merge and fresh-create branches below).
	if err := validateTaskRuntimeFields(input.Provider, input.Model, input.Effort); err != nil {
		return teamTask{}, false, err
	}
	mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
	rollbackTask := func() {
		mutationSnapshot.restore(b)
	}
	title := strings.TrimSpace(input.Title)
	if existing := b.findReusableTaskLocked(taskReuseMatch{
		Channel:          channel,
		Title:            title,
		ThreadID:         strings.TrimSpace(input.ThreadID),
		Owner:            strings.TrimSpace(input.Owner),
		PipelineID:       strings.TrimSpace(input.PipelineID),
		SourceSignalID:   strings.TrimSpace(input.SourceSignalID),
		SourceDecisionID: strings.TrimSpace(input.SourceDecisionID),
	}); existing != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		if existing.Details == "" && strings.TrimSpace(input.Details) != "" {
			existing.Details = strings.TrimSpace(input.Details)
		}
		if existing.Owner == "" && strings.TrimSpace(input.Owner) != "" {
			existing.Owner = strings.TrimSpace(input.Owner)
		}
		if existing.ThreadID == "" && strings.TrimSpace(input.ThreadID) != "" {
			existing.ThreadID = strings.TrimSpace(input.ThreadID)
		}
		if existing.TaskType == "" && strings.TrimSpace(input.TaskType) != "" {
			existing.TaskType = strings.TrimSpace(input.TaskType)
		}
		if existing.PipelineID == "" && strings.TrimSpace(input.PipelineID) != "" {
			existing.PipelineID = strings.TrimSpace(input.PipelineID)
		}
		if existing.ExecutionMode == "" && strings.TrimSpace(input.ExecutionMode) != "" {
			existing.ExecutionMode = strings.TrimSpace(input.ExecutionMode)
		}
		if existing.Effort == "" && strings.TrimSpace(input.Effort) != "" {
			existing.Effort = strings.TrimSpace(input.Effort)
		}
		if existing.Provider == "" && strings.TrimSpace(input.Provider) != "" {
			existing.Provider = strings.TrimSpace(input.Provider)
		}
		if existing.Model == "" && strings.TrimSpace(input.Model) != "" {
			existing.Model = strings.TrimSpace(input.Model)
		}
		if existing.reviewState == "" && strings.TrimSpace(input.ReviewState) != "" {
			existing.reviewState = strings.TrimSpace(input.ReviewState)
		}
		if existing.SourceSignalID == "" && strings.TrimSpace(input.SourceSignalID) != "" {
			existing.SourceSignalID = strings.TrimSpace(input.SourceSignalID)
		}
		if existing.SourceDecisionID == "" && strings.TrimSpace(input.SourceDecisionID) != "" {
			existing.SourceDecisionID = strings.TrimSpace(input.SourceDecisionID)
		}
		if input.DependsOn != nil {
			existing.DependsOn = append([]string(nil), input.DependsOn...)
		}
		b.refreshPlannedTaskBlockStateLocked(existing)
		syncTaskMemoryWorkflow(existing, now)
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = now
		b.queueTaskBehindActiveOwnerLaneLocked(existing)
		if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
			rollbackTask()
			return teamTask{}, false, err
		}
		b.scheduleTaskLifecycleLocked(existing)
		if err := b.syncTaskWorktreeLocked(existing); err != nil {
			rollbackTask()
			return teamTask{}, false, err
		}
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return teamTask{}, false, err
		}
		return *existing, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// Allocate the task ID before choosing the channel so we can name
	// the per-task channel deterministically.
	b.counter++
	taskID := b.allocateIssueIDLocked()
	// Mint a dedicated channel for new business-objective tasks that
	// defaulted to "general".
	if shouldMintPerTaskChannel(channel, &teamTask{
		Title:         title,
		Details:       strings.TrimSpace(input.Details),
		Owner:         strings.TrimSpace(input.Owner),
		TaskType:      strings.TrimSpace(input.TaskType),
		PipelineID:    strings.TrimSpace(input.PipelineID),
		ExecutionMode: strings.TrimSpace(input.ExecutionMode),
	}) {
		if ch := b.createPerTaskChannelLocked(taskID, title, strings.TrimSpace(input.Owner), strings.TrimSpace(input.CreatedBy)); ch != nil {
			channel = ch.Slug
		}
	}
	task := teamTask{
		ID:               taskID,
		Channel:          channel,
		Title:            title,
		Details:          strings.TrimSpace(input.Details),
		Owner:            strings.TrimSpace(input.Owner),
		status:           "open",
		CreatedBy:        strings.TrimSpace(input.CreatedBy),
		ThreadID:         strings.TrimSpace(input.ThreadID),
		TaskType:         strings.TrimSpace(input.TaskType),
		PipelineID:       strings.TrimSpace(input.PipelineID),
		ExecutionMode:    strings.TrimSpace(input.ExecutionMode),
		Effort:           strings.TrimSpace(input.Effort),
		Provider:         strings.TrimSpace(input.Provider),
		Model:            strings.TrimSpace(input.Model),
		reviewState:      strings.TrimSpace(input.ReviewState),
		SourceSignalID:   strings.TrimSpace(input.SourceSignalID),
		SourceDecisionID: strings.TrimSpace(input.SourceDecisionID),
		DependsOn:        append([]string(nil), input.DependsOn...),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	b.refreshPlannedTaskBlockStateLocked(&task)
	syncTaskMemoryWorkflow(&task, now)
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		rollbackTask()
		return teamTask{}, false, err
	}
	b.scheduleTaskLifecycleLocked(&task)
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		rollbackTask()
		return teamTask{}, false, err
	}
	b.tasks = append(b.tasks, task)
	b.appendActionWithRefsLocked("task_created", "office", channel, input.CreatedBy, truncateSummary(task.Title, 140), task.ID, compactStringList([]string{task.SourceSignalID}), task.SourceDecisionID)
	if err := b.saveLocked(); err != nil {
		rollbackTask()
		return teamTask{}, false, err
	}
	return task, false, nil
}
