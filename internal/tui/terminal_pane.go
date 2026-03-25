package tui

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty/v2"
)

// claudeEnvPrefixes are env var prefixes stripped from child processes to avoid
// Claude Code detecting a recursive invocation.
var claudeEnvPrefixes = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_SESSION",
	"CLAUDE_CODE_PARENT_SESSION",
}

// TerminalPane wraps a single PTY process with a VT terminal emulator.
// It is the building block for embedded Claude Code terminals.
type TerminalPane struct {
	slug     string
	name     string
	emulator *vt.SafeEmulator
	ptmx     *os.File
	cmd      *exec.Cmd
	focused  bool
	width    int
	height   int
	alive    bool
	mu       sync.Mutex

	// observerWriter receives a copy of all PTY output for the GossipBus.
	observerWriter io.Writer
}

// NewTerminalPane creates a TerminalPane with a VT emulator sized to w x h.
func NewTerminalPane(slug, name string, w, h int) *TerminalPane {
	return &TerminalPane{
		slug:     slug,
		name:     name,
		emulator: vt.NewSafeEmulator(w, h),
		width:    w,
		height:   h,
	}
}

// Spawn starts a process inside a PTY and begins feeding output to the emulator.
func (p *TerminalPane) Spawn(command string, args []string, env []string, cwd string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	cmd.Env = append(filteredClaudeEnv(), env...)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(p.height),
		Cols: uint16(p.width),
	})
	if err != nil {
		return err
	}

	p.cmd = cmd
	p.ptmx = ptmx
	p.alive = true

	// Reader goroutine: PTY -> emulator (and observer if set).
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				data := buf[:n]
				p.emulator.Write(data)

				p.mu.Lock()
				w := p.observerWriter
				p.mu.Unlock()
				if w != nil {
					w.Write(data)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait goroutine: marks pane as dead when process exits.
	go func() {
		cmd.Wait()
		p.mu.Lock()
		p.alive = false
		p.mu.Unlock()
	}()

	return nil
}

// View returns the emulator's rendered ANSI output.
func (p *TerminalPane) View() string {
	return p.emulator.Render()
}

// SendKey writes raw key bytes to the PTY.
func (p *TerminalPane) SendKey(data []byte) {
	p.mu.Lock()
	ptmx := p.ptmx
	p.mu.Unlock()
	if ptmx != nil {
		ptmx.Write(data)
	}
}

// SendText writes text to the PTY stdin.
func (p *TerminalPane) SendText(text string) {
	p.SendKey([]byte(text))
}

// Resize updates both the PTY window size and the emulator dimensions.
func (p *TerminalPane) Resize(w, h int) {
	p.mu.Lock()
	p.width = w
	p.height = h
	ptmx := p.ptmx
	p.mu.Unlock()

	if ptmx != nil {
		pty.Setsize(ptmx, &pty.Winsize{
			Rows: uint16(h),
			Cols: uint16(w),
		})
	}
	p.emulator.Resize(w, h)
}

// IsAlive reports whether the underlying process is still running.
func (p *TerminalPane) IsAlive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.alive
}

// Slug returns the pane's slug identifier.
func (p *TerminalPane) Slug() string {
	return p.slug
}

// Name returns the pane's display name.
func (p *TerminalPane) Name() string {
	return p.name
}

// SetObserverWriter sets the writer that receives a copy of PTY output.
func (p *TerminalPane) SetObserverWriter(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.observerWriter = w
}

// SetFocused sets the focused state of this pane.
func (p *TerminalPane) SetFocused(focused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.focused = focused
}

// IsFocused reports whether this pane is currently focused.
func (p *TerminalPane) IsFocused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.focused
}

// Close gracefully shuts down the PTY process.
// Sends SIGTERM first, waits up to 5 seconds, then SIGKILL if needed.
func (p *TerminalPane) Close() {
	p.mu.Lock()
	cmd := p.cmd
	ptmx := p.ptmx
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Try graceful shutdown.
	cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Signal(syscall.SIGKILL)
		<-done
	}

	if ptmx != nil {
		ptmx.Close()
	}

	p.mu.Lock()
	p.alive = false
	p.mu.Unlock()
}

// filteredClaudeEnv returns os.Environ() with Claude Code env vars removed.
func filteredClaudeEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		skip := false
		for _, prefix := range claudeEnvPrefixes {
			if key == prefix {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, kv)
		}
	}
	return out
}
