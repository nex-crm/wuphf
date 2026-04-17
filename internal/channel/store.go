// internal/channel/store.go
package channel

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store is the authoritative store for channels, members, and messages.
// It maintains in-memory indexes for O(1) lookups on hot paths.
type Store struct {
	channels []Channel
	members  []ChannelMember
	messages []Message
	counter  int

	// In-memory indexes (maintained on write)
	byID     map[string]*Channel // channel UUID → *Channel
	bySlug   map[string]*Channel // channel slug → *Channel
	memberOf map[string][]string // member slug → []channel IDs

	mu sync.RWMutex
}

// NewStore constructs an empty Store with initialized indexes.
func NewStore() *Store {
	return &Store{
		channels: []Channel{},
		members:  []ChannelMember{},
		messages: []Message{},
		byID:     make(map[string]*Channel),
		bySlug:   make(map[string]*Channel),
		memberOf: make(map[string][]string),
	}
}

// now returns the current time as RFC3339 string.
func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Create adds a new channel to the store, generating a UUID and timestamps.
func (s *Store) Create(ch Channel) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch.Slug == "" {
		return nil, fmt.Errorf("channel slug is required")
	}
	if _, exists := s.bySlug[ch.Slug]; exists {
		return nil, fmt.Errorf("channel with slug %q already exists", ch.Slug)
	}

	ch.ID = uuid.NewString()
	ts := now()
	ch.CreatedAt = ts
	ch.UpdatedAt = ts

	s.channels = append(s.channels, ch)
	s.rebuildIndexes()

	return s.byID[ch.ID], nil
}

// Get returns a channel by UUID.
func (s *Store) Get(id string) (*Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.byID[id]
	return ch, ok
}

// GetBySlug returns a channel by slug.
func (s *Store) GetBySlug(slug string) (*Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.bySlug[slug]
	return ch, ok
}

// List returns channels matching the filter. An empty filter returns all channels.
func (s *Store) List(filter ChannelFilter) []Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var memberChannelIDs map[string]bool
	if filter.Member != "" {
		ids := s.memberOf[filter.Member]
		memberChannelIDs = make(map[string]bool, len(ids))
		for _, id := range ids {
			memberChannelIDs[id] = true
		}
	}

	var result []Channel
	for _, ch := range s.channels {
		if filter.Type != "" && ch.Type != filter.Type {
			continue
		}
		if filter.Member != "" && !memberChannelIDs[ch.ID] {
			continue
		}
		result = append(result, ch)
	}
	return result
}

// Delete removes a channel and its members from the store.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.byID[id]
	if !ok {
		return fmt.Errorf("channel %q not found", id)
	}

	slug := ch.Slug
	delete(s.byID, id)
	delete(s.bySlug, slug)

	// Remove from channels slice
	filtered := make([]Channel, 0, len(s.channels)-1)
	for _, c := range s.channels {
		if c.ID != id {
			filtered = append(filtered, c)
		}
	}
	s.channels = filtered

	// Remove associated members
	filteredMembers := make([]ChannelMember, 0, len(s.members))
	for _, m := range s.members {
		if m.ChannelID != id {
			filteredMembers = append(filteredMembers, m)
		}
	}
	s.members = filteredMembers

	s.rebuildIndexes()
	return nil
}

// GetOrCreateDirect returns the DM channel between two members, creating it if absent.
// The channel slug is deterministic: DirectSlug(a, b). Both members get NotifyLevel "all".
// Returns an error if either slug is empty or if a == b (self-DM).
func (s *Store) GetOrCreateDirect(a, b string) (*Channel, error) {
	if a == "" || b == "" {
		return nil, fmt.Errorf("GetOrCreateDirect: member slugs must not be empty")
	}
	if a == b {
		return nil, fmt.Errorf("GetOrCreateDirect: cannot create DM with self (%q)", a)
	}
	slug := DirectSlug(a, b)

	s.mu.RLock()
	existing, ok := s.bySlug[slug]
	s.mu.RUnlock()
	if ok {
		return existing, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check under write lock
	if existing, ok = s.bySlug[slug]; ok {
		return existing, nil
	}

	ch := Channel{
		ID:        uuid.NewString(),
		Name:      "DM: " + a + " & " + b,
		Slug:      slug,
		Type:      ChannelTypeDirect,
		CreatedBy: a,
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	s.channels = append(s.channels, ch)
	s.addMemberLocked(ch.ID, a, "all")
	s.addMemberLocked(ch.ID, b, "all")
	s.rebuildIndexes()

	return s.byID[ch.ID], nil
}

// GetOrCreateGroup returns the group DM channel for the given members, creating if absent.
func (s *Store) GetOrCreateGroup(members []string, createdBy string) (*Channel, error) {
	slug := GroupSlug(members)

	s.mu.RLock()
	existing, ok := s.bySlug[slug]
	s.mu.RUnlock()
	if ok {
		return existing, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok = s.bySlug[slug]; ok {
		return existing, nil
	}

	ch := Channel{
		ID:        uuid.NewString(),
		Name:      "Group DM",
		Slug:      slug,
		Type:      ChannelTypeGroup,
		CreatedBy: createdBy,
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	s.channels = append(s.channels, ch)
	for _, m := range members {
		s.addMemberLocked(ch.ID, m, "all")
	}
	s.rebuildIndexes()

	return s.byID[ch.ID], nil
}

// FindDirectByMembers returns the DM channel between two members if it exists.
func (s *Store) FindDirectByMembers(a, b string) (*Channel, bool) {
	slug := DirectSlug(a, b)
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.bySlug[slug]
	if !ok || ch.Type != ChannelTypeDirect {
		return nil, false
	}
	return ch, true
}

// OtherMember returns the other member in a DM channel.
func (s *Store) OtherMember(channelID, slug string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ch, ok := s.byID[channelID]
	if !ok || ch.Type != ChannelTypeDirect {
		return "", false
	}

	for _, m := range s.members {
		if m.ChannelID == channelID && m.Slug != slug {
			return m.Slug, true
		}
	}
	return "", false
}

// AddMember adds a member to a channel.
func (s *Store) AddMember(channelID, slug, notifyLevel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[channelID]; !ok {
		return fmt.Errorf("channel %q not found", channelID)
	}
	// Check if already a member
	for _, m := range s.members {
		if m.ChannelID == channelID && m.Slug == slug {
			return nil // idempotent
		}
	}
	s.addMemberLocked(channelID, slug, notifyLevel)
	return nil
}

// addMemberLocked must be called with write lock held.
func (s *Store) addMemberLocked(channelID, slug, notifyLevel string) {
	m := ChannelMember{
		ChannelID:   channelID,
		Slug:        slug,
		NotifyLevel: notifyLevel,
		JoinedAt:    now(),
	}
	s.members = append(s.members, m)
	s.memberOf[slug] = append(s.memberOf[slug], channelID)
}

// RemoveMember removes a member from a channel.
func (s *Store) RemoveMember(channelID, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.members[:0]
	found := false
	for _, m := range s.members {
		if m.ChannelID == channelID && m.Slug == slug {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}
	if !found {
		return fmt.Errorf("member %q not in channel %q", slug, channelID)
	}
	s.members = filtered

	ids := s.memberOf[slug]
	newIDs := ids[:0]
	for _, id := range ids {
		if id != channelID {
			newIDs = append(newIDs, id)
		}
	}
	s.memberOf[slug] = newIDs

	return nil
}

// Members returns all members of a channel.
func (s *Store) Members(channelID string) []ChannelMember {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ChannelMember
	for _, m := range s.members {
		if m.ChannelID == channelID {
			result = append(result, m)
		}
	}
	return result
}

// IsMember returns true if slug is a member of the channel.
func (s *Store) IsMember(channelID, slug string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, id := range s.memberOf[slug] {
		if id == channelID {
			return true
		}
	}
	return false
}

// IsMemberBySlug checks if a slug is a member of the channel identified by channelSlug.
func (s *Store) IsMemberBySlug(channelSlug, memberSlug string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ch, ok := s.bySlug[channelSlug]
	if !ok {
		return false
	}
	for _, id := range s.memberOf[memberSlug] {
		if id == ch.ID {
			return true
		}
	}
	return false
}

// MemberChannels returns all channels a member belongs to.
func (s *Store) MemberChannels(slug string) []Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.memberOf[slug]
	result := make([]Channel, 0, len(ids))
	for _, id := range ids {
		if ch, ok := s.byID[id]; ok {
			result = append(result, *ch)
		}
	}
	return result
}

// IsDirectMessage returns true if channelID is a DM channel.
func (s *Store) IsDirectMessage(channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.byID[channelID]
	return ok && ch.Type == ChannelTypeDirect
}

// IsGroupMessage returns true if channelID is a group DM channel.
func (s *Store) IsGroupMessage(channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.byID[channelID]
	return ok && ch.Type == ChannelTypeGroup
}

// IsDirectMessageBySlug returns true if the given slug identifies a DM channel.
// Unlike IsDirectMessage (which takes a UUID), this checks by slug.
func (s *Store) IsDirectMessageBySlug(slug string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.bySlug[slug]
	return ok && ch.Type == ChannelTypeDirect
}

// OtherMemberBySlug returns the other member in a DM channel identified by slug.
func (s *Store) OtherMemberBySlug(channelSlug, memberSlug string) (string, bool) {
	s.mu.RLock()
	ch, ok := s.bySlug[channelSlug]
	if !ok || ch.Type != ChannelTypeDirect {
		s.mu.RUnlock()
		return "", false
	}
	channelID := ch.ID
	s.mu.RUnlock()
	return s.OtherMember(channelID, memberSlug)
}

// storeJSON is the on-disk representation of the store.
type storeJSON struct {
	Channels []Channel       `json:"channels"`
	Members  []ChannelMember `json:"members"`
	Messages []Message       `json:"messages"`
	Counter  int             `json:"counter"`
}

// MarshalJSON serializes the store for persistence.
func (s *Store) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.Marshal(storeJSON{
		Channels: s.channels,
		Members:  s.members,
		Messages: s.messages,
		Counter:  s.counter,
	})
}

// UnmarshalJSON deserializes the store and rebuilds indexes.
func (s *Store) UnmarshalJSON(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var sj storeJSON
	if err := json.Unmarshal(data, &sj); err != nil {
		return err
	}

	s.channels = sj.Channels
	s.members = sj.Members
	s.messages = sj.Messages
	s.counter = sj.Counter

	if s.channels == nil {
		s.channels = []Channel{}
	}
	if s.members == nil {
		s.members = []ChannelMember{}
	}
	if s.messages == nil {
		s.messages = []Message{}
	}

	s.rebuildIndexes()
	return nil
}

// rebuildIndexes reconstructs byID, bySlug, and memberOf from slices.
// Must be called with write lock held (or before concurrent access).
func (s *Store) rebuildIndexes() {
	s.byID = make(map[string]*Channel, len(s.channels))
	s.bySlug = make(map[string]*Channel, len(s.channels))
	s.memberOf = make(map[string][]string)

	for i := range s.channels {
		ch := &s.channels[i]
		if ch.ID != "" {
			s.byID[ch.ID] = ch
		}
		if ch.Slug != "" {
			s.bySlug[ch.Slug] = ch
		}
	}

	for _, m := range s.members {
		s.memberOf[m.Slug] = append(s.memberOf[m.Slug], m.ChannelID)
	}
}
