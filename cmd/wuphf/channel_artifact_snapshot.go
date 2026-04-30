package main

import (
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) currentArtifactSnapshot(limit int) channelui.RuntimeArtifactSnapshot {
	taskLogs := m.recentTaskLogArtifacts(channelui.MaxInt(limit, 12))
	taskLogsByID := make(map[string]channelui.TaskLogArtifact, len(taskLogs))
	for _, artifact := range taskLogs {
		if id := strings.TrimSpace(artifact.TaskID); id != "" {
			taskLogsByID[id] = artifact
		}
	}

	artifacts := make([]team.RuntimeArtifact, 0, len(m.tasks)+len(taskLogs)+len(m.requests)+len(m.actions)+8)
	for _, task := range channelui.RecentArtifactTasks(m.tasks, channelui.MaxInt(limit, 12)) {
		logArtifact, ok := taskLogsByID[strings.TrimSpace(task.ID)]
		if ok {
			delete(taskLogsByID, strings.TrimSpace(task.ID))
		}
		artifacts = append(artifacts, channelui.BuildTaskRuntimeArtifact(task, logArtifact, ok))
	}
	for _, orphan := range taskLogs {
		if _, ok := taskLogsByID[strings.TrimSpace(orphan.TaskID)]; !ok {
			continue
		}
		artifacts = append(artifacts, channelui.BuildOrphanTaskLogRuntimeArtifact(orphan))
	}
	for _, run := range recentWorkflowRunArtifacts(channelui.MaxInt(limit, 8)) {
		artifacts = append(artifacts, channelui.BuildWorkflowRuntimeArtifact(run))
	}
	for _, req := range channelui.RecentHumanArtifactRequests(m.requests, channelui.MaxInt(limit, 8)) {
		artifacts = append(artifacts, channelui.BuildRequestRuntimeArtifact(req))
	}
	for _, action := range channelui.RecentExecutionArtifactActions(m.actions, channelui.MaxInt(limit, 8)) {
		artifacts = append(artifacts, channelui.BuildActionRuntimeArtifact(action))
	}

	sort.SliceStable(artifacts, func(i, j int) bool {
		left := channelui.ParseArtifactTimestamp(artifacts[i].UpdatedAt, artifacts[i].StartedAt)
		right := channelui.ParseArtifactTimestamp(artifacts[j].UpdatedAt, artifacts[j].StartedAt)
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
	return channelui.RuntimeArtifactSnapshot{Items: artifacts}
}
