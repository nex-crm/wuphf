package team

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

const selfHealingTaskTitlePrefix = "Self-heal "

func (l *Launcher) requestSelfHealing(agentSlug, taskID string, reason agent.EscalationReason, detail string) (teamTask, bool, error) {
	if l == nil || l.broker == nil {
		return teamTask{}, false, nil
	}
	return l.broker.RequestSelfHealing(agentSlug, taskID, reason, detail)
}

func (b *Broker) RequestSelfHealing(agentSlug, taskID string, reason agent.EscalationReason, detail string) (teamTask, bool, error) {
	if b == nil {
		return teamTask{}, false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requestSelfHealingLocked(agentSlug, taskID, reason, detail)
}

func (b *Broker) requestSelfHealingLocked(agentSlug, taskID string, reason agent.EscalationReason, detail string) (teamTask, bool, error) {
	agentSlug = strings.TrimSpace(agentSlug)
	taskID = strings.TrimSpace(taskID)
	if b.isSelfHealingTaskIDLocked(taskID) {
		return teamTask{}, true, nil
	}

	owner := strings.TrimSpace(officeLeadSlugFrom(b.members))
	if owner == "" {
		owner = agentSlug
	}
	title := selfHealingTaskTitle(agentSlug, taskID)
	details := selfHealingTaskDetails(agentSlug, taskID, reason, detail)
	createdBy := selfHealingCreatedByForMode(b.sessionMode)
	channel := b.preferredTaskChannelLocked("general", createdBy, owner, title, details)
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(createdBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}

	if existing := b.findReusableTaskLocked(taskReuseMatch{
		Channel:    channel,
		Title:      title,
		Owner:      owner,
		PipelineID: "incident",
	}); existing != nil {
		if existing.Details == "" {
			existing.Details = details
		} else if err := appendTaskDetailLocked(existing, selfHealingIncidentUpdate(reason, detail)); err != nil {
			return teamTask{}, true, err
		}
		if existing.Owner == "" && owner != "" {
			existing.Owner = owner
			existing.Status = "in_progress"
		}
		if existing.TaskType == "" {
			existing.TaskType = "incident"
		}
		if existing.PipelineID == "" {
			existing.PipelineID = "incident"
		}
		if existing.ExecutionMode == "" {
			existing.ExecutionMode = "office"
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.queueTaskBehindActiveOwnerLaneLocked(existing)
		if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
			return teamTask{}, true, err
		}
		b.scheduleTaskLifecycleLocked(existing)
		if err := b.syncTaskWorktreeLocked(existing); err != nil {
			return teamTask{}, true, err
		}
		b.appendActionLocked("task_updated", "office", channel, createdBy, truncateSummary(existing.Title+" [updated]", 140), existing.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, true, err
		}
		return *existing, true, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	task := teamTask{
		ID:            fmt.Sprintf("task-%d", b.counter),
		Channel:       channel,
		Title:         title,
		Details:       details,
		Owner:         owner,
		Status:        "open",
		CreatedBy:     createdBy,
		TaskType:      "incident",
		PipelineID:    "incident",
		ExecutionMode: "office",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if task.Owner != "" {
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

func (b *Broker) requestCapabilitySelfHealingLocked(blockedTask *teamTask, actor, detail string) {
	if blockedTask == nil || !isCapabilityGapBlocker(detail) || isSelfHealingTaskTitle(blockedTask.Title) {
		return
	}
	agentSlug := strings.TrimSpace(actor)
	if agentSlug == "" || agentSlug == "system" {
		agentSlug = strings.TrimSpace(blockedTask.Owner)
	}
	if agentSlug == "" {
		agentSlug = "agent"
	}
	if _, _, err := b.requestSelfHealingLocked(agentSlug, blockedTask.ID, agent.EscalationCapabilityGap, detail); err != nil {
		log.Printf("self-healing: create capability repair task for agent=%s task=%s: %v", agentSlug, blockedTask.ID, err)
	}
}

func isCapabilityGapBlocker(detail string) bool {
	text := strings.ToLower(strings.TrimSpace(detail))
	if text == "" {
		return false
	}
	if strings.Contains(text, "capability gap") || strings.Contains(text, "missing capability") {
		return true
	}
	capabilityTerms := []string{
		"specialist", "channel", "skill", "playbook", "tool", "provider", "integration",
		"workflow", "action", "api", "connection", "connector", "credential", "credentials",
		"permission", "access", "account", "runtime", "session",
	}
	positiveSignals := []string{
		"missing", "no ", "not connected", "not configured", "not available", "unavailable",
		"unsupported", "can't", "cannot", "unable", "need", "needs", "requires", "require",
	}
	for _, term := range capabilityTerms {
		if !strings.Contains(text, term) {
			continue
		}
		for _, signal := range positiveSignals {
			if strings.Contains(text, signal) {
				return true
			}
		}
		if strings.Contains(text, "tool path") || strings.Contains(text, "provider gap") || strings.Contains(text, "integration path") {
			return true
		}
	}
	return false
}

func (l *Launcher) selfHealingCreatedBy() string {
	if l == nil {
		return "system"
	}
	return selfHealingCreatedByForMode(l.sessionMode)
}

func selfHealingCreatedByForMode(mode string) string {
	if NormalizeSessionMode(mode) == SessionModeOneOnOne {
		return "you"
	}
	return "system"
}

func (l *Launcher) isSelfHealingTaskID(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || l == nil || l.broker == nil {
		return false
	}
	return l.broker.isSelfHealingTaskID(taskID)
}

func (b *Broker) isSelfHealingTaskID(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.isSelfHealingTaskIDLocked(taskID)
}

func (b *Broker) isSelfHealingTaskIDLocked(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || b == nil {
		return false
	}
	for _, task := range b.tasks {
		if strings.TrimSpace(task.ID) != taskID {
			continue
		}
		return isSelfHealingTaskTitle(task.Title)
	}
	return false
}

func isSelfHealingTaskTitle(title string) bool {
	return strings.HasPrefix(strings.TrimSpace(title), selfHealingTaskTitlePrefix)
}

func selfHealingTaskTitle(agentSlug, taskID string) string {
	who := strings.TrimSpace(agentSlug)
	if who == "" {
		who = "agent"
	}
	if taskID = strings.TrimSpace(taskID); taskID != "" {
		return fmt.Sprintf("%s@%s on %s", selfHealingTaskTitlePrefix, who, taskID)
	}
	return fmt.Sprintf("%s@%s runtime failure", selfHealingTaskTitlePrefix, who)
}

func selfHealingTaskDetails(agentSlug, taskID string, reason agent.EscalationReason, detail string) string {
	who := strings.TrimSpace(agentSlug)
	if who == "" {
		who = "unknown"
	}
	originalTask := strings.TrimSpace(taskID)
	if originalTask == "" {
		originalTask = "unknown"
	}
	trigger := strings.TrimSpace(string(reason))
	if trigger == "" {
		trigger = "unknown"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "no detail provided"
	}

	return strings.Join([]string{
		"Automatic self-healing incident.",
		"",
		fmt.Sprintf("- Agent: @%s", who),
		fmt.Sprintf("- Original task: %s", originalTask),
		fmt.Sprintf("- Trigger: %s", trigger),
		fmt.Sprintf("- Detail: %s", detail),
		"",
		"Repair loop:",
		"1. Inspect the failed task and recent thread context. Use the pushed packet as authoritative; call team_poll or team_tasks only if context is missing.",
		"2. Classify the blocker: missing specialist/channel, missing or outdated skill/playbook, missing tool/provider/integration, stale runtime/session, unclear human decision, or implementation bug.",
		"3. Take the smallest reversible repair in office state. Prefer a bounded refresh/retry/requeue, reassignment, capability-check step, specialist/channel creation, skill proposal, playbook update, or exact human question before broad process changes.",
		"4. If runtime/tool state looks stale, refresh or reconnect once and verify with a cheap health check before treating it as a human blocker.",
		"5. Repair the missing capability first, then resume or requeue the original workflow with a concrete verification step. A self-heal that only reports the blocker is incomplete.",
		"6. Treat learning as a post-repair review: propose a skill or update a wiki/playbook only when the workaround is durable and reusable. Include the trigger, failure signature, recovery step, verification signal, and any tool/provider/channel constraints. If nothing reusable was learned, leave skills unchanged.",
		"7. Do not mark this self-healing task complete until the original task is unblocked, resumed/requeued with a clearer owner/cut line, or explicitly blocked behind a human decision.",
	}, "\n")
}

func selfHealingIncidentUpdate(reason agent.EscalationReason, detail string) string {
	trigger := strings.TrimSpace(string(reason))
	if trigger == "" {
		trigger = "unknown"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "no detail provided"
	}
	return strings.Join([]string{
		"Latest incident:",
		fmt.Sprintf("- Trigger: %s", trigger),
		fmt.Sprintf("- Detail: %s", detail),
	}, "\n")
}
