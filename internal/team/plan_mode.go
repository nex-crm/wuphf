package team

// plan_mode.go owns "Plan mode" (Phase 5): a per-task "Plan first" toggle
// (default ON) that makes the owner plan autonomously before executing. A
// Plan-first task enters LifecycleStatePlanning, where the owner is dispatched
// with a PLAN-ONLY work packet — it writes a plan into its own notebook, posts
// a summary, and stops to await "Approve & Start" (Planning → Running). With
// the toggle OFF the task skips Planning and runs immediately.
//
// Specs live in the owner's notebook, not the wiki; the Librarian promotes a
// plan to the canonical wiki only when it is worth the team seeing — the team
// usually cares about the OUTPUT, not the spec.

// taskIsPreExecution reports whether a task has not started executing yet — so
// a (re)assignment can route it into Plan mode (Planning) rather than straight
// to Running. Running/Review/Decision/terminal states are NOT pre-execution: a
// task already mid-flight must not be bounced back into planning when its owner
// changes.
func taskIsPreExecution(s LifecycleState) bool {
	switch s {
	case "", LifecycleStateUnknown, LifecycleStateDrafting, LifecycleStateIntake,
		LifecycleStateReady, LifecycleStatePlanning, LifecycleStateQueuedBehindOwner:
		return true
	}
	return false
}

// isPlanningLifecycleState reports whether a task is in the autonomous planning
// phase (LifecycleStatePlanning), where the owner is dispatched to write a plan
// read-only before execution. It is the single trigger for running a turn in the
// provider's NATIVE plan/read-only permission mode (Claude --permission-mode
// plan, Codex -s read-only) instead of full bypass — see resolveTurnPosture. A
// task in any other state (office/conversational turns, Running/Approved work)
// stays execute-posture with full autonomy.
func isPlanningLifecycleState(s LifecycleState) bool {
	return s == LifecycleStatePlanning
}

// specIsFrozen reports whether a task's human-approved spec BODY (its Details —
// the problem / approach / acceptance criteria the human signed off on) may no
// longer be rewritten. Once the owner is executing an approved plan (Running)
// or the task has moved into review/decision/blocked or a terminal state, that
// body is locked: a duplicate create / plan request, or an update, must NOT
// silently overwrite it. This enforces the product rule "once a spec is
// approved it must not change after that."
//
// Scope note: only Details is frozen. Classification/routing (TaskType,
// ExecutionMode), dependency wiring (DependsOn), and runtime config
// (effort/provider/model) are NOT — the system legitimately recomputes them on
// reuse (e.g. the memory-workflow completion gate keys off TaskType).
//
// Editable states are the pre-approval ones (Drafting/Intake/Ready/Planning/
// QueuedBehindOwner) plus ChangesRequested — the request_changes revise loop
// intentionally re-opens the spec, so it is NOT frozen. To edit an
// approved/running spec, a reviewer first request_changes it back into that
// loop.
func specIsFrozen(s LifecycleState) bool {
	switch s {
	case LifecycleStateRunning, LifecycleStateReview, LifecycleStateDecision,
		LifecycleStateBlockedOnPRMerge, LifecycleStateApproved,
		LifecycleStateRejected, LifecycleStateArchived:
		return true
	}
	return false
}

// planModeDirective is prepended to the work packet for a task in
// LifecycleStatePlanning. It tells the owner to plan only (no repo changes, no
// external actions), capture the plan in its notebook, post a summary, and
// stop. Enforcement is now two layers: this instruction PLUS the runtime running
// the planning turn in the provider's native read-only/plan permission mode
// (Claude --permission-mode plan, Codex -s read-only — see resolveTurnPosture),
// so a planning turn cannot change the repo even if the model ignores the
// directive. opencode / openai-compat have no native sandbox, so for those
// providers this directive remains the sole enforcement.
func planModeDirective(task teamTask) string {
	notebookPath := "agents/<your-slug>/notebook/plan-" + task.ID + ".md"
	return "[PLAN MODE] This task is in planning — do NOT change the repo, run build/deploy steps, or take external actions yet. Plan first:\n" +
		"1. Read only what you need to understand the work; do not do the work.\n" +
		"2. Write a tight PLAN to your notebook with notebook_write (path like " + notebookPath + "): the goal, a concrete step-by-step approach, acceptance criteria, and risks/open questions. This is a draft for you and the human — it is NOT promoted to the team wiki unless @librarian decides it is worth it.\n" +
		"3. Post a short summary of the plan to the task channel so the human can review it.\n" +
		"4. Then STOP. Execution starts only after the human clicks \"Approve & Start\"; you will be re-notified to begin the work then. Do NOT start implementing in this turn.\n" +
		"---\n"
}
