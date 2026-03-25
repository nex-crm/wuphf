// Package chat implements a Slack-style channel and message system for agent-to-agent communication.
package chat

import "time"

// ChannelType describes the kind of channel.
type ChannelType string

const (
	ChannelDirect ChannelType = "direct"
	ChannelGroup  ChannelType = "group"
	ChannelPublic ChannelType = "public"
)

// MessageType describes the kind of message.
type MessageType string

const (
	MsgText   MessageType = "text"
	MsgSystem MessageType = "system"
	MsgTool   MessageType = "tool"
)

// Channel represents a communication channel between agents.
type Channel struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      ChannelType `json:"type"`
	Members   []string    `json:"members"`
	CreatedAt time.Time   `json:"created_at"`
}

// Message represents a single message in a channel.
type Message struct {
	ID         string      `json:"id"`
	ChannelID  string      `json:"channel_id"`
	SenderSlug string      `json:"sender_slug"`
	SenderName string      `json:"sender_name"`
	Content    string      `json:"content"`
	Timestamp  time.Time   `json:"timestamp"`
	Type       MessageType `json:"type"`
}
