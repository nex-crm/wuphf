package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

type slackConnectRequest struct {
	BotToken       string `json:"bot_token,omitempty"`
	AppToken       string `json:"app_token,omitempty"`
	SigningSecret  string `json:"signing_secret,omitempty"`
	AppConfigToken string `json:"app_config_token,omitempty"`
	ChannelID      string `json:"channel_id,omitempty"`
	ChannelName    string `json:"channel_name,omitempty"`
	ChannelSlug    string `json:"channel_slug,omitempty"`
}

func (b *Broker) handleSlackVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body slackConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	botToken := resolveSlackBotTokenFromBody(body.BotToken)
	if botToken == "" {
		http.Error(w, "slack bot token required", http.StatusBadRequest)
		return
	}
	client := newSlackAPIClient(botToken, resolveSlackAppTokenFromBody(body.AppToken))
	auth, err := client.authTest(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := b.saveSlackInstallIfPresent(body, auth); err != nil {
		http.Error(w, "save slack config failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"team":        auth.Team,
		"team_id":     auth.TeamID,
		"app_id":      auth.AppID,
		"bot_user_id": auth.UserID,
		"bot_name":    auth.User,
	})
}

func (b *Broker) handleSlackChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body slackConnectRequest
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	}
	botToken := resolveSlackBotTokenFromBody(body.BotToken)
	if botToken == "" {
		http.Error(w, "slack bot token required", http.StatusBadRequest)
		return
	}
	client := newSlackAPIClient(botToken, "")
	channels, err := client.listChannels(r.Context(), 200)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not list slack channels: %v", err), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

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
	if channelID == "" {
		http.Error(w, "channel_id required", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(body.ChannelName)
	if title == "" {
		title = channelID
	}
	slug := normalizeChannelSlug(body.ChannelSlug)
	if slug == "" || slug == "general" && strings.TrimSpace(body.ChannelSlug) == "" {
		slug = normalizeChannelSlug(title)
	}
	ch, err := b.createOrUpdateSlackChannel(slug, title, channelID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "reserved") || strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	if err := b.saveSlackInstallIfPresent(body, slackAuthTestResponse{}); err != nil {
		http.Error(w, "save slack config failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":      ch,
		"channel_slug": ch.Slug,
		"slack_id":     channelID,
	})
}

func (b *Broker) handleSlackDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body slackConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	slug := normalizeChannelSlug(body.ChannelSlug)
	if slug == "" {
		http.Error(w, "channel_slug required", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked(slug)
	if ch == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if ch.Surface == nil || ch.Surface.Provider != "slack" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": false})
		return
	}
	ch.Surface = nil
	ch.UpdatedAt = nowRFC3339()
	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: slug})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": true})
}

func (b *Broker) handleSlackStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, _ := config.Load()
	surfaceChannels := b.SurfaceChannels("slack")
	writeJSON(w, http.StatusOK, map[string]any{
		"bot_token_set":        config.ResolveSlackBotToken() != "",
		"app_token_set":        config.ResolveSlackAppToken() != "",
		"signing_secret_set":   config.ResolveSlackSigningSecret() != "",
		"app_config_token_set": config.ResolveSlackAppConfigToken() != "",
		"team_id":              strings.TrimSpace(cfg.SlackTeamID),
		"app_id":               strings.TrimSpace(cfg.SlackAppID),
		"bot_user_id":          strings.TrimSpace(cfg.SlackBotUserID),
		"mirrored_channels":    surfaceChannels,
	})
}

func (b *Broker) createOrUpdateSlackChannel(slug, title, slackID string) (*teamChannel, error) {
	slug = normalizeChannelSlug(slug)
	slackID = strings.TrimSpace(slackID)
	if slug == "" {
		return nil, fmt.Errorf("channel slug required")
	}
	if slackID == "" {
		return nil, fmt.Errorf("slack channel id required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing := b.findChannelLocked(slug); existing != nil {
		if existing.Surface != nil && existing.Surface.Provider != "slack" {
			return nil, fmt.Errorf("channel %q already bridges %s", slug, existing.Surface.Provider)
		}
		existing.Surface = &channelSurface{Provider: "slack", RemoteID: slackID, RemoteTitle: title, Mode: "channel"}
		if title != "" {
			existing.Name = title
		}
		existing.UpdatedAt = nowRFC3339()
		if err := b.saveLocked(); err != nil {
			return nil, err
		}
		b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: slug})
		return existing, nil
	}
	members := b.allOfficeMemberSlugsLocked()
	ch, cerr := b.createChannelLocked(channelCreateInput{
		Slug:        slug,
		Name:        title,
		Description: fmt.Sprintf("Slack mirror for #%s.", title),
		Members:     members,
		CreatedBy:   "slack",
		Surface:     &channelSurface{Provider: "slack", RemoteID: slackID, RemoteTitle: title, Mode: "channel"},
	})
	if cerr != nil {
		return nil, cerr
	}
	return ch, nil
}

func (b *Broker) allOfficeMemberSlugsLocked() []string {
	out := make([]string, 0, len(b.members))
	for _, member := range b.members {
		if member.Slug == "" {
			continue
		}
		out = append(out, member.Slug)
	}
	return out
}

func resolveSlackBotTokenFromBody(token string) string {
	if t := strings.TrimSpace(token); t != "" {
		return t
	}
	return config.ResolveSlackBotToken()
}

func resolveSlackAppTokenFromBody(token string) string {
	if t := strings.TrimSpace(token); t != "" {
		return t
	}
	return config.ResolveSlackAppToken()
}

func (b *Broker) saveSlackInstallIfPresent(body slackConnectRequest, auth slackAuthTestResponse) error {
	if strings.TrimSpace(body.BotToken) == "" &&
		strings.TrimSpace(body.AppToken) == "" &&
		strings.TrimSpace(body.SigningSecret) == "" &&
		strings.TrimSpace(body.AppConfigToken) == "" &&
		auth.TeamID == "" &&
		auth.AppID == "" &&
		auth.UserID == "" {
		return nil
	}
	b.configMu.Lock()
	defer b.configMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if t := strings.TrimSpace(body.BotToken); t != "" {
		cfg.SlackBotToken = t
	}
	if t := strings.TrimSpace(body.AppToken); t != "" {
		cfg.SlackAppToken = t
	}
	if t := strings.TrimSpace(body.SigningSecret); t != "" {
		cfg.SlackSigningSecret = t
	}
	if t := strings.TrimSpace(body.AppConfigToken); t != "" {
		cfg.SlackAppConfigToken = t
	}
	if t := strings.TrimSpace(auth.TeamID); t != "" {
		cfg.SlackTeamID = t
	}
	if t := strings.TrimSpace(auth.AppID); t != "" {
		cfg.SlackAppID = t
	}
	if t := strings.TrimSpace(auth.UserID); t != "" {
		cfg.SlackBotUserID = t
	}
	return config.Save(cfg)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
