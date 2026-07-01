package team

import "time"

// broker_indexes.go — channel and member lookup indexes. Extracted verbatim from
// broker.go to keep that core file under the file-size budget; behaviour is
// unchanged. These maintain O(1) slug lookups over b.channels / b.members with a
// length-check + rebuild-on-miss that stays correct across appends, removes, and
// same-length slice replacements (snapshot rollback / state load).

func (b *Broker) findChannelLocked(slug string) *teamChannel {
	slug = normalizeChannelSlug(slug)
	// Channel-per-task makes b.channels grow with the office, so the old
	// linear scan was O(channels) per call on hot paths (membership checks,
	// access control, startup reconciliation). Index by slug instead.
	if len(b.channelIndex) != len(b.channels) {
		b.rebuildChannelIndexLocked()
	}
	if i, ok := b.channelIndex[slug]; ok && i < len(b.channels) && b.channels[i].Slug == slug {
		return &b.channels[i]
	}
	// A miss may be genuine OR a stale index left by a same-length slice
	// replacement (snapshot rollback / state load) that the length check
	// can't detect. Rebuild once and retry so we never return a false
	// negative for a channel that actually exists. Hits never reach here, so
	// the hot lookup path stays O(1).
	b.rebuildChannelIndexLocked()
	if i, ok := b.channelIndex[slug]; ok && i < len(b.channels) && b.channels[i].Slug == slug {
		return &b.channels[i]
	}
	return nil
}

// rebuildChannelIndexLocked rebuilds channelIndex from b.channels. Callers must
// hold b.mu. findChannelLocked's length-check + rebuild-on-miss keep the map in
// sync with the slice across appends, removes, and same-length replacements.
func (b *Broker) rebuildChannelIndexLocked() {
	b.channelIndex = make(map[string]int, len(b.channels))
	for i := range b.channels {
		b.channelIndex[b.channels[i].Slug] = i
	}
}

// ensureDMConversationLocked returns the DM conversation for the given slug,
// creating it on the fly if it doesn't exist. Mirrors Slack's conversations.open.
// It delegates creation to channelStore so DM channels have proper types and members.
func (b *Broker) ensureDMConversationLocked(slug string) *teamChannel {
	if ch := b.findChannelLocked(slug); ch != nil {
		return ch
	}
	if !IsDMSlug(slug) {
		return nil
	}
	agentSlug := DMTargetAgent(slug)
	if agentSlug == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// Register in channelStore for proper type-based DM detection.
	if b.channelStore != nil {
		newSlug := DMSlugFor(agentSlug)
		if _, err := b.channelStore.GetOrCreateDirect("human", agentSlug); err == nil {
			// Update slug in broker to the new deterministic format if different.
			if newSlug != slug {
				slug = newSlug
			}
		}
	}
	b.channels = append(b.channels, teamChannel{
		Slug:        slug,
		Name:        slug,
		Type:        "dm",
		Description: "Direct messages with " + agentSlug,
		Members:     []string{"human", agentSlug},
		CreatedBy:   "wuphf",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	return &b.channels[len(b.channels)-1]
}

func (b *Broker) findMemberLocked(slug string) *officeMember {
	slug = normalizeChannelSlug(slug)
	if len(b.memberIndex) != len(b.members) {
		b.rebuildMemberIndexLocked()
	}
	if i, ok := b.memberIndex[slug]; ok && i < len(b.members) && b.members[i].Slug == slug {
		return &b.members[i]
	}
	return nil
}

// hasMember reports whether the roster contains slug. Lock-safe wrapper around
// findMemberLocked for callers that do not already hold b.mu (e.g. HTTP
// handlers).
func (b *Broker) hasMember(slug string) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.findMemberLocked(slug) != nil
}

// rebuildMemberIndexLocked rebuilds memberIndex from b.members. Callers must
// hold b.mu. Called on load and after any structural mutation (remove, reorder)
// to keep the map in sync with the slice. Appends and in-place updates are
// handled by findMemberLocked's length-check lazy rebuild.
func (b *Broker) rebuildMemberIndexLocked() {
	b.memberIndex = make(map[string]int, len(b.members))
	for i, m := range b.members {
		b.memberIndex[m.Slug] = i
	}
}
