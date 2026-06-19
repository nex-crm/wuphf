package team

import (
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

// connectionRegistryTTL is how long a last-known connection state is trusted
// during a provider outage before it is treated as stale. Within the TTL a
// connected last-known state lets actions proceed during a Composio outage;
// past it, the resolver blocks-with-retry rather than firing blind.
const connectionRegistryTTL = 10 * time.Minute

// connectionRegistryEntry is the persisted, last-known connection state for one
// platform. It is the cached projection the resolver reads so the hot path does
// not call the provider on every action. Refreshed by probe + connect/disconnect
// events. Stored as a dedicated map in broker state, never derived from the
// 150-entry action ring.
type connectionRegistryEntry struct {
	Platform       string `json:"platform"`
	Provider       string `json:"provider,omitempty"`
	State          string `json:"state"`
	ConnectionKey  string `json:"connection_key,omitempty"`
	AccountName    string `json:"account_name,omitempty"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
}

// connectionRegistryKey normalizes a platform name to its registry key.
func connectionRegistryKey(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

// lookupConnectionRegistry returns the cached entry for a platform, if any.
// Locks b.mu; callers must not already hold it.
func (b *Broker) lookupConnectionRegistry(platform string) (connectionRegistryEntry, bool) {
	key := connectionRegistryKey(platform)
	if key == "" {
		return connectionRegistryEntry{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.connectionRegistry[key]
	return entry, ok
}

// upsertConnectionRegistry writes the last-known state for a platform and stamps
// last_verified_at to now. It deliberately refuses to record indeterminate
// state: a provider outage must never overwrite a good last-known entry, or the
// fail-safe path would lose the very last-known-good it depends on. Locks b.mu.
func (b *Broker) upsertConnectionRegistry(entry connectionRegistryEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.upsertConnectionRegistryLocked(entry)
}

// upsertConnectionRegistryLocked is the body of upsertConnectionRegistry for
// callers that already hold b.mu (the connect fan-out resumes parked work under
// the lock). Same invariants: refuses indeterminate, skips redundant writes,
// best-effort persists.
func (b *Broker) upsertConnectionRegistryLocked(entry connectionRegistryEntry) {
	key := connectionRegistryKey(entry.Platform)
	if key == "" || entry.State == "" || entry.State == string(action.StateIndeterminate) {
		return
	}
	entry.Platform = key
	entry.LastVerifiedAt = time.Now().UTC().Format(time.RFC3339)
	if b.connectionRegistry == nil {
		b.connectionRegistry = make(map[string]connectionRegistryEntry)
	}
	// Skip a redundant disk write when nothing meaningful changed. resolve runs
	// on the action hot path and re-probes the same connected state constantly;
	// only state/key/account changes warrant a save (LastVerifiedAt always
	// moves and is not worth persisting on its own).
	if existing, ok := b.connectionRegistry[key]; ok &&
		existing.State == entry.State &&
		existing.ConnectionKey == entry.ConnectionKey &&
		existing.AccountName == entry.AccountName {
		return
	}
	b.connectionRegistry[key] = entry
	// Best-effort persist: the registry is a cache rebuildable by re-probe, so a
	// failed write is non-fatal and must not block the action gate.
	_ = b.saveLocked()
}

// draftKnownPlatforms returns the set of integration platform slugs the office
// has a connection-registry entry for. The workflow drafter uses it to bind a
// detected action token (e.g. "gmail_fetch_emails") back to a real
// platform/action_id ONLY when the platform is one the office actually knows,
// so a synthetic or non-integration token ("summarize_threads") is never
// mis-bound to a bogus platform. Returns nil when the office has no known
// connections, in which case the drafter leaves actions unbound. Locks b.mu.
func (b *Broker) draftKnownPlatforms() map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.connectionRegistry) == 0 {
		return nil
	}
	out := make(map[string]bool, len(b.connectionRegistry))
	for k := range b.connectionRegistry {
		out[k] = true
	}
	return out
}

// cloneConnectionRegistryLocked returns a copy of the registry for persistence.
// The caller MUST hold b.mu (it is invoked from the locked save buildState
// path, mirroring how other broker slices are cloned there).
func (b *Broker) cloneConnectionRegistryLocked() map[string]connectionRegistryEntry {
	if len(b.connectionRegistry) == 0 {
		return nil
	}
	out := make(map[string]connectionRegistryEntry, len(b.connectionRegistry))
	for k, v := range b.connectionRegistry {
		out[k] = v
	}
	return out
}

// connectionRegistryFresh reports whether an entry was verified within the
// staleness TTL of now. An entry with no/unparseable timestamp is never fresh.
func connectionRegistryFresh(entry connectionRegistryEntry, now time.Time) bool {
	if strings.TrimSpace(entry.LastVerifiedAt) == "" {
		return false
	}
	ts, err := time.Parse(time.RFC3339, entry.LastVerifiedAt)
	if err != nil {
		return false
	}
	return !now.Before(ts) && now.Sub(ts) <= connectionRegistryTTL
}
