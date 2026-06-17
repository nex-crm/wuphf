package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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

// workflowActionExec dispatches one contract action by kind. Deterministic
// actions complete inline; llm and external actions are the seams where the
// scoped agent draft and the approval gate plug in. For now they record intent
// (OK) so the orchestration is exercised end-to-end; the live agent/Composio
// wiring is the next runtime slice.
func (b *Broker) workflowActionExec(action workflow.Action, _ map[string]any) workflow.ActionOutcome {
	switch action.Kind {
	case workflow.ActionLLM:
		// TODO: route to the scoped agent to draft the message.
		return workflow.ActionOutcome{OK: true, Output: map[string]any{"drafted": true}}
	case workflow.ActionExternal:
		// TODO: route through ExternalActionApprovalCard (gate, dedupe, throttle).
		return workflow.ActionOutcome{OK: true, Output: map[string]any{"gated": true}}
	default:
		return workflow.ActionOutcome{OK: true}
	}
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

	rec := workflow.Execute(spec, trigger, events, b.workflowActionExec)
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
