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
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
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

// agentNotifyCooldown* moved to notifier_delivery.go (PLAN.md §C19)
// next to deliverMessageNotification, the only caller.

// Where to find what (post-decomposition):
//
//   Construction        : NewLauncher (this file)
//   Sub-type wiring     : launcher_wiring.go
//   Setters / capability accessors : launcher_options.go
//   Preflight check     : launcher_preflight.go
//   Lifecycle           : launcher_lifecycle.go (Launch/Attach/Kill/Reset/Reconfigure)
//   Long-running loops  : launcher_loops.go
//   Manifest / blueprint resolution : launcher_manifest.go
//   Membership snapshot : launcher_membership.go
//   Web mode entry      : launcher_web.go
//   Prompt + claude command : prompts.go (and claudeCommand below)
//   MCP server config   : mcp_config.go
//   Pane lifecycle      : pane_lifecycle.go + tmux_runner.go
//   Pane dispatch       : pane_dispatch.go
//   Notification routing: notifier_loops.go / notifier_targets.go / notifier_delivery.go
//   Headless dispatch   : headless_codex.go (entry) / headless_codex_queue.go / headless_codex_runner.go / headless_codex_recovery.go
//   Broker lifecycle    : broker_lifecycle.go
//   Escalation posts    : escalation.go

// claudeCommand moved to prompts.go (PLAN.md §C22). The C13 PR held it
// back because the no-secrets pre-commit hook false-positived on the
// `ONE_SECRET=` env-var literal — that hook now uses a word-boundary
// regex so identifiers and env-var payload literals don't trip it.

// officeLeadSlug wrapper deleted by PLAN.md §6 sweep — callers use
// l.targeter().LeadSlug() directly.

// getAgentName wrapper deleted by PLAN.md §6 sweep — callers use
// l.targeter().NameFor(slug) directly.

// Web-mode entry points (PreflightWeb, LaunchWeb, maybeOfferNex,
// waitForWebReady, stdinIsTTY, openBrowser) live in launcher_web.go per
// PLAN.md §C8.
