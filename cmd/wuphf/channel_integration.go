package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

func channelIntegrationOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(channelIntegrationSpecs))
	for _, spec := range channelIntegrationSpecs {
		options = append(options, tui.PickerOption{
			Label:       spec.Label,
			Value:       spec.Value,
			Description: spec.Description,
		})
	}
	return options
}

func findChannelIntegration(value string) (channelIntegrationSpec, bool) {
	for _, spec := range channelIntegrationSpecs {
		if spec.Value == value {
			return spec, true
		}
	}
	return channelIntegrationSpec{}, false
}

func connectIntegration(spec channelIntegrationSpec) tea.Cmd {
	return func() tea.Msg {
		apiKey := config.ResolveAPIKey("")
		if apiKey == "" {
			return channelIntegrationDoneMsg{err: errors.New("run /init first to configure your WUPHF API key")}
		}
		client := api.NewClient(apiKey)
		result, err := api.Post[map[string]any](client,
			fmt.Sprintf("/v1/integrations/%s/%s/connect", spec.Type, spec.Provider),
			nil,
			30*time.Second,
		)
		if err != nil {
			return channelIntegrationDoneMsg{err: err}
		}

		authURL := mapString(result, "auth_url")
		if authURL != "" {
			_ = openBrowserURL(authURL)
		}
		connectID := mapString(result, "connect_id")
		if connectID == "" {
			return channelIntegrationDoneMsg{label: spec.Label, url: authURL}
		}

		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			statusResp, err := api.Get[map[string]any](client,
				fmt.Sprintf("/v1/integrations/connect/%s/status", connectID),
				15*time.Second,
			)
			if err != nil {
				if _, ok := err.(*api.AuthError); ok {
					return channelIntegrationDoneMsg{err: err}
				}
				continue
			}
			status := strings.ToLower(mapString(statusResp, "status"))
			switch status {
			case "connected", "complete", "completed", "active":
				return channelIntegrationDoneMsg{label: spec.Label, url: authURL}
			case "failed", "error":
				reason := mapString(statusResp, "error")
				if reason == "" {
					reason = status
				}
				return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection failed: %s", spec.Label, reason)}
			}
		}

		if authURL != "" {
			return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection timed out. Finish OAuth at %s", spec.Label, authURL)}
		}
		return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection timed out", spec.Label)}
	}
}

func (m *channelModel) startTelegramConnect() tea.Cmd {
	token := os.Getenv("WUPHF_TELEGRAM_BOT_TOKEN")
	if token == "" {
		token = config.ResolveTelegramBotToken()
	}
	if token != "" {
		m.posting = true
		m.notice = "Verifying bot token and discovering groups..."
		return discoverTelegramGroups(token)
	}
	// Show token input inside the picker overlay
	m.picker = tui.NewPicker("Connect Telegram", nil)
	m.picker.TextInput = true
	m.picker.TextPrompt = "Paste your bot token from @BotFather:"
	m.picker.SetActive(true)
	m.pickerMode = channelPickerTelegramToken
	return nil
}

func discoverTelegramGroups(token string) tea.Cmd {
	return func() tea.Msg {
		botName, err := team.VerifyBot(token)
		if err != nil {
			return telegramDiscoverMsg{err: fmt.Errorf("bot verification failed: %w", err)}
		}
		// Try getUpdates first
		groups, _ := team.DiscoverGroups(token)

		// Also fetch groups the transport has seen (via broker API)
		seen := make(map[int64]bool)
		for _, g := range groups {
			seen[g.ChatID] = true
		}
		req, reqErr := newBrokerRequest("GET", "http://127.0.0.1:7890/telegram/groups", nil)
		if reqErr == nil {
			client := &http.Client{Timeout: 2 * time.Second}
			if resp, err := client.Do(req); err == nil {
				defer resp.Body.Close()
				var result struct {
					Groups []struct {
						ChatID int64  `json:"chat_id"`
						Title  string `json:"title"`
					} `json:"groups"`
				}
				if json.NewDecoder(resp.Body).Decode(&result) == nil {
					for _, g := range result.Groups {
						if !seen[g.ChatID] {
							groups = append(groups, team.TelegramGroup{
								ChatID: g.ChatID,
								Title:  g.Title,
								Type:   "group",
							})
						}
					}
				}
			}
		}

		return telegramDiscoverMsg{
			botName: botName,
			groups:  groups,
			token:   token,
		}
	}
}

func connectTelegramGroup(token string, group team.TelegramGroup) tea.Cmd {
	return func() tea.Msg {
		slug := slugifyGroupTitle(group.Title)

		// Load manifest and add the new channel with telegram surface
		manifest, err := company.LoadManifest()
		if err != nil {
			manifest = company.DefaultManifest()
		}

		// Check if channel already exists (by slug or remote_id)
		remoteID := fmt.Sprintf("%d", group.ChatID)
		for _, ch := range manifest.Channels {
			if ch.Slug == slug {
				return telegramConnectDoneMsg{
					channelSlug: ch.Slug,
					groupTitle:  group.Title,
				}
			}
			if ch.Surface != nil && ch.Surface.Provider == "telegram" && ch.Surface.RemoteID == remoteID {
				return telegramConnectDoneMsg{
					channelSlug: ch.Slug,
					groupTitle:  group.Title,
				}
			}
		}

		// Build default members: lead + all manifest members
		members := []string{manifest.Lead}
		for _, member := range manifest.Members {
			if member.Slug != manifest.Lead {
				members = append(members, member.Slug)
			}
		}

		newChannel := company.ChannelSpec{
			Slug:        slug,
			Name:        group.Title,
			Description: fmt.Sprintf("Telegram bridge for %s.", group.Title),
			Members:     members,
			Surface: &company.ChannelSurfaceSpec{
				Provider:    "telegram",
				RemoteID:    fmt.Sprintf("%d", group.ChatID),
				RemoteTitle: group.Title,
				BotTokenEnv: "WUPHF_TELEGRAM_BOT_TOKEN",
			},
		}
		manifest.Channels = append(manifest.Channels, newChannel)
		if err := company.SaveManifest(manifest); err != nil {
			return telegramConnectDoneMsg{err: fmt.Errorf("failed to save manifest: %w", err)}
		}

		// Create channel in the live broker WITH surface metadata
		body, _ := json.Marshal(map[string]any{
			"action":      "create",
			"slug":        slug,
			"name":        group.Title,
			"description": fmt.Sprintf("Telegram bridge for %s.", group.Title),
			"members":     members,
			"created_by":  "you",
			"surface": map[string]any{
				"provider":      "telegram",
				"remote_id":     fmt.Sprintf("%d", group.ChatID),
				"remote_title":  group.Title,
				"mode":          group.Type,
				"bot_token_env": "WUPHF_TELEGRAM_BOT_TOKEN",
			},
		})
		req, reqErr := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channels", bytes.NewReader(body))
		if reqErr == nil {
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}

		// Send confirmation message to the Telegram group
		if group.ChatID != 0 {
			_ = team.SendTelegramMessage(token, group.ChatID,
				"Connected to WUPHF Office. Messages here will be visible to the team.")
		}

		// Clear broker state so next restart picks up the manifest with surfaces
		_ = os.Remove(filepath.Join(os.Getenv("HOME"), ".wuphf", "team", "broker-state.json"))

		return telegramConnectDoneMsg{
			channelSlug: slug,
			groupTitle:  group.Title,
		}
	}
}

func slugifyGroupTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "telegram"
	}
	// Prefix with tg- to make it clear it's a telegram channel
	return "tg-" + slug
}

// startOpenclawConnect seeds the /connect openclaw picker flow at the URL step.
// It reuses any saved gateway URL/token from config as defaults.
func (m *channelModel) startOpenclawConnect() {
	cfg, _ := config.Load()
	if m.openclawURL == "" {
		m.openclawURL = cfg.OpenclawGatewayURL
	}
	if m.openclawToken == "" {
		m.openclawToken = cfg.OpenclawToken
	}
	m.promptOpenclawURL()
}

func (m *channelModel) promptOpenclawURL() {
	m.picker = tui.NewPicker("Connect OpenClaw", nil)
	m.picker.TextInput = true
	m.picker.TextPrompt = "Gateway URL (default ws://127.0.0.1:18789):"
	m.picker.SetActive(true)
	m.pickerMode = channelPickerOpenclawURL
	m.notice = "Paste your OpenClaw gateway URL or press Enter for the default."
}

func (m *channelModel) promptOpenclawToken() {
	m.picker = tui.NewPicker("Connect OpenClaw", nil)
	m.picker.TextInput = true
	m.picker.TextPrompt = "Shared secret (gateway.auth.token from ~/.openclaw/openclaw.json):"
	m.picker.SetActive(true)
	m.pickerMode = channelPickerOpenclawToken
	m.notice = "Paste the shared secret for the gateway."
}

// fetchOpenclawSessions dials the gateway and enumerates bridgeable sessions.
func fetchOpenclawSessions(url, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		identity, err := openclaw.LoadOrCreateDeviceIdentity(config.ResolveOpenclawIdentityPath())
		if err != nil {
			return openclawSessionsMsg{err: err}
		}
		client, err := openclaw.Dial(ctx, openclaw.Config{URL: url, Token: token, Identity: identity})
		if err != nil {
			return openclawSessionsMsg{err: err}
		}
		defer func() { _ = client.Close() }()
		rows, err := client.SessionsList(ctx, openclaw.SessionsListFilter{Limit: 50, IncludeLastMessage: true})
		if err != nil {
			return openclawSessionsMsg{err: err}
		}
		out := make([]openclawSessionOption, 0, len(rows))
		for _, r := range rows {
			label := r.DisplayName
			if label == "" {
				label = r.Label
			}
			if label == "" {
				label = r.Key
			}
			preview := strings.TrimSpace(r.LastMessage)
			if preview == "" && r.Kind != "" {
				preview = r.Kind
			}
			out = append(out, openclawSessionOption{
				SessionKey: r.Key,
				Label:      label,
				Preview:    preview,
			})
		}
		return openclawSessionsMsg{sessions: out}
	}
}

// connectOpenclawSession persists the binding and saves gateway creds into config.
func connectOpenclawSession(url, token string, session openclawSessionOption) tea.Cmd {
	return func() tea.Msg {
		slug := "openclaw-" + slugifyOpenclawLabel(session.Label)
		cfg, err := config.Load()
		if err != nil {
			return openclawConnectDoneMsg{err: fmt.Errorf("load config: %w", err)}
		}
		cfg.OpenclawGatewayURL = url
		cfg.OpenclawToken = token
		// Dedupe on SessionKey: replace existing binding if present.
		replaced := false
		for i := range cfg.OpenclawBridges {
			if cfg.OpenclawBridges[i].SessionKey == session.SessionKey {
				cfg.OpenclawBridges[i] = config.OpenclawBridgeBinding{
					SessionKey:  session.SessionKey,
					Slug:        slug,
					DisplayName: session.Label,
				}
				slug = cfg.OpenclawBridges[i].Slug
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.OpenclawBridges = append(cfg.OpenclawBridges, config.OpenclawBridgeBinding{
				SessionKey:  session.SessionKey,
				Slug:        slug,
				DisplayName: session.Label,
			})
		}
		if err := config.Save(cfg); err != nil {
			return openclawConnectDoneMsg{err: fmt.Errorf("save config: %w", err)}
		}
		return openclawConnectDoneMsg{slug: slug, label: session.Label}
	}
}

func slugifyOpenclawLabel(label string) string {
	slug := strings.ToLower(strings.TrimSpace(label))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "session"
	}
	return slug
}
