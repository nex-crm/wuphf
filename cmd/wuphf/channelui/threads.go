package channelui

import (
	"sort"
	"strings"
)

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

// CountThreadReplies returns the total number of descendants of rootID
// in the children adjacency map (built by flattenThreadMessages). The
// root itself is not counted.
func CountThreadReplies(children map[string][]BrokerMessage, rootID string) int {
	count := 0
	for _, child := range children[rootID] {
		count++
		count += CountThreadReplies(children, child.ID)
	}
	return count
}

// FlattenThreadMessages produces the office-feed thread layout: a
// timestamp-sorted list of messages where each entry is a
// ThreadedMessage with Depth, ParentLabel, and (when collapsed)
// HiddenReplies / ThreadParticipants populated. Threads default to
// expanded; expanded[msg.ID] == false collapses a thread root and
// hides its descendants from the output. Dangling ReplyTo targets
// (parent missing from messages) promote the orphan to a root.
func FlattenThreadMessages(messages []BrokerMessage, expanded map[string]bool) []ThreadedMessage {
	if len(messages) == 0 {
		return nil
	}

	sorted := make([]BrokerMessage, len(messages))
	copy(sorted, messages)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	byID := make(map[string]BrokerMessage, len(sorted))
	children := make(map[string][]BrokerMessage)
	var roots []BrokerMessage

	for _, msg := range sorted {
		byID[msg.ID] = msg
	}
	for _, msg := range sorted {
		if msg.ReplyTo != "" {
			if _, ok := byID[msg.ReplyTo]; ok {
				children[msg.ReplyTo] = append(children[msg.ReplyTo], msg)
				continue
			}
		}
		roots = append(roots, msg)
	}

	var out []ThreadedMessage
	var walk func(msg BrokerMessage, depth int)
	walk = func(msg BrokerMessage, depth int) {
		parentLabel := ""
		if msg.ReplyTo != "" {
			parentLabel = msg.ReplyTo
			if parent, ok := byID[msg.ReplyTo]; ok {
				parentLabel = "@" + parent.From
			}
		}
		tm := ThreadedMessage{
			Message:     msg,
			Depth:       depth,
			ParentLabel: parentLabel,
		}
		if len(children[msg.ID]) > 0 {
			isExpanded, explicit := expanded[msg.ID]
			if explicit && !isExpanded {
				tm.Collapsed = true
				tm.HiddenReplies = CountThreadReplies(children, msg.ID)
				tm.ThreadParticipants = ThreadParticipants(children, msg.ID)
			}
		}
		out = append(out, tm)
		if tm.Collapsed {
			return
		}
		for _, child := range children[msg.ID] {
			walk(child, depth+1)
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}
	return out
}

// ThreadParticipants returns the distinct display names of every
// descendant sender under rootID in walk-order (depth-first, children
// in slice order). Names are resolved via the package's office
// directory so a "ceo" slug becomes its display name. The root sender
// is intentionally excluded — only replies count.
func ThreadParticipants(children map[string][]BrokerMessage, rootID string) []string {
	seen := make(map[string]bool)
	var participants []string
	var walk func(id string)
	walk = func(id string) {
		for _, child := range children[id] {
			name := DisplayName(child.From)
			if !seen[name] {
				seen[name] = true
				participants = append(participants, name)
			}
			walk(child.ID)
		}
	}
	walk(rootID)
	return participants
}
