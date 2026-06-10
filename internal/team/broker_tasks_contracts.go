package team

import "github.com/nex-crm/wuphf/internal/agent"

type TaskListRequest struct {
	StatusFilter string `json:"status,omitempty"`
	MySlug       string `json:"my_slug,omitempty"`
	ViewerSlug   string `json:"viewer_slug,omitempty"`
	Channel      string `json:"channel,omitempty"`
	AllChannels  bool   `json:"all_channels,omitempty"`
	IncludeDone  bool   `json:"include_done,omitempty"`
	// ParentIssueID filters to sub-issues of a specific parent Issue.
	// When empty, no parent filter is applied. When non-empty, only
	// tasks whose ParentIssueID matches are returned. Used by the
	// Issue detail surface to render the sub-issues list.
	ParentIssueID string `json:"parent_issue_id,omitempty"`
}

type TaskPostRequest struct {
	Action           string   `json:"action"`
	Channel          string   `json:"channel"`
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Details          string   `json:"details"`
	Owner            string   `json:"owner"`
	CreatedBy        string   `json:"created_by"`
	ThreadID         string   `json:"thread_id"`
	TaskType         string   `json:"task_type"`
	PipelineID       string   `json:"pipeline_id"`
	ExecutionMode    string   `json:"execution_mode"`
	ReviewState      string   `json:"review_state"`
	SourceSignalID   string   `json:"source_signal_id"`
	SourceDecisionID string   `json:"source_decision_id"`
	WorktreePath     string   `json:"worktree_path"`
	WorktreeBranch   string   `json:"worktree_branch"`
	DependsOn        []string `json:"depends_on"`
	ParentIssueID    string   `json:"parent_issue_id"`
	// VerificationKind/Spec/Required set the machine-checkable definition
	// of done on create (U1.1, task_verification.go). Kind is one of
	// command|artifact|url|none.
	VerificationKind     string `json:"verification_kind,omitempty"`
	VerificationSpec     string `json:"verification_spec,omitempty"`
	VerificationRequired bool   `json:"verification_required,omitempty"`
	// Definition is the R4 structured intake contract for action=define
	// (task_definition.go): goal (required), deliverables (+format),
	// success_criteria (entries non-empty), access_needed. CEO/human only —
	// same auth class as the other scope-shaping actions. When a success
	// criterion is machine-checkable and the task has no verification yet,
	// pass VerificationKind/Spec/Required in the same define call.
	Definition *TaskDefinition `json:"definition,omitempty"`
	// ArtifactPath records the delivered artifact on the task (core-loop B1):
	// a wiki-relative path (e.g. "team/playbooks/launch.md") or a
	// visual-artifact id. Pass it on complete/approve (or any earlier
	// mutation). A task WITH a Definition cannot reach done until an
	// artifact is recorded; the gate returns TaskMutationArtifactRequired
	// explaining exactly this field.
	ArtifactPath                 string `json:"artifact_path,omitempty"`
	MemoryWorkflowOverride       bool   `json:"memory_workflow_override"`
	MemoryWorkflowOverrideActor  string `json:"memory_workflow_override_actor"`
	MemoryWorkflowOverrideReason string `json:"memory_workflow_override_reason"`
	OverrideReason               string `json:"override_reason"`
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
	Title         string `json:"title"`
	Assignee      string `json:"assignee"`
	Details       string `json:"details"`
	TaskType      string `json:"task_type"`
	ExecutionMode string `json:"execution_mode"`
	// Effort is the optional model-specific reasoning-effort level chosen in
	// the new-task composer (e.g. "high" for claude, "medium" for codex). It
	// is stored on the task and applied at dispatch. Empty means default.
	Effort string `json:"effort"`
	// Provider and Model are the optional per-task LLM runtime override
	// (runtime kind + model id). Stored on the task; dispatch prefers them
	// over the owner agent's binding. Empty means inherit the binding/default.
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Park, when true, creates the task ASSIGNED but parked in the backlog
	// (non-executable) instead of dispatching the owner now. The "Backlog"
	// composer action sets this; "Start now" leaves it false.
	Park      bool     `json:"park"`
	DependsOn []string `json:"depends_on"`
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
