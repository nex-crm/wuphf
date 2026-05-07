package team

// broker_transport.go implements transport.Host on the Broker. This is the
// only file in internal/team that imports internal/team/transport — the
// one-way compile boundary (team → transport) is enforced here.
//
// ReceiveMessage routes inbound channel-bound traffic to PostInboundSurfaceMessage.
//
// UpsertParticipant flips per-member presence on for member-bound bindings
// (binding.Scope == ScopeMember and binding.MemberSlug non-empty). Channel-bound
// (Telegram) and office-bound (share) bindings are presence-irrelevant and take
// a no-op path: telegram participants aren't members, and admitted-human
// presence is tracked separately via humanSession.LastSeenAt + RevokedAt.
//
// DetachParticipant flips per-member presence off by reverse-looking-up the
// adapter+key pair against the slug map populated on UpsertParticipant. The
// bridge has already cleared its own slug binding by the time this fires, so
// the broker maintains its own reverse map (broker_presence.go) — the host
// cannot ask the bridge.
//
// RevokeParticipant routes the office-bound (human-share) revoke flow to
// Broker.revokeHumanSession so an adapter calling Host.RevokeParticipant after
// an invite is revoked tears down the corresponding session.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// brokerTransportHost implements transport.Host for the Broker.
type brokerTransportHost struct {
	broker *Broker
}

// ReceiveMessage delivers an inbound message from a channel-bound adapter to
// the office by calling PostInboundSurfaceMessage. Returns
// transport.ErrBindingChannelMissing if the declared channel does not exist.
func (h *brokerTransportHost) ReceiveMessage(_ context.Context, msg transport.Message) error {
	_, err := h.broker.PostInboundSurfaceMessage(
		msg.Participant.DisplayName,
		msg.Binding.ChannelSlug,
		msg.Text,
		msg.Participant.AdapterName,
	)
	if err != nil {
		if errors.Is(err, ErrChannelNotFound) {
			return &transport.BindingChannelMissingError{ChannelSlug: msg.Binding.ChannelSlug}
		}
		return fmt.Errorf("transport: ReceiveMessage: %w", err)
	}
	return nil
}

// UpsertParticipant registers an external identity. For member-bound bindings
// (openclaw today, future member-bound adapters tomorrow) the call flips the
// slug's presence on and stamps LastSeenAt. Channel-bound (Telegram)
// participants and office-bound (share) admitted humans are presence-irrelevant
// here: telegram attribution lives in PostInboundSurfaceMessage by display
// name, and humanSession owns admitted-human presence via its own LastSeenAt.
//
// A nil broker is rejected so a misconfigured host fails loudly rather than
// silently dropping presence updates. A binding without ScopeMember/MemberSlug
// is treated as "no member to mark online" and returns nil — the contract is
// satisfied; nothing to do.
//
// Adapter validation mirrors DetachParticipant's allowlist: only adapters that
// also have a valid detach path are accepted at member scope. Without this
// symmetry, a non-openclaw member-scope upsert would set online=true with no
// way to ever clear it (DetachParticipant would error on the unknown adapter
// name), leaving a permanent stale "online" indicator. New member-bound
// adapters must be added to BOTH switches together.
func (h *brokerTransportHost) UpsertParticipant(_ context.Context, p transport.Participant, b transport.Binding) error {
	if h == nil || h.broker == nil {
		return errors.New("transport: UpsertParticipant: nil broker")
	}
	if b.Scope != transport.ScopeMember {
		return nil
	}
	// Trim and reject empty before canonicalizing: normalizeChannelSlug falls
	// back to "general" on empty input, which would silently mark the general
	// channel as online for an empty MemberSlug binding. The helper applies
	// the same canonicalization on its end, but doing it here too keeps the
	// empty-check above the adapter/key validation so the error messages
	// report a non-empty slug.
	if strings.TrimSpace(b.MemberSlug) == "" {
		return nil
	}
	slug := normalizeChannelSlug(b.MemberSlug)
	adapter := strings.TrimSpace(p.AdapterName)
	if adapter == "" {
		return fmt.Errorf("transport: UpsertParticipant: empty AdapterName for slug %q", slug)
	}
	switch adapter {
	case openclawAdapterName:
		// recognized member-bound adapter; falls through
	default:
		return fmt.Errorf("transport: UpsertParticipant: unsupported adapter %q at member scope (slug=%q)", adapter, slug)
	}
	key := strings.TrimSpace(p.Key)
	if key == "" {
		return fmt.Errorf("transport: UpsertParticipant: empty Key for slug %q (adapter=%q)", slug, adapter)
	}
	h.broker.mu.Lock()
	h.broker.markMemberPresenceOnlineLocked(slug, adapter, key, time.Now())
	h.broker.mu.Unlock()
	return nil
}

// DetachParticipant marks a participant offline. Resolves slug from the
// (adapter, key) pair via the reverse map populated on UpsertParticipant. The
// bridge has already cleared its own slug↔key binding by the time this fires,
// so the broker cannot ask the bridge — it owns its own reverse lookup. An
// unknown (adapter, key) pair is not an error: it just means the host never
// saw an UpsertParticipant for that pair (e.g. the adapter sends Detach on
// shutdown for a session that never produced a message).
//
// LastSeenAt is preserved on flip-off so the API can render "last seen 5m
// ago"; only Online flips. An unknown adapterName is rejected to keep
// misnamed calls visible. A nil broker is rejected so a misconfigured host
// fails loudly rather than silently dropping detach updates.
func (h *brokerTransportHost) DetachParticipant(_ context.Context, adapterName, key string) error {
	if h == nil || h.broker == nil {
		return errors.New("transport: DetachParticipant: nil broker")
	}
	switch adapterName {
	case openclawAdapterName:
		h.broker.mu.Lock()
		h.broker.markMemberPresenceOfflineByKeyLocked(adapterName, key)
		h.broker.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("transport: DetachParticipant: unsupported adapter %q (key=%q)", adapterName, key)
	}
}

// RevokeParticipant removes an admitted human from the broker. For the
// office-bound human-share adapter the key is a humanSession.ID and the
// implementation routes to Broker.revokeHumanSession so the session is marked
// revoked and any active wait channels are closed. Other adapter names are
// rejected so a misnamed call surfaces loudly rather than silently no-op'ing.
func (h *brokerTransportHost) RevokeParticipant(_ context.Context, adapterName, key string) error {
	if h == nil || h.broker == nil {
		return errors.New("transport: RevokeParticipant: nil broker")
	}
	switch adapterName {
	case shareAdapterName:
		if err := h.broker.revokeHumanSession(key); err != nil {
			return fmt.Errorf("transport: RevokeParticipant %s/%s: %w", adapterName, key, err)
		}
		return nil
	default:
		return fmt.Errorf("transport: RevokeParticipant: unsupported adapter %q (key=%q)", adapterName, key)
	}
}
