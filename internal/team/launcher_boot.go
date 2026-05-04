package team

// launcher_boot.go owns the tmux-mode boot path (PLAN.md §C20).
// Launch() composes the broker, spawns the channel TUI in tmux,
// kicks off the headless context + notification goroutines, and
// returns once the session is steady-state. Web-mode boot lives in
// launcher_web.go (LaunchWeb); headless-only boot lives in
// headless_codex.go (launchHeadlessCodex). All three share the
// broker.Start + writeOfficePIDFile + goroutine fan-out shape but
// differ in whether they spawn tmux or print a web URL.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	l.installBroker(NewBroker())
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

	stopTransports, err := RegisterTransports(l.broker)
	if err != nil {
		// Non-fatal: a misconfigured optional adapter (e.g. bad Telegram token)
		// should not prevent the office from starting.
		fmt.Fprintf(os.Stderr, "warning: transport registration: %v\n", err)
	}

	// Kill any existing session
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	// Resolve wuphf binary path for the channel view
	wuphfBinary, _ := os.Executable()
	if err := os.MkdirAll(filepath.Dir(channelStderrLogPath()), 0o700); err != nil {
		// Stop adapters before the broker so they can flush in-flight sends.
		stopTransports()
		l.broker.Stop()
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
		stopTransports()
		l.broker.Stop()
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

func (l *Launcher) launchHeadlessCodex() error {
	killStaleBroker()
	killStaleHeadlessTaskRunners()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	l.installBroker(NewBroker())
	l.broker.runtimeProvider = l.provider
	l.broker.packSlug = l.packSlug
	l.broker.blankSlateLaunch = l.blankSlateLaunch
	// Wire the notebook-promotion reviewer resolver from the active
	// blueprint, mirroring Launch(). Without this, every promotion
	// in headless mode falls back to "ceo" regardless of blueprint
	// reviewer_paths.
	if l.operationBlueprint != nil {
		bp := l.operationBlueprint
		l.broker.SetReviewerResolver(func(wikiPath string) string {
			return bp.ResolveReviewer(wikiPath)
		})
	}
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	// SetFocusMode mirrors Launch() so that the broker's focus-mode
	// predicate (used by isFocusModeEnabled) is wired before the broker
	// becomes reachable. Pre-fix headless launches missed this, leaving
	// isFocusModeEnabled stuck on l.focusMode regardless of broker state.
	if err := l.broker.SetFocusMode(l.focusMode); err != nil {
		return fmt.Errorf("set focus mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	stopTransports, err := RegisterTransports(l.broker)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: transport registration: %v\n", err)
	}

	if err := writeOfficePIDFile(); err != nil {
		stopTransports()
		l.broker.Stop()
		return fmt.Errorf("write office pid: %w", err)
	}

	l.headless.ctx, l.headless.cancel = context.WithCancel(context.Background())

	l.resumeInFlightWork()
	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.notifyOfficeChangesLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}
