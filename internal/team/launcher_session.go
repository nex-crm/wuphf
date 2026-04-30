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
// Drains every long-lived goroutine before broker.Stop and the
// per-launch tempdir RemoveAll. Pre-fix the scheduler goroutine
// outlived Kill (now drained via schedulerWorker.Stop) but the
// headless workers were only context-cancelled — cancel kicks the
// subprocess but the worker goroutine takes a tick to unwind, and
// it can race os.RemoveAll(launchTempDirPath) inside
// cleanupAgentTempFiles by writing a fresh per-agent prompt/MCP
// file into a directory the cleanup is removing.
//
// Sequence:
//  1. schedulerWorker.Stop()    — drain watchdog
//  2. headless.cancel()         — kick subprocess
//  3. stopHeadlessWorkers()     — wait on workerWg
//  4. broker.Stop()             — listener teardown
//  5. cleanupAgentTempFiles()   — rm -rf launch dir
func (l *Launcher) Kill() error {
	if l.schedulerWorker != nil {
		l.schedulerWorker.Stop()
	}
	if l.headless.cancel != nil {
		l.headless.cancel()
	}
	l.stopHeadlessWorkers()
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
		// session is gone". isMissingTmuxSession matches tmux's
		// "can't find" / "no server" / "error connecting" outputs,
		// so kill-twice-in-a-row no longer surfaces a misleading
		// exit-1 to the caller.
		if isMissingTmuxSession(string(out)) {
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
