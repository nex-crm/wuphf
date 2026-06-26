package provider

// deepagents.go is the Go side of the Go<->Python seam for the LangGraph
// orchestrator-of-record (the migration plan's P1b). It is the counterpart to
// the orchestrator's FastAPI service (orchestrator/src/orchestrator/service.py):
// the broker hands an authoritative task record to the orchestrator and gets a
// terminal StepResult plus a one-way projection it writes back onto the task.
//
// Why this is a standalone client and not a registered StreamFn Entry: the
// StreamFn seam (internal/agent/types.go) is func(msgs, tools) <-chan chunk — a
// per-turn token stream with no task identity. The orchestrator owns the whole
// task lifecycle and needs the full record to re-hydrate run-state, so a turn
// stream is the wrong shape. The broker routing that decides "this task goes to
// the orchestrator" + the projection write-back into task records live in a
// later slice (internal/team); this file is just the typed transport.
//
// The wire contract mirrors orchestrator/src/orchestrator/wire.py field-for-
// field (snake_case JSON tags). Secrets never cross in the body: MCP env is
// passed by env-var NAME only (McpServer.EnvPassthrough), mirroring
// SlackProviderBinding.BotTokenEnv.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// OrchestratorSchemaVersion is the wire-contract version. It must match
// wire.SCHEMA_VERSION on the Python side; bump both together on a breaking
// change so a mismatched sidecar fails loud instead of silently misreading.
const OrchestratorSchemaVersion = 1

// defaultOrchestratorBaseURL is the loopback address the bundled Python
// orchestrator sidecar listens on. Overridable via WUPHF_ORCHESTRATOR_URL for
// out-of-process / container deployments.
const defaultOrchestratorBaseURL = "http://127.0.0.1:8770"

// Step status values returned by the orchestrator (StepResult.Status).
const (
	StepStatusDone        = "done"        // the step ran to a stable lifecycle state
	StepStatusInterrupted = "interrupted" // a human gate fired; Interrupt is populated
)

// Human-gate decisions accepted by POST /resume (ResumeRequest.Decision).
const (
	DecisionApprove        = "approve"
	DecisionRequestChanges = "request_changes"
	DecisionReject         = "reject"
)

// McpServer names a teammcp (or other MCP) subprocess the orchestrator should
// launch for the inner harness. EnvPassthrough lists ENV VAR NAMES to forward
// to that subprocess (e.g. WUPHF_BROKER_TOKEN) — never the values, which stay
// in process env on both sides of the wire.
type McpServer struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	EnvPassthrough []string `json:"env_passthrough,omitempty"`
}

// DispatchRequest is POST /run's body. Record is the authoritative broker task
// record (the re-hydrate source); it carries lifecycle_state plus the legacy
// 4-tuple, and the orchestrator never trusts a derived state when the field is
// present.
type DispatchRequest struct {
	SchemaVersion int                  `json:"schema_version"`
	TaskID        string               `json:"task_id"`
	Record        map[string]any       `json:"record"`
	Model         string               `json:"model,omitempty"`
	SystemPrompt  string               `json:"system_prompt,omitempty"`
	Messages      []map[string]any     `json:"messages,omitempty"`
	MCP           map[string]McpServer `json:"mcp,omitempty"`
}

// ResumeRequest is POST /resume's body: it resolves a pending human gate on an
// interrupted thread and continues the graph.
type ResumeRequest struct {
	SchemaVersion int    `json:"schema_version"`
	TaskID        string `json:"task_id"`
	ThreadID      string `json:"thread_id"`
	Decision      string `json:"decision"`
}

// Projection is the one-way task-status shape the orchestrator emits and the
// broker persists so the existing web renders unchanged. It mirrors
// runstate.to_projection. A fail-loud unmappable record yields only TaskID +
// LifecycleState=="unknown" (the rest zero) — see IsUnknown.
type Projection struct {
	TaskID         string `json:"task_id"`
	LifecycleState string `json:"lifecycle_state"`
	PipelineStage  string `json:"pipeline_stage"`
	ReviewState    string `json:"review_state"`
	Status         string `json:"status"`
	Blocked        bool   `json:"blocked"`
}

// IsUnknown reports whether the orchestrator failed to map this task to a known
// lifecycle state. The broker must surface these for operator triage and must
// not treat them as a real state (the migration plan's fail-loud rule).
func (p Projection) IsUnknown() bool {
	return p.LifecycleState == "unknown"
}

// StepResult is the terminal summary of one orchestration step. The SSE stream
// (a later slice) carries the same information incrementally; this is what the
// broker persists.
type StepResult struct {
	Status     string         `json:"status"`
	ThreadID   string         `json:"thread_id"`
	Projection Projection     `json:"projection"`
	Interrupt  map[string]any `json:"interrupt,omitempty"`
}

// Coordination action values a CoordinationPlan assigns to each child. They
// mirror the kernel's CoordAction enum (orchestrator/coordination.py).
const (
	CoordStart    = "start"    // pre-execution + deps satisfied: activate to running
	CoordDispatch = "dispatch" // already executing: run a turn
	CoordBlock    = "block"    // a dependency is unresolved: leave blocked
	CoordIdle     = "idle"     // terminal: nothing to do
	CoordAwait    = "await"    // review/decision: waiting on a human
	CoordUnknown  = "unknown"  // unmappable lifecycle: operator triage, never act
)

// CoordinateRequest is POST /coordinate's body: a goal's CHILD records,
// re-hydrated from the broker. Each child must carry task_id + lifecycle_state +
// depends_on (the broker sends DependsOn ∪ BlockedOn so the kernel's release rule
// matches the broker's unblock cascade).
type CoordinateRequest struct {
	SchemaVersion int              `json:"schema_version"`
	GoalID        string           `json:"goal_id"`
	Children      []map[string]any `json:"children"`
}

// CoordinationPlan is POST /coordinate's response: the per-child action the
// broker applies this tick. Cycle, when non-nil, is a dependency-cycle path —
// the broker must fail loud and dispatch nothing.
type CoordinationPlan struct {
	GoalID  string            `json:"goal_id"`
	Actions map[string]string `json:"actions"`
	Ready   []string          `json:"ready"`
	Cycle   []string          `json:"cycle,omitempty"`
}

// ErrUnexpectedStatus is returned when the orchestrator answers with a status
// outside the contract's {done, interrupted}. Sentinel so callers can branch.
var ErrUnexpectedStatus = errors.New("deepagents: unexpected step status")

// DispatchClient talks to the orchestrator sidecar's HTTP surface. It is safe
// for concurrent use (the underlying http.Client is).
type DispatchClient struct {
	baseURL    string
	httpClient *http.Client
}

// DispatchOption configures a DispatchClient.
type DispatchOption func(*DispatchClient)

// WithHTTPClient overrides the HTTP client (tests inject an httptest transport).
func WithHTTPClient(c *http.Client) DispatchOption {
	return func(d *DispatchClient) {
		if c != nil {
			d.httpClient = c
		}
	}
}

// NewDispatchClient builds a client for the orchestrator at baseURL. An empty
// baseURL resolves to WUPHF_ORCHESTRATOR_URL, then the loopback default. Per-
// step deadlines come from the caller's context; the default client has only a
// bounded dial timeout so an unreachable sidecar fails fast instead of hanging
// a dispatch until the OS connect timeout.
func NewDispatchClient(baseURL string, opts ...DispatchOption) *DispatchClient {
	resolved := strings.TrimSpace(baseURL)
	if resolved == "" {
		resolved = strings.TrimSpace(os.Getenv("WUPHF_ORCHESTRATOR_URL"))
	}
	if resolved == "" {
		resolved = defaultOrchestratorBaseURL
	}
	d := &DispatchClient{
		baseURL: strings.TrimRight(resolved, "/"),
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			},
		},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Run dispatches one orchestration step (POST /run): re-hydrate from the
// record, run a turn, return the terminal StepResult. SchemaVersion is stamped
// if the caller left it zero.
func (c *DispatchClient) Run(ctx context.Context, req DispatchRequest) (*StepResult, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, errors.New("deepagents: Run requires a task_id")
	}
	if req.SchemaVersion == 0 {
		req.SchemaVersion = OrchestratorSchemaVersion
	}
	return c.postStep(ctx, "/run", req)
}

// Resume resolves a pending human gate on an interrupted thread (POST /resume)
// and continues the graph to the next stable state.
func (c *DispatchClient) Resume(ctx context.Context, req ResumeRequest) (*StepResult, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, errors.New("deepagents: Resume requires a task_id")
	}
	if strings.TrimSpace(req.ThreadID) == "" {
		return nil, errors.New("deepagents: Resume requires a thread_id")
	}
	switch req.Decision {
	case DecisionApprove, DecisionRequestChanges, DecisionReject:
	default:
		return nil, fmt.Errorf("deepagents: invalid resume decision %q (want %s|%s|%s)",
			req.Decision, DecisionApprove, DecisionRequestChanges, DecisionReject)
	}
	if req.SchemaVersion == 0 {
		req.SchemaVersion = OrchestratorSchemaVersion
	}
	return c.postStep(ctx, "/resume", req)
}

// Coordinate asks the orchestrator for a goal's per-child action plan (POST
// /coordinate). Pure on the orchestrator side: it re-hydrates the child graph,
// returns start/block/dispatch per child plus the ready batch, and surfaces a
// dependency cycle in Cycle. SchemaVersion is stamped if the caller left it zero.
func (c *DispatchClient) Coordinate(ctx context.Context, req CoordinateRequest) (*CoordinationPlan, error) {
	if strings.TrimSpace(req.GoalID) == "" {
		return nil, errors.New("deepagents: Coordinate requires a goal_id")
	}
	if req.SchemaVersion == 0 {
		req.SchemaVersion = OrchestratorSchemaVersion
	}
	raw, err := c.postRaw(ctx, "/coordinate", req)
	if err != nil {
		return nil, err
	}
	var plan CoordinationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("deepagents: decode /coordinate CoordinationPlan: %w", err)
	}
	return &plan, nil
}

// Health probes GET /health. Returns nil when the sidecar is live.
func (c *DispatchClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("deepagents: build health request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deepagents: orchestrator unreachable at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10)) //nolint:errcheck // draining for keep-alive
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deepagents: health HTTP %d from %s", resp.StatusCode, c.baseURL)
	}
	return nil
}

// postStep POSTs body to path and decodes a StepResult, failing loud on a status
// outside the contract or an interrupted result with no payload.
func (c *DispatchClient) postStep(ctx context.Context, path string, body any) (*StepResult, error) {
	raw, err := c.postRaw(ctx, path, body)
	if err != nil {
		return nil, err
	}
	var result StepResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("deepagents: decode %s StepResult: %w", path, err)
	}
	switch result.Status {
	case StepStatusDone, StepStatusInterrupted:
	default:
		return nil, fmt.Errorf("%w: %q from %s%s", ErrUnexpectedStatus, result.Status, c.baseURL, path)
	}
	if result.Status == StepStatusInterrupted && result.Interrupt == nil {
		return nil, fmt.Errorf("deepagents: %s reported interrupted with no interrupt payload", path)
	}
	return &result, nil
}

// postRaw marshals body, POSTs it to path, and returns the (bounded) response
// body. It fails loud on non-2xx, surfacing up to 8 KiB of the server's error
// body. Shared by the StepResult and CoordinationPlan endpoints.
func (c *DispatchClient) postRaw(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("deepagents: marshal %s request: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("deepagents: build %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepagents: POST %s%s: %w (is the orchestrator sidecar running?)", c.baseURL, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("deepagents: read %s response: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 8<<10 {
			snippet = snippet[:8<<10]
		}
		return nil, fmt.Errorf("deepagents: HTTP %d from %s%s: %s", resp.StatusCode, c.baseURL, path, snippet)
	}
	return raw, nil
}
