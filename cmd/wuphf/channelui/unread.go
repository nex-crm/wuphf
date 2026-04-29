package channelui

import (
	"fmt"
	"strings"
)

// SummarizeUnreadMessages renders a short "N new from <names>" label
// for the away-strip, naming up to three distinct senders. With
// senders that resolve to "" (or duplicates) the unnamed count is
// preserved. Returns "" for an empty slice.
func SummarizeUnreadMessages(messages []BrokerMessage) string {
	if len(messages) == 0 {
		return ""
	}
	names := []string{}
	seen := map[string]bool{}
	for _, msg := range messages {
		if strings.TrimSpace(msg.From) == "" || seen[msg.From] {
			continue
		}
		seen[msg.From] = true
		names = append(names, DisplayName(msg.From))
		if len(names) == 3 {
			break
		}
	}
	switch len(names) {
	case 0:
		return fmt.Sprintf("%d new messages", len(messages))
	case 1:
		return fmt.Sprintf("%d new from %s", len(messages), names[0])
	case 2:
		return fmt.Sprintf("%d new from %s and %s", len(messages), names[0], names[1])
	default:
		return fmt.Sprintf("%d new from %s, %s, and %s", len(messages), names[0], names[1], names[2])
	}
}
