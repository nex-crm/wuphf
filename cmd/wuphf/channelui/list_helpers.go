package channelui

import "strings"

// ReverseSignals returns the most recent up-to-limit signals in
// newest-first order. limit <= 0 keeps all signals. The input slice is
// not mutated.
func ReverseSignals(signals []Signal, limit int) []Signal {
	if limit > 0 && len(signals) > limit {
		signals = signals[len(signals)-limit:]
	}
	out := append([]Signal(nil), signals...)
	reverseAny(out)
	return out
}

// ReverseDecisions returns the most recent up-to-limit decisions in
// newest-first order. limit <= 0 keeps all decisions. The input slice
// is not mutated.
func ReverseDecisions(decisions []Decision, limit int) []Decision {
	if limit > 0 && len(decisions) > limit {
		decisions = decisions[len(decisions)-limit:]
	}
	out := append([]Decision(nil), decisions...)
	reverseAny(out)
	return out
}

// ActiveWatchdogs filters out watchdogs whose Status is "resolved",
// preserving original order. Whitespace around Status is trimmed before
// the comparison so persisted records with stray spaces still resolve
// correctly.
func ActiveWatchdogs(alerts []Watchdog) []Watchdog {
	var out []Watchdog
	for _, alert := range alerts {
		if strings.TrimSpace(alert.Status) == "resolved" {
			continue
		}
		out = append(out, alert)
	}
	return out
}

// ReverseWatchdogs returns the most recent up-to-limit watchdogs in
// newest-first order. limit <= 0 keeps all alerts. The input slice is
// not mutated.
func ReverseWatchdogs(alerts []Watchdog, limit int) []Watchdog {
	if limit > 0 && len(alerts) > limit {
		alerts = alerts[len(alerts)-limit:]
	}
	out := append([]Watchdog(nil), alerts...)
	reverseAny(out)
	return out
}

// RecentExternalActions returns the most recent up-to-limit actions
// whose Kind starts with "external_" or equals "bridge_channel", in
// newest-first order. limit <= 0 keeps all matching actions.
func RecentExternalActions(actions []Action, limit int) []Action {
	var filtered []Action
	for _, action := range actions {
		kind := strings.TrimSpace(action.Kind)
		if !strings.HasPrefix(kind, "external_") && kind != "bridge_channel" {
			continue
		}
		filtered = append(filtered, action)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := append([]Action(nil), filtered...)
	reverseAny(out)
	return out
}

// AgentSlugForDisplay returns the slug of the member whose Name or
// resolved display-name matches the given name. Returns "" when no
// member matches.
func AgentSlugForDisplay(name string, members []Member) string {
	for _, member := range members {
		if member.Name == name || DisplayName(member.Slug) == name {
			return member.Slug
		}
	}
	return ""
}

// DisplaySignalKind returns the user-facing label for a signal's kind:
// "Human directive" for kind "directive" (case-insensitive), the
// trimmed kind otherwise. Empty kinds yield "".
func DisplaySignalKind(signal Signal) string {
	kind := strings.TrimSpace(signal.Kind)
	if kind == "" {
		return ""
	}
	if strings.EqualFold(kind, "directive") {
		return "Human directive"
	}
	return kind
}

// reverseAny reverses items in place. Generic so the typed Reverse*
// helpers above can share one implementation.
func reverseAny[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}
