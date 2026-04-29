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
	isHumanOrCEO := msg.From == "you" || msg.From == "human" || msg.From == "nex" || msg.From == l.targeter().LeadSlug()
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
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	notification := l.buildTaskExecutionPacket(target.Slug, action, task, content)
	if l.targeter().ShouldUseHeadlessForTarget(target) {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.queuePaneNotification(target.Slug, target.PaneTarget, notification)
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
	for workerSlug, queue := range l.headless.queues {
		if workerSlug == except {
			continue
		}
		if len(queue) > 0 {
			out[workerSlug] = struct{}{}
		}
	}
	for workerSlug, active := range l.headless.active {
		if workerSlug == except {
			continue
		}
		if active != nil {
			out[workerSlug] = struct{}{}
		}
	}
	return out
}

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

	if l.targeter().ShouldUseHeadlessForTarget(target) {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.queuePaneNotification(target.Slug, target.PaneTarget, notification)
}
