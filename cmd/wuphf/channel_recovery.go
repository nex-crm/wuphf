package main

import (
	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) currentRuntimeSnapshot() team.RuntimeSnapshot {
	return team.BuildRuntimeSnapshot(team.RuntimeSnapshotInput{
		Channel:     m.activeChannel,
		SessionMode: m.sessionMode,
		DirectAgent: m.oneOnOneAgentSlug(),
		Tasks:       channelui.RuntimeTasksFromChannel(m.tasks),
		Requests:    channelui.RuntimeRequestsFromChannel(m.requests),
		Recent:      channelui.RuntimeMessagesFromChannel(m.messages, 6),
	})
}

func (m channelModel) buildRecoveryLines(contentWidth int) []channelui.RenderedLine {
	return channelui.BuildRecoveryLines(m.currentWorkspaceUIState(), contentWidth, m.tasks, m.requests, m.messages)
}

func (m channelModel) currentAwaySummary() string {
	return m.currentWorkspaceUIState().AwaySummary
}
