package team

import (
	"fmt"

	"github.com/nex-crm/wuphf/internal/provider"
)

// Per-agent provider binding. The launcher's dispatch switch consults
// MemberProviderKind to decide which runtime to invoke for a given
// agent — one team can mix Codex agents and Claude Code agents and
// OpenClaw bridges, each with their own ProviderBinding.
//
// SetMemberProvider is the write path used by:
//   - the OpenClaw bootstrap migration (legacy config.OpenclawBridges
//     -> per-member bindings)
//   - the handleOfficeMembers update path
// MemberProviderBinding / MemberProviderKind are the read paths the
// launcher consults at dispatch time.

// SetMemberProvider attaches or replaces the ProviderBinding on the given
// office member and persists broker state. Used by the OpenClaw bootstrap
// migration (moving legacy config.OpenclawBridges onto members) and by the
// handleOfficeMembers update path. Returns an error if the member doesn't
// exist; callers should ensure the member exists first.
func (b *Broker) SetMemberProvider(slug string, binding provider.ProviderBinding) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil {
		return fmt.Errorf("set member provider: unknown slug %q", slug)
	}
	m.Provider = binding
	return b.saveLocked()
}

// MemberProviderBinding returns the per-agent provider binding for slug, or
// the zero value if the member does not exist. Safe to call from outside the
// broker; takes the mutex internally.
func (b *Broker) MemberProviderBinding(slug string) provider.ProviderBinding {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil {
		return provider.ProviderBinding{}
	}
	return m.Provider
}

// MemberProviderKind returns the per-member runtime kind for the given slug,
// or "" if the member does not exist or has no explicit binding. Callers
// should fall back to the global runtime when the return value is empty.
// Used by the launcher's dispatch switch so each agent can run on its own
// provider (e.g., one Codex agent + one Claude Code agent in the same team).
func (b *Broker) MemberProviderKind(slug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil {
		return ""
	}
	return m.Provider.Kind
}
