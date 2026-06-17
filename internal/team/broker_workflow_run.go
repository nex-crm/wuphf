package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// broker_workflow_run.go is the runtime: it executes a frozen contract and
// records the run. The state machine runs deterministically; actions are
// dispatched by kind through workflowActionExec, which is where the real agent
// draft (llm) and the ExternalActionApprovalCard gate (external) plug in.

func workflowRunsPath(id string) string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "office", "workflows", id+".runs.jsonl")
}

// workflowGate decides (and, on proceed, executes) one external action. The
// production gate is externalGateDecision; tests inject a fake.
type workflowGate func(ctx context.Context, agent, platform, actionID string, data map[string]any) workflow.ActionOutcome

// makeWorkflowActionExec builds the ActionExec the runtime executes the spec
// with, bound to the operating agent + the real external gate.
func (b *Broker) makeWorkflowActionExec(ctx context.Context, agent string) workflow.ActionExec {
	return b.makeWorkflowActionExecWithGate(ctx, agent, b.externalGateDecision)
}

// makeWorkflowActionExecWithGate is the testable core: it routes actions by kind
// and delegates external actions to the supplied gate.
func (b *Broker) makeWorkflowActionExecWithGate(ctx context.Context, agent string, gate workflowGate) workflow.ActionExec {
	return func(a workflow.Action, data map[string]any) workflow.ActionOutcome {
		switch a.Kind {
		case workflow.ActionLLM:
			// Real agent-draft seam. WUPHF agents are headless (async), so a
			// synchronous in-process draft uses a deterministic baseline until the
			// agent-dispatch seam lands; the drafted text flows to the send action.
			return workflow.ActionOutcome{OK: true, Output: map[string]any{"draft": draftMessage(a, data)}}
		case workflow.ActionExternal:
			if strings.TrimSpace(a.Platform) == "" || strings.TrimSpace(a.ActionID) == "" {
				// No integration target declared yet (auto-draft): record intent.
				return workflow.ActionOutcome{OK: true, Output: map[string]any{"gated": true, "note": "no integration target"}}
			}
			return gate(ctx, agent, a.Platform, a.ActionID, data)
		default:
			return workflow.ActionOutcome{OK: true}
		}
	}
}

// externalGateDecision runs the real deterministic-integrations gate: classify
// the action against the live connection state + scoped grants. On "proceed"
// (connected + granted, or read-only) it executes the send via Composio; any
// other decision blocks the send and records that a human must act.
func (b *Broker) externalGateDecision(ctx context.Context, agent, platform, actionID string, data map[string]any) workflow.ActionOutcome {
	resp := b.resolveExternalAction(ctx, integrationResolveRequest{
		Provider: "composio", Platform: platform, ActionID: actionID, Agent: agent, Data: data,
	})
	if resp.Decision != string(action.DecisionProceed) {
		// The gate requires human action (connect / approve / wait). The send
		// does not fire; the run records the stop so the workflow can resume
		// once the human resolves it.
		return workflow.ActionOutcome{OK: false, Output: map[string]any{
			"decision": resp.Decision, "needs_approval": true, "request_id": resp.RequestID,
		}}
	}
	composio := action.NewComposioFromEnv()
	if !composio.Configured() || resp.Account == nil {
		return workflow.ActionOutcome{OK: true, Output: map[string]any{"sent": false, "note": "no connected account"}}
	}
	if _, err := composio.ExecuteAction(ctx, action.ExecuteRequest{
		Platform: platform, ActionID: actionID, ConnectionKey: resp.Account.Key, Data: data,
	}); err != nil {
		return workflow.ActionOutcome{OK: false, Err: err.Error()}
	}
	return workflow.ActionOutcome{OK: true, Output: map[string]any{"sent": true}}
}

// draftMessage is the deterministic baseline draft (sorted for stability). The
// real LLM agent-draft replaces this body once the headless-agent draft seam is
// wired; the rest of the runtime is unchanged.
func draftMessage(a workflow.Action, data map[string]any) string {
	if len(data) == 0 {
		return fmt.Sprintf("Draft for %q", a.ID)
	}
	parts := make([]string, 0, len(data))
	for k, v := range data {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(parts)
	return fmt.Sprintf("Draft for %q (%s)", a.ID, strings.Join(parts, ", "))
}

// runWorkflowSpec loads a stored contract, executes it over the events, persists
// the run observation, and updates the binding's last-execution. The run is the
// raw material the improvement loop mines.
func (b *Broker) runWorkflowSpec(specID, trigger string, events []workflow.ScenarioEvent) (workflow.RunRecord, error) {
	path := workflowSpecPath(specID)
	if path == "" {
		return workflow.RunRecord{}, fmt.Errorf("no runtime home")
	}
	spec, err := workflow.LoadSpec(path)
	if err != nil {
		return workflow.RunRecord{}, err
	}
	if len(events) == 0 {
		ev := "run"
		if len(spec.Events) > 0 {
			ev = spec.Events[0].ID
		}
		events = []workflow.ScenarioEvent{{Event: ev}}
	}

	exec := b.makeWorkflowActionExec(context.Background(), strings.TrimSpace(spec.Operator))
	rec := workflow.Execute(spec, trigger, events, exec)
	rec.At = time.Now().UTC().Format(time.RFC3339)
	_ = workflow.AppendRun(workflowRunsPath(specID), rec)

	b.mu.Lock()
	if sk := b.findSkillByNameLocked(specID); sk != nil {
		sk.LastExecutionAt = rec.At
		sk.LastExecutionStatus = "ok"
		sk.UsageCount++
	}
	_ = b.saveLocked()
	b.mu.Unlock()
	return rec, nil
}

// RunWorkflowSpec is the scheduler entry point (TargetType=workflow_spec).
func (b *Broker) RunWorkflowSpec(specID, trigger string) error {
	_, err := b.runWorkflowSpec(specID, trigger, nil)
	return err
}

// handleWorkflowsProposals closes the self-healing loop: it mines a contract's
// run records for recurring exceptions and returns drafted overlays the operator
// can review and accept (via /workflows/improve). Read-only; nothing is applied.
func (b *Broker) handleWorkflowsProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID string `json:"spec_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	id := strings.TrimSpace(body.SpecID)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	path := workflowSpecPath(id)
	if path == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "no_runtime_home"})
		return
	}
	spec, err := workflow.LoadSpec(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "spec_not_found"})
		return
	}
	runs, _ := workflow.ReadRuns(workflowRunsPath(id))
	proposals := workflow.ProposeOverlays(spec, runs, workflow.ProposeOptions{})
	writeJSON(w, http.StatusOK, map[string]any{"spec_id": id, "runs": len(runs), "proposals": proposals})
}

// handleWorkflowsRun is the manual / programmatic trigger. Schedule and event
// triggers fire the same runtime via RunWorkflowSpec.
func (b *Broker) handleWorkflowsRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID   string         `json:"spec_id"`
		Event    string         `json:"event,omitempty"`
		Data     map[string]any `json:"data,omitempty"`
		DedupKey string         `json:"dedup_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	id := strings.TrimSpace(body.SpecID)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	var events []workflow.ScenarioEvent
	if strings.TrimSpace(body.Event) != "" {
		events = []workflow.ScenarioEvent{{Event: body.Event, Data: body.Data, DedupKey: body.DedupKey}}
	}
	rec, err := b.runWorkflowSpec(id, "manual", events)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "run_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": rec})
}
