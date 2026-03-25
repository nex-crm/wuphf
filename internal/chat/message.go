package chat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MessageStore manages per-channel JSONL message files.
type MessageStore struct {
	mu      sync.Mutex
	baseDir string
}

// NewMessageStore creates a store rooted at ~/.wuphf/chat/.
func NewMessageStore() (*MessageStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return NewMessageStoreAt(filepath.Join(home, ".wuphf", "chat")), nil
}

// NewMessageStoreAt creates a store rooted at a specific directory.
func NewMessageStoreAt(baseDir string) *MessageStore {
	return &MessageStore{baseDir: baseDir}
}

// msgPath returns the JSONL file path for a channel.
func (ms *MessageStore) msgPath(channelID string) string {
	return filepath.Join(ms.baseDir, "msg-"+channelID+".jsonl")
}

// Send appends a message to a channel and returns the stored message.
func (ms *MessageStore) Send(channelID, senderSlug, senderName, content string, msgType MessageType) (Message, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	msg := Message{
		ID:         uuid.NewString(),
		ChannelID:  channelID,
		SenderSlug: senderSlug,
		SenderName: senderName,
		Content:    content,
		Timestamp:  time.Now(),
		Type:       msgType,
	}

	if err := os.MkdirAll(ms.baseDir, 0o700); err != nil {
		return Message{}, fmt.Errorf("create message dir: %w", err)
	}

	f, err := os.OpenFile(ms.msgPath(channelID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Message{}, fmt.Errorf("open message file: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(msg)
	if err != nil {
		return Message{}, fmt.Errorf("marshal message: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
		return Message{}, fmt.Errorf("write message: %w", err)
	}

	return msg, nil
}

// SendSystem is a convenience for sending a system message.
func (ms *MessageStore) SendSystem(channelID, content string) (Message, error) {
	return ms.Send(channelID, "system", "System", content, MsgSystem)
}

// List reads the most recent messages from a channel.
// limit <= 0 returns all messages.
func (ms *MessageStore) List(channelID string, limit int) ([]Message, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	f, err := os.Open(ms.msgPath(channelID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open message file: %w", err)
	}
	defer f.Close()

	var messages []Message
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return nil, fmt.Errorf("parse message: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan message file: %w", err)
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	return messages, nil
}
