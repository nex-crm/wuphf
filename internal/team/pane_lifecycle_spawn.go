package team

// pane_lifecycle_spawn.go owns the high-level pane orchestration
// methods on paneLifecycle (PLAN.md §C24): the multi-step "spawn the
// team" / "detect dead spawns" / "prime the panes" / "respawn-after-
// reseed" flows that compose the single-call tmux primitives in
// pane_lifecycle.go. The split keeps pane_lifecycle.go focused on
// tmux verbs (one-call-per-method) while this file owns the
// orchestrations that wire those verbs into roster-aware spawn flows
// driven by paneLifecycleDeps.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SpawnVisibleAgents creates the visible agent panes (PLAN.md §C5e).
// One-on-one mode: a single split, channel-pane title becomes "📢
// direct". Multi-agent mode: first agent split-h-65, additional agents
// split vertically off pane 1, then main-vertical layout normalizes
// the column. Returns the slugs of agents whose first split succeeded;
// later splits may individually fail (recorded via recordFailure
// callback so the targeter routes those agents headless).
func (p *paneLifecycle) SpawnVisibleAgents() ([]string, error) {
	channelPane := p.sessionName + ":team.0"
	if p.deps.isOneOnOne != nil && p.deps.isOneOnOne() {
		slug := p.deps.oneOnOneAgent()
		firstCmd, err := p.deps.claudeCommand(slug, p.deps.buildPrompt(slug))
		if err != nil {
			return nil, err
		}
		out, err := p.SplitFirstAgent(p.deps.cwd, firstCmd)
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail == "" {
				return nil, fmt.Errorf("spawn one-on-one agent: %w", err)
			}
			return nil, fmt.Errorf("spawn one-on-one agent: %w (tmux: %s)", err, detail)
		}
		p.ApplyMainVerticalLayout()
		p.SetPaneTitle(channelPane, "📢 direct")
		p.SetPaneTitle(fmt.Sprintf("%s:team.1", p.sessionName),
			fmt.Sprintf("🤖 %s (@%s)", p.deps.agentName(slug), slug),
		)
		p.SelectTeamWindow()
		p.FocusPane(channelPane)
		return []string{slug}, nil
	}

	// Layout: channel (left 35%) | agents in 2-column grid (right 65%)
	//
	// ┌─ channel ──┬─ CEO ───┬─ PM ────┐
	// │            │         │         │
	// │            ├─ FE ────┼─ BE ────┤
	// │            │         │         │
	// └────────────┴─────────┴─────────┘
	visible := p.deps.visibleOfficeMembers()
	if len(visible) == 0 {
		return nil, nil
	}
	firstCmd, err := p.deps.claudeCommand(visible[0].Slug, p.deps.buildPrompt(visible[0].Slug))
	if err != nil {
		return nil, err
	}
	out, err := p.SplitFirstAgent(p.deps.cwd, firstCmd)
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return nil, fmt.Errorf("spawn first agent: %w", err)
		}
		return nil, fmt.Errorf("spawn first agent: %w (tmux: %s)", err, detail)
	}

	// Remaining agents: split from agent area, then use "tiled" layout. First
	// agent (pane 1) is mandatory — a failure there aborts the whole launch.
	// Subsequent splits can fail individually (e.g. terminal too small to
	// accommodate another tile); record the failure and fall those agents
	// back to headless dispatch so the capture loop doesn't hunt ghost panes.
	for i := 1; i < len(visible); i++ {
		agentCmd, err := p.deps.claudeCommand(visible[i].Slug, p.deps.buildPrompt(visible[i].Slug))
		if err != nil {
			p.deps.recordFailure(visible[i].Slug, fmt.Sprintf("claudeCommand: %v", err))
			continue
		}
		out, err := p.SplitAdditionalAgent(p.deps.cwd, agentCmd)
		if err != nil {
			detail := strings.TrimSpace(string(out))
			reason := err.Error()
			if detail != "" {
				reason = fmt.Sprintf("%s (tmux: %s)", reason, detail)
			}
			fmt.Fprintf(os.Stderr,
				"  Agents:  visible pane for %s failed to spawn; falling back to headless (%s)\n",
				visible[i].Slug, reason,
			)
			p.deps.recordFailure(visible[i].Slug, reason)
		}
	}

	p.ApplyMainVerticalLayout()

	var visibleSlugs []string
	p.SetPaneTitle(channelPane, "📢 channel")
	for i, a := range visible {
		paneIdx := i + 1 // pane 0 is channel
		name := p.deps.agentName(a.Slug)
		p.SetPaneTitle(
			fmt.Sprintf("%s:team.%d", p.sessionName, paneIdx),
			fmt.Sprintf("🤖 %s (@%s)", name, a.Slug),
		)
		visibleSlugs = append(visibleSlugs, a.Slug)
	}
	p.SelectTeamWindow()
	p.FocusPane(channelPane)
	return visibleSlugs, nil
}

// SpawnOverflowAgents creates a hidden tmux window per overflow agent
// (PLAN.md §C5e). Overflow agents are members beyond the visible team
// grid; they still need a live claude pane in pane-backed mode.
// Codex/Opencode-bound members are skipped because they use the
// headless one-shot pipeline. Failures are recorded but don't abort —
// each agent falls back to headless individually.
func (p *paneLifecycle) SpawnOverflowAgents() {
	for _, member := range p.deps.overflowOfficeMembers() {
		if p.deps.memberUsesHeadlessOneShotRuntime(member.Slug) {
			continue
		}
		agentCmd, err := p.deps.claudeCommand(member.Slug, p.deps.buildPrompt(member.Slug))
		if err != nil {
			fmt.Fprintf(os.Stderr, "spawn overflow agent %s: %v\n", member.Slug, err)
			p.deps.recordFailure(member.Slug, fmt.Sprintf("claudeCommand: %v", err))
			continue
		}
		windowName := overflowWindowName(member.Slug)
		out, err := p.NewOverflowWindow(windowName, p.deps.cwd, agentCmd)
		if err != nil {
			detail := strings.TrimSpace(string(out))
			reason := err.Error()
			if detail != "" {
				reason = fmt.Sprintf("%s (tmux: %s)", reason, detail)
			}
			fmt.Fprintf(os.Stderr,
				"  Agents:  overflow pane for %s failed to spawn; falling back to headless for this agent (%s)\n",
				member.Slug, reason,
			)
			p.deps.recordFailure(member.Slug, reason)
		}
	}
}

// DetectDeadPanesAfterSpawn waits 1.5s for fresh panes to either settle
// into claude or die on launch (PLAN.md §C5e). Dead panes are recorded
// via recordFailure and surface to the user via stderr + a #general
// system message. The fixed sleep is the same one the original
// Launcher method had; clock injection is deferred to a follow-up.
func (p *paneLifecycle) DetectDeadPanesAfterSpawn(members []officeMember) {
	if p == nil || p.sessionName == "" {
		return
	}
	<-p.clock.After(1500 * time.Millisecond)
	targets := p.deps.agentPaneTargets()
	for _, m := range members {
		target, ok := targets[m.Slug]
		if !ok || target.PaneTarget == "" {
			continue
		}
		dead, err := p.IsPaneDead(target.PaneTarget)
		if err != nil || !dead {
			continue
		}
		history, _ := p.CapturePaneHistory(target.PaneTarget, 200)
		snippet := strings.TrimSpace(history)
		if len(snippet) > 400 {
			snippet = snippet[:400] + "..."
		}
		fmt.Fprintf(os.Stderr,
			"  Agents:  pane for %s (%s) died on launch; falling back to headless. Last output: %q\n",
			m.Slug, target.PaneTarget, snippet,
		)
		p.deps.recordFailure(m.Slug, "pane died on launch; last output: "+snippet)
		if p.deps.postSystemMessage != nil {
			p.deps.postSystemMessage("general",
				fmt.Sprintf("Agent @%s did not start cleanly; running in headless fallback. Check the launcher log for details.", m.Slug),
				"runtime",
			)
		}
	}
}

// TrySpawnWebAgentPanes attempts to spawn the full pane-backed
// fallback session (PLAN.md §C5e). On success flips the
// paneBackedFlag *bool true so the targeter routes through pane
// dispatch. On failure, calls reportPaneFallback which prints the
// stderr banner and posts the broker-side advisory.
func (p *paneLifecycle) TrySpawnWebAgentPanes() {
	if p.deps.postSystemMessage == nil {
		// Production wires postSystemMessage from a non-nil broker;
		// nil here matches the legacy "broker == nil" early return.
		return
	}
	if p.deps.usesPaneRuntime != nil && !p.deps.usesPaneRuntime() {
		return
	}
	if err := p.TmuxAvailable(); err != nil {
		p.ReportPaneFallback(false, "tmux not found on PATH", err)
		return
	}
	p.KillSession()
	placeholderCmd := "sh -c 'while :; do sleep 3600; done'"
	if err := p.NewSession(p.deps.cwd, placeholderCmd); err != nil {
		p.ReportPaneFallback(true, "tmux new-session failed", err)
		return
	}
	p.SetSessionOption("mouse", "off")
	p.SetSessionOption("status", "off")
	p.SetTeamWindowOption("remain-on-exit", "on")

	if _, err := p.SpawnVisibleAgents(); err != nil {
		p.KillSession()
		p.ReportPaneFallback(true, "spawn visible agents failed", err)
		return
	}
	p.SpawnOverflowAgents()

	if p.deps.paneBackedFlag != nil {
		*p.deps.paneBackedFlag = true
	}
	go p.DetectDeadPanesAfterSpawn(append(p.deps.visibleOfficeMembers(), p.deps.overflowOfficeMembers()...))
	fmt.Printf("  Agents:  interactive Claude panes in tmux session %q (pane-backed fallback active)\n", p.sessionName)
}

// PrimeVisibleAgents waits for visible agent panes to clear claude's
// startup interactivity (folder-trust, security-guide, "press Enter")
// so dispatch can type into the pane. Returns once all panes report
// ready or after 3 attempts. Replay of the latest broker message
// (the "first message lost behind startup" recovery) stays on
// Launcher.primeVisibleAgents because it depends on broker
// state and the headless-resume path.
func (p *paneLifecycle) PrimeVisibleAgents() {
	<-p.clock.After(1 * time.Second)

	targets := p.deps.agentPaneTargets()
	if len(targets) == 0 {
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		allReady := true
		for _, target := range targets {
			content, err := p.CapturePaneTargetContent(target.PaneTarget)
			if err != nil {
				allReady = false
				continue
			}
			if shouldPrimeClaudePane(content) {
				p.SendEnter(target.PaneTarget)
				allReady = false
			}
		}
		if allReady {
			break
		}
		<-p.clock.After(1 * time.Second)
	}
}

// ReportPaneFallback prints the stderr banner and posts the
// broker-side fallback advisory (PLAN.md §C5e). Pure thin wrapper
// around paneFallbackMessages + the postSystemMessage callback so
// trySpawnWebAgentPanes can surface failures without re-reaching
// into Launcher state.
func (p *paneLifecycle) ReportPaneFallback(tmuxInstalled bool, summary string, err error) {
	detail := summary
	if err != nil {
		detail = fmt.Sprintf("%s: %v", summary, err)
	}
	stderrMsg, brokerMsg := paneFallbackMessages(tmuxInstalled, detail)
	fmt.Fprint(os.Stderr, stderrMsg)
	if p.deps.postSystemMessage != nil {
		p.deps.postSystemMessage("general", brokerMsg, "runtime")
	}
}

// CaptureDeadChannelPane writes a timestamped snapshot of the channel
// pane (pane 0) plus its display-message status to
// channelPaneSnapshotPath. Used by watchChannelPaneLoop the first time
// a dead pane is observed, so post-mortem inspection has the pane's
// last known content even after the respawn. Capture failures degrade
// to a "<capture failed: …>" placeholder so the snapshot file always
// exists with the status line.
func (p *paneLifecycle) CaptureDeadChannelPane(status string) error {
	content, err := p.CapturePaneContent(0)
	if err != nil {
		content = fmt.Sprintf("<capture failed: %v>", err)
	}
	path := channelPaneSnapshotPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "\n[%s] status=%s\n%s\n", time.Now().Format(time.RFC3339), status, content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
