// internal/channel/message_test.go
package channel

import "testing"

func TestAppendAndRetrieveMessages(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.AppendMessage(Message{ID: "msg-1", From: "human", Channel: ch.Slug, Content: "hello"})
	msgs := s.ChannelMessages(ch.Slug)
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestIncrementMentionsDM(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	// Human sends message → engineering's mention count goes up
	s.AppendMessage(Message{ID: "msg-1", From: "human", Channel: ch.Slug, Content: "hey"})
	s.IncrementMentions(ch.Slug, "human")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.MentionCount != 1 {
		t.Errorf("expected 1 mention for engineering, got %d", m.MentionCount)
	}
	// Sender's own count should not increase
	mh, _ := s.GetMember(ch.ID, "human")
	if mh.MentionCount != 0 {
		t.Errorf("sender should have 0 mentions, got %d", mh.MentionCount)
	}
}

func TestIncrementMentionsPublicChannel(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.AddMember(ch.ID, "human", "mention")
	s.AddMember(ch.ID, "engineering", "mention")
	// Public channel: IncrementMentions only bumps explicitly tagged members
	s.IncrementMentionsForTagged(ch.Slug, "human", []string{"engineering"})
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.MentionCount != 1 {
		t.Errorf("expected 1 mention for tagged member, got %d", m.MentionCount)
	}
}

func TestThreadMessages(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.AppendMessage(Message{ID: "msg-1", From: "human", Channel: ch.Slug, Content: "root"})
	s.AppendMessage(Message{ID: "msg-2", From: "engineering", Channel: ch.Slug, Content: "reply", ReplyTo: "msg-1"})
	s.AppendMessage(Message{ID: "msg-3", From: "human", Channel: ch.Slug, Content: "other thread"})
	thread := s.ThreadMessages(ch.Slug, "msg-1")
	if len(thread) != 2 {
		t.Errorf("expected 2 messages in thread, got %d", len(thread))
	}
}

func TestIncrementMentionsDeletedChannelIsNoOp(t *testing.T) {
	s := NewStore()
	// Incrementing mentions on a channel slug that doesn't exist should not panic
	s.IncrementMentions("nonexistent-channel", "human")
	// No error, no panic — silent skip per spec failure modes
}

func TestCounterIncrements(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	id1 := s.NextMessageID()
	s.AppendMessage(Message{ID: id1, From: "human", Channel: ch.Slug, Content: "first"})
	id2 := s.NextMessageID()
	if id1 == id2 {
		t.Error("counter should produce unique IDs")
	}
}
