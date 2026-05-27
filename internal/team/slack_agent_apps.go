package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

type slackAgentAppRequest struct {
	Slug           string `json:"slug"`
	AppConfigToken string `json:"app_config_token,omitempty"`
}

type slackAgentAppResult struct {
	Slug              string `json:"slug"`
	AppID             string `json:"app_id,omitempty"`
	OAuthAuthorizeURL string `json:"oauth_authorize_url,omitempty"`
	SigningSecret     string `json:"signing_secret,omitempty"`
}

func (b *Broker) handleSlackAgentApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body slackAgentAppRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	result, err := b.CreateSlackAgentApp(r.Context(), body.Slug, body.AppConfigToken)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "not found") {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_app": result})
}

func (b *Broker) CreateSlackAgentApp(ctx context.Context, slug, configToken string) (slackAgentAppResult, error) {
	slug = normalizeActorSlug(slug)
	if slug == "" {
		return slackAgentAppResult{}, fmt.Errorf("agent slug required")
	}
	member := b.officeMemberBySlug(slug)
	if member == nil {
		return slackAgentAppResult{}, fmt.Errorf("agent %q not found", slug)
	}
	token := strings.TrimSpace(configToken)
	if token == "" {
		token = config.ResolveSlackAppConfigToken()
	}
	if token == "" {
		return slackAgentAppResult{}, fmt.Errorf("slack app config token required")
	}
	manifest := slackAgentManifestForMember(*member)
	resp, err := newSlackAPIClient("", "").createManifestApp(ctx, token, manifest)
	if err != nil {
		return slackAgentAppResult{}, err
	}
	return slackAgentAppResult{
		Slug:              slug,
		AppID:             resp.AppID,
		OAuthAuthorizeURL: resp.OAuthAuthorizeURL,
		SigningSecret:     resp.Credentials.SigningSecret,
	}, nil
}

func (b *Broker) officeMemberBySlug(slug string) *officeMember {
	b.mu.Lock()
	defer b.mu.Unlock()
	member := b.findMemberLocked(slug)
	if member == nil {
		return nil
	}
	cp := cloneOfficeMemberForRead(*member)
	return &cp
}

func slackAgentManifestForMember(member officeMember) map[string]any {
	name := strings.TrimSpace(member.Name)
	if name == "" {
		name = member.Slug
	}
	description := strings.TrimSpace(member.Role)
	if description == "" {
		description = "WUPHF office agent"
	}
	longDescription := fmt.Sprintf("%s is a WUPHF office agent. The app receives Slack AI app thread events, message.im direct-message events, and app mentions, then routes work through the WUPHF office broker so the same agent can operate from Slack.", name)
	if len([]rune(longDescription)) < 174 {
		longDescription += " It mirrors the WUPHF office context into Slack while preserving WUPHF orchestration, channel, wiki, and task controls."
	}
	return map[string]any{
		"display_information": map[string]any{
			"name":             truncateManifestText("WUPHF "+name, 35),
			"description":      truncateManifestText(description, 140),
			"long_description": truncateManifestText(longDescription, 4000),
			"background_color": "#111827",
		},
		"features": map[string]any{
			"bot_user": map[string]any{
				"display_name":  truncateManifestText(name, 80),
				"always_online": true,
			},
			"agent_view": map[string]any{
				"description": truncateManifestText(description, 260),
			},
		},
		"oauth_config": map[string]any{
			"scopes": map[string]any{
				"bot": []string{
					"assistant:write",
					"chat:write",
					"im:history",
					"app_mentions:read",
					"channels:history",
				},
			},
		},
		"settings": map[string]any{
			"socket_mode_enabled":    true,
			"token_rotation_enabled": false,
			"event_subscriptions": map[string]any{
				"bot_events": []string{
					"assistant_thread_started",
					"assistant_thread_context_changed",
					"message.im",
					"app_mention",
				},
			},
		},
	}
}

var whitespaceRE = regexp.MustCompile(`\s+`)

func truncateManifestText(text string, max int) string {
	text = strings.TrimSpace(whitespaceRE.ReplaceAllString(text, " "))
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
