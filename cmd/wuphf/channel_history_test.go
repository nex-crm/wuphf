package main

import "testing"

func TestComposerHistoryRestoresStashedDraft(t *testing.T) {
	h := newComposerHistory()
	h.record([]rune("first"), len([]rune("first")))
	h.record([]rune("second"), len([]rune("second")))

	snapshot, ok := h.previous([]rune("working draft"), len([]rune("working draft")))
	if !ok || string(snapshot.input) != "second" || snapshot.pos != len([]rune("second")) {
		t.Fatalf("expected second entry, got %q %d %v", string(snapshot.input), snapshot.pos, ok)
	}

	snapshot, ok = h.next()
	if !ok || string(snapshot.input) != "working draft" || snapshot.pos != len([]rune("working draft")) {
		t.Fatalf("expected restored draft, got %q %d %v", string(snapshot.input), snapshot.pos, ok)
	}
}

func TestComposerHistoryDedupesAdjacentEntries(t *testing.T) {
	h := newComposerHistory()
	h.record([]rune("same"), 4)
	h.record([]rune("same"), 4)
	if len(h.entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(h.entries))
	}
}
