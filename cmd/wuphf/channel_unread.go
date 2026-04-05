package main

import (
	"fmt"
	"strings"
)

func (m *channelModel) noteIncomingMessages(added []brokerMessage) {
	if len(added) == 0 {
		return
	}
	if m.unreadAnchorID == "" {
		m.unreadAnchorID = added[0].ID
	}
	m.unreadCount += len(added)
	m.awaySummary = summarizeUnreadMessages(added)
}

func (m *channelModel) clearUnreadState() {
	m.unreadCount = 0
	m.unreadAnchorID = ""
	m.awaySummary = ""
}

func summarizeUnreadMessages(messages []brokerMessage) string {
	if len(messages) == 0 {
		return ""
	}
	names := []string{}
	seen := map[string]bool{}
	for _, msg := range messages {
		if strings.TrimSpace(msg.From) == "" || seen[msg.From] {
			continue
		}
		seen[msg.From] = true
		names = append(names, displayName(msg.From))
		if len(names) == 3 {
			break
		}
	}
	switch len(names) {
	case 0:
		return fmt.Sprintf("%d new messages", len(messages))
	case 1:
		return fmt.Sprintf("%d new from %s", len(messages), names[0])
	case 2:
		return fmt.Sprintf("%d new from %s and %s", len(messages), names[0], names[1])
	default:
		return fmt.Sprintf("%d new from %s, %s, and %s", len(messages), names[0], names[1], names[2])
	}
}
