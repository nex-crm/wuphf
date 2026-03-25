package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// InputMode represents vim-style editing modes.
type InputMode string

const (
	ModeNormal InputMode = "NORMAL"
	ModeInsert InputMode = "INSERT"
)

// KeyAction represents what a key press should do.
type KeyAction string

const (
	ActionNone         KeyAction = ""
	ActionInsertMode   KeyAction = "insert_mode"
	ActionNormalMode   KeyAction = "normal_mode"
	ActionScrollUp     KeyAction = "scroll_up"
	ActionScrollDown   KeyAction = "scroll_down"
	ActionScrollTop    KeyAction = "scroll_top"
	ActionScrollBottom KeyAction = "scroll_bottom"
	ActionHalfPageUp   KeyAction = "half_page_up"
	ActionHalfPageDown KeyAction = "half_page_down"
	ActionQuit         KeyAction = "quit"
	ActionHelp         KeyAction = "help"
	ActionAgents       KeyAction = "agents"
	ActionChat         KeyAction = "chat"
	ActionSearch       KeyAction = "search"
	ActionSubmit       KeyAction = "submit"
	ActionAutocomplete KeyAction = "autocomplete"
	ActionAutocompPrev KeyAction = "autocomp_prev"
	ActionDismiss      KeyAction = "dismiss"
	ActionCancel       KeyAction = "cancel"
)

// MapKey returns the action for a key press in the given mode.
func MapKey(mode InputMode, key tea.KeyMsg) KeyAction {
	switch mode {
	case ModeNormal:
		return mapNormalKey(key)
	case ModeInsert:
		return mapInsertKey(key)
	}
	return ActionNone
}

func mapNormalKey(key tea.KeyMsg) KeyAction {
	switch key.String() {
	case "j", "down":
		return ActionScrollDown
	case "k", "up":
		return ActionScrollUp
	case "g":
		return ActionScrollTop // TODO: gg detection needs state
	case "G", "end":
		return ActionScrollBottom
	case "i":
		return ActionInsertMode
	case "/":
		return ActionSearch
	case "?":
		return ActionHelp
	case "a":
		return ActionAgents
	case "c":
		return ActionChat
	case "q":
		return ActionQuit
	case "ctrl+c":
		return ActionQuit
	case "ctrl+d":
		return ActionHalfPageDown
	case "ctrl+u":
		return ActionHalfPageUp
	}
	return ActionNone
}

func mapInsertKey(key tea.KeyMsg) KeyAction {
	switch key.String() {
	case "esc":
		return ActionNormalMode
	case "enter":
		return ActionSubmit
	case "tab":
		return ActionAutocomplete
	case "shift+tab":
		return ActionAutocompPrev
	case "ctrl+c":
		return ActionCancel
	}
	return ActionNone
}

// DoublePress tracks double key-press detection within a time window.
type DoublePress struct {
	lastPress time.Time
	window    time.Duration
}

// NewDoublePress creates a DoublePress tracker with the given time window.
func NewDoublePress(window time.Duration) *DoublePress {
	return &DoublePress{window: window}
}

// Press records a key press. Returns true if a double-press was detected
// (second press within the window). Resets state after a detected double-press.
// On first press: records time, returns false.
// On second press within window: resets, returns true.
// After window expires: treats as first press, returns false.
func (d *DoublePress) Press() bool {
	now := time.Now()
	if !d.lastPress.IsZero() && now.Sub(d.lastPress) <= d.window {
		d.lastPress = time.Time{} // reset after double-press detected
		return true
	}
	d.lastPress = now
	return false
}
