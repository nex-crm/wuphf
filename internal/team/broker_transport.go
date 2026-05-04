package team

// broker_transport.go implements transport.Host on the Broker. This is the
// only file in internal/team that imports internal/team/transport — the
// one-way compile boundary (team → transport) is enforced here.
//
// Phase 2a wires the existing TelegramTransport directly against *Broker (not
// through this Host). The Host and brokerTransportHost are preserved for Phase
// 2b when TelegramTransport is refactored onto the transport.Transport contract
// and will call Host.ReceiveMessage / Host.UpsertParticipant instead of writing
// to the broker directly.
//
// Until Phase 2b, every Host method returns a descriptive stub error so any
// contributor who wires a new adapter against this Host sees a clear
// "not yet wired" message rather than a silent no-op.

import (
	"context"
	"fmt"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// brokerTransportHost implements transport.Host for the Broker.
// Constructed in Phase 2b when adapters start using the contract interfaces.
type brokerTransportHost struct {
	broker *Broker
}

// ReceiveMessage delivers an inbound message from an external adapter to the
// office. Phase 2b TODO: resolve binding → channel, write message to broker
// under h.broker.mu, deduplicate by (AdapterName+Key+ExternalID).
func (h *brokerTransportHost) ReceiveMessage(_ context.Context, msg transport.Message) error {
	return fmt.Errorf("transport: ReceiveMessage not yet implemented (phase 2b): adapter=%q",
		msg.Participant.AdapterName)
}

// UpsertParticipant registers or refreshes an external identity in the broker.
// Phase 2b TODO: look up (p.AdapterName, p.Key) in broker member store.
func (h *brokerTransportHost) UpsertParticipant(_ context.Context, p transport.Participant, _ transport.Binding) error {
	return fmt.Errorf("transport: UpsertParticipant not yet implemented (phase 2b): adapter=%q",
		p.AdapterName)
}

// DetachParticipant marks a participant as offline. Phase 2b TODO.
func (h *brokerTransportHost) DetachParticipant(_ context.Context, adapterName, _ string) error {
	return fmt.Errorf("transport: DetachParticipant not yet implemented (phase 2b): adapter=%q",
		adapterName)
}

// RevokeParticipant removes an admitted human from the broker. Phase 4 TODO.
func (h *brokerTransportHost) RevokeParticipant(_ context.Context, adapterName, _ string) error {
	return fmt.Errorf("transport: RevokeParticipant not yet implemented (phase 4): adapter=%q",
		adapterName)
}
