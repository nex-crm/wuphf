// internal/channel/migration_test.go
package channel

import "testing"

func TestMigrateDMSlug(t *testing.T) {
	s := NewStore()
	// Simulate old broker state: channel with dm-* slug
	s.channels = append(s.channels, Channel{
		Slug: "dm-engineering",
		Name: "dm-engineering",
		Type: "dm", // old type value
	})
	s.messages = append(s.messages, Message{
		ID: "msg-1", From: "human", Channel: "dm-engineering", Content: "hello",
	})
	s.rebuildIndexes()

	migrated := s.MigrateLegacyDM()
	if !migrated {
		t.Error("expected migration to occur")
	}

	// Channel should now have proper type and slug
	ch, ok := s.GetBySlug(DirectSlug("human", "engineering"))
	if !ok {
		t.Fatal("migrated channel not found by new slug")
	}
	if ch.Type != ChannelTypeDirect {
		t.Errorf("expected type D, got %s", ch.Type)
	}
	if ch.ID == "" {
		t.Error("expected UUID to be assigned")
	}

	// Messages should reference new slug
	msgs := s.ChannelMessages(ch.Slug)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after migration, got %d", len(msgs))
	}

	// Members should be created
	members := s.Members(ch.ID)
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
}

func TestMigratePublicChannel(t *testing.T) {
	s := NewStore()
	s.channels = append(s.channels, Channel{
		Slug: "general",
		Name: "general",
	})
	s.rebuildIndexes()

	s.MigrateLegacyDM()

	ch, ok := s.GetBySlug("general")
	if !ok {
		t.Fatal("general channel should still exist")
	}
	if ch.Type != ChannelTypePublic {
		t.Errorf("expected type O, got %s", ch.Type)
	}
	if ch.ID == "" {
		t.Error("expected UUID to be assigned")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	originalID := ch.ID
	migrated := s.MigrateLegacyDM()
	if migrated {
		t.Error("migration should be no-op when channels already have UUIDs and proper types")
	}
	ch2, _ := s.GetBySlug("general")
	if ch2.ID != originalID {
		t.Error("UUID should not change on no-op migration")
	}
}

func TestMigrateTaskChannelReferences(t *testing.T) {
	old := "dm-engineering"
	expected := DirectSlug("human", "engineering")
	result := MigrateDMSlugString(old)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
	// Non-DM slugs should pass through unchanged
	if MigrateDMSlugString("general") != "general" {
		t.Error("non-DM slugs should pass through unchanged")
	}
}
