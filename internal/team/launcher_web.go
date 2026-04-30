package team

// launcher_web.go owns the web-mode entry points (PLAN.md §C8): the
// preflight check, the LaunchWeb path that boots the broker + web UI
// without tmux, the optional Nex-onboarding offer, and the small
// browser-launch helpers used only by web mode. Splitting these off
// keeps launcher.go focused on the tmux-mode orchestrator while letting
// web-only imports (`net`, `golang.org/x/term`, `internal/setup`,
// `internal/nex`, `internal/runtimebin` for the opencode lookup) sit in
// one file. No new types or behaviour changes — pure file split.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
	"github.com/nex-crm/wuphf/internal/runtimebin"
	"github.com/nex-crm/wuphf/internal/setup"
)

// PreflightWeb checks only for claude (no tmux requirement for web mode).
//
// When the user has not yet completed onboarding we deliberately skip the
// runtime-binary check: the whole point of the web-mode onboarding wizard is
// to pick a runtime. Hard-failing here would make the binary unlaunchable
// until the user already had the CLI they were trying to pick. A missing
// runtime is still caught at first-dispatch time with a clear message once
// onboarding has committed a choice to ~/.wuphf/config.json.
func (l *Launcher) PreflightWeb() error {
	if !isOnboarded() {
		if _, _, note := checkGHCapability(); note != "" {
			fmt.Fprintf(os.Stderr, "note: %s\n", note)
		}
		return nil
	}
	if l.usesCodexRuntime() {
		if l.usesOpencodeRuntime() {
			if _, err := runtimebin.LookPath("opencode"); err != nil {
				return fmt.Errorf("opencode not found. Install Opencode CLI (https://opencode.ai) and configure your provider credentials")
			}
			return nil
		}
		if _, err := exec.LookPath("codex"); err != nil {
			return fmt.Errorf("codex not found. Install Codex CLI and run `codex login`")
		}
		return nil
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude not found in PATH. Install Claude Code CLI first")
	}
	if _, _, note := checkGHCapability(); note != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", note)
	}
	return nil
}

// LaunchWeb starts the broker, web UI server, and background agents without tmux.
func (l *Launcher) LaunchWeb(webPort int) error {
	// Offer to wire Nex when the user hasn't opted out and nex-cli isn't yet
	// installed. `nex setup` handles detection and wiring for us — we just
	// surface the prompt.
	l.maybeOfferNex()

	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig
	l.webMode = true

	killStaleBroker()

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
	if err := writeOfficePIDFile(); err != nil {
		// Stop the broker we just started so we don't leave an
		// orphaned listener bound to the broker port. Without this,
		// a PID-file write failure (full disk, perms, …) leaves
		// the broker accepting requests with no PID record — the
		// next launch can't kill it cleanly.
		l.broker.Stop()
		return fmt.Errorf("write office pid: %w", err)
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

	l.broker.SetGenerateMemberFn(l.GenerateMemberTemplateFromPrompt)
	l.broker.SetGenerateChannelFn(l.GenerateChannelTemplateFromPrompt)
	if err := l.broker.ServeWebUI(webPort); err != nil {
		// The broker is already running and the office PID file is on
		// disk (above). On a port-bind failure we exit, so tear both
		// down — leaving the broker accepting requests on a "wuphf has
		// failed to start" path is worse than a clean exit, and a stale
		// PID file would block the next launch attempt's writeOfficePID.
		l.broker.Stop()
		_ = clearOfficePIDFile()
		return fmt.Errorf("web UI failed to start: %w\n\nIs port %d already in use? Try: wuphf --web-port %d", err, webPort, webPort+1)
	}

	// Default path: headless `claude --print` per turn. Anthropic re-sanctioned
	// this invocation (OpenClaw policy note, 2026-04), so it runs on the user's
	// normal subscription quota — no separate extra-usage quota is charged on
	// top. The legacy interactive pane-per-agent mode remains reachable via
	// trySpawnWebAgentPanes as an internal fallback primitive, but is not
	// invoked at startup.

	// Headless context is used for codex runtime, default dispatch, and
	// per-turn operations that don't fit a long-lived pane session.
	l.headless.ctx, l.headless.cancel = context.WithCancel(context.Background())
	l.resumeInFlightWork()

	// Stream tmux pane output to the web UI's per-agent stream so users see
	// live Claude TUI activity (thinking, tool calls, responses) during a
	// pane-backed turn. No-op when paneBackedAgents is false.
	l.startPaneCaptureLoops(l.headless.ctx)

	go l.notifyAgentsLoop()
	go l.notifyTaskActionsLoop()
	go l.notifyOfficeChangesLoop()
	go l.pollNexNotificationsLoop()
	go l.watchdogSchedulerLoop()
	if l.paneBackedAgents {
		go l.primeVisibleAgents()
	}

	// Use 127.0.0.1 in both the printed URL and the readiness probe so the
	// dial target matches what the browser will request. localhost can
	// resolve to ::1 first on IPv6-preferring setups, while ServeWebUI binds
	// only to 127.0.0.1 — that mismatch reproduces ERR_CONNECTION_REFUSED
	// even after the probe succeeds.
	webAddr := fmt.Sprintf("127.0.0.1:%d", webPort)
	webURL := fmt.Sprintf("http://%s", webAddr)
	fmt.Printf("\n  Web UI:  %s\n", webURL)
	fmt.Printf("  Broker:  %s\n", l.BrokerBaseURL())
	fmt.Printf("  Press Ctrl+C to stop.\n\n")

	if !l.noOpen {
		// Wait for the web server to actually accept connections before
		// triggering the browser. Otherwise users on cold starts (and PH
		// visitors clicking through `npx wuphf` for the first time) hit
		// ERR_CONNECTION_REFUSED before the listener is ready. 5s is a
		// generous ceiling: in practice the listener is up in milliseconds.
		// Skip the open if the listener never came up — opening a dead URL
		// just produces a confusing error page in the user's first second.
		if waitForWebReady(webAddr, 5*time.Second) {
			openBrowser(webURL)
		} else {
			fmt.Printf("  Web UI did not become reachable at %s within 5s; skipping browser auto-open.\n", webURL)
		}
	}

	// Broker, web UI, and background goroutines own the process lifetime;
	// Ctrl+C (default SIGINT) is the only exit path.
	select {}
}

// maybeOfferNex offers to wire up Nex for memory/context when nex-cli
// isn't already installed. Prints an explicit "skipping Nex" line when
// stdin isn't a TTY (npx, pipes, CI, containers) — fmt.Scanln returns
// empty in that case, which the prompt would have silently accepted as
// "yes" and tried to install. Users can rerun `nex setup` later or set
// WUPHF_NO_NEX=1 to suppress the offer.
func (l *Launcher) maybeOfferNex() {
	if config.ResolveNoNex() || nex.IsInstalled() {
		return
	}
	if !stdinIsTTY() {
		fmt.Println()
		fmt.Println("  Skipping Nex (no interactive terminal). Run `nex setup` later to add memory.")
		fmt.Println()
		return
	}
	fmt.Println()
	fmt.Print("  Connect Nex for memory and context? [Y/n] ")
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		// fmt.Scanln has two distinct error shapes here:
		//   - io.EOF: stdin was closed underneath us. By the time we
		//     reach this branch stdinIsTTY() already returned true, so
		//     EOF means the user explicitly hit Ctrl-D rather than
		//     answering. Treat as a deliberate skip.
		//   - any other error (most commonly "unexpected newline" from
		//     a bare Enter): the prompt label says [Y/n], so capital-Y
		//     is the visible default. Accept Enter as "yes" so the UX
		//     contract matches the prompt.
		if errors.Is(err, io.EOF) {
			fmt.Println("  Skipping Nex. Agents will work without organizational memory.")
			fmt.Println()
			return
		}
		answer = ""
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Println("  Skipping Nex. Agents will work without organizational memory.")
		fmt.Println()
		return
	}
	fmt.Println()
	fmt.Println("  Nex CLI not found. Installing...")
	if _, installErr := setup.InstallLatestCLI(context.Background()); installErr != nil {
		fmt.Printf("  Could not install: %v\n", installErr)
		fmt.Println("  Continuing without Nex.")
	}
	if nexBin := nex.BinaryPath(); nexBin != "" {
		cmd := exec.CommandContext(context.Background(), nexBin, "setup")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Setup did not complete: %v\n", err)
			fmt.Println("  Continuing without Nex.")
		} else {
			fmt.Println("  Nex connected.")
		}
	}
	fmt.Println()
}

// waitForWebReady polls addr until a TCP dial succeeds or the timeout
// elapses. It exists because ServeWebUI returns immediately and the
// listener can take a few hundred ms to come up — opening the browser
// before then produces ERR_CONNECTION_REFUSED in the user's first
// second of the product. Returns true when the listener accepted a
// connection within the timeout, false otherwise. LaunchWeb gates
// openBrowser on this return value, so a never-up listener results in
// a printed "skipping browser auto-open" line rather than a dead URL.
func waitForWebReady(addr string, timeout time.Duration) bool {
	dialer := &net.Dialer{Timeout: 250 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// stdinIsTTY reports whether os.Stdin is connected to a real terminal.
// Uses golang.org/x/term so /dev/null (a char device but not a TTY) is
// classified correctly — the original os.ModeCharDevice check let
// `npx ... </dev/null` fall back to the auto-yes install path, which
// is the cold-start bug this whole helper exists to prevent.
func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(context.Background(), "open", url)
	case "linux":
		cmd = exec.CommandContext(context.Background(), "xdg-open", url)
	case "windows":
		cmd = exec.CommandContext(context.Background(), "cmd", "/c", "start", "", url)
	default:
		return
	}
	_ = cmd.Start()
}
