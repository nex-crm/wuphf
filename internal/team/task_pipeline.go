package team

import (
	"fmt"
	"strings"
)

type taskPipelineTemplate struct {
	ID             string
	OpenStage      string
	ActiveStage    string
	ReviewStage    string
	DoneStage      string
	ReviewRequired bool
}

var taskPipelineTemplates = map[string]taskPipelineTemplate{
	"feature":   {ID: "feature", OpenStage: "triage", ActiveStage: "implement", ReviewStage: "review", DoneStage: "ship", ReviewRequired: true},
	"bugfix":    {ID: "bugfix", OpenStage: "triage", ActiveStage: "fix", ReviewStage: "review", DoneStage: "verify", ReviewRequired: true},
	"research":  {ID: "research", OpenStage: "question", ActiveStage: "investigate", ReviewStage: "synthesize", DoneStage: "recommend"},
	"launch":    {ID: "launch", OpenStage: "brief", ActiveStage: "execute", ReviewStage: "review", DoneStage: "ship"},
	"incident":  {ID: "incident", OpenStage: "assess", ActiveStage: "mitigate", ReviewStage: "verify", DoneStage: "postmortem"},
	"follow_up": {ID: "follow_up", OpenStage: "triage", ActiveStage: "act", ReviewStage: "verify", DoneStage: "done"},
}

func inferTaskType(owner, title, details string) string {
	text := strings.ToLower(strings.TrimSpace(owner + " " + title + " " + details))
	switch {
	case containsAnyTaskFragment(text, "bug", "fix", "regression", "broken", "error", "panic", "crash"):
		return "bugfix"
	case containsAnyTaskFragment(text, "incident", "outage", "sev", "mitigate", "hotfix"):
		return "incident"
	case containsAnyTaskFragment(text, "launch", "campaign", "announce", "rollout", "go to market"):
		return "launch"
	case containsAnyTaskFragment(text, "research", "investigate", "evaluate", "compare", "analyze", "audit", "thesis", "framework", "recommend"):
		return "research"
	case containsAnyTaskFragment(text, "feature", "build", "implement", "ship", "signup", "flow"):
		return "feature"
	default:
		return "follow_up"
	}
}

func pipelineTemplate(taskType string) taskPipelineTemplate {
	if template, ok := taskPipelineTemplates[strings.TrimSpace(taskType)]; ok {
		return template
	}
	return taskPipelineTemplates["follow_up"]
}

func taskNeedsStructuredReview(task *teamTask) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") ||
		strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "live_external") {
		return true
	}
	template := pipelineTemplate(task.TaskType)
	if !template.ReviewRequired {
		return false
	}
	return taskWorkRequiresLocalExecution(task.Owner, task.Title, task.Details)
}

func taskDefaultExecutionMode(owner, taskType, title, details string) string {
	task := &teamTask{Owner: owner, TaskType: taskType, Title: title, Details: details}
	if taskRequiresRealExternalExecution(task) {
		return "live_external"
	}
	switch strings.TrimSpace(strings.ToLower(taskType)) {
	case "feature", "bugfix", "incident":
		if taskWorkRequiresLocalExecution(owner, title, details) {
			return "local_worktree"
		}
	}
	return "office"
}

func taskStageForStatus(task *teamTask) string {
	template := pipelineTemplate(task.TaskType)
	switch strings.TrimSpace(task.status) {
	case "in_progress":
		return template.ActiveStage
	case "review":
		return template.ReviewStage
	case "done":
		return template.DoneStage
	default:
		return template.OpenStage
	}
}

func normalizeTaskPlan(task *teamTask) {
	if task == nil {
		return
	}
	if strings.TrimSpace(task.TaskType) == "" {
		task.TaskType = inferTaskType(task.Owner, task.Title, task.Details)
	}
	if strings.TrimSpace(task.PipelineID) == "" {
		task.PipelineID = task.TaskType
	}
	if strings.TrimSpace(task.ExecutionMode) == "" {
		task.ExecutionMode = taskDefaultExecutionMode(task.Owner, task.TaskType, task.Title, task.Details)
	}
	if strings.TrimSpace(task.reviewState) == "" {
		if taskNeedsStructuredReview(task) {
			task.reviewState = "pending_review"
		} else {
			task.reviewState = "not_required"
		}
	}
	if strings.TrimSpace(task.status) == "review" {
		task.reviewState = "ready_for_review"
	}
	if strings.TrimSpace(task.status) == "done" &&
		(task.reviewState == "pending_review" || task.reviewState == "ready_for_review") {
		task.reviewState = "approved"
	}
	task.pipelineStage = taskStageForStatus(task)
}

func requestIsResolvedLocked(requests []humanInterview, requestID string) bool {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false
	}
	for _, req := range requests {
		if strings.TrimSpace(req.ID) != requestID {
			continue
		}
		if req.Answered != nil {
			return true
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		return status == "answered" || status == "canceled" || status == "cancelled"
	}
	return false
}

func containsAnyTaskFragment(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func taskWorkRequiresLocalExecution(owner, title, details string) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{owner, title, details}, " ")))
	return containsAnyTaskFragment(text,
		"eng", "engineer", "developer",
		"repo", "repository", "worktree", "workspace", "filesystem",
		"code", "coding", "implement", "build", "ship",
		"frontend", "backend", "api", "database", "schema", "migration",
		"bug", "fix", "panic", "crash", "compile", "test",
	)
}

func taskRequiresRealExternalExecution(task *teamTask) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{task.Owner, task.Title, task.Details}, " ")))
	if text == "" {
		return false
	}
	if containsAnyTaskFragment(text,
		"mock preview", "preview only", "stub only",
		"local-only", "local only", "repo-only", "repo only",
		"no live write", "no external write", "do not post", "don't post",
		"do not create remotely", "don't create remotely",
	) {
		return false
	}
	if !containsAnyTaskFragment(text,
		"slack", "notion", "google drive", "drive", "discord",
		"calendar", "crm", "hubspot", "salesforce", "airtable",
		"linear", "jira", "confluence", "integration", "connected account",
		"external system", "external tool", "external workflow", "one action",
	) {
		return false
	}
	return containsAnyTaskFragment(text,
		"post", "create", "write", "publish", "send", "join", "search",
		"query", "read", "fetch", "sync", "run", "execute", "trigger",
		"handoff", "proof artifact", "page", "doc", "document", "message",
		"database", "workflow", "fan-out", "fanout",
	)
}

func taskHasMockPreviewStubTestingIntent(task *teamTask) bool {
	if task == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{task.Channel, task.Owner, task.Title, task.Details, task.TaskType, task.PipelineID, task.ExecutionMode}, " ")))
	if text == "" {
		return false
	}
	return containsAnyTaskFragment(text,
		"mock", "preview", "stub", "test", "testing",
		"dry run", "dry-run", "sandbox", "simulate", "simulation",
	)
}

func taskLooksLikeInternalTheater(task *teamTask) bool {
	if task == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{task.Channel, task.Owner, task.Title, task.Details, task.TaskType, task.PipelineID, task.ExecutionMode}, " ")))
	if text == "" {
		return false
	}
	return containsAnyTaskFragment(text,
		"proof artifact", "proof packet", "proof page", "review bundle", "review packet",
		"local artifact", "preview packet", "artifact theater", "eval", "evaluation",
		"blueprint-derived scaffolding", "blueprint derived scaffolding",
		"scaffolding", "scaffold", "rubric", "scorecard", "smoke test",
		"handoff packet", "delivery packet",
		"artifact path", "local path", "reviewable artifact", "reviewable bundle",
		"source-of-truth artifact", "source of truth artifact",
		"blueprint.yaml", "updated blueprint", "template review packet",
	)
}

func taskLooksLikeLiveBusinessObjective(task *teamTask) bool {
	if task == nil {
		return false
	}
	if taskRequiresRealExternalExecution(task) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{task.Channel, task.Owner, task.Title, task.Details, task.TaskType, task.PipelineID, task.ExecutionMode}, " ")))
	if text == "" {
		return false
	}
	return containsAnyTaskFragment(text,
		"launch", "go live", "go-live",
		"end to end", "end-to-end",
		"client", "customer", "customer-facing", "client-facing",
		"revenue", "sales", "deliverable", "publish", "ship", "deploy",
		"production", "live external", "real external", "business objective",
		"client-delivery", "delivery", "fulfillment", "customer-success",
		"marketing", "growth", "publishing", "publish", "content",
		"website", "landing page", "offer", "youtube", "video", "script",
	)
}

func rejectTheaterTaskForLiveBusiness(task *teamTask) error {
	if task == nil {
		return nil
	}
	if !taskLooksLikeLiveBusinessObjective(task) {
		return nil
	}
	if taskHasMockPreviewStubTestingIntent(task) {
		return nil
	}
	if !taskLooksLikeInternalTheater(task) {
		return nil
	}
	return fmt.Errorf("live business task cannot be framed as proof/test/review-bundle/local-artifact theater; mark it mock/preview/stub/testing if that is intentional")
}
