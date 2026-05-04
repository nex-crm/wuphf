package team

// broker_transport.go implements transport.Host on the Broker. This is the
// only file in internal/team that imports internal/team/transport — the
// one-way compile boundary (team → transport) is enforced here.
//
// Phase 1 ships the Host interface and the stub implementation. The real
// routing logic (message fan-out, participant upsert into broker member store,
// per-transport worker goroutines) lands in Phase 2b when Telegram is refactored
// onto the contract. Until then every method returns a descriptive stub error
// so contributors wiring a new adapter against this Host see a clear "not yet
// wired" message rather than a silent no-op.
//
// Note on cleanup: RegisterTransports (called by Launch, LaunchWeb, and
// launchHeadlessCodex) is a Phase 1 stub that starts no goroutines and opens
// no connections, so the launchers' early-abort paths require no teardown.
// When Phase 2a wires real adapters, RegisterTransports will return a cleanup
// function and the launchers will call it on abort — tracked in launcher_transports.go.

import (
	"context"
	"fmt"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// brokerTransportHost implements transport.Host for the Broker.
// One instance is created per Broker via newBrokerTransportHost.
type brokerTransportHost struct {
	broker *Broker
}

// newBrokerTransportHost returns a Host implementation backed by b.
// Called by launcher.RegisterTransports when adapters are registered.
func newBrokerTransportHost(b *Broker) transport.Host {
	return &brokerTransportHost{broker: b}
}

// ReceiveMessage delivers an inbound message from an external adapter to the
// office. Phase 1 stub — returns an informative error until Phase 2b wires the
// real routing.
func (h *brokerTransportHost) ReceiveMessage(_ context.Context, msg transport.Message) error {
	// Phase 2b TODO: resolve binding → channel, write message to broker under
	// h.broker.mu, deduplicate by (AdapterName+Key+ExternalID).
	return fmt.Errorf("transport: ReceiveMessage not yet implemented (phase 2b): adapter=%q",
		msg.Participant.AdapterName)
}

// UpsertParticipant registers or refreshes an external identity in the broker.
// Phase 1 stub — returns an informative error until Phase 2b wires the real
// member upsert.
func (h *brokerTransportHost) UpsertParticipant(_ context.Context, p transport.Participant, _ transport.Binding) error {
	// Phase 2b TODO: look up (p.AdapterName, p.Key) in broker member store;
	// create member if new; update DisplayName on revisit; return
	// ErrRegistrationConflict if key maps to a different existing slug.
	return fmt.Errorf("transport: UpsertParticipant not yet implemented (phase 2b): adapter=%q",
		p.AdapterName)
}

// DetachParticipant marks a participant as offline. Phase 1 stub.
func (h *brokerTransportHost) DetachParticipant(_ context.Context, adapterName, _ string) error {
	// Phase 2b TODO: mark the member corresponding to (adapterName, key) as
	// offline in the broker member store.
	return fmt.Errorf("transport: DetachParticipant not yet implemented (phase 2b): adapter=%q",
		adapterName)
}

// RevokeParticipant removes an admitted human from the broker. Phase 1 stub.
func (h *brokerTransportHost) RevokeParticipant(_ context.Context, adapterName, _ string) error {
	// Phase 4 TODO: delete the member record for (adapterName, key) from the
	// broker member store and close any open sessions associated with this key.
	return fmt.Errorf("transport: RevokeParticipant not yet implemented (phase 4): adapter=%q",
		adapterName)
}
