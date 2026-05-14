package team

import "strings"

// Pub/sub fanout to broker subscribers. Owns:
//   - appendMessageLocked: convenience wrapper that persists +
//     publishes a channel message in one step
//   - publishMessageLocked / publishActionLocked / publishActivityLocked:
//     the three "always-on" channels that every broker subscriber
//     receives. Each is a non-blocking send (default branch on a
//     full channel drops the event) so a slow subscriber can't stall
//     the publisher.
//
// Other publish*Locked helpers live alongside their owners (e.g.
// publishOfficeChangeLocked next to office channel CRUD); these three
// are core enough to live in their own kin file.
//
// All entries require the caller to hold b.mu — the *Locked suffix
// is the contract.

// maxMessages is the rolling cap on in-memory channel messages. Oldest
// messages are dropped when the cap is exceeded. 500 is enough for the
// web UI's scroll-back while keeping the state file under ~1MB for this
// slice. Configurable via WUPHF_MAX_MESSAGES env var.
const defaultMaxMessages = 500

func (b *Broker) appendMessageLocked(msg channelMessage) channelMessage {
	msg = sanitizeChannelMessageSecrets(msg)
	// Tag the message with the sender's current in-flight task so
	// the agent-context builder can suppress pre-review chatter from
	// downstream consumers. Already-stamped messages (system posts
	// from broadcastDecisionLocked, persistence banners, etc.) keep
	// their explicit value. Human and system senders are never
	// auto-stamped because they don't have an owner-lane task.
	if strings.TrimSpace(msg.SourceTaskID) == "" &&
		!isHumanMessageSender(msg.From) &&
		msg.From != "system" {
		// Scope source-task stamping to the message's channel (and
		// thread, when available) so an agent owning multiple lanes
		// doesn't get a message in lane A stamped with task B. Falls
		// back to channel-only when ReplyTo is unset.
		if taskID := b.activeOwnerTaskIDLocked(msg.From, msg.Channel, msg.ReplyTo); taskID != "" {
			msg.SourceTaskID = taskID
		}
	}
	b.messages = append(b.messages, msg)
	cap := maxMessagesFromEnv()
	if len(b.messages) > cap {
		b.messages = append([]channelMessage(nil), b.messages[len(b.messages)-cap:]...)
	}
	b.publishMessageLocked(msg)
	// First-run nudge dismissal: track the very first human-authored message
	// so the office sidebar can drop the "→ tag @<agent> in #general" hint.
	// Once true the field stays true for the lifetime of the broker (and
	// across restarts, because the message log is persisted and rescanned
	// on bootstrap). System and agent messages do not flip the bit. Empty
	// From is rejected explicitly: isHumanMessageSender("") returns true for
	// historical reasons but a missing sender is not proof of a real human.
	if !b.humanHasPosted && strings.TrimSpace(msg.From) != "" && isHumanMessageSender(msg.From) {
		b.humanHasPosted = true
	}
	return msg
}

// activeOwnerTaskIDLocked returns the task ID for the most recently-
// updated task owned by `slug` that's in a pre-merge lifecycle state
// (running, review, decision, blocked_on_pr_merge, changes_requested)
// AND is scoped to the message context (channel + optional thread).
//
// Scoping rules:
//   - channel == "" matches any channel (free-conversation default).
//   - channel != "" restricts to tasks whose teamTask.Channel matches
//     (case-sensitive — channel names are canonical identifiers).
//   - replyTo != "" prefers tasks whose ThreadID matches; if no
//     thread-scoped match exists, falls back to channel-only matches
//     so an in-thread reply still gets stamped if the owner has any
//     active lane on that channel.
//
// Returns the empty string when the agent has no in-flight task in
// the requested scope. Used by appendMessageLocked to stamp
// source-task-ID on agent messages.
//
// Caller must hold b.mu.
func (b *Broker) activeOwnerTaskIDLocked(slug, channel, replyTo string) string {
	slug = normalizeActorSlug(slug)
	if b == nil || slug == "" {
		return ""
	}
	channel = strings.TrimSpace(channel)
	replyTo = strings.TrimSpace(replyTo)
	var (
		bestThread  *teamTask
		bestChannel *teamTask
	)
	for i := range b.tasks {
		t := &b.tasks[i]
		if !strings.EqualFold(strings.TrimSpace(t.Owner), slug) {
			continue
		}
		if !lifecycleStateIsPreMerge(t.LifecycleState) {
			continue
		}
		if channel != "" && strings.TrimSpace(t.Channel) != channel {
			continue
		}
		// Prefer a thread-affine task when ReplyTo is set on the
		// message; fall back to a channel-only match otherwise.
		if replyTo != "" && strings.TrimSpace(t.ThreadID) == replyTo {
			if bestThread == nil || t.UpdatedAt > bestThread.UpdatedAt {
				bestThread = t
			}
		}
		if bestChannel == nil || t.UpdatedAt > bestChannel.UpdatedAt {
			bestChannel = t
		}
	}
	if bestThread != nil {
		return bestThread.ID
	}
	if bestChannel != nil {
		return bestChannel.ID
	}
	return ""
}

// lifecycleStateIsPreMerge returns true when the state describes work
// that hasn't been canonically resolved yet. Messages posted under a
// pre-merge state are still subject to review and should be hidden from
// downstream agents that aren't authoritatively involved.
func lifecycleStateIsPreMerge(s LifecycleState) bool {
	switch s {
	case LifecycleStateRunning,
		LifecycleStateReview,
		LifecycleStateDecision,
		LifecycleStateBlockedOnPRMerge,
		LifecycleStateChangesRequested:
		return true
	}
	return false
}

func (b *Broker) publishMessageLocked(msg channelMessage) {
	for _, ch := range b.messageSubscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (b *Broker) publishActionLocked(action officeActionLog) {
	action = sanitizeOfficeActionLog(action)
	for _, ch := range b.actionSubscribers {
		select {
		case ch <- action:
		default:
		}
	}
}

func (b *Broker) publishActivityLocked(activity agentActivitySnapshot) {
	activity.Activity = redactSecretsInText(activity.Activity)
	activity.Detail = redactSecretsInText(activity.Detail)
	for _, ch := range b.activitySubscribers {
		select {
		case ch <- activity:
		default:
		}
	}
}
