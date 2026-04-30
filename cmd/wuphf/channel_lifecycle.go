package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

// Process / lifecycle helpers for the channel TUI binary:
//   - tickChannel: 2s heartbeat that drives liveness redraws
//   - killTeamSession: best-effort cleanup at exit (tmux kill + broker
//     ping). Not a graceful Stop() — agents own their own tmux panes
//     and we can't await them.
//   - runChannelView: process entrypoint; runs onboarding/splash gates
//     before booting the channel program. Wraps in a recover so a
//     channel crash writes to the crash log instead of taking the
//     whole binary down silently.
//   - reportChannelCrash: human-readable last-words to stderr + crash
//     log handoff.

func tickChannel() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return channelTickMsg(t)
	})
}

// killTeamSession kills the entire wuphf-team tmux session and all agent processes.
func killTeamSession() {
	// Best-effort cleanup at process exit; cap each step so a hung tmux or
	// broker doesn't keep us alive forever.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Kill tmux session (kills all agent processes in all panes/windows)
	_ = exec.CommandContext(ctx, "tmux", "-L", "wuphf", "kill-session", "-t", "wuphf-team").Run()
	// Stop the broker
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerURL("/health"), nil)
	if err != nil {
		return
	}
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

func runChannelView(threadsCollapsed bool, initialApp channelui.OfficeApp, skipSplash bool) {
	defer func() {
		if r := recover(); r != nil {
			reportChannelCrash(fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack()))
		}
	}()

	// Check if onboarding is needed before launching the channel view.
	if os.Getenv("WUPHF_SKIP_ONBOARDING") == "" {
		state, err := fetchOnboardingState(brokerBaseURL())
		if err == nil && !state.Onboarded {
			om := newOnboardingModel(brokerBaseURL(), 0, 0)
			op := tea.NewProgram(om, tea.WithAltScreen())
			if _, err := op.Run(); err != nil {
				reportChannelCrash(fmt.Sprintf("onboarding error: %v\n", err))
				return
			}
			// Fall through to channel view after onboarding completes.
		}
	}

	if !skipSplash && os.Getenv("WUPHF_NO_SPLASH") == "" {
		splash := tea.NewProgram(newSplashModel(), tea.WithAltScreen())
		if _, err := splash.Run(); err != nil {
			reportChannelCrash(fmt.Sprintf("splash error: %v\n", err))
			return
		}
	}

	p := tea.NewProgram(newChannelModelWithApp(threadsCollapsed, initialApp), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		reportChannelCrash(fmt.Sprintf("channel view error: %v\n", err))
	}
}

func reportChannelCrash(details string) {
	_ = channelui.AppendChannelCrashLog(details)
	fmt.Fprintln(os.Stderr, "WUPHF channel crashed.")
	fmt.Fprintln(os.Stderr, "Log:", channelui.ChannelCrashLogPath())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The rest of the team is still running.")
	if strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")) != "" {
		fmt.Fprintln(os.Stderr, "Restart WUPHF when ready to reconnect to the headless office runtime.")
	} else {
		fmt.Fprintln(os.Stderr, "Use `tmux -L wuphf attach -t wuphf-team` to inspect panes,")
		fmt.Fprintln(os.Stderr, "then restart WUPHF when ready.")
	}
	select {}
}
