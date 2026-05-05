package team

// broker_presence.go owns per-member presence state derived from
// transport.Host calls. UpsertParticipant flips a slug online; DetachParticipant
// flips it offline. The /office-members handler reads this map to render a live
// connection indicator next to each member, distinct from agentActivitySnapshot
// (which tracks "is the agent processing right now"); presence tracks "does the
// adapter still have a live session for this slug".
//
// Two maps are kept in lockstep under b.mu:
//   - memberPresence map[slug]record: read path for the API surface.
//   - presenceKeyToSlug map[adapter:key]slug: needed because Host.DetachParticipant
//     only carries (adapter, key); the bridge has already cleared its own slug→key
//     map by the time the host call fires, so the host needs its own reverse
//     lookup to know which slug to mark offline.
//
// Presence is in-memory only — restart re-derives it from the next Upsert wave
// (each adapter calls UpsertParticipant on bootstrap-replay or first message).
// Persisting it would invite stale "online" indicators after a crash; the
// adapter-driven re-flip is the source of truth.

import (
	"strings"
	"time"
)

// memberPresenceRecord is the per-slug presence snapshot. Online flips on
// UpsertParticipant and clears on DetachParticipant; LastSeenAt is bumped on
// every Upsert so the UI can show "last seen 5m ago" when a member is offline.
// AdapterName + SessionKey identify which adapter session last touched the
// member — useful for diagnostics and for the future case where a single slug
// might be served by more than one adapter.
type memberPresenceRecord struct {
	Online      bool
	LastSeenAt  time.Time
	AdapterName string
	SessionKey  string
}

// markMemberPresenceOnline records that an adapter session is now live for slug.
// Caller must already hold b.mu. Updates both maps so a follow-up
// DetachParticipant (which only carries adapter+key) can resolve the slug.
func (b *Broker) markMemberPresenceOnlineLocked(slug, adapterName, sessionKey string, at time.Time) {
	if b == nil {
		return
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return
	}
	if b.memberPresence == nil {
		b.memberPresence = make(map[string]memberPresenceRecord)
	}
	if b.presenceKeyToSlug == nil {
		b.presenceKeyToSlug = make(map[string]string)
	}
	prev := b.memberPresence[slug]
	if prev.AdapterName != "" && prev.SessionKey != "" &&
		(prev.AdapterName != adapterName || prev.SessionKey != sessionKey) {
		// A different adapter session previously held this slug. Clear its
		// reverse-lookup entry so a stale Detach against the old key cannot
		// flip the slug offline once the new session is live.
		delete(b.presenceKeyToSlug, presenceLookupKey(prev.AdapterName, prev.SessionKey))
	}
	b.memberPresence[slug] = memberPresenceRecord{
		Online:      true,
		LastSeenAt:  at,
		AdapterName: adapterName,
		SessionKey:  sessionKey,
	}
	b.presenceKeyToSlug[presenceLookupKey(adapterName, sessionKey)] = slug
}

// markMemberPresenceOfflineByKey resolves slug from the adapter+key reverse map
// and flips that slug offline. LastSeenAt is preserved so the UI can render a
// "last online" timestamp instead of erasing the history. Caller must already
// hold b.mu. Returns the slug that was cleared (empty if no match — useful for
// tests and for diagnostics).
func (b *Broker) markMemberPresenceOfflineByKeyLocked(adapterName, sessionKey string) string {
	if b == nil || b.presenceKeyToSlug == nil {
		return ""
	}
	lookup := presenceLookupKey(adapterName, sessionKey)
	slug, ok := b.presenceKeyToSlug[lookup]
	if !ok {
		return ""
	}
	delete(b.presenceKeyToSlug, lookup)
	if b.memberPresence == nil {
		return slug
	}
	rec := b.memberPresence[slug]
	if rec.AdapterName != adapterName || rec.SessionKey != sessionKey {
		// The reverse map pointed at this slug, but the slug's record has
		// already moved on to another session. Don't clobber the newer
		// session's Online flag — the reverse-map entry is just stale.
		return slug
	}
	rec.Online = false
	b.memberPresence[slug] = rec
	return slug
}

// presenceForSlug returns a copy of the presence record for slug. Caller must
// already hold b.mu. The bool result is false when no record exists (e.g. the
// slug has never been touched by an adapter); callers should treat this as
// "presence unknown", not "offline".
func (b *Broker) presenceForSlugLocked(slug string) (memberPresenceRecord, bool) {
	if b == nil || b.memberPresence == nil {
		return memberPresenceRecord{}, false
	}
	rec, ok := b.memberPresence[slug]
	return rec, ok
}

// presenceLookupKey builds the composite key used in presenceKeyToSlug.
// Adapter names are stable Go identifiers per Transport.Name() so a colon
// separator is collision-free.
func presenceLookupKey(adapterName, sessionKey string) string {
	return adapterName + ":" + sessionKey
}
