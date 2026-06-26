package team

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// TestOrchestratorE2E_RealClientThroughBroker is the CI-safe end-to-end proof of
// the P1b path: a real task is BORN orchestrator-owned through the real creation
// path (EnsurePlannedTask), routed through the REAL provider.DispatchClient over
// REAL HTTP to a stand-in for orchestrator/service.py, and the returned
// projection is written back so the web-facing task wire shape reflects the new
// state. The only thing faked is the orchestrator's business logic (the httptest
// handler returns a canned StepResult); every layer in between — wire marshal,
// HTTP, StepResult decode, projection write-back, lifecycle transition, derived
// web fields — is the production code.
func TestOrchestratorE2E_RealClientThroughBroker(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })

	var sawRunPath bool
	var sawLifecycle string
	// Stand-in for the Python orchestrator's POST /run (orchestrator/service.py).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sawRunPath = true
		raw, _ := io.ReadAll(r.Body)
		var req provider.DispatchRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("orchestrator: undecodable DispatchRequest: %v", err)
		}
		// The record is the authoritative re-hydrate source: it must carry the
		// task's current canonical lifecycle, not a derived guess.
		sawLifecycle, _ = req.Record["lifecycle_state"].(string)
		// Secrets cross by env-var NAME only.
		if office, ok := req.MCP["wuphf-office"]; ok {
			for _, name := range office.EnvPassthrough {
				if strings.Contains(name, "=") {
					t.Errorf("env_passthrough leaked a value, not a name: %q", name)
				}
			}
		}
		// Mirror the REAL service shape for a turn that submits for review: the
		// step INTERRUPTS at a human gate and the projection is the GATE state
		// (review), never the pre-gate executable "running". Emitting "running"
		// here would let the broker re-dispatch the task forever — the bug the
		// graph's enter_gate split fixed. The interrupt payload carries only the
		// gate identity (the broker logs gate_kind, never the agent text).
		_ = json.NewEncoder(w).Encode(provider.StepResult{
			Status:   provider.StepStatusInterrupted,
			ThreadID: req.TaskID,
			Projection: provider.Projection{
				TaskID: req.TaskID, LifecycleState: "review",
				PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress",
			},
			Interrupt: map[string]any{"type": "approval_required", "gate_kind": "review"},
		})
	}))
	defer srv.Close()

	b := newTestBroker(t)
	l := &Launcher{broker: b}
	// The REAL dispatch client, pointed at the stand-in over real HTTP.
	l.SetTaskOrchestrator(provider.NewDispatchClient(srv.URL, provider.WithHTTPClient(srv.Client())))

	// 1. A task is born orchestrator-owned through the real creation path.
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:      "general",
		Title:        "render a chart",
		Owner:        "eng",
		CreatedBy:    "human",
		Orchestrator: orchestratorLangGraph,
	})
	if err != nil || reused {
		t.Fatalf("EnsurePlannedTask: err=%v reused=%v (want a fresh create)", err, reused)
	}
	if !taskUsesOrchestrator(task) {
		t.Fatalf("created task is not orchestrator-owned: %q", task.Orchestrator)
	}

	// 2. Move it to an executable state, as the broker would before dispatch.
	if err := b.TransitionLifecycle(task.ID, LifecycleStateRunning, "test:start"); err != nil {
		t.Fatalf("transition to running: %v", err)
	}
	running, _ := taskByID(t, b, task.ID)

	// 3. Dispatch through the real client → real HTTP → projection write-back.
	l.dispatchTaskViaOrchestrator("eng", running)

	if !sawRunPath {
		t.Fatal("orchestrator /run was never called")
	}
	if sawLifecycle != "running" {
		t.Fatalf("re-hydrate record lifecycle_state = %q, want running", sawLifecycle)
	}

	// 4. The projection landed: the task parked at the human gate (review), which
	// is NON-executable — so the broker surfaces the gate and will not re-dispatch.
	got, _ := taskByID(t, b, task.ID)
	if got.LifecycleState != LifecycleStateReview {
		t.Fatalf("lifecycle = %q, want review", got.LifecycleState)
	}
	if isExecutableTeamTaskStatus(got.LifecycleState) {
		t.Fatalf("task parked at a gate must be non-executable, got executable %q", got.LifecycleState)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	for _, want := range []string{`"pipeline_stage":"review"`, `"review_state":"ready_for_review"`, `"status":"in_progress"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("web wire shape missing %s in: %s", want, data)
		}
	}
}

// taskByID lives in broker_phase6_migration_test.go (returns teamTask, bool).

// TestEnsurePlannedTask_RejectsBadOrchestrator locks the create-time validation:
// only "" and "langgraph" are accepted.
func TestEnsurePlannedTask_RejectsBadOrchestrator(t *testing.T) {
	t.Parallel()
	if err := validateTaskRuntimeFields("", "", "", "wat"); err == nil {
		t.Fatal("expected rejection of unknown orchestrator value")
	}
	if err := validateTaskRuntimeFields("", "", "", orchestratorLangGraph); err != nil {
		t.Fatalf("langgraph should validate: %v", err)
	}
	if err := validateTaskRuntimeFields("", "", "", ""); err != nil {
		t.Fatalf("empty orchestrator should validate: %v", err)
	}
}
