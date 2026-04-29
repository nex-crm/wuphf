package team

// pane_lifecycle.go owns pure pane-lifecycle helpers extracted from
// launcher.go (PLAN.md §C5, partial). The shell-out methods (spawn*,
// trySpawnWebAgentPanes, watchChannelPaneLoop, capturePaneContent, etc.)
// stay on Launcher pending the tmuxRunner interface — that follow-up
// extraction (C5b) introduces a fakeable runner so those methods become
// testable too. Today's file is a "header split" for the helpers that
// don't shell out and can already be exercised in tests.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// channelPaneNeedsRespawn parses a tmux display-message status string of
// the form "{exit-status} {pane-pid}" and returns true when the pane has
// exited (exit-status == "1"). Returns false on empty input so a transient
// tmux failure doesn't trigger a spurious respawn.
func channelPaneNeedsRespawn(status string) bool {
	if strings.TrimSpace(status) == "" {
		return false
	}
	fields := strings.Fields(status)
	if len(fields) == 0 {
		return false
	}
	return fields[0] == "1"
}

// isNoSessionError matches tmux error messages indicating the session or
// server isn't available. Used by the channel-pane watcher to distinguish
// "session gone, respawn it" from genuinely fatal tmux errors.
func isNoSessionError(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "can't find") || strings.Contains(msg, "no server")
}

// isMissingTmuxSession matches the broader set of tmux outputs that mean
// "no usable session/server" — including filesystem-level "no such file"
// errors that come from tmux failing to open its socket.
func isMissingTmuxSession(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "no server") ||
		strings.Contains(normalized, "can't find") ||
		strings.Contains(normalized, "failed to connect to server") ||
		strings.Contains(normalized, "error connecting to") ||
		strings.Contains(normalized, "no such file or directory")
}

// channelStderrLogPath returns the path the channel pane's stderr should
// be redirected to for post-mortem inspection. Falls back to a CWD-local
// dotfile when the runtime home dir is unset.
func channelStderrLogPath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return ".wuphf-channel-stderr.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-stderr.log")
}

// channelPaneSnapshotPath returns the path the channel pane's last-known
// captured content should be archived to before respawn. Symmetric to
// channelStderrLogPath.
func channelPaneSnapshotPath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return ".wuphf-channel-pane.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-pane.log")
}

// shellQuote single-quotes s for safe interpolation into a shell command.
// Embedded single quotes are escaped via the standard '\” sequence so
// quoting is preserved through tmux's command-line parsing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// parseAgentPaneIndices parses tmux list-panes output (one pane per line,
// "<index> <title>") and returns the integer indices that point at agent
// panes. Pane 0 (the channel/observer) and any pane whose title contains
// "channel" are skipped — those are infrastructure, not agents.
func parseAgentPaneIndices(output string) []int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var panes []int
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 0 {
			continue
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		title := ""
		if len(parts) > 1 {
			title = parts[1]
		}
		if idx == 0 || strings.Contains(title, "channel") {
			continue
		}
		panes = append(panes, idx)
	}
	return panes
}

// shouldPrimeClaudePane returns true when the pane content shows Claude's
// startup interactivity (folder-trust, security-guide blurbs, the
// "press Enter" prompt) that the priming helper needs to clear before
// dispatch can type into the pane.
func shouldPrimeClaudePane(content string) bool {
	normalized := strings.ToLower(content)
	return strings.Contains(normalized, "trust this folder") ||
		strings.Contains(normalized, "security guide") ||
		strings.Contains(normalized, "enter to confirm") ||
		strings.Contains(normalized, "claude in chrome")
}

// paneFallbackMessages renders the two user-facing messages for a pane-
// spawn fallback (stderr banner + broker #general post). Headless is the
// normal default now, so the fallback message is neutral — it only fires
// when something in the runtime promoted us to panes and the spawn failed.
//
// Pure function so it can be unit-tested without touching os.Stderr or the
// broker. Keep in sync with reportPaneFallback in launcher.go.
func paneFallbackMessages(tmuxInstalled bool, detail string) (stderrMsg, brokerMsg string) {
	const headlessBlurb = "Continuing with the default headless path (`claude --print` per turn on your normal subscription)."
	const brokerBlurb = "Running in headless mode (%s). Agent turns dispatch as `claude --print` on your normal subscription."
	if !tmuxInstalled {
		stderrMsg = fmt.Sprintf(
			"  Agents:  pane-backed fallback attempted but tmux not found (%s). %s Install tmux if you want the fallback to be available.\n",
			detail, headlessBlurb,
		)
		brokerMsg = fmt.Sprintf(
			brokerBlurb+" Install tmux so the pane-backed fallback is available next time.",
			detail,
		)
		return
	}
	stderrMsg = fmt.Sprintf(
		"  Agents:  pane-backed fallback attempted but unavailable (%s). %s tmux IS installed but rejected the launch command; please file a bug with the detail above at https://github.com/nex-crm/wuphf/issues.\n",
		detail, headlessBlurb,
	)
	brokerMsg = fmt.Sprintf(
		brokerBlurb+" tmux is installed but rejected the pane-spawn command; please file a bug so we can fix the regression.",
		detail,
	)
	return
}
