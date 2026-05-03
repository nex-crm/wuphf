package team

// notifier_targets.go owns notification target resolution
// (PLAN.md §C11): given a channel message or a task action, who
// should be woken (immediate vs delayed)? The biggest piece is
// notificationTargetsForMessage (200+ lines) which runs the
// CEO/specialist routing decision tree. Split out of launcher.go
// so the routing logic is reviewable separately from delivery.

import (
	"strings"
	"time"
)

// containsSlug reports whether the slug list contains want. Moved
// here from launcher.go (PLAN.md §C16) — the routing decision tree
// is the only in-package caller. teammcp/server.go has its own copy
// by the same name; the packages don't share helpers.
func containsSlug(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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

	targetMap := l.targeter().PaneTargets()
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

type notificationTarget struct {
	PaneTarget string
	Slug       string
}

func (l *Launcher) taskNotificationTargets(action officeActionLog, task teamTask) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.targeter().NotificationTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	lead := l.targeter().LeadSlug()
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
	if strings.TrimSpace(current.Owner) != "" && strings.TrimSpace(current.Owner) != targetSlug && targetSlug != l.targeter().LeadSlug() {
		return false
	}
	if strings.TrimSpace(current.Owner) == "" && targetSlug != l.targeter().LeadSlug() {
		return false
	}
	return true
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
					if !isHumanMessageSender(m.Slug) {
						return true, m.Slug
					}
				}
			}
		}
	}
	return false, ""
}

func (l *Launcher) notificationTargetsForMessage(msg channelMessage) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.targeter().NotificationTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	// DMs are isolated: only the target agent gets notified, never CEO or others.
	if ch := normalizeChannelSlug(msg.Channel); IsDMSlug(ch) {
		agentSlug := DMTargetAgent(ch)
		if !isHumanMessageSender(msg.From) && agentSlug == msg.From {
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
			if !isHumanMessageSender(msg.From) && agentSlug == msg.From {
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
	lead := l.targeter().LeadSlug()
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
		case isHumanMessageSender(msg.From) || msg.Kind == "automation" || msg.From == "nex":
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
	case isHumanMessageSender(msg.From) || msg.Kind == "automation" || msg.From == "nex":
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
