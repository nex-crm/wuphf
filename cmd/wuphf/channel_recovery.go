package main

import (
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) currentRuntimeSnapshot() team.RuntimeSnapshot {
	return team.BuildRuntimeSnapshot(team.RuntimeSnapshotInput{
		Channel:     m.activeChannel,
		SessionMode: m.sessionMode,
		DirectAgent: m.oneOnOneAgentSlug(),
		Tasks:       runtimeTasksFromChannel(m.tasks),
		Requests:    runtimeRequestsFromChannel(m.requests),
		Recent:      runtimeMessagesFromChannel(m.messages, 6),
	})
}

func (m channelModel) buildRecoveryLines(contentWidth int) []renderedLine {
	return buildRecoveryLines(m.currentWorkspaceUIState(), contentWidth, m.tasks, m.requests, m.messages)
}

func (m channelModel) currentAwaySummary() string {
	return m.currentWorkspaceUIState().AwaySummary
}
