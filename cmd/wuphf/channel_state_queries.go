package main

import (
	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Pure state queries — visible-pending-request, composer label,
// interview option helpers, focus-cycle navigation. Each is a tiny
// read-only function over channelModel state that the Update method
// calls to make local decisions.

func (m channelModel) visiblePendingRequest() *channelui.Interview {
	if m.pending == nil {
		return nil
	}
	if m.pending.Channel != "" && m.pending.Channel != m.activeChannel {
		return nil
	}
	return m.pending
}

func (m channelModel) composerTargetLabel() string {
	if m.isOneOnOne() {
		return "1:1 with " + m.oneOnOneAgentName()
	}
	if chInfo := m.currentChannelInfo(); chInfo != nil && chInfo.IsDM() {
		name := chInfo.Name
		if name == "" {
			name = chInfo.Slug
		}
		return "DM→" + name
	}
	return m.activeChannel
}

func (m channelModel) recommendedOptionIndex() int {
	if m.pending == nil {
		return 0
	}
	for i, option := range m.pending.Options {
		if option.ID == m.pending.RecommendedID {
			return i
		}
	}
	return 0
}

func (m channelModel) interviewOptionCount() int {
	if m.pending == nil {
		return 0
	}
	return len(m.pending.Options) + 1
}

func (m channelModel) selectedInterviewOption() *channelui.InterviewOption {
	if m.pending == nil {
		return nil
	}
	if len(m.pending.Options) == 0 {
		return nil
	}
	if m.selectedOption < 0 {
		return &m.pending.Options[0]
	}
	if m.selectedOption >= len(m.pending.Options) {
		return nil
	}
	return &m.pending.Options[m.selectedOption]
}

// nextFocus cycles through visible panels: main → sidebar → thread → main.
func (m channelModel) nextFocus() focusArea {
	order := []focusArea{focusMain}
	if !m.sidebarCollapsed {
		order = append(order, focusSidebar)
	}
	if m.threadPanelOpen {
		order = append(order, focusThread)
	}
	for i, f := range order {
		if f == m.focus {
			return order[(i+1)%len(order)]
		}
	}
	return focusMain
}
