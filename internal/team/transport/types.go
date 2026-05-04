// Package transport defines the contract between the WUPHF broker and external
// message adapters (Telegram, OpenClaw, human-share, and future integrations).
//
// # Architecture summary
//
// The broker is the authority for office state. Adapters are reactive: they
// emit inbound messages via [Host.ReceiveMessage] and receive outbound messages
// via [Transport.Send]. The Host owns the subscriber loop and per-transport
// worker goroutines so a slow adapter never blocks another.
//
// Three adapter scopes:
//
//   - [Transport] — channel-bound (e.g. Telegram). One external chat maps to
//     one office channel.
//   - [MemberBoundTransport] — member-bound (e.g. OpenClaw). Each bridged
//     session becomes an office member; the adapter manages its own session
//     lifecycle.
//   - [OfficeBoundTransport] — office-bound (e.g. human-share). Admitted
//     humans interact with the whole office, not a single channel or member.
//
// # Package boundary
//
// This package imports only the standard library and external SDKs. It does
// NOT import internal/team. The broker imports this package and implements
// [Host]. Adapters implement one of the Transport sub-interfaces. Go's import
// rules make the one-way arrow (team → transport) a compile-time invariant:
// any future PR that needs broker internals from inside an adapter MUST extend
// [Host] instead, keeping the boundary visible at review time.
//
// See docs/ADD-A-TRANSPORT.md for a step-by-step contributor guide.
package transport

import "time"

// Scope declares where an adapter binds within the office.
type Scope string

const (
	// ScopeChannel is for adapters that map one external chat to one office
	// channel (e.g. Telegram group → #standup).
	ScopeChannel Scope = "channel"

	// ScopeMember is for adapters where each bridged session becomes an office
	// member (e.g. OpenClaw agent hired via the web UI).
	ScopeMember Scope = "member"

	// ScopeOffice is for adapters that admit an external human to the whole
	// office (e.g. human-share invite link).
	ScopeOffice Scope = "office"
)

// Binding declares how an adapter attaches to the office. The fields that
// apply depend on Scope:
//
//   - ScopeChannel: ChannelSlug is required; MemberSlug is empty.
//   - ScopeMember:  MemberSlug is required; ChannelSlug is empty.
//   - ScopeOffice:  both are empty (office-wide scope has no single anchor).
type Binding struct {
	Scope       Scope
	ChannelSlug string
	MemberSlug  string
}

// Participant identifies the external identity behind a message. The Host uses
// (AdapterName, Key) as a stable composite key across restarts to deduplicate
// participants and avoid creating duplicate members.
type Participant struct {
	// AdapterName must match [Transport.Name]. The Host uses this to namespace
	// participant keys across adapter types.
	AdapterName string
	// Key is the adapter-internal, stable identifier for this participant
	// (e.g. Telegram user ID as string, OpenClaw session key, share token).
	// Must be stable across broker restarts.
	Key string
	// DisplayName is the human-readable name shown in the office. The Host
	// may override this from its member store if the participant has connected
	// before.
	DisplayName string
	// Human is true when the participant is a human (as opposed to an AI agent
	// or automated bot). The broker uses this for attribution and rate-limiting.
	Human bool
}

// Message is an inbound message from an external participant to the office.
// The adapter constructs this and passes it to [Host.ReceiveMessage].
type Message struct {
	// Participant is who sent the message. For channel-bound adapters this is
	// the Telegram user; for member-bound this is the OpenClaw session acting
	// as a member; for office-bound this is the admitted human.
	Participant Participant
	// Binding is where the message should land in the office. The Host uses
	// Binding.Scope to decide routing: channel-bound posts into ChannelSlug;
	// member-bound routes to the last channel the member addressed; office-bound
	// posts as the admitted human into whatever channel they addressed.
	Binding Binding
	// Text is the message body. Adapters perform any network-layer decoding
	// (e.g. HTML entity unescaping from Telegram) before setting this field.
	Text string
	// ExternalID is an opaque, adapter-specific message identifier used by the
	// Host to deduplicate at-least-once delivery across restarts. Adapters that
	// guarantee at-most-once delivery may leave this empty; the Host will not
	// apply deduplication.
	ExternalID string
	// ThreadKey is optional and opaque to the contract. Adapters that support
	// threading (e.g. Telegram reply-to message_id) populate this field; it
	// travels with the message so the Host can echo it back on [Outbound] for
	// the adapter to use when sending a threaded reply.
	ThreadKey string
}

// Outbound is a message from the office to an external participant. The Host
// constructs this and delivers it to the per-transport worker goroutine, which
// calls [Transport.Send].
type Outbound struct {
	// Participant is who the office is replying to. Channel-bound adapters may
	// ignore this (reply goes to the bound chat); member-bound and office-bound
	// adapters use it to find the right session or admitted-human socket.
	Participant Participant
	// Binding matches the Binding that produced the original inbound message,
	// so the adapter can correlate the reply to the correct chat/session.
	Binding Binding
	// Text is the message body to deliver.
	Text string
	// ThreadKey is echoed from the inbound [Message.ThreadKey]. Adapters that
	// support threading use this to send a threaded reply; adapters that do not
	// support threading ignore it.
	ThreadKey string
}

// HealthState is the connection state of an adapter.
type HealthState string

const (
	// HealthConnected means the adapter is actively polling or subscribed and
	// has received a successful response within the expected polling window.
	HealthConnected HealthState = "connected"
	// HealthDegraded means the adapter is running but has experienced recent
	// errors. It may recover without intervention (transient network issue) or
	// may require user action (token revoked).
	HealthDegraded HealthState = "degraded"
	// HealthDisconnected means the adapter is not running or has given up
	// reconnecting (circuit breaker open).
	HealthDisconnected HealthState = "disconnected"
)

// Health is a point-in-time snapshot of adapter health. The Host reads this
// via [Transport.Health] to surface a per-channel status indicator in the UI.
type Health struct {
	State         HealthState
	LastSuccessAt time.Time
	// LastError is the most recent error the adapter encountered, or nil if
	// the adapter has never errored or has recovered.
	LastError error
}
