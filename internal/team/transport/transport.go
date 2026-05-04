package transport

import "context"

// Transport is the base adapter interface — implemented by all external
// message adapters regardless of scope. Channel-bound adapters (Telegram)
// implement this interface directly. Member-bound and office-bound adapters
// implement [MemberBoundTransport] or [OfficeBoundTransport], which embed
// Transport.
//
// # Implementing Transport
//
// An adapter must:
//  1. Return a stable, unique name from [Name] (used as the AdapterName key
//     in every [Participant] the adapter creates — changing it between
//     releases loses participant identity across restarts).
//  2. Return the scope declaration from [Binding]. The Host uses Binding.Scope
//     to route inbound messages; an incorrect scope causes silent misrouting.
//  3. Block in [Run] until ctx is cancelled. The Host supervises Run in a
//     dedicated goroutine; if Run returns a non-nil error the Host applies
//     backoff and calls Run again. Run returning nil means intentional shutdown.
//  4. Send one outbound message per [Send] call. Send should respect ctx for
//     timeouts. If Send blocks longer than the Host-configured timeout the
//     Host logs the delay and drops the message (visible failure, not silent).
//  5. Return a non-stale [Health] snapshot from [Health]. This is called on
//     every channel-header render — it must be O(1) (read from a cached field,
//     not a network call).
//
// Pitfalls (see docs/ADD-A-TRANSPORT.md for full list):
//   - Call [Host.UpsertParticipant] before the first [Host.ReceiveMessage] for
//     any new external identity. Skipping this causes [ErrParticipantUnknown].
//   - Do not import internal/team from inside this package. The one-way
//     compile boundary (team → transport) is the core contract invariant.
//   - Channel-bound adapters: declare the bound channel slug in [Binding] and
//     verify it exists in the broker before calling [Run]. If the channel was
//     deleted, [Run] will receive [ErrBindingChannelMissing] on its first
//     [Host.ReceiveMessage] call and should shut down gracefully.
type Transport interface {
	// Name returns the stable, unique adapter name (e.g. "telegram", "openclaw",
	// "share"). Used as AdapterName in all Participant values the adapter creates.
	// Must be a valid Go identifier (lowercase, no spaces).
	Name() string

	// Binding returns the scope and anchor of this adapter within the office.
	// Called once at registration; the result is cached by the Host. If the
	// binding references a channel or member that does not exist the Host returns
	// [ErrBindingChannelMissing] or [ErrParticipantUnknown] at registration time.
	Binding() Binding

	// Run starts the adapter and blocks until ctx is cancelled or an
	// unrecoverable error occurs. The Host calls Run in a dedicated goroutine;
	// returning a non-nil error triggers supervised reconnection with backoff.
	// Returning nil means intentional shutdown — the Host will not reconnect.
	//
	// The adapter calls host.ReceiveMessage for each inbound message and keeps
	// host.UpsertParticipant current for each external identity it encounters.
	Run(ctx context.Context, host Host) error

	// Send delivers one outbound message from the office to the external network.
	// The Host calls Send from a per-transport worker goroutine (not the broker's
	// hot path). Send should time-out or return an error rather than block
	// indefinitely; the Host will log and discard messages that exceed the
	// configured send timeout.
	Send(ctx context.Context, msg Outbound) error

	// Health returns a point-in-time snapshot of adapter connectivity. Called
	// on every channel-header render — must be O(1) (read from a cached field).
	// Never make a network call inside Health.
	Health() Health
}

// MemberBoundTransport is implemented by adapters where each bridged external
// identity becomes an office member (e.g. OpenClaw). The adapter manages
// session lifecycle; the Host manages member identity in the broker.
//
// # Implementing MemberBoundTransport
//
// In addition to [Transport] requirements:
//  1. Call [Host.UpsertParticipant] when a new session is created or
//     reconnected. The Host creates (or finds) the corresponding office member.
//  2. Call [Host.DetachParticipant] when a session ends permanently. The Host
//     marks the member offline in the broker.
//  3. [CreateSession]: the Host calls this when a user hires a new agent from
//     the web UI. The adapter creates the upstream session and returns its key.
//  4. [AttachSlug] / [DetachSlug]: called by the Host when an office member
//     slug is bound to or unbound from an adapter session. The adapter uses
//     these to maintain its slug→key and key→slug maps for routing outbound
//     messages to the correct session.
type MemberBoundTransport interface {
	Transport

	// CreateSession creates a new upstream session for the given agentID and
	// human-readable label. Returns the opaque session key the adapter will use
	// to correlate future events. The Host stores the key and calls [AttachSlug]
	// immediately after.
	CreateSession(ctx context.Context, agentID, label string) (sessionKey string, err error)

	// AttachSlug binds an office member slug to a session key. The adapter
	// should update its internal slug→key and key→slug maps under its own lock.
	// Called by the Host after [CreateSession] and on reconnect.
	AttachSlug(slug, sessionKey string)

	// DetachSlug removes the binding between slug and its session key. Called
	// by the Host when the office member is removed or the session is terminated
	// by the upstream service.
	DetachSlug(slug string)
}

// OfficeBoundTransport is implemented by adapters that admit external humans to
// the whole office (e.g. human-share invite links). The adapter manages invite
// tokens and session cookies; the Host manages admitted-human identity.
//
// # Implementing OfficeBoundTransport
//
// In addition to [Transport] requirements:
//  1. Call [Host.UpsertParticipant] when a new human is admitted. The Host
//     creates a temporary office member for the admitted human.
//  2. Call [Host.RevokeParticipant] when an invite is revoked or the session
//     cookie expires. The Host removes the admitted human from the broker.
//  3. [CreateInvite]: the Host calls this when an office admin creates a new
//     invite link. The adapter generates the token, stores it, and returns the
//     shareable URL.
//  4. [RevokeInvite]: the Host calls this when the admin revokes an existing
//     invite. The adapter invalidates the token and closes any active sessions
//     using it.
type OfficeBoundTransport interface {
	Transport

	// CreateInvite creates a new invite token for the given network binding
	// (e.g. Tailscale, LAN, public). Returns the shareable invite URL.
	// The adapter is responsible for persisting the token across restarts.
	CreateInvite(ctx context.Context, network string) (inviteURL string, err error)

	// RevokeInvite invalidates the invite identified by inviteID. Active
	// sessions that were admitted via this invite are terminated by the adapter.
	// The Host calls [Host.RevokeParticipant] for each affected admitted human.
	RevokeInvite(ctx context.Context, inviteID string) error
}

// Host is the broker-side interface that adapters use to interact with the
// office. The Host implementation lives in internal/team/broker_transport.go.
// Adapters receive a Host when [Transport.Run] is called and use only this
// interface — they never import internal/team directly.
//
// The Host guarantees:
//   - [ReceiveMessage] is goroutine-safe. Multiple adapter goroutines may call
//     it concurrently.
//   - [UpsertParticipant] is idempotent. Call it every time you encounter a
//     participant, not just on first contact. The Host coalesces by (AdapterName, Key).
//   - All methods respect the ctx passed to [Transport.Run]. When ctx is
//     cancelled the Host returns promptly so the adapter can shut down cleanly.
//
// See docs/ADD-A-TRANSPORT.md §Host contract for the full invariant table.
type Host interface {
	// ReceiveMessage delivers an inbound message from an external participant
	// to the office. The Host resolves the binding to the correct channel,
	// writes the message to the broker under the broker mutex, and returns nil
	// on success. Possible errors: [ErrParticipantUnknown] (adapter forgot to
	// call UpsertParticipant first), [ErrBindingChannelMissing] (the declared
	// channel no longer exists). [ErrAdapterCrashed] (broker panic recovered)
	// is reserved for Phase 2b when the real dispatch path adds a recover()
	// wrapper — the Phase 1 stub does not return it.
	//
	// Pitfall: always call UpsertParticipant before the first ReceiveMessage
	// for a new external identity. Skipping this returns ErrParticipantUnknown.
	ReceiveMessage(ctx context.Context, msg Message) error

	// UpsertParticipant registers or refreshes an external identity in the
	// broker. Idempotent: safe to call on every inbound event. The Host creates
	// a broker member for new identities and updates DisplayName/Human for
	// existing ones. Returns nil on success; returns [ErrRegistrationConflict]
	// if (AdapterName, Key) maps to a different member slug than the one already
	// registered (adapter restart with a conflicting key assignment).
	UpsertParticipant(ctx context.Context, p Participant, b Binding) error

	// DetachParticipant marks the participant identified by (adapterName, key)
	// as offline in the broker. Called by member-bound and office-bound adapters
	// when a session or invite expires. Idempotent: no-op if the participant is
	// not known.
	DetachParticipant(ctx context.Context, adapterName, key string) error

	// RevokeParticipant removes the admitted human identified by (adapterName,
	// key) from the broker. Called by office-bound adapters when an invite is
	// revoked. More permanent than DetachParticipant: the member record is
	// deleted, not just marked offline.
	RevokeParticipant(ctx context.Context, adapterName, key string) error
}
