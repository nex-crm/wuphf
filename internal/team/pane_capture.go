package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Pane capture defaults. Values are intentionally not user-configurable:
// 1s is a reasonable tradeoff between "feels live" and tmux CPU cost, and
// introducing a knob would add complexity without clear wins.
const (
	paneCapturePollInterval   = 1 * time.Second
	paneCaptureMaxFailures    = 5
	paneCaptureBackoffOnError = 2 * time.Second
	// After paneCaptureMaxFailures in a row, the pane is probably gone (office
	// reseed rebuilt tmux, pane crashed, etc.). Instead of giving up forever,
	// sleep this long then re-resolve the current pane target from the launcher
	// and try again. Users running wuphf for hours see office reseeds and panes
	// should recover without restarting the whole office. If the target is still
	// stale after the long sleep, we repeat — cheaper than dropping the agent's
	// live-stream feed.
	paneCaptureRetryAfterDeath = 30 * time.Second
	paneCaptureMaxDiffBytes    = 64 * 1024
	paneCaptureTruncateMarker  = "...[truncated]"
	paneCaptureHistoryLines    = 200 // tmux -S: scroll back lines to include
)

// ansiEscapePattern strips ANSI escape sequences (CSI, OSC, and standalone
// ESC-prefixed controls). It intentionally matches greedily within a single
// sequence — tmux capture-pane -J output is line-joined so sequences do not
// span lines in practice.
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// stripANSI removes ANSI escape sequences and common control characters that
// tmux pane captures can leak through (carriage returns, bells).
func stripANSI(s string) string {
	if s == "" {
		return s
	}
	s = ansiEscapePattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\a", "")
	return s
}

// diffPaneLines returns lines present in the new capture that were not present
// in the previous capture, preserving their order. This uses a line-set
// comparison rather than a byte offset so claude's TUI re-renders (which
// rewrite the entire visible region on each frame) do not produce spurious
// duplicate pushes.
//
// Exact-duplicate lines that already appear in prev are skipped. Blank lines
// are skipped entirely (they add no signal and TUI frames tend to emit many).
func diffPaneLines(prev, next []string) []string {
	if len(next) == 0 {
		return nil
	}
	seen := make(map[string]int, len(prev))
	for _, line := range prev {
		seen[line]++
	}
	out := make([]string, 0, len(next))
	for _, line := range next {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			continue
		}
		if seen[trimmed] > 0 {
			seen[trimmed]--
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// startPaneCaptureLoops kicks off one goroutine per pane-backed agent. Each
// goroutine polls tmux capture-pane on an interval, strips ANSI, diffs against
// the previous snapshot, and pushes new lines to the per-agent broker stream
// so the web UI's "live output" pane stays in sync with the real Claude
// session running in the tmux pane.
//
// Safe to call only when l.paneBackedAgents == true.
func (l *Launcher) startPaneCaptureLoops(ctx context.Context) {
	if !l.paneBackedAgents || l.broker == nil {
		return
	}
	targets := l.agentPaneTargets()
	for slug, target := range targets {
		if slug == "" || target.PaneTarget == "" {
			continue
		}
		go l.paneCaptureLoop(ctx, slug, target.PaneTarget)
	}
}

// paneCaptureLoop polls a single tmux pane on a fixed interval. It keeps
// running even after the pane disappears: after paneCaptureMaxFailures in
// a row, it sleeps paneCaptureRetryAfterDeath, re-resolves the current
// pane target from the launcher (in case an office reseed rebuilt tmux
// and the old pane id is stale), and tries again. The loop only exits
// when the context is canceled.
func (l *Launcher) paneCaptureLoop(ctx context.Context, slug, paneTarget string) {
	stream := l.broker.AgentStream(slug)
	if stream == nil {
		return
	}

	ticker := time.NewTicker(paneCapturePollInterval)
	defer ticker.Stop()

	var prevLines []string
	failures := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		snapshot, err := capturePane(ctx, paneTarget)
		if err != nil {
			failures++
			if failures >= paneCaptureMaxFailures {
				fmt.Fprintf(os.Stderr,
					"  Agents:  pane capture for %s (%s) paused after %d failures; will retry in %s: %v\n",
					slug, paneTarget, failures, paneCaptureRetryAfterDeath, err,
				)
				// Sleep, then re-resolve the pane target. Office reseeds and
				// overflow-pane recreation change pane ids — the old target
				// stays dead forever but the agent may have a live pane
				// under a new address. Re-resolve before retrying.
				select {
				case <-ctx.Done():
					return
				case <-time.After(paneCaptureRetryAfterDeath):
				}
				if newTarget, ok := l.resolvePaneTargetForSlug(slug); ok && newTarget != "" {
					paneTarget = newTarget
				}
				// Clear prev-line cache so the re-surfaced pane pushes a
				// fresh snapshot rather than diffing against stale state.
				prevLines = nil
				failures = 0
				continue
			}
			// Back off briefly on errors instead of spinning on the 1s tick.
			select {
			case <-ctx.Done():
				return
			case <-time.After(paneCaptureBackoffOnError):
			}
			continue
		}
		failures = 0

		nextLines := strings.Split(snapshot, "\n")
		for i, line := range nextLines {
			nextLines[i] = stripANSI(line)
		}

		newLines := diffPaneLines(prevLines, nextLines)
		if len(newLines) == 0 {
			prevLines = nextLines
			continue
		}

		for _, line := range newLines {
			if len(line) > paneCaptureMaxDiffBytes {
				line = line[:paneCaptureMaxDiffBytes] + paneCaptureTruncateMarker
			}
			stream.Push(line)
		}
		prevLines = nextLines
	}
}

// capturePane shells out to tmux capture-pane with -J (join wrapped lines)
// and -p (stdout). It returns the raw captured text on success.
func capturePane(ctx context.Context, paneTarget string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux",
		"-L", tmuxSocketName,
		"capture-pane",
		"-p",
		"-J",
		"-t", paneTarget,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane %s: %w", paneTarget, err)
	}
	return string(out), nil
}
