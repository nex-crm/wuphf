package team

// launcher_wiring.go owns the lazy sub-type accessors that wire
// Launcher state into each extracted sub-type (PLAN.md §C2-§C7,
// §C5b). Every accessor follows the same shape: nil-safe early
// return, one-shot construction stored on the launcher, callbacks
// captured by closure so the sub-type sees current Launcher state
// without holding a pointer to the Launcher itself. This file is
// the explicit "wiring layer" of the orchestrator — separate from
// construction (NewLauncher) and lifecycle (Launch/Kill).

import (
	"context"
	"time"

	"github.com/nex-crm/wuphf/internal/channel"
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

func (l *Launcher) updateSchedulerJob(slug, label string, interval time.Duration, nextRun time.Time, status string) {
	l.scheduler().updateJob(slug, label, interval, nextRun, status)
}

func (l *Launcher) watchdogSchedulerLoop() {
	l.scheduler().Start(context.Background())
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
