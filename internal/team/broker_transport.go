package team

// broker_transport.go implements transport.Host on the Broker. This is the
// only file in internal/team that imports internal/team/transport — the
// one-way compile boundary (team → transport) is enforced here.
//
// Phase 2b wires ReceiveMessage and UpsertParticipant for the channel-bound
// (Telegram) case. Inbound messages are delivered via PostInboundSurfaceMessage;
// UpsertParticipant is a no-op for channel-bound adapters because participant
// attribution is handled inside PostInboundSurfaceMessage by display name.
//
// DetachParticipant (phase 3b) and RevokeParticipant (phase 4) remain stubs.

import (
	"context"
	"fmt"
	"strings"

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
		if strings.Contains(err.Error(), "channel not found") {
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

// RevokeParticipant removes an admitted human from the broker. Phase 4 TODO
// for office-bound adapters (human-share).
func (h *brokerTransportHost) RevokeParticipant(_ context.Context, adapterName, _ string) error {
	return fmt.Errorf("transport: RevokeParticipant not yet implemented (phase 4): adapter=%q",
		adapterName)
}
