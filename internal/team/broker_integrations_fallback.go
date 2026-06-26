package team

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
)

// broker_integrations_fallback.go owns the `fallback` manual-handoff decision.
// When the resolver classifies a mutating action as `fallback` (the platform
// has no Composio path, so it cannot be automated), it raises a blocking
// handoff card asking the human to complete the action manually (mark_done) or
// abandon it (skip). One CLI is product-removed, so manual handoff is the only
// fallback. Unlike connect there is no automated completion to fan out from —
// the human answers the card through the normal decision path, which runs the
// standard unblock cascade so the parked work moves on.

// fallbackRequestDedupeKey scopes a handoff card to a specific (platform,
// action) so retries of the same unsupported action collapse onto one card,
// while a genuinely different action type on the same platform raises its own.
func fallbackRequestDedupeKey(platform, actionID string) string {
	return "fallback:" + connectionRegistryKey(platform) + ":" + strings.ToLower(strings.TrimSpace(actionID))
}

// ensureFallbackRequest returns the active handoff card for (platform, action),
// creating one if none exists. Locks b.mu; callers must not already hold it.
func (b *Broker) ensureFallbackRequest(platform, actionID, channel, agent, name, logoURL, summary string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ensureFallbackRequestLocked(platform, actionID, channel, agent, name, logoURL, summary)
}

func (b *Broker) ensureFallbackRequestLocked(platform, actionID, channel, agent, name, logoURL, summary string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return ""
	}
	display := strings.TrimSpace(name)
	if display == "" {
		display = action.DisplayPlatformName(platform)
	}
	return b.ensureIntegrationDecisionLocked(integrationDecisionCard{
		Kind:      "fallback",
		DedupeKey: fallbackRequestDedupeKey(platform, actionID),
		Platform:  platform,
		Channel:   channel,
		Agent:     agent,
		Title:     "Handle " + display + " manually",
		Question:  fmt.Sprintf("%s can't be automated via Composio. Please complete this manually, then mark it done.", display),
		Context:   strings.TrimSpace(summary),
		LogoURL:   logoURL,
		AuditKind: "integration_fallback_requested",
	})
}
