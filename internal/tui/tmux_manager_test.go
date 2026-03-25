package tui

import (
	"os"
	"testing"
)

func TestHasTmux(t *testing.T) {
	// Should not panic, just returns a bool.
	got := HasTmux()
	t.Logf("HasTmux() = %v", got)
}

func TestIsITerm2(t *testing.T) {
	// Save and restore TERM_PROGRAM.
	orig := os.Getenv("TERM_PROGRAM")
	defer os.Setenv("TERM_PROGRAM", orig)

	os.Setenv("TERM_PROGRAM", "iTerm.app")
	if !IsITerm2() {
		t.Error("expected IsITerm2()=true when TERM_PROGRAM=iTerm.app")
	}

	os.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if IsITerm2() {
		t.Error("expected IsITerm2()=false when TERM_PROGRAM=Apple_Terminal")
	}

	os.Unsetenv("TERM_PROGRAM")
	if IsITerm2() {
		t.Error("expected IsITerm2()=false when TERM_PROGRAM is unset")
	}
}

func TestAttachHint(t *testing.T) {
	tm := NewTmuxManager("wuphf-agents")

	orig := os.Getenv("TERM_PROGRAM")
	defer os.Setenv("TERM_PROGRAM", orig)

	os.Setenv("TERM_PROGRAM", "Apple_Terminal")
	hint := tm.AttachHint("ceo")
	if hint != "tmux select-window -t wuphf-agents:ceo" {
		t.Errorf("unexpected hint: %s", hint)
	}

	os.Setenv("TERM_PROGRAM", "iTerm.app")
	hint = tm.AttachHint("ceo")
	if hint != "tmux -CC attach -t wuphf-agents" {
		t.Errorf("unexpected iTerm hint: %s", hint)
	}
}

// Integration tests — only run when tmux is available.

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if !HasTmux() {
		t.Skip("tmux not available")
	}
}

func TestTmuxCreateAndKillSession(t *testing.T) {
	skipIfNoTmux(t)

	tm := NewTmuxManager("nex-test-session")
	// Ensure clean state.
	_ = tm.KillSession()

	if err := tm.CreateSession(); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Calling CreateSession again should be a no-op (idempotent).
	if err := tm.CreateSession(); err != nil {
		t.Fatalf("CreateSession (idempotent): %v", err)
	}

	windows, err := tm.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows: %v", err)
	}
	if len(windows) == 0 {
		t.Error("expected at least one window in new session")
	}

	if err := tm.KillSession(); err != nil {
		t.Fatalf("KillSession: %v", err)
	}

	// Killing again should be a no-op.
	if err := tm.KillSession(); err != nil {
		t.Fatalf("KillSession (idempotent): %v", err)
	}
}

func TestTmuxSpawnAndCapture(t *testing.T) {
	skipIfNoTmux(t)

	tm := NewTmuxManager("nex-test-spawn")
	_ = tm.KillSession()
	defer tm.KillSession()

	if err := tm.CreateSession(); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Spawn a window that echoes something and stays alive briefly.
	if err := tm.SpawnAgent("test-agent", "bash", []string{"-c", "'echo HELLO_TMUX; sleep 5'"}, nil); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}

	windows, err := tm.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows: %v", err)
	}

	found := false
	for _, w := range windows {
		if w == "test-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected window 'test-agent' in %v", windows)
	}

	// Capture pane content — should contain our echo output.
	content, err := tm.CapturePaneContent("test-agent")
	if err != nil {
		t.Fatalf("CapturePaneContent: %v", err)
	}
	t.Logf("captured content:\n%s", content)
}
