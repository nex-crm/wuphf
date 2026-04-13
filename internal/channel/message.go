// internal/channel/message.go
package channel

import "fmt"

// AppendMessage appends a message to the store and updates LastPostAt on the channel.
func (s *Store) AppendMessage(msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, msg)

	// Update LastPostAt on the channel (look up by slug)
	if ch, ok := s.bySlug[msg.Channel]; ok {
		ch.LastPostAt = now()
		ch.UpdatedAt = ch.LastPostAt
	}
	return nil
}

// ChannelMessages returns all messages for a channel identified by slug.
func (s *Store) ChannelMessages(channelSlug string) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Message
	for _, m := range s.messages {
		if m.Channel == channelSlug {
			result = append(result, m)
		}
	}
	return result
}

// ThreadMessages returns the root message and all replies to it within a channel.
func (s *Store) ThreadMessages(channelSlug, threadID string) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Message
	for _, m := range s.messages {
		if m.Channel != channelSlug {
			continue
		}
		if m.ID == threadID || m.ReplyTo == threadID {
			result = append(result, m)
		}
	}
	return result
}

// AllMessages returns a copy of all messages across all channels.
func (s *Store) AllMessages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Message, len(s.messages))
	copy(result, s.messages)
	return result
}

// NextMessageID returns a new unique message ID by incrementing the counter.
func (s *Store) NextMessageID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	return fmt.Sprintf("msg-%d", s.counter)
}

// IncrementMentions bumps MentionCount for all channel members except the sender.
// Used for DM (type D) and group DM (type G) channels where every message is a mention.
// For channels identified by slug that don't exist, this is a no-op (silent skip).
func (s *Store) IncrementMentions(channelSlug, senderSlug string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.bySlug[channelSlug]
	if !ok {
		return // silent skip per spec failure modes
	}
	if ch.Type != ChannelTypeDirect && ch.Type != ChannelTypeGroup {
		return // only DM/Group auto-mention all
	}

	for i := range s.members {
		if s.members[i].ChannelID == ch.ID && s.members[i].Slug != senderSlug {
			s.members[i].MentionCount++
		}
	}
}

// IncrementMentionsForTagged bumps MentionCount only for explicitly tagged members.
// Used for public channels where only @mentions trigger notifications.
func (s *Store) IncrementMentionsForTagged(channelSlug, senderSlug string, tagged []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.bySlug[channelSlug]
	if !ok {
		return
	}

	taggedSet := make(map[string]bool, len(tagged))
	for _, t := range tagged {
		taggedSet[t] = true
	}

	for i := range s.members {
		if s.members[i].ChannelID == ch.ID && s.members[i].Slug != senderSlug && taggedSet[s.members[i].Slug] {
			s.members[i].MentionCount++
		}
	}
}
