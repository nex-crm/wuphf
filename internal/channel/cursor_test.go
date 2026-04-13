// internal/channel/cursor_test.go
package channel

import "testing"

func TestMarkReadAdvancesCursor(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	s.MarkRead(ch.ID, "engineering", "msg-5")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.LastReadID != "msg-5" {
		t.Errorf("expected LastReadID=msg-5, got %s", m.LastReadID)
	}
}

func TestMarkProcessedAdvancesCursor(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	s.MarkProcessed(ch.ID, "engineering", "msg-3")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.LastProcessedID != "msg-3" {
		t.Errorf("expected LastProcessedID=msg-3, got %s", m.LastProcessedID)
	}
}

func TestMarkProcessedNotAdvancedOnMissedCall(t *testing.T) {
	// Simulates agent crash: MarkRead advances but MarkProcessed is never called.
	// On next poll, LastProcessedID should still be empty, allowing retry.
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	s.MarkRead(ch.ID, "engineering", "msg-5")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.LastReadID != "msg-5" {
		t.Error("LastReadID should advance")
	}
	if m.LastProcessedID != "" {
		t.Error("LastProcessedID should remain empty until explicitly set")
	}
}

func TestMarkReadOnNonMemberIsNoOp(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	err := s.MarkRead(ch.ID, "nonexistent", "msg-1")
	if err == nil {
		t.Error("expected error marking read for non-member")
	}
}
