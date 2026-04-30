package channelui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// TaskLogRecord is the on-disk JSON shape of a single line in a
// headless task tool log. Captured here so the channel UI can
// project it into a runtime-artifact and so the package-main disk
// readers can share the schema.
type TaskLogRecord struct {
	TaskID      string          `json:"task_id"`
	AgentSlug   string          `json:"agent_slug"`
	ToolName    string          `json:"tool_name"`
	Params      json.RawMessage `json:"params"`
	Result      json.RawMessage `json:"result"`
	Error       json.RawMessage `json:"error"`
	StartedAt   string          `json:"started_at"`
	CompletedAt string          `json:"completed_at"`
}

// TaskLogArtifact is the channel-UI projection of a retained task
// log: the parsed fields from the latest TaskLogRecord plus the
// log file metadata (path, entry count, mtime) and any runtime
// metadata (worktree path, task title) the package-main disk
// reader stitches in from the active task list.
type TaskLogArtifact struct {
	TaskID       string
	AgentSlug    string
	ToolName     string
	Summary      string
	StartedAt    string
	CompletedAt  string
	LogPath      string
	EntryCount   int
	UpdatedAt    time.Time
	WorktreePath string
	TaskTitle    string
}

// WorkflowRunArtifact is the on-disk JSON shape of a workflow
// run row, plus the file path and mtime stitched in by the disk
// reader.
type WorkflowRunArtifact struct {
	Provider    string `json:"provider"`
	WorkflowKey string `json:"workflow_key"`
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
	Path        string
	UpdatedAt   time.Time
}

// SummarizeTaskLogRecord picks the most informative one-line
// summary for a task log record: error wins if present, then
// result, then params (prefixed with "Params: "). Falls back to a
// generic "finished" message when none of the three are populated.
func SummarizeTaskLogRecord(record TaskLogRecord) string {
	if text := SummarizeJSONField(record.Error, 120); text != "" && text != "null" {
		return "Error: " + text
	}
	if text := SummarizeJSONField(record.Result, 160); text != "" && text != "null" {
		return text
	}
	if text := SummarizeJSONField(record.Params, 120); text != "" && text != "null" {
		return "Params: " + text
	}
	return "Tool execution finished."
}

// BuildTaskRuntimeArtifact projects a Task plus its optional
// retained log into a team.RuntimeArtifact for the artifacts view.
// Path / PartialOutput are populated from the log when hasLog is
// true; updatedAt picks the latest of the task and log timestamps.
func BuildTaskRuntimeArtifact(task Task, logArtifact TaskLogArtifact, hasLog bool) team.RuntimeArtifact {
	state := NormalizeTaskArtifactState(task.Status, task.ReviewState)
	reviewHint := BuildTaskArtifactReviewHint(task, logArtifact, hasLog)
	updatedAt := LatestArtifactTimestamp(task.UpdatedAt, task.CreatedAt, logArtifact.CompletedAt, logArtifact.StartedAt, logArtifact.UpdatedAt.Format(time.RFC3339))
	path := ""
	partialOutput := ""
	if hasLog {
		path = strings.TrimSpace(logArtifact.LogPath)
		partialOutput = strings.TrimSpace(logArtifact.Summary)
	}
	return team.RuntimeArtifact{
		ID:            strings.TrimSpace(task.ID),
		Kind:          team.RuntimeArtifactTask,
		Title:         FallbackString(strings.TrimSpace(task.Title), "Task "+FallbackString(task.ID, "artifact")),
		Summary:       BuildTaskArtifactSummary(task, state),
		State:         state,
		Progress:      BuildTaskArtifactProgress(task),
		Owner:         strings.TrimSpace(task.Owner),
		Channel:       strings.TrimSpace(task.Channel),
		RelatedID:     strings.TrimSpace(task.ThreadID),
		StartedAt:     strings.TrimSpace(task.CreatedAt),
		UpdatedAt:     updatedAt,
		Path:          path,
		Worktree:      strings.TrimSpace(task.WorktreePath),
		PartialOutput: partialOutput,
		ResumeHint:    BuildTaskArtifactResumeHint(task, state),
		ReviewHint:    reviewHint,
		Blocking:      state == "blocked",
	}
}

// BuildOrphanTaskLogRuntimeArtifact projects a TaskLogArtifact
// whose owning Task is no longer in the active runtime list into
// a team.RuntimeArtifactTaskLog row. State is "completed" when
// the log records a CompletedAt timestamp; otherwise "running".
func BuildOrphanTaskLogRuntimeArtifact(artifact TaskLogArtifact) team.RuntimeArtifact {
	state := "completed"
	if strings.TrimSpace(artifact.CompletedAt) == "" {
		state = "running"
	}
	reviewHint := ""
	if artifact.EntryCount > 0 {
		reviewHint = fmt.Sprintf("Retained %d log %s.", artifact.EntryCount, PluralizeWord(artifact.EntryCount, "entry", "entries"))
	}
	return team.RuntimeArtifact{
		ID:            strings.TrimSpace(artifact.TaskID),
		Kind:          team.RuntimeArtifactTaskLog,
		Title:         fmt.Sprintf("Task %s log", FallbackString(artifact.TaskID, "artifact")),
		Summary:       "Retained task output from a task that is no longer in the active runtime list.",
		State:         state,
		Owner:         strings.TrimSpace(artifact.AgentSlug),
		StartedAt:     strings.TrimSpace(artifact.StartedAt),
		UpdatedAt:     LatestArtifactTimestamp(artifact.CompletedAt, artifact.StartedAt, artifact.UpdatedAt.Format(time.RFC3339)),
		Path:          strings.TrimSpace(artifact.LogPath),
		Worktree:      strings.TrimSpace(artifact.WorktreePath),
		PartialOutput: strings.TrimSpace(artifact.Summary),
		ResumeHint:    "Inspect the retained log on disk or reopen the task from the office history.",
		ReviewHint:    reviewHint,
	}
}

// BuildWorkflowRuntimeArtifact projects a WorkflowRunArtifact into
// the team.RuntimeArtifact shape. ResumeHint is tweaked for the
// "running" state so the UI hints at waiting for the provider.
func BuildWorkflowRuntimeArtifact(run WorkflowRunArtifact) team.RuntimeArtifact {
	state := NormalizeWorkflowArtifactState(run.Status)
	reviewHint := ""
	if status := strings.TrimSpace(run.Status); status != "" && !strings.EqualFold(status, state) {
		reviewHint = "Provider status: " + status
	}
	resumeHint := "Review the retained run log or rerun the workflow from the provider."
	if state == "running" {
		resumeHint = "Review the retained run log or wait for the provider to finish."
	}
	return team.RuntimeArtifact{
		ID:         FallbackString(strings.TrimSpace(run.RunID), strings.TrimSpace(run.WorkflowKey)),
		Kind:       team.RuntimeArtifactWorkflowRun,
		Title:      FallbackString(strings.TrimSpace(run.WorkflowKey), "workflow"),
		Summary:    fmt.Sprintf("%s via %s", FallbackString(strings.TrimSpace(run.RunID), "run"), FallbackString(strings.TrimSpace(run.Provider), "provider")),
		State:      state,
		Progress:   WorkflowArtifactProgress(run),
		StartedAt:  strings.TrimSpace(run.StartedAt),
		UpdatedAt:  LatestArtifactTimestamp(run.FinishedAt, run.StartedAt, run.UpdatedAt.Format(time.RFC3339)),
		Path:       strings.TrimSpace(run.Path),
		ResumeHint: resumeHint,
		ReviewHint: reviewHint,
	}
}

// BuildTaskArtifactSummary picks a body sentence for a task
// artifact: the task's own Details if non-empty, otherwise a
// state-specific fallback.
func BuildTaskArtifactSummary(task Task, state string) string {
	if details := strings.TrimSpace(task.Details); details != "" {
		return details
	}
	switch state {
	case "blocked":
		return "This task is blocked and needs a human decision, dependency update, or follow-up."
	case "review":
		return "This task is waiting for review, approval, or a final handoff."
	case "completed":
		return "This task finished and keeps its latest output and resume context here."
	default:
		return "This task is retained as a live execution artifact with its current runtime context."
	}
}

// BuildTaskArtifactProgress collects the optional pipeline /
// review / execution / due-date progress strip for a task.
func BuildTaskArtifactProgress(task Task) string {
	parts := make([]string, 0, 4)
	if stage := strings.TrimSpace(task.PipelineStage); stage != "" {
		parts = append(parts, "Stage: "+strings.ReplaceAll(stage, "_", " "))
	}
	if review := strings.TrimSpace(task.ReviewState); review != "" {
		parts = append(parts, "Review: "+strings.ReplaceAll(review, "_", " "))
	}
	if mode := strings.TrimSpace(task.ExecutionMode); mode != "" {
		parts = append(parts, "Execution: "+strings.ReplaceAll(mode, "_", " "))
	}
	if due := strings.TrimSpace(task.DueAt); due != "" {
		parts = append(parts, "Due "+PrettyRelativeTime(due))
	}
	return strings.Join(parts, " · ")
}

// BuildTaskArtifactReviewHint surfaces review guidance for a task
// artifact: the explicit review state, a "Review is the current
// pipeline state." hint when status==review, and an entry-count
// hint when a retained log is present.
func BuildTaskArtifactReviewHint(task Task, logArtifact TaskLogArtifact, hasLog bool) string {
	parts := make([]string, 0, 3)
	if review := strings.TrimSpace(task.ReviewState); review != "" {
		parts = append(parts, "Review "+strings.ReplaceAll(review, "_", " "))
	}
	if strings.EqualFold(strings.TrimSpace(task.Status), "review") {
		parts = append(parts, "Review is the current pipeline state.")
	}
	if hasLog && logArtifact.EntryCount > 0 {
		parts = append(parts, fmt.Sprintf("Retained %d log %s.", logArtifact.EntryCount, PluralizeWord(logArtifact.EntryCount, "entry", "entries")))
	}
	return strings.Join(parts, " · ")
}

// BuildTaskArtifactResumeHint suggests where the human should
// resume work on a task: in the worktree (with state-specific
// wording when blocked / completed), in the originating thread, or
// in the Tasks view.
func BuildTaskArtifactResumeHint(task Task, state string) string {
	if worktree := strings.TrimSpace(task.WorktreePath); worktree != "" {
		switch state {
		case "completed":
			return "Review the retained output or reopen the task thread before reusing the worktree."
		case "blocked":
			return "Resolve the blocker, then continue in " + worktree + " or reopen the task thread."
		default:
			return "Resume in " + worktree + " or reopen the task thread."
		}
	}
	if thread := strings.TrimSpace(task.ThreadID); thread != "" {
		return "Resume from thread " + thread + " or reopen the task in Tasks."
	}
	return "Reopen the task in Tasks to continue or review it."
}

// NormalizeTaskArtifactState canonicalizes a Task.Status (with a
// ReviewState assist) into one of the artifact-state strings:
// completed / blocked / review / started / running. Unknown
// statuses pass through (lower-cased + trimmed).
func NormalizeTaskArtifactState(status, reviewState string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "completed":
		return "completed"
	case "blocked":
		return "blocked"
	case "review":
		return "review"
	case "open", "queued", "pending":
		return "started"
	case "", "running", "in_progress":
		if strings.EqualFold(strings.TrimSpace(reviewState), "ready_for_review") || strings.EqualFold(strings.TrimSpace(reviewState), "pending_review") {
			return "review"
		}
		return "running"
	default:
		return strings.TrimSpace(strings.ToLower(status))
	}
}

// WorkflowArtifactProgress collects the optional provider + raw
// status progress strip for a workflow artifact.
func WorkflowArtifactProgress(run WorkflowRunArtifact) string {
	parts := []string{}
	if provider := strings.TrimSpace(run.Provider); provider != "" {
		parts = append(parts, "Provider: "+provider)
	}
	if rawStatus := strings.TrimSpace(run.Status); rawStatus != "" {
		parts = append(parts, "Raw status: "+rawStatus)
	}
	return strings.Join(parts, " · ")
}

// NormalizeWorkflowArtifactState classifies a workflow status
// string into one of the artifact-state strings: completed /
// failed / running. Unknown statuses pass through; empty status
// is treated as completed since the row would otherwise be
// dropped from the artifacts view.
func NormalizeWorkflowArtifactState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "done", "completed", "finished":
		return "completed"
	case "failed", "error":
		return "failed"
	case "queued", "pending", "running", "in_progress", "started":
		return "running"
	case "":
		return "completed"
	default:
		return strings.TrimSpace(strings.ToLower(status))
	}
}
