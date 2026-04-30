package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Composer input — overlay management, key handling, and snapshot
// restore for both the main and thread composers.
//
// The model has *one* composer concept that switches its target
// (m.input vs m.threadInput) based on m.focus. updateOverlays* /
// setActiveInput / activeInputString / insertAcceptedMention /
// restore*Snapshot all dispatch on focus and write to the right
// rune buffer. updateThread is the thread-mode keymap handler;
// the main composer's keymap lives inline in Update() because it
// shares the giant tea.KeyMsg switch with every other key event.
//
// composerMotionKey + composerInsertRunes are pure-function
// classifiers shared by both composers — given a tea.KeyMsg,
// answer "is this a cursor motion?" and "what runes does this
// insert?" The Update method and updateThread both call them.
//
// Rendering (renderComposer) lives in channel_composer.go.

// ─── Overlay management ─────────────────────────────────────────────────

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

// ─── Active-input mutation (focus-aware) ────────────────────────────────

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

// ─── Key classification (shared by both composers) ──────────────────────

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

// ─── Thread-mode keymap ─────────────────────────────────────────────────

// updateThread handles key events when the thread panel is focused.
// Mirrors the main composer's keymap (motion, history, line insert,
// scroll) but writes through to threadInput / threadInputPos /
// threadScroll instead of the main input.
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
