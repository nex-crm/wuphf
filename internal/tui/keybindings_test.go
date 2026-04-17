package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// mkKey creates a tea.KeyMsg for the given key string.
func mkKey(s string) tea.KeyMsg {
	switch s {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	default:
		runes := []rune(s)
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
	}
}

// --- Normal mode tests ---

func TestNormalMode_ScrollDown(t *testing.T) {
	for _, key := range []string{"j", "down"} {
		got := MapKey(ModeNormal, mkKey(key))
		if got != ActionScrollDown {
			t.Errorf("MapKey(Normal, %q) = %q, want %q", key, got, ActionScrollDown)
		}
	}
}

func TestNormalMode_ScrollUp(t *testing.T) {
	for _, key := range []string{"k", "up"} {
		got := MapKey(ModeNormal, mkKey(key))
		if got != ActionScrollUp {
			t.Errorf("MapKey(Normal, %q) = %q, want %q", key, got, ActionScrollUp)
		}
	}
}

func TestNormalMode_ScrollTop(t *testing.T) {
	got := MapKey(ModeNormal, mkKey("g"))
	if got != ActionScrollTop {
		t.Errorf("MapKey(Normal, \"g\") = %q, want %q", got, ActionScrollTop)
	}
}

func TestNormalMode_ScrollBottom(t *testing.T) {
	for _, key := range []string{"G", "end"} {
		got := MapKey(ModeNormal, mkKey(key))
		if got != ActionScrollBottom {
			t.Errorf("MapKey(Normal, %q) = %q, want %q", key, got, ActionScrollBottom)
		}
	}
}

func TestNormalMode_InsertMode(t *testing.T) {
	got := MapKey(ModeNormal, mkKey("i"))
	if got != ActionInsertMode {
		t.Errorf("MapKey(Normal, \"i\") = %q, want %q", got, ActionInsertMode)
	}
}

func TestNormalMode_Quit(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		got := MapKey(ModeNormal, mkKey(key))
		if got != ActionQuit {
			t.Errorf("MapKey(Normal, %q) = %q, want %q", key, got, ActionQuit)
		}
	}
}

func TestNormalMode_Help(t *testing.T) {
	got := MapKey(ModeNormal, mkKey("?"))
	if got != ActionHelp {
		t.Errorf("MapKey(Normal, \"?\") = %q, want %q", got, ActionHelp)
	}
}

func TestNormalMode_Agents(t *testing.T) {
	got := MapKey(ModeNormal, mkKey("a"))
	if got != ActionAgents {
		t.Errorf("MapKey(Normal, \"a\") = %q, want %q", got, ActionAgents)
	}
}

func TestNormalMode_HalfPage(t *testing.T) {
	if got := MapKey(ModeNormal, mkKey("ctrl+d")); got != ActionHalfPageDown {
		t.Errorf("MapKey(Normal, ctrl+d) = %q, want %q", got, ActionHalfPageDown)
	}
	if got := MapKey(ModeNormal, mkKey("ctrl+u")); got != ActionHalfPageUp {
		t.Errorf("MapKey(Normal, ctrl+u) = %q, want %q", got, ActionHalfPageUp)
	}
}

func TestNormalMode_Unknown(t *testing.T) {
	got := MapKey(ModeNormal, mkKey("x"))
	if got != ActionNone {
		t.Errorf("MapKey(Normal, \"x\") = %q, want %q", got, ActionNone)
	}
}

// --- Insert mode tests ---

func TestInsertMode_NormalMode(t *testing.T) {
	got := MapKey(ModeInsert, mkKey("esc"))
	if got != ActionNormalMode {
		t.Errorf("MapKey(Insert, \"esc\") = %q, want %q", got, ActionNormalMode)
	}
}

func TestInsertMode_Submit(t *testing.T) {
	got := MapKey(ModeInsert, mkKey("enter"))
	if got != ActionSubmit {
		t.Errorf("MapKey(Insert, \"enter\") = %q, want %q", got, ActionSubmit)
	}
}

func TestInsertMode_Autocomplete(t *testing.T) {
	got := MapKey(ModeInsert, mkKey("tab"))
	if got != ActionAutocomplete {
		t.Errorf("MapKey(Insert, \"tab\") = %q, want %q", got, ActionAutocomplete)
	}
}

func TestInsertMode_AutocompPrev(t *testing.T) {
	got := MapKey(ModeInsert, mkKey("shift+tab"))
	if got != ActionAutocompPrev {
		t.Errorf("MapKey(Insert, \"shift+tab\") = %q, want %q", got, ActionAutocompPrev)
	}
}

func TestInsertMode_Cancel(t *testing.T) {
	got := MapKey(ModeInsert, mkKey("ctrl+c"))
	if got != ActionCancel {
		t.Errorf("MapKey(Insert, \"ctrl+c\") = %q, want %q", got, ActionCancel)
	}
}

func TestInsertMode_UnknownReturnsNone(t *testing.T) {
	got := MapKey(ModeInsert, mkKey("j"))
	if got != ActionNone {
		t.Errorf("MapKey(Insert, \"j\") = %q, want %q", got, ActionNone)
	}
}

// --- DoublePress tests ---

func TestDoublePress_FirstPressReturnsFalse(t *testing.T) {
	dp := NewDoublePress(time.Second)
	if dp.Press() {
		t.Error("first press should return false")
	}
}

func TestDoublePress_SecondPressWithinWindowReturnsTrue(t *testing.T) {
	dp := NewDoublePress(time.Second)
	dp.Press() // first press
	if !dp.Press() {
		t.Error("second press within window should return true")
	}
}

func TestDoublePress_ResetsAfterDoublePress(t *testing.T) {
	dp := NewDoublePress(time.Second)
	dp.Press() // first
	dp.Press() // second (detected double)
	if dp.Press() {
		t.Error("press after reset should return false (first of new sequence)")
	}
}

func TestDoublePress_ExpiredWindowResetsToFirstPress(t *testing.T) {
	dp := NewDoublePress(20 * time.Millisecond)
	dp.Press() // first press
	time.Sleep(30 * time.Millisecond)
	// Window expired — second press is treated as first
	if dp.Press() {
		t.Error("press after window expiry should return false (treated as first press)")
	}
}

func TestDoublePress_QuickSuccessionDetected(t *testing.T) {
	dp := NewDoublePress(200 * time.Millisecond)
	first := dp.Press()
	second := dp.Press()
	if first {
		t.Error("first press should be false")
	}
	if !second {
		t.Error("second rapid press should be true")
	}
}
