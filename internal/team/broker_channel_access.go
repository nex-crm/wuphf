package team

import "time"

// Channel access control: the security boundary that gates message
// publishing. canAccessChannelLocked is the policy decision; the
// supporting predicates (channelHasMemberLocked,
// channelMemberEnabledLocked, enabledChannelMembersLocked) are its
// data lookups. ensureTaskOwnerChannelMembershipLocked is the
// auto-promotion rule that keeps task owners on the channel they
// own work in.
//
// The reservedChannelSlugs invariant — that user-created channels
// cannot shadow trusted sender slugs ("system", "nex", "you",
// "human") — is co-located here on purpose: every entry in that
// map is also handled as a trusted bypass in canAccessChannelLocked.
// The two lists are coupled, and the channel-create handler in
// broker_office_channels.go is the third call site that must reject
// any new channel whose slug matches.

// reservedChannelSlugs are slug values that canAccessChannelLocked treats as
// universally trusted senders. Any user-created channel sharing one of these
// slugs would inherit that trust — every actor in the trust list could read
// every message in that channel without an explicit Members entry. The
// channel-create handler guards against this by rejecting create requests
// whose slug matches this set; keep the two lists in sync.
var reservedChannelSlugs = map[string]bool{
	"system": true,
	"nex":    true,
	"you":    true,
	"human":  true,
	"ceo":    true,
}

func (b *Broker) canAccessChannelLocked(slug, channel string) bool {
	slug = normalizeActorSlug(slug)
	channel = normalizeChannelSlug(channel)
	if b.sessionMode == SessionModeOneOnOne {
		if slug == "" || slug == "you" || slug == "human" {
			return true
		}
		return slug == b.oneOnOneAgent
	}
	// NOTE: any new entry added here MUST also be added to
	// reservedChannelSlugs above so the channel-create handler keeps the
	// invariant "no user channel can shadow a trusted sender slug".
	if slug == "" || slug == "you" || slug == "human" || slug == "nex" || slug == "system" {
		return true
	}
	if slug == "ceo" {
		return true
	}
	return b.channelHasMemberLocked(channel, slug)
}

func (b *Broker) channelHasMemberLocked(channel, slug string) bool {
	ch := b.findChannelLocked(channel)
	if ch == nil {
		// Fall back to channelStore for new-format channels (e.g. "eng__human")
		if b.channelStore != nil {
			return b.channelStore.IsMemberBySlug(channel, slug)
		}
		return false
	}
	for _, member := range ch.Members {
		if member == slug {
			return true
		}
	}
	return false
}

func (b *Broker) channelMemberEnabledLocked(channel, slug string) bool {
	if !b.channelHasMemberLocked(channel, slug) {
		return false
	}
	ch := b.findChannelLocked(channel)
	if ch == nil {
		return true
	}
	for _, disabled := range ch.Disabled {
		if disabled == slug {
			return false
		}
	}
	return true
}

func (b *Broker) enabledChannelMembersLocked(channel string, candidates []string) []string {
	var out []string
	for _, candidate := range candidates {
		if b.channelMemberEnabledLocked(channel, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// ensureTaskOwnerChannelMembershipLocked auto-promotes a task owner
// onto the channel where the task lives. The auto-promotion lives in
// the channel-access file because it shares the same invariant set:
// "every actor that posts/owns work in a channel must be a Member of
// that channel" (canAccessChannelLocked enforces it from the publish
// side; this helper restores it from the assignment side).
func (b *Broker) ensureTaskOwnerChannelMembershipLocked(channel, owner string) {
	channel = normalizeChannelSlug(channel)
	owner = normalizeChannelSlug(owner)
	if channel == "" || owner == "" {
		return
	}
	if b.findMemberLocked(owner) == nil {
		return
	}
	ch := b.findChannelLocked(channel)
	if ch == nil {
		return
	}
	if !containsString(ch.Members, owner) {
		ch.Members = uniqueSlugs(append(ch.Members, owner))
		ch.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if len(ch.Disabled) > 0 {
		filtered := ch.Disabled[:0]
		for _, disabled := range ch.Disabled {
			if disabled != owner {
				filtered = append(filtered, disabled)
			}
		}
		ch.Disabled = filtered
	}
}
