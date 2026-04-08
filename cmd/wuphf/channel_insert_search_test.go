package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/tui"
)

func TestInsertCommandOpensReferencePicker(t *testing.T) {
	m := newChannelModel(false)
	m.channels = []channelInfo{{Slug: "launch", Description: "Launch work"}}
	m.members = []channelMember{{Slug: "pm", Name: "Product Manager"}}

	next, _ := m.runCommand("/insert", "")
	got := next.(channelModel)

	if got.pickerMode != channelPickerInsert {
		t.Fatalf("expected insert picker mode, got %q", got.pickerMode)
	}
	if !got.picker.IsActive() {
		t.Fatal("expected insert picker to be active")
	}
}

func TestPickerInsertSelectionUpdatesComposer(t *testing.T) {
	m := newChannelModel(false)
	m.pickerMode = channelPickerInsert
	m.picker = tui.NewPicker("Insert", []tui.PickerOption{{Label: "@pm", Value: "@pm "}})
	m.picker.SetActive(true)

	next, _ := m.Update(tui.PickerSelectMsg{Label: "@pm", Value: "@pm "})
	got := next.(channelModel)

	if string(got.input) != "@pm" && string(got.input) != "@pm " {
		t.Fatalf("expected inserted mention in composer, got %q", string(got.input))
	}
	if !strings.Contains(got.notice, "Inserted reference") {
		t.Fatalf("expected insert notice, got %q", got.notice)
	}
}

func TestSearchCommandOpensWorkspaceSearchPicker(t *testing.T) {
	m := newChannelModel(false)
	m.channels = []channelInfo{{Slug: "launch", Description: "Launch work"}}

	next, _ := m.runCommand("/search", "")
	got := next.(channelModel)

	if got.pickerMode != channelPickerSearch {
		t.Fatalf("expected search picker mode, got %q", got.pickerMode)
	}
	if !got.picker.IsActive() {
		t.Fatal("expected search picker to be active")
	}
}

func TestSearchPickerIncludesOfficeApps(t *testing.T) {
	m := newChannelModel(false)

	options := m.buildSearchPickerOptions()
	values := make([]string, 0, len(options))
	for _, opt := range options {
		values = append(values, opt.Value)
	}
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, "app:messages") || !strings.Contains(joined, "app:artifacts") {
		t.Fatalf("expected office app targets in search picker, got %q", joined)
	}
}

func TestSearchSelectionOpensThread(t *testing.T) {
	m := newChannelModel(false)
	m.messages = []brokerMessage{
		{ID: "msg-1", From: "ceo", Content: "Root thread"},
		{ID: "msg-2", From: "pm", Content: "Reply", ReplyTo: "msg-1"},
	}

	cmd := m.applySearchSelection("thread:msg-1", "Message msg-1")
	if cmd == nil {
		t.Fatal("expected poll command for thread selection")
	}
	if !m.threadPanelOpen || m.threadPanelID != "msg-1" || m.replyToID != "msg-1" {
		t.Fatalf("expected thread focus on msg-1, got threadOpen=%v threadID=%q replyTo=%q", m.threadPanelOpen, m.threadPanelID, m.replyToID)
	}
}

func TestSearchSelectionSupportsOfficeAppTargets(t *testing.T) {
	m := newChannelModel(false)

	cmd := m.applySearchSelection("app:artifacts", "Artifacts")
	if cmd == nil {
		t.Fatal("expected app selection to trigger a refresh command")
	}
	if m.activeApp != officeAppArtifacts {
		t.Fatalf("expected artifacts app to be active, got %q", m.activeApp)
	}
	if !strings.Contains(m.notice, "Viewing Artifacts") {
		t.Fatalf("expected app selection notice, got %q", m.notice)
	}
}

func TestRewindCommandOpensRecoveryPromptPicker(t *testing.T) {
	m := newChannelModel(false)
	m.messages = []brokerMessage{{ID: "msg-1", From: "ceo", Content: "Need a summary."}}

	next, _ := m.runCommand("/rewind", "")
	got := next.(channelModel)

	if got.pickerMode != channelPickerRewind {
		t.Fatalf("expected rewind picker mode, got %q", got.pickerMode)
	}
	if !got.picker.IsActive() {
		t.Fatal("expected rewind picker to be active")
	}
}

func TestRewindSelectionInsertsRecoveryPrompt(t *testing.T) {
	m := newChannelModel(false)
	m.activeApp = officeAppRecovery
	m.pickerMode = channelPickerRewind
	m.picker = tui.NewPicker("Rewind", []tui.PickerOption{{Label: "Since msg-1", Value: "Summarize everything since msg-1"}})
	m.picker.SetActive(true)

	next, _ := m.Update(tui.PickerSelectMsg{Label: "Since msg-1", Value: "Summarize everything since msg-1"})
	got := next.(channelModel)

	if !strings.Contains(string(got.input), "Summarize everything since msg-1") {
		t.Fatalf("expected recovery prompt in composer, got %q", string(got.input))
	}
	if got.activeApp != officeAppMessages {
		t.Fatalf("expected rewind selection to return to messages, got %q", got.activeApp)
	}
	if got.focus != focusMain {
		t.Fatalf("expected focus to return to the main composer, got %v", got.focus)
	}
}

func TestInsertAndSearchCommandsAreAvailableThroughEnter(t *testing.T) {
	m := newChannelModel(false)
	m.channels = []channelInfo{{Slug: "launch", Description: "Launch work"}}
	m.input = []rune("/search")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)
	if got.pickerMode != channelPickerSearch {
		t.Fatalf("expected enter to open search picker, got %q", got.pickerMode)
	}
}
