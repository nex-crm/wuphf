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

	ctx, cancel := context.WithTimeout(context.Background(), orchestratorStepTimeout)
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
		log.Printf("team: orchestrator interrupted task %q at a human gate: %v", taskID, result.Interrupt)
	}

	newState := LifecycleState(strings.TrimSpace(proj.LifecycleState))
	if _, ok := derivedFieldsFor(newState); !ok {
		log.Printf("team: orchestrator returned non-canonical lifecycle %q for task %q — refusing to transition", proj.LifecycleState, taskID)
		return
	}
	if err := l.broker.TransitionLifecycle(taskID, newState, "orchestrator:"+result.Status); err != nil {
		log.Printf("team: applying orchestrator projection for task %q -> %q failed: %v", taskID, newState, err)
	}
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
