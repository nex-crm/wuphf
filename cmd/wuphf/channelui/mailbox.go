package channelui

import "strings"

// FilterMessagesForViewerScope returns messages whose viewer-scope
// matches scope ("inbox" / "outbox" / "agent"). Empty / unknown scope
// returns a copy of the input unchanged. The returned slice is fresh
// so callers may mutate without affecting messages.
func FilterMessagesForViewerScope(messages []BrokerMessage, viewerSlug, scope string) []BrokerMessage {
	scope = NormalizeMailboxScope(scope)
	if scope == "" {
		return append([]BrokerMessage(nil), messages...)
	}
	index := make(map[string]BrokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			index[id] = msg
		}
	}
	filtered := make([]BrokerMessage, 0, len(messages))
	for _, msg := range messages {
		if MailboxMessageMatchesViewerScope(msg, viewerSlug, scope, index) {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// NormalizeMailboxScope canonicalizes a mailbox-scope label to one of
// "inbox" / "outbox" / "agent", returning "" for anything else.
func NormalizeMailboxScope(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "inbox", "outbox", "agent":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

// MailboxMessageMatchesViewerScope dispatches to the per-scope
// predicate. messagesByID indexes messages by ID so the inbox
// predicate can walk ReplyTo chains. Unknown scopes match everything.
func MailboxMessageMatchesViewerScope(msg BrokerMessage, viewerSlug, scope string, messagesByID map[string]BrokerMessage) bool {
	switch NormalizeMailboxScope(scope) {
	case "inbox":
		return MailboxMessageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
	case "outbox":
		return MailboxMessageBelongsToViewerOutbox(msg, viewerSlug)
	case "agent":
		return MailboxMessageBelongsToViewerOutbox(msg, viewerSlug) || MailboxMessageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
	default:
		return true
	}
}

// MailboxMessageBelongsToViewerOutbox is true when msg.From equals
// viewerSlug. Empty viewerSlug never matches.
func MailboxMessageBelongsToViewerOutbox(msg BrokerMessage, viewerSlug string) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	return viewerSlug != "" && strings.TrimSpace(msg.From) == viewerSlug
}

// MailboxMessageBelongsToViewerInbox is true when msg is addressed to
// viewerSlug — either authored by a human ("you" / "human" / "ceo"),
// tagging the viewer (or "all"), or replying within a thread the
// viewer started. The viewer's own messages do not count as inbox.
func MailboxMessageBelongsToViewerInbox(msg BrokerMessage, viewerSlug string, messagesByID map[string]BrokerMessage) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	if viewerSlug == "" {
		return false
	}
	from := strings.TrimSpace(msg.From)
	switch from {
	case viewerSlug:
		return false
	case "you", "human", "ceo":
		return true
	}
	for _, tagged := range msg.Tagged {
		tagged = strings.TrimSpace(tagged)
		if tagged == viewerSlug || tagged == "all" {
			return true
		}
	}
	return MailboxMessageRepliesToViewerThread(msg, viewerSlug, messagesByID)
}

// MailboxMessageRepliesToViewerThread walks ReplyTo up the thread
// chain and returns true when any ancestor's From equals viewerSlug.
// The walk is cycle-safe; missing parents stop the walk.
func MailboxMessageRepliesToViewerThread(msg BrokerMessage, viewerSlug string, messagesByID map[string]BrokerMessage) bool {
	replyTo := strings.TrimSpace(msg.ReplyTo)
	if replyTo == "" || viewerSlug == "" {
		return false
	}
	seen := map[string]bool{}
	for replyTo != "" {
		if seen[replyTo] {
			return false
		}
		seen[replyTo] = true
		parent, ok := messagesByID[replyTo]
		if !ok {
			return false
		}
		if strings.TrimSpace(parent.From) == viewerSlug {
			return true
		}
		replyTo = strings.TrimSpace(parent.ReplyTo)
	}
	return false
}
