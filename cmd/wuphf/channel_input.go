package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Composer overlay management + input mutation. The Update method
// touches main and thread inputs through these wrappers so the same
// "set + clamp + recompute autocomplete/mention overlay" sequence
// happens once per code path.
//
// updateOverlaysForInput is the inner primitive; the named wrappers
// (updateInputOverlays, updateThreadOverlays, updateOverlaysForCurrentInput)
// route to the right input based on focus.

func (m *channelModel) updateOverlaysForInput(input []rune, cursor int) {
	text := string(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(input) {
		cursor = len(input)
	}
	m.autocomplete.UpdateQuery(strings.TrimLeft(text[:cursor], " "))
	m.mention.UpdateAgents(channelMentionAgents(m.members))
	m.mention.UpdateQuery(text[:cursor])
}

func (m *channelModel) updateInputOverlays() {
	m.updateOverlaysForInput(m.input, m.inputPos)
}

func (m *channelModel) updateThreadOverlays() {
	m.updateOverlaysForInput(m.threadInput, m.threadInputPos)
}

func (m *channelModel) updateOverlaysForCurrentInput() {
	if m.focus == focusThread && m.threadPanelOpen {
		m.updateThreadOverlays()
		return
	}
	if m.focus == focusMain {
		m.updateInputOverlays()
		m.maybeActivateChannelPickerFromInput()
		return
	}
	m.autocomplete.Dismiss()
	m.mention.Dismiss()
}

func (m *channelModel) setActiveInput(text string) {
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInput = []rune(text)
		m.threadInputPos = len(m.threadInput)
		m.threadInputHistory.ResetRecall()
		m.updateThreadOverlays()
		return
	}
	m.input = []rune(text)
	m.inputPos = len(m.input)
	m.inputHistory.ResetRecall()
	m.updateInputOverlays()
	m.maybeActivateChannelPickerFromInput()
}

func (m *channelModel) activeInputString() string {
	if m.focus == focusThread && m.threadPanelOpen {
		return string(m.threadInput)
	}
	return string(m.input)
}

func (m *channelModel) insertAcceptedMention(mention string) {
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInputHistory.ResetRecall()
		m.threadInput, m.threadInputPos = channelui.ReplaceMentionInInput(m.threadInput, m.threadInputPos, mention)
		m.updateThreadOverlays()
		return
	}
	m.inputHistory.ResetRecall()
	m.input, m.inputPos = channelui.ReplaceMentionInInput(m.input, m.inputPos, mention)
	m.updateInputOverlays()
}

func (m *channelModel) restoreMainSnapshot(snapshot channelui.Snapshot) {
	m.input = append([]rune(nil), snapshot.Input...)
	m.inputPos = channelui.NormalizeCursorPos(m.input, snapshot.Pos)
	m.updateInputOverlays()
}

func (m *channelModel) restoreThreadSnapshot(snapshot channelui.Snapshot) {
	m.threadInput = append([]rune(nil), snapshot.Input...)
	m.threadInputPos = channelui.NormalizeCursorPos(m.threadInput, snapshot.Pos)
	m.updateThreadOverlays()
}

// composerMotionKey decides whether a key event is a vim/emacs-style
// motion (alt+h/l/b/w/0/$, ctrl+a/e/b/f, arrow keys) that the composer
// should treat as cursor movement rather than literal text input.
func composerMotionKey(msg tea.KeyMsg) (string, bool) {
	if msg.Alt && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'h':
			return "alt+h", true
		case 'l':
			return "alt+l", true
		case 'b':
			return "alt+b", true
		case 'w':
			return "alt+w", true
		case '0':
			return "alt+0", true
		case '$':
			return "alt+$", true
		}
	}
	switch msg.String() {
	case "ctrl+a", "ctrl+e", "ctrl+b", "ctrl+f", "left", "right", "alt+h", "alt+l", "alt+b", "alt+w", "alt+0", "alt+$":
		return msg.String(), true
	default:
		return "", false
	}
}

// composerInsertRunes returns the rune sequence to insert for a key
// event, or nil if the event is not literal text (alt-prefixed,
// modifier-only, etc).
func composerInsertRunes(msg tea.KeyMsg) []rune {
	if msg.Type == tea.KeySpace || msg.String() == " " {
		return []rune{' '}
	}
	if msg.Alt {
		return nil
	}
	if len(msg.Runes) > 0 {
		return msg.Runes
	}
	return nil
}
