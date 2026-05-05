package team

// broker_transport.go implements transport.Host on the Broker. This is the
// only file in internal/team that imports internal/team/transport — the
// one-way compile boundary (team → transport) is enforced here.
//
// ReceiveMessage and UpsertParticipant route to PostInboundSurfaceMessage for
// channel-bound (Telegram) adapters; UpsertParticipant is a no-op there because
// participant attribution is handled inside PostInboundSurfaceMessage by
// display name. The share adapter takes the same no-op path: admitted-human
// identity already exists in the broker as a humanSession before the adapter
// fires the contract call, so UpsertParticipant just confirms the contract
// without duplicating broker state.
//
// RevokeParticipant routes the office-bound (human-share) revoke flow to
// Broker.revokeHumanSession so an adapter calling Host.RevokeParticipant after
// an invite is revoked tears down the corresponding session.
//
// DetachParticipant routes the member-bound (openclaw) detach flow to a
// session-end notice. The broker has no per-member presence flag today, so the
// hook validates the call and returns nil — future presence work plugs in
// here without further adapter changes.

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

// UpsertParticipant registers an external identity. For channel-bound adapters
// (Telegram) participant attribution is handled inside PostInboundSurfaceMessage
// by display name, so the call is a no-op. For the office-bound share adapter
// the admitted-human record already exists in the broker as a humanSession by
// the time this fires (see broker_human_share.handleHumanInviteAccept), so the
// contract is satisfied without a duplicate broker write. Member-bound
// adapters (OpenClaw) take the same no-op path until the broker grows a
// per-member registration store.
func (h *brokerTransportHost) UpsertParticipant(_ context.Context, _ transport.Participant, _ transport.Binding) error {
	return nil
}

// DetachParticipant marks a participant as offline. Recognized adapters
// (openclaw) take a no-op success path: the bridge has already cleared its
// own slug binding by the time this is called and the broker has no
// per-member presence flag yet. Wiring the call site now means future
// presence work can extend this without touching adapters. An unknown
// adapterName is rejected so a misnamed call surfaces loudly rather than
// silently no-op'ing.
func (h *brokerTransportHost) DetachParticipant(_ context.Context, adapterName, key string) error {
	if h == nil || h.broker == nil {
		return errors.New("transport: DetachParticipant: nil broker")
	}
	switch adapterName {
	case openclawAdapterName:
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
