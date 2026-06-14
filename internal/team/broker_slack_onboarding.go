package team

// broker_slack_onboarding.go powers the web app's guided Slack-onboarding
// wizard. It turns the previously manual setup (paste env vars, curl
// /slack/connect, restart) into three small endpoints the wizard drives:
//
//   GET  /slack/app-manifest  → the ready-to-paste office app manifest + guide.
//                               Pasting it pre-configures every painful part:
//                               scopes, Socket Mode, event subscriptions,
//                               interactivity, and the App Home tab.
//   POST /slack/tokens        → validate the bot token against Slack (auth.test)
//                               and persist both tokens to config; returns the
//                               workspace + bot identity for a confident
//                               "connected as … in …" confirmation.
//   GET  /slack/status        → tokens-set + channel-connected state, so the
//                               wizard can poll the office back to life after
//                               the activation restart.
//
// The channel binding itself reuses POST /slack/connect; activation reuses
// POST /api/broker/restart (the Socket Mode transport binds at boot).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/config"
)

// officeSlackManifest is the full Slack app manifest for the OFFICE bridge —
// richer than a spawned agent's (broker_slack_spawn.go), because the bridge
// needs to receive events (Socket Mode), render the approval gate
// (interactivity), and publish the App Home tab. Pasting this manifest when
// creating the app means the operator never has to hand-toggle any of it.
type officeSlackManifest struct {
	DisplayInformation slackManifestDisplay   `json:"display_information"`
	Features           officeManifestFeatures `json:"features"`
	OauthConfig        slackManifestOauth     `json:"oauth_config"`
	Settings           officeManifestSettings `json:"settings"`
}

type officeManifestFeatures struct {
	BotUser slackManifestBotUser  `json:"bot_user"`
	AppHome officeManifestAppHome `json:"app_home"`
}

type officeManifestAppHome struct {
	HomeTabEnabled     bool `json:"home_tab_enabled"`
	MessagesTabEnabled bool `json:"messages_tab_enabled"`
}

type officeManifestSettings struct {
	EventSubscriptions officeManifestEvents        `json:"event_subscriptions"`
	Interactivity      officeManifestInteractivity `json:"interactivity"`
	SocketModeEnabled  bool                        `json:"socket_mode_enabled"`
}

type officeManifestEvents struct {
	BotEvents []string `json:"bot_events"`
}

type officeManifestInteractivity struct {
	IsEnabled bool `json:"is_enabled"`
}

// officeSlackAppManifest builds the office bridge manifest. The display name
// is the workspace's office identity ("wuphf" by default); the bot scopes,
// events, and settings cover exactly what the transport uses and nothing more.
func officeSlackAppManifest(appName string) officeSlackManifest {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = "WUPHF Office"
	}
	return officeSlackManifest{
		DisplayInformation: slackManifestDisplay{
			Name:        appName,
			Description: "Your AI office, bridged into Slack — tasks, agents, and the team wiki, right where you work.",
		},
		Features: officeManifestFeatures{
			BotUser: slackManifestBotUser{DisplayName: "wuphf", AlwaysOnline: true},
			AppHome: officeManifestAppHome{HomeTabEnabled: true, MessagesTabEnabled: false},
		},
		OauthConfig: slackManifestOauth{
			Scopes: slackManifestScopes{Bot: []string{
				"app_mentions:read",
				"channels:history",
				"channels:read",
				"chat:write",
				"groups:history",
				"groups:read",
				"pins:write",
				"users:read",
			}},
		},
		Settings: officeManifestSettings{
			EventSubscriptions: officeManifestEvents{BotEvents: []string{
				"app_home_opened",
				"app_mention",
				"message.channels",
				"message.groups",
			}},
			Interactivity:     officeManifestInteractivity{IsEnabled: true},
			SocketModeEnabled: true,
		},
	}
}

// handleSlackAppManifest returns the office app manifest (as a struct AND a
// pretty-printed string the wizard shows in a copy box) plus a numbered guide.
func (b *Broker) handleSlackAppManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	manifest := officeSlackAppManifest("WUPHF Office")
	pretty, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		http.Error(w, "manifest render failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":      manifest,
		"manifest_json": string(pretty),
		"create_url":    "https://api.slack.com/apps?new_app=1",
		"guide": []string{
			`Open api.slack.com/apps, click "Create New App", and choose "From an app manifest".`,
			"Pick the Slack workspace your team lives in, then paste the manifest above and create the app.",
			`Under "Install App", install it to your workspace and copy the Bot User OAuth Token (starts with xoxb-).`,
			`Under "Basic Information" → "App-Level Tokens", generate a token with the connections:write scope and copy it (starts with xapp-).`,
			"Invite the bot to the channel your office should live in: /invite @wuphf",
			"Come back here, paste both tokens, choose the channel, and you're live.",
		},
	})
}

// slackOnboardingAuthTest is the auth.test seam (overridable in tests). Returns
// the bot's user id, display name, and workspace name.
type slackOnboardingAuthTestFunc func(ctx context.Context, token string) (botUserID, botName, workspace string, err error)

func realSlackOnboardingAuthTest(ctx context.Context, token string) (string, string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := slack.New(token).AuthTestContext(ctx)
	if err != nil {
		return "", "", "", err
	}
	return resp.UserID, strings.TrimSpace(resp.User), strings.TrimSpace(resp.Team), nil
}

// handleSlackTokens validates the bot token against Slack and persists both
// tokens to config. The app token is format-checked (xapp-) since there is no
// cheap online check for it. Returns the workspace + bot identity so the wizard
// can show a real confirmation rather than a hopeful "saved".
func (b *Broker) handleSlackTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		BotToken string `json:"bot_token"`
		AppToken string `json:"app_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	botToken := strings.TrimSpace(body.BotToken)
	appToken := strings.TrimSpace(body.AppToken)
	if !strings.HasPrefix(botToken, "xoxb-") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the bot token should start with xoxb- — copy it from OAuth & Permissions after installing the app."})
		return
	}
	if !strings.HasPrefix(appToken, "xapp-") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the app-level token should start with xapp- — generate it under Basic Information → App-Level Tokens with the connections:write scope."})
		return
	}

	authTest := b.slackOnboardingAuthTest
	if authTest == nil {
		authTest = realSlackOnboardingAuthTest
	}
	botUserID, botName, workspace, err := authTest(r.Context(), botToken)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Slack rejected that bot token: " + err.Error() + " — re-copy the Bot User OAuth Token and try again."})
		return
	}

	if err := config.SaveSlackTokens(botToken, appToken); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "couldn't save the tokens to config — " + err.Error()})
		return
	}
	if botName == "" {
		botName = "wuphf"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"bot_user_id": botUserID,
		"bot_name":    botName,
		"workspace":   workspace,
	})
}

// handleSlackStatus reports the onboarding state so the wizard can render the
// right step on open and poll the Socket Mode connection live after
// /slack/connect hot-starts the transport (no broker restart). "ready" now means
// the transport is actually connected — tokens + a connected channel + a healthy
// Socket Mode link — so the wizard's "you're live" reflects a real connection
// rather than just persisted config.
func (b *Broker) handleSlackStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	botSet := strings.TrimSpace(config.ResolveSlackBotToken()) != ""
	appSet := strings.TrimSpace(config.ResolveSlackAppToken()) != ""

	channelSlug := ""
	for _, ch := range b.SurfaceChannels("slack") {
		channelSlug = ch.Slug
		break
	}
	channelConnected := channelSlug != ""
	transportConnected := b.slackTransportConnected()

	writeJSON(w, http.StatusOK, map[string]any{
		"bot_token_set":       botSet,
		"app_token_set":       appSet,
		"channel_connected":   channelConnected,
		"channel_slug":        channelSlug,
		"transport_connected": transportConnected,
		"ready":               botSet && appSet && channelConnected && transportConnected,
	})
}
