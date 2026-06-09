package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// broker_slack_connect.go is the Slack equivalent of broker_telegram_connect.go:
// it binds a Slack channel (C…/G…) to an office channel carrying a "slack"
// surface, so the SlackTransport — which maps broker.SurfaceChannels("slack") to
// office slugs — has a channel to bridge. Without a connected channel the
// transport skips silently at boot (see launcher_transports.go).
//
//	POST /slack/connect { channel_id, name } → { channel_slug, name }

var errSlackChannelAlreadyBridges = errors.New("channel already bridges a different slack channel")

type slackConnectRequest struct {
	ChannelID string `json:"channel_id,omitempty"`
	Name      string `json:"name,omitempty"`
}

// handleSlackConnect binds a Slack channel id to an office channel. The bot must
// already be invited to the Slack channel; this only records the binding.
func (b *Broker) handleSlackConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body slackConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channelID := strings.TrimSpace(body.ChannelID)
	if !isSlackChannelID(channelID) {
		http.Error(w, "slack channel_id required (C… or G…)", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = channelID
	}

	ch, err := b.createSlackChannel(channelID, name)
	if err != nil && !errors.Is(err, errChannelAlreadyExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel_slug": ch.Slug, "name": ch.Name})
}

// createSlackChannel binds channelID to an office channel with a "slack" surface.
// If a channel with the derived slug already exists it must already bridge the
// same Slack channel id (idempotent reconnect); otherwise the call errors.
// Returns errChannelAlreadyExists (wrapped) with the existing channel on an
// idempotent reconnect so the handler can treat it as success.
func (b *Broker) createSlackChannel(channelID, name string) (*teamChannel, error) {
	slug := slackChannelSlug(name)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Every adopted office member joins (ceo is prepended by createChannelLocked).
	members := make([]string, 0, len(b.members))
	for _, m := range b.members {
		if m.Slug != "" && m.Slug != "ceo" {
			members = append(members, m.Slug)
		}
	}

	if existing := b.findChannelLocked(slug); existing != nil {
		if existing.Surface == nil || existing.Surface.Provider != "slack" {
			return existing, fmt.Errorf("%w: %q exists but is not a slack channel", errSlackChannelAlreadyBridges, slug)
		}
		if existing.Surface.RemoteID != "" && existing.Surface.RemoteID != channelID {
			return existing, fmt.Errorf("%w: %q already bridges slack channel %s", errSlackChannelAlreadyBridges, slug, existing.Surface.RemoteID)
		}
		return existing, errChannelAlreadyExists
	}

	ch, cerr := b.createChannelLocked(channelCreateInput{
		Slug:        slug,
		Name:        name,
		Description: fmt.Sprintf("Slack bridge for %s.", name),
		Members:     members,
		CreatedBy:   "you",
		Surface: &channelSurface{
			Provider:    "slack",
			RemoteID:    channelID,
			RemoteTitle: name,
			BotTokenEnv: "SLACK_BOT_TOKEN",
		},
	})
	if cerr != nil {
		// Return a plain error, not the typed-nil *channelCreateError, to avoid
		// the non-nil-interface gotcha on the success path below.
		return nil, cerr
	}
	return ch, nil
}

// isSlackChannelID reports whether s looks like a Slack channel/group id.
func isSlackChannelID(s string) bool {
	if len(s) < 2 {
		return false
	}
	switch s[0] {
	case 'C', 'G':
		return true
	default:
		return false
	}
}

// slackChannelSlug derives a stable office slug from a Slack channel name,
// prefixed "slack-" so Slack channels are visually grouped and never collide
// with a same-named native channel.
func slackChannelSlug(name string) string {
	s := normalizeChannelSlug(name)
	if s == "" {
		s = "channel"
	}
	if !strings.HasPrefix(s, "slack-") {
		s = "slack-" + s
	}
	return s
}
