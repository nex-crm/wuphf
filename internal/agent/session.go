package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SessionStore manages JSONL-based session files for agents.
type SessionStore struct {
	baseDir string
}

// NewSessionStore creates a store rooted at ~/.wuphf/sessions by default.
func NewSessionStore() (*SessionStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return &SessionStore{baseDir: filepath.Join(home, ".wuphf", "sessions")}, nil
}

// NewSessionStoreAt creates a store rooted at a specific directory (useful for tests).
func NewSessionStoreAt(baseDir string) *SessionStore {
	return &SessionStore{baseDir: baseDir}
}

// sessionPath returns the JSONL file path for a given session ID.
// Session ID format: "<agentSlug>_<uuid>"
func (s *SessionStore) sessionPath(sessionID string) (string, error) {
	slug, err := slugFromID(sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.baseDir, slug, sessionID+".jsonl"), nil
}

// slugFromID extracts the agent slug from a session ID.
func slugFromID(sessionID string) (string, error) {
	idx := strings.LastIndex(sessionID, "_")
	if idx <= 0 {
		return "", fmt.Errorf("invalid session ID format: %q", sessionID)
	}
	return sessionID[:idx], nil
}

// Create creates a new empty session file and returns the session ID.
func (s *SessionStore) Create(agentSlug string) (string, error) {
	sessionID := agentSlug + "_" + uuid.NewString()
	dir := filepath.Join(s.baseDir, agentSlug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("create session file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close session file: %w", err)
	}
	return sessionID, nil
}

// Append writes a new entry to the session file and returns the stored entry.
func (s *SessionStore) Append(sessionID string, entry SessionEntry) (SessionEntry, error) {
	path, err := s.sessionPath(sessionID)
	if err != nil {
		return SessionEntry{}, err
	}

	entry.ID = uuid.NewString()
	entry.Timestamp = time.Now().UnixMilli()

	line, err := json.Marshal(entry)
	if err != nil {
		return SessionEntry{}, fmt.Errorf("marshal entry: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return SessionEntry{}, fmt.Errorf("open session file: %w", err)
	}

	if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
		_ = f.Close()
		return SessionEntry{}, fmt.Errorf("write entry: %w", err)
	}
	if err := f.Close(); err != nil {
		return SessionEntry{}, fmt.Errorf("close session file: %w", err)
	}

	return entry, nil
}

// GetHistory reads all entries from a session, with optional filtering.
func (s *SessionStore) GetHistory(sessionID string, limit int, fromID string) ([]SessionEntry, error) {
	path, err := s.sessionPath(sessionID)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []SessionEntry
	scanner := bufio.NewScanner(f)
	collecting := fromID == ""

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry SessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse entry: %w", err)
		}
		if !collecting {
			if entry.ID == fromID {
				collecting = true
			}
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session file: %w", err)
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	return entries, nil
}

// Branch creates a new session, copying history up to and including fromEntryID.
func (s *SessionStore) Branch(sessionID, fromEntryID string) (string, error) {
	slug, err := slugFromID(sessionID)
	if err != nil {
		return "", err
	}

	path, err := s.sessionPath(sessionID)
	if err != nil {
		return "", err
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open source session: %w", err)
	}
	defer func() { _ = f.Close() }()

	var toCopy []SessionEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry SessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return "", fmt.Errorf("parse entry: %w", err)
		}
		toCopy = append(toCopy, entry)
		if entry.ID == fromEntryID {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan source session: %w", err)
	}

	newID := slug + "_" + uuid.NewString()
	dir := filepath.Join(s.baseDir, slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create branch dir: %w", err)
	}

	newPath := filepath.Join(dir, newID+".jsonl")
	out, err := os.OpenFile(newPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("create branch file: %w", err)
	}

	for _, entry := range toCopy {
		line, err := json.Marshal(entry)
		if err != nil {
			_ = out.Close()
			return "", fmt.Errorf("marshal branch entry: %w", err)
		}
		if _, err := fmt.Fprintf(out, "%s\n", line); err != nil {
			_ = out.Close()
			return "", fmt.Errorf("write branch entry: %w", err)
		}
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close branch file: %w", err)
	}

	return newID, nil
}

// ListSessions returns all session IDs for the given agent slug.
func (s *SessionStore) ListSessions(agentSlug string) ([]string, error) {
	dir := filepath.Join(s.baseDir, agentSlug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".jsonl"))
	}
	return ids, nil
}
