package team

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// workflow_extract.go is the completion-time / on-demand workflow extractor's
// broker glue. It reads a completed task's persisted traces (trace_sink.go),
// applies the cheap gate, runs the grounded LLM extract+judge
// (internal/workflow/extract.go) through the office's configured provider, and
// returns a proposal: is-this-a-workflow + the executable contract. The pure
// logic lives in internal/workflow; this file only wires data + the model.

// brokerExtractor implements workflow.Extractor with the office's configured
// default provider — the same one-shot path the CEO and Librarian use.
type brokerExtractor struct{ ctx context.Context }

func (e brokerExtractor) Extract(in workflow.ExtractInput) (workflow.Extraction, error) {
	system, user := workflow.BuildExtractPrompt(in)
	out, err := provider.RunConfiguredOneShotCtx(e.ctx, system, user, "")
	if err != nil {
		return workflow.Extraction{}, err
	}
	return workflow.ParseExtraction(out)
}

// workflowProposal is the result of extracting a workflow from one task. When
// IsWorkflow is false, Spec/Shipcheck are nil and Reason explains why.
type workflowProposal struct {
	TaskID     string                    `json:"task_id"`
	IsWorkflow bool                      `json:"is_workflow"`
	Confidence float64                   `json:"confidence"`
	Name       string                    `json:"name"`
	Trigger    workflow.ExtractedTrigger `json:"trigger"`
	Reason     string                    `json:"reason,omitempty"`
	Spec       *workflow.Spec            `json:"spec,omitempty"`
	Shipcheck  *workflow.ShipcheckReport `json:"shipcheck,omitempty"`
}

// taskGoalAndOwner returns the human-facing ask (title + details) and the owner
// slug for a task, or empty strings when the task is unknown. Locks b.mu.
func (b *Broker) taskGoalAndOwner(taskID string) (goal, owner string) {
	id := strings.TrimSpace(taskID)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			return strings.TrimSpace(b.tasks[i].Title + "\n" + b.tasks[i].Details), strings.TrimSpace(b.tasks[i].Owner)
		}
	}
	return "", ""
}

// extractWorkflowForTask runs the completion-time extractor over one task's
// traces: cheap gate (>=2 distinct integration actions) -> grounded
// extract+judge -> build + shipcheck the executable contract. ext is injected
// so tests can drive it without a live model.
func (b *Broker) extractWorkflowForTask(taskID string, ext workflow.Extractor) (workflowProposal, error) {
	prop := workflowProposal{TaskID: strings.TrimSpace(taskID)}
	traces, err := ActionTracesForTask(TraceSinkPath(), taskID)
	if err != nil {
		return workflowProposal{}, err
	}

	// Build the gated shape (distinct action tokens, first-use order) and the
	// model-facing trace steps.
	var shape []string
	seen := map[string]bool{}
	steps := make([]workflow.TraceStep, 0, len(traces))
	for _, tr := range traces {
		tok := strings.ToLower(strings.TrimSpace(tr.ActionID))
		if tok == "" {
			continue
		}
		if !seen[tok] {
			seen[tok] = true
			shape = append(shape, tok)
		}
		steps = append(steps, workflow.TraceStep{
			ActionID: tr.ActionID, Platform: tr.Platform, Args: tr.Args, Result: tr.Result,
		})
	}

	// Cheap gate: a single integration call is not a workflow — skip the model.
	if len(shape) < 2 {
		prop.Reason = "task used fewer than two integration actions — not a workflow"
		return prop, nil
	}

	goal, owner := b.taskGoalAndOwner(taskID)
	ex, err := ext.Extract(workflow.ExtractInput{Goal: goal, Shape: shape, Traces: steps})
	if err != nil {
		return workflowProposal{}, err
	}
	ex = workflow.GroundExtraction(ex, shape)
	prop.IsWorkflow = ex.IsWorkflow
	prop.Confidence = ex.Confidence
	prop.Name = ex.Name
	prop.Trigger = ex.Trigger
	prop.Reason = ex.Reason
	if !ex.IsWorkflow || len(ex.Steps) == 0 {
		return prop, nil
	}

	spec, err := workflow.BuildSpecFromExtraction("extracted-"+prop.TaskID, owner, ex, b.draftKnownPlatforms())
	if err != nil {
		// A grounded extraction that won't validate is reported as not-a-workflow
		// rather than surfaced as a broken contract.
		prop.IsWorkflow = false
		prop.Reason = "extracted contract did not validate: " + err.Error()
		return prop, nil
	}
	report := workflow.Shipcheck(&spec)
	prop.Spec = &spec
	prop.Shipcheck = &report
	return prop, nil
}

// handleWorkflowsExtract is the explicit-ask trigger: POST {task_id} extracts a
// workflow from that completed task's trace on demand.
func (b *Broker) handleWorkflowsExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(body.TaskID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id_required"})
		return
	}
	prop, err := b.extractWorkflowForTask(body.TaskID, brokerExtractor{ctx: r.Context()})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "extract_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, prop)
}

// handleWorkflowsExtracted surfaces the completion-time extractions: workflows
// the model judged real, grouped by fingerprint with a recurrence count
// (distinct completed tasks). This is the proactive "press this into a
// workflow" feed, populated by the task-completion hook.
func (b *Broker) handleWorkflowsExtracted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	items, err := surfaceExtractedWorkflows(ProposalSinkPath())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "read_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": items})
}
