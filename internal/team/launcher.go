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
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/runtimebin"
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

	// headless is the per-launcher headless-worker pool (PLAN.md §C7).
	// All headless dispatch state — mutex, ctx/cancel, queues, workers,
	// active turns, deferred lead turn, stop channel, and worker
	// WaitGroup — is grouped here so the Launcher struct no longer owns
	// a third sub-mutex directly. Embedded by value (not pointer) so
	// zero-value &Launcher{} in tests still gets a usable pool with
	// sane lazy-allocated maps; PR #320's stop-channel goroutine-leak
	// fix is preserved via the same lazy-allocate-under-mu pattern.
	headless         headlessWorkerPool
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

	// targets owns the office-membership-shape and routing-decision logic
	// (PLAN.md §C2). Lazily constructed via targeter() so tests that build
	// &Launcher{} directly stay nil-safe. The launcher field stays the
	// authoritative source for sessionName / pack / failedPaneSlugs /
	// paneBackedAgents — the targeter holds pointers/callbacks back into
	// the launcher rather than copies.
	targets *officeTargeter

	// notify owns notification-context and work-packet construction
	// (PLAN.md §C3). Lazily constructed via notifyCtx() and shares state
	// with the launcher via callbacks (broker reads, headless queue peek).
	notify *notificationContextBuilder

	// schedulerWorker owns the watchdog scheduler goroutine
	// (PLAN.md §C4). Lazily constructed via scheduler(); Launch() calls
	// Start, Kill() calls Stop. clock is realClock in production.
	schedulerWorker *watchdogScheduler

	// dispatcher owns the per-slug pane-dispatch workers (PLAN.md §C6).
	// Lazily constructed via paneDispatch(); the dispatcher.sendFn
	// closure consults the package-global launcherSendNotificationToPaneOverride
	// seam on every call so existing tests keep working unchanged.
	dispatcher *paneDispatcher

	// paneLC owns the tmux pane lifecycle (PLAN.md §C5b). Lazily
	// constructed via panes(); the runner is resolved through the
	// tmuxRunnerOverride seam at construction time so tests injecting a
	// fakeTmuxRunner before Launch get their fake transparently. Today
	// the type covers read-only methods (HasLiveSession, ListTeamPanes,
	// ChannelPaneStatus, capture*); the spawn/clear methods migrate in
	// follow-up PRs.
	paneLC *paneLifecycle
}

// headlessWorkerPool groups the per-launcher headless-dispatch state
// (PLAN.md §C7). All fields are lowercase package-internal — the pool is
// never used outside `internal/team` and stays an embedded value on
// Launcher rather than its own pointer so zero-value &Launcher{} in
// tests gets a usable pool with sane lazy-allocated maps. PR #320's
// goroutine-leak fix relies on stopCh being lazily allocated under mu
// before any worker can read it; that contract is preserved here.
type headlessWorkerPool struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	workers      map[string]bool
	active       map[string]*headlessCodexActiveTurn
	queues       map[string][]headlessCodexTurn
	deferredLead *headlessCodexTurn
	stopCh       chan struct{}
	workerWg     sync.WaitGroup
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

	// --pack is authoritative: when explicitly provided, reset company.json to
	// match the pack so the broker doesn't silently load stale members.
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
		// Drop stale broker state so the new pack starts clean.
		_ = os.Remove(defaultBrokerStatePath())
	}
	sessionMode, oneOnOne := loadRunningSessionMode()

	return &Launcher{
		packSlug:           packSlug,
		pack:               pack,
		operationBlueprint: loadedBlueprint,
		blankSlateLaunch:   blankSlateLaunch,
		sessionName:        SessionName,
		cwd:                cwd,
		sessionMode:        sessionMode,
		oneOnOne:           oneOnOne,
		provider:           config.ResolveLLMProvider(""),
		headless: headlessWorkerPool{
			workers: make(map[string]bool),
			active:  make(map[string]*headlessCodexActiveTurn),
			queues:  make(map[string][]headlessCodexTurn),
		},
		notifyLastDelivered: make(map[string]time.Time),
	}, nil
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

const (
	agentNotifyCooldown      = 1 * time.Second
	agentNotifyCooldownAgent = 2 * time.Second
)

// scheduler returns the watchdog scheduler, lazily constructing it on
// first access. Constructed nil-safe so tests that build &Launcher{}
// directly never trip on a missing scheduler. Production wiring
// (clock=realClock, broker=l.broker) happens here.
func (l *Launcher) scheduler() *watchdogScheduler {
	if l == nil {
		return nil
	}
	if l.schedulerWorker != nil {
		return l.schedulerWorker
	}
	l.schedulerWorker = &watchdogScheduler{
		broker:      l.broker,
		clock:       realClock{},
		deliverTask: l.deliverTaskNotification,
	}
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
	if l.targets != nil {
		return l.targets
	}
	if l.failedPaneSlugs == nil {
		l.failedPaneSlugs = map[string]string{}
	}
	l.targets = &officeTargeter{
		sessionName:        l.sessionName,
		pack:               l.pack,
		cwd:                l.cwd,
		provider:           l.provider,
		paneBackedFlag:     &l.paneBackedAgents,
		failedPaneSlugs:    l.failedPaneSlugs,
		isOneOnOne:         l.isOneOnOne,
		oneOnOneSlug:       l.oneOnOneAgent,
		isChannelDM:        l.isChannelDMRaw,
		snapshotMembers:    l.officeMembersSnapshot,
		memberProviderKind: l.brokerMemberProviderKind,
	}
	return l.targets
}

// agentPaneSlugs / officeAgentOrder / visibleOfficeMembers /
// overflowOfficeMembers / paneEligibleOfficeMembers /
// resolvePaneTargetForSlug / agentPaneTargets / agentNotificationTargets /
// shouldUseHeadlessDispatchForSlug / shouldUseHeadlessDispatchForTarget /
// skipPaneForSlug — historically thin Launcher wrappers around the
// targeter (PLAN.md §C2). PLAN.md §6 sweep deleted them; in-package
// call sites now use l.targeter().<Method>() directly.

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

// usesPaneRuntime / requiresClaudeSessionReset /
// memberEffectiveProviderKind / memberUsesHeadlessOneShotRuntime
// live on officeTargeter (PLAN.md §C2). PLAN.md §6 sweep deleted the
// transitional wrappers; in-package callers use
// l.targeter().<Method>() directly. UsesTmuxRuntime stays because
// cmd/wuphf/main.go imports it.

// UsesTmuxRuntime reports whether agents run in tmux panes. Exported
// for cmd/wuphf/main.go and tests; thin delegator over the targeter.
func (l *Launcher) UsesTmuxRuntime() bool {
	return l.targeter().UsesPaneRuntime()
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

// killStaleBroker, the office-PID-file helpers, ResetBrokerState,
// ClearPersistedBrokerState, resetBrokerState, brokerBaseURL, and
// Launcher.BrokerBaseURL live in broker_lifecycle.go per PLAN.md §C8.

func containsSlug(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// notifyCtx returns the notification-context builder, lazily constructing
// it on first access (PLAN.md §C3). The builder shares state with Launcher
// via callbacks for broker reads and headless-queue peek; constructed
// fresh per call so each work packet sees current broker state.
func (l *Launcher) notifyCtx() *notificationContextBuilder {
	if l == nil {
		return nil
	}
	if l.notify != nil {
		return l.notify
	}
	l.notify = &notificationContextBuilder{
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
	return l.notify
}

// Notification-context methods on Launcher are thin wrappers; the bodies
// live in notification_context.go on notificationContextBuilder. Wrappers
// kept (rather than removed) so the ~50 in-package callers don't need a
// rename sweep in this PR — that's a follow-up.

// shouldUseHeadlessDispatch is a thin wrapper around the targeter; see
// officeTargeter.ShouldUseHeadless for semantics.
// shouldUseHeadlessDispatch wrapper deleted by PLAN.md §6 sweep —
// callers use l.targeter().ShouldUseHeadless() directly.

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

// paneDispatch returns the lazily-constructed dispatcher (PLAN.md §C6).
// Nil-safe: returns a fresh dispatcher even when l == nil so &Launcher{}
// fixtures stay nil-safe in tests. The sendFn closure consults the
// package-global launcherSendNotificationToPaneOverride seam on every
// call so existing tests keep working unchanged (PLAN.md trap §5.3).
func (l *Launcher) paneDispatch() *paneDispatcher {
	if l == nil {
		return &paneDispatcher{clock: realClock{}}
	}
	if l.dispatcher != nil {
		return l.dispatcher
	}
	l.dispatcher = &paneDispatcher{
		clock: realClock{},
		sendFn: func(paneTarget, notification string) {
			launcherSendNotificationToPane(l, paneTarget, notification)
		},
	}
	return l.dispatcher
}

// panes returns the per-launcher paneLifecycle (PLAN.md §C5b), lazily
// constructing it on first access. A nil receiver returns a default
// paneLifecycle bound to the package-level SessionName so the
// HasLiveTmuxSession free function can route through the same path
// without a Launcher.
//
// Spawn orchestration (PLAN.md §C5e) requires the full paneLifecycleDeps
// callbacks. Building those closures here means Launcher state (broker,
// failedPaneSlugs, paneBackedAgents flag, targeter delegates) flows
// into paneLifecycle without paneLifecycle reaching back into the
// Launcher type.
func (l *Launcher) panes() *paneLifecycle {
	if l == nil {
		return newPaneLifecycle(SessionName)
	}
	if l.paneLC != nil {
		return l.paneLC
	}
	name := l.sessionName
	if name == "" {
		name = SessionName
	}
	deps := paneLifecycleDeps{
		cwd:                              l.cwd,
		isOneOnOne:                       l.isOneOnOne,
		oneOnOneAgent:                    l.oneOnOneAgent,
		usesPaneRuntime:                  l.targeter().UsesPaneRuntime,
		visibleOfficeMembers:             l.targeter().VisibleMembers,
		overflowOfficeMembers:            l.targeter().OverflowMembers,
		agentPaneTargets:                 l.targeter().PaneTargets,
		memberUsesHeadlessOneShotRuntime: l.targeter().MemberUsesHeadlessOneShotRuntime,
		claudeCommand:                    l.claudeCommand,
		buildPrompt:                      l.buildPrompt,
		agentName:                        l.targeter().NameFor,
		recordFailure:                    l.recordPaneSpawnFailure,
		paneBackedFlag:                   &l.paneBackedAgents,
	}
	if l.broker != nil {
		// Capture the pointer at construction so the deps closure
		// remains stable even if `l.broker` is reassigned later.
		// Production never reassigns broker after Launch(), but tests
		// build &Launcher{} fixtures and want the captured pointer to
		// match the broker they wired.
		broker := l.broker
		deps.postSystemMessage = func(channel, body, kind string) {
			broker.PostSystemMessage(channel, body, kind)
		}
	}
	l.paneLC = newPaneLifecycleWithDeps(name, deps)
	return l.paneLC
}

// queuePaneNotification is a thin wrapper around paneDispatcher.Enqueue
// (PLAN.md §C6). Kept as a Launcher method so existing call sites and
// the pane_dispatch_queue_test.go safety net don't need a rename sweep
// in this PR.
func (l *Launcher) queuePaneNotification(slug, paneTarget, notification string) {
	l.paneDispatch().Enqueue(slug, paneTarget, notification)
}

// runPaneDispatchQueue is retained as a Launcher method for compatibility
// with any in-package goroutine spawn that might still reference the
// historical name. Internally it just invokes the dispatcher's runQueue.
func (l *Launcher) runPaneDispatchQueue(slug string) {
	l.paneDispatch().runQueue(slug)
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

// capturePaneTargetContent / capturePaneContent / listTeamPanes /
// channelPaneStatus delegate to paneLifecycle (PLAN.md §C5b). Thin
// wrappers keep current callers (broker_handlers, watchdog,
// captureDeadChannelPane, clearAgentPanes) working without a rename
// sweep in this PR.
func (l *Launcher) capturePaneTargetContent(target string) (string, error) {
	return l.panes().CapturePaneTargetContent(target)
}

func (l *Launcher) capturePaneContent(paneIdx int) (string, error) {
	return l.panes().CapturePaneContent(paneIdx)
}

// clearAgentPanes / clearOverflowAgentWindows delegate to paneLifecycle
// (PLAN.md §C5c).
func (l *Launcher) clearAgentPanes() error {
	return l.panes().ClearAgentPanes()
}

func (l *Launcher) clearOverflowAgentWindows() {
	l.panes().ClearOverflowAgentWindows()
}

func (l *Launcher) listTeamPanes() ([]int, error) {
	return l.panes().ListTeamPanes()
}

// HasLiveTmuxSession returns true if a wuphf-team tmux session is
// running. Routes through paneLifecycle (PLAN.md §C5b) so tests can
// drive it via setTmuxRunnerForTest without a real tmux server.
func HasLiveTmuxSession() bool {
	return newPaneLifecycle(SessionName).HasLiveSession()
}

// spawnVisibleAgents / spawnOverflowAgents / detectDeadPanesAfterSpawn /
// trySpawnWebAgentPanes / reportPaneFallback delegate to paneLifecycle
// (PLAN.md §C5e). The orchestration bodies moved onto the type via
// paneLifecycleDeps so `failedPaneSlugs` writes still flow into the
// same map the targeter reads (PLAN.md trap §1).
func (l *Launcher) spawnVisibleAgents() ([]string, error) {
	return l.panes().SpawnVisibleAgents()
}

func (l *Launcher) spawnOverflowAgents() {
	l.panes().SpawnOverflowAgents()
}

func (l *Launcher) detectDeadPanesAfterSpawn(members []officeMember) {
	l.panes().DetectDeadPanesAfterSpawn(members)
}

func (l *Launcher) trySpawnWebAgentPanes() {
	l.panes().TrySpawnWebAgentPanes()
}

func (l *Launcher) reportPaneFallback(tmuxInstalled bool, summary string, err error) {
	l.panes().ReportPaneFallback(tmuxInstalled, summary, err)
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

	name := strings.ReplaceAll(l.targeter().NameFor(slug), "'", "'\\''")
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

// officeLeadSlug wrapper deleted by PLAN.md §6 sweep — callers use
// l.targeter().LeadSlug() directly.

// getAgentName wrapper deleted by PLAN.md §6 sweep — callers use
// l.targeter().NameFor(slug) directly.

// Web-mode entry points (PreflightWeb, LaunchWeb, maybeOfferNex,
// waitForWebReady, stdinIsTTY, openBrowser) live in launcher_web.go per
// PLAN.md §C8.

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
