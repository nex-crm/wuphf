package team

import "strings"

type RuntimeArtifactKind string

const (
	RuntimeArtifactTask           RuntimeArtifactKind = "task"
	RuntimeArtifactTaskLog        RuntimeArtifactKind = "task_log"
	RuntimeArtifactWorkflowRun    RuntimeArtifactKind = "workflow_run"
	RuntimeArtifactRequest        RuntimeArtifactKind = "request"
	RuntimeArtifactHumanAction    RuntimeArtifactKind = "human_action"
	RuntimeArtifactExternalAction RuntimeArtifactKind = "external_action"
)

type RuntimeArtifact struct {
	ID            string
	Kind          RuntimeArtifactKind
	Title         string
	Summary       string
	State         string
	Progress      string
	Owner         string
	Channel       string
	RelatedID     string
	StartedAt     string
	UpdatedAt     string
	Path          string
	Worktree      string
	PartialOutput string
	ResumeHint    string
	ReviewHint    string
	Blocking      bool
}

func (a RuntimeArtifact) EffectiveTitle() string {
	title := strings.TrimSpace(a.Title)
	if title != "" {
		return title
	}
	if id := strings.TrimSpace(a.ID); id != "" {
		return id
	}
	return "artifact"
}

func (a RuntimeArtifact) EffectiveSummary() string {
	summary := strings.TrimSpace(a.Summary)
	if summary != "" {
		return summary
	}
	if progress := strings.TrimSpace(a.Progress); progress != "" {
		return progress
	}
	if state := strings.TrimSpace(a.State); state != "" {
		return strings.ReplaceAll(state, "_", " ")
	}
	return "No summary yet."
}

func (a RuntimeArtifact) EffectiveProgress() string {
	progress := strings.TrimSpace(a.Progress)
	if progress != "" {
		return progress
	}
	return strings.ReplaceAll(strings.TrimSpace(a.State), "_", " ")
}
