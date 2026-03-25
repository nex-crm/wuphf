package tui

import (
	"strings"
	"testing"
	"time"
)

func TestTerminalPaneSpawnEcho(t *testing.T) {
	pane := NewTerminalPane("test", "Test", 80, 24)
	if err := pane.Spawn("echo", []string{"hello"}, nil, t.TempDir()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer pane.Close()

	// Wait for output to reach emulator.
	time.Sleep(500 * time.Millisecond)

	view := pane.View()
	if !strings.Contains(view, "hello") {
		t.Errorf("expected View to contain %q, got:\n%s", "hello", view)
	}
}

func TestTerminalPaneAlive(t *testing.T) {
	pane := NewTerminalPane("sleeper", "Sleeper", 80, 24)
	if err := pane.Spawn("sleep", []string{"10"}, nil, t.TempDir()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if !pane.IsAlive() {
		t.Error("expected IsAlive() == true after spawn")
	}

	pane.Close()

	// Give the wait goroutine a moment to update state.
	time.Sleep(100 * time.Millisecond)

	if pane.IsAlive() {
		t.Error("expected IsAlive() == false after Close")
	}
}

func TestTerminalPaneSendText(t *testing.T) {
	pane := NewTerminalPane("cat", "Cat", 80, 24)
	if err := pane.Spawn("cat", nil, nil, t.TempDir()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer pane.Close()

	pane.SendText("test123\n")

	// Wait for echo back through PTY.
	time.Sleep(500 * time.Millisecond)

	view := pane.View()
	if !strings.Contains(view, "test123") {
		t.Errorf("expected View to contain %q, got:\n%s", "test123", view)
	}
}

func TestTerminalPaneResize(t *testing.T) {
	pane := NewTerminalPane("resize", "Resize", 80, 24)

	// Resize without a running process should not panic.
	pane.Resize(120, 40)

	// Spawn and resize while running.
	if err := pane.Spawn("cat", nil, nil, t.TempDir()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer pane.Close()

	pane.Resize(120, 40)

	// Verify emulator dimensions updated.
	if w := pane.emulator.Width(); w != 120 {
		t.Errorf("expected width 120, got %d", w)
	}
	if h := pane.emulator.Height(); h != 40 {
		t.Errorf("expected height 40, got %d", h)
	}
}
