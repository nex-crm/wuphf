package team

// broker_inbox_packet_types.go is the Lane E read-side adapter to
// Lane C's Decision Packet store.
//
// Lane C (broker_decision_packet*.go) owns the canonical shape and the
// mutators. Lane E used to ship a parallel set of stubs while building
// in a worktree; integration drops those stubs in favour of Lane C's
// canonical types so the wire/disk shape stays single-sourced. The
// only Lane-E-owned read helper is findDecisionPacketLocked below — it
// returns the live in-memory packet (or nil) without persisting on
// read, which is what the inbox row severity rollup and the
// /tasks/{id} packet view both need.

// findDecisionPacketLocked returns the in-memory Decision Packet for a
// task ID, or nil if Lane C has not stored one. Caller must hold b.mu.
//
// Lane E reads through this single accessor so the read path stays
// consistent across the inbox query (severity rollup) and the single-
// packet handler (full packet payload). Lane C's mutators are the
// only writers; Lane E never mutates the store.
func (b *Broker) findDecisionPacketLocked(taskID string) *DecisionPacket {
	if b == nil || taskID == "" || b.decisionPackets == nil {
		return nil
	}
	state := b.decisionPackets
	state.mu.Lock()
	defer state.mu.Unlock()
	if packet, ok := state.packets[taskID]; ok {
		return packet
	}
	return nil
}
