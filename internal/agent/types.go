package agent

import "context"

// AgentPhase represents the lifecycle phase of an agent.
type AgentPhase string

const (
	PhaseIdle         AgentPhase = "idle"
	PhaseBuildContext AgentPhase = "build_context"
	PhaseStreamLLM    AgentPhase = "stream_llm"
	PhaseExecuteTool  AgentPhase = "execute_tool"
	PhaseDone         AgentPhase = "done"
	PhaseError        AgentPhase = "error"
)

// BudgetLimit defines token and cost limits for an agent session.
type BudgetLimit struct {
	MaxTokens  int     `json:"maxTokens"`
	MaxCostUsd float64 `json:"maxCostUsd"`
}

// AgentConfig holds static configuration for an agent template.
type AgentConfig struct {
	Slug              string       `json:"slug,omitempty"`
	Name              string       `json:"name"`
	Expertise         []string     `json:"expertise"`
	Personality       string       `json:"personality,omitempty"`
	HeartbeatCron     string       `json:"heartbeatCron,omitempty"`
	Tools             []string     `json:"tools,omitempty"`
	Budget            *BudgetLimit `json:"budget,omitempty"`
	AutoDecideTimeout int          `json:"autoDecideTimeout,omitempty"`
	PermissionMode    string       `json:"permissionMode,omitempty"`
	AllowedTools      []string     `json:"allowedTools,omitempty"`
}

// AgentState holds the runtime state of a running agent.
type AgentState struct {
	Phase         AgentPhase  `json:"phase"`
	Config        AgentConfig `json:"config"`
	SessionID     string      `json:"sessionId,omitempty"`
	CurrentTask   string      `json:"currentTask,omitempty"`
	TaskID        string      `json:"taskId,omitempty"`
	TokensUsed    int         `json:"tokensUsed"`
	CostUsd       float64     `json:"costUsd"`
	LastHeartbeat int64       `json:"lastHeartbeat,omitempty"`
	NextHeartbeat int64       `json:"nextHeartbeat,omitempty"`
	Error         string      `json:"error,omitempty"`
}

// AgentTool is a named tool an agent can invoke.
type AgentTool struct {
	Name        string
	Description string
	Schema      map[string]any
	Execute     func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error)
}

// ToolCall records a single tool invocation and its result.
type ToolCall struct {
	ToolName    string         `json:"toolName"`
	Params      map[string]any `json:"params"`
	Result      string         `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	StartedAt   int64          `json:"startedAt"`
	CompletedAt int64          `json:"completedAt,omitempty"`
}

// SessionEntry is one entry in an agent's session history.
type SessionEntry struct {
	ID        string         `json:"id"`
	ParentID  string         `json:"parentId,omitempty"`
	Type      string         `json:"type"` // "user" | "assistant" | "tool_call" | "tool_result" | "system"
	Content   string         `json:"content"`
	Timestamp int64          `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Message is a single LLM conversation turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChunk is one piece of streamed output from the LLM.
// Type is one of: "text", "tool_call", "error", "thinking", "tool_use", "tool_result", "usage"
type StreamChunk struct {
	Type       string         `json:"type"`
	Content    string         `json:"content,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	ToolParams map[string]any `json:"toolParams,omitempty"`
	ToolUseID  string         `json:"toolUseId,omitempty"` // for tool_use / tool_result correlation
	ToolInput  string         `json:"toolInput,omitempty"` // serialized tool input for display

	// Token counts on Type == "usage" chunks. Providers that surface usage
	// (Claude, OpenAI-compatible local servers with stream_options.include_usage,
	// etc) emit a single trailing chunk per turn with these fields populated.
	// Other chunk types leave them zero.
	InputTokens         int `json:"inputTokens,omitempty"`
	OutputTokens        int `json:"outputTokens,omitempty"`
	CacheReadTokens     int `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens int `json:"cacheCreationTokens,omitempty"`
}

// StreamFn is a function that streams LLM output as a channel of chunks.
type StreamFn func(msgs []Message, tools []AgentTool) <-chan StreamChunk

// EscalationReason tags why an escalation fired so the consumer can format
// the message consistently.
type EscalationReason string

const (
	EscalationStuck         EscalationReason = "stuck"          // no phase change for too many ticks
	EscalationMaxRetries    EscalationReason = "max_retries"    // PhaseError seen too many times for one task
	EscalationCapabilityGap EscalationReason = "capability_gap" // agent identified missing capability needed to proceed
)

// Escalator is a callback invoked when an agent can't make forward progress.
// The consumer should post a heads-up message to the team. Non-blocking from
// the loop's perspective — the loop calls it while holding no locks.
type Escalator func(agentSlug, taskID string, reason EscalationReason, detail string)
