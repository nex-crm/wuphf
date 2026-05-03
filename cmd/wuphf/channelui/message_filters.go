package channelui

import "strings"

// IsHumanSender reports whether from is one of the broker's human sender forms.
func IsHumanSender(from string) bool {
	from = strings.TrimSpace(from)
	return from == "" || from == "you" || from == "human" || strings.HasPrefix(from, "human:")
}

// FilterInsightMessages returns the subset of messages that are
// automation / context-graph entries — kind "automation" or from the
// system "nex" sender. Used to populate the insights side panels.
func FilterInsightMessages(messages []BrokerMessage) []BrokerMessage {
	filtered := make([]BrokerMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Kind == "automation" || msg.From == "nex" {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// LatestHumanFacingMessage returns a pointer to the most recent
// human_*-kind message in messages, or nil if none exist. Walks newest
// to oldest so the first match wins.
func LatestHumanFacingMessage(messages []BrokerMessage) *BrokerMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(messages[i].Kind), "human_") {
			return &messages[i]
		}
	}
	return nil
}

// CountUniqueAgents counts distinct non-system / non-user senders in
// messages: "you" (the human), "nex" (automation), kind=="automation"
// rows, and any blank/whitespace-only senders are excluded from the
// tally so unset From values don't read as a phantom agent.
func CountUniqueAgents(messages []BrokerMessage) int {
	seen := make(map[string]bool)
	for _, m := range messages {
		if IsHumanSender(m.From) || m.From == "nex" || m.Kind == "automation" {
			continue
		}
		from := strings.TrimSpace(m.From)
		seen[from] = true
	}
	return len(seen)
}
