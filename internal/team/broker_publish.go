package team

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
	activity.Detail = redactSecretsInText(activity.Detail)
	for _, ch := range b.activitySubscribers {
		select {
		case ch <- activity:
		default:
		}
	}
}
