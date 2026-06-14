package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/nex-crm/wuphf/internal/company"
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
	// Persist the binding to company.yaml so it survives a restart — otherwise
	// the transport, which only starts when a slack surface channel exists at
	// boot, would silently skip after every restart (mirrors the Telegram flow).
	syncManifestForSlackChannel(ch.Slug, channelID, name)

	// Hot-start: bring the Slack transport up in-process now that a channel is
	// bound, so the connection is live without a broker re-exec. Idempotent — if
	// the transport is already running it just refreshes the live channel map to
	// include this newly-connected channel. createSlackChannel has already
	// released b.mu, so this call is safe (EnsureSlackTransportRunning reads
	// surface channels under b.mu itself).
	b.EnsureSlackTransportRunning()

	writeJSON(w, http.StatusOK, map[string]any{"channel_slug": ch.Slug, "name": ch.Name})
}

// syncManifestForSlackChannel appends the slack-bridged channel to company.yaml
// so a future restart re-reads it. Best-effort: the in-memory channel is already
// created and persisted via saveLocked; a manifest failure is logged, not fatal.
func syncManifestForSlackChannel(slug, channelID, name string) {
	err := company.UpdateManifest(func(manifest *company.Manifest) error {
		for _, ch := range manifest.Channels {
			if ch.Slug == slug {
				return nil
			}
		}
		members := make([]string, 0, len(manifest.Members)+1)
		if manifest.Lead != "" {
			members = append(members, manifest.Lead)
		}
		for _, m := range manifest.Members {
			if m.Slug != "" && m.Slug != manifest.Lead {
				members = append(members, m.Slug)
			}
		}
		manifest.Channels = append(manifest.Channels, company.ChannelSpec{
			Slug:        slug,
			Name:        name,
			Description: fmt.Sprintf("Slack bridge for %s.", name),
			Members:     members,
			Surface: &company.ChannelSurfaceSpec{
				Provider:    "slack",
				RemoteID:    channelID,
				RemoteTitle: name,
				BotTokenEnv: "SLACK_BOT_TOKEN",
			},
		})
		return nil
	})
	if err != nil {
		log.Printf("[slack] manifest sync failed for %s: %v", slug, err)
	}
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

	// One Slack channel must bind to exactly one office slug. A different name
	// derives a different slug, so the per-slug check above would miss a second
	// binding of the SAME remote channel id — reject it here so attribution and
	// the outbound thread mapping stay one-to-one.
	for i := range b.channels {
		ch := &b.channels[i]
		if ch.Surface != nil && ch.Surface.Provider == "slack" && ch.Surface.RemoteID == channelID {
			return ch, fmt.Errorf("%w: slack channel %s already bridges office channel %q", errSlackChannelAlreadyBridges, channelID, ch.Slug)
		}
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
