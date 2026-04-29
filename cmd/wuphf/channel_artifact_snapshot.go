package main

import (
	"sort"
	"strings"

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
