package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) currentArtifactSnapshot(limit int) runtimeArtifactSnapshot {
	taskLogs := m.recentTaskLogArtifacts(maxInt(limit, 12))
	taskLogsByID := make(map[string]taskLogArtifact, len(taskLogs))
	for _, artifact := range taskLogs {
		if id := strings.TrimSpace(artifact.TaskID); id != "" {
			taskLogsByID[id] = artifact
		}
	}

	artifacts := make([]team.RuntimeArtifact, 0, len(m.tasks)+len(taskLogs)+len(m.requests)+len(m.actions)+8)
	for _, task := range recentArtifactTasks(m.tasks, maxInt(limit, 12)) {
		logArtifact, ok := taskLogsByID[strings.TrimSpace(task.ID)]
		if ok {
			delete(taskLogsByID, strings.TrimSpace(task.ID))
		}
		artifacts = append(artifacts, buildTaskRuntimeArtifact(task, logArtifact, ok))
	}
	for _, orphan := range taskLogs {
		if _, ok := taskLogsByID[strings.TrimSpace(orphan.TaskID)]; !ok {
			continue
		}
		artifacts = append(artifacts, buildOrphanTaskLogRuntimeArtifact(orphan))
	}
	for _, run := range recentWorkflowRunArtifacts(maxInt(limit, 8)) {
		artifacts = append(artifacts, buildWorkflowRuntimeArtifact(run))
	}
	for _, req := range recentHumanArtifactRequests(m.requests, maxInt(limit, 8)) {
		artifacts = append(artifacts, buildRequestRuntimeArtifact(req))
	}
	for _, action := range recentExecutionArtifactActions(m.actions, maxInt(limit, 8)) {
		artifacts = append(artifacts, buildActionRuntimeArtifact(action))
	}

	sort.SliceStable(artifacts, func(i, j int) bool {
		left := parseArtifactTimestamp(artifacts[i].UpdatedAt, artifacts[i].StartedAt)
		right := parseArtifactTimestamp(artifacts[j].UpdatedAt, artifacts[j].StartedAt)
		switch {
		case !left.IsZero() && !right.IsZero():
			return left.After(right)
		case !left.IsZero():
			return true
		case !right.IsZero():
			return false
		default:
			return artifacts[i].ID > artifacts[j].ID
		}
	})
	if limit > 0 && len(artifacts) > limit {
		artifacts = artifacts[:limit]
	}
	return runtimeArtifactSnapshot{Items: artifacts}
}

func buildTaskRuntimeArtifact(task channelTask, logArtifact taskLogArtifact, hasLog bool) team.RuntimeArtifact {
	state := normalizeTaskArtifactState(task.Status, task.ReviewState)
	reviewHint := buildTaskArtifactReviewHint(task, logArtifact, hasLog)
	updatedAt := latestArtifactTimestamp(task.UpdatedAt, task.CreatedAt, logArtifact.CompletedAt, logArtifact.StartedAt, logArtifact.UpdatedAt.Format(time.RFC3339))
	path := ""
	partialOutput := ""
	if hasLog {
		path = strings.TrimSpace(logArtifact.LogPath)
		partialOutput = strings.TrimSpace(logArtifact.Summary)
	}
	return team.RuntimeArtifact{
		ID:            strings.TrimSpace(task.ID),
		Kind:          team.RuntimeArtifactTask,
		Title:         fallbackString(strings.TrimSpace(task.Title), "Task "+fallbackString(task.ID, "artifact")),
		Summary:       buildTaskArtifactSummary(task, state),
		State:         state,
		Progress:      buildTaskArtifactProgress(task),
		Owner:         strings.TrimSpace(task.Owner),
		Channel:       strings.TrimSpace(task.Channel),
		RelatedID:     strings.TrimSpace(task.ThreadID),
		StartedAt:     strings.TrimSpace(task.CreatedAt),
		UpdatedAt:     updatedAt,
		Path:          path,
		Worktree:      strings.TrimSpace(task.WorktreePath),
		PartialOutput: partialOutput,
		ResumeHint:    buildTaskArtifactResumeHint(task, state),
		ReviewHint:    reviewHint,
		Blocking:      state == "blocked",
	}
}

func buildOrphanTaskLogRuntimeArtifact(artifact taskLogArtifact) team.RuntimeArtifact {
	state := "completed"
	if strings.TrimSpace(artifact.CompletedAt) == "" {
		state = "running"
	}
	reviewHint := ""
	if artifact.EntryCount > 0 {
		reviewHint = fmt.Sprintf("Retained %d log %s.", artifact.EntryCount, pluralizeWord(artifact.EntryCount, "entry", "entries"))
	}
	return team.RuntimeArtifact{
		ID:            strings.TrimSpace(artifact.TaskID),
		Kind:          team.RuntimeArtifactTaskLog,
		Title:         fmt.Sprintf("Task %s log", fallbackString(artifact.TaskID, "artifact")),
		Summary:       "Retained task output from a task that is no longer in the active runtime list.",
		State:         state,
		Owner:         strings.TrimSpace(artifact.AgentSlug),
		StartedAt:     strings.TrimSpace(artifact.StartedAt),
		UpdatedAt:     latestArtifactTimestamp(artifact.CompletedAt, artifact.StartedAt, artifact.UpdatedAt.Format(time.RFC3339)),
		Path:          strings.TrimSpace(artifact.LogPath),
		Worktree:      strings.TrimSpace(artifact.WorktreePath),
		PartialOutput: strings.TrimSpace(artifact.Summary),
		ResumeHint:    "Inspect the retained log on disk or reopen the task from the office history.",
		ReviewHint:    reviewHint,
	}
}

func buildWorkflowRuntimeArtifact(run workflowRunArtifact) team.RuntimeArtifact {
	state := normalizeWorkflowArtifactState(run.Status)
	reviewHint := ""
	if status := strings.TrimSpace(run.Status); status != "" && !strings.EqualFold(status, state) {
		reviewHint = "Provider status: " + status
	}
	resumeHint := "Review the retained run log or rerun the workflow from the provider."
	if state == "running" {
		resumeHint = "Review the retained run log or wait for the provider to finish."
	}
	return team.RuntimeArtifact{
		ID:         fallbackString(strings.TrimSpace(run.RunID), strings.TrimSpace(run.WorkflowKey)),
		Kind:       team.RuntimeArtifactWorkflowRun,
		Title:      fallbackString(strings.TrimSpace(run.WorkflowKey), "workflow"),
		Summary:    fmt.Sprintf("%s via %s", fallbackString(strings.TrimSpace(run.RunID), "run"), fallbackString(strings.TrimSpace(run.Provider), "provider")),
		State:      state,
		Progress:   workflowArtifactProgress(run),
		StartedAt:  strings.TrimSpace(run.StartedAt),
		UpdatedAt:  latestArtifactTimestamp(run.FinishedAt, run.StartedAt, run.UpdatedAt.Format(time.RFC3339)),
		Path:       strings.TrimSpace(run.Path),
		ResumeHint: resumeHint,
		ReviewHint: reviewHint,
	}
}

func buildTaskArtifactSummary(task channelTask, state string) string {
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

func buildTaskArtifactProgress(task channelTask) string {
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
		parts = append(parts, "Due "+prettyRelativeTime(due))
	}
	return strings.Join(parts, " · ")
}

func buildTaskArtifactReviewHint(task channelTask, logArtifact taskLogArtifact, hasLog bool) string {
	parts := make([]string, 0, 3)
	if review := strings.TrimSpace(task.ReviewState); review != "" {
		parts = append(parts, "Review "+strings.ReplaceAll(review, "_", " "))
	}
	if strings.EqualFold(strings.TrimSpace(task.Status), "review") {
		parts = append(parts, "Review is the current pipeline state.")
	}
	if hasLog && logArtifact.EntryCount > 0 {
		parts = append(parts, fmt.Sprintf("Retained %d log %s.", logArtifact.EntryCount, pluralizeWord(logArtifact.EntryCount, "entry", "entries")))
	}
	return strings.Join(parts, " · ")
}

func buildTaskArtifactResumeHint(task channelTask, state string) string {
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

func normalizeTaskArtifactState(status, reviewState string) string {
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

func workflowArtifactProgress(run workflowRunArtifact) string {
	parts := []string{}
	if provider := strings.TrimSpace(run.Provider); provider != "" {
		parts = append(parts, "Provider: "+provider)
	}
	if rawStatus := strings.TrimSpace(run.Status); rawStatus != "" {
		parts = append(parts, "Raw status: "+rawStatus)
	}
	return strings.Join(parts, " · ")
}

func normalizeWorkflowArtifactState(status string) string {
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
