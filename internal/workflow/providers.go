package workflow

import (
	"context"
	"time"
)

// ActionProvider executes side-effects (Composio actions, broker mutations).
// Implementations are injected at startup and called asynchronously by the
// bubbletea layer — the runtime itself never calls Execute directly.
type ActionProvider interface {
	Execute(ctx context.Context, exec ExecuteSpec, dataStore map[string]any) (map[string]any, error)
}

// AgentDispatcher sends tasks to agents and waits for replies.
type AgentDispatcher interface {
	Dispatch(ctx context.Context, agentSlug string, prompt string) (map[string]any, error)
}

// StateStore persists workflow instance state for resume-after-restart.
type StateStore interface {
	Save(workflowID string, state RuntimeSnapshot) error
	Load(workflowID string) (*RuntimeSnapshot, error)
	Delete(workflowID string) error
}

// RuntimeSnapshot captures the state needed to resume a workflow.
type RuntimeSnapshot struct {
	WorkflowID    string         `json:"workflow_id"`
	CurrentStepID string         `json:"current_step_id"`
	State         RuntimeState   `json:"state"`
	DataStore     map[string]any `json:"data_store"`
	StepHistory   []StepEvent    `json:"step_history"`
	RetryCount    int            `json:"retry_count"`
	SavedAt       time.Time      `json:"saved_at"`
}
