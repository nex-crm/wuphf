package channelui

import (
	"fmt"
	"strings"
)

// AppendUniqueMessages returns existing extended with the entries of
// incoming whose IDs are not already present, plus the count actually
// added. Empty / whitespace-only IDs are appended unconditionally — the
// dedup index is keyed on trimmed IDs only, so existing entries seed
// the index with the trimmed form and incoming poll results dedupe
// against that canonical key. Order is preserved: existing first, then
// new arrivals in their incoming order.
func AppendUniqueMessages(existing, incoming []BrokerMessage) ([]BrokerMessage, int) {
	if len(incoming) == 0 {
		return existing, 0
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	out := make([]BrokerMessage, 0, len(existing)+len(incoming))
	for _, msg := range existing {
		out = append(out, msg)
		if id := strings.TrimSpace(msg.ID); id != "" {
			seen[id] = struct{}{}
		}
	}
	added := 0
	for _, msg := range incoming {
		if id := strings.TrimSpace(msg.ID); id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, msg)
		added++
	}
	return out, added
}

// PopupActionIndex parses a numeric popup-action token (e.g. the "3"
// in a "popup_action_3" mouse target) and returns it. Returns
// (0, false) on a parse error or a negative value, so callers can
// safely fall through.
func PopupActionIndex(raw string) (int, bool) {
	var idx int
	if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil || idx < 0 {
		return 0, false
	}
	return idx, true
}

// FormatUSD renders a dollar cost with two decimal places (e.g.
// "$0.04", "$12.50"). Used in the usage strip / token meta lines.
func FormatUSD(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}
