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

// findDecisionPacketLocked returns the Decision Packet for a task ID.
// Falls through to the on-disk store when the in-memory cache misses,
// so packets persisted across broker restarts surface correctly to
// the inbox + /tasks/{id} handler. Returns nil only when the packet
// doesn't exist on disk either. Caller must hold b.mu.
//
// Lane E reads through this single accessor so the read path stays
// consistent across the inbox query (severity rollup) and the single-
// packet handler (full packet payload). Lane C's mutators are the
// only writers; Lane E never mutates the store except for the
// implicit memoization that happens on a cache miss.
func (b *Broker) findDecisionPacketLocked(taskID string) *DecisionPacket {
	if b == nil || taskID == "" {
		return nil
	}
	state := b.ensureDecisionPacketStateLocked()
	state.mu.Lock()
	if packet, ok := state.packets[taskID]; ok {
		state.mu.Unlock()
		return packet
	}
	state.mu.Unlock()
	// Cache miss: try the on-disk store. Common path is fresh broker
	// startup where seed/in-flight tasks have packet JSON on disk but
	// the in-memory cache is cold.
	disk, err := state.store.Read(taskID)
	if err != nil {
		return nil
	}
	state.mu.Lock()
	cp := disk
	state.packets[taskID] = &cp
	state.mu.Unlock()
	return &cp
}
