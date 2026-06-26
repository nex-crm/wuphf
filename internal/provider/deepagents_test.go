package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeOrchestrator stands in for orchestrator/src/orchestrator/service.py. It
// asserts the request shape the Go client sends and replies with a canned
// StepResult, so these tests pin the Go<->Python wire contract without a live
// Python sidecar.
func fakeOrchestrator(t *testing.T, handler func(path string, body map[string]any) (int, any)) *DispatchClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("server: request to %s not JSON: %v\nraw: %s", r.URL.Path, err, raw)
			}
		}
		code, payload := handler(r.URL.Path, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if s, ok := payload.(string); ok { // allow raw non-JSON error bodies
			io.WriteString(w, s)
			return
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	return NewDispatchClient(srv.URL, WithHTTPClient(srv.Client()))
}

func doneResult(taskID string) StepResult {
	return StepResult{
		Status:   StepStatusDone,
		ThreadID: taskID,
		Projection: Projection{
			TaskID:         taskID,
			LifecycleState: "running",
			PipelineStage:  "implement",
			ReviewState:    "pending_review",
			Status:         "in_progress",
			Blocked:        false,
		},
	}
}

func TestDispatchClientRun_Done(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotBody map[string]any
	c := fakeOrchestrator(t, func(path string, body map[string]any) (int, any) {
		gotPath, gotBody = path, body
		return http.StatusOK, doneResult("task-1")
	})

	res, err := c.Run(context.Background(), DispatchRequest{
		TaskID: "task-1",
		Record: map[string]any{"lifecycle_state": "running"},
		Model:  "claude-sonnet-4-6",
		MCP: map[string]McpServer{
			"teammcp": {Command: "wuphf", Args: []string{"mcp-team"}, EnvPassthrough: []string{"WUPHF_BROKER_TOKEN"}},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPath != "/run" {
		t.Fatalf("posted to %q, want /run", gotPath)
	}
	// SchemaVersion must be stamped even though the caller left it zero.
	if v, _ := gotBody["schema_version"].(float64); int(v) != OrchestratorSchemaVersion {
		t.Fatalf("schema_version on wire = %v, want %d", gotBody["schema_version"], OrchestratorSchemaVersion)
	}
	// Secrets cross by env-var NAME only — never a value.
	mcp := gotBody["mcp"].(map[string]any)["teammcp"].(map[string]any)
	env := mcp["env_passthrough"].([]any)
	if len(env) != 1 || env[0] != "WUPHF_BROKER_TOKEN" {
		t.Fatalf("env_passthrough = %v, want [WUPHF_BROKER_TOKEN]", env)
	}
	if res.Status != StepStatusDone || res.Projection.LifecycleState != "running" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Projection.IsUnknown() {
		t.Fatalf("running projection should not be unknown")
	}
}

func TestDispatchClientRun_Interrupted(t *testing.T) {
	t.Parallel()
	c := fakeOrchestrator(t, func(_ string, _ map[string]any) (int, any) {
		return http.StatusOK, StepResult{
			Status:     StepStatusInterrupted,
			ThreadID:   "task-2",
			Projection: Projection{TaskID: "task-2", LifecycleState: "decision"},
			Interrupt:  map[string]any{"gate_kind": "review", "summary": "approve the plan?"},
		}
	})
	res, err := c.Run(context.Background(), DispatchRequest{TaskID: "task-2", Record: map[string]any{"lifecycle_state": "decision"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StepStatusInterrupted {
		t.Fatalf("status = %q, want interrupted", res.Status)
	}
	if res.Interrupt["gate_kind"] != "review" {
		t.Fatalf("interrupt payload not surfaced: %+v", res.Interrupt)
	}
}

func TestDispatchClientRun_InterruptedMissingPayloadFailsLoud(t *testing.T) {
	t.Parallel()
	c := fakeOrchestrator(t, func(_ string, _ map[string]any) (int, any) {
		return http.StatusOK, map[string]any{ // interrupted but no interrupt object
			"status": StepStatusInterrupted, "thread_id": "task-3",
			"projection": map[string]any{"task_id": "task-3", "lifecycle_state": "decision"},
		}
	})
	if _, err := c.Run(context.Background(), DispatchRequest{TaskID: "task-3", Record: map[string]any{}}); err == nil {
		t.Fatal("expected error when interrupted with no interrupt payload")
	}
}

func TestDispatchClientRun_UnknownProjection(t *testing.T) {
	t.Parallel()
	// service.py fail-loud shape: status done, projection carries only
	// task_id + lifecycle_state=unknown. The client decodes it cleanly and
	// IsUnknown flags it for operator triage.
	c := fakeOrchestrator(t, func(_ string, _ map[string]any) (int, any) {
		return http.StatusOK, map[string]any{
			"status": StepStatusDone, "thread_id": "task-x",
			"projection": map[string]any{"task_id": "task-x", "lifecycle_state": "unknown"},
		}
	})
	res, err := c.Run(context.Background(), DispatchRequest{TaskID: "task-x", Record: map[string]any{"weird": true}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Projection.IsUnknown() {
		t.Fatalf("expected unknown projection, got %+v", res.Projection)
	}
	if res.Projection.PipelineStage != "" {
		t.Fatalf("unknown projection should leave derived fields empty, got %+v", res.Projection)
	}
}

func TestDispatchClientRun_RequiresTaskID(t *testing.T) {
	t.Parallel()
	c := NewDispatchClient("http://127.0.0.1:1") // never dialed
	if _, err := c.Run(context.Background(), DispatchRequest{Record: map[string]any{}}); err == nil {
		t.Fatal("expected error for empty task_id")
	}
}

func TestDispatchClientResume_DecisionValidation(t *testing.T) {
	t.Parallel()
	c := fakeOrchestrator(t, func(path string, body map[string]any) (int, any) {
		if path != "/resume" {
			t.Errorf("posted to %q, want /resume", path)
		}
		if body["decision"] != DecisionApprove {
			t.Errorf("decision on wire = %v, want approve", body["decision"])
		}
		return http.StatusOK, doneResult("task-4")
	})

	// Valid decision flows through.
	if _, err := c.Resume(context.Background(), ResumeRequest{
		TaskID: "task-4", ThreadID: "task-4", Decision: DecisionApprove,
	}); err != nil {
		t.Fatalf("Resume(approve): %v", err)
	}

	// Invalid decision is rejected client-side without a round-trip.
	if _, err := c.Resume(context.Background(), ResumeRequest{
		TaskID: "task-4", ThreadID: "task-4", Decision: "yolo",
	}); err == nil {
		t.Fatal("expected error for invalid decision")
	}

	// Missing task_id is rejected before any round-trip (mirrors Run).
	if _, err := c.Resume(context.Background(), ResumeRequest{
		ThreadID: "task-4", Decision: DecisionApprove,
	}); err == nil {
		t.Fatal("expected error for empty task_id")
	}

	// Missing thread_id is rejected.
	if _, err := c.Resume(context.Background(), ResumeRequest{TaskID: "task-4", Decision: DecisionApprove}); err == nil {
		t.Fatal("expected error for empty thread_id")
	}
}

func TestDispatchClient_HTTPErrorSurfacesBody(t *testing.T) {
	t.Parallel()
	c := fakeOrchestrator(t, func(_ string, _ map[string]any) (int, any) {
		return http.StatusInternalServerError, `{"detail":"checkpointer exploded"}`
	})
	_, err := c.Run(context.Background(), DispatchRequest{TaskID: "task-5", Record: map[string]any{}})
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "checkpointer exploded") {
		t.Fatalf("error should surface status + body, got: %v", err)
	}
}

func TestDispatchClient_UnexpectedStatus(t *testing.T) {
	t.Parallel()
	c := fakeOrchestrator(t, func(_ string, _ map[string]any) (int, any) {
		return http.StatusOK, map[string]any{
			"status": "thinking", "thread_id": "task-6",
			"projection": map[string]any{"task_id": "task-6", "lifecycle_state": "running"},
		}
	})
	_, err := c.Run(context.Background(), DispatchRequest{TaskID: "task-6", Record: map[string]any{}})
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("expected ErrUnexpectedStatus, got: %v", err)
	}
}

func TestDispatchClient_ContextCancel(t *testing.T) {
	t.Parallel()
	c := fakeOrchestrator(t, func(_ string, _ map[string]any) (int, any) {
		return http.StatusOK, doneResult("task-7")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before dispatch
	if _, err := c.Run(ctx, DispatchRequest{TaskID: "task-7", Record: map[string]any{}}); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestNewDispatchClient_BaseURLResolution(t *testing.T) {
	// Not parallel: t.Setenv forbids it.
	// Explicit wins; trailing slash trimmed.
	if got := NewDispatchClient("http://example/v1/").baseURL; got != "http://example/v1" {
		t.Fatalf("baseURL = %q, want trailing slash trimmed", got)
	}
	// Empty falls back to the loopback default (env not set in this test proc).
	t.Setenv("WUPHF_ORCHESTRATOR_URL", "")
	if got := NewDispatchClient("").baseURL; got != defaultOrchestratorBaseURL {
		t.Fatalf("baseURL = %q, want default %q", got, defaultOrchestratorBaseURL)
	}
	t.Setenv("WUPHF_ORCHESTRATOR_URL", "http://sidecar:9999")
	if got := NewDispatchClient("").baseURL; got != "http://sidecar:9999" {
		t.Fatalf("baseURL = %q, want env override", got)
	}
}

// TestProjectionWireShape locks the JSON tags against runstate.to_projection so
// a Python-side rename can't silently drift the Go decode.
func TestProjectionWireShape(t *testing.T) {
	t.Parallel()
	const fromPython = `{"task_id":"t","lifecycle_state":"approved","pipeline_stage":"ship","review_state":"approved","status":"done","blocked":false}`
	var p Projection
	if err := json.Unmarshal([]byte(fromPython), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := Projection{TaskID: "t", LifecycleState: "approved", PipelineStage: "ship", ReviewState: "approved", Status: "done"}
	if p != want {
		t.Fatalf("projection decode drift: got %+v want %+v", p, want)
	}
}
