package team

// broker_transport.go implements transport.Host on the Broker. This is the
// only file in internal/team that imports internal/team/transport — the
// one-way compile boundary (team → transport) is enforced here.
//
// ReceiveMessage and UpsertParticipant route to PostInboundSurfaceMessage for
// channel-bound (Telegram) adapters; UpsertParticipant is a no-op there because
// participant attribution is handled inside PostInboundSurfaceMessage by
// display name.
//
// RevokeParticipant routes the office-bound (human-share) revoke flow to
// Broker.revokeHumanSession so an adapter calling Host.RevokeParticipant after
// an invite is revoked tears down the corresponding session. Member-bound
// DetachParticipant remains a stub until the openclaw lifecycle adds it.

import (
	"context"
	"errors"
	"fmt"

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

// UpsertParticipant is a no-op for channel-bound adapters (Telegram). Channel-
// bound participant identity is resolved by display name inside
// PostInboundSurfaceMessage. Phase 3b will add a real member-store lookup for
// member-bound adapters (OpenClaw).
func (h *brokerTransportHost) UpsertParticipant(_ context.Context, _ transport.Participant, _ transport.Binding) error {
	return nil
}

// DetachParticipant marks a participant as offline. Phase 3b TODO for
// member-bound adapters (OpenClaw).
func (h *brokerTransportHost) DetachParticipant(_ context.Context, adapterName, _ string) error {
	return fmt.Errorf("transport: DetachParticipant not yet implemented (phase 3b): adapter=%q",
		adapterName)
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
