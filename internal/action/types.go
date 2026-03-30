package action

import (
	"context"
	"encoding/json"
)

type Capability string

const (
	CapabilityGuide            Capability = "guide"
	CapabilityConnections      Capability = "connections"
	CapabilityActionSearch     Capability = "action_search"
	CapabilityActionKnowledge  Capability = "action_knowledge"
	CapabilityActionExecute    Capability = "action_execute"
	CapabilityWorkflowCreate   Capability = "workflow_create"
	CapabilityWorkflowExecute  Capability = "workflow_execute"
	CapabilityWorkflowRuns     Capability = "workflow_runs"
	CapabilityRelayList        Capability = "relay_list"
	CapabilityRelayEventTypes  Capability = "relay_event_types"
	CapabilityRelayCreate      Capability = "relay_create"
	CapabilityRelayActivate    Capability = "relay_activate"
	CapabilityRelayEvents      Capability = "relay_events"
	CapabilityRelayEvent       Capability = "relay_event"
)

// Provider exposes a provider-agnostic action plane for external systems.
type Provider interface {
	Name() string
	Configured() bool
	Supports(Capability) bool
	Guide(ctx context.Context, topic string) (GuideResult, error)
	ListConnections(ctx context.Context, opts ListConnectionsOptions) (ConnectionsResult, error)
	SearchActions(ctx context.Context, platform, query, mode string) (ActionSearchResult, error)
	ActionKnowledge(ctx context.Context, platform, actionID string) (KnowledgeResult, error)
	ExecuteAction(ctx context.Context, req ExecuteRequest) (ExecuteResult, error)
	CreateWorkflow(ctx context.Context, req WorkflowCreateRequest) (WorkflowCreateResult, error)
	ExecuteWorkflow(ctx context.Context, req WorkflowExecuteRequest) (WorkflowExecuteResult, error)
	ListWorkflowRuns(ctx context.Context, key string) (WorkflowRunsResult, error)
	ListRelays(ctx context.Context, opts ListRelaysOptions) (RelayListResult, error)
	RelayEventTypes(ctx context.Context, platform string) (RelayEventTypesResult, error)
	CreateRelay(ctx context.Context, req RelayCreateRequest) (RelayResult, error)
	ActivateRelay(ctx context.Context, req RelayActivateRequest) (RelayResult, error)
	ListRelayEvents(ctx context.Context, opts RelayEventsOptions) (RelayEventsResult, error)
	GetRelayEvent(ctx context.Context, id string) (RelayEventDetail, error)
}

type GuideResult struct {
	Topic  string          `json:"topic,omitempty"`
	Guide  string          `json:"guide,omitempty"`
	Raw    json.RawMessage `json:"raw,omitempty"`
	Topics []string        `json:"topics,omitempty"`
}

type ListConnectionsOptions struct {
	Search string
	Limit  int
}

type Connection struct {
	Platform string   `json:"platform"`
	State    string   `json:"state,omitempty"`
	Key      string   `json:"key"`
	Name     string   `json:"name,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type ConnectionsResult struct {
	Total       int          `json:"total,omitempty"`
	Showing     int          `json:"showing,omitempty"`
	Search      string       `json:"search,omitempty"`
	Hint        string       `json:"hint,omitempty"`
	Connections []Connection `json:"connections"`
}

type Action struct {
	ActionID string `json:"action_id"`
	Title    string `json:"title,omitempty"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
}

type ActionSearchResult struct {
	Platform string   `json:"platform,omitempty"`
	Query    string   `json:"query,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	Actions  []Action `json:"actions"`
}

type KnowledgeResult struct {
	Platform  string `json:"platform,omitempty"`
	ActionID  string `json:"action_id,omitempty"`
	Method    string `json:"method,omitempty"`
	Knowledge string `json:"knowledge"`
}

type ExecuteRequest struct {
	Platform        string         `json:"platform"`
	ActionID        string         `json:"action_id"`
	ConnectionKey   string         `json:"connection_key"`
	Data            map[string]any `json:"data,omitempty"`
	PathVariables   map[string]any `json:"path_variables,omitempty"`
	QueryParameters map[string]any `json:"query_parameters,omitempty"`
	Headers         map[string]any `json:"headers,omitempty"`
	FormData        bool           `json:"form_data,omitempty"`
	FormURLEncoded  bool           `json:"form_url_encoded,omitempty"`
	DryRun          bool           `json:"dry_run,omitempty"`
}

type ExecuteResult struct {
	DryRun   bool            `json:"dry_run"`
	Request  ExecuteEnvelope `json:"request"`
	Response json.RawMessage `json:"response,omitempty"`
}

type ExecuteEnvelope struct {
	Method  string         `json:"method,omitempty"`
	URL     string         `json:"url,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type WorkflowCreateRequest struct {
	Key        string          `json:"key"`
	Definition json.RawMessage `json:"definition"`
}

type WorkflowCreateResult struct {
	Created bool   `json:"created"`
	Key     string `json:"key,omitempty"`
	Path    string `json:"path,omitempty"`
}

type WorkflowExecuteRequest struct {
	KeyOrPath string         `json:"key_or_path"`
	Inputs    map[string]any `json:"inputs,omitempty"`
	DryRun    bool           `json:"dry_run,omitempty"`
	Verbose   bool           `json:"verbose,omitempty"`
	Mock      bool           `json:"mock,omitempty"`
	AllowBash bool           `json:"allow_bash,omitempty"`
}

type WorkflowExecuteResult struct {
	RunID   string                     `json:"run_id,omitempty"`
	LogFile string                     `json:"log_file,omitempty"`
	Status  string                     `json:"status,omitempty"`
	Steps   map[string]json.RawMessage `json:"steps,omitempty"`
	Events  []json.RawMessage          `json:"events,omitempty"`
}

type WorkflowRunsResult struct {
	Runs []json.RawMessage `json:"runs,omitempty"`
	Raw  json.RawMessage   `json:"raw,omitempty"`
}

type ListRelaysOptions struct {
	Limit int
	Page  int
}

type Relay struct {
	ID           string   `json:"id"`
	URL          string   `json:"url,omitempty"`
	Active       bool     `json:"active,omitempty"`
	Description  string   `json:"description,omitempty"`
	EventFilters []string `json:"event_filters,omitempty"`
	ActionsCount int      `json:"actions_count,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
}

type RelayListResult struct {
	Total     int     `json:"total,omitempty"`
	Showing   int     `json:"showing,omitempty"`
	Endpoints []Relay `json:"endpoints"`
}

type RelayEventTypesResult struct {
	Platform   string   `json:"platform"`
	EventTypes []string `json:"event_types"`
}

type RelayCreateRequest struct {
	ConnectionKey string   `json:"connection_key"`
	Description   string   `json:"description,omitempty"`
	EventFilters  []string `json:"event_filters,omitempty"`
	CreateWebhook bool     `json:"create_webhook,omitempty"`
}

type RelayActivateRequest struct {
	ID            string          `json:"id"`
	Actions       json.RawMessage `json:"actions"`
	WebhookSecret string          `json:"webhook_secret,omitempty"`
}

type RelayResult struct {
	ID           string          `json:"id"`
	URL          string          `json:"url,omitempty"`
	Active       bool            `json:"active,omitempty"`
	Description  string          `json:"description,omitempty"`
	EventFilters []string        `json:"event_filters,omitempty"`
	Actions      json.RawMessage `json:"actions,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

type RelayEventsOptions struct {
	Limit     int    `json:"limit,omitempty"`
	Page      int    `json:"page,omitempty"`
	Platform  string `json:"platform,omitempty"`
	EventType string `json:"event_type,omitempty"`
	After     string `json:"after,omitempty"`
	Before    string `json:"before,omitempty"`
}

type RelayEvent struct {
	ID        string `json:"id"`
	Platform  string `json:"platform,omitempty"`
	EventType string `json:"event_type,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type RelayEventsResult struct {
	Total   int          `json:"total,omitempty"`
	Showing int          `json:"showing,omitempty"`
	Events  []RelayEvent `json:"events"`
}

type RelayEventDetail struct {
	ID        string          `json:"id"`
	Platform  string          `json:"platform,omitempty"`
	EventType string          `json:"event_type,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}
