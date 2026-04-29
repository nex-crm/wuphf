package channelui

import "strings"

// ThreadRootMessageID walks up the ReplyTo chain from messageID and
// returns the root message's ID. Returns the input ID (trimmed) when
// the message is not in the slice. Returns the dangling ReplyTo target
// when an intermediate message has been pruned but the chain still
// points to it — callers can use that as a synthetic root.
func ThreadRootMessageID(messages []BrokerMessage, messageID string) string {
	current, ok := FindMessageByID(messages, messageID)
	if !ok {
		return strings.TrimSpace(messageID)
	}
	for strings.TrimSpace(current.ReplyTo) != "" {
		parent, ok := FindMessageByID(messages, current.ReplyTo)
		if !ok {
			return current.ReplyTo
		}
		current = parent
	}
	return current.ID
}

// HasThreadReplies reports whether any message in messages replies to
// id (i.e. has ReplyTo == id). Cheap linear scan; the caller should
// hoist the slice walk if testing many ids.
func HasThreadReplies(messages []BrokerMessage, id string) bool {
	for _, msg := range messages {
		if msg.ReplyTo == id {
			return true
		}
	}
	return false
}
