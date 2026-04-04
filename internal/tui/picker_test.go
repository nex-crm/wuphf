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
	if len(p.filtered) != len(testOptions) {
		t.Fatalf("expected %d visible options, got %d", len(testOptions), len(p.filtered))
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

func TestPickerFuzzyFilter(t *testing.T) {
	p := NewPicker("Choose", testOptions)
	p.UpdateQuery("gm")

	if got := p.Query(); got != "gm" {
		t.Fatalf("expected query to be %q, got %q", "gm", got)
	}
	if len(p.filtered) != 1 {
		t.Fatalf("expected 1 fuzzy match, got %d", len(p.filtered))
	}
	if got := p.FilteredOptions()[0].Value; got != "gamma" {
		t.Fatalf("expected fuzzy match to select gamma, got %q", got)
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

func TestPickerEnterUsesFilteredSelection(t *testing.T) {
	var got string
	p := NewPicker("Choose", testOptions)
	p.SetActive(true)
	p.OnSelect = func(v string) { got = v }

	p.UpdateQuery("gm")

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
	if sel.Value != "gamma" {
		t.Fatalf("expected 'gamma', got %q", sel.Value)
	}
	if got != "gamma" {
		t.Fatalf("expected OnSelect called with 'gamma', got %q", got)
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

func TestPickerEscCloses(t *testing.T) {
	p := NewPicker("Choose", testOptions)
	p.SetActive(true)

	p2, _ := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p2.IsActive() {
		t.Fatal("expected picker to close on esc")
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
