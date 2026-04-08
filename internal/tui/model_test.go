package tui

import "testing"

func TestCodexDisablesTmuxAndPaneModes(t *testing.T) {
	if shouldUseEmbeddedPanes("codex", true, true) {
		t.Fatal("expected codex provider to disable embedded Claude panes")
	}
	if shouldUseTmuxChannelMode("codex", true, true) {
		t.Fatal("expected codex provider to disable tmux channel mode")
	}
}

func TestClaudeStillUsesTmuxModesWhenAvailable(t *testing.T) {
	if !shouldUseEmbeddedPanes("claude-code", true, true) {
		t.Fatal("expected claude provider to allow embedded panes")
	}
	if !shouldUseTmuxChannelMode("claude-code", true, true) {
		t.Fatal("expected claude provider to allow tmux channel mode")
	}
}
