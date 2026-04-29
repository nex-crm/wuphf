package channelui

// BrokerReaction is a single emoji reaction on a broker message.
type BrokerReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}

// BrokerMessageUsage counts the LLM tokens (and cache hits) attributed
// to a single message. All fields are optional; zero values mean the
// broker did not report that dimension for the message.
type BrokerMessageUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	TotalTokens         int `json:"total_tokens,omitempty"`
}

// BrokerMessage is a single message record as the broker returns it.
// The shape mirrors the broker's JSON contract so it round-trips
// directly through encoding/json without an intermediate DTO.
type BrokerMessage struct {
	ID          string              `json:"id"`
	From        string              `json:"from"`
	Kind        string              `json:"kind,omitempty"`
	Source      string              `json:"source,omitempty"`
	SourceLabel string              `json:"source_label,omitempty"`
	EventID     string              `json:"event_id,omitempty"`
	Title       string              `json:"title,omitempty"`
	Content     string              `json:"content"`
	Tagged      []string            `json:"tagged"`
	ReplyTo     string              `json:"reply_to"`
	Timestamp   string              `json:"timestamp"`
	Usage       *BrokerMessageUsage `json:"usage,omitempty"`
	Reactions   []BrokerReaction    `json:"reactions,omitempty"`
}

// RenderedLine is a single line of pre-styled output destined for the
// main panel. The metadata fields (ThreadID/TaskID/…) let the mouse
// layer route a click on the line back to the underlying entity.
type RenderedLine struct {
	Text        string
	ThreadID    string
	TaskID      string
	RequestID   string
	AgentSlug   string
	PromptValue string
}

// ThreadedMessage decorates a BrokerMessage with the structural context
// the thread-view renderer needs: how deep in the reply chain it sits,
// the human-readable label of its parent, and whether the renderer has
// chosen to collapse its descendants behind a "+N hidden" affordance.
type ThreadedMessage struct {
	Message            BrokerMessage
	Depth              int
	ParentLabel        string
	Collapsed          bool
	HiddenReplies      int
	ThreadParticipants []string
}
