package chat

import (
	"strings"
)

// Router parses @mentions and routes messages to appropriate channels.
type Router struct {
	channels *ChannelManager
	messages *MessageStore
}

// NewRouter creates a router backed by the given channel and message stores.
func NewRouter(channels *ChannelManager, messages *MessageStore) *Router {
	return &Router{
		channels: channels,
		messages: messages,
	}
}

// RouteResult describes where a message was delivered.
type RouteResult struct {
	ChannelID string
	Mentions  []string
}

// Route sends a message from sender to the appropriate channel.
// If the content contains @mentions, it routes to a DM channel with the first mentioned slug.
// Otherwise it routes to the "general" public channel.
func (r *Router) Route(senderSlug, senderName, content string) (RouteResult, error) {
	mentions := parseMentions(content)

	if len(mentions) > 0 {
		target := mentions[0]
		ch, err := r.channels.GetOrCreateDM(senderSlug, target)
		if err != nil {
			return RouteResult{}, err
		}
		if _, err := r.messages.Send(ch.ID, senderSlug, senderName, content, MsgText); err != nil {
			return RouteResult{}, err
		}
		return RouteResult{ChannelID: ch.ID, Mentions: mentions}, nil
	}

	// Default: general channel.
	ch, err := r.getOrCreateGeneral()
	if err != nil {
		return RouteResult{}, err
	}
	if _, err := r.messages.Send(ch.ID, senderSlug, senderName, content, MsgText); err != nil {
		return RouteResult{}, err
	}
	return RouteResult{ChannelID: ch.ID, Mentions: nil}, nil
}

// getOrCreateGeneral returns the #general public channel, creating it if needed.
func (r *Router) getOrCreateGeneral() (Channel, error) {
	for _, ch := range r.channels.List() {
		if ch.Type == ChannelPublic && ch.Name == "general" {
			return ch, nil
		}
	}
	return r.channels.Create("general", ChannelPublic, nil)
}

// parseMentions extracts @slug tokens from content.
func parseMentions(content string) []string {
	var mentions []string
	seen := make(map[string]bool)
	for _, word := range strings.Fields(content) {
		if strings.HasPrefix(word, "@") {
			slug := strings.TrimPrefix(word, "@")
			// Strip trailing punctuation.
			slug = strings.TrimRight(slug, ".,!?;:")
			if slug != "" && !seen[slug] {
				mentions = append(mentions, slug)
				seen[slug] = true
			}
		}
	}
	return mentions
}
