package team

// pane_lifecycle.go owns the pane-lifecycle helpers extracted from
// launcher.go (PLAN.md §C5). The first wave (C5a) was the pure helpers
// (parseAgentPaneIndices, shouldPrimeClaudePane, etc.). The second wave
// (C5b) adds the paneLifecycle type and migrates the read-only tmux
// methods (HasLiveSession, ListTeamPanes, ChannelPaneStatus, capture*)
// onto it through the tmuxRunner seam (tmux_runner.go). Spawn/clear/
// respawn methods stay on Launcher pending follow-up PRs that migrate
// them onto the same type.

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// paneLifecycleDeps wires the Launcher state and methods that the spawn
// orchestration methods (SpawnVisibleAgents/SpawnOverflowAgents/
// DetectDeadPanesAfterSpawn/TrySpawnWebAgentPanes/PrimeVisibleAgents)
// need to consult. All fields are optional — read-only operations that
// don't touch a particular dependency leave it nil. The deps struct is
// captured at paneLifecycle construction time so callers (tests + the
// Launcher) can supply only what they need.
//
// Why a struct instead of a wide constructor: the spawn methods need
// 8+ callbacks each, and a flat constructor would be unreadable at
// call sites. PLAN.md trap §1 (failedPaneSlugs) is addressed by
// recordFailure being a callback that writes back into the Launcher's
// shared map — the targeter still reads from the same map, so its
// view is unchanged.
type paneLifecycleDeps struct {
	// cwd is the working directory tmux uses when spawning agent
	// processes. Empty in nil-safe / read-only paths.
	cwd string
	// isOneOnOne / oneOnOneAgent gate the one-on-one spawn shape.
	isOneOnOne    func() bool
	oneOnOneAgent func() string
	// usesPaneRuntime gates TrySpawnWebAgentPanes. Headless runtimes
	// short-circuit to nil-op without consulting tmux.
	usesPaneRuntime func() bool
	// visibleOfficeMembers / overflowOfficeMembers / agentPaneTargets
	// come from the targeter (PLAN.md §C2). The targeter already owns
	// the pane-eligible members + slug→target map; paneLifecycle
	// reads them through these closures.
	visibleOfficeMembers  func() []officeMember
	overflowOfficeMembers func() []officeMember
	agentPaneTargets      func() map[string]notificationTarget
	// memberUsesHeadlessOneShotRuntime distinguishes Codex/Opencode
	// agents (no claude pane) from claude agents inside the spawn loop.
	memberUsesHeadlessOneShotRuntime func(slug string) bool
	// claudeCommand / buildPrompt / agentName are Launcher methods
	// that the spawn loop needs per agent. Wired as callbacks so the
	// promptBuilder / officeTargeter ownership stays on Launcher.
	claudeCommand func(slug, prompt string) (string, error)
	buildPrompt   func(slug string) string
	agentName     func(slug string) string
	// recordFailure writes into Launcher.failedPaneSlugs (PLAN.md
	// trap §1: shared map between targeter and paneLifecycle). Keeping
	// the write as a callback preserves the targeter's existing read
	// path through the same map.
	recordFailure func(slug, reason string)
	// postSystemMessage forwards to broker.PostSystemMessage when the
	// broker is set. Nil = no broker available; spawn methods skip
	// the broker-side notification.
	postSystemMessage func(channel, body, kind string)
	// paneBackedFlag is a back-pointer to Launcher.paneBackedAgents.
	// TrySpawnWebAgentPanes flips it true on success; the targeter
	// reads it via its own paneBackedFlag *bool to decide routing.
	paneBackedFlag *bool
}

// paneLifecycle owns the tmux pane lifecycle (PLAN.md §C5). The runner
// field is the test seam for tmux invocations; production gets
// realTmuxRunner via newTmuxRunner. The clock field is the test seam
// for time.Sleep — production gets realClock{}, tests inject a
// manualClock via withClock so the sleep-heavy spawn orchestration
// methods (DetectDeadPanesAfterSpawn, PrimeVisibleAgents) can be
// driven without real wall-clock waits.
type paneLifecycle struct {
	runner      tmuxRunner
	clock       clock
	sessionName string
	deps        paneLifecycleDeps
}

// newPaneLifecycle constructs a paneLifecycle bound to a specific tmux
// session name. The runner is resolved through the package-global
// override seam at construction time, so a test that calls
// setTmuxRunnerForTest before constructing the launcher gets its fake
// runner installed transparently. deps is empty (nil callbacks);
// callers that need spawn orchestration use newPaneLifecycleWithDeps.
// Production clock is realClock; tests override via withClock.
func newPaneLifecycle(sessionName string) *paneLifecycle {
	return &paneLifecycle{
		runner:      newTmuxRunner(),
		clock:       realClock{},
		sessionName: sessionName,
	}
}

// newPaneLifecycleWithDeps is the spawn-capable constructor used by the
// Launcher. The runner still routes through the override seam, and
// the clock defaults to realClock so production timing matches the
// pre-C5f behaviour exactly.
func newPaneLifecycleWithDeps(sessionName string, deps paneLifecycleDeps) *paneLifecycle {
	return &paneLifecycle{
		runner:      newTmuxRunner(),
		clock:       realClock{},
		sessionName: sessionName,
		deps:        deps,
	}
}

// withClock swaps the clock used by sleep-heavy orchestration methods.
// Returns the same paneLifecycle so the call can be chained off a
// constructor in test setup. Production code never calls this — it's
// only useful from _test.go files that build a manualClock.
func (p *paneLifecycle) withClock(c clock) *paneLifecycle {
	p.clock = c
	return p
}

// HasLiveSession returns true when a wuphf-team tmux session is running.
// Mirrors the historical free-function HasLiveTmuxSession but routes
// through the runner so tests can drive it without a real tmux server.
func (p *paneLifecycle) HasLiveSession() bool {
	return p.runner.Run("has-session", "-t", p.sessionName) == nil
}

// CapturePaneTargetContent captures the visible content of an arbitrary
// tmux pane target (e.g. "wuphf-team:team.0") with capture-pane's -p -J
// flags. Returns the raw stdout (no trim) so callers can render the
// captured pane verbatim into snapshot logs.
func (p *paneLifecycle) CapturePaneTargetContent(target string) (string, error) {
	out, err := p.runner.Combined("capture-pane", "-p", "-J", "-t", target)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CapturePaneContent captures the visible content of pane <paneIdx> in
// the "team" window. Convenience wrapper around CapturePaneTargetContent.
func (p *paneLifecycle) CapturePaneContent(paneIdx int) (string, error) {
	target := fmt.Sprintf("%s:team.%d", p.sessionName, paneIdx)
	return p.CapturePaneTargetContent(target)
}

// ListTeamPanes returns the agent-pane indices in the "team" window.
// Pane 0 (the channel observer) and any pane whose title contains
// "channel" are filtered out by parseAgentPaneIndices. When the session
// isn't up, returns (nil, nil) — callers treat that as "nothing to
// clean up" rather than an error.
func (p *paneLifecycle) ListTeamPanes() ([]int, error) {
	out, err := p.runner.Combined("list-panes",
		"-t", p.sessionName+":team",
		"-F", "#{pane_index} #{pane_title}",
	)
	if err != nil {
		if isMissingTmuxSession(string(out)) {
			return nil, nil
		}
		return nil, fmt.Errorf("list panes: %w", err)
	}
	return parseAgentPaneIndices(string(out)), nil
}

// ChannelPaneStatus returns the tmux display-message status for pane 0
// in the "team" window: "{pane_dead} {pane_dead_status}
// {pane_current_command}". Used by the channel-pane watcher to decide
// whether to respawn. tmux failures surface as the trimmed stderr text
// in the returned error — callers match that text via isNoSessionError.
func (p *paneLifecycle) ChannelPaneStatus() (string, error) {
	out, err := p.runner.Combined("display-message",
		"-p",
		"-t", p.sessionName+":team.0",
		"#{pane_dead} #{pane_dead_status} #{pane_current_command}",
	)
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ClearAgentPanes kills every agent pane in the "team" window
// (preserving pane 0, the channel observer). The list is sorted in
// reverse so kill-pane on a higher index doesn't reshuffle the lower
// ones we still need to address. Errors from individual kill-pane calls
// are intentionally swallowed — the caller (reconfigureVisibleAgents)
// follows up with spawnVisibleAgents which is where actual failures
// surface; here, the worst case is a pane that won't die which becomes
// visible at next list-panes anyway.
func (p *paneLifecycle) ClearAgentPanes() error {
	panes, err := p.ListTeamPanes()
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.IntSlice(panes)))
	for _, idx := range panes {
		if idx == 0 {
			continue
		}
		target := fmt.Sprintf("%s:team.%d", p.sessionName, idx)
		_ = p.runner.Run("kill-pane", "-t", target)
	}
	return nil
}

// ClearOverflowAgentWindows enumerates the tmux windows in the session
// and kills any whose name starts with "agent-" — the prefix used by
// spawnOverflowAgents for agents that don't fit in the visible "team"
// grid. A list-windows failure is treated as "nothing to clean up"
// (tmux is probably down). Like ClearAgentPanes, individual kill-window
// errors are swallowed — overflow windows are best-effort housekeeping.
func (p *paneLifecycle) ClearOverflowAgentWindows() {
	out, err := p.runner.Combined("list-windows",
		"-t", p.sessionName,
		"-F", "#{window_name}",
	)
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, "agent-") {
			continue
		}
		_ = p.runner.Run("kill-window",
			"-t", fmt.Sprintf("%s:%s", p.sessionName, name),
		)
	}
}

// KillSession terminates the entire wuphf-team tmux session. Used by
// reconfigureVisibleAgents when the runtime no longer needs panes (the
// user switched to headless mid-session). Errors are intentionally
// dropped — if the session is already gone, kill-session returns a "no
// such session" error that's the desired post-condition.
func (p *paneLifecycle) KillSession() {
	_ = p.runner.Run("kill-session", "-t", p.sessionName)
}

// RespawnAgentPane runs `respawn-pane -k` against pane <idx> in the
// "team" window, replacing the running process with cmd executed in
// cwd. Returns the combined stdout/stderr so callers can surface the
// tmux error text in their own error messages. Used by
// reconfigureVisibleAgents to restart agent processes in place,
// preserving pane sizes and positions.
func (p *paneLifecycle) RespawnAgentPane(idx int, cwd, cmd string) ([]byte, error) {
	target := fmt.Sprintf("%s:team.%d", p.sessionName, idx)
	return p.runner.Combined("respawn-pane", "-k",
		"-t", target,
		"-c", cwd,
		cmd,
	)
}

// RespawnChannelPane respawns pane 0 (the channel observer) with the
// given channelCmd executed in cwd, then re-applies the channel pane
// title. Used by watchChannelPaneLoop when the channel pane has been
// dead for at least channelRespawnDelay. Errors are swallowed —
// watchChannelPaneLoop runs on a periodic tick and will retry on the
// next iteration if the respawn didn't take.
func (p *paneLifecycle) RespawnChannelPane(channelCmd, cwd string) {
	target := p.sessionName + ":team.0"
	_ = p.runner.Run("respawn-pane", "-k",
		"-t", target,
		"-c", cwd,
		channelCmd,
	)
	_ = p.runner.Run("select-pane",
		"-t", target,
		"-T", "📢 channel",
	)
}

// SplitFirstAgent runs `tmux split-window -h -t session:team -p 65 -c
// cwd cmd` — the layout primitive that creates pane 1 (the first
// agent) to the right of the channel pane (pane 0). Combined output is
// returned so callers can surface tmux's stderr in their own error
// messages.
func (p *paneLifecycle) SplitFirstAgent(cwd, cmd string) ([]byte, error) {
	return p.runner.Combined("split-window", "-h",
		"-t", p.sessionName+":team",
		"-p", "65",
		"-c", cwd,
		cmd,
	)
}

// SplitAdditionalAgent runs `tmux split-window -t session:team.1 -c cwd
// cmd` — adds a pane that the subsequent main-vertical layout will
// arrange next to the existing agent panes. Combined output is returned
// so callers can surface tmux's error text.
func (p *paneLifecycle) SplitAdditionalAgent(cwd, cmd string) ([]byte, error) {
	return p.runner.Combined("split-window",
		"-t", p.sessionName+":team.1",
		"-c", cwd,
		cmd,
	)
}

// NewOverflowWindow runs `tmux new-window -d -t session -n name -c cwd
// cmd` — creates a hidden ("-d") window for an overflow agent that
// doesn't fit in the visible team grid. Combined output is returned so
// callers can surface tmux's error text in failure messages.
func (p *paneLifecycle) NewOverflowWindow(windowName, cwd, cmd string) ([]byte, error) {
	return p.runner.Combined("new-window", "-d",
		"-t", p.sessionName,
		"-n", windowName,
		"-c", cwd,
		cmd,
	)
}

// NewSession runs `tmux new-session -d -s session -n team -c cwd cmd`
// to create the detached session that hosts the team window. Returns
// the exec error directly because the calling code (trySpawnWebAgentPanes)
// converts a non-nil result into a "tmux new-session failed" fallback.
func (p *paneLifecycle) NewSession(cwd, placeholderCmd string) error {
	return p.runner.Run("new-session", "-d",
		"-s", p.sessionName,
		"-n", "team",
		"-c", cwd,
		placeholderCmd,
	)
}

// SetSessionOption applies a tmux session-level option (e.g.
// `set-option -t session mouse off`). Errors are intentionally
// dropped — these are cosmetic / interactivity tweaks that don't gate
// the launch.
func (p *paneLifecycle) SetSessionOption(name, value string) {
	_ = p.runner.Run("set-option",
		"-t", p.sessionName,
		name, value,
	)
}

// SetTeamWindowOption applies a tmux window-level option scoped to the
// "team" window (e.g. `set-window-option -t session:team
// remain-on-exit on`). Errors dropped — same rationale as
// SetSessionOption.
func (p *paneLifecycle) SetTeamWindowOption(name, value string) {
	_ = p.runner.Run("set-window-option",
		"-t", p.sessionName+":team",
		name, value,
	)
}

// ApplyMainVerticalLayout selects the `main-vertical` tmux layout for
// the team window. Re-applied after each pane add so the channel
// (pane 0) stays on the left and agent panes tile vertically on the
// right. Errors dropped — layout failures aren't recoverable here, and
// retrying on the next pane add usually fixes them.
func (p *paneLifecycle) ApplyMainVerticalLayout() {
	_ = p.runner.Run("select-layout",
		"-t", p.sessionName+":team",
		"main-vertical",
	)
}

// SetPaneTitle re-titles a pane via `select-pane -t target -T title`.
// Errors dropped — titles are cosmetic and a transient tmux failure
// shouldn't fail the launch.
func (p *paneLifecycle) SetPaneTitle(target, title string) {
	_ = p.runner.Run("select-pane",
		"-t", target,
		"-T", title,
	)
}

// SelectTeamWindow runs `tmux select-window -t session:team`. Used to
// raise the team window after pane adds so the user lands in the
// expected place when attaching. Errors dropped.
func (p *paneLifecycle) SelectTeamWindow() {
	_ = p.runner.Run("select-window",
		"-t", p.sessionName+":team",
	)
}

// FocusPane runs `tmux select-pane -t target` (without -T). Used to
// focus the channel pane after the spawn flow so the user lands there
// instead of in an agent pane. Errors dropped.
func (p *paneLifecycle) FocusPane(target string) {
	_ = p.runner.Run("select-pane",
		"-t", target,
	)
}

// IsPaneDead reads `#{pane_dead}` for target via display-message.
// Returns (true, nil) when tmux reports "1", (false, nil) when "0", and
// the parse-or-exec error otherwise. Used by detectDeadPanesAfterSpawn
// to decide whether a freshly-spawned agent pane crashed at startup.
func (p *paneLifecycle) IsPaneDead(target string) (bool, error) {
	out, err := p.runner.Combined("display-message",
		"-t", target,
		"-p", "#{pane_dead}",
	)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// CapturePaneHistory captures the last `lines` rows of pane scrollback
// at target via `capture-pane -t target -p -J -S -<lines>`. Used by
// detectDeadPanesAfterSpawn to surface the last output of a dead pane
// in the headless-fallback warning so the user has a hint about why it
// died.
func (p *paneLifecycle) CapturePaneHistory(target string, lines int) (string, error) {
	out, err := p.runner.Combined("capture-pane",
		"-t", target,
		"-p", "-J", "-S", fmt.Sprintf("-%d", lines),
	)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SendEnter sends a single Enter keypress to the target pane via
// `send-keys -t target Enter`. Used by primeVisibleAgents to clear
// claude's startup confirmation prompts so dispatch can type into the
// pane.
func (p *paneLifecycle) SendEnter(target string) {
	_ = p.runner.Run("send-keys",
		"-t", target,
		"Enter",
	)
}

// TmuxAvailable returns nil when the tmux binary is on PATH, or the
// LookPath error when not. Wrapper around exec.LookPath so the runner
// seam is not bypassed for the "is tmux installed?" check (although
// LookPath itself does not go through the runner — there's nothing
// useful to fake about a binary lookup).
func (p *paneLifecycle) TmuxAvailable() error {
	_, err := exec.LookPath("tmux")
	return err
}

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

// HasLiveTmuxSession returns true if a wuphf-team tmux session is
// running. Routes through paneLifecycle (PLAN.md §C5b) so tests can
// drive it via setTmuxRunnerForTest without a real tmux server.
func HasLiveTmuxSession() bool {
	return newPaneLifecycle(SessionName).HasLiveSession()
}
