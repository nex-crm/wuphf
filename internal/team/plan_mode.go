package team

import (
	"encoding/json"
	"strings"
)

// plan_mode.go owns "Plan mode": a structured-planning phase that a work-shaped
// human goal enters BEFORE any execution or sub-task decomposition. A plan-first
// task enters LifecycleStatePlanning, where the owner is dispatched read-only
// (the provider's NATIVE plan mode — Claude --permission-mode plan, Codex
// -s read-only; see resolveTurnPosture). In that turn the owner explores, asks
// the human any genuine clarifying questions via human_interview, writes a plan
// into its own notebook, and stops. The plan is surfaced for approval through a
// human_interview; approving it transitions Planning→Running, after which the
// owner executes and creates the sub-tasks the plan called for.
//
// This is what stops the duplicate / shallow-subtask spray: no sub-issues are
// created and no execution runs until the human has seen and approved a single
// coherent plan.

// isPlanningLifecycleState reports whether a task is in the structured-planning
// phase (LifecycleStatePlanning), where the owner is dispatched to write a plan
// read-only before execution. It is the single trigger for running a turn in the
// provider's NATIVE plan/read-only permission mode (Claude --permission-mode
// plan, Codex -s read-only) instead of full bypass — see resolveTurnPosture. A
// task in any other state (office/conversational turns, Running/Approved work)
// stays execute-posture with full autonomy.
func isPlanningLifecycleState(s LifecycleState) bool {
	return s == LifecycleStatePlanning
}

// extractClaudePlanArtifact pulls the plan text out of an ExitPlanMode tool_use
// input. Under Claude's native plan mode the finished plan is delivered as the
// ExitPlanMode tool call ({"plan": "..."}), NOT as assistant text — so a
// planning turn must harvest it from the tool input. Returns "" when toolInput
// is not ExitPlanMode-shaped or carries no plan.
func extractClaudePlanArtifact(toolInput string) string {
	var parsed struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(toolInput)), &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Plan)
}

// isExitPlanModeTool reports whether a tool name is Claude's ExitPlanMode tool,
// tolerant of the MCP-style prefixes Claude may surface it under.
func isExitPlanModeTool(toolName string) bool {
	name := strings.TrimSpace(toolName)
	if idx := strings.LastIndex(name, "__"); idx >= 0 {
		name = name[idx+2:]
	}
	return strings.EqualFold(name, "ExitPlanMode")
}

// planModeDirective is prepended to the work packet for a task in
// LifecycleStatePlanning. It tells the owner to plan only (no repo changes, no
// external actions, no sub-task creation), ask the human any genuine open
// questions, capture the plan in its notebook, and stop. Enforcement is two
// layers: this instruction PLUS the runtime running the planning turn in the
// provider's native read-only/plan permission mode (Claude --permission-mode
// plan, Codex -s read-only — see resolveTurnPosture), so a planning turn cannot
// change the repo even if the model ignores the directive. opencode /
// openai-compat have no native sandbox, so for those providers this directive
// remains the sole enforcement.
func planModeDirective(task teamTask) string {
	notebookPath := "agents/<your-slug>/notebook/plan-" + task.ID + ".md"
	return "[PLAN MODE] This task is in structured planning and your runtime is read-only — do NOT change the repo, run build/deploy steps, create sub-tasks, request tool/connection access, or take external actions this turn (the sandbox will block them anyway). Plan FIRST, then the human reviews:\n" +
		"1. Read only what you need to understand the work; do not do the work.\n" +
		"2. PLAN BEFORE YOU ASK. The human wants to see your plan before any questions. Produce a tight PLAN: the goal, the concrete step-by-step approach, the SPECIFIC sub-tasks you will create (each a distinct, non-overlapping slice — no sub-task that merely restates the goal or duplicates a sibling), the access/connections execution will need (state these IN the plan as prerequisites — do NOT ask the human to connect anything this turn), acceptance criteria, and risks/open questions. Present the plan as your final answer (in plan mode this goes through the plan/ExitPlanMode surface). If your runtime still allows writes, also save it to your notebook with notebook_write (path like " + notebookPath + ").\n" +
		"3. ASK ONLY WHAT GENUINELY BLOCKS THE PLAN, one question at a time. If you truly cannot write a sensible plan without a decision from the human, call human_interview with ONE clear, single-threaded question and 2-4 CONCRETE suggested answers as `options` (each a real choice they can accept in one click) plus `recommended_id` for your best guess — the broker automatically adds a final \"write your own\" option, so never include one yourself. One question per interview; never batch unrelated questions; never ask for tool/connection access here. If nothing genuinely blocks the plan, ask nothing.\n" +
		"4. Then STOP — the plan is surfaced to the human for approval automatically. Execution, connections, and sub-task creation start ONLY after the human approves the plan; you will be re-notified to begin then. Do NOT start implementing or decompose into sub-tasks in this turn.\n" +
		"---\n"
}
