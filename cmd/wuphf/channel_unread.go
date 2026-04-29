package main

func (m *channelModel) noteIncomingMessages(added []brokerMessage) {
	if len(added) == 0 {
		return
	}
	if m.unreadAnchorID == "" {
		m.unreadAnchorID = added[0].ID
	}
	m.unreadCount += len(added)
	m.awaySummary = resolveWorkspaceAwaySummary("", m.unreadCount, m.currentRuntimeSnapshot().Recovery)
}

func (m *channelModel) clearUnreadState() {
	m.unreadCount = 0
	m.unreadAnchorID = ""
	m.awaySummary = ""
}
