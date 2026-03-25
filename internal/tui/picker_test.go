package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var testOptions = []PickerOption{
	{Label: "Alpha", Value: "alpha", Description: "First"},
	{Label: "Beta", Value: "beta", Description: "Second"},
	{Label: "Gamma", Value: "gamma", Description: "Third"},
}

func TestPickerArrowNavigation(t *testing.T) {
	p := NewPicker("Choose", testOptions)
	p.SetActive(true)

	if p.selected != 0 {
		t.Fatal("expected initial selection to be 0")
	}

	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	p2, _ := p.Update(downMsg)
	if p2.selected != 1 {
		t.Fatalf("expected selection 1 after down, got %d", p2.selected)
	}

	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	p3, _ := p2.Update(upMsg)
	if p3.selected != 0 {
		t.Fatalf("expected selection 0 after up, got %d", p3.selected)
	}
}

func TestPickerNumberKeySelection(t *testing.T) {
	var got string
	p := NewPicker("Choose", testOptions)
	p.SetActive(true)
	p.OnSelect = func(v string) { got = v }

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}}
	p2, cmd := p.Update(msg)
	if p2.selected != 1 {
		t.Fatalf("expected selected=1 after pressing '2', got %d", p2.selected)
	}
	if got != "beta" {
		t.Fatalf("expected OnSelect called with 'beta', got %q", got)
	}
	if cmd == nil {
		t.Fatal("expected a PickerSelectMsg command")
	}
	result := cmd()
	sel, ok := result.(PickerSelectMsg)
	if !ok {
		t.Fatal("expected PickerSelectMsg")
	}
	if sel.Value != "beta" {
		t.Fatalf("expected value 'beta', got %q", sel.Value)
	}
}

func TestPickerEnterEmitsMsg(t *testing.T) {
	p := NewPicker("Choose", testOptions)
	p.SetActive(true)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := p.Update(msg)
	if cmd == nil {
		t.Fatal("expected command on Enter")
	}
	result := cmd()
	sel, ok := result.(PickerSelectMsg)
	if !ok {
		t.Fatal("expected PickerSelectMsg")
	}
	if sel.Value != "alpha" {
		t.Fatalf("expected 'alpha', got %q", sel.Value)
	}
}

func TestPickerInactiveIgnoresKeys(t *testing.T) {
	p := NewPicker("Choose", testOptions)
	// active is false by default

	msg := tea.KeyMsg{Type: tea.KeyDown}
	p2, _ := p.Update(msg)
	if p2.selected != 0 {
		t.Fatal("expected selection unchanged when picker is inactive")
	}
}

func TestConfirmYesKey(t *testing.T) {
	c := NewConfirm("Are you sure?")
	c.SetActive(true)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	c2, cmd := c.Update(msg)
	if !c2.answered {
		t.Fatal("expected answered=true after y")
	}
	if !c2.confirmed {
		t.Fatal("expected confirmed=true after y")
	}
	if cmd == nil {
		t.Fatal("expected ConfirmMsg command")
	}
	result := cmd()
	cm, ok := result.(ConfirmMsg)
	if !ok {
		t.Fatal("expected ConfirmMsg")
	}
	if !cm.Confirmed {
		t.Fatal("expected Confirmed=true")
	}
}

func TestConfirmNoKey(t *testing.T) {
	c := NewConfirm("Are you sure?")
	c.SetActive(true)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
	c2, cmd := c.Update(msg)
	if !c2.answered {
		t.Fatal("expected answered=true after n")
	}
	if c2.confirmed {
		t.Fatal("expected confirmed=false after n")
	}
	result := cmd()
	cm, ok := result.(ConfirmMsg)
	if !ok {
		t.Fatal("expected ConfirmMsg")
	}
	if cm.Confirmed {
		t.Fatal("expected Confirmed=false")
	}
}

func TestConfirmEscKey(t *testing.T) {
	c := NewConfirm("Are you sure?")
	c.SetActive(true)

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	c2, _ := c.Update(msg)
	if !c2.answered {
		t.Fatal("expected answered=true after esc")
	}
	if c2.confirmed {
		t.Fatal("expected confirmed=false after esc")
	}
}
