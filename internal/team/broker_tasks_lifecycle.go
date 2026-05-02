package team

import (
	"encoding/json"
	"fmt"
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
		now := time.Now().UTC().Format(time.RFC3339)
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
	b.appendActionLocked("task_created", "office", channel, createdBy, truncateSummary(task.Title, 140), task.ID)
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
		if isTerminalTeamTaskStatus(task.Status) {
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
