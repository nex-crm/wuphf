package team

// launcher_session.go owns the user-facing session state-change
// methods (PLAN.md §C20): Attach (re-attach the user's terminal to
// the running tmux session), Kill (graceful drain + tear-down), and
// ResetSession (clear broker state + manifest + per-agent temp files
// for a fresh-start). Each is small (15-30 lines) but together they
// form the "user can drive the running team" surface — separate from
// boot (Launch) and reconfigure (ReconfigureSession).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nex-crm/wuphf/internal/provider"
)

// Attach attaches the user's terminal to the tmux session.
// In iTerm2: uses tmux -CC for native panes (resizable, close buttons, drag).
// Otherwise: uses regular tmux attach with -L wuphf to avoid nesting.
func (l *Launcher) Attach() error {
	var cmd *exec.Cmd
	if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
		// tmux -CC mode: iTerm2 takes over window management.
		// Creates native iTerm2 tabs/splits for each tmux window/pane.
		cmd = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "-CC", "attach-session", "-t", l.sessionName)
	} else {
		cmd = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "attach-session", "-t", l.sessionName)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Unset TMUX env to allow nesting
	cmd.Env = filterEnv(os.Environ(), "TMUX")
	return cmd.Run()
}

// Kill destroys the tmux session, all agent processes, and the broker. Also
// removes per-agent temp files (MCP config + system prompt) so the broker
// token and prompt content do not linger in $TMPDIR.
func (l *Launcher) Kill() error {
	if l.headless.cancel != nil {
		l.headless.cancel()
	}
	if l.broker != nil {
		l.broker.Stop()
	}
	// Clean temp files before tearing down tmux so the claude processes are
	// still alive to release any open handles (harmless, but principle of
	// least surprise).
	l.cleanupAgentTempFiles()
	if !l.targeter().UsesPaneRuntime() {
		if err := killPersistedOfficeProcess(); err != nil {
			return err
		}
		killStaleHeadlessTaskRunners()
		_ = clearOfficePIDFile()
		return nil
	}
	err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()
	if err != nil {
		// Check if the session simply doesn't exist
		out, _ := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "list-sessions").CombinedOutput()
		if strings.Contains(string(out), "no server") || strings.Contains(string(out), "error connecting") {
			return nil // no session running, nothing to kill
		}
		return err
	}
	return nil
}

func (l *Launcher) ResetSession() error {
	if !l.targeter().RequiresClaudeSessionReset() {
		if l != nil && l.broker != nil {
			l.broker.Reset()
			return nil
		}
		if err := ResetBrokerState(); err != nil {
			return fmt.Errorf("reset broker state: %w", err)
		}
		return nil
	}
	if err := provider.ResetClaudeSessions(); err != nil {
		return fmt.Errorf("reset Claude sessions: %w", err)
	}
	if l != nil && l.broker != nil {
		l.broker.Reset()
		return nil
	}
	if err := ResetBrokerState(); err != nil {
		return fmt.Errorf("reset broker state: %w", err)
	}
	return nil
}
