package tui

import (
	"strings"
	"testing"
)

func TestComposerHintIncludesHistoryWhenAvailable(t *testing.T) {
	got := ComposerHint(ComposerHintState{
		Context:          ContextCompose,
		HistoryAvailable: true,
	})
	if !containsAllHint(got, "Ctrl+P recall", "Ctrl+N restore", "Enter send") {
		t.Fatalf("unexpected compose hint: %q", got)
	}
}

func TestComposerHintForAutocomplete(t *testing.T) {
	got := ComposerHint(ComposerHintState{Context: ContextAutocomplete})
	if !containsAllHint(got, "Tab complete", "Enter run", "Esc close") {
		t.Fatalf("unexpected autocomplete hint: %q", got)
	}
}

func TestContinuePromptInlineHint(t *testing.T) {
	got := ContinuePrompt{
		Title:       "Review answer",
		Description: "Check the summary before submission",
	}.InlineHint()
	if !containsAllHint(got, "Review answer", "Enter continue", "Esc cancel") {
		t.Fatalf("unexpected continue hint: %q", got)
	}
}

func containsAllHint(text string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(text, want) {
			return false
		}
	}
	return true
}
