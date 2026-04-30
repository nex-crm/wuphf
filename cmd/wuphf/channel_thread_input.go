package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// updateThread handles key events when the thread panel is focused.
// Mirrors the main composer's keymap (motion, history, line insert,
// scroll) but writes through to threadInput / threadInputPos /
// threadScroll instead of the main input. Splitting it out keeps the
// thread-mode keymap reviewable in isolation; pair with the main
// composer keymap that lives inside Update() in channel.go.
//
// Named channel_thread_input.go (not channel_thread.go) because that
// name is already taken by the thread-panel renderer.
func (m channelModel) updateThread(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if motionKey, ok := composerMotionKey(msg); ok {
		if nextPos, handled := channelui.MoveComposerCursor(m.threadInput, m.threadInputPos, motionKey); handled {
			m.threadInputPos = nextPos
			m.updateThreadOverlays()
		}
		return m, nil
	}
	key := msg.String()
	if msg.Type == tea.KeyCtrlJ {
		key = "ctrl+j"
	}
	switch key {
	case "enter":
		if len(m.threadInput) > 0 {
			text := string(m.threadInput)
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "/") {
				m.threadInputHistory.Record(m.threadInput, m.threadInputPos)
				return m.runCommand(trimmed, m.threadPanelID)
			}
			m.threadInputHistory.Record(m.threadInput, m.threadInputPos)
			m.threadInput = nil
			m.threadInputPos = 0
			m.posting = true
			return m, postToChannel(text, m.threadPanelID, m.activeChannel)
		}
	case "backspace":
		if m.threadInputPos > 0 {
			m.threadInputHistory.ResetRecall()
			m.threadInput = append(m.threadInput[:m.threadInputPos-1], m.threadInput[m.threadInputPos:]...)
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "ctrl+u":
		m.threadInputHistory.ResetRecall()
		m.threadInput = nil
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+p":
		if snapshot, ok := m.threadInputHistory.Previous(m.threadInput, m.threadInputPos); ok {
			m.restoreThreadSnapshot(snapshot)
		}
	case "ctrl+n":
		if snapshot, ok := m.threadInputHistory.Next(); ok {
			m.restoreThreadSnapshot(snapshot)
		}
	case "ctrl+a":
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+e":
		m.threadInputPos = len(m.threadInput)
		m.updateThreadOverlays()
	case "ctrl+j":
		m.threadInputHistory.ResetRecall()
		ch := []rune{'\n'}
		tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
		copy(tail, m.threadInput[m.threadInputPos:])
		m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
		m.threadInputPos++
		m.updateThreadOverlays()
	case "left":
		if m.threadInputPos > 0 {
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "right":
		if m.threadInputPos < len(m.threadInput) {
			m.threadInputPos++
			m.updateThreadOverlays()
		}
	case "up":
		m.threadScroll++
	case "down":
		m.threadScroll--
		if m.threadScroll < 0 {
			m.threadScroll = 0
		}
	case "pgup":
		m.threadScroll += 5
	case "pgdown":
		m.threadScroll -= 5
		if m.threadScroll < 0 {
			m.threadScroll = 0
		}
	default:
		if ch := composerInsertRunes(msg); len(ch) > 0 {
			m.threadInputHistory.ResetRecall()
			m.threadInput, m.threadInputPos = channelui.InsertComposerRunes(m.threadInput, m.threadInputPos, ch)
			m.updateThreadOverlays()
		} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
			ch := msg.Runes
			if len(ch) == 0 {
				ch = []rune(msg.String())
			}
			if len(ch) > 0 {
				m.threadInputHistory.ResetRecall()
				tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
				copy(tail, m.threadInput[m.threadInputPos:])
				m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
				m.threadInputPos += len(ch)
				m.updateThreadOverlays()
			}
		}
	}
	return m, nil
}
