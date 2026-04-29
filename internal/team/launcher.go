// Package team implements the WUPHF team launcher that starts a multi-agent
// collaborative team using tmux + Claude Code + the WUPHF office broker.
//
// Architecture:
//   - Each agent is a real Claude Code session in a tmux window
//   - the office broker provides the shared channel (all agents see all messages)
//   - Nex is an optional context layer, not a requirement
//   - CEO has final decision authority; agents participate when relevant
//   - Go TUI is the channel "observer" — displays the conversation
package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/runtimebin"
	"github.com/nex-crm/wuphf/internal/setup"
)

const (
	defaultNotificationPollInterval = 15 * time.Minute
	channelRespawnDelay             = 8 * time.Second
	ceoHeadStartDelay               = 250 * time.Millisecond
	blankSlateLaunchSlug            = "__blank_slate__"

	// baseSessionName and baseTmuxSocketName are the default, un-suffixed
	// identifiers used when the broker runs on the default port (prod).
	// The exported SessionName and tmuxSocketName include a per-port suffix
	// when a non-default broker port is configured, so concurrent prod, dev,
	// and worktree launches cannot collide on a shared tmux socket or
	// session name. See nameWithPortSuffix for the suffixing rule.
	baseSessionName    = "wuphf-team"
	baseTmuxSocketName = "wuphf"
)

// SessionName and tmuxSocketName are derived at package init from the
// broker port resolved via brokeraddr. On the default port they keep their
// historical values ("wuphf-team", "wuphf"); on any non-default port they
// gain a "-<port>" suffix. This isolation is what prevents the
// "spawn first agent: exit status 1" race seen when two WUPHF instances
// tried to share a single tmux socket + session name.
var (
	SessionName    = nameWithPortSuffix(baseSessionName)
	tmuxSocketName = nameWithPortSuffix(baseTmuxSocketName)
)

func nameWithPortSuffix(base string) string {
	return nameWithPortSuffixForPort(base, brokeraddr.ResolvePort())
}

func nameWithPortSuffixForPort(base string, port int) string {
	if port <= 0 || port == brokeraddr.DefaultPort {
		return base
	}
	return fmt.Sprintf("%s-%d", base, port)
}

// Launcher sets up and manages the multi-agent team.
type Launcher struct {
	packSlug           string
	pack               *agent.PackDefinition
	operationBlueprint *operations.Blueprint
	blankSlateLaunch   bool
	sessionName        string
	cwd                string
	broker             *Broker
	mcpConfig          string
	unsafe             bool
	opusCEO            bool
	focusMode          bool
	sessionMode        string
	oneOnOne           string
	provider           string

	headlessMu           sync.Mutex
	headlessCtx          context.Context
	headlessCancel       context.CancelFunc
	headlessWorkers      map[string]bool
	headlessActive       map[string]*headlessCodexActiveTurn
	headlessQueues       map[string][]headlessCodexTurn
	headlessDeferredLead *headlessCodexTurn
	// headlessStopCh is closed by stopHeadlessWorkers to tell every active
	// runHeadlessCodexQueue goroutine to exit at its next outer-loop tick.
	// Lazily allocated by spawnHeadlessWorker / stopHeadlessWorkers under
	// headlessMu so the zero-value Launcher used in tests doesn't need an
	// explicit init. headlessWorkerWg tracks live worker goroutines so the
	// stop helper can drain them deterministically — closing the channel is
	// a request, the WaitGroup is the proof everyone observed it.
	headlessStopCh   chan struct{}
	headlessWorkerWg sync.WaitGroup
	webMode          bool
	paneBackedAgents bool // web mode may spawn per-agent tmux panes; true when panes are live
	noOpen           bool

	// failedPaneSlugs records agents whose tmux pane/window creation failed.
	// agentPaneTargets() omits them so the pane-capture loops don't spin on
	// missing targets (which produces "stopped after 5 failures" spam). These
	// agents fall back to the headless dispatch path automatically.
	failedPaneSlugs map[string]string

	notifyMu            sync.Mutex
	notifyLastDelivered map[string]time.Time

	// paneDispatchMu serializes access to paneDispatchQueues /
	// paneDispatchWorkers. Each pane-backed agent has its own queue so
	// rapid notifications no longer race with claude's in-progress turn —
	// one worker per slug drains its queue with a minimum spacing between
	// `/clear` sends so claude has time to finalise each reply.
	paneDispatchMu      sync.Mutex
	paneDispatchQueues  map[string][]paneDispatchTurn
	paneDispatchWorkers map[string]bool
	// paneDispatchLastSentAt records when each slug most recently had a
	// `/clear` + type cycle dispatched. The coalesce logic in
	// queuePaneNotification uses this to decide whether to merge a new
	// notification with a pending one or to start a fresh dispatch.
	paneDispatchLastSentAt map[string]time.Time

	// targets owns the office-membership-shape and routing-decision logic
	// (PLAN.md §C2). Lazily constructed via targeter() so tests that build
	// &Launcher{} directly stay nil-safe. The launcher field stays the
	// authoritative source for sessionName / pack / failedPaneSlugs /
	// paneBackedAgents — the targeter holds pointers/callbacks back into
	// the launcher rather than copies.
	targets     *officeTargeter
	targetsOnce sync.Once

	// notify owns notification-context and work-packet construction
	// (PLAN.md §C3). Lazily constructed via notifyCtx() and shares state
	// with the launcher via callbacks (broker reads, headless queue peek).
	notify     *notificationContextBuilder
	notifyOnce sync.Once

	// schedulerWorker owns the watchdog scheduler goroutine
	// (PLAN.md §C4). Lazily constructed via scheduler(); Launch starts the
	// goroutine via watchdogSchedulerLoop, Kill drains it via Stop before
	// tearing down the broker. clock is realClock in production.
	schedulerWorker     *watchdogScheduler
	schedulerWorkerOnce sync.Once
}

// paneDispatchTurn is one queued notification to type into a tmux pane.
// Held in the per-slug queue, consumed by runPaneDispatchQueue.
type paneDispatchTurn struct {
	PaneTarget   string
	Notification string
	EnqueuedAt   time.Time
}

// SetUnsafe enables unrestricted permissions for all agents (CLI-only flag).
func (l *Launcher) SetUnsafe(v bool) { l.unsafe = v }

// SetOpusCEO upgrades the CEO agent from Sonnet to Opus.
func (l *Launcher) SetOpusCEO(v bool) { l.opusCEO = v }

// SetFocusMode enables CEO-routed delegation mode.
func (l *Launcher) SetFocusMode(v bool) { l.focusMode = v }

// SetNoOpen suppresses automatic browser launch on startup.
func (l *Launcher) SetNoOpen(v bool) { l.noOpen = v }

func (l *Launcher) SetOneOnOne(slug string) {
	l.sessionMode = SessionModeOneOnOne
	l.oneOnOne = NormalizeOneOnOneAgent(slug)
}

func isBlankSlateLaunchSlug(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "from-scratch", "blank-slate", blankSlateLaunchSlug:
		return true
	default:
		return false
	}
}

// NewLauncher creates a launcher for the given operation blueprint or legacy pack.
func NewLauncher(packSlug string) (*Launcher, error) {
	cfg, _ := config.Load()
	explicitPack := packSlug != "" // true when user passed --pack explicitly
	blankSlateLaunch := isBlankSlateLaunchSlug(packSlug) || strings.TrimSpace(os.Getenv("WUPHF_START_FROM_SCRATCH")) == "1"
	if isBlankSlateLaunchSlug(packSlug) {
		packSlug = ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	repoRoot := resolveRepoRoot(cwd)
	if packSlug == "" && !blankSlateLaunch {
		packSlug = cfg.ActiveBlueprint()
		if packSlug == "" {
			if manifest, err := company.LoadManifest(); err == nil {
				if refs := manifest.BlueprintRefsByKind("operation"); len(refs) > 0 {
					packSlug = refs[0].ID
				}
			}
		}
	}

	operationTemplateExists := false
	var loadedBlueprint *operations.Blueprint
	if strings.TrimSpace(packSlug) != "" {
		if loaded, err := operations.LoadBlueprint(repoRoot, packSlug); err == nil {
			operationTemplateExists = true
			bp := loaded
			loadedBlueprint = &bp
		}
	}
	var pack *agent.PackDefinition
	if !operationTemplateExists && !blankSlateLaunch {
		pack = agent.GetPack(packSlug)
	}
	if pack == nil && strings.TrimSpace(packSlug) != "" && !operationTemplateExists && !blankSlateLaunch {
		return nil, fmt.Errorf("unknown pack or operation blueprint: %s", packSlug)
	}

	// --pack is authoritative for company.json, but it must not implicitly
	// delete broker runtime state. A restart after a crash may pass the same
	// pack/blueprint again; wiping broker-state.json there makes the office
	// appear to reset even though the user never requested a destructive wipe.
	if explicitPack {
		var err error
		switch {
		case operationTemplateExists:
			err = resetManifestToOperationBlueprint(repoRoot, packSlug)
		case pack != nil:
			err = resetManifestToPack(pack)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: save blueprint/pack config: %v\n", err)
		}
	}
	sessionMode, oneOnOne := loadRunningSessionMode()

	return &Launcher{
		packSlug:            packSlug,
		pack:                pack,
		operationBlueprint:  loadedBlueprint,
		blankSlateLaunch:    blankSlateLaunch,
		sessionName:         SessionName,
		cwd:                 cwd,
		sessionMode:         sessionMode,
		oneOnOne:            oneOnOne,
		provider:            config.ResolveLLMProvider(""),
		headlessWorkers:     make(map[string]bool),
		headlessActive:      make(map[string]*headlessCodexActiveTurn),
		headlessQueues:      make(map[string][]headlessCodexTurn),
		notifyLastDelivered: make(map[string]time.Time),
	}, nil
}

// isOnboarded reports whether the user has completed the onboarding wizard.
// Any error loading state is treated as not-onboarded so a corrupt or
// missing ~/.wuphf/onboarded.json still lets the web UI boot into the
// wizard rather than failing at preflight.
func isOnboarded() bool {
	s, err := onboarding.Load()
	if err != nil || s == nil {
		return false
	}
	return s.Onboarded()
}

// Preflight checks that required tools are available.
func (l *Launcher) Preflight() error {
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
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found. Install: brew install tmux")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude not found. Install: npm install -g @anthropic-ai/claude-code")
	}
	if _, _, note := checkGHCapability(); note != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", note)
	}
	return nil
}

// checkGHCapability checks whether the gh CLI is installed and authenticated.
// It returns a soft-warning note when either condition is not met; callers
// should print the note but must NOT treat it as a fatal error — agents can
// still work locally without gh. Only PR-opening will be unavailable.
func checkGHCapability() (installed bool, authed bool, note string) {
	if _, err := exec.LookPath("gh"); err != nil {
		return false, false, "gh CLI not found in PATH; agents won't be able to open real PRs. Install from https://cli.github.com."
	}
	cmd := exec.CommandContext(context.Background(), "gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return true, false, "gh installed but not authenticated; run `gh auth login` so agents can open real PRs."
	}
	return true, true, ""
}

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
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())
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

// notifyAgentsLoop subscribes to broker messages and pushes notifications immediately.
func (l *Launcher) notifyAgentsLoop() {
	if l.broker == nil {
		return
	}
	msgs, unsubscribe := l.broker.SubscribeMessages(128)
	defer unsubscribe()

	for msg := range msgs {
		if l.broker.HasPendingInterview() {
			continue
		}
		if msg.From == "system" {
			continue
		}
		l.safeDeliverMessage(msg)
	}
}

// safeDeliverMessage wraps deliverMessageNotification in a panic recover so a
// bad message doesn't take the whole broker down. Stack is written to stderr
// and logs/panics.log so we can diagnose the next occurrence.
func (l *Launcher) safeDeliverMessage(msg channelMessage) {
	defer recoverPanicTo("deliverMessageNotification", fmt.Sprintf("msg=%+v", msg))
	l.deliverMessageNotification(msg)
}

// recoverPanicTo is the shared panic-recovery body used by broker background
// goroutines. It logs the goroutine stack to stderr and to
// ~/.wuphf/logs/panics.log so the broker stays up even if a specific action
// path blows up. Call as: defer recoverPanicTo("loopName", "extra context").
func recoverPanicTo(site, extra string) {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, 16<<10)
	n := runtime.Stack(buf, false)
	fmt.Fprintf(os.Stderr, "panic in %s: %v\n%s\n%s\n", site, r, extra, buf[:n])
	if home, err := os.UserHomeDir(); err == nil {
		if f, ferr := os.OpenFile(filepath.Join(home, ".wuphf", "logs", "panics.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
			_, _ = fmt.Fprintf(f, "%s panic in %s: %v\n%s\n%s\n\n", time.Now().UTC().Format(time.RFC3339), site, r, extra, buf[:n])
			_ = f.Close()
		}
	}
}

func (l *Launcher) notifyTaskActionsLoop() {
	if l.broker == nil {
		return
	}
	actions, unsubscribe := l.broker.SubscribeActions(128)
	defer unsubscribe()

	for action := range actions {
		if l.broker.HasPendingInterview() {
			continue
		}
		if action.Kind != "task_created" && action.Kind != "task_updated" && action.Kind != "task_unblocked" {
			continue
		}
		task, ok := l.taskForAction(action)
		if !ok {
			continue
		}
		// Skip "done" tasks for task_created / task_updated — the agent that completed
		// the task should send a follow-up broadcast which wakes CEO via the message
		// loop. But for task_unblocked the task status is still "in_progress" (it was
		// just unblocked), so we must never skip it regardless of status.
		if action.Kind != "task_unblocked" && strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		func() {
			defer recoverPanicTo("deliverTaskNotification", fmt.Sprintf("action=%+v task=%+v", action, task))
			l.deliverTaskNotification(action, task)
		}()
	}
}

func (l *Launcher) notifyOfficeChangesLoop() {
	if l.broker == nil {
		return
	}
	changes, unsubscribe := l.broker.SubscribeOfficeChanges(128)
	defer unsubscribe()

	for evt := range changes {
		// office_reseeded fires after onboarding rewrites the whole roster
		// (blueprint selection). The interactive claude panes were spawned
		// from the earlier default team and now point at slugs that are no
		// longer registered agents — messages typed into them go into a
		// dead shell. Respawn them against the new roster, outside the
		// interview guard so it can't be blocked by a half-complete wizard.
		if evt.Kind == "office_reseeded" {
			l.respawnPanesAfterReseed()
			continue
		}
		if l.broker.HasPendingInterview() {
			continue
		}
		l.deliverOfficeChangeNotification(evt)
	}
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

type officeChangeTaskNotification struct {
	Target  notificationTarget
	Action  officeActionLog
	Task    teamTask
	Content string
}

func (l *Launcher) deliverOfficeChangeNotification(evt officeChangeEvent) {
	for _, notification := range l.officeChangeTaskNotifications(evt) {
		l.sendTaskUpdate(notification.Target, notification.Action, notification.Task, notification.Content)
	}
}

func (l *Launcher) officeChangeTaskNotifications(evt officeChangeEvent) []officeChangeTaskNotification {
	if l == nil || l.broker == nil {
		return nil
	}

	kind := strings.TrimSpace(evt.Kind)
	slug := normalizeChannelSlug(evt.Slug)
	switch kind {
	case "member_created", "channel_created", "channel_updated":
	default:
		return nil
	}

	targetMap := l.agentPaneTargets()
	if len(targetMap) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	seen := make(map[string]struct{})
	var out []officeChangeTaskNotification
	for _, task := range l.broker.AllTasks() {
		owner := strings.TrimSpace(task.Owner)
		if owner == "" {
			continue
		}
		if !shouldBackfillTaskOwner(kind, slug, task) {
			continue
		}
		enabled := false
		for _, member := range l.broker.EnabledMembers(task.Channel) {
			if member == owner {
				enabled = true
				break
			}
		}
		if !enabled {
			continue
		}
		target, ok := targetMap[owner]
		if !ok {
			continue
		}
		key := owner + ":" + task.ID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		action := officeActionLog{
			Kind:      "task_updated",
			Source:    "office",
			Channel:   normalizeChannelSlug(task.Channel),
			Actor:     "system",
			RelatedID: task.ID,
			CreatedAt: now,
		}
		out = append(out, officeChangeTaskNotification{
			Target:  target,
			Action:  action,
			Task:    task,
			Content: l.taskNotificationContent(action, task),
		})
	}
	return out
}

func shouldBackfillTaskOwner(kind, slug string, task teamTask) bool {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	if status == "done" || status == "canceled" || status == "cancelled" || status == "review" {
		return false
	}
	if task.Blocked {
		return false
	}
	switch kind {
	case "member_created":
		return strings.TrimSpace(task.Owner) == slug
	case "channel_created", "channel_updated":
		return normalizeChannelSlug(task.Channel) == slug
	default:
		return false
	}
}

const (
	agentNotifyCooldown      = 1 * time.Second
	agentNotifyCooldownAgent = 2 * time.Second
)

func (l *Launcher) deliverMessageNotification(msg channelMessage) {
	// demo_seed messages exist purely to make #general feel staffed on first
	// paint; they must never wake an agent or burn an LLM call. Filter at
	// the central delivery point (not just notifyAgentsLoop) so other
	// callers — primeVisibleAgents, replays, future routes — can't bypass
	// it. Today these don't actually route demo_seed targets because the
	// lead is the From and Tagged is empty, but a future @all-default
	// change would silently turn the demo seed into an LLM-burning
	// broadcast. One filter, one place.
	if msg.Kind == "demo_seed" {
		return
	}
	immediate, delayed := l.notificationTargetsForMessage(msg)

	// Debounce: use shorter cooldown for human/CEO messages, longer for agent-originated
	// to prevent agent-to-agent feedback loops (devil's advocate finding #3)
	isHumanOrCEO := msg.From == "you" || msg.From == "human" || msg.From == "nex" || msg.From == l.officeLeadSlug()
	cooldown := agentNotifyCooldownAgent
	if isHumanOrCEO {
		cooldown = agentNotifyCooldown
	}
	now := time.Now()
	filtered := make([]notificationTarget, 0, len(immediate))
	l.notifyMu.Lock()
	if l.notifyLastDelivered == nil {
		l.notifyLastDelivered = make(map[string]time.Time)
	}
	for _, t := range immediate {
		if last, ok := l.notifyLastDelivered[t.Slug]; ok && now.Sub(last) < cooldown {
			continue
		}
		l.notifyLastDelivered[t.Slug] = now
		filtered = append(filtered, t)
	}
	l.notifyMu.Unlock()
	immediate = filtered

	// Mark implicit public-channel routing targets as active so the UI can show
	// the ephemeral "X is thinking..." indicator. DMs suppress this signal.
	isDM, _ := l.isChannelDM(normalizeChannelSlug(msg.Channel))
	if l.broker != nil && len(immediate) > 0 && (msg.From == "you" || msg.From == "human") && !l.isOneOnOne() && !isDM && len(msg.Tagged) == 0 {
		slugs := make([]string, 0, len(immediate))
		for _, t := range immediate {
			slugs = append(slugs, t.Slug)
		}
		l.broker.MarkRoutingTargets(slugs)
	}

	for _, target := range immediate {
		l.sendChannelUpdate(target, msg)
	}
	// Note: delayed is always empty for message notifications — notificationTargetsForMessage
	// only ever populates immediate. The delayed path is used only for task notifications
	// via taskNotificationTargets/deliverTaskNotification.
	_ = delayed
}

func (l *Launcher) deliverTaskNotification(action officeActionLog, task teamTask) {
	immediate, delayed := l.taskNotificationTargets(action, task)
	if len(immediate) == 0 && len(delayed) == 0 {
		return
	}
	content := l.taskNotificationContent(action, task)
	for _, target := range immediate {
		l.sendTaskUpdate(target, action, task, content)
	}
	for _, target := range delayed {
		go func(target notificationTarget, action officeActionLog, task teamTask) {
			time.Sleep(ceoHeadStartDelay)
			if !l.shouldDeliverDelayedTaskNotification(target.Slug, action, task) {
				return
			}
			l.sendTaskUpdate(target, action, task, content)
		}(target, action, task)
	}
}

type notificationTarget struct {
	PaneTarget string
	Slug       string
}

func (l *Launcher) taskNotificationTargets(action officeActionLog, task teamTask) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.agentNotificationTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	lead := l.officeLeadSlug()
	enabledMembers := map[string]struct{}{}
	disabledMembers := map[string]struct{}{}
	if l.broker != nil {
		for _, member := range l.broker.EnabledMembers(task.Channel) {
			enabledMembers[member] = struct{}{}
		}
		for _, member := range l.broker.DisabledMembers(task.Channel) {
			disabledMembers[member] = struct{}{}
		}
	}
	// Task ownership is an explicit human/CEO assignment. The same bypass that
	// lets an @-tag wake a wizard-hired specialist applies here: the owner may
	// have been hired post-seed and not yet in ch.Members. Disabled (muted)
	// members are still excluded — muting is an explicit silence.
	actor := strings.TrimSpace(action.Actor)
	owner := strings.TrimSpace(task.Owner)
	isAssigned := func(slug string) bool {
		return slug != "" && (slug == owner || slug == actor)
	}
	addImmediate := func(slug string) {
		if slug == "" {
			return
		}
		if _, muted := disabledMembers[slug]; muted {
			return
		}
		if !isAssigned(slug) && len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			immediate = append(immediate, target)
			delete(targetMap, slug)
		}
	}
	addDelayed := func(slug string) {
		if slug == "" {
			return
		}
		if _, muted := disabledMembers[slug]; muted {
			return
		}
		if !isAssigned(slug) && len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			delayed = append(delayed, target)
			delete(targetMap, slug)
		}
	}

	if owner == "" {
		if lead != "" && lead != actor {
			addImmediate(lead)
		}
		return immediate, delayed
	}

	if owner == lead {
		if lead != "" && lead != actor {
			addImmediate(lead)
		}
		return immediate, delayed
	}

	// Assigned owners should start immediately when new work lands, especially
	// for CEO-created or automation-created tasks. This is the bridge between
	// "policy created work" and "the specialist actually begins moving."
	//
	// Exception: do not wake the owner when the task is blocked (unresolved
	// dependencies). They have no work to do until the blocker clears. They
	// will be notified via a task_unblocked action when deps resolve.
	if (action.Kind == "task_created" || action.Kind == "watchdog_alert" || action.Kind == "task_unblocked") && owner != actor && !task.Blocked {
		addImmediate(owner)
	} else if owner != actor && action.Kind != "task_created" {
		addDelayed(owner)
	}

	if lead != "" && lead != owner && lead != actor && !(action.Kind == "task_created" && actor == lead) && shouldWakeLeadForTaskAction(action, task) {
		addImmediate(lead)
	}

	return immediate, delayed
}

func shouldWakeLeadForTaskAction(action officeActionLog, task teamTask) bool {
	if strings.TrimSpace(action.Kind) != "task_updated" {
		return true
	}
	actor := strings.TrimSpace(action.Actor)
	owner := strings.TrimSpace(task.Owner)
	if actor == "" || owner == "" || actor != owner {
		return true
	}
	if task.Blocked {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	review := strings.ToLower(strings.TrimSpace(task.ReviewState))
	if status == "review" || status == "done" || status == "blocked" {
		return true
	}
	if review == "ready_for_review" || review == "approved" {
		return true
	}
	return false
}

func (l *Launcher) taskForAction(action officeActionLog) (teamTask, bool) {
	if l.broker == nil || strings.TrimSpace(action.RelatedID) == "" {
		return teamTask{}, false
	}
	id := strings.TrimSpace(action.RelatedID)
	for _, task := range l.broker.AllTasks() {
		if task.ID == id {
			return task, true
		}
	}
	return teamTask{}, false
}

func (l *Launcher) shouldDeliverDelayedTaskNotification(targetSlug string, action officeActionLog, task teamTask) bool {
	if l.broker == nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
		return false
	}
	current, ok := l.taskForAction(action)
	if !ok {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(current.Status), "done") {
		return false
	}
	if strings.TrimSpace(current.Owner) != "" && strings.TrimSpace(current.Owner) != targetSlug && targetSlug != l.officeLeadSlug() {
		return false
	}
	if strings.TrimSpace(current.Owner) == "" && targetSlug != l.officeLeadSlug() {
		return false
	}
	return true
}

// taskNotificationContent delegates to the notificationContextBuilder
// (PLAN.md §C3). See notification_context.go for the formatting body.
func (l *Launcher) taskNotificationContent(action officeActionLog, task teamTask) string {
	return l.notifyCtx().TaskNotificationContent(action, task)
}

func (l *Launcher) sendTaskUpdate(target notificationTarget, action officeActionLog, task teamTask, content string) {
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	notification := l.buildTaskExecutionPacket(target.Slug, action, task, content)
	if l.shouldUseHeadlessDispatchForTarget(target) {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.queuePaneNotification(target.Slug, target.PaneTarget, notification)
}

// isChannelDM returns true if the channel is a DM (either old dm-* format or new Store type).
// agentTarget returns the agent slug that should receive the DM notification (non-human side).
// isChannelDM is the public entry point used by dispatch code; targeter
// reads the same logic via the isChannelDMRaw callback.
func (l *Launcher) isChannelDM(channelSlug string) (isDM bool, agentTarget string) {
	return l.isChannelDMRaw(channelSlug)
}

// isChannelDMRaw resolves whether a channel is a direct-message channel
// and, if so, which agent it targets. Two formats supported: the legacy
// "dm-{agent}" slug and the new store format where channel.type == "D".
func (l *Launcher) isChannelDMRaw(channelSlug string) (isDM bool, agentTarget string) {
	if IsDMSlug(channelSlug) {
		return true, DMTargetAgent(channelSlug)
	}
	if l.broker != nil {
		cs := l.broker.ChannelStore()
		if cs != nil && cs.IsDirectMessageBySlug(channelSlug) {
			ch, ok := cs.GetBySlug(channelSlug)
			if ok {
				members := cs.Members(ch.ID)
				for _, m := range members {
					if m.Slug != "human" && m.Slug != "you" {
						return true, m.Slug
					}
				}
			}
		}
	}
	return false, ""
}

func (l *Launcher) notificationTargetsForMessage(msg channelMessage) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.agentNotificationTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	// DMs are isolated: only the target agent gets notified, never CEO or others.
	if ch := normalizeChannelSlug(msg.Channel); IsDMSlug(ch) {
		agentSlug := DMTargetAgent(ch)
		if agentSlug == msg.From {
			return nil, nil // agent's own message, don't echo back
		}
		if target, ok := targetMap[agentSlug]; ok {
			return []notificationTarget{target}, nil
		}
		return nil, nil
	}
	// Also check the new Store-based DM format.
	if ch := normalizeChannelSlug(msg.Channel); !IsDMSlug(ch) {
		if isDM, agentSlug := l.isChannelDM(ch); isDM {
			if agentSlug == msg.From {
				return nil, nil
			}
			if target, ok := targetMap[agentSlug]; ok {
				return []notificationTarget{target}, nil
			}
			return nil, nil
		}
	}
	if l.isOneOnOne() {
		slug := l.oneOnOneAgent()
		if slug == "" || slug == msg.From {
			return nil, nil
		}
		target, ok := targetMap[slug]
		if !ok {
			return nil, nil
		}
		return []notificationTarget{target}, nil
	}
	lead := l.officeLeadSlug()
	owner := ""
	if l.broker != nil {
		owner = l.taskOwnerForMessage(msg)
	}
	enabledMembers := map[string]struct{}{}
	disabledMembers := map[string]struct{}{}
	if l.broker != nil {
		for _, member := range l.broker.EnabledMembers(msg.Channel) {
			enabledMembers[member] = struct{}{}
		}
		for _, member := range l.broker.DisabledMembers(msg.Channel) {
			disabledMembers[member] = struct{}{}
		}
	}

	// isExplicit checks whether a slug was explicitly @-tagged by the sender.
	// Explicit tags bypass the enabledMembers filter so a newly hired specialist
	// not yet in ch.Members can still be reached. They do NOT bypass ch.Disabled:
	// an explicit disable is the user's intent to silence the agent, and an
	// @-tag must not override it.
	isExplicit := func(slug string) bool { return containsSlug(msg.Tagged, slug) }

	addImmediate := func(slug string) {
		if slug == "" || slug == msg.From {
			return
		}
		if _, muted := disabledMembers[slug]; muted {
			return
		}
		if !isExplicit(slug) && len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			immediate = append(immediate, target)
			delete(targetMap, slug)
		}
	}
	allowTarget := func(slug string) bool {
		if slug == "" || slug == msg.From {
			return false
		}
		if _, muted := disabledMembers[slug]; muted {
			return false
		}
		explicit := isExplicit(slug)
		if !explicit && len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return false
			}
		}
		if slug == lead {
			return true
		}
		// Explicit @-tag: always allow regardless of domain. Domain inference is
		// for implicit routing only — it should never suppress an explicit mention.
		if explicit {
			return true
		}
		if owner != "" {
			return slug == owner
		}
		if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Title) == "" {
			return false
		}
		return l.messageTargetsAgent(msg, slug)
	}

	// Focus mode (delegation): CEO routes all work. Specialists only wake
	// when explicitly tagged by CEO or human. No cross-agent chatter.
	if l.isFocusModeEnabled() {
		switch {
		case msg.From == "you" || msg.From == "human" || msg.Kind == "automation" || msg.From == "nex":
			// When the human explicitly @tags one or more specialists, deliver directly
			// to those specialists only. CEO does not need to re-route explicit assignments —
			// the specialist is already awake and acting. CEO only sees untagged human messages
			// (general questions, requests that need routing decisions).
			humanExplicitlyTaggedSpecialists := false
			for _, slug := range msg.Tagged {
				if slug == "" || slug == msg.From || slug == lead {
					continue
				}
				// Respect explicit disables. A muted specialist stays muted
				// even when @-tagged — muting is the user's explicit intent.
				if _, muted := disabledMembers[slug]; muted {
					continue
				}
				// Explicit @-tag trumps channel-membership. The specialist
				// may have been hired after #general was seeded and not yet
				// added to ch.Members; dropping the notification here would
				// silently re-route the human's direct address to CEO.
				if target, ok := targetMap[slug]; ok {
					immediate = append(immediate, target)
					delete(targetMap, slug)
					humanExplicitlyTaggedSpecialists = true
				}
			}
			if !humanExplicitlyTaggedSpecialists {
				// No specialist tagged — CEO decides who handles this.
				addImmediate(lead)
			}
		case msg.From == lead:
			for _, slug := range msg.Tagged {
				if slug != lead && allowTarget(slug) {
					addImmediate(slug)
				}
			}
		default:
			// Specialist message: wake CEO only if it is a substantive update (not a status ping).
			// [STATUS] lines are internal progress markers — CEO does not need to re-route on them.
			isStatusOnly := strings.HasPrefix(strings.TrimSpace(msg.Content), "[STATUS]")
			if !isStatusOnly {
				addImmediate(lead)
			}
		}
		return immediate, delayed
	}

	// Collaborative mode: all agents can see domain-relevant messages
	switch {
	case msg.From == "you" || msg.From == "human" || msg.Kind == "automation" || msg.From == "nex":
		// @all: notify every agent immediately.
		if containsSlug(msg.Tagged, "all") {
			addImmediate(lead)
			for slug := range targetMap {
				addImmediate(slug)
			}
			break
		}
		addImmediate(lead)
		if owner != "" && owner != lead && allowTarget(owner) {
			addImmediate(owner)
		}
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	case msg.From == lead:
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	case containsSlug(msg.Tagged, lead):
		addImmediate(lead)
		if owner != "" && owner != lead && allowTarget(owner) {
			addImmediate(owner)
		}
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	default:
		// Specialist-to-channel message in collaborative mode: CEO stays in the loop
		// plus any tagged agents and the task owner.
		addImmediate(lead)
		if owner != "" && owner != lead && allowTarget(owner) {
			addImmediate(owner)
		}
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	}
	return immediate, delayed
}

func (l *Launcher) watchChannelPaneLoop(channelCmd string) {
	unhealthyCount := 0
	var deadSince time.Time
	snapshotWritten := false
	for {
		time.Sleep(2 * time.Second)

		status, err := l.channelPaneStatus()
		if err != nil {
			if isNoSessionError(err.Error()) {
				return
			}
			continue
		}
		if !channelPaneNeedsRespawn(status) {
			unhealthyCount = 0
			deadSince = time.Time{}
			snapshotWritten = false
			continue
		}
		unhealthyCount++
		if unhealthyCount < 2 {
			continue
		}
		if deadSince.IsZero() {
			deadSince = time.Now()
		}
		if !snapshotWritten {
			_ = l.captureDeadChannelPane(status)
			snapshotWritten = true
		}
		if time.Since(deadSince) < channelRespawnDelay {
			continue
		}
		unhealthyCount = 0
		deadSince = time.Time{}
		snapshotWritten = false
		target := l.sessionName + ":team.0"
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "respawn-pane", "-k",
			"-t", target,
			"-c", l.cwd,
			channelCmd,
		).Run()
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
			"-t", target,
			"-T", "📢 channel",
		).Run()
	}
}

func (l *Launcher) channelPaneStatus() (string, error) {
	out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "display-message",
		"-p",
		"-t", l.sessionName+":team.0",
		"#{pane_dead} #{pane_dead_status} #{pane_current_command}",
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

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

func isNoSessionError(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "can't find") || strings.Contains(msg, "no server")
}

func (l *Launcher) captureDeadChannelPane(status string) error {
	content, err := l.capturePaneContent(0)
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

func channelStderrLogPath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return ".wuphf-channel-stderr.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-stderr.log")
}

func channelPaneSnapshotPath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return ".wuphf-channel-pane.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-pane.log")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// primeVisibleAgents clears Claude startup interactivity in newly spawned panes and
// replays a catch-up channel nudge once they are actually ready to read it.
func (l *Launcher) primeVisibleAgents() {
	time.Sleep(1 * time.Second)

	targets := l.agentPaneTargets()
	if len(targets) == 0 {
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		allReady := true
		for _, target := range targets {
			content, err := l.capturePaneTargetContent(target.PaneTarget)
			if err != nil {
				allReady = false
				continue
			}
			if shouldPrimeClaudePane(content) {
				_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "send-keys",
					"-t", target.PaneTarget,
					"Enter",
				).Run()
				allReady = false
			}
		}
		if allReady {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// If the human already posted while Claude was still booting, replay a catch-up nudge
	// so the first visible message is not lost forever behind the startup interactivity.
	if l.broker == nil {
		return
	}
	msgs := l.broker.Messages()
	if len(msgs) > 0 {
		latest := msgs[len(msgs)-1]
		l.deliverMessageNotification(latest)
	}
	l.resumeInFlightWork()
}

func (l *Launcher) pollOneRelayEventsLoop() {
	if l.broker == nil {
		return
	}
	provider := action.NewOneCLIFromEnv()
	if _, err := provider.ListRelays(context.Background(), action.ListRelaysOptions{Limit: 1}); err != nil {
		return
	}
	interval := time.Minute
	time.Sleep(25 * time.Second)
	for {
		l.updateSchedulerJob("one-relay-events", "One relay events", interval, time.Now().UTC(), "running")
		l.fetchAndRecordOneRelayEvents(provider)
		l.updateSchedulerJob("one-relay-events", "One relay events", interval, time.Now().UTC().Add(interval), "sleeping")
		time.Sleep(interval)
	}
}

func (l *Launcher) fetchAndRecordOneRelayEvents(provider *action.OneCLI) {
	if l.broker == nil || provider == nil {
		return
	}
	result, err := provider.ListRelayEvents(context.Background(), action.RelayEventsOptions{Limit: 20})
	if err != nil {
		return
	}
	if len(result.Events) == 0 {
		return
	}
	var signals []officeSignal
	for _, event := range result.Events {
		title := strings.TrimSpace(event.EventType)
		if title == "" {
			title = "Relay event"
		}
		content := fmt.Sprintf("One relay received %s on %s.", strings.TrimSpace(event.EventType), strings.TrimSpace(event.Platform))
		signals = append(signals, officeSignal{
			ID:         strings.TrimSpace(event.ID),
			Source:     "one",
			Kind:       "relay_event",
			Title:      title,
			Content:    content,
			Channel:    "general",
			Owner:      "ceo",
			Confidence: "medium",
			Urgency:    "medium",
		})
	}
	records, err := l.broker.RecordSignals(signals)
	if err != nil || len(records) == 0 {
		return
	}
	for _, record := range records {
		_ = l.broker.RecordAction(
			"external_trigger_received",
			"one",
			record.Channel,
			"one",
			truncateSummary(record.Title+" "+record.Content, 140),
			record.ID,
			[]string{record.ID},
			"",
		)
	}
}

// scheduler returns the watchdog scheduler, lazily constructing it on
// first access. Constructed nil-safe so tests that build &Launcher{}
// directly never trip on a missing scheduler. Production wiring
// (clock=realClock, broker=l.broker) happens here.
func (l *Launcher) scheduler() *watchdogScheduler {
	if l == nil {
		return nil
	}
	// sync.Once guards lazy-init: Launch() spawns watchdogSchedulerLoop
	// alongside other goroutines that hit scheduler() concurrently
	// (e.g. headless dispatch enqueues that call updateSchedulerJob).
	l.schedulerWorkerOnce.Do(func() {
		l.schedulerWorker = &watchdogScheduler{
			broker:      l.broker,
			clock:       realClock{},
			deliverTask: l.deliverTaskNotification,
		}
	})
	return l.schedulerWorker
}

// updateSchedulerJob and watchdogSchedulerLoop are thin Launcher wrappers
// around the watchdogScheduler type (PLAN.md §C4). Wrappers kept so the
// existing callers in headless_codex.go and Launch() don't need a rename
// sweep in this PR; cleanup is a follow-up.

func (l *Launcher) updateSchedulerJob(slug, label string, interval time.Duration, nextRun time.Time, status string) {
	l.scheduler().updateJob(slug, label, interval, nextRun, status)
}

func (l *Launcher) watchdogSchedulerLoop() {
	l.scheduler().Start(context.Background())
}

func (l *Launcher) processDueSchedulerJobs() { l.scheduler().processOnce() }

func (l *Launcher) processDueTaskJob(job schedulerJob) { l.scheduler().processTaskJob(job) }

func (l *Launcher) processDueRequestJob(job schedulerJob) { l.scheduler().processRequestJob(job) }

func (l *Launcher) processDueWorkflowJob(job schedulerJob) { l.scheduler().processWorkflowJob(job) }

func (l *Launcher) recordWatchdogLedger(channel, kind, targetID, owner, summary, sourceSignalID string) ([]string, string) {
	return l.scheduler().recordLedger(channel, kind, targetID, owner, summary, sourceSignalID)
}

func (req humanInterview) TitleOrDefault() string {
	if strings.TrimSpace(req.Title) != "" {
		return req.Title
	}
	return "Request"
}

// targeter returns the office-targeter, lazily constructing it on first
// access. PLAN.md trap §5.4: tests build &Launcher{} directly and rely on
// every sub-type being nil-safe. The targeter shares mutable state with
// the launcher via pointers/maps (paneBackedFlag, failedPaneSlugs) so
// later mutations on the launcher are visible to the targeter immediately.
func (l *Launcher) targeter() *officeTargeter {
	if l == nil {
		return nil
	}
	// sync.Once guards against the lazy-init races that arise during
	// Launch() — multiple goroutines (notifyAgentsLoop, watchdogSchedulerLoop,
	// pollNexNotificationsLoop, etc.) can hit this concurrently before the
	// first ordered call has finished. Without the Once, two goroutines can
	// each observe l.targets == nil and both write a fresh pointer.
	l.targetsOnce.Do(func() {
		l.targets = &officeTargeter{
			sessionName:    l.sessionName,
			pack:           l.pack,
			cwd:            l.cwd,
			provider:       l.provider,
			paneBackedFlag: &l.paneBackedAgents,
			// Read via callback so reconfigureVisibleAgents nilling
			// l.failedPaneSlugs (and recordPaneSpawnFailure rebuilding it)
			// stays observable to the targeter. A snapshotted map pointer
			// would orphan after the first reconfigure.
			failedPaneSlugs:    func() map[string]string { return l.failedPaneSlugs },
			isOneOnOne:         l.isOneOnOne,
			oneOnOneSlug:       l.oneOnOneAgent,
			isChannelDM:        l.isChannelDMRaw,
			snapshotMembers:    l.officeMembersSnapshot,
			memberProviderKind: l.brokerMemberProviderKind,
		}
	})
	return l.targets
}

// brokerMemberProviderKind reads the per-member provider override from the
// broker, or "" when no broker is wired or no override is set.
func (l *Launcher) brokerMemberProviderKind(slug string) string {
	if l == nil || l.broker == nil {
		return ""
	}
	return l.broker.MemberProviderKind(slug)
}

// agentPaneSlugs and friends are thin wrappers that delegate to the
// targeter. Kept as Launcher methods so the ~110 callers across
// internal/team don't need a sweep in this PR; consolidation is a
// follow-up. PLAN.md §6 wants the call sites renamed eventually but the
// bulk of the diff would be call-site churn rather than logic changes.

func (l *Launcher) agentPaneSlugs() []string { return l.targeter().PaneSlugs() }

func (l *Launcher) officeAgentOrder() []officeMember { return l.targeter().AgentOrder() }

func (l *Launcher) visibleOfficeMembers() []officeMember {
	return l.targeter().VisibleMembers()
}

func (l *Launcher) overflowOfficeMembers() []officeMember {
	return l.targeter().OverflowMembers()
}

func (l *Launcher) paneEligibleOfficeMembers() []officeMember {
	return l.targeter().PaneEligibleMembers()
}

func (l *Launcher) resolvePaneTargetForSlug(slug string) (string, bool) {
	return l.targeter().ResolvePaneTarget(slug)
}

func (l *Launcher) agentPaneTargets() map[string]notificationTarget {
	return l.targeter().PaneTargets()
}

func (l *Launcher) agentNotificationTargets() map[string]notificationTarget {
	return l.targeter().NotificationTargets()
}

func (l *Launcher) shouldUseHeadlessDispatchForSlug(slug string) bool {
	return l.targeter().ShouldUseHeadlessForSlug(slug)
}

func (l *Launcher) shouldUseHeadlessDispatchForTarget(target notificationTarget) bool {
	return l.targeter().ShouldUseHeadlessForTarget(target)
}

func (l *Launcher) skipPaneForSlug(slug string) bool {
	return l.targeter().SkipPane(slug)
}

func (l *Launcher) isOneOnOne() bool {
	if l.broker != nil {
		mode, _ := l.broker.SessionModeState()
		return mode == SessionModeOneOnOne
	}
	return NormalizeSessionMode(l.sessionMode) == SessionModeOneOnOne
}

func (l *Launcher) oneOnOneAgent() string {
	if l.broker != nil {
		_, agent := l.broker.SessionModeState()
		return NormalizeOneOnOneAgent(agent)
	}
	return NormalizeOneOnOneAgent(l.oneOnOne)
}

// usesCodexRuntime reports whether the active install-wide provider uses the
// headless one-shot runtime (shared by Codex and Opencode — both skip the
// tmux/claude pane infrastructure and drive a fresh CLI per turn through the
// broker queue in headless_codex.go).
//
// Prefer the capability helpers (usesPaneRuntime, requiresClaudeSessionReset)
// for new code asking "is this a non-pane runtime" — they're Registry-driven
// and pick up future providers (Ollama, vLLM, exo, OpenAI-compatible) without
// further edits here. usesCodexRuntime stays for codex/opencode-binary-specific
// concerns (Preflight, launch routing).
func (l *Launcher) usesCodexRuntime() bool {
	p := strings.TrimSpace(strings.ToLower(l.provider))
	return p == "codex" || p == "opencode"
}

// usesOpencodeRuntime reports whether the install-wide provider is Opencode
// specifically. Used only where the per-turn CLI invocation differs from Codex
// (binary name, args, prompt layout).
func (l *Launcher) usesOpencodeRuntime() bool {
	return strings.EqualFold(strings.TrimSpace(l.provider), "opencode")
}

// usesPaneRuntime / requiresClaudeSessionReset / memberEffectiveProviderKind /
// memberUsesHeadlessOneShotRuntime live on officeTargeter (PLAN.md §C2);
// these wrappers keep ~30 in-package callers compiling without a rename
// sweep in this PR.
func (l *Launcher) usesPaneRuntime() bool { return l.targeter().UsesPaneRuntime() }

func (l *Launcher) requiresClaudeSessionReset() bool {
	return l.targeter().RequiresClaudeSessionReset()
}

func (l *Launcher) memberEffectiveProviderKind(slug string) string {
	return l.targeter().MemberEffectiveProviderKind(slug)
}

func (l *Launcher) memberUsesHeadlessOneShotRuntime(slug string) bool {
	return l.targeter().MemberUsesHeadlessOneShotRuntime(slug)
}

// UsesTmuxRuntime reports whether agents run in tmux panes. Equivalent to
// usesPaneRuntime — exported for cmd/wuphf/main.go and tests.
func (l *Launcher) UsesTmuxRuntime() bool {
	return l.usesPaneRuntime()
}

func (l *Launcher) BrokerToken() string {
	if l == nil || l.broker == nil {
		return ""
	}
	return l.broker.Token()
}

// OneOnOneAgent returns the active direct-session agent slug, if any.
func (l *Launcher) OneOnOneAgent() string {
	return l.oneOnOneAgent()
}

// killStaleBroker kills any process holding the configured broker port from a previous run.
func killStaleBroker() {
	out, err := exec.CommandContext(context.Background(), "lsof", "-i", fmt.Sprintf(":%d", brokeraddr.ResolvePort()), "-t").Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, pid := range strings.Fields(strings.TrimSpace(string(out))) {
		_ = exec.CommandContext(context.Background(), "kill", "-9", pid).Run()
	}
	time.Sleep(500 * time.Millisecond)
}

func containsSlug(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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
	if l.headlessCancel != nil {
		l.headlessCancel()
	}
	// Drain the watchdog scheduler before tearing down the broker so the
	// goroutine doesn't try to read jobs through a half-shut-down broker.
	// Stop() is a no-op if the scheduler was never started.
	if l.schedulerWorker != nil {
		l.schedulerWorker.Stop()
	}
	if l.broker != nil {
		l.broker.Stop()
	}
	// Clean temp files before tearing down tmux so the claude processes are
	// still alive to release any open handles (harmless, but principle of
	// least surprise).
	l.cleanupAgentTempFiles()
	if !l.usesPaneRuntime() {
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
	if !l.requiresClaudeSessionReset() {
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
	if !l.usesPaneRuntime() {
		if err := provider.ResetClaudeSessions(); err != nil {
			return fmt.Errorf("reset Claude sessions: %w", err)
		}
		if err := l.clearAgentPanes(); err != nil {
			return err
		}
		l.clearOverflowAgentWindows()
		return nil
	}
	return l.reconfigureVisibleAgents()
}

func (l *Launcher) reconfigureVisibleAgents() error {
	l.provider = config.ResolveLLMProvider("")
	if !l.usesPaneRuntime() {
		if l.paneBackedAgents {
			_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()
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
	panes, err := l.listTeamPanes()
	if err != nil {
		return err
	}
	l.clearOverflowAgentWindows()

	// Respawn each agent pane in place, preserving layout.
	// Never clear+recreate panes — that destroys the channel's layout.
	visibleMembers := l.visibleOfficeMembers()
	if len(panes) != len(visibleMembers) {
		if err := l.clearAgentPanes(); err != nil {
			return err
		}
		if _, err := l.spawnVisibleAgents(); err != nil {
			return err
		}
		l.spawnOverflowAgents()
		go l.detectDeadPanesAfterSpawn(visibleMembers)
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

		target := fmt.Sprintf("%s:team.%d", l.sessionName, idx)
		// respawn-pane -k kills the current process and starts a new one
		// in the same pane — preserving size and position
		out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "respawn-pane", "-k",
			"-t", target,
			"-c", l.cwd,
			cmd,
		).CombinedOutput()
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
	l.spawnOverflowAgents()
	go l.detectDeadPanesAfterSpawn(visibleMembers)

	if l.broker != nil {
		go l.primeVisibleAgents()
	}

	return nil
}

// notifyCtx returns the notification-context builder, lazily constructing
// it once via sync.Once (PLAN.md §C3). The builder shares state with
// Launcher via callbacks (channelMessages, channelTasks, allTasks,
// channelStore, scoreTaskCandidate, activeHeadlessAgents) — those are
// re-resolved through l.broker on every invocation, so the cached
// builder still sees current broker state on each work-packet build.
func (l *Launcher) notifyCtx() *notificationContextBuilder {
	if l == nil {
		return nil
	}
	// sync.Once guards against concurrent first-callers from the various
	// notify-* goroutines spawned in Launch() — without it, two of them
	// can both observe l.notify == nil and write competing pointers.
	l.notifyOnce.Do(func() {
		l.notify = newNotifyCtx(l)
	})
	return l.notify
}

func newNotifyCtx(l *Launcher) *notificationContextBuilder {
	return &notificationContextBuilder{
		targeter: l.targeter(),
		channelMessages: func(channelSlug string) []channelMessage {
			if l.broker == nil {
				return nil
			}
			return l.broker.ChannelMessages(channelSlug)
		},
		channelTasks: func(channelSlug string) []teamTask {
			if l.broker == nil {
				return nil
			}
			return l.broker.ChannelTasks(channelSlug)
		},
		allTasks: func() []teamTask {
			if l.broker == nil {
				return nil
			}
			return l.broker.AllTasks()
		},
		channelStore: func() *channel.Store {
			if l.broker == nil {
				return nil
			}
			return l.broker.ChannelStore()
		},
		scoreTaskCandidate:   l.scoreMessageForTaskCandidate,
		activeHeadlessAgents: l.activeHeadlessSlugs,
	}
}

// activeHeadlessSlugs returns the slugs that have non-empty headless
// queues or active turns at the moment of the call. Locks headlessMu so
// the snapshot is consistent. The except parameter is the slug being
// notified — the lead must not list itself as "already active".
func (l *Launcher) activeHeadlessSlugs(except string) map[string]struct{} {
	if l == nil {
		return nil
	}
	l.headlessMu.Lock()
	defer l.headlessMu.Unlock()
	out := map[string]struct{}{}
	for workerSlug, queue := range l.headlessQueues {
		if workerSlug == except {
			continue
		}
		if len(queue) > 0 {
			out[workerSlug] = struct{}{}
		}
	}
	for workerSlug, active := range l.headlessActive {
		if workerSlug == except {
			continue
		}
		if active != nil {
			out[workerSlug] = struct{}{}
		}
	}
	return out
}

// Notification-context methods on Launcher are thin wrappers; the bodies
// live in notification_context.go on notificationContextBuilder. Wrappers
// kept (rather than removed) so the ~50 in-package callers don't need a
// rename sweep in this PR — that's a follow-up.

func (l *Launcher) buildNotificationContext(channelSlug, triggerMsgID, threadRootID string, limit int) string {
	return l.notifyCtx().NotificationContext(channelSlug, triggerMsgID, threadRootID, limit)
}

func (l *Launcher) ultimateThreadRoot(channelSlug, startID string) string {
	return l.notifyCtx().UltimateThreadRoot(channelSlug, startID)
}

func (l *Launcher) threadMessageIDs(channelSlug, rootID string) map[string]struct{} {
	return l.notifyCtx().ThreadMessageIDs(channelSlug, rootID)
}

func (l *Launcher) buildTaskNotificationContext(channelSlug, slug string, limit int) string {
	return l.notifyCtx().TaskNotificationContext(channelSlug, slug, limit)
}

func (l *Launcher) relevantTaskForTarget(msg channelMessage, slug string) (teamTask, bool) {
	return l.notifyCtx().RelevantTaskForTarget(msg, slug)
}

func (l *Launcher) responseInstructionForTarget(msg channelMessage, slug string) string {
	return l.notifyCtx().ResponseInstructionForTarget(msg, slug)
}

func (l *Launcher) buildMessageWorkPacket(msg channelMessage, slug string) string {
	return l.notifyCtx().BuildMessageWorkPacket(msg, slug)
}

func (l *Launcher) buildTaskExecutionPacket(slug string, action officeActionLog, task teamTask, content string) string {
	return l.notifyCtx().BuildTaskExecutionPacket(slug, action, task, content)
}

func (l *Launcher) sendChannelUpdate(target notificationTarget, msg channelMessage) {
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		channel = "general"
	}
	notification := ""
	if l.isOneOnOne() {
		notification = fmt.Sprintf(
			"[New from @%s]: %s\n%s Reply using team_broadcast with my_slug \"%s\" and channel \"%s\" reply_to_id \"%s\". Once you have posted the needed reply, STOP and wait for the next pushed notification.",
			msg.From, truncate(msg.Content, 1000), l.responseInstructionForTarget(msg, target.Slug), target.Slug, channel, msg.ID,
		)
	} else {
		packet := l.buildMessageWorkPacket(msg, target.Slug)
		notification = fmt.Sprintf(
			"%s\n---\n[New from @%s]: %s\n%s This packet is your complete context — do NOT call team_poll or team_tasks. Just do the work and reply via team_broadcast with my_slug \"%s\", channel \"%s\", reply_to_id \"%s\". Once you have posted the needed update, STOP and wait for the next pushed notification.",
			packet, msg.From, truncate(msg.Content, 1000), l.responseInstructionForTarget(msg, target.Slug), target.Slug, channel, msg.ID,
		)
	}

	if l.shouldUseHeadlessDispatchForTarget(target) {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.queuePaneNotification(target.Slug, target.PaneTarget, notification)
}

// shouldUseHeadlessDispatch is a thin wrapper around the targeter; see
// officeTargeter.ShouldUseHeadless for semantics.
func (l *Launcher) shouldUseHeadlessDispatch() bool { return l.targeter().ShouldUseHeadless() }

// Minimum gap between two consecutive pane `/clear` + type cycles for the
// same agent. Serves as a safety floor against truly back-to-back sends
// (sub-second bursts) that could race tmux's input buffer. Not a substitute
// for the coalesce logic below — claude turns typically take 20-60s, which
// is much larger than any reasonable fixed gap.
//
// Declared as var rather than const so tests can shorten it.
var paneDispatchMinGap = 3 * time.Second

// paneDispatchCoalesceWindow: if a new notification arrives this soon after
// the previous send, treat claude's current turn as still in flight and
// MERGE the new content into the next dispatch instead of triggering a
// fresh `/clear`. Mid-turn clears were wiping claude's in-progress output
// and dropping one of two rapid @-tags entirely.
//
// 60s covers the p95 claude turn duration (including MCP tool calls) in
// the observed workload. Multiple bursts in the window get concatenated
// so claude eventually sees one prompt containing every question, answers
// them together, and never loses one to a mid-turn clear.
var paneDispatchCoalesceWindow = 60 * time.Second

// queuePaneNotification enqueues a notification for a pane-backed agent
// and ensures there is one worker draining its queue. Rapid successive
// tags for the same slug coalesce into a single dispatch if they arrive
// inside paneDispatchCoalesceWindow — this is the defence against
// mid-turn `/clear` wiping claude's in-progress output.
func (l *Launcher) queuePaneNotification(slug, paneTarget, notification string) {
	slug = strings.TrimSpace(slug)
	paneTarget = strings.TrimSpace(paneTarget)
	if slug == "" || paneTarget == "" || notification == "" {
		return
	}
	l.paneDispatchMu.Lock()
	if l.paneDispatchQueues == nil {
		l.paneDispatchQueues = make(map[string][]paneDispatchTurn)
	}
	if l.paneDispatchWorkers == nil {
		l.paneDispatchWorkers = make(map[string]bool)
	}
	if l.paneDispatchLastSentAt == nil {
		l.paneDispatchLastSentAt = make(map[string]time.Time)
	}
	// Coalesce path: if a pending turn already exists OR the last send was
	// within the coalesce window, merge rather than enqueue a separate
	// dispatch. Merging into the head-of-queue pending item is the cleanest
	// because the worker picks up the merged content on its next iteration.
	inflight := false
	if last, ok := l.paneDispatchLastSentAt[slug]; ok && time.Since(last) < paneDispatchCoalesceWindow {
		inflight = true
	}
	queue := l.paneDispatchQueues[slug]
	if (inflight || len(queue) > 0) && len(queue) > 0 {
		// Combine with the pending turn. Claude will see both prompts
		// separated by a visible divider and typically answers both.
		last := &l.paneDispatchQueues[slug][len(queue)-1]
		last.Notification = last.Notification + "\n\n---\n\n" + notification
		last.EnqueuedAt = time.Now()
		l.paneDispatchMu.Unlock()
		return
	}
	if inflight && len(queue) == 0 {
		// No pending turn yet but claude is mid-flight from a recent send.
		// Create a single pending turn that will absorb further bursts
		// through the branch above. The worker's pre-send wait will let
		// claude's current turn finish before /clear fires.
		l.paneDispatchQueues[slug] = []paneDispatchTurn{{
			PaneTarget:   paneTarget,
			Notification: notification,
			EnqueuedAt:   time.Now(),
		}}
		startWorker := !l.paneDispatchWorkers[slug]
		if startWorker {
			l.paneDispatchWorkers[slug] = true
		}
		l.paneDispatchMu.Unlock()
		if startWorker {
			go l.runPaneDispatchQueue(slug)
		}
		return
	}
	// Cold path: no recent activity, no queue. Dispatch immediately.
	l.paneDispatchQueues[slug] = append(l.paneDispatchQueues[slug], paneDispatchTurn{
		PaneTarget:   paneTarget,
		Notification: notification,
		EnqueuedAt:   time.Now(),
	})
	startWorker := !l.paneDispatchWorkers[slug]
	if startWorker {
		l.paneDispatchWorkers[slug] = true
	}
	l.paneDispatchMu.Unlock()
	if startWorker {
		go l.runPaneDispatchQueue(slug)
	}
}

// runPaneDispatchQueue is the single worker per agent slug that drains
// pane-dispatch notifications serially with a minimum gap between
// `/clear` cycles. Exits when the queue is empty.
//
// Per-iteration flow:
//  1. Peek head of queue.
//  2. Wait out min-gap (floor) + coalesce window (lets claude's current turn
//     land). During the wait, concurrent queuePaneNotification calls MAY
//     merge new content into the head's Notification field — the peek is
//     re-read after the wait so the pop sees the merged string.
//  3. Pop + send.
//  4. Record the send time so the next enqueue sees "claude in flight".
func (l *Launcher) runPaneDispatchQueue(slug string) {
	var lastSentAt time.Time
	for {
		// Step 1: peek (not pop).
		l.paneDispatchMu.Lock()
		queue := l.paneDispatchQueues[slug]
		if len(queue) == 0 {
			// Atomic handoff: clear worker flag while holding the lock so a
			// concurrent queuePaneNotification observes "no worker" and
			// starts a fresh goroutine for the next enqueue.
			delete(l.paneDispatchWorkers, slug)
			delete(l.paneDispatchQueues, slug)
			l.paneDispatchMu.Unlock()
			return
		}
		globalLastSentAt := l.paneDispatchLastSentAt[slug]
		l.paneDispatchMu.Unlock()

		// Step 2a: min-gap floor against sub-second bursts.
		if !lastSentAt.IsZero() {
			wait := paneDispatchMinGap - time.Since(lastSentAt)
			if wait > 0 {
				time.Sleep(wait)
			}
		}
		// Step 2b: coalesce window — wait for claude's in-flight turn to
		// land before /clear fires again. New notifications arriving
		// during this wait are merged into the head by queuePaneNotification.
		if !globalLastSentAt.IsZero() {
			wait := paneDispatchCoalesceWindow - time.Since(globalLastSentAt)
			if wait > 0 {
				time.Sleep(wait)
			}
		}

		// Step 3: pop (re-read head to pick up any merged content).
		l.paneDispatchMu.Lock()
		queue = l.paneDispatchQueues[slug]
		if len(queue) == 0 {
			// Defensive: external actor emptied the queue during our wait.
			l.paneDispatchMu.Unlock()
			continue
		}
		turn := queue[0]
		if len(queue) == 1 {
			delete(l.paneDispatchQueues, slug)
		} else {
			l.paneDispatchQueues[slug] = queue[1:]
		}
		l.paneDispatchMu.Unlock()

		launcherSendNotificationToPane(l, turn.PaneTarget, turn.Notification)

		// Step 4: record send time for the next enqueue's coalesce check.
		lastSentAt = time.Now()
		l.paneDispatchMu.Lock()
		l.paneDispatchLastSentAt[slug] = lastSentAt
		l.paneDispatchMu.Unlock()
	}
}

// launcherSendNotificationToPaneFn is the test seam type swapped via
// setLauncherSendNotificationToPaneForTest.
type launcherSendNotificationToPaneFn func(l *Launcher, paneTarget, notification string)

// launcherSendNotificationToPaneOverride is read by the pane-dispatch and
// resume goroutines, so it lives behind atomic.Pointer to stay race-clean
// against test cleanups that fire while a worker is mid-dispatch.
// Production reads fall through to sendNotificationToPane.
var launcherSendNotificationToPaneOverride atomic.Pointer[launcherSendNotificationToPaneFn]

func launcherSendNotificationToPane(l *Launcher, paneTarget, notification string) {
	if p := launcherSendNotificationToPaneOverride.Load(); p != nil {
		(*p)(l, paneTarget, notification)
		return
	}
	l.sendNotificationToPane(paneTarget, notification)
}

// sendNotificationToPane delivers a notification to a persistent interactive
// Claude session in a tmux pane. It sends /clear first so each turn starts
// with a fresh context window — the work packet carries all required context,
// so accumulated history is not needed and only causes drift over time.
// --append-system-prompt is a CLI flag and survives /clear intact.
//
// Callers should prefer queuePaneNotification — this function runs /clear +
// type + Enter unconditionally, so rapid direct calls will race each other.
// queuePaneNotification serializes per-slug and inserts the minimum gap.
func (l *Launcher) sendNotificationToPane(paneTarget, notification string) {
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "/clear", "Enter",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "-l", notification,
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "Enter",
	).Run()
}

func (l *Launcher) capturePaneTargetContent(target string) (string, error) {
	out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "capture-pane",
		"-p", "-J",
		"-t", target,
	).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (l *Launcher) capturePaneContent(paneIdx int) (string, error) {
	target := fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx)
	return l.capturePaneTargetContent(target)
}

func ResetBrokerState() error {
	token := os.Getenv("WUPHF_BROKER_TOKEN")
	if token == "" {
		token = os.Getenv("NEX_BROKER_TOKEN")
	}
	return resetBrokerState(brokerBaseURL(), token)
}

func ClearPersistedBrokerState() error {
	path := defaultBrokerStatePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func officePIDFilePath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "office.pid")
	}
	return filepath.Join(home, ".wuphf", "team", "office.pid")
}

func writeOfficePIDFile() error {
	path := officePIDFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func clearOfficePIDFile() error {
	path := officePIDFilePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func killPersistedOfficeProcess() error {
	raw, err := os.ReadFile(officePIDFilePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		// Stale or corrupt PID file: there's no process to kill, just
		// clear the file so the next launcher boots cleanly. Don't
		// surface the parse error — the caller's intent is "shut
		// anything down", and an unparseable PID file means there's
		// nothing to shut down.
		_ = clearOfficePIDFile()
		return nil //nolint:nilerr // intentional: corrupt PID file is a no-op
	}
	if pid == os.Getpid() {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		// On Unix os.FindProcess never errors, but cover the API
		// contract: if it ever does, clear the stale PID file and
		// continue — there's nothing to kill.
		_ = clearOfficePIDFile()
		return nil //nolint:nilerr // intentional: no process to kill, clear PID and move on
	}
	_ = proc.Kill()
	return nil
}

func resetBrokerState(baseURL, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/reset", nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("broker reset failed: %s", resp.Status)
	}
	return nil
}

func (l *Launcher) clearAgentPanes() error {
	panes, err := l.listTeamPanes()
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.IntSlice(panes)))
	for _, idx := range panes {
		if idx == 0 {
			continue // skip pane 0 (channel TUI)
		}
		target := fmt.Sprintf("%s:team.%d", l.sessionName, idx)
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-pane", "-t", target).Run()
	}
	return nil
}

func (l *Launcher) clearOverflowAgentWindows() {
	out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "list-windows",
		"-t", l.sessionName,
		"-F", "#{window_name}",
	).CombinedOutput()
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, "agent-") {
			continue
		}
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-window",
			"-t", fmt.Sprintf("%s:%s", l.sessionName, name),
		).Run()
	}
}

func (l *Launcher) listTeamPanes() ([]int, error) {
	out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "list-panes",
		"-t", l.sessionName+":team",
		"-F", "#{pane_index} #{pane_title}",
	).CombinedOutput()
	if err != nil {
		// If the session isn't up, there's nothing to clear.
		if isMissingTmuxSession(string(out)) {
			return nil, nil
		}
		return nil, fmt.Errorf("list panes: %w", err)
	}
	return parseAgentPaneIndices(string(out)), nil
}

// HasLiveTmuxSession returns true if a wuphf-team tmux session is running.
func HasLiveTmuxSession() bool {
	err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "has-session", "-t", SessionName).Run()
	return err == nil
}

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

func shouldPrimeClaudePane(content string) bool {
	normalized := strings.ToLower(content)
	return strings.Contains(normalized, "trust this folder") ||
		strings.Contains(normalized, "security guide") ||
		strings.Contains(normalized, "enter to confirm") ||
		strings.Contains(normalized, "claude in chrome")
}

func (l *Launcher) spawnVisibleAgents() ([]string, error) {
	if l.isOneOnOne() {
		slug := l.oneOnOneAgent()
		firstCmd, err := l.claudeCommand(slug, l.buildPrompt(slug))
		if err != nil {
			return nil, err
		}
		out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "split-window", "-h",
			"-t", l.sessionName+":team",
			"-p", "65",
			"-c", l.cwd,
			firstCmd,
		).CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail == "" {
				return nil, fmt.Errorf("spawn one-on-one agent: %w", err)
			}
			return nil, fmt.Errorf("spawn one-on-one agent: %w (tmux: %s)", err, detail)
		}
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-layout",
			"-t", l.sessionName+":team",
			"main-vertical",
		).Run()
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
			"-t", l.sessionName+":team.0",
			"-T", "📢 direct",
		).Run()
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
			"-t", fmt.Sprintf("%s:team.1", l.sessionName),
			"-T", fmt.Sprintf("🤖 %s (@%s)", l.getAgentName(slug), slug),
		).Run()
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-window",
			"-t", l.sessionName+":team",
		).Run()
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
			"-t", l.sessionName+":team.0",
		).Run()
		return []string{slug}, nil
	}

	// Layout: channel (left 35%) | agents in 2-column grid (right 65%)
	//
	// ┌─ channel ──┬─ CEO ───┬─ PM ────┐
	// │            │         │         │
	// │            ├─ FE ────┼─ BE ────┤
	// │            │         │         │
	// └────────────┴─────────┴─────────┘

	visible := l.visibleOfficeMembers()

	// First agent: split right from channel (horizontal split)
	if len(visible) == 0 {
		return nil, nil
	}
	firstCmd, err := l.claudeCommand(visible[0].Slug, l.buildPrompt(visible[0].Slug))
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "split-window", "-h",
		"-t", l.sessionName+":team",
		"-p", "65",
		"-c", l.cwd,
		firstCmd,
	).CombinedOutput()
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
		agentCmd, err := l.claudeCommand(visible[i].Slug, l.buildPrompt(visible[i].Slug))
		if err != nil {
			l.recordPaneSpawnFailure(visible[i].Slug, fmt.Sprintf("claudeCommand: %v", err))
			continue
		}
		out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "split-window",
			"-t", l.sessionName+":team.1",
			"-c", l.cwd,
			agentCmd,
		).CombinedOutput()
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
			l.recordPaneSpawnFailure(visible[i].Slug, reason)
		}
	}

	// Apply tiled layout to agent panes, but keep channel (pane 0) as main-vertical
	// Use main-vertical first to keep channel on the left
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-layout",
		"-t", l.sessionName+":team",
		"main-vertical",
	).Run()

	// Now set pane titles
	var visibleSlugs []string
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
		"-T", "📢 channel",
	).Run()
	for i, a := range visible {
		paneIdx := i + 1 // pane 0 is channel
		name := l.getAgentName(a.Slug)
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
			"-t", fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx),
			"-T", fmt.Sprintf("🤖 %s (@%s)", name, a.Slug),
		).Run()
		visibleSlugs = append(visibleSlugs, a.Slug)
	}

	// Focus channel pane
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-window",
		"-t", l.sessionName+":team",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
	).Run()

	return visibleSlugs, nil
}

func (l *Launcher) spawnOverflowAgents() {
	for _, member := range l.overflowOfficeMembers() {
		// Codex/Opencode-bound agents use the headless one-shot pipeline; they
		// don't need a claude pane. Creating one would launch `claude` with
		// the wrong model and quota semantics.
		if l.memberUsesHeadlessOneShotRuntime(member.Slug) {
			continue
		}
		agentCmd, err := l.claudeCommand(member.Slug, l.buildPrompt(member.Slug))
		if err != nil {
			fmt.Fprintf(os.Stderr, "spawn overflow agent %s: %v\n", member.Slug, err)
			l.recordPaneSpawnFailure(member.Slug, fmt.Sprintf("claudeCommand: %v", err))
			continue
		}
		windowName := overflowWindowName(member.Slug)
		out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "new-window", "-d",
			"-t", l.sessionName,
			"-n", windowName,
			"-c", l.cwd,
			agentCmd,
		).CombinedOutput()
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
			l.recordPaneSpawnFailure(member.Slug, reason)
		}
	}
}

// detectDeadPanesAfterSpawn waits briefly for fresh panes to either settle
// into claude or die on launch. Dead panes are marked failed so subsequent
// notifications fall back to the headless path instead of polling ghost panes.
func (l *Launcher) detectDeadPanesAfterSpawn(members []officeMember) {
	if l == nil || l.sessionName == "" {
		return
	}
	time.Sleep(1500 * time.Millisecond)
	targets := l.agentPaneTargets()
	for _, m := range members {
		target, ok := targets[m.Slug]
		if !ok || target.PaneTarget == "" {
			continue
		}
		out, err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "display-message",
			"-t", target.PaneTarget,
			"-p", "#{pane_dead}",
		).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "1" {
			continue
		}
		history, _ := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "capture-pane",
			"-t", target.PaneTarget,
			"-p", "-J", "-S", "-200",
		).CombinedOutput()
		snippet := strings.TrimSpace(string(history))
		if len(snippet) > 400 {
			snippet = snippet[:400] + "..."
		}
		fmt.Fprintf(os.Stderr,
			"  Agents:  pane for %s (%s) died on launch; falling back to headless. Last output: %q\n",
			m.Slug, target.PaneTarget, snippet,
		)
		l.recordPaneSpawnFailure(m.Slug, "pane died on launch; last output: "+snippet)
		if l.broker != nil {
			l.broker.PostSystemMessage("general",
				fmt.Sprintf("Agent @%s did not start cleanly; running in headless fallback. Check the launcher log for details.", m.Slug),
				"runtime",
			)
		}
	}
}

// recordPaneSpawnFailure marks a slug so agentPaneTargets() omits it and the
// pane-capture loops never try to read from a non-existent target. The agent
// still receives messages via the headless dispatch fallback.
func (l *Launcher) recordPaneSpawnFailure(slug, reason string) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return
	}
	if l.failedPaneSlugs == nil {
		l.failedPaneSlugs = make(map[string]string)
	}
	l.failedPaneSlugs[slug] = reason
}

// trySpawnWebAgentPanes attempts to create a detached tmux session with one
// interactive `claude` pane per agent so message dispatch can type into a live
// session. This is the internal fallback primitive for web and TUI modes —
// the default is headless `claude --print` per turn, which Anthropic
// re-sanctioned in the 2026-04 OpenClaw policy note and runs on the normal
// subscription quota without a separate extra-usage charge. Nothing in the
// startup path calls this today; it is reachable for a runtime-promotion
// fallback (e.g. repeated headless failures) without needing to be wired in
// advance.
//
// On success, l.paneBackedAgents is set to true and dispatch routes through
// sendNotificationToPane. On any failure (tmux missing, session create failure,
// spawn error) the method logs the tradeoff, posts a system message to
// #general, and leaves paneBackedAgents false so the default headless path
// continues to work.
func (l *Launcher) trySpawnWebAgentPanes() {
	if l.broker == nil {
		return
	}
	// Non-pane runtimes (Codex, future Ollama/vLLM/exo/openai-compat) use the
	// headless pipeline; panes don't apply.
	if !l.usesPaneRuntime() {
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		l.reportPaneFallback(false, "tmux not found on PATH", err)
		return
	}

	// Remove any stale session from a previous run. Ignore errors — the common
	// case is "no such session".
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	// Create a detached session with a placeholder pane 0. Agent panes are
	// attached as splits afterward so agentPaneTargets() (which starts at
	// team.1) maps correctly.
	placeholderCmd := "sh -c 'while :; do sleep 3600; done'"
	if err := exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "new-session", "-d",
		"-s", l.sessionName,
		"-n", "team",
		"-c", l.cwd,
		placeholderCmd,
	).Run(); err != nil {
		l.reportPaneFallback(true, "tmux new-session failed", err)
		return
	}
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"mouse", "off",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"status", "off",
	).Run()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "set-window-option", "-t", l.sessionName+":team",
		"remain-on-exit", "on",
	).Run()

	if _, err := l.spawnVisibleAgents(); err != nil {
		_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()
		l.reportPaneFallback(true, "spawn visible agents failed", err)
		return
	}
	l.spawnOverflowAgents()

	l.paneBackedAgents = true
	go l.detectDeadPanesAfterSpawn(append(l.visibleOfficeMembers(), l.overflowOfficeMembers()...))
	fmt.Printf("  Agents:  interactive Claude panes in tmux session %q (pane-backed fallback active)\n", l.sessionName)
}

// paneFallbackMessages renders the two user-facing messages for a pane-spawn
// fallback (stderr banner + broker #general post). Headless is the normal
// default now, so the fallback message is neutral — it only fires when
// something in the runtime promoted us to panes and the spawn failed.
// Remediation advice depends on whether tmux is installed:
//
//   - tmuxInstalled=false → install tmux if you want panes; otherwise headless
//     is a fine default and runs on your normal subscription.
//   - tmuxInstalled=true  → tmux rejected the command; ask the user to file a
//     bug. Headless continues to work.
//
// Pure function so it can be unit-tested without touching os.Stderr or the
// broker. Keep in sync with reportPaneFallback below.
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

// reportPaneFallback logs a pane-spawn failure and surfaces the billing
// tradeoff to the web user via a #general system message. Pass
// tmuxInstalled=false only when the failure was "tmux not on PATH"; any
// other failure implies tmux IS installed and rejected our command, which
// needs different remediation advice.
func (l *Launcher) reportPaneFallback(tmuxInstalled bool, summary string, err error) {
	detail := summary
	if err != nil {
		detail = fmt.Sprintf("%s: %v", summary, err)
	}
	stderrMsg, brokerMsg := paneFallbackMessages(tmuxInstalled, detail)
	fmt.Fprint(os.Stderr, stderrMsg)
	if l.broker != nil {
		l.broker.PostSystemMessage("general", brokerMsg, "runtime")
	}
}

// buildPrompt generates the system prompt for an agent. The body lives on
// promptBuilder (see prompt_builder.go) so it can be tested without a
// Launcher; this wrapper assembles the snapshot accessors from launcher
// state and delegates.
func (l *Launcher) buildPrompt(slug string) string {
	return l.newPromptBuilder().Build(slug)
}

// newPromptBuilder captures the launcher state the prompt depends on into
// a promptBuilder. Built fresh per call so each prompt sees the current
// member roster and active policies; the construction itself is cheap
// (closures + a couple of map lookups).
func (l *Launcher) newPromptBuilder() *promptBuilder {
	memoryBackend := config.ResolveMemoryBackend("")
	noNex := config.ResolveNoNex() || config.ResolveAPIKey("") == ""
	return &promptBuilder{
		isOneOnOne:  l.isOneOnOne,
		isFocusMode: l.isFocusModeEnabled,
		packName:    l.PackName,
		leadSlug:    l.officeLeadSlug,
		members:     l.officeMembersSnapshot,
		policies: func() []officePolicy {
			if l == nil || l.broker == nil {
				return nil
			}
			policies := l.broker.ListPolicies()
			// Sort so the policies section is deterministic and
			// cache-friendly across turns.
			sort.Slice(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
			return policies
		},
		nameFor:        l.getAgentName,
		markdownMemory: memoryBackend == config.MemoryBackendMarkdown,
		nexDisabled:    noNex,
	}
}

// claudeCommand builds the shell command string for spawning a claude session.
// Sets WUPHF_AGENT_SLUG so the MCP knows which agent this session serves.
// claudeCommand returns the shell command that launches an interactive
// `claude` session for the given agent. The command is passed as a single
// argument to tmux split-window; if it grows past tmux's internal
// command-parse buffer, tmux rejects it with "command too long" before the
// shell ever runs. Keep the command bounded — put the bulky system prompt in
// a file and pass --append-system-prompt-file <path> instead of inlining.
//
// Returns an error if the per-agent temp files (MCP config or prompt) cannot
// be written; callers should fall back to the headless path so agents do not
// silently launch with a missing system prompt.
func (l *Launcher) claudeCommand(slug, systemPrompt string) (string, error) {
	agentMCP, err := l.ensureAgentMCPConfig(slug)
	if err != nil {
		if l.mcpConfig == "" {
			return "", fmt.Errorf("claudeCommand(%s): write agent MCP config: %w", slug, err)
		}
		agentMCP = l.mcpConfig
	}
	mcpConfig := strings.ReplaceAll(agentMCP, "'", "'\\''")

	promptPath, err := l.writeAgentPromptFile(slug, systemPrompt)
	if err != nil {
		return "", fmt.Errorf("claudeCommand(%s): write prompt file: %w", slug, err)
	}
	promptPathQuoted := strings.ReplaceAll(promptPath, "'", "'\\''")

	name := strings.ReplaceAll(l.getAgentName(slug), "'", "'\\''")
	permFlags := l.resolvePermissionFlags(slug)

	brokerToken := ""
	if l.broker != nil {
		brokerToken = l.broker.Token()
	}

	oneOnOneEnv := ""
	if l.isOneOnOne() {
		oneOnOneEnv = fmt.Sprintf("WUPHF_ONE_ON_ONE=1 WUPHF_ONE_ON_ONE_AGENT=%s ", l.oneOnOneAgent())
	}
	oneSecretEnv := ""
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		oneSecretEnv = "ONE_SECRET=" + shellQuote(secret) + " "
	}
	oneIdentityEnv := ""
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		oneIdentityEnv = "ONE_IDENTITY=" + shellQuote(identity) + " "
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			oneIdentityEnv += "ONE_IDENTITY_TYPE=" + shellQuote(identityType) + " "
		}
	}

	model := l.headlessClaudeModel(slug)

	return fmt.Sprintf(
		"%s%s%sWUPHF_AGENT_SLUG=%s WUPHF_BROKER_TOKEN=%s WUPHF_BROKER_BASE_URL=%s WUPHF_NO_NEX=%t ANTHROPIC_PROMPT_CACHING=1 CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/json OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=%s/v1/logs OTEL_EXPORTER_OTLP_HEADERS='Authorization=Bearer %s' OTEL_RESOURCE_ATTRIBUTES='agent.slug=%s,wuphf.channel=office' claude --model %s %s --append-system-prompt-file '%s' --mcp-config '%s' --strict-mcp-config -n '%s'",
		oneOnOneEnv,
		oneSecretEnv,
		oneIdentityEnv,
		slug,
		brokerToken,
		l.BrokerBaseURL(),
		config.ResolveNoNex(),
		l.BrokerBaseURL(),
		brokerToken,
		slug,
		model,
		permFlags,
		promptPathQuoted,
		mcpConfig,
		name,
	), nil
}

// writeAgentPromptFile persists the per-agent system prompt to a stable
// per-slug temp file so it can be passed to `claude --append-system-prompt-file`
// without bloating the tmux command.
//
// File naming mirrors ensureAgentMCPConfig (wuphf-mcp-<slug>.json) so both
// artifacts are easy to clean up together. Perms are 0o600 because the prompt
// can contain team-internal instructions and tool lists.
func (l *Launcher) writeAgentPromptFile(slug, prompt string) (string, error) {
	path := filepath.Join(os.TempDir(), "wuphf-prompt-"+slug+".txt")
	if err := os.WriteFile(path, []byte(prompt), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// cleanupAgentTempFiles removes the per-agent MCP config + system prompt
// temp files for every known office member. Safe to call multiple times and
// idempotent — missing files are ignored. Called from Shutdown so the broker
// token + prompt content do not linger in $TMPDIR after the session ends.
func (l *Launcher) cleanupAgentTempFiles() {
	tmp := os.TempDir()
	for _, m := range l.officeMembersSnapshot() {
		for _, path := range []string{
			filepath.Join(tmp, "wuphf-mcp-"+m.Slug+".json"),
			filepath.Join(tmp, "wuphf-prompt-"+m.Slug+".txt"),
		} {
			_ = os.Remove(path)
		}
	}
}

// resolvePermissionFlags returns the Claude Code permission flags for an agent.
// All agents run in bypass mode by default — the team is autonomous.
func (l *Launcher) resolvePermissionFlags(slug string) string {
	return "--permission-mode bypassPermissions --dangerously-skip-permissions"
}

// codingAgentSlugs lists agents that default to a minimal coding-focused MCP set.
// Task-level local_worktree isolation is driven by execution_mode, not this list.
var codingAgentSlugs = map[string]bool{
	"eng":       true,
	"fe":        true,
	"be":        true,
	"ai":        true,
	"qa":        true,
	"tech-lead": true,
}

// agentMCPServers returns the MCP server keys that a given agent should receive.
func agentMCPServers(slug string) []string {
	channel := strings.TrimSpace(os.Getenv("WUPHF_CHANNEL"))
	// DM mode: only wuphf-office (minimal tool set, no nex overhead)
	if strings.HasPrefix(channel, "dm-") {
		return []string{"wuphf-office"}
	}
	if codingAgentSlugs[slug] {
		return []string{"wuphf-office"}
	}
	return []string{"wuphf-office", "nex"}
}

// buildMCPServerMap constructs the full set of MCP server entries.
// This is the shared helper used by both ensureMCPConfig and ensureAgentMCPConfig.
func (l *Launcher) buildMCPServerMap() (map[string]any, error) {
	apiKey := config.ResolveAPIKey("")
	servers := map[string]any{}
	wuphfBinary, err := os.Executable()
	if err != nil {
		return nil, err
	}

	office := map[string]any{
		"command": wuphfBinary,
		"args":    []string{"mcp-team"},
	}
	servers["wuphf-office"] = office
	if oneSecret := strings.TrimSpace(config.ResolveOneSecret()); oneSecret != "" {
		office["env"] = map[string]string{
			"ONE_SECRET": oneSecret,
		}
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		env["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env["ONE_IDENTITY_TYPE"] = identityType
		}
		office["env"] = env
	}

	switch config.ResolveMemoryBackend("") {
	case config.MemoryBackendNex:
		if apiKey != "" {
			env, _ := office["env"].(map[string]string)
			if env == nil {
				env = map[string]string{}
			}
			env["WUPHF_API_KEY"] = apiKey
			env["NEX_API_KEY"] = apiKey
			office["env"] = env
		}
	case config.MemoryBackendGBrain:
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range gbrainMCPEnv() {
			env[key] = value
		}
		office["env"] = env
	}

	if memoryServer, err := resolvedMemoryMCPServer(); err != nil {
		return nil, err
	} else if memoryServer != nil && len(memoryServer.Env) > 0 {
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range memoryServer.Env {
			env[key] = value
		}
		office["env"] = env
	}

	if !config.ResolveNoNex() && apiKey != "" {
		if nexMCP, err := exec.LookPath("nex-mcp"); err == nil {
			servers["nex"] = map[string]any{
				"command": nexMCP,
				"env": map[string]string{
					"WUPHF_API_KEY": apiKey,
					"NEX_API_KEY":   apiKey,
				},
			}
		}
	}

	return servers, nil
}

func (l *Launcher) ensureMCPConfig() (string, error) {
	servers, err := l.buildMCPServerMap()
	if err != nil {
		return "", err
	}

	cfg := map[string]any{
		"mcpServers": servers,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	path := filepath.Join(os.TempDir(), "wuphf-team-mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ensureAgentMCPConfig writes a per-agent MCP config containing only the servers
// that agent needs. Returns the config file path.
func (l *Launcher) ensureAgentMCPConfig(slug string) (string, error) {
	allServers, err := l.buildMCPServerMap()
	if err != nil {
		return "", err
	}

	allowed := agentMCPServers(slug)
	filtered := make(map[string]any, len(allowed))
	for _, key := range allowed {
		if srv, ok := allServers[key]; ok {
			filtered[key] = srv
		}
	}

	cfg := map[string]any{
		"mcpServers": filtered,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	path := filepath.Join(os.TempDir(), "wuphf-mcp-"+slug+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// agentActiveTask returns the first in_progress task owned by the given agent slug.
// AllTasks() is used so agents working in non-general channels still get their
// worktree set up correctly.
func (l *Launcher) agentActiveTask(slug string) *teamTask {
	if l.broker == nil {
		return nil
	}
	tasks := l.broker.AllTasks()
	for i := range tasks {
		if tasks[i].Owner == slug && tasks[i].Status == "in_progress" {
			return &tasks[i]
		}
	}
	return nil
}

func (l *Launcher) officeMembersSnapshot() []officeMember {
	mergePackMembers := func(members []officeMember) []officeMember {
		if l == nil || l.pack == nil || len(l.pack.Agents) == 0 {
			return members
		}
		bySlug := make(map[string]struct{}, len(members))
		for _, member := range members {
			bySlug[member.Slug] = struct{}{}
		}
		for _, cfg := range l.pack.Agents {
			if _, ok := bySlug[cfg.Slug]; ok {
				continue
			}
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return members
	}
	if l.broker != nil {
		if members := l.broker.OfficeMembers(); len(members) > 0 {
			return mergePackMembers(members)
		}
	}
	path := defaultBrokerStatePath()
	data, err := os.ReadFile(path)
	if err == nil {
		var state brokerState
		if json.Unmarshal(data, &state) == nil && len(state.Members) > 0 {
			for i := range state.Members {
				applyOfficeMemberDefaults(&state.Members[i])
			}
			return state.Members
		}
	}
	if l.pack != nil && len(l.pack.Agents) > 0 {
		members := make([]officeMember, 0, len(l.pack.Agents))
		for _, cfg := range l.pack.Agents {
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return mergePackMembers(members)
	}
	if manifest, err := company.LoadRuntimeManifest(resolveRepoRoot(l.cwd)); err == nil && len(manifest.Members) > 0 {
		members := make([]officeMember, 0, len(manifest.Members))
		for _, cfg := range manifest.Members {
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Role,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.System,
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return mergePackMembers(members)
	}
	return mergePackMembers(defaultOfficeMembers())
}

// resetManifestToPack overwrites company.json with the members defined in the
// given legacy pack. Called when the user passes --pack explicitly so the flag
// remains authoritative over any previously saved company configuration.
func resetManifestToPack(pack *agent.PackDefinition) error {
	members := make([]company.MemberSpec, 0, len(pack.Agents))
	for _, cfg := range pack.Agents {
		members = append(members, company.MemberSpec{
			Slug:           cfg.Slug,
			Name:           cfg.Name,
			Role:           cfg.Name,
			Expertise:      append([]string(nil), cfg.Expertise...),
			Personality:    cfg.Personality,
			PermissionMode: cfg.PermissionMode,
			AllowedTools:   append([]string(nil), cfg.AllowedTools...),
			System:         cfg.Slug == pack.LeadSlug || cfg.Slug == "ceo",
		})
	}
	manifest := company.Manifest{
		Name:    pack.Name,
		Lead:    pack.LeadSlug,
		Members: members,
	}
	return company.SaveManifest(manifest)
}

func resetManifestToOperationBlueprint(repoRoot, blueprintID string) error {
	manifest := company.Manifest{
		BlueprintRefs: []company.BlueprintRef{{
			Kind:   "operation",
			ID:     blueprintID,
			Source: "launcher",
		}},
	}
	resolved, ok := company.MaterializeManifest(manifest, repoRoot)
	if !ok {
		return fmt.Errorf("materialize operation blueprint %q", blueprintID)
	}
	return company.SaveManifest(resolved)
}

func resolveRepoRoot(start string) string {
	start = strings.TrimSpace(start)
	if start == "" {
		start = "."
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		if _, err := os.Stat(filepath.Join(current, "templates")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}

func loadRunningSessionMode() (string, string) {
	token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN"))
	if token == "" {
		return SessionModeOffice, DefaultOneOnOneAgent
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerBaseURL()+"/session-mode", nil)
	if err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SessionModeOffice, DefaultOneOnOneAgent
	}

	var result struct {
		SessionMode   string `json:"session_mode"`
		OneOnOneAgent string `json:"one_on_one_agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	return NormalizeSessionMode(result.SessionMode), NormalizeOneOnOneAgent(result.OneOnOneAgent)
}

func (l *Launcher) isFocusModeEnabled() bool {
	if l != nil && l.broker != nil {
		return l.broker.FocusModeEnabled()
	}
	if l == nil {
		return false
	}
	return l.focusMode
}

func brokerBaseURL() string {
	return brokeraddr.ResolveBaseURL()
}

func (l *Launcher) BrokerBaseURL() string {
	if l != nil && l.broker != nil {
		if addr := strings.TrimSpace(l.broker.Addr()); addr != "" {
			return "http://" + addr
		}
	}
	return brokerBaseURL()
}

// officeMemberBySlug / officeLeadSlug / activeSessionMembers / getAgentName
// live on officeTargeter (PLAN.md §C2); thin wrappers keep current callers
// working without a rename sweep.
func (l *Launcher) officeMemberBySlug(slug string) officeMember {
	return l.targeter().MemberBySlug(slug)
}

func (l *Launcher) officeLeadSlug() string { return l.targeter().LeadSlug() }

func agentConfigFromMember(member officeMember) agent.AgentConfig {
	cfg := agent.AgentConfig{
		Slug:           member.Slug,
		Name:           member.Name,
		Expertise:      append([]string(nil), member.Expertise...),
		Personality:    member.Personality,
		PermissionMode: member.PermissionMode,
		AllowedTools:   append([]string(nil), member.AllowedTools...),
	}
	if cfg.Name == "" {
		cfg.Name = humanizeSlug(member.Slug)
	}
	if len(cfg.Expertise) == 0 {
		cfg.Expertise = inferOfficeExpertise(member.Slug, member.Role)
	}
	if cfg.Personality == "" {
		cfg.Personality = inferOfficePersonality(member.Slug, member.Role)
	}
	return cfg
}

func (l *Launcher) activeSessionMembers() []officeMember {
	return l.targeter().ActiveSessionMembers()
}

// PackName returns the display name of the pack.
func (l *Launcher) PackName() string {
	if l.isOneOnOne() {
		return "1:1 with " + l.getAgentName(l.oneOnOneAgent())
	}
	return "WUPHF Office"
}

// AgentCount returns the number of agents in the pack.
func (l *Launcher) AgentCount() int {
	if l.isOneOnOne() {
		return 1
	}
	return len(l.officeMembersSnapshot())
}

// filterEnv returns env with the given key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}

// getAgentName returns the display name for an agent slug. Delegates to
// the targeter (PLAN.md §C2).
func (l *Launcher) getAgentName(slug string) string { return l.targeter().NameFor(slug) }

// ═══════════════════════════════════════════════════════════════
// Web View Mode
// ═══════════════════════════════════════════════════════════════

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
	if err := writeOfficePIDFile(); err != nil {
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
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())
	l.resumeInFlightWork()

	// Stream tmux pane output to the web UI's per-agent stream so users see
	// live Claude TUI activity (thinking, tool calls, responses) during a
	// pane-backed turn. No-op when paneBackedAgents is false.
	l.startPaneCaptureLoops(l.headlessCtx)

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

// postEscalation writes a system message to #general when an agent is stuck
// or has blown its retry budget. The Slack-style UI renders this as a normal
// message so humans see it without needing to open a panel.
func (l *Launcher) postEscalation(slug, taskID string, reason agent.EscalationReason, detail string) {
	if l.broker == nil {
		return
	}
	who := strings.TrimSpace(slug)
	if who == "" {
		who = "an agent"
	}
	var body string
	switch reason {
	case agent.EscalationStuck:
		body = fmt.Sprintf("Heads up: %s looks stuck. Task %s — %s. Needs eyes.", who, taskID, detail)
	case agent.EscalationMaxRetries:
		body = fmt.Sprintf("Heads up: %s keeps erroring on task %s. Last error: %s. Needs eyes.", who, taskID, detail)
	default:
		body = fmt.Sprintf("Heads up: %s escalation on %s: %s", who, taskID, detail)
	}
	l.broker.PostSystemMessage("general", body, "escalation")
	_, _, _ = l.requestSelfHealing(slug, taskID, reason, detail)
}
