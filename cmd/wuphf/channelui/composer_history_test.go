package channelui

import "testing"

func TestComposerHistoryRestoresStashedDraft(t *testing.T) {
	h := NewHistory()
	h.Record([]rune("first"), len([]rune("first")))
	h.Record([]rune("second"), len([]rune("second")))

	snapshot, ok := h.Previous([]rune("working draft"), len([]rune("working draft")))
	if !ok || string(snapshot.Input) != "second" || snapshot.Pos != len([]rune("second")) {
		t.Fatalf("expected second entry, got %q %d %v", string(snapshot.Input), snapshot.Pos, ok)
	}

	snapshot, ok = h.Next()
	if !ok || string(snapshot.Input) != "working draft" || snapshot.Pos != len([]rune("working draft")) {
		t.Fatalf("expected restored draft, got %q %d %v", string(snapshot.Input), snapshot.Pos, ok)
	}
}

func TestComposerHistoryDedupesAdjacentEntries(t *testing.T) {
	h := NewHistory()
	h.Record([]rune("same"), 4)
	h.Record([]rune("same"), 4)
	if h.Len() != 1 {
		t.Fatalf("expected one entry, got %d", h.Len())
	}
}
