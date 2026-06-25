package team

import (
	"fmt"
	"strings"
)

// applySubIssueCreateRulesLocked enforces the sub-issue invariants at create
// time when a task carries a parent_issue_id. Extracted from MutateTask to keep
// broker_tasks_mutation_service.go under the file-size budget. Caller holds b.mu
// and is responsible for rollback + returning the error. No-op for top-level
// tasks (empty ParentIssueID).
//
// Rules:
//   - Force task_type=issue so every sub-issue renders on the Issue detail
//     surface (same lifecycle, packet, components).
//   - Sub-issues nest one level deep only (reject a parent that is itself a
//     sub-issue, so the UI never has to render sub-sub-issue cascades).
//   - Plan-gate: refuse creation while the parent is still in Planning — its
//     plan is not approved, so this is the premature decomposition the planning
//     phase exists to prevent.
//   - Shallow-subtask guard: refuse a sub-issue that merely restates its parent.
//   - Sibling-dedup guard: refuse a sub-issue that duplicates an existing
//     non-terminal sibling under the same parent.
//
// Internal recovery actors (system/broker/nex) are exempt from the plan/shallow/
// sibling guards so migration and fold-in paths are never blocked.
func (b *Broker) applySubIssueCreateRulesLocked(task *teamTask, actor string) error {
	if task == nil || task.ParentIssueID == "" {
		return nil
	}
	task.TaskType = "issue"
	parent := b.findTaskByIDLocked(task.ParentIssueID)
	if parent == nil {
		// Refuse a sub-issue whose parent does not exist — a dangling
		// parent_issue_id would otherwise create an orphan that bypasses every
		// parent-based guard (plan-gate, shallow-restate, sibling dedup).
		return taskMutationError(
			TaskMutationNotFound,
			fmt.Sprintf("parent issue %s not found — create the parent first or fix the parent_issue_id", strings.TrimSpace(task.ParentIssueID)),
			nil,
		)
	}
	if strings.TrimSpace(parent.ParentIssueID) != "" {
		return taskMutationError(
			TaskMutationConflict,
			"sub-issues can only be one level deep; pick the top-level parent instead",
			nil,
		)
	}
	internal := isInternalTaskActor(actor)
	if parent.LifecycleState == LifecycleStatePlanning && !internal {
		return taskMutationError(
			TaskMutationConflict,
			fmt.Sprintf("parent %s is still in planning — its plan has not been approved yet. Finish the plan and wait for the human to approve it before creating sub-tasks.", parent.ID),
			nil,
		)
	}
	if internal {
		return nil
	}
	if titlesAreSimilar(task.Title, parent.Title) {
		return taskMutationError(
			TaskMutationConflict,
			fmt.Sprintf("sub-task %q just restates parent %s — break the parent into distinct, non-overlapping pieces instead of duplicating it.", strings.TrimSpace(task.Title), parent.ID),
			nil,
		)
	}
	for j := range b.tasks {
		sib := &b.tasks[j]
		if strings.TrimSpace(sib.ParentIssueID) != task.ParentIssueID {
			continue
		}
		if isTerminalTeamTaskStatus(sib.status) {
			continue
		}
		if titlesAreSimilar(task.Title, sib.Title) {
			return taskMutationError(
				TaskMutationConflict,
				fmt.Sprintf("sub-task %q duplicates existing sibling %s (%q) under parent %s — comment on it or pick a distinct slice instead.", strings.TrimSpace(task.Title), sib.ID, strings.TrimSpace(sib.Title), parent.ID),
				nil,
			)
		}
	}
	return nil
}
