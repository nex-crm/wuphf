package team

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/slack-go/slack"
)

// slack_discover.go powers the onboarding "bring your other agents in" step: it
// lists the bot members already present in a bridged Slack channel — the "other
// AI agents" the office can coordinate — so the wizard can offer a one-click
// "Connect all" instead of making the user register each foreign bot by hand.
// Discovery is read-only; connecting a bot is the existing POST /slack/agents
// path (RegisterSlackAgent), so the membrane trust model is unchanged.
//
//	GET /slack/discover?channel_id=C… → { channel_id, bots: [ { user_id, name, … } ] }

// DiscoveredSlackBot is one bot member of a bridged channel — a candidate
// "other AI agent" to connect, or one already registered as a foreign agent.
type DiscoveredSlackBot struct {
	UserID            string `json:"user_id"`
	Name              string `json:"name"`
	RealName          string `json:"real_name,omitempty"`
	AlreadyRegistered bool   `json:"already_registered"`
	RegisteredSlug    string `json:"registered_slug,omitempty"`
}

// DiscoverChannelBots lists the bot members of a Slack channel, excluding this
// office's own coordinator bot. It paginates the conversation membership (Slack
// returns a cursor when the member list spans pages) and reads users.info to
// keep only bots — humans are co-workers, not agents to register. Each bot is
// marked with whether it is already a known office member (foreign or spawned)
// so "Connect all" can skip the ones already wired in.
func (t *SlackTransport) DiscoverChannelBots(ctx context.Context, channelID string) ([]DiscoveredSlackBot, error) {
	if t == nil || t.api == nil {
		return nil, fmt.Errorf("slack transport not configured")
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, fmt.Errorf("empty channel id")
	}

	// Page through the full membership — a >1-page channel must not silently
	// drop the agents past the first page.
	var memberIDs []string
	cursor := ""
	for {
		ids, next, err := t.api.GetUsersInConversationContext(ctx, &slack.GetUsersInConversationParameters{
			ChannelID: channelID,
			Limit:     200,
			Cursor:    cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("list members of %s: %w", channelID, err)
		}
		memberIDs = append(memberIDs, ids...)
		if strings.TrimSpace(next) == "" {
			break
		}
		cursor = next
	}

	out := []DiscoveredSlackBot{}
	seen := make(map[string]struct{}, len(memberIDs))
	for _, uid := range memberIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" || uid == t.botUserID {
			continue // skip blanks + WUPHF's own coordinator bot
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}

		user, err := t.api.GetUserInfoContext(ctx, uid)
		if err != nil || user == nil {
			continue // unresolvable member — omit rather than guess
		}
		if !user.IsBot {
			continue // humans are co-workers, not agents to register
		}

		slug := ""
		if t.Broker != nil {
			slug = t.Broker.slackKnownMemberSlugByUserID(uid)
		}
		out = append(out, DiscoveredSlackBot{
			UserID:            uid,
			Name:              slackDisplayName(user),
			RealName:          strings.TrimSpace(user.Profile.RealName),
			AlreadyRegistered: slug != "",
			RegisteredSlug:    slug,
		})
	}
	return out, nil
}

// handleSlackDiscover lists the bots in a bridged Slack channel for the
// onboarding "connect your other agents" step. The channel must already be
// bridged (its id matches a "slack" surface) — discovery is gated to connected
// channels, never an arbitrary-channel enumeration — and the transport must be
// running, since it reads the Slack Web API live.
//
//	GET /slack/discover?channel_id=C…
func (b *Broker) handleSlackDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	channelID := strings.TrimSpace(r.URL.Query().Get("channel_id"))
	if !isSlackChannelID(channelID) {
		http.Error(w, "slack channel_id required (C… or G…)", http.StatusBadRequest)
		return
	}
	if !b.slackChannelIsBridged(channelID) {
		http.Error(w, "channel is not connected — connect it first", http.StatusBadRequest)
		return
	}
	transport := b.slackTransportRef()
	if transport == nil {
		http.Error(w, "slack transport not running — finish connecting Slack first", http.StatusConflict)
		return
	}
	bots, err := transport.DiscoverChannelBots(r.Context(), channelID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel_id": channelID, "bots": bots})
}

// slackTransportRef returns the live Slack transport, or nil when none is
// running. Guarded by slackTransportMu (see broker_slack_transport.go).
func (b *Broker) slackTransportRef() *SlackTransport {
	b.slackTransportMu.Lock()
	defer b.slackTransportMu.Unlock()
	return b.slackTransport
}

// slackChannelIsBridged reports whether channelID is bound to an office channel
// via a "slack" surface — the gate that keeps discovery to connected channels.
func (b *Broker) slackChannelIsBridged(channelID string) bool {
	for _, ch := range b.SurfaceChannels("slack") {
		if ch.Surface != nil && ch.Surface.RemoteID == channelID {
			return true
		}
	}
	return false
}

// slackKnownMemberSlugByUserID resolves a Slack user id to the slug of ANY office
// member bound to it — foreign (Kind == slack) OR spawned (own runtime + Slack
// identity) — or "" if none. Broader than SlackAgentSlugByUserID (foreign-only):
// discovery uses it so a bot that is already wired in either way is shown as
// connected and skipped by "Connect all".
func (b *Broker) slackKnownMemberSlugByUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.members {
		m := &b.members[i]
		if m.Provider.Slack != nil && m.Provider.Slack.UserID == userID {
			return m.Slug
		}
	}
	return ""
}
