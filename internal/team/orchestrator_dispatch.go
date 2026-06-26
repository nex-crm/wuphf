package team

// orchestrator_dispatch.go is the broker side of the LangGraph orchestrator-of-
// record path (the migration plan's P1b-ii). When a task carries
// Orchestrator=="langgraph" AND a dispatcher is wired, the broker hands the
// whole task to the Python orchestrator (POST /run) instead of enqueuing a
// headless CLI turn, then writes the returned projection back onto the task
// record by transitioning its lifecycle. The web renders the projected state
// unchanged.
//
// Re-hydrate model (the spike's P4 decision): each dispatch is ONE step. The
// orchestrator rebuilds run-state from the record we send every time, so a
// human-gate interrupt is resolved simply by re-dispatching once the broker's
// existing approval path has moved the task forward — no separate Resume wiring
// is needed on this path. Resume stays available on the client for the future
// streaming path.
//
// Strictly additive: l.orchestrator is nil unless WUPHF_ORCHESTRATOR_URL is set,
// so default installs are byte-for-byte unchanged.

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// orchestratorLangGraph is the only Orchestrator flag value honoured today. The
// empty string means "the broker owns this task" (the default).
const orchestratorLangGraph = "langgraph"

// orchestratorStepTimeout bounds a single /run or /resume call so an unreachable
// or wedged sidecar can't hang a dispatch indefinitely. One step maps to one
// agent turn, which can be slow under a real inner harness (P2), so this is
// generous.
const orchestratorStepTimeout = 10 * time.Minute

// taskOrchestrator is the broker's view of the deepagents dispatch client
// (internal/provider.DispatchClient satisfies it). Narrow by design so tests
// inject a fake without a live sidecar.
type taskOrchestrator interface {
	Run(ctx context.Context, req provider.DispatchRequest) (*provider.StepResult, error)
	Resume(ctx context.Context, req provider.ResumeRequest) (*provider.StepResult, error)
	Coordinate(ctx context.Context, req provider.CoordinateRequest) (*provider.CoordinationPlan, error)
}

// newConfiguredTaskOrchestrator returns a live dispatch client when
// WUPHF_ORCHESTRATOR_URL is set, else nil. nil disables the orchestrator path
// entirely (strangler-fig default-off).
func newConfiguredTaskOrchestrator() taskOrchestrator {
	if strings.TrimSpace(os.Getenv("WUPHF_ORCHESTRATOR_URL")) == "" {
		return nil
	}
	return provider.NewDispatchClient("")
}

// SetTaskOrchestrator overrides the orchestrator dispatch client. Used by
// command-level wiring and by tests to inject a fake. Passing nil disables the
// orchestrator path.
func (l *Launcher) SetTaskOrchestrator(o taskOrchestrator) {
	if l == nil {
		return
	}
	l.orchestrator = o
}

// taskUsesOrchestrator reports whether this task is owned by the LangGraph
// orchestrator. Case/space-insensitive on the flag so a hand-edited
// broker-state.json doesn't silently fall back to the broker path.
func taskUsesOrchestrator(task teamTask) bool {
	return strings.EqualFold(strings.TrimSpace(task.Orchestrator), orchestratorLangGraph)
}

// dispatchTaskViaOrchestrator runs one orchestration step for task and applies
// the resulting projection. Safe to call in a goroutine; it acquires no
// Launcher locks (the broker transition takes its own). A nil dispatcher is a
// no-op — the caller is expected to have checked, but this stays defensive.
func (l *Launcher) dispatchTaskViaOrchestrator(slug string, task teamTask) {
	if l == nil || l.orchestrator == nil {
		return
	}
	taskID := strings.TrimSpace(task.ID)
	if taskID == "" {
		return
	}

	// Single-flight guard (Finding D3): repeated sendTaskUpdate ticks for one
	// task must not spawn concurrent /run goroutines — they would race the
	// orchestrator's shared checkpointer and run duplicate agent turns. Hold a
	// per-task slot for the duration of this step; a second concurrent dispatch
	// for the same task is skipped, and the next tick re-dispatches once this
	// one releases the slot.
	if _, inFlight := l.orchestratorInFlight.LoadOrStore(taskID, struct{}{}); inFlight {
		log.Printf("team: orchestrator dispatch for task %q (owner %q) already in flight — skipping duplicate", taskID, slug)
		return
	}
	defer l.orchestratorInFlight.Delete(taskID)

	// Derive the step deadline from the launcher's lifecycle context so a Kill()
	// — which cancels l.headless.ctx — aborts an in-flight step instead of
	// letting it run for the full orchestratorStepTimeout after shutdown and
	// then writing to a stopped broker (Finding D2). Fall back to Background only
	// for zero-value &Launcher{} fixtures where the pool context was never wired.
	baseCtx := l.headless.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, orchestratorStepTimeout)
	defer cancel()

	req := provider.DispatchRequest{
		TaskID: taskID,
		Record: orchestratorRecord(task),
		Model:  strings.TrimSpace(task.Model),
		MCP:    orchestratorMCPServers(),
	}
	result, err := l.orchestrator.Run(ctx, req)
	if err != nil {
		// Leave the task in its current state; the dispatch loop retries on the
		// next executable tick. Loud so a misconfigured sidecar is visible.
		log.Printf("team: orchestrator Run failed for task %q (owner %q): %v", taskID, slug, err)
		return
	}
	l.applyOrchestratorProjection(taskID, result)
}

// taskGoalChildren returns the child tasks of goalID (ParentIssueID == goalID).
// Linear scan — there is no indexed children lookup in the broker — but it only
// runs on an orchestrator-owned goal tick, behind the per-task flag.
func (l *Launcher) taskGoalChildren(goalID string) []teamTask {
	if l == nil || l.broker == nil {
		return nil
	}
	var kids []teamTask
	for _, t := range l.broker.AllTasks() {
		if strings.TrimSpace(t.ParentIssueID) == goalID {
			kids = append(kids, t)
		}
	}
	return kids
}

// taskIsGoal reports whether this task is a goal the orchestrator should
// COORDINATE (a top-level task that has children) rather than dispatch as a
// single agent turn. A child task (one level deep) is never a goal.
func (l *Launcher) taskIsGoal(task teamTask) bool {
	if strings.TrimSpace(task.ParentIssueID) != "" {
		return false
	}
	return len(l.taskGoalChildren(task.ID)) > 0
}

// orchestratorGoalParent returns the parent goal a child's state change should
// re-coordinate, if any: the child must have a parent that is an executable,
// orchestrator-owned goal. A goal that has already completed (non-executable,
// e.g. in review) is not re-coordinated.
func (l *Launcher) orchestratorGoalParent(child teamTask) (teamTask, bool) {
	if l == nil || l.broker == nil || l.orchestrator == nil {
		return teamTask{}, false
	}
	parentID := strings.TrimSpace(child.ParentIssueID)
	if parentID == "" {
		return teamTask{}, false
	}
	parent := l.broker.TaskByID(parentID)
	if parent == nil || !taskUsesOrchestrator(*parent) {
		return teamTask{}, false
	}
	if !isExecutableTeamTaskStatus(parent.LifecycleState) {
		return teamTask{}, false
	}
	return *parent, true
}

// retickOrchestratorGoalParent re-coordinates a child's parent goal after the
// child changes state. Lifecycle transitions append no actions, so without this a
// goal would coordinate once and then stall — it would never notice a child
// finishing (to dispatch the next one) or all children finishing (to complete the
// goal). Rides the existing action-driven dispatch loop (the notifier), not a
// timer or the lifecycle-transition chokepoint. Per-goal single-flight in
// coordinateGoalViaOrchestrator collapses the bursts a child's actions produce.
func (l *Launcher) retickOrchestratorGoalParent(child teamTask) {
	parent, ok := l.orchestratorGoalParent(child)
	if !ok {
		return
	}
	go func() {
		defer recoverPanicTo("coordinateGoalViaOrchestrator",
			fmt.Sprintf("retick goal=%s child=%s", parent.ID, child.ID))
		l.coordinateGoalViaOrchestrator(parent.Owner, parent)
	}()
}

// coordinateGoalViaOrchestrator re-hydrates a goal's children, asks the
// orchestrator for the per-child action plan, and applies it. START activates a
// child to running (the notify loop dispatches it on the next tick); DISPATCH
// runs a child turn now; BLOCK/IDLE/AWAIT leave the child alone; a dependency
// cycle or an UNKNOWN action fails loud and acts on nothing for that child.
// Safe in a goroutine — it takes no Launcher locks (broker calls take their own).
func (l *Launcher) coordinateGoalViaOrchestrator(slug string, goal teamTask) {
	if l == nil || l.orchestrator == nil {
		return
	}
	goalID := strings.TrimSpace(goal.ID)
	if goalID == "" {
		return
	}
	// Single-flight per goal, in a key space distinct from per-task dispatch so a
	// goal and a same-named child can never collide.
	key := "coord:" + goalID
	if _, inFlight := l.orchestratorInFlight.LoadOrStore(key, struct{}{}); inFlight {
		log.Printf("team: goal coordination for %q (owner %q) already in flight — skipping duplicate", goalID, slug)
		return
	}
	defer l.orchestratorInFlight.Delete(key)

	children := l.taskGoalChildren(goalID)
	if len(children) == 0 {
		return
	}

	baseCtx := l.headless.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, orchestratorStepTimeout)
	defer cancel()

	plan, err := l.orchestrator.Coordinate(ctx, provider.CoordinateRequest{
		GoalID:   goalID,
		Children: orchestratorChildRecords(children),
	})
	if err != nil {
		log.Printf("team: orchestrator Coordinate failed for goal %q (owner %q): %v", goalID, slug, err)
		return
	}
	l.applyCoordinationPlan(slug, goalID, children, plan)
}

// applyCoordinationPlan acts on the orchestrator's per-child plan. A cycle is a
// deadlocked decomposition: log loud and coordinate nothing.
func (l *Launcher) applyCoordinationPlan(slug, goalID string, children []teamTask, plan *provider.CoordinationPlan) {
	if l == nil || l.broker == nil || plan == nil {
		return
	}
	if len(plan.Cycle) > 0 {
		log.Printf("team: goal %q has a dependency cycle %v — coordinating nothing", goalID, plan.Cycle)
		return
	}
	byID := make(map[string]teamTask, len(children))
	for _, c := range children {
		byID[c.ID] = c
	}
	for childID, action := range plan.Actions {
		child, ok := byID[childID]
		if !ok {
			// The orchestrator returned an id we did not send. Ignore defensively
			// rather than act on a task outside this goal.
			log.Printf("team: coordinate returned action %q for unknown child %q of goal %q — ignoring", action, childID, goalID)
			continue
		}
		switch action {
		case provider.CoordStart:
			if err := l.broker.TransitionLifecycle(childID, LifecycleStateRunning, "orchestrator:coordinate"); err != nil {
				log.Printf("team: coordinate START for child %q (goal %q) failed: %v", childID, goalID, err)
			}
		case provider.CoordDispatch:
			c := child
			go func() {
				defer recoverPanicTo("dispatchTaskViaOrchestrator", fmt.Sprintf("slug=%s task=%s (coordinate)", slug, c.ID))
				l.dispatchTaskViaOrchestrator(slug, c)
			}()
		case provider.CoordBlock, provider.CoordIdle, provider.CoordAwait:
			// Nothing for the orchestrator to do this tick.
		case provider.CoordUnknown:
			log.Printf("team: coordinate returned UNKNOWN for child %q (goal %q) — leaving for operator triage", childID, goalID)
		default:
			log.Printf("team: coordinate returned unrecognized action %q for child %q (goal %q) — ignoring", action, childID, goalID)
		}
	}

	// Goal completion: when every child has reached a terminal status, the goal's
	// work is done. Surface it for a final human review (the children were each
	// reviewed; this is the last sign-off). Computed from the CURRENT broker state,
	// not the dispatch snapshot, so it can't complete a goal whose child just
	// reopened. A goal in REVIEW is non-executable, so it is not re-coordinated.
	if l.allGoalChildrenTerminal(goalID) {
		if err := l.broker.TransitionLifecycle(goalID, LifecycleStateReview, "orchestrator: all children complete"); err != nil {
			log.Printf("team: completing goal %q (all children terminal) failed: %v", goalID, err)
		}
	}
}

// allGoalChildrenTerminal reports whether goalID has at least one child and every
// child is in a terminal status (done/archived/...). Reads current broker state.
func (l *Launcher) allGoalChildrenTerminal(goalID string) bool {
	children := l.taskGoalChildren(goalID)
	if len(children) == 0 {
		return false
	}
	for _, c := range children {
		row, ok := derivedFieldsFor(c.LifecycleState)
		if !ok || !isTerminalTeamTaskStatus(row.Status) {
			return false
		}
	}
	return true
}

// orchestratorChildRecords builds the re-hydrate records for a goal's children.
// depends_on is the UNION of DependsOn and BlockedOn so the kernel's release rule
// matches the broker's unblock cascade (which sweeps both); orchestratorRecord
// for a single task omits these, so coordination sends them explicitly.
func orchestratorChildRecords(children []teamTask) []map[string]any {
	out := make([]map[string]any, 0, len(children))
	for _, c := range children {
		out = append(out, map[string]any{
			"task_id":         c.ID,
			"lifecycle_state": string(c.LifecycleState),
			"depends_on":      unionDeps(c.DependsOn, c.BlockedOn),
		})
	}
	return out
}

// unionDeps merges two dependency lists, trimming blanks and de-duplicating
// while preserving first-seen order (deterministic for the kernel).
func unionDeps(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, d := range list {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if _, dup := seen[d]; dup {
				continue
			}
			seen[d] = struct{}{}
			out = append(out, d)
		}
	}
	return out
}

// applyOrchestratorProjection maps the orchestrator's one-way projection back
// onto the broker task by transitioning its lifecycle. Fail-loud on the
// orchestrator's "unmappable record" signal and on any non-canonical state —
// neither transitions the task (the migration plan's fail-loud rule).
func (l *Launcher) applyOrchestratorProjection(taskID string, result *provider.StepResult) {
	if l == nil || l.broker == nil || result == nil {
		return
	}
	proj := result.Projection
	if proj.IsUnknown() {
		log.Printf("team: orchestrator returned UNKNOWN lifecycle for task %q — leaving unchanged for operator triage", taskID)
		return
	}
	if result.Status == provider.StepStatusInterrupted && result.Interrupt != nil {
		// The projection already reflects the gate state (e.g. decision); the
		// broker's existing human-approval path resolves it, and the next
		// dispatch re-hydrates the orchestrator from the moved-forward record.
		//
		// Log ONLY the gate identity, never the full interrupt map (Finding I):
		// the orchestrator (graph.py) folds the agent's last turn text into the
		// interrupt payload, which may carry sensitive content.
		log.Printf("team: orchestrator interrupted task %q at a human gate (gate_kind=%q)", taskID, orchestratorGateKind(result.Interrupt))
	}

	newState := LifecycleState(strings.TrimSpace(proj.LifecycleState))
	if _, ok := derivedFieldsFor(newState); !ok {
		log.Printf("team: orchestrator returned non-canonical lifecycle %q for task %q — refusing to transition", proj.LifecycleState, taskID)
		return
	}
	// Terminal-resurrection guard (Finding C): this projection was computed from
	// the record captured at dispatch time. If the task reached a terminal
	// lifecycle in the meantime — e.g. a human approved or archived it while the
	// step was running — applying a stale "running"/gate projection would
	// transition it backward and revive a closed task. Read the CURRENT state
	// from the broker and refuse loudly rather than resurrect.
	if current := l.broker.TaskByID(taskID); current != nil {
		if row, ok := derivedFieldsFor(current.LifecycleState); ok && isTerminalTeamTaskStatus(row.Status) {
			log.Printf("team: orchestrator projected %q for task %q but it is already terminal (current=%q) — refusing to resurrect", newState, taskID, current.LifecycleState)
			return
		}
	}
	if err := l.broker.TransitionLifecycle(taskID, newState, "orchestrator:"+result.Status); err != nil {
		log.Printf("team: applying orchestrator projection for task %q -> %q failed: %v", taskID, newState, err)
	}
}

// orchestratorGateKind extracts the human-gate kind from an interrupt payload
// for logging. It reads only the gate_kind key and never the rest of the map,
// which can carry the agent's full turn text (Finding I). Defensive against a
// missing key or non-string value.
func orchestratorGateKind(interrupt map[string]any) string {
	if interrupt == nil {
		return ""
	}
	if v, ok := interrupt["gate_kind"].(string); ok {
		return v
	}
	return ""
}

// orchestratorRecord builds the authoritative re-hydrate record the orchestrator
// rebuilds run-state from. lifecycle_state is carried directly (lossless); the
// orchestrator never re-derives it from the legacy 4-tuple when the field is
// present (the spike's rule).
func orchestratorRecord(task teamTask) map[string]any {
	return map[string]any{
		"id":              task.ID,
		"task_id":         task.ID,
		"lifecycle_state": string(task.LifecycleState),
		"owner":           task.Owner,
		"title":           task.Title,
		"details":         task.Details,
	}
}

// orchestratorMCPServers names the teammcp server the orchestrator launches for
// the inner harness. Secrets cross by env-var NAME only (EnvPassthrough): the
// orchestrator process forwards these from its own env to the teammcp
// subprocess, mirroring SlackProviderBinding.BotTokenEnv. The broker binary is
// the MCP command (it serves `mcp-team`). os.Executable failing is non-fatal —
// the orchestrator falls back to "wuphf" on PATH.
func orchestratorMCPServers() map[string]provider.McpServer {
	command := "wuphf"
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		command = exe
	}
	return map[string]provider.McpServer{
		"wuphf-office": {
			Command:        command,
			Args:           []string{"mcp-team"},
			EnvPassthrough: []string{"WUPHF_BROKER_TOKEN", "WUPHF_BROKER_BASE", "WUPHF_BROKER_STATE"},
		},
	}
}
