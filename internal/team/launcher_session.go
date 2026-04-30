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
//
// PLAN.md §C25 staff-review fix: drain the watchdog scheduler before
// tearing down the broker. Pre-fix, the scheduler goroutine outlived
// Kill and held a reference to the broker; the Stop() call here unblocks
// done.Wait inside the scheduler so Kill returns once the goroutine has
// exited.
func (l *Launcher) Kill() error {
	if l.schedulerWorker != nil {
		l.schedulerWorker.Stop()
	}
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
	out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).CombinedOutput()
	if err != nil {
		// "session not found" is the desired post-condition for
		// Kill, not an error — caller's intent is "make sure the
		// session is gone". Match against tmux's literal "can't find
		// session" error text plus the broader "no server" /
		// "error connecting" outputs we'd see if the socket itself
		// is already torn down. Without this check, killing a
		// session twice (or killing one that exited on its own)
		// surfaces a misleading exit-1 error to the caller.
		if isMissingTmuxSession(string(out)) || strings.Contains(string(out), "can't find session") {
			return nil
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
