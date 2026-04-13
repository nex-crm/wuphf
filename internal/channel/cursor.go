// internal/channel/cursor.go
package channel

import "fmt"

// GetMember returns the ChannelMember for a given channel+slug pair.
func (s *Store) GetMember(channelID, slug string) (*ChannelMember, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.members {
		if s.members[i].ChannelID == channelID && s.members[i].Slug == slug {
			return &s.members[i], true
		}
	}
	return nil, false
}

// MarkRead advances the LastReadID cursor for a channel member.
func (s *Store) MarkRead(channelID, slug, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.members {
		if s.members[i].ChannelID == channelID && s.members[i].Slug == slug {
			s.members[i].LastReadID = messageID
			return nil
		}
	}
	return fmt.Errorf("member %q not in channel %q", slug, channelID)
}

// MarkProcessed advances the LastProcessedID cursor and resets MentionCount.
func (s *Store) MarkProcessed(channelID, slug, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.members {
		if s.members[i].ChannelID == channelID && s.members[i].Slug == slug {
			s.members[i].LastProcessedID = messageID
			s.members[i].MentionCount = 0
			return nil
		}
	}
	return fmt.Errorf("member %q not in channel %q", slug, channelID)
}
