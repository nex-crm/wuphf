package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

func taskNeedsLocalWorktree(task *teamTask) bool {
	if task == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	if strings.TrimSpace(task.Owner) == "" {
		return false
	}
	switch strings.TrimSpace(task.Status) {
	case "", "open":
		return false
	case "done":
		return strings.TrimSpace(task.WorktreePath) != "" || strings.TrimSpace(task.WorktreeBranch) != ""
	default:
		return true
	}
}

func taskBlockReasonLooksLikeWorkspaceWriteIssue(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	markers := []string{
		"read-only",
		"read only",
		"writable workspace",
		"write access",
		"filesystem sandbox",
		"workspace sandbox",
		"operation not permitted",
		"permission denied",
	}
	for _, marker := range markers {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

func rejectFalseLocalWorktreeBlock(task *teamTask, reason string) error {
	if task == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return nil
	}
	if !taskBlockReasonLooksLikeWorkspaceWriteIssue(reason) {
		return nil
	}
	worktreePath := strings.TrimSpace(task.WorktreePath)
	if worktreePath == "" {
		return nil
	}
	if err := verifyTaskWorktreeWritable(worktreePath); err == nil {
		return fmt.Errorf("assigned local worktree is writable at %s; do not request writable-workspace approval; continue implementation in that worktree", worktreePath)
	}
	return nil
}

func taskRequiresExclusiveOwnerTurn(task *teamTask) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.Owner) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(task.ExecutionMode)) {
	case "local_worktree", "live_external":
		return true
	default:
		return false
	}
}

func taskStatusConsumesExclusiveOwnerTurn(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_progress", "review":
		return true
	default:
		return false
	}
}

func taskChannelCandidateOwnerAllowed(ch *teamChannel, owner string) bool {
	if ch == nil {
		return false
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return true
	}
	return stringSliceContainsFold(ch.Members, owner) || strings.EqualFold(strings.TrimSpace(ch.CreatedBy), owner)
}

func (b *Broker) syncTaskWorktreeLocked(task *teamTask) error {
	if task == nil {
		return nil
	}
	// Automatically assign local_worktree mode when a coding agent claims a task.
	if task.ExecutionMode == "" && codingAgentSlugs[strings.TrimSpace(task.Owner)] {
		switch strings.TrimSpace(task.Status) {
		case "", "open", "done":
			// not yet in-progress; leave mode unset
		default:
			task.ExecutionMode = "local_worktree"
		}
	}
	if taskNeedsLocalWorktree(task) {
		if strings.TrimSpace(task.WorktreePath) != "" && strings.TrimSpace(task.WorktreeBranch) != "" {
			if taskWorktreeSourceLooksUsable(task.WorktreePath) {
				return nil
			}
			if err := cleanupTaskWorktree(task.WorktreePath, task.WorktreeBranch); err != nil {
				return err
			}
			task.WorktreePath = ""
			task.WorktreeBranch = ""
		}
		if path, branch := b.reusableDependencyWorktreeLocked(task); path != "" && branch != "" {
			task.WorktreePath = path
			task.WorktreeBranch = branch
			return nil
		}
		path, branch, err := prepareTaskWorktree(task.ID)
		if err != nil {
			return err
		}
		task.WorktreePath = path
		task.WorktreeBranch = branch
		return nil
	}

	if strings.TrimSpace(task.WorktreePath) == "" && strings.TrimSpace(task.WorktreeBranch) == "" {
		return nil
	}
	if err := cleanupTaskWorktree(task.WorktreePath, task.WorktreeBranch); err != nil {
		return err
	}
	task.WorktreePath = ""
	task.WorktreeBranch = ""
	return nil
}

func (b *Broker) reusableDependencyWorktreeLocked(task *teamTask) (string, string) {
	if b == nil || task == nil || len(task.DependsOn) == 0 {
		return "", ""
	}
	owner := strings.TrimSpace(task.Owner)
	var fallbackPath string
	var fallbackBranch string
	for _, depID := range task.DependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		for i := range b.tasks {
			dep := &b.tasks[i]
			if strings.TrimSpace(dep.ID) != depID {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(dep.ExecutionMode), "local_worktree") {
				continue
			}
			path := strings.TrimSpace(dep.WorktreePath)
			branch := strings.TrimSpace(dep.WorktreeBranch)
			if path == "" || branch == "" {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(dep.Status))
			review := strings.ToLower(strings.TrimSpace(dep.ReviewState))
			if status != "review" && status != "done" && review != "ready_for_review" && review != "approved" {
				continue
			}
			if owner != "" && strings.TrimSpace(dep.Owner) == owner {
				return path, branch
			}
			if fallbackPath == "" && fallbackBranch == "" {
				fallbackPath = path
				fallbackBranch = branch
			}
		}
	}
	return fallbackPath, fallbackBranch
}

func (b *Broker) activeExclusiveOwnerTaskLocked(owner, excludeTaskID string) *teamTask {
	owner = strings.TrimSpace(owner)
	excludeTaskID = strings.TrimSpace(excludeTaskID)
	if b == nil || owner == "" {
		return nil
	}
	for i := range b.tasks {
		task := &b.tasks[i]
		if excludeTaskID != "" && strings.TrimSpace(task.ID) == excludeTaskID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(task.Owner), owner) {
			continue
		}
		if !taskRequiresExclusiveOwnerTurn(task) {
			continue
		}
		if !taskStatusConsumesExclusiveOwnerTurn(task.Status) {
			continue
		}
		return task
	}
	return nil
}

func (b *Broker) queueTaskBehindActiveOwnerLaneLocked(task *teamTask) {
	if b == nil || task == nil {
		return
	}
	if !taskRequiresExclusiveOwnerTurn(task) {
		return
	}
	if !taskStatusConsumesExclusiveOwnerTurn(task.Status) {
		return
	}
	active := b.activeExclusiveOwnerTaskLocked(task.Owner, task.ID)
	if active == nil {
		return
	}
	if !stringSliceContainsFold(task.DependsOn, active.ID) {
		task.DependsOn = append(task.DependsOn, active.ID)
	}
	task.Blocked = true
	task.Status = "open"
	queueNote := fmt.Sprintf("Queued behind %s so @%s only carries one active %s lane at a time.", active.ID, strings.TrimSpace(task.Owner), strings.TrimSpace(task.ExecutionMode))
	switch existing := strings.TrimSpace(task.Details); {
	case existing == "":
		task.Details = queueNote
	case !strings.Contains(existing, queueNote):
		task.Details = existing + "\n\n" + queueNote
	}
}

func (b *Broker) preferredTaskChannelLocked(requestedChannel, createdBy, owner, title, details string) string {
	channel := normalizeChannelSlug(requestedChannel)
	if channel == "" {
		channel = "general"
	}
	if channel != "general" || b == nil {
		return channel
	}
	createdBy = strings.TrimSpace(createdBy)
	if createdBy == "" {
		return channel
	}
	probe := teamTask{
		Channel: channel,
		Owner:   strings.TrimSpace(owner),
		Title:   strings.TrimSpace(title),
		Details: strings.TrimSpace(details),
	}
	if !taskLooksLikeLiveBusinessObjective(&probe) {
		return channel
	}
	now := time.Now().UTC()
	var best *teamChannel
	var bestCreated time.Time
	for i := range b.channels {
		ch := &b.channels[i]
		slug := normalizeChannelSlug(ch.Slug)
		if slug == "" || slug == "general" || ch.isDM() {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(ch.CreatedBy), createdBy) {
			continue
		}
		if !taskChannelCandidateOwnerAllowed(ch, owner) {
			continue
		}
		createdAt := parseBrokerTimestamp(ch.CreatedAt)
		if !createdAt.IsZero() && now.Sub(createdAt) > 20*time.Minute {
			continue
		}
		if best == nil || (!createdAt.IsZero() && createdAt.After(bestCreated)) {
			best = ch
			bestCreated = createdAt
		}
	}
	if best == nil {
		return channel
	}
	return normalizeChannelSlug(best.Slug)
}

func (b *Broker) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetTasks(w, r)
	case http.MethodPost:
		b.handlePostTask(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	root := b.agentLogRoot
	b.mu.Unlock()
	if root == "" {
		root = agent.DefaultTaskLogRoot()
	}

	task := strings.TrimSpace(r.URL.Query().Get("task"))
	if task != "" {
		// Guard against path traversal — the task id is a single directory name.
		if strings.Contains(task, "..") || strings.ContainsAny(task, `/\`) {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		entries, err := agent.ReadTaskLog(root, task)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task":    task,
			"entries": entries,
		})
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	tasks, err := agent.ListRecentTasks(root, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
}

func (b *Broker) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	mySlug := strings.TrimSpace(r.URL.Query().Get("my_slug"))
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	allChannels := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all_channels")), "true")
	if channel == "" && !allChannels {
		channel = "general"
	}
	includeDone := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_done")), "true")

	b.mu.Lock()
	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	result := make([]teamTask, 0, len(b.tasks))
	// allChannels=true must NOT bypass channel authorization. Without this
	// per-task check, an authenticated viewer could enumerate every task in
	// every channel — including private ones they aren't a member of —
	// just by passing all_channels=true. Apply the same access predicate
	// to each candidate channel before letting the task into the response.
	allChannelsCache := make(map[string]bool)
	channelAllowed := func(slug string) bool {
		if !allChannels {
			return true
		}
		if v, ok := allChannelsCache[slug]; ok {
			return v
		}
		v := b.canAccessChannelLocked(viewerSlug, slug)
		allChannelsCache[slug] = v
		return v
	}
	for _, task := range b.tasks {
		taskChannel := normalizeChannelSlug(task.Channel)
		if !allChannels && taskChannel != channel {
			continue
		}
		if !channelAllowed(taskChannel) {
			continue
		}
		if task.Status == "done" && !includeDone && statusFilter == "" {
			continue
		}
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if mySlug != "" && task.Owner != "" && task.Owner != mySlug {
			continue
		}
		result = append(result, task)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "tasks": result})
}

func (b *Broker) handlePostTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action           string   `json:"action"`
		Channel          string   `json:"channel"`
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		Details          string   `json:"details"`
		Owner            string   `json:"owner"`
		CreatedBy        string   `json:"created_by"`
		ThreadID         string   `json:"thread_id"`
		TaskType         string   `json:"task_type"`
		PipelineID       string   `json:"pipeline_id"`
		ExecutionMode    string   `json:"execution_mode"`
		ReviewState      string   `json:"review_state"`
		SourceSignalID   string   `json:"source_signal_id"`
		SourceDecisionID string   `json:"source_decision_id"`
		WorktreePath     string   `json:"worktree_path"`
		WorktreeBranch   string   `json:"worktree_branch"`
		DependsOn        []string `json:"depends_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(body.Action)
	now := time.Now().UTC().Format(time.RFC3339)
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
	if !b.canAccessChannelLocked(body.CreatedBy, channel) {
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}

	if action == "create" {
		if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.CreatedBy) == "" {
			http.Error(w, "title and created_by required", http.StatusBadRequest)
			return
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
			b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
			existing.UpdatedAt = now
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
				return
			}
			b.appendActionLocked("task_updated", "office", channel, strings.TrimSpace(body.CreatedBy), truncateSummary(existing.Title+" ["+existing.Status+"]", 140), existing.ID)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"task": *existing})
			return
		}
		b.counter++
		task := teamTask{
			ID:               fmt.Sprintf("task-%d", b.counter),
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			Details:          strings.TrimSpace(body.Details),
			Owner:            strings.TrimSpace(body.Owner),
			Status:           "open",
			CreatedBy:        strings.TrimSpace(body.CreatedBy),
			ThreadID:         strings.TrimSpace(body.ThreadID),
			TaskType:         strings.TrimSpace(body.TaskType),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			ExecutionMode:    strings.TrimSpace(body.ExecutionMode),
			ReviewState:      strings.TrimSpace(body.ReviewState),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
			WorktreePath:     strings.TrimSpace(body.WorktreePath),
			WorktreeBranch:   strings.TrimSpace(body.WorktreeBranch),
			DependsOn:        body.DependsOn,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.Blocked = true
		} else if task.Owner != "" {
			task.Status = "in_progress"
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
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
		b.appendActionLocked("task_created", "office", channel, task.CreatedBy, truncateSummary(task.Title, 140), task.ID)
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
		return
	}

	requestedID := strings.TrimSpace(body.ID)
	for i := range b.tasks {
		if b.tasks[i].ID != requestedID {
			continue
		}
		task := &b.tasks[i]
		taskChannel := normalizeChannelSlug(task.Channel)
		// Authorize against the task's ACTUAL channel, not the channel the
		// caller put in the body. Without this, a viewer with access to
		// any channel could mutate any task ID by spoofing body.Channel
		// to a channel they're allowed in.
		if taskChannel != "" && taskChannel != channel {
			if !b.canAccessChannelLocked(body.CreatedBy, taskChannel) {
				http.Error(w, "channel access denied", http.StatusForbidden)
				return
			}
		}
		appendDetails := false
		reassignPrevOwner := ""
		reassignTriggered := false
		cancelTriggered := false
		cancelPrevOwner := ""
		switch action {
		case "claim", "assign":
			if strings.TrimSpace(body.Owner) == "" {
				http.Error(w, "owner required", http.StatusBadRequest)
				return
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
				http.Error(w, "owner required", http.StatusBadRequest)
				return
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
				task.Status = "done"
				if taskNeedsStructuredReview(task) {
					task.ReviewState = "approved"
				}
				task.Blocked = false
			} else if taskNeedsStructuredReview(task) {
				task.Status = "review"
				task.ReviewState = "ready_for_review"
			} else {
				task.Status = "done"
			}
		case "review":
			task.Status = "review"
			task.ReviewState = "ready_for_review"
		case "approve":
			task.Status = "done"
			if taskNeedsStructuredReview(task) {
				task.ReviewState = "approved"
			}
		case "block":
			if err := rejectFalseLocalWorktreeBlock(task, body.Details); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
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
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Details) != "" {
			if appendDetails {
				if err := appendTaskDetailLocked(task, body.Details); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
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
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		// Any terminal status releases waiting dependents. Previously
		// only "done" fired this; cancelling a parent silently orphaned
		// every dependent. isTerminalTeamTaskStatus matches the same
		// done/completed/canceled/cancelled set hasUnresolvedDepsLocked
		// treats as resolved, so the two stay symmetric.
		if isTerminalTeamTaskStatus(task.Status) {
			b.unblockDependentsLocked(task.ID)
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.appendActionLocked("task_updated", "office", taskChannel, strings.TrimSpace(body.CreatedBy), truncateSummary(task.Title+" ["+task.Status+"]", 140), task.ID)
		if action == "block" {
			b.requestCapabilitySelfHealingLocked(task, strings.TrimSpace(body.CreatedBy), body.Details)
		}
		if reassignTriggered {
			b.postTaskReassignNotificationsLocked(strings.TrimSpace(body.CreatedBy), task, reassignPrevOwner)
		}
		if cancelTriggered {
			b.postTaskCancelNotificationsLocked(strings.TrimSpace(body.CreatedBy), task, cancelPrevOwner)
		}
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"task": *task})
		return
	}

	http.Error(w, "task not found", http.StatusNotFound)
}

// postTaskReassignNotificationsLocked posts the channel announcement plus DMs
// to the new owner and previous owner whenever a task ownership change happens.
// The CEO is tagged in the channel message rather than DM'd (CEO is the human
// user; human↔ceo self-DM is not a valid DM target).
//
// Must be called while b.mu is held for write.
func (b *Broker) postTaskReassignNotificationsLocked(actor string, task *teamTask, prevOwner string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	newOwner := strings.TrimSpace(task.Owner)
	prevOwner = strings.TrimSpace(prevOwner)
	if newOwner == prevOwner {
		return
	}
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	now := time.Now().UTC().Format(time.RFC3339)

	newLabel := "(unassigned)"
	if newOwner != "" {
		newLabel = "@" + newOwner
	}
	prevLabel := "(unassigned)"
	if prevOwner != "" {
		prevLabel = "@" + prevOwner
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_reassigned",
		Title:     title,
		Content:   fmt.Sprintf("Task %q reassigned: %s → %s. (by @%s, cc @ceo)", title, prevLabel, newLabel, actor),
		Tagged:    dedupeReassignTags([]string{"ceo", newOwner, prevOwner}),
		Timestamp: now,
	})

	if isDMTargetSlug(newOwner) {
		b.postTaskDMLocked(actor, newOwner, "task_reassigned", title,
			fmt.Sprintf("Task %q is yours now. Details live in #%s.", title, taskChannel))
	}
	if isDMTargetSlug(prevOwner) && prevOwner != newOwner {
		b.postTaskDMLocked(actor, prevOwner, "task_reassigned", title,
			fmt.Sprintf("Task %q is off your plate — it moved to %s.", title, newLabel))
	}
}

// postTaskCancelNotificationsLocked posts a channel announcement plus a DM
// to the (previous) owner whenever a task is closed as "won't do".
// Must be called while b.mu is held for write.
func (b *Broker) postTaskCancelNotificationsLocked(actor string, task *teamTask, prevOwner string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	prevOwner = strings.TrimSpace(prevOwner)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	now := time.Now().UTC().Format(time.RFC3339)

	ownerLabel := "(no owner)"
	if prevOwner != "" {
		ownerLabel = "@" + prevOwner
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_canceled",
		Title:     title,
		Content:   fmt.Sprintf("Task %q closed as won't do. Owner was %s. (by @%s, cc @ceo)", title, ownerLabel, actor),
		Tagged:    dedupeReassignTags([]string{"ceo", prevOwner}),
		Timestamp: now,
	})

	if isDMTargetSlug(prevOwner) {
		b.postTaskDMLocked(actor, prevOwner, "task_canceled", title,
			fmt.Sprintf("Heads up — task %q was closed as won't do. Take it off your list.", title))
	}
}

// postTaskDMLocked appends a direct-message notification to the DM channel
// between "human" and targetSlug, creating the channel if necessary.
// Must be called while b.mu is held for write.
func (b *Broker) postTaskDMLocked(from, targetSlug, kind, title, content string) {
	targetSlug = strings.TrimSpace(targetSlug)
	if targetSlug == "" || b.channelStore == nil {
		return
	}
	ch, err := b.channelStore.GetOrCreateDirect("human", targetSlug)
	if err != nil {
		return
	}
	if b.findChannelLocked(ch.Slug) == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		b.channels = append(b.channels, teamChannel{
			Slug:        ch.Slug,
			Name:        ch.Slug,
			Type:        "dm",
			Description: "Direct messages with " + targetSlug,
			Members:     []string{"human", targetSlug},
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      strings.TrimSpace(from),
		Channel:   ch.Slug,
		Kind:      strings.TrimSpace(kind),
		Title:     title,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// isDMTargetSlug reports whether slug is a valid recipient for a human-to-agent DM.
// The human user ("human"/"you") and the CEO seat ("ceo", which is the human)
// are excluded because they would create self-DMs.
func isDMTargetSlug(slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return false
	}
	switch slug {
	case "human", "you", "ceo":
		return false
	}
	return true
}

func dedupeReassignTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

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
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "done" || status == "completed" || status == "canceled" || status == "cancelled" {
			return *task, false, nil
		}
		if err := rejectFalseLocalWorktreeBlock(task, reason); err != nil {
			return *task, false, err
		}
		if reason != "" {
			switch existing := strings.TrimSpace(task.Details); {
			case existing == "":
				task.Details = reason
			case !strings.Contains(existing, reason):
				task.Details = existing + "\n\n" + reason
			}
		}
		task.Status = "blocked"
		task.Blocked = true
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			return *task, false, err
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			return teamTask{}, false, err
		}
		b.appendActionLocked("task_updated", "office", normalizeChannelSlug(task.Channel), actor, truncateSummary(task.Title+" ["+task.Status+"]", 140), task.ID)
		b.requestCapabilitySelfHealingLocked(task, actor, reason)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
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
		changed := false
		if task.Blocked {
			task.Blocked = false
			changed = true
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "blocked") {
			if strings.TrimSpace(task.Owner) != "" {
				task.Status = "in_progress"
			} else {
				task.Status = "open"
			}
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
		return *task, true, nil
	}

	return teamTask{}, false, fmt.Errorf("task not found")
}

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

func (b *Broker) handleMemory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		keyFilter := strings.TrimSpace(r.URL.Query().Get("key"))
		limit := 5
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		// Snapshot under the lock: `mem := b.sharedMemory` only copies the
		// map header, leaving readers below racing concurrent POST writes
		// for the same outer/inner maps. searchPrivateMemory iterates the
		// entry maps and json.Encoder serializes the whole tree, both of
		// which can panic with "concurrent map iteration and map write".
		b.mu.Lock()
		mem := make(map[string]map[string]string, len(b.sharedMemory))
		for ns, entries := range b.sharedMemory {
			cloned := make(map[string]string, len(entries))
			for k, v := range entries {
				cloned[k] = v
			}
			mem[ns] = cloned
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if namespace != "" {
			entries := mem[namespace]
			switch {
			case keyFilter != "":
				var payload []brokerMemoryEntry
				if raw, ok := entries[keyFilter]; ok {
					payload = append(payload, brokerEntryFromNote(decodePrivateMemoryNote(keyFilter, raw)))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			case query != "":
				matches := searchPrivateMemory(entries, query, limit)
				payload := make([]brokerMemoryEntry, 0, len(matches))
				for _, note := range matches {
					payload = append(payload, brokerEntryFromNote(note))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			default:
				matches := searchPrivateMemory(entries, "", len(entries))
				payload := make([]brokerMemoryEntry, 0, len(matches))
				for _, note := range matches {
					payload = append(payload, brokerEntryFromNote(note))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"memory": mem})
	case http.MethodPost:
		var body struct {
			Namespace string `json:"namespace"`
			Key       string `json:"key"`
			Value     any    `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ns := strings.TrimSpace(body.Namespace)
		key := strings.TrimSpace(body.Key)
		if ns == "" || key == "" {
			http.Error(w, "namespace and key required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		if b.sharedMemory == nil {
			b.sharedMemory = make(map[string]map[string]string)
		}
		if b.sharedMemory[ns] == nil {
			b.sharedMemory[ns] = make(map[string]string)
		}
		value := ""
		switch typed := body.Value.(type) {
		case string:
			value = typed
		default:
			data, err := json.Marshal(typed)
			if err != nil {
				b.mu.Unlock()
				http.Error(w, "invalid value", http.StatusBadRequest)
				return
			}
			value = string(data)
		}
		b.sharedMemory[ns][key] = value
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "namespace": ns, "key": key})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleTaskAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
		Slug    string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	taskID := strings.TrimSpace(body.ID)
	slug := strings.TrimSpace(body.Slug)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	if taskID == "" || slug == "" {
		http.Error(w, "id and slug required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == taskID && normalizeChannelSlug(b.tasks[i].Channel) == channel {
			if b.tasks[i].Owner != slug {
				http.Error(w, "only the task owner can ack", http.StatusForbidden)
				return
			}
			now := time.Now().UTC().Format(time.RFC3339)
			b.tasks[i].AckedAt = now
			b.tasks[i].UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"task": b.tasks[i]})
			return
		}
	}
	http.Error(w, "task not found", http.StatusNotFound)
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
		if existing.Details == "" && strings.TrimSpace(details) != "" {
			existing.Details = strings.TrimSpace(details)
		}
		if existing.Owner == "" && strings.TrimSpace(owner) != "" {
			existing.Owner = strings.TrimSpace(owner)
			if !existing.Blocked {
				existing.Status = "in_progress"
			}
		}
		if existing.ThreadID == "" && strings.TrimSpace(threadID) != "" {
			existing.ThreadID = strings.TrimSpace(threadID)
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
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
		ID:        fmt.Sprintf("task-%d", b.counter),
		Channel:   channel,
		Title:     title,
		Details:   strings.TrimSpace(details),
		Owner:     strings.TrimSpace(owner),
		Status:    "open",
		CreatedBy: strings.TrimSpace(createdBy),
		ThreadID:  strings.TrimSpace(threadID),
		DependsOn: dependsOn,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
		task.Blocked = true
	} else if task.Owner != "" {
		task.Status = "in_progress"
	}
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
	b.appendActionLocked("task_created", "office", channel, createdBy, truncateSummary(task.Title, 140), task.ID)
	if err := b.saveLocked(); err != nil {
		return teamTask{}, false, err
	}
	return task, false, nil
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
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
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
				if !isTerminalTeamTaskStatus(b.tasks[j].Status) {
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
func (b *Broker) unblockDependentsLocked(completedTaskID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range b.tasks {
		if !b.tasks[i].Blocked {
			continue
		}
		hasDep := false
		for _, depID := range b.tasks[i].DependsOn {
			if depID == completedTaskID {
				hasDep = true
				break
			}
		}
		if !hasDep {
			continue
		}
		if !b.hasUnresolvedDepsLocked(&b.tasks[i]) {
			b.tasks[i].Blocked = false
			if strings.TrimSpace(b.tasks[i].Owner) != "" {
				b.tasks[i].Status = "in_progress"
			} else {
				b.tasks[i].Status = "open"
			}
			b.queueTaskBehindActiveOwnerLaneLocked(&b.tasks[i])
			b.tasks[i].UpdatedAt = now
			b.scheduleTaskLifecycleLocked(&b.tasks[i])
			_ = b.syncTaskWorktreeLocked(&b.tasks[i])
			b.appendActionLocked(
				"task_unblocked",
				"office",
				normalizeChannelSlug(b.tasks[i].Channel),
				"system",
				truncateSummary(b.tasks[i].Title+" unblocked by "+completedTaskID, 140),
				b.tasks[i].ID,
			)
		}
	}
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
	return strings.TrimSpace(m.SourceSignalID) != "" ||
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
	if match.PipelineID != "" && strings.TrimSpace(task.PipelineID) != "" && strings.TrimSpace(task.PipelineID) != match.PipelineID {
		return false
	}
	if match.SourceSignalID != "" && strings.TrimSpace(task.SourceSignalID) != match.SourceSignalID {
		return false
	}
	if match.SourceDecisionID != "" && strings.TrimSpace(task.SourceDecisionID) != match.SourceDecisionID {
		return false
	}
	return true
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
		if isTerminalTeamTaskStatus(task.Status) {
			continue
		}
		sameTitle := title != "" && strings.EqualFold(strings.TrimSpace(task.Title), title)
		if threadID != "" && strings.TrimSpace(task.ThreadID) == threadID {
			if sameTitle && taskOwnerMatches(task, owner) {
				taskHasScopedIdentity := hasScopedTaskIdentity(task)
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
		taskHasScopedIdentity := hasScopedTaskIdentity(task)
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
