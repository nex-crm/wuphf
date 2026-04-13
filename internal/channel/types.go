// internal/channel/types.go
package channel

// ChannelType identifies the kind of channel (Mattermost-aligned).
type ChannelType string

const (
	ChannelTypePublic ChannelType = "O" // Public channels (general, engineering)
	ChannelTypeDirect ChannelType = "D" // 1:1 DMs (human + one agent)
	ChannelTypeGroup  ChannelType = "G" // Group DMs (human + N agents)
)

// Channel represents a communication channel.
type Channel struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	Type        ChannelType `json:"type"`
	CreatedBy   string      `json:"created_by,omitempty"`
	CreatedAt   string      `json:"created_at,omitempty"`
	UpdatedAt   string      `json:"updated_at,omitempty"`
	LastPostAt  string      `json:"last_post_at,omitempty"`
	Description string      `json:"description,omitempty"`
}

// ChannelMember tracks a member's relationship to a channel.
type ChannelMember struct {
	ChannelID       string `json:"channel_id"`
	Slug            string `json:"slug"`
	Role            string `json:"role,omitempty"`
	LastReadID      string `json:"last_read_id,omitempty"`
	LastProcessedID string `json:"last_processed_id,omitempty"`
	MentionCount    int    `json:"mention_count"`
	NotifyLevel     string `json:"notify_level"`
	JoinedAt        string `json:"joined_at,omitempty"`
}

// ChannelFilter constrains Store.List results.
type ChannelFilter struct {
	Type   ChannelType // empty = all
	Member string      // empty = all; set = only channels containing this member
}

// Message represents a single message in a channel.
// Moved from internal/team/broker.go channelMessage.
type Message struct {
	ID          string            `json:"id"`
	From        string            `json:"from"`
	Channel     string            `json:"channel,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Source      string            `json:"source,omitempty"`
	SourceLabel string            `json:"source_label,omitempty"`
	EventID     string            `json:"event_id,omitempty"`
	Title       string            `json:"title,omitempty"`
	Content     string            `json:"content"`
	Tagged      []string          `json:"tagged"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Timestamp   string            `json:"timestamp"`
	Reactions   []MessageReaction `json:"reactions,omitempty"`
}

// MessageReaction represents a reaction to a message.
type MessageReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}
