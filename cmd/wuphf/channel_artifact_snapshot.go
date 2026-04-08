package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

type runtimeArtifactSnapshot struct {
	Items []team.RuntimeArtifact
}

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

func (s runtimeArtifactSnapshot) Count(kinds ...team.RuntimeArtifactKind) int {
	return len(s.Filter(kinds...))
}

func (s runtimeArtifactSnapshot) Filter(kinds ...team.RuntimeArtifactKind) []team.RuntimeArtifact {
	if len(kinds) == 0 {
		return append([]team.RuntimeArtifact(nil), s.Items...)
	}
	set := make(map[team.RuntimeArtifactKind]struct{}, len(kinds))
	for _, kind := range kinds {
		set[kind] = struct{}{}
	}
	out := make([]team.RuntimeArtifact, 0, len(s.Items))
	for _, artifact := range s.Items {
		if _, ok := set[artifact.Kind]; ok {
			out = append(out, artifact)
		}
	}
	return out
}

func (s runtimeArtifactSnapshot) ReviewCandidates(limit int) []team.RuntimeArtifact {
	return prioritizeArtifacts(s.Items, limit, artifactReviewRank)
}

func (s runtimeArtifactSnapshot) ResumeCandidates(limit int) []team.RuntimeArtifact {
	return prioritizeArtifacts(s.Items, limit, artifactResumeRank)
}

type artifactRankFn func(team.RuntimeArtifact) (int, bool)

func prioritizeArtifacts(items []team.RuntimeArtifact, limit int, rankFn artifactRankFn) []team.RuntimeArtifact {
	type rankedArtifact struct {
		artifact team.RuntimeArtifact
		rank     int
		updated  time.Time
	}
	ranked := make([]rankedArtifact, 0, len(items))
	for _, artifact := range items {
		rank, ok := rankFn(artifact)
		if !ok {
			continue
		}
		ranked = append(ranked, rankedArtifact{
			artifact: artifact,
			rank:     rank,
			updated:  parseArtifactTimestamp(artifact.UpdatedAt, artifact.StartedAt),
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rank != ranked[j].rank {
			return ranked[i].rank < ranked[j].rank
		}
		left, right := ranked[i].updated, ranked[j].updated
		switch {
		case !left.IsZero() && !right.IsZero():
			return left.After(right)
		case !left.IsZero():
			return true
		case !right.IsZero():
			return false
		default:
			return ranked[i].artifact.ID > ranked[j].artifact.ID
		}
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]team.RuntimeArtifact, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.artifact)
	}
	return out
}

func artifactReviewRank(artifact team.RuntimeArtifact) (int, bool) {
	if !artifact.Reviewable() {
		return 0, false
	}
	switch artifact.Kind {
	case team.RuntimeArtifactRequest:
		if artifact.Blocking {
			return 0, true
		}
		return 1, true
	case team.RuntimeArtifactTask:
		if artifact.NormalizedState() == "review" {
			return 2, true
		}
		return 3, true
	default:
		return 0, false
	}
}

func artifactResumeRank(artifact team.RuntimeArtifact) (int, bool) {
	if !artifact.Resumable() {
		return 0, false
	}
	state := artifact.NormalizedState()
	switch {
	case state == "blocked" && strings.TrimSpace(artifact.Worktree) != "":
		return 0, true
	case strings.TrimSpace(artifact.Worktree) != "":
		return 1, true
	case strings.TrimSpace(artifact.RelatedID) != "":
		return 2, true
	default:
		return 3, true
	}
}

func recentArtifactTasks(tasks []channelTask, limit int) []channelTask {
	filtered := make([]channelTask, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) == "" && strings.TrimSpace(task.Title) == "" {
			continue
		}
		filtered = append(filtered, task)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left := parseArtifactTimestamp(filtered[i].UpdatedAt, filtered[i].CreatedAt)
		right := parseArtifactTimestamp(filtered[j].UpdatedAt, filtered[j].CreatedAt)
		switch {
		case !left.IsZero() && !right.IsZero():
			return left.After(right)
		case !left.IsZero():
			return true
		case !right.IsZero():
			return false
		default:
			return filtered[i].ID > filtered[j].ID
		}
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
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
		Branch:        strings.TrimSpace(task.WorktreeBranch),
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

func buildRequestRuntimeArtifact(req channelInterview) team.RuntimeArtifact {
	state := normalizeRequestArtifactState(req.Status)
	return team.RuntimeArtifact{
		ID:         strings.TrimSpace(req.ID),
		Kind:       team.RuntimeArtifactRequest,
		Title:      req.TitleOrQuestion(),
		Summary:    fallbackString(strings.TrimSpace(req.Context), strings.TrimSpace(req.Question)),
		State:      state,
		Progress:   requestArtifactProgress(req),
		Owner:      strings.TrimSpace(req.From),
		Channel:    strings.TrimSpace(req.Channel),
		RelatedID:  strings.TrimSpace(req.ReplyTo),
		StartedAt:  strings.TrimSpace(req.CreatedAt),
		UpdatedAt:  latestArtifactTimestamp(req.FollowUpAt, req.ReminderAt, req.RecheckAt, req.DueAt, req.CreatedAt),
		ResumeHint: "Answer the request or reopen it from Recovery.",
		ReviewHint: requestArtifactReviewHint(req),
		Blocking:   req.Blocking || req.Required,
	}
}

func buildActionRuntimeArtifact(action channelAction) team.RuntimeArtifact {
	kind := team.RuntimeArtifactHumanAction
	if strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
		kind = team.RuntimeArtifactExternalAction
	}
	title := strings.TrimSpace(action.Summary)
	if title == "" {
		title = strings.ReplaceAll(strings.TrimSpace(action.Kind), "_", " ")
	}
	return team.RuntimeArtifact{
		ID:         strings.TrimSpace(action.ID),
		Kind:       kind,
		Title:      title,
		Summary:    actionArtifactSummary(action),
		State:      normalizeActionArtifactState(action.Kind),
		Progress:   actionArtifactProgress(action),
		Owner:      strings.TrimSpace(action.Actor),
		Channel:    strings.TrimSpace(action.Channel),
		RelatedID:  fallbackString(strings.TrimSpace(action.RelatedID), strings.TrimSpace(action.DecisionID)),
		StartedAt:  strings.TrimSpace(action.CreatedAt),
		UpdatedAt:  strings.TrimSpace(action.CreatedAt),
		ResumeHint: actionArtifactResumeHint(action),
		ReviewHint: strings.TrimSpace(action.Source),
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
	branch := strings.TrimSpace(task.WorktreeBranch)
	if worktree := strings.TrimSpace(task.WorktreePath); worktree != "" {
		target := worktree
		if branch != "" {
			target += " on " + branch
		}
		switch state {
		case "completed":
			return "Review the retained output or reopen the task thread before reusing " + target + "."
		case "blocked":
			return "Resolve the blocker, then continue in " + target + " or reopen the task thread."
		default:
			return "Resume in " + target + " or reopen the task thread."
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

func requestArtifactProgress(req channelInterview) string {
	parts := make([]string, 0, 3)
	if choice := strings.TrimSpace(req.RecommendedID); choice != "" {
		parts = append(parts, "Recommended: "+choice)
	}
	if due := strings.TrimSpace(req.DueAt); due != "" {
		parts = append(parts, "Due "+prettyRelativeTime(due))
	}
	if followUp := strings.TrimSpace(req.FollowUpAt); followUp != "" {
		parts = append(parts, "Follow-up "+prettyRelativeTime(followUp))
	}
	return strings.Join(parts, " · ")
}

func requestArtifactReviewHint(req channelInterview) string {
	if recommended := strings.TrimSpace(req.RecommendedID); recommended != "" {
		return "Review recommendation " + recommended + " before answering."
	}
	if due := strings.TrimSpace(req.DueAt); due != "" {
		return "Due " + prettyRelativeTime(due)
	}
	return ""
}

func normalizeRequestArtifactState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "open":
		return "pending"
	case "answered", "complete", "completed":
		return "completed"
	case "canceled", "cancelled":
		return "canceled"
	default:
		return strings.TrimSpace(strings.ToLower(status))
	}
}

func actionArtifactSummary(action channelAction) string {
	parts := make([]string, 0, 4)
	if channel := strings.TrimSpace(action.Channel); channel != "" {
		parts = append(parts, "#"+channel)
	}
	if actor := strings.TrimSpace(action.Actor); actor != "" {
		parts = append(parts, "@"+actor)
	}
	if when := strings.TrimSpace(prettyRelativeTime(action.CreatedAt)); when != "" {
		parts = append(parts, when)
	}
	if len(parts) == 0 {
		return "Retained action trace."
	}
	return strings.Join(parts, " · ")
}

func actionArtifactProgress(action channelAction) string {
	if source := strings.TrimSpace(action.Source); source != "" {
		return "Source: " + source
	}
	return ""
}

func actionArtifactResumeHint(action channelAction) string {
	if related := strings.TrimSpace(action.RelatedID); related != "" {
		return "Review the related artifact or thread " + related + "."
	}
	if decision := strings.TrimSpace(action.DecisionID); decision != "" {
		return "Review decision " + decision + " or reopen the related thread."
	}
	return "Review the related thread or action provider details."
}

func normalizeActionArtifactState(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(kind, "failed"), strings.Contains(kind, "error"):
		return "failed"
	case strings.Contains(kind, "canceled"), strings.Contains(kind, "cancelled"):
		return "canceled"
	case strings.Contains(kind, "blocked"), strings.Contains(kind, "waiting"), strings.Contains(kind, "follow_up"):
		return "blocked"
	case strings.Contains(kind, "planned"), strings.Contains(kind, "created"), strings.Contains(kind, "received"), strings.Contains(kind, "started"):
		return "running"
	case strings.Contains(kind, "answered"), strings.Contains(kind, "executed"), strings.Contains(kind, "completed"), strings.Contains(kind, "sent"):
		return "completed"
	default:
		return fallbackString(kind, "running")
	}
}

func latestArtifactTimestamp(candidates ...string) string {
	var latest time.Time
	for _, candidate := range candidates {
		if ts, ok := parseChannelTime(candidate); ok && ts.After(latest) {
			latest = ts
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.Format(time.RFC3339)
}
