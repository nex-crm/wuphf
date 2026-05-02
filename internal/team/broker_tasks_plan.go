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
	var body struct {
		Channel   string `json:"channel"`
		CreatedBy string `json:"created_by"`
		Tasks     []struct {
			Title         string   `json:"title"`
			Assignee      string   `json:"assignee"`
			Details       string   `json:"details"`
			TaskType      string   `json:"task_type"`
			ExecutionMode string   `json:"execution_mode"`
			DependsOn     []string `json:"depends_on"`
		} `json:"tasks"`
	}
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

	// Map title → task ID for resolving depends_on by title
	titleToID := map[string]string{}
	now := time.Now().UTC().Format(time.RFC3339)
	created := make([]teamTask, 0, len(body.Tasks))

	for _, item := range body.Tasks {
		taskChannel := b.preferredTaskChannelLocked(channel, createdBy, item.Assignee, item.Title, item.Details)
		if b.findChannelLocked(taskChannel) == nil {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		// Authorize on the resolved task channel, not body.Channel — the
		// body channel is just a default and the planner may route the
		// task to a different channel where the assignee actually lives.
		// Without this gate any authenticated caller could plant tasks in
		// channels they aren't a member of by spoofing the body channel.
		if !b.canAccessChannelLocked(createdBy, taskChannel) {
			http.Error(w, "channel access denied", http.StatusForbidden)
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
			existing.DependsOn = resolvedDeps
			if len(existing.DependsOn) > 0 && b.hasUnresolvedDepsLocked(existing) {
				existing.Blocked = true
				existing.Status = "open"
			} else if strings.TrimSpace(existing.Owner) != "" {
				existing.Blocked = false
				existing.Status = "in_progress"
			}
			syncTaskMemoryWorkflow(existing, now)
			b.ensureTaskOwnerChannelMembershipLocked(taskChannel, existing.Owner)
			b.queueTaskBehindActiveOwnerLaneLocked(existing)
			existing.UpdatedAt = now
			if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
				return
			}
			b.appendActionLocked("task_updated", "office", taskChannel, createdBy, truncateSummary(existing.Title+" ["+existing.Status+"]", 140), existing.ID)
			created = append(created, *existing)
			continue
		}

		b.counter++
		taskID := fmt.Sprintf("task-%d", b.counter)
		titleToID[strings.TrimSpace(item.Title)] = taskID

		task := teamTask{
			ID:            taskID,
			Channel:       taskChannel,
			Title:         strings.TrimSpace(item.Title),
			Details:       strings.TrimSpace(item.Details),
			Owner:         strings.TrimSpace(item.Assignee),
			Status:        "open",
			CreatedBy:     createdBy,
			TaskType:      strings.TrimSpace(item.TaskType),
			ExecutionMode: strings.TrimSpace(item.ExecutionMode),
			DependsOn:     resolvedDeps,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if task.Owner != "" && len(resolvedDeps) == 0 {
			task.Status = "in_progress"
		}
		if len(resolvedDeps) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.Blocked = true
		}
		syncTaskMemoryWorkflow(&task, now)
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&task)
		if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		b.scheduleTaskLifecycleLocked(&task)
		if err := b.syncTaskWorktreeLocked(&task); err != nil {
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.tasks = append(b.tasks, task)
		b.appendActionLocked("task_created", "office", taskChannel, createdBy, truncateSummary(task.Title, 140), task.ID)
		created = append(created, task)
	}

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": created})
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
	ReviewState      string
	SourceSignalID   string
	SourceDecisionID string
	DependsOn        []string
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
			existing.Status = "in_progress"
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
		if existing.ReviewState == "" && strings.TrimSpace(input.ReviewState) != "" {
			existing.ReviewState = strings.TrimSpace(input.ReviewState)
		}
		if existing.SourceSignalID == "" && strings.TrimSpace(input.SourceSignalID) != "" {
			existing.SourceSignalID = strings.TrimSpace(input.SourceSignalID)
		}
		if existing.SourceDecisionID == "" && strings.TrimSpace(input.SourceDecisionID) != "" {
			existing.SourceDecisionID = strings.TrimSpace(input.SourceDecisionID)
		}
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
		return *existing, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	task := teamTask{
		ID:               fmt.Sprintf("task-%d", b.counter),
		Channel:          channel,
		Title:            title,
		Details:          strings.TrimSpace(input.Details),
		Owner:            strings.TrimSpace(input.Owner),
		Status:           "open",
		CreatedBy:        strings.TrimSpace(input.CreatedBy),
		ThreadID:         strings.TrimSpace(input.ThreadID),
		TaskType:         strings.TrimSpace(input.TaskType),
		PipelineID:       strings.TrimSpace(input.PipelineID),
		ExecutionMode:    strings.TrimSpace(input.ExecutionMode),
		ReviewState:      strings.TrimSpace(input.ReviewState),
		SourceSignalID:   strings.TrimSpace(input.SourceSignalID),
		SourceDecisionID: strings.TrimSpace(input.SourceDecisionID),
		DependsOn:        input.DependsOn,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
		task.Blocked = true
	} else if task.Owner != "" {
		task.Status = "in_progress"
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
	b.tasks = append(b.tasks, task)
	b.appendActionWithRefsLocked("task_created", "office", channel, input.CreatedBy, truncateSummary(task.Title, 140), task.ID, compactStringList([]string{task.SourceSignalID}), task.SourceDecisionID)
	if err := b.saveLocked(); err != nil {
		return teamTask{}, false, err
	}
	return task, false, nil
}
