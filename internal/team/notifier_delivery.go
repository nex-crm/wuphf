package team

// notifier_delivery.go owns the actual notification delivery path
// (PLAN.md §C11): once notifier_targets.go has decided who should
// be notified, the methods here build the work packet, route it
// through the headless queue or pane dispatch, and post the
// follow-up channel update. Also hosts the notification-context
// builder thin-wrappers (buildNotificationContext et al.) that
// delegate to notifyCtx() (PLAN.md §C3).

import (
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/onboarding"
)

// Notification debounce cooldowns. Prevents agent-to-agent feedback
// loops where one agent's response triggers another agent which
// triggers a third, ad infinitum. Human/CEO messages get the shorter
// cooldown so the user-facing pace stays snappy; agent-originated
// messages get the longer cooldown to break loops at their source.
const (
	agentNotifyCooldown      = 1 * time.Second
	agentNotifyCooldownAgent = 2 * time.Second
)

// notifyDedupKey is the composite key the dedup map uses. Struct-keyed
// so a slug or sender containing the previous "\x00" separator can
// never collide; Go's map runtime hashes structs as cheaply as
// strings for this size.
type notifyDedupKey struct {
	slug    string
	sender  string
	channel string
}

func (l *Launcher) deliverMessageNotification(msg channelMessage) {
	// Phase 2 onboarding is fully deterministic — the broker emits CEO
	// cards from ceoDeterministicMessages and the user replies via the
	// structured-card POST handlers. The CEO agent must NOT fire an LLM
	// run in response, or the user sees a spurious "CEO is typing…"
	// stream and the deterministic flow stalls behind a Claude turn that
	// has nothing to add. (Spec: docs/specs/onboarding-into-office.md
	// "zero LLM tokens in Phase 2".) Gate on (CEO DM channel + onboarding
	// phase is deterministic) so other channels and the post-onboarding
	// CEO DM keep their normal headless behaviour.
	if isDeterministicPhase2CEODM(msg.Channel) {
		return
	}
	immediate, delayed := l.notificationTargetsForMessage(msg)

	// Debounce: use shorter cooldown for human/CEO messages, longer for agent-originated
	// to prevent agent-to-agent feedback loops (devil's advocate finding #3).
	//
	// The dedup key is (recipient slug, sender, channel) — recipient-
	// only would silently drop an unrelated message that arrives within
	// the cooldown window from a different sender or in a different
	// channel. Per-(recipient, sender, channel) keeps the loop-breaker
	// behaviour while letting genuinely unrelated traffic through.
	isHumanOrCEO := isHumanMessageSender(msg.From) || msg.From == "nex" || msg.From == l.targeter().LeadSlug()
	cooldown := agentNotifyCooldownAgent
	if isHumanOrCEO {
		cooldown = agentNotifyCooldown
	}
	now := time.Now()
	filtered := make([]notificationTarget, 0, len(immediate))
	channelKey := normalizeChannelSlug(msg.Channel)
	l.notifyMu.Lock()
	if l.notifyLastDelivered == nil {
		l.notifyLastDelivered = make(map[notifyDedupKey]time.Time)
	}
	// Opportunistic purge: drop entries older than 2× the longer
	// cooldown (still well past any legitimate dedup window) so the
	// map can't grow unbounded over a long-running session. The
	// (slug, sender, channel) key shape grows O(slugs × senders ×
	// channels) which is bounded but not small.
	purgeBefore := now.Add(-2 * agentNotifyCooldownAgent)
	for k, t := range l.notifyLastDelivered {
		if t.Before(purgeBefore) {
			delete(l.notifyLastDelivered, k)
		}
	}
	for _, t := range immediate {
		key := notifyDedupKey{slug: t.Slug, sender: msg.From, channel: channelKey}
		if last, ok := l.notifyLastDelivered[key]; ok && now.Sub(last) < cooldown {
			continue
		}
		l.notifyLastDelivered[key] = now
		filtered = append(filtered, t)
	}
	l.notifyMu.Unlock()
	immediate = filtered

	// Mark implicit public-channel routing targets as active so the UI can show
	// the ephemeral "X is thinking..." indicator. DMs suppress this signal.
	isDM, _ := l.isChannelDM(normalizeChannelSlug(msg.Channel))
	if l.broker != nil && len(immediate) > 0 && isHumanMessageSender(msg.From) && !l.isOneOnOne() && !isDM && len(msg.Tagged) == 0 {
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

// taskNotificationContent delegates to the notificationContextBuilder
// (PLAN.md §C3). See notification_context.go for the formatting body.
func (l *Launcher) taskNotificationContent(action officeActionLog, task teamTask) string {
	return l.notifyCtx().TaskNotificationContent(action, task)
}

func (l *Launcher) sendTaskUpdate(target notificationTarget, action officeActionLog, task teamTask, content string) {
	// Approval gate (Phase 4): refuse to dispatch execution work to agents
	// for tasks that are not in an executable lifecycle state. Only Running
	// and Approved tasks may trigger agent execution turns. Drafting, Intake,
	// Review, and ChangesRequested tasks are blocked here — agents may still
	// post comments via the comment endpoint, which does not go through this
	// path. ErrIssueNotApproved is the sentinel; log and drop (no retry).
	if task.LifecycleState != "" && !isExecutableTeamTaskStatus(task.LifecycleState) {
		return
	}
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	notification := l.buildTaskExecutionPacket(target.Slug, action, task, content)
	if l.targeter().ShouldUseHeadlessForTarget(target) {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.paneDispatch().Enqueue(target.Slug, target.PaneTarget, notification)
}

// activeHeadlessSlugs returns the slugs that have non-empty headless
// queues or active turns at the moment of the call. Locks headlessMu so
// the snapshot is consistent. The except parameter is the slug being
// notified — the lead must not list itself as "already active".
func (l *Launcher) activeHeadlessSlugs(except string) map[string]struct{} {
	if l == nil {
		return nil
	}
	l.headless.mu.Lock()
	defer l.headless.mu.Unlock()
	out := map[string]struct{}{}
	// An agent may now span several lanes; group by lane.slug so a slug counts
	// as active when ANY of its lanes has queued or in-flight work.
	for workerLane, queue := range l.headless.queues {
		if workerLane.slug == except {
			continue
		}
		if len(queue) > 0 {
			out[workerLane.slug] = struct{}{}
		}
	}
	for workerLane, active := range l.headless.active {
		if workerLane.slug == except {
			continue
		}
		if active != nil {
			out[workerLane.slug] = struct{}{}
		}
	}
	return out
}

func (l *Launcher) buildNotificationContext(recipientSlug, channelSlug, triggerMsgID, threadRootID string, limit int) string {
	return l.notifyCtx().NotificationContext(recipientSlug, channelSlug, triggerMsgID, threadRootID, limit)
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
	humanPrefix := ""
	fromHuman := isHumanMessageSender(msg.From)
	if fromHuman {
		// Front-load a directive so the model treats the human chat as a
		// preemption signal: absorb the message before resuming any prior
		// task, then decide whether to abandon, give a status update, or
		// queue the request for later. The same priority semantics are
		// enforced in the queue (FromHuman bypasses the hold/cap and forces
		// preemption) so the model and the dispatcher stay in agreement.
		humanPrefix = "[HUMAN-PRIORITY] A real person just messaged you. Stop, absorb this message before continuing any prior task, then decide which is appropriate: (a) abandon the prior task and address the human directly, (b) give a brief status update if they are asking what you're doing, or (c) acknowledge and queue their request for after the current task. Human messages take priority over agent-to-agent follow-ups.\n---\n"
	}
	if l.isOneOnOne() {
		notification = fmt.Sprintf(
			"%s[New from @%s]: %s\n%s Reply using team_broadcast with my_slug \"%s\" and channel \"%s\" reply_to_id \"%s\". Once you have posted the needed reply, STOP and wait for the next pushed notification.",
			humanPrefix, msg.From, truncate(msg.Content, 1000), l.responseInstructionForTarget(msg, target.Slug), target.Slug, channel, msg.ID,
		)
	} else {
		packet := l.buildMessageWorkPacket(msg, target.Slug)
		notification = fmt.Sprintf(
			"%s%s\n---\n[New from @%s]: %s\n%s This packet is your complete context — do NOT call team_poll or team_tasks. Just do the work and reply via team_broadcast with my_slug \"%s\", channel \"%s\", reply_to_id \"%s\". Once you have posted the needed update, STOP and wait for the next pushed notification.",
			humanPrefix, packet, msg.From, truncate(msg.Content, 1000), l.responseInstructionForTarget(msg, target.Slug), target.Slug, channel, msg.ID,
		)
	}

	if l.targeter().ShouldUseHeadlessForTarget(target) {
		prompt := headlessSandboxNote() + notification
		l.enqueueHeadlessCodexTurnRecord(target.Slug, headlessCodexTurn{
			Prompt:     prompt,
			Channel:    channel,
			TaskID:     headlessCodexTaskID(prompt),
			FromHuman:  fromHuman,
			EnqueuedAt: time.Now(),
		})
		return
	}
	l.paneDispatch().Enqueue(target.Slug, target.PaneTarget, notification)
}

// isDeterministicPhase2CEODM reports whether a message landed in the
// CEO DM during an onboarding phase where the broker drives the
// conversation via deterministic templates and no LLM should fire.
//
// In production the CEO DM lands at the canonical pair-sorted slug
// ("ceo__human"), not the reserved CEOOnboardingDMSlug constant — that
// constant is for the state record only. So we match on "DM whose
// target agent is CEO" instead of a literal slug comparison.
//
// Phase 2 covers greet → bridge (everything before the user opts into
// the first issue). draft/approve/kickoff and complete are NOT gated —
// those are the LLM-backed phases where the CEO agent is supposed to
// drive the chat.
//
// Errors loading the state file fall through to "not gated" so a
// missing or corrupt onboarded.json never silences a real post-
// onboarding CEO turn.
func isDeterministicPhase2CEODM(channel string) bool {
	ch := normalizeChannelSlug(channel)
	if ch == "" {
		return false
	}
	target := DMTargetAgent(ch)
	if target != "ceo" && ch != onboarding.CEOOnboardingDMSlug {
		return false
	}
	s, err := onboarding.Load()
	if err != nil || s == nil {
		return false
	}
	// Already onboarded → CEO LLM is fully active again.
	if s.Onboarded() {
		return false
	}
	switch s.Phase {
	case onboarding.PhaseGreet,
		onboarding.PhaseIdentity,
		onboarding.PhaseWebsite,
		onboarding.PhaseScan,
		onboarding.PhaseBlueprint,
		onboarding.PhaseTeam,
		onboarding.PhaseSeed,
		onboarding.PhaseBridge:
		return true
	}
	return false
}
