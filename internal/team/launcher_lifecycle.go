package team

// launcher_lifecycle.go owns the Launcher lifecycle methods
// (PLAN.md §C12): Launch boots broker + tmux + goroutines; Attach
// re-attaches an existing tmux session; Kill drains workers and
// tears down the session; ResetSession + ReconfigureSession +
// reconfigureVisibleAgents handle the user-visible "fresh team"
// flows; respawnPanesAfterReseed is the office_reseeded
// notification handler.
//
// Split out of launcher.go so the orchestrator file stays focused
// on construction (NewLauncher) and shared state (Launcher struct,
// lazy accessors). No new types or behaviour changes — pure file
// move.

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
)

// Launch starts a tmux session hosting the channel-view TUI and the shared
// broker. Agents run headlessly by default via `claude --print` per turn;
// per-agent interactive panes are reserved as an internal fallback primitive
// (see trySpawnWebAgentPanes) and are not spawned at startup. The user
// attaches to tmux to drive the channel view; agent output is surfaced
// through the channel timeline rather than a dedicated pane.
func (l *Launcher) Launch() error {
	if l.usesCodexRuntime() {
		return l.launchHeadlessCodex()
	}
	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig

	// Kill any stale broker from a previous run
	killStaleBroker()

	// Start the shared channel broker
	l.broker = NewBroker()
	l.broker.runtimeProvider = l.provider
	l.broker.packSlug = l.packSlug
	l.broker.blankSlateLaunch = l.blankSlateLaunch
	// Wire the notebook-promotion reviewer resolver from the active
	// blueprint. Without this, every promotion falls back to "ceo"
	// regardless of blueprint reviewer_paths. Safe on nil (packs-only
	// launches or blank-slate runs).
	if l.operationBlueprint != nil {
		bp := l.operationBlueprint
		l.broker.SetReviewerResolver(func(wikiPath string) string {
			return bp.ResolveReviewer(wikiPath)
		})
	}
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.SetFocusMode(l.focusMode); err != nil {
		return fmt.Errorf("set focus mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	// Pre-seed any default skills declared by the pack (idempotent).
	// Always seed the cross-cutting productivity skills (grill-me, tdd,
	// diagnose, etc., adapted from github.com/mattpocock/skills) on top of
	// whatever the active pack defines. They're useful for every install,
	// not just packs that explicitly enumerate them.
	if l.pack != nil {
		l.broker.SeedDefaultSkills(agent.AppendProductivitySkills(l.pack.DefaultSkills))
	} else {
		l.broker.SeedDefaultSkills(agent.AppendProductivitySkills(nil))
	}

	// Kill any existing session
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	// Resolve wuphf binary path for the channel view
	wuphfBinary, _ := os.Executable()
	if err := os.MkdirAll(filepath.Dir(channelStderrLogPath()), 0o700); err != nil {
		return fmt.Errorf("prepare channel log dir: %w", err)
	}

	// Window 0 "team": channel on the left
	// Pass broker token via env so channel view + agents can authenticate
	channelEnv := []string{
		fmt.Sprintf("WUPHF_BROKER_TOKEN=%s", l.broker.Token()),
		fmt.Sprintf("WUPHF_BROKER_BASE_URL=%s", l.BrokerBaseURL()),
	}
	if l.isOneOnOne() {
		channelEnv = append(channelEnv,
			"WUPHF_ONE_ON_ONE=1",
			fmt.Sprintf("WUPHF_ONE_ON_ONE_AGENT=%s", l.oneOnOneAgent()),
		)
	}
	channelCmd := fmt.Sprintf("%s %s --channel-view 2>>%s", strings.Join(channelEnv, " "), wuphfBinary, shellQuote(channelStderrLogPath()))
	err = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "new-session", "-d",
		"-s", l.sessionName,
		"-n", "team",
		"-c", l.cwd,
		channelCmd,
	).Run()
	if err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}

	// Keep tmux mouse off for this session so native terminal selection/copy works.
	// WUPHF is keyboard-first; we don't want the TUI or tmux to steal mouse events.
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"mouse", "off",
	).Run()

	// Hide tmux's default status bar — our channel TUI has its own.
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"status", "off",
	).Run()
	// Keep panes visible if a process exits so crashes don't collapse the layout.
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-window-option", "-t", l.sessionName+":team",
		"remain-on-exit", "on",
	).Run()

	// Pane border cosmetics — kept so the channel pane renders with a border
	// title. Per-agent panes are not spawned in the default path; they live
	// only as an internal fallback (see trySpawnWebAgentPanes).
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-status", "top",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-format", " #{pane_title} ",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-style", "fg=colour240",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-active-border-style", "fg=colour45",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-lines", "heavy",
	).Run()

	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
		"-T", "📢 channel",
	).Run()

	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-window",
		"-t", l.sessionName+":team",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
	).Run()

	// Headless context for per-turn Claude invocations. Used by both TUI and
	// web modes since agent dispatch is headless by default.
	l.headless.ctx, l.headless.cancel = context.WithCancel(context.Background())
	l.resumeInFlightWork()

	go l.watchChannelPaneLoop(channelCmd)
	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.notifyOfficeChangesLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}

// respawnPanesAfterReseed restarts the interactive agent panes so they match
// the newly-seeded roster from onboarding. Best-effort: the codex runtime has
// no interactive panes, and reconfigureVisibleAgents handles an uninitialised
// paneBackedAgents state by no-op'ing. Errors are logged but do not propagate
// — failing to respawn leaves the previous panes running (degraded, but the
// headless path can still deliver).
func (l *Launcher) respawnPanesAfterReseed() {
	if l == nil {
		return
	}
	l.provider = config.ResolveLLMProvider("")
	if err := l.reconfigureVisibleAgents(); err != nil {
		// "No tmux server running" / "can't find session" are the expected
		// states when the launcher runs in headless/web mode without a
		// persistent tmux session — reconfigureVisibleAgents tries to
		// attach, fails, and the headless dispatch path takes over silently.
		// Logging it as an error makes a normal code path look like a
		// recurring failure in the console. Uses the canonical tmux-error
		// classifier (isMissingTmuxSession) so this path stays consistent
		// with every other tmux-attach site. Real failures (permission
		// denied, exec-not-found with no tmux prefix) keep logging.
		if isMissingTmuxSession(err.Error()) {
			return
		}
		log.Printf("office_reseeded: respawn panes failed: %v", err)
	}
}

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

func (l *Launcher) ReconfigureSession() error {
	if !l.targeter().UsesPaneRuntime() {
		if err := provider.ResetClaudeSessions(); err != nil {
			return fmt.Errorf("reset Claude sessions: %w", err)
		}
		if err := l.panes().ClearAgentPanes(); err != nil {
			return err
		}
		l.panes().ClearOverflowAgentWindows()
		return nil
	}
	return l.reconfigureVisibleAgents()
}

func (l *Launcher) reconfigureVisibleAgents() error {
	l.provider = config.ResolveLLMProvider("")
	if !l.targeter().UsesPaneRuntime() {
		if l.paneBackedAgents {
			l.panes().KillSession()
			l.paneBackedAgents = false
		}
		return nil
	}

	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig

	if err := provider.ResetClaudeSessions(); err != nil {
		return fmt.Errorf("reset Claude sessions: %w", err)
	}

	l.failedPaneSlugs = nil

	// Use respawn-pane to restart agent processes IN PLACE.
	// This preserves pane sizes and positions (no layout reset).
	panes, err := l.panes().ListTeamPanes()
	if err != nil {
		return err
	}
	l.panes().ClearOverflowAgentWindows()

	// Respawn each agent pane in place, preserving layout.
	// Never clear+recreate panes — that destroys the channel's layout.
	visibleMembers := l.targeter().VisibleMembers()
	if len(panes) != len(visibleMembers) {
		if err := l.panes().ClearAgentPanes(); err != nil {
			return err
		}
		if _, err := l.panes().SpawnVisibleAgents(); err != nil {
			return err
		}
		l.panes().SpawnOverflowAgents()
		go l.panes().DetectDeadPanesAfterSpawn(visibleMembers)
		if l.broker != nil {
			go l.primeVisibleAgents()
		}
		return nil
	}

	for _, idx := range panes {
		// Map pane index to agent slug (pane 1 = first agent, etc.)
		slugIdx := idx - 1 // pane 0 is channel
		if slugIdx < 0 || slugIdx >= len(visibleMembers) {
			continue
		}
		slug := visibleMembers[slugIdx].Slug
		cmd, err := l.claudeCommand(slug, l.buildPrompt(slug))
		if err != nil {
			fmt.Fprintf(os.Stderr, "respawn pane for %s: %v\n", slug, err)
			l.recordPaneSpawnFailure(slug, fmt.Sprintf("claudeCommand: %v", err))
			continue
		}

		// respawn-pane -k kills the current process and starts a new one
		// in the same pane — preserving size and position
		out, err := l.panes().RespawnAgentPane(idx, l.cwd, cmd)
		if err != nil {
			detail := strings.TrimSpace(string(out))
			reason := err.Error()
			if detail != "" {
				reason = fmt.Sprintf("%s (tmux: %s)", reason, detail)
			}
			fmt.Fprintf(os.Stderr,
				"  Agents:  respawn-pane for %s failed (%s); falling back to headless\n",
				slug, reason,
			)
			l.recordPaneSpawnFailure(slug, reason)
		}
	}
	l.panes().SpawnOverflowAgents()
	go l.panes().DetectDeadPanesAfterSpawn(visibleMembers)

	if l.broker != nil {
		go l.primeVisibleAgents()
	}

	return nil
}
