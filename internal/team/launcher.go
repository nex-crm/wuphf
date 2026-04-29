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
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
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

// headlessWorkerPool moved to headless_codex.go (PLAN.md §C16) so the
// type sits next to the queue methods that operate on its fields.
// Launcher embeds it by value via the headless field; the embed is
// declared on the Launcher struct above so zero-value &Launcher{}
// fixtures still get a usable pool with sane lazy-allocated maps.

// paneDispatchTurn moved to pane_dispatch.go (PLAN.md §C15) so the
// dispatcher type and its on-the-wire turn shape sit in the same
// file. Same for paneDispatchMinGap, paneDispatchCoalesceWindow,
// launcherSendNotificationToPaneFn, launcherSendNotificationToPaneOverride,
// and launcherSendNotificationToPane.

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

// processDue* / recordWatchdogLedger thin Launcher wrappers were
// deleted by the C15 sweep — call sites use l.scheduler().processOnce()
// / .processTaskJob(...) / .processRequestJob(...) /
// .processWorkflowJob(...) / .recordLedger(...) directly. PLAN.md §6
// "no compatibility shims".

// humanInterview.TitleOrDefault moved to broker.go (PLAN.md §C16)
// next to the type definition.

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

// usesPaneRuntime / requiresClaudeSessionReset /
// memberEffectiveProviderKind / memberUsesHeadlessOneShotRuntime
// live on officeTargeter (PLAN.md §C2). PLAN.md §6 sweep deleted the
// transitional wrappers; in-package callers use
// l.targeter().<Method>() directly. UsesTmuxRuntime stays because
// cmd/wuphf/main.go imports it.

// killStaleBroker, the office-PID-file helpers, ResetBrokerState,
// ClearPersistedBrokerState, resetBrokerState, brokerBaseURL, and
// Launcher.BrokerBaseURL live in broker_lifecycle.go per PLAN.md §C8.

// containsSlug moved to notifier_targets.go (PLAN.md §C16) — its only
// in-package caller is the notification routing decision tree.

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

// paneDispatchMinGap and paneDispatchCoalesceWindow live in
// pane_dispatch.go (PLAN.md §C15) — they're dispatcher knobs, not
// Launcher knobs.

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

// queuePaneNotification + runPaneDispatchQueue thin wrappers deleted by
// the C16 sweep — call sites use l.paneDispatch().Enqueue(...) and
// l.paneDispatch().runQueue(...) directly. PLAN.md §6 "no compatibility
// shims".
//
// sendNotificationToPane (the actual /clear + send-keys body) and the
// launcherSendNotificationToPane* seam live in pane_dispatch.go
// (PLAN.md §C15/§C16) so the dispatcher's send path is co-located.

// capturePaneTargetContent / capturePaneContent / listTeamPanes /
// channelPaneStatus delegate to paneLifecycle (PLAN.md §C5b). Thin
// pane-method thin wrappers (capturePaneTargetContent, capturePaneContent,
// listTeamPanes, clearAgentPanes, clearOverflowAgentWindows,
// channelPaneStatus, captureDeadChannelPane, spawnVisibleAgents,
// spawnOverflowAgents, detectDeadPanesAfterSpawn, trySpawnWebAgentPanes,
// reportPaneFallback) deleted by the C15 sweep — call sites use
// l.panes().<Method>() directly. PLAN.md §6 "no compatibility shims".

// HasLiveTmuxSession returns true if a wuphf-team tmux session is
// running. Routes through paneLifecycle (PLAN.md §C5b) so tests can
// drive it via setTmuxRunnerForTest without a real tmux server.
func HasLiveTmuxSession() bool {
	return newPaneLifecycle(SessionName).HasLiveSession()
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
