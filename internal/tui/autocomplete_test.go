package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var testCommands = []SlashCommand{
	{Name: "help", Description: "Show help", Category: "general"},
	{Name: "history", Description: "Show history", Category: "general"},
	{Name: "agents", Description: "List agents", Category: "team"},
	{Name: "clear", Description: "Clear screen", Category: "general"},
}

func TestAutocompleteFilter(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/h")

	if !ac.IsVisible() {
		t.Fatal("expected autocomplete to be visible after /h")
	}
	// "help" and "history" both start with "h"
	if len(ac.matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(ac.matches))
	}
}

func TestAutocompleteHideWhenNoSlash(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/h")
	ac.UpdateQuery("hello") // no slash prefix

	if ac.IsVisible() {
		t.Fatal("expected autocomplete to be hidden without slash prefix")
	}
}

func TestAutocompleteTabCycles(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/h")

	initial := ac.selected
	ac.Next()
	if ac.selected == initial {
		t.Fatal("expected selection to advance on Next()")
	}
	ac.Prev()
	if ac.selected != initial {
		t.Fatal("expected selection to return to initial on Prev()")
	}
}

func TestAutocompleteAccept(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/he")

	name := ac.Accept()
	if name != "help" {
		t.Fatalf("expected 'help', got %q", name)
	}
	if ac.IsVisible() {
		t.Fatal("expected autocomplete to hide after Accept()")
	}
}

func TestAutocompleteDismiss(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/h")
	ac.Dismiss()

	if ac.IsVisible() {
		t.Fatal("expected autocomplete to hide after Dismiss()")
	}
}

func TestAutocompleteEscKeyDismisses(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/h")

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	ac2, _ := ac.Update(msg)
	if ac2.IsVisible() {
		t.Fatal("expected autocomplete to hide on esc key")
	}
}

func TestAutocompleteMaxMatches(t *testing.T) {
	var cmds []SlashCommand
	for i := 0; i < 20; i++ {
		cmds = append(cmds, SlashCommand{Name: "cmd", Description: ""})
		cmds[i].Name = "cmd" + string(rune('a'+i))
	}
	ac := NewAutocomplete(cmds)
	ac.UpdateQuery("/cmd")

	if len(ac.matches) > maxAutocompleteMatches {
		t.Fatalf("expected at most %d matches, got %d", maxAutocompleteMatches, len(ac.matches))
	}
}

func TestAutocompleteViewShowsCategoryLabels(t *testing.T) {
	ac := NewAutocomplete(testCommands)
	ac.UpdateQuery("/")

	view := ac.View()
	if view == "" {
		t.Fatal("expected autocomplete view to render")
	}
	if !strings.Contains(view, "general") || !strings.Contains(view, "team") {
		t.Fatalf("expected category labels in autocomplete view, got %q", view)
	}
}
