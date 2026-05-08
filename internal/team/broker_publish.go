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

func (b *Broker) appendMessageLocked(msg channelMessage) channelMessage {
	msg = sanitizeChannelMessageSecrets(msg)
	b.messages = append(b.messages, msg)
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
