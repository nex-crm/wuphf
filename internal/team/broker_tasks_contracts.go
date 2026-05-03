package team

import "github.com/nex-crm/wuphf/internal/agent"

type TaskListRequest struct {
	StatusFilter string `json:"status,omitempty"`
	MySlug       string `json:"my_slug,omitempty"`
	ViewerSlug   string `json:"viewer_slug,omitempty"`
	Channel      string `json:"channel,omitempty"`
	AllChannels  bool   `json:"all_channels,omitempty"`
	IncludeDone  bool   `json:"include_done,omitempty"`
}

type TaskPostRequest struct {
	Action                       string   `json:"action"`
	Channel                      string   `json:"channel"`
	ID                           string   `json:"id"`
	Title                        string   `json:"title"`
	Details                      string   `json:"details"`
	Owner                        string   `json:"owner"`
	CreatedBy                    string   `json:"created_by"`
	ThreadID                     string   `json:"thread_id"`
	TaskType                     string   `json:"task_type"`
	PipelineID                   string   `json:"pipeline_id"`
	ExecutionMode                string   `json:"execution_mode"`
	ReviewState                  string   `json:"review_state"`
	SourceSignalID               string   `json:"source_signal_id"`
	SourceDecisionID             string   `json:"source_decision_id"`
	WorktreePath                 string   `json:"worktree_path"`
	WorktreeBranch               string   `json:"worktree_branch"`
	DependsOn                    []string `json:"depends_on"`
	MemoryWorkflowOverride       bool     `json:"memory_workflow_override"`
	MemoryWorkflowOverrideActor  string   `json:"memory_workflow_override_actor"`
	MemoryWorkflowOverrideReason string   `json:"memory_workflow_override_reason"`
	OverrideReason               string   `json:"override_reason"`
}

type TaskAckRequest struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	Slug    string `json:"slug"`
}

type TaskPlanRequest struct {
	Channel   string          `json:"channel"`
	CreatedBy string          `json:"created_by"`
	Tasks     []TaskPlanInput `json:"tasks"`
}

type TaskPlanInput struct {
	Title         string   `json:"title"`
	Assignee      string   `json:"assignee"`
	Details       string   `json:"details"`
	TaskType      string   `json:"task_type"`
	ExecutionMode string   `json:"execution_mode"`
	DependsOn     []string `json:"depends_on"`
}

type TaskMemoryWorkflowRequest struct {
	Action     string                   `json:"action"`
	Event      string                   `json:"event"`
	TaskID     string                   `json:"task_id"`
	Actor      string                   `json:"actor"`
	Query      string                   `json:"query"`
	Citations  []ContextCitation        `json:"citations"`
	Artifact   MemoryWorkflowArtifact   `json:"artifact"`
	Artifacts  []MemoryWorkflowArtifact `json:"artifacts"`
	SkipReason string                   `json:"skip_reason"`
}

type TaskResponse struct {
	Task teamTask `json:"task"`
}

type TaskListResponse struct {
	Channel string     `json:"channel,omitempty"`
	Tasks   []teamTask `json:"tasks"`
}

type TaskMemoryWorkflowResponse struct {
	Task    teamTask `json:"task"`
	Updated bool     `json:"updated"`
}

type TaskMemoryWorkflowReconcileResponse struct {
	Report MemoryWorkflowReconcileReport `json:"report"`
}

type AgentLogTasksResponse struct {
	Tasks []agent.TaskLogSummary `json:"tasks"`
}

type AgentLogEntriesResponse struct {
	Task    string               `json:"task"`
	Entries []agent.TaskLogEntry `json:"entries"`
}
