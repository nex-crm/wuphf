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

func (l *Launcher) installBroker(b *Broker) {
	if l == nil {
		return
	}
	l.broker = b
	if b != nil && l.brokerConfigurator != nil {
		l.brokerConfigurator(b)
	}
}

// scheduler returns the watchdog scheduler, lazily constructing it on
// first access. Constructed nil-safe so tests that build &Launcher{}
// directly never trip on a missing scheduler. Production wiring
// (clock=realClock, broker=l.broker) happens here.
//
// PLAN.md §C25 staff-review fix: sync.Once guards lazy-init. Launch()
// spawns watchdogSchedulerLoop alongside other goroutines (notify*Loop,
// pollNexNotificationsLoop, headless dispatch enqueues calling
// updateSchedulerJob) that hit scheduler() concurrently. Without the
// Once, two goroutines can both observe nil and write competing
// pointers.
func (l *Launcher) scheduler() *watchdogScheduler {
	if l == nil {
		return nil
	}
	l.schedulerWorkerOnce.Do(func() {
		l.schedulerWorker = &watchdogScheduler{
			broker:      l.broker,
			clock:       realClock{},
			deliverTask: l.deliverTaskNotification,
		}
	})
	return l.schedulerWorker
}

// updateSchedulerJob is nil-safe on a nil receiver: scheduler() returns
// nil for nil-l fixtures (matches the rest of the wiring layer), so
// guard the call. Same shape as targeter()/notifyCtx() callers.
func (l *Launcher) updateSchedulerJob(slug, label string, interval time.Duration, nextRun time.Time, status string) {
	if s := l.scheduler(); s != nil {
		s.updateJob(slug, label, interval, nextRun, status)
	}
}

func (l *Launcher) watchdogSchedulerLoop() {
	if s := l.scheduler(); s != nil {
		s.Start(context.Background())
	}
}

// targeter returns the office-targeter, lazily constructing it on first
// access. PLAN.md trap §5.4: tests build &Launcher{} directly and rely on
// every sub-type being nil-safe. The targeter shares mutable state with
// the launcher via pointers/maps (paneBackedFlag, failedPaneSlugs) so
// later mutations on the launcher are visible to the targeter immediately.
// PLAN.md §C25 staff-review fix: sync.Once guards lazy-init against
// the goroutine fan-out from Launch (multiple goroutines hit
// targeter() concurrently before the first ordered call finishes).
// failedPaneSlugs is read via per-slug callback (l.isFailedPaneSlug)
// so each lookup acquires l.failedPaneMu's read-lock for the duration
// of the check. Returning the map handle would race a concurrent
// recordPaneSpawnFailure write running from
// detectDeadPanesAfterSpawn's goroutine. The callback also keeps the
// reconfigure boundary observable to the targeter — clear() under
// the same mutex preserves the underlying map handle so the targeter
// always sees current launcher state.
func (l *Launcher) targeter() *officeTargeter {
	if l == nil {
		return nil
	}
	l.targetsOnce.Do(func() {
		l.targets = &officeTargeter{
			sessionName:        l.sessionName,
			pack:               l.pack,
			cwd:                l.cwd,
			provider:           l.provider,
			paneBackedFlag:     &l.paneBackedAgents,
			failedPaneSlugs:    l.isFailedPaneSlug,
			isOneOnOne:         l.isOneOnOne,
			oneOnOneSlug:       l.oneOnOneAgent,
			isChannelDM:        l.isChannelDMRaw,
			snapshotMembers:    l.officeMembersSnapshot,
			memberProviderKind: l.brokerMemberProviderKind,
		}
	})
	return l.targets
}

// notifyCtx returns the notification-context builder, lazily constructing
// it on first access (PLAN.md §C3). The builder is cached after first
// call; freshness comes from its inner callbacks re-resolving through
// l.broker on each invocation, not from rebuilding the struct.
//
// PLAN.md §C25 staff-review fix: sync.Once guards lazy-init from
// concurrent first-callers spawned by Launch.
func (l *Launcher) notifyCtx() *notificationContextBuilder {
	if l == nil {
		return nil
	}
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

// paneDispatch returns the lazily-constructed dispatcher (PLAN.md §C6).
// Nil-safe: returns a fresh dispatcher even when l == nil so &Launcher{}
// fixtures stay nil-safe in tests. The sendFn closure consults the
// package-global launcherSendNotificationToPaneOverride seam on every
// call so existing tests keep working unchanged (PLAN.md trap §5.3).
//
// PLAN.md §C25 staff-review fix: sync.Once guards lazy-init from
// concurrent first-callers spawned by Launch.
func (l *Launcher) paneDispatch() *paneDispatcher {
	if l == nil {
		return &paneDispatcher{clock: realClock{}}
	}
	l.dispatcherOnce.Do(func() {
		l.dispatcher = &paneDispatcher{
			clock: realClock{},
			sendFn: func(paneTarget, notification string) error {
				return launcherSendNotificationToPane(l, paneTarget, notification)
			},
		}
	})
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
	l.paneLCOnce.Do(func() {
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
	})
	return l.paneLC
}
