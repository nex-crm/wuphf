package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/channel"
)

// Direct-message slug helpers. The broker has two DM slug formats it
// must recognize:
//   - legacy "dm-<agent>" / "dm-human-<agent>"
//   - canonical "<a>__<b>" pair-sorted, owned by channel.DirectSlug
//
// Loading state from disk migrates legacy → canonical (see
// channel.MigrateDMSlugString in broker_persistence.go's loadState),
// but recognition has to handle both. The IsDMSlug + DMTargetAgent +
// canonicalDMTargetAgent trio makes the dual-format awareness explicit
// in one place.

func (ch *teamChannel) isDM() bool {
	return ch.Type == "dm" || IsDMSlug(ch.Slug)
}

// IsDMSlug checks whether a channel slug represents a direct message.
func IsDMSlug(slug string) bool {
	slug = normalizeChannelSlug(slug)
	return strings.HasPrefix(slug, "dm-") || canonicalDMTargetAgent(slug) != ""
}

// DMSlugFor returns the DM channel slug for a given agent.
func DMSlugFor(agentSlug string) string {
	agentSlug = normalizeActorSlug(agentSlug)
	if agentSlug == "" {
		return ""
	}
	return channel.DirectSlug("human", agentSlug)
}

// DMTargetAgent extracts the agent slug from a DM channel slug.
// Returns "" if the slug is not a DM.
func DMTargetAgent(slug string) string {
	slug = normalizeChannelSlug(slug)
	if strings.HasPrefix(slug, "dm-human-") {
		return strings.TrimPrefix(slug, "dm-human-")
	}
	if strings.HasPrefix(slug, "dm-") {
		return strings.TrimPrefix(slug, "dm-")
	}
	return canonicalDMTargetAgent(slug)
}

// DMPartner returns the non-human member slug of a 1:1 DM channel. Returns
// "" if the channel is not a DM, does not exist, or is a group DM. Used by
// surface bridges to resolve who the human is talking to when routing DM posts
// to the right agent without requiring an @mention.
func (b *Broker) DMPartner(channelSlug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked(normalizeChannelSlug(channelSlug))
	if ch == nil || !ch.isDM() {
		return ""
	}
	if len(ch.Members) != 2 {
		return ""
	}
	for _, m := range ch.Members {
		if !isHumanMessageSender(m) {
			return m
		}
	}
	return ""
}

func canonicalDMTargetAgent(slug string) string {
	parts := strings.Split(normalizeChannelSlug(slug), "__")
	if len(parts) != 2 {
		return ""
	}
	switch {
	case isHumanMessageSender(parts[0]):
		return parts[1]
	case isHumanMessageSender(parts[1]):
		return parts[0]
	default:
		return ""
	}
}
