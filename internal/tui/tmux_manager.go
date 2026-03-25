package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// TmuxManager manages a tmux session with one window per agent.
type TmuxManager struct {
	sessionName string
}

// NewTmuxManager creates a TmuxManager for the given session name.
func NewTmuxManager(sessionName string) *TmuxManager {
	return &TmuxManager{sessionName: sessionName}
}

// HasTmux returns true if tmux is in PATH.
func HasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// IsITerm2 returns true if running in iTerm2 (supports tmux -CC).
func IsITerm2() bool {
	return os.Getenv("TERM_PROGRAM") == "iTerm.app"
}

// CreateSession creates a new detached tmux session. If the session already
// exists, this is a no-op.
func (t *TmuxManager) CreateSession() error {
	if t.sessionExists() {
		return nil
	}
	cmd := exec.Command("tmux", "new-session", "-d", "-s", t.sessionName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-session: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// SpawnAgent creates a new window in the session running the given command.
// If a window with the same slug already exists, it is killed first.
func (t *TmuxManager) SpawnAgent(slug string, command string, args []string, env []string) error {
	// Build the full shell command string.
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, command)
	parts = append(parts, args...)
	fullCmd := strings.Join(parts, " ")

	// Build environment: inherit current env, strip Claude vars, add extras.
	cleanEnv := filteredClaudeEnv()
	cleanEnv = append(cleanEnv, env...)

	cmd := exec.Command("tmux", "new-window", "-d", "-t", t.sessionName, "-n", slug, fullCmd)
	cmd.Env = cleanEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-window %s: %s: %w", slug, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// CapturePaneContent captures the visible text from an agent's tmux pane.
func (t *TmuxManager) CapturePaneContent(slug string) (string, error) {
	target := t.sessionName + ":" + slug
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-t", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane %s: %s: %w", slug, strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// KillSession kills the entire tmux session and all agent windows.
func (t *TmuxManager) KillSession() error {
	if !t.sessionExists() {
		return nil
	}
	cmd := exec.Command("tmux", "kill-session", "-t", t.sessionName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux kill-session: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ListWindows returns the names of all windows in the session.
func (t *TmuxManager) ListWindows() ([]string, error) {
	cmd := exec.Command("tmux", "list-windows", "-t", t.sessionName, "-F", "#{window_name}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows: %s: %w", strings.TrimSpace(string(out)), err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	// Filter empty lines.
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}

// AttachHint returns a hint string telling the user how to view an agent's terminal.
func (t *TmuxManager) AttachHint(slug string) string {
	if IsITerm2() {
		return "tmux -CC attach -t " + t.sessionName
	}
	return fmt.Sprintf("tmux select-window -t %s:%s", t.sessionName, slug)
}

// sessionExists checks if the tmux session already exists.
func (t *TmuxManager) sessionExists() bool {
	cmd := exec.Command("tmux", "has-session", "-t", t.sessionName)
	return cmd.Run() == nil
}

