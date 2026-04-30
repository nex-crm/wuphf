package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Sidebar state and selection logic. Owns:
//   - the sidebarItem type
//   - the projection from channelModel state to []sidebarItem
//   - cursor management (clamp, set-for-item, sync-to-active)
//   - the keyboard handler that drives the sidebar (updateSidebar)
//   - mouse hit-testing into the rendered sidebar (sidebarItemAt)
//
// Rendering of those items lives in channel_sidebar.go.

type sidebarItem struct {
	Kind  string
	Value string
	Label string
}

func (m channelModel) sidebarItemAt(y int) (sidebarItem, bool) {
	lines := 0
	lines++ // blank
	lines++ // WUPHF
	lines++ // subtitle
	lines++ // blank
	lines++ // Channels header
	items := m.sidebarItems()
	channelCount := len(m.channels)
	if channelCount == 0 {
		channelCount = 1
	}
	for i := 0; i < channelCount; i++ {
		if y == lines {
			return items[i], true
		}
		lines++
	}
	lines++ // blank before Apps
	lines++ // Apps header
	for i := channelCount; i < len(items); i++ {
		if y == lines {
			return items[i], true
		}
		lines++
	}
	return sidebarItem{}, false
}

func (m channelModel) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	roster := channelui.MergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo())
	switch msg.String() {
	case "up", "k":
		m.sidebarCursor--
		m.clampSidebarCursor()
	case "down", "j":
		m.sidebarCursor++
		m.clampSidebarCursor()
	case "pgup":
		m.sidebarRosterOffset -= 3
		if m.sidebarRosterOffset < 0 {
			m.sidebarRosterOffset = 0
		}
	case "pgdown":
		m.sidebarRosterOffset += 3
		maxOffset := channelui.MaxInt(0, len(roster)-1)
		if m.sidebarRosterOffset > maxOffset {
			m.sidebarRosterOffset = maxOffset
		}
	case "home":
		m.sidebarRosterOffset = 0
	case "end":
		m.sidebarRosterOffset = channelui.MaxInt(0, len(roster)-1)
	case "enter":
		items := m.sidebarItems()
		m.clampSidebarCursor()
		if len(items) == 0 {
			return m, nil
		}
		return m, m.selectSidebarItem(items[m.sidebarCursor])
	case "d":
		// Switch the main channel view to the per-agent DM channel.
		// The office continues running; this is just a channel switch.
		roster := channelui.MergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo())
		if len(roster) > 0 {
			idx := m.sidebarRosterOffset
			if idx < 0 {
				idx = 0
			}
			if idx >= len(roster) {
				idx = len(roster) - 1
			}
			target := roster[idx]
			name := target.Name
			if name == "" {
				name = target.Slug
			}
			// Use POST /channels/dm to get the deterministic DM slug.
			m.notice = fmt.Sprintf("Opening DM with %s…", name)
			return m, createDMChannel(target.Slug)
		}
	}
	return m, nil
}

func (m channelModel) sidebarItems() []sidebarItem {
	if m.isOneOnOne() {
		return nil
	}
	items := make([]sidebarItem, 0, len(m.channels)+5)
	items = append(items, m.channelSidebarItems()...)
	items = append(items, m.appSidebarItems()...)
	return items
}

func (m channelModel) channelSidebarItems() []sidebarItem {
	items := make([]sidebarItem, 0, len(m.channels))
	channels := m.channels
	if len(channels) == 0 {
		channels = []channelui.ChannelInfo{{Slug: "general", Name: "general"}}
	}
	for _, ch := range channels {
		items = append(items, sidebarItem{Kind: "channel", Value: ch.Slug, Label: "# " + ch.Slug})
	}
	return items
}

func (m channelModel) appSidebarItems() []sidebarItem {
	apps := channelui.OfficeSidebarApps()
	items := make([]sidebarItem, 0, len(apps))
	for _, app := range apps {
		items = append(items, sidebarItem{Kind: "app", Value: string(app.App), Label: app.Label})
	}
	return items
}

func (m channelModel) quickJumpItems() []sidebarItem {
	switch m.quickJumpTarget {
	case quickJumpChannels:
		return m.channelSidebarItems()
	case quickJumpApps:
		return m.appSidebarItems()
	default:
		return nil
	}
}

func (m *channelModel) setSidebarCursorForItem(target sidebarItem) {
	items := m.sidebarItems()
	for i, item := range items {
		if item.Kind == target.Kind && item.Value == target.Value {
			m.sidebarCursor = i
			return
		}
	}
}

func (m *channelModel) clampSidebarCursor() {
	items := m.sidebarItems()
	if len(items) == 0 {
		m.sidebarCursor = 0
		return
	}
	if m.sidebarCursor < 0 {
		m.sidebarCursor = 0
	}
	if m.sidebarCursor >= len(items) {
		m.sidebarCursor = len(items) - 1
	}
}

func (m *channelModel) selectSidebarItem(item sidebarItem) tea.Cmd {
	switch item.Kind {
	case "channel":
		m.activeChannel = item.Value
		m.activeApp = channelui.OfficeAppMessages
		m.syncSidebarCursorToActive()
		m.lastID = ""
		m.messages = nil
		m.members = nil
		m.requests = nil
		m.tasks = nil
		m.replyToID = ""
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.notice = "Switched to #" + m.activeChannel
		return tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel))
	case "app":
		m.activeApp = channelui.OfficeApp(item.Value)
		m.syncSidebarCursorToActive()
		switch m.activeApp {
		case channelui.OfficeAppMessages:
			m.notice = "Viewing #" + m.activeChannel + "."
			return pollBroker("", m.activeChannel)
		case channelui.OfficeAppInbox:
			m.notice = "Viewing the selected agent inbox."
			return pollBroker("", m.activeChannel)
		case channelui.OfficeAppOutbox:
			m.notice = "Viewing the selected agent outbox."
			return pollBroker("", m.activeChannel)
		case channelui.OfficeAppRecovery:
			m.notice = "Viewing the recovery summary."
			return m.pollCurrentState()
		case channelui.OfficeAppTasks:
			m.notice = "Viewing tasks in #" + m.activeChannel + "."
			return pollTasks(m.activeChannel)
		case channelui.OfficeAppRequests:
			m.notice = "Viewing requests in #" + m.activeChannel + "."
			return pollRequests(m.activeChannel)
		case channelui.OfficeAppPolicies:
			m.notice = "Viewing Nex and office insights."
			return pollOfficeLedger()
		case channelui.OfficeAppCalendar:
			m.notice = "Viewing the office calendar."
			return pollOfficeLedger()
		case channelui.OfficeAppArtifacts:
			m.notice = "Viewing recent execution artifacts."
			return m.pollCurrentState()
		case channelui.OfficeAppSkills:
			m.notice = "Viewing skills."
			return pollSkills("")
		}
	}
	return nil
}

func (m *channelModel) syncSidebarCursorToActive() {
	items := m.sidebarItems()
	for i, item := range items {
		if item.Kind == "channel" && item.Value == m.activeChannel && m.activeApp == channelui.OfficeAppMessages {
			m.sidebarCursor = i
			return
		}
		if item.Kind == "app" && item.Value == string(m.activeApp) {
			m.sidebarCursor = i
			return
		}
	}
	m.clampSidebarCursor()
}
