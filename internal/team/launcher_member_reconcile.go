package team

import (
	"fmt"

	"github.com/nex-crm/wuphf/internal/provider"
)

// reconcileMemberRuntime runs after the broker publishes a member_updated
// event. It exists to close the gap exposed by the per-agent runtime picker:
// switching an existing agent from one runtime to another used to leave the
// stale runtime's state behind, and the next dispatch could silently land on
// the wrong path (typically: user picks codex, agent never replies because
// the dispatch loop is still pointing at a dead claude session).
//
// What survives the broker-side update path:
//
//   - The per-agent ProviderBinding (member.Provider) is already the new
//     kind/model, so MemberEffectiveProviderKind returns the new value and
//     ShouldUseHeadlessForSlug → PaneTargets correctly skips the pane for
//     non-pane-eligible kinds. The next notification IS routed to the
//     headless runner.
//
//   - The claudeSessionStore entry for the slug is cleared on the broker
//     side (broker_office_members.go calls provider.ResetClaudeSessionFor
//     when the kind actually changed).
//
// What this method adds:
//
//   - A brief system message in the agent's most-likely-visible channel
//     telling the human that the runtime change landed. Without this the
//     switch is silent until the next message, which is the exact symptom
//     ("I changed the agent's provider and it never replied") the picker
//     exposes when paired with a broker that doesn't echo the new state.
//
// reconcileMemberRuntime is idempotent — the event fires on every member
// update (name, role, runtime) and a no-op runtime change has no side
// effect because the broker already clears the claude session only when
// the kind actually changed.
func (l *Launcher) reconcileMemberRuntime(slug string) {
	if l == nil || l.broker == nil || slug == "" {
		return
	}
	binding := l.broker.MemberProviderBinding(slug)
	kind := normalizeProviderKind(binding.Kind)
	if kind == "" {
		// Inherit-default: the launcher continues using the install-wide
		// resolver on the next dispatch, no further work needed.
		return
	}
	// Post a system message in #general so the human sees the switch
	// landed. We don't try to detect "the user is staring at a different
	// channel" — getting the signal in one canonical place is better than
	// a guessing game that risks duplicating across many channels.
	if l.broker != nil {
		body := fmt.Sprintf("@%s runtime → %s", slug, kind)
		if model := binding.Model; model != "" {
			body = fmt.Sprintf("@%s runtime → %s · %s", slug, kind, model)
		}
		l.broker.PostSystemMessage("general", body, "runtime")
	}
	// Belt-and-suspenders: re-clear the claude session record for this
	// slug even though the broker already did it. The store is shared
	// state and the underlying file write is not transactional with the
	// broker's state save — racing the launcher's next claude resume
	// lookup against a half-completed broker write would otherwise let a
	// stale id leak through. The per-slug clear is idempotent.
	provider.ResetClaudeSessionFor(slug)
}
