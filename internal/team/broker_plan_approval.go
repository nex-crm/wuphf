package team

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// broker_plan_approval.go wires the structured-planning approval gate. A task in
// LifecycleStatePlanning is dispatched read-only (see plan_mode.go); when that
// turn finishes with a plan, the broker raises ONE human_interview asking the
// human to approve the plan. Approving it transitions Planning→Running and
// dispatches the owner to execute — the single point at which sub-task creation
// and repo changes become legal for that work.

const planApprovalDedupePrefix = "plan-approval:"

// issueShouldPlanFirstLocked decides whether a freshly-created top-level Issue
// enters structured planning (Planning) before execution rather than landing
// straight in Running. The strong default: every top-level work Issue plans
// first, so the owner asks the human its genuine open questions, writes a single
// coherent plan, and gets it approved BEFORE any sub-tasks or repo changes —
// which is what stops the duplicate / shallow-subtask spray.
//
// Exemptions:
//   - sub-issues (ParentIssueID set): created from an already-approved parent
//     plan, so they execute directly.
//   - internal recovery actors (system/broker/nex): migration / fold-in /
//     self-heal paths must not stall on human plan approval.
//
// Caller holds b.mu.
func (b *Broker) issueShouldPlanFirstLocked(task *teamTask, actor string) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.ParentIssueID) != "" {
		return false
	}
	if isInternalTaskActor(actor) {
		return false
	}
	if b != nil && b.disablePlanFirstDefault {
		return false
	}
	return true
}

// requestIsPlanApproval reports whether a request is the plan-approval interview
// the broker raised for a planning task (DedupeKey "plan-approval:<taskID>").
func requestIsPlanApproval(req humanInterview) bool {
	return strings.HasPrefix(strings.TrimSpace(req.DedupeKey), planApprovalDedupePrefix)
}

// RaisePlanApproval surfaces a finished planning turn's plan for human approval.
// Safe to call from a runner goroutine: it takes b.mu, is idempotent (an active
// plan-approval interview for the task short-circuits), and persists. Returns
// the request id ("" when nothing was raised).
func (b *Broker) RaisePlanApproval(taskID, actor, plan string) string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.raisePlanApprovalInterviewLocked(taskID, actor, plan)
	if id != "" {
		if err := b.saveLocked(); err != nil {
			log.Printf("broker: persist plan-approval interview for %q: %v", taskID, err)
		}
	}
	return id
}

// raisePlanApprovalInterviewLocked raises the plan-approval human_interview for a
// planning task. Idempotent on DedupeKey + on any active interview already linked
// to the task. No-op when the task is missing or not in Planning. Caller holds
// b.mu and is responsible for persistence.
func (b *Broker) raisePlanApprovalInterviewLocked(taskID, actor, plan string) string {
	task := b.taskByIDLocked(strings.TrimSpace(taskID))
	if task == nil || task.LifecycleState != LifecycleStatePlanning {
		return ""
	}
	dedupeKey := planApprovalDedupePrefix + task.ID
	for i := range b.requests {
		if !requestIsActive(b.requests[i]) {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) == dedupeKey {
			return b.requests[i].ID
		}
	}

	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	from := strings.TrimSpace(actor)
	if from == "" {
		from = strings.TrimSpace(task.Owner)
	}
	if from == "" {
		from = "office"
	}

	var qb strings.Builder
	fmt.Fprintf(&qb, "Plan ready for %q. Review it and approve to start execution (the team will create the sub-tasks and begin once you approve).\n\n", strings.TrimSpace(task.Title))
	if p := strings.TrimSpace(plan); p != "" {
		qb.WriteString(truncateSummary(p, 1200))
	} else {
		qb.WriteString("(See the owner's plan in the task channel / notebook.)")
	}

	options, recommended := requestOptionDefaults("approval")
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	req := humanInterview{
		ID:            fmt.Sprintf("request-%d", b.counter),
		Kind:          "approval",
		Status:        "pending",
		From:          from,
		Channel:       channel,
		Title:         "Approve plan for " + task.ID,
		Question:      qb.String(),
		Options:       options,
		RecommendedID: recommended,
		DedupeKey:     dedupeKey,
		IssueID:       task.ID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	b.scheduleRequestLifecycleLocked(&req)
	b.postRequestRaisedChatMessageLocked(&req)
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	b.appendActionLocked("request_created", "office", channel, from,
		truncateSummary(req.Title+" "+req.Question, 140), req.ID)
	return req.ID
}

// startApprovedPlanTaskLocked transitions a planning task to Running and
// dispatches its owner — the structured-planning analogue of the human
// "approve = start" affordance for parked tasks. No-op unless the task is in
// Planning. Caller holds b.mu; persistence is the caller's responsibility.
func (b *Broker) startApprovedPlanTaskLocked(task *teamTask, actor string) {
	if b == nil || task == nil || task.LifecycleState != LifecycleStatePlanning {
		return
	}
	if err := b.applyLifecycleStateLocked(task, LifecycleStateRunning); err != nil {
		log.Printf("broker: start approved plan %q: %v", task.ID, err)
		return
	}
	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	channel := normalizeChannelSlug(task.Channel)
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(task)
	b.scheduleTaskLifecycleLocked(task)
	if err := b.syncTaskWorktreeLocked(task); err != nil {
		log.Printf("broker: worktree sync for approved plan %q: %v", task.ID, err)
	}
	b.appendActionLocked("task_updated", "office", channel, actor,
		truncateSummary(task.Title+" [plan approved]", 140), task.ID)
}

// applyPlanApprovalAnswerLocked is called from applyRequestAnswerLocked when a
// plan-approval interview is answered. An approve choice starts the task
// (Planning→Running + dispatch); any other choice (reject / needs-more-info)
// leaves the task in Planning so the owner can revise on the next notification.
// Caller holds b.mu.
func (b *Broker) applyPlanApprovalAnswerLocked(req humanInterview, answer *interviewAnswer, actor string) {
	if !requestIsPlanApproval(req) || answer == nil {
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(answer.ChoiceID), "approve") {
		return
	}
	task := b.taskByIDLocked(strings.TrimSpace(req.IssueID))
	if task == nil {
		return
	}
	b.startApprovedPlanTaskLocked(task, actor)
}
