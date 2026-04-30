package channelui

import (
	"sort"
	"strings"
)

// ThreadRootMessageID walks up the ReplyTo chain from messageID and
// returns the root message's ID. Returns the input ID (trimmed) when
// the message is not in the slice. Returns the dangling ReplyTo target
// when an intermediate message has been pruned but the chain still
// points to it — callers can use that as a synthetic root. Visited IDs
// are tracked so malformed cyclic data (A→B→A) terminates instead of
// hanging the caller; on cycle detection the current node is returned
// as the root.
func ThreadRootMessageID(messages []BrokerMessage, messageID string) string {
	current, ok := FindMessageByID(messages, messageID)
	if !ok {
		return strings.TrimSpace(messageID)
	}
	visited := map[string]bool{current.ID: true}
	for strings.TrimSpace(current.ReplyTo) != "" {
		if visited[current.ReplyTo] {
			return current.ID
		}
		parent, ok := FindMessageByID(messages, current.ReplyTo)
		if !ok {
			return current.ReplyTo
		}
		visited[parent.ID] = true
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
// root itself is not counted, including in cyclic graphs — back-edges
// to an already-visited node are skipped before count++ so a A→B→A
// cycle doesn't inflate B's descendant count by counting A through the
// back-edge. The visited set is seeded with rootID so the root can't
// be reached as a child of any descendant.
func CountThreadReplies(children map[string][]BrokerMessage, rootID string) int {
	visited := map[string]bool{rootID: true}
	var walk func(id string) int
	walk = func(id string) int {
		count := 0
		for _, child := range children[id] {
			if visited[child.ID] {
				continue
			}
			visited[child.ID] = true
			count++
			count += walk(child.ID)
		}
		return count
	}
	return walk(rootID)
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
	// Pure-cycle guard: if every message points at a present parent
	// (e.g. broker emits A→B→A), no node ever becomes a root and the
	// walker would have nothing to traverse. Promote the
	// chronologically-first message to a synthetic root so the
	// thread still renders instead of silently disappearing.
	if len(roots) == 0 && len(sorted) > 0 {
		roots = append(roots, sorted[0])
	}

	var out []ThreadedMessage
	visited := make(map[string]bool)
	var walk func(msg BrokerMessage, depth int)
	walk = func(msg BrokerMessage, depth int) {
		if visited[msg.ID] {
			return
		}
		visited[msg.ID] = true
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
// is intentionally excluded — only replies count. A visited set guards
// against cyclic adjacency so malformed broker data can't hang the
// walk.
func ThreadParticipants(children map[string][]BrokerMessage, rootID string) []string {
	seen := make(map[string]bool)
	visited := make(map[string]bool)
	var participants []string
	var walk func(id string)
	walk = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
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
