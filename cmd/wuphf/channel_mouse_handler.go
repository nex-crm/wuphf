package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Mouse event handler — tea.MouseMsg case body extracted from
// Update. Pairs with channel_mouse.go which holds the hit-testing
// (mouseActionAt, mainPanelMouseAction) and geometry projection.
//
// Three button paths:
//   - Wheel up/down: scroll the focused panel (thread / sidebar
//     roster / main viewport).
//   - Left click: hit-test via mouseActionAt and dispatch on
//     action.Kind (focus / thread / jump-latest / autocomplete /
//     mention / task / request / prompt / channel / app).
//
// channel_mouse.go owns the hit-tester; this file owns "what to do
// once the hit-tester says where you clicked."

func (m channelModel) handleMouseMsg(msg tea.MouseMsg) (channelModel, tea.Cmd) {
	layout := m.currentLayout()
	inSidebar := layout.ShowSidebar && msg.X < layout.SidebarW
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.focus == focusThread && m.threadPanelOpen {
			m.threadScroll++
		} else if inSidebar {
			if m.sidebarRosterOffset > 0 {
				m.sidebarRosterOffset--
			}
		} else {
			m.scroll++
		}
	case tea.MouseButtonWheelDown:
		if m.focus == focusThread && m.threadPanelOpen {
			if m.threadScroll > 0 {
				m.threadScroll--
			}
		} else if inSidebar {
			m.sidebarRosterOffset++
		} else {
			if m.scroll > 0 {
				m.scroll--
				if m.scroll == 0 {
					m.clearUnreadState()
				}
			}
		}
	case tea.MouseButtonLeft:
		if action, ok := m.mouseActionAt(msg.X, msg.Y); ok {
			switch action.Kind {
			case "focus":
				switch action.Value {
				case "sidebar":
					m.focus = focusSidebar
				case "thread":
					m.focus = focusThread
				default:
					m.focus = focusMain
				}
				m.updateOverlaysForCurrentInput()
				return m, nil
			case "thread":
				m.threadPanelOpen = true
				m.threadPanelID = action.Value
				m.replyToID = action.Value
				m.focus = focusThread
				m.threadScroll = 0
				m.notice = fmt.Sprintf("Replying in thread %s", action.Value)
				return m, nil
			case "jump-latest":
				m.scroll = 0
				m.clearUnreadState()
				return m, nil
			case "autocomplete":
				if idx, ok := channelui.PopupActionIndex(action.Value); ok {
					for i := 0; i < m.autocomplete.Len() && m.autocomplete.SelectedIndex() != idx; i++ {
						m.autocomplete.Next()
					}
					if m.autocomplete.SelectedIndex() == idx {
						if name := m.autocomplete.Accept(); name != "" {
							next, cmd := m.runActiveCommand("/" + name)
							return next.(channelModel), cmd
						}
					}
				}
				return m, nil
			case "mention":
				if idx, ok := channelui.PopupActionIndex(action.Value); ok {
					for i := 0; i < m.mention.Len() && m.mention.SelectedIndex() != idx; i++ {
						m.mention.Next()
					}
					if m.mention.SelectedIndex() == idx {
						if mention := m.mention.Accept(); mention != "" {
							m.insertAcceptedMention(mention)
						}
					}
				}
				return m, nil
			case "task":
				if task, ok := m.findTaskByID(action.Value); ok {
					m.focus = focusMain
					return m, m.openTaskActionPicker(task)
				}
				return m, nil
			case "request":
				if req, ok := m.findRequestByID(action.Value); ok {
					m.focus = focusMain
					return m, m.openRequestActionPicker(req)
				}
				return m, nil
			case "prompt":
				m.focus = focusMain
				m.applyRecoveryPrompt(action.Value)
				return m, nil
			case "channel", "app":
				items := m.sidebarItems()
				for idx, item := range items {
					if item.Kind == action.Kind && item.Value == action.Value {
						m.sidebarCursor = idx
						break
					}
				}
				m.focus = focusSidebar
				return m, m.selectSidebarItem(sidebarItem{Kind: action.Kind, Value: action.Value})
			}
		}
	}
	return m, nil
}
