package channelui

import "strings"

// ResolveInitialOfficeApp normalizes a CLI-flag string into a known
// OfficeApp value. Empty / unknown / whitespace input falls back to
// OfficeAppMessages. The legacy alias "insights" maps to
// OfficeAppPolicies (which absorbed the old insights surface).
func ResolveInitialOfficeApp(name string) OfficeApp {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "insights" {
		return OfficeAppPolicies
	}
	switch OfficeApp(normalized) {
	case OfficeAppMessages, OfficeAppInbox, OfficeAppOutbox, OfficeAppRecovery, OfficeAppTasks, OfficeAppRequests, OfficeAppPolicies, OfficeAppCalendar, OfficeAppArtifacts:
		return OfficeApp(normalized)
	default:
		return OfficeAppMessages
	}
}
