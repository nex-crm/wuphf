package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type claudeSessionRecord struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

type claudeSessionStore struct {
	mu       sync.Mutex
	path     string
	sessions map[string]claudeSessionRecord
}

var (
	claudeSessionStoreMu       sync.Mutex
	claudeSessionStoreInstance *claudeSessionStore
	claudeSessionStoreFactory  = func() *claudeSessionStore {
		return newClaudeSessionStore()
	}
)

func getClaudeSessionStore() *claudeSessionStore {
	claudeSessionStoreMu.Lock()
	defer claudeSessionStoreMu.Unlock()

	if claudeSessionStoreInstance == nil {
		claudeSessionStoreInstance = claudeSessionStoreFactory()
	}
	return claudeSessionStoreInstance
}

// ResetClaudeSessions clears all persisted Claude resume state.
func ResetClaudeSessions() error {
	claudeSessionStoreMu.Lock()
	store := claudeSessionStoreInstance
	if store == nil {
		store = claudeSessionStoreFactory()
		claudeSessionStoreInstance = store
	}
	claudeSessionStoreMu.Unlock()

	return store.clearAll()
}

func newClaudeSessionStore() *claudeSessionStore {
	home, err := os.UserHomeDir()
	if err != nil {
		return newClaudeSessionStoreAt("")
	}
	return newClaudeSessionStoreAt(filepath.Join(home, ".wuphf", "providers", "claude-sessions.json"))
}

func newClaudeSessionStoreAt(path string) *claudeSessionStore {
	store := &claudeSessionStore{
		path:     path,
		sessions: make(map[string]claudeSessionRecord),
	}
	store.load()
	return store
}

func (s *claudeSessionStore) resumeSessionID(agentSlug string, cwd string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.sessions[agentSlug]
	if !ok || record.SessionID == "" {
		return ""
	}
	if record.Cwd != "" && cwd != "" && record.Cwd != cwd {
		return ""
	}
	return record.SessionID
}

func (s *claudeSessionStore) save(agentSlug string, sessionID string, cwd string) {
	if s == nil || agentSlug == "" || sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[agentSlug] = claudeSessionRecord{
		SessionID: sessionID,
		Cwd:       cwd,
		UpdatedAt: time.Now().UnixMilli(),
	}
	s.persist()
}

func (s *claudeSessionStore) clear(agentSlug string) {
	if s == nil || agentSlug == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, agentSlug)
	s.persist()
}

func (s *claudeSessionStore) clearAll() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]claudeSessionRecord)
	if s.path == "" {
		return nil
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *claudeSessionStore) load() {
	if s == nil || s.path == "" {
		return
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}

	loaded := make(map[string]claudeSessionRecord)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	s.sessions = loaded
}

func (s *claudeSessionStore) persist() {
	if s == nil || s.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = os.WriteFile(s.path, data, 0o600)
}
