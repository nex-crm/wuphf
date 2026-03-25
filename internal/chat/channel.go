package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ChannelManager manages channels with in-memory state and JSON file persistence.
type ChannelManager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	filePath string
}

// NewChannelManager creates a manager that persists to ~/.wuphf/chat/channels.json.
func NewChannelManager() (*ChannelManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return NewChannelManagerAt(filepath.Join(home, ".wuphf", "chat", "channels.json"))
}

// NewChannelManagerAt creates a manager persisting to a specific file path.
func NewChannelManagerAt(filePath string) (*ChannelManager, error) {
	cm := &ChannelManager{
		channels: make(map[string]Channel),
		filePath: filePath,
	}
	if err := cm.load(); err != nil {
		return nil, err
	}
	return cm, nil
}

// Create adds a new channel and persists to disk.
func (cm *ChannelManager) Create(name string, chType ChannelType, members []string) (Channel, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	ch := Channel{
		ID:        uuid.NewString(),
		Name:      name,
		Type:      chType,
		Members:   members,
		CreatedAt: time.Now(),
	}
	cm.channels[ch.ID] = ch
	if err := cm.save(); err != nil {
		delete(cm.channels, ch.ID)
		return Channel{}, err
	}
	return ch, nil
}

// List returns all channels sorted by creation time.
func (cm *ChannelManager) List() []Channel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	list := make([]Channel, 0, len(cm.channels))
	for _, ch := range cm.channels {
		list = append(list, ch)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list
}

// Get returns a channel by ID.
func (cm *ChannelManager) Get(id string) (Channel, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ch, ok := cm.channels[id]
	return ch, ok
}

// Delete removes a channel by ID and persists.
func (cm *ChannelManager) Delete(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, ok := cm.channels[id]; !ok {
		return fmt.Errorf("channel %q not found", id)
	}
	delete(cm.channels, id)
	return cm.save()
}

// GetOrCreateDM returns an existing DM channel between two slugs, or creates one.
func (cm *ChannelManager) GetOrCreateDM(slug1, slug2 string) (Channel, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Normalize order for consistent lookup.
	a, b := slug1, slug2
	if a > b {
		a, b = b, a
	}

	for _, ch := range cm.channels {
		if ch.Type != ChannelDirect || len(ch.Members) != 2 {
			continue
		}
		m0, m1 := ch.Members[0], ch.Members[1]
		if m0 > m1 {
			m0, m1 = m1, m0
		}
		if m0 == a && m1 == b {
			return ch, nil
		}
	}

	ch := Channel{
		ID:        uuid.NewString(),
		Name:      dmName(a, b),
		Type:      ChannelDirect,
		Members:   []string{a, b},
		CreatedAt: time.Now(),
	}
	cm.channels[ch.ID] = ch
	if err := cm.save(); err != nil {
		delete(cm.channels, ch.ID)
		return Channel{}, err
	}
	return ch, nil
}

// dmName generates a predictable DM channel name.
func dmName(a, b string) string {
	return "dm-" + strings.Join([]string{a, b}, "-")
}

// load reads channels from the JSON file. Missing file is treated as empty.
func (cm *ChannelManager) load() error {
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read channels file: %w", err)
	}
	var list []Channel
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse channels file: %w", err)
	}
	for _, ch := range list {
		cm.channels[ch.ID] = ch
	}
	return nil
}

// save writes all channels to disk as a JSON array.
func (cm *ChannelManager) save() error {
	if err := os.MkdirAll(filepath.Dir(cm.filePath), 0o700); err != nil {
		return fmt.Errorf("create chat dir: %w", err)
	}
	list := make([]Channel, 0, len(cm.channels))
	for _, ch := range cm.channels {
		list = append(list, ch)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal channels: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(cm.filePath, data, 0o600)
}
