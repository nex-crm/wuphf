package main

import "github.com/nex-crm/wuphf/cmd/wuphf/channelui"

func (m *channelModel) noteIncomingMessages(added []channelui.BrokerMessage) {
	if len(added) == 0 {
		return
	}
	if m.unreadAnchorID == "" {
		m.unreadAnchorID = added[0].ID
	}
	m.unreadCount += len(added)
	m.awaySummary = channelui.ResolveWorkspaceAwaySummary("", m.unreadCount, m.currentRuntimeSnapshot().Recovery)
}

func (m *channelModel) clearUnreadState() {
	m.unreadCount = 0
	m.unreadAnchorID = ""
	m.awaySummary = ""
}
