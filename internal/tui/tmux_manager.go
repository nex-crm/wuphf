package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// TmuxManager manages a tmux session with one window per agent.
type TmuxManager struct {
	sessionName string
	pipeDir     string
	pipePaths   map[string]string
	pipeFiles   map[string]*os.File
}

// NewTmuxManager creates a TmuxManager for the given session name.
func NewTmuxManager(sessionName string) *TmuxManager {
	return &TmuxManager{
		sessionName: sessionName,
		pipePaths:   make(map[string]string),
		pipeFiles:   make(map[string]*os.File),
	}
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

// AttachObserverPipe streams fresh pane output through a FIFO so observers can
// read it directly without polling capture-pane snapshots.
func (t *TmuxManager) AttachObserverPipe(slug string) (io.ReadCloser, error) {
	if err := t.ensurePipeDir(); err != nil {
		return nil, err
	}
	path := filepath.Join(t.pipeDir, slug+".fifo")
	_ = os.Remove(path)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		return nil, fmt.Errorf("mkfifo %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open fifo %s: %w", path, err)
	}
	cmd := exec.Command("tmux", "pipe-pane", "-o", "-t", t.sessionName+":"+slug, "cat > "+tmuxShellQuote(path))
	if out, err := cmd.CombinedOutput(); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("tmux pipe-pane %s: %s: %w", slug, strings.TrimSpace(string(out)), err)
	}
	t.pipePaths[slug] = path
	t.pipeFiles[slug] = file
	return file, nil
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
	t.closeObserverPipes()
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

func (t *TmuxManager) ensurePipeDir() error {
	if t.pipeDir != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", t.sessionName+"-pipes-")
	if err != nil {
		return err
	}
	t.pipeDir = dir
	return nil
}

func (t *TmuxManager) closeObserverPipes() {
	for slug, file := range t.pipeFiles {
		if file != nil {
			_ = file.Close()
		}
		delete(t.pipeFiles, slug)
	}
	for slug, path := range t.pipePaths {
		_ = os.Remove(path)
		delete(t.pipePaths, slug)
	}
	if t.pipeDir != "" {
		_ = os.RemoveAll(t.pipeDir)
		t.pipeDir = ""
	}
}

func tmuxShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
