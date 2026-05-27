package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const slackAPIBase = "https://slack.com/api"

var slackHTTPClient = &http.Client{Timeout: 30 * time.Second}

type slackAPIClient struct {
	botToken string
	appToken string
	baseURL  string
	client   *http.Client
}

type slackAPIError struct {
	Method string
	Code   string
}

func (e slackAPIError) Error() string {
	if e.Code == "" {
		return e.Method + ": slack api error"
	}
	return e.Method + ": " + e.Code
}

type slackAuthTestResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	URL    string `json:"url,omitempty"`
	Team   string `json:"team,omitempty"`
	User   string `json:"user,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	UserID string `json:"user_id,omitempty"`
	BotID  string `json:"bot_id,omitempty"`
	AppID  string `json:"app_id,omitempty"`
}

type slackConnectionOpenResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	URL   string `json:"url,omitempty"`
}

type slackChannelInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsMember   bool   `json:"is_member"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
}

type slackChannelsResponse struct {
	OK               bool               `json:"ok"`
	Error            string             `json:"error,omitempty"`
	Channels         []slackChannelInfo `json:"channels"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

type slackPostMessageResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Channel string `json:"channel,omitempty"`
	TS      string `json:"ts,omitempty"`
}

type slackOKResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type slackStreamResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Channel   string `json:"channel,omitempty"`
	TS        string `json:"ts,omitempty"`
	MessageTS string `json:"message_ts,omitempty"`
}

type slackViewResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type slackAssistantPrompt struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type slackManifestCreateResponse struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
	AppID             string `json:"app_id,omitempty"`
	OAuthAuthorizeURL string `json:"oauth_authorize_url,omitempty"`
	Credentials       struct {
		ClientID          string `json:"client_id,omitempty"`
		ClientSecret      string `json:"client_secret,omitempty"`
		VerificationToken string `json:"verification_token,omitempty"`
		SigningSecret     string `json:"signing_secret,omitempty"`
	} `json:"credentials,omitempty"`
}

func newSlackAPIClient(botToken, appToken string) *slackAPIClient {
	return &slackAPIClient{
		botToken: strings.TrimSpace(botToken),
		appToken: strings.TrimSpace(appToken),
		baseURL:  slackAPIBase,
		client:   slackHTTPClient,
	}
}

func (c *slackAPIClient) authTest(ctx context.Context) (slackAuthTestResponse, error) {
	var out slackAuthTestResponse
	if err := c.postForm(ctx, "auth.test", c.botToken, nil, &out); err != nil {
		return out, err
	}
	if !out.OK {
		return out, slackAPIError{Method: "auth.test", Code: out.Error}
	}
	return out, nil
}

func (c *slackAPIClient) openSocketModeURL(ctx context.Context) (string, error) {
	var out slackConnectionOpenResponse
	if err := c.postForm(ctx, "apps.connections.open", c.appToken, nil, &out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", slackAPIError{Method: "apps.connections.open", Code: out.Error}
	}
	if strings.TrimSpace(out.URL) == "" {
		return "", slackAPIError{Method: "apps.connections.open", Code: "missing_url"}
	}
	return out.URL, nil
}

func (c *slackAPIClient) listChannels(ctx context.Context, limit int) ([]slackChannelInfo, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	cursor := ""
	var all []slackChannelInfo
	for {
		values := url.Values{}
		values.Set("types", "public_channel,private_channel")
		values.Set("exclude_archived", "true")
		values.Set("limit", strconv.Itoa(limit))
		if cursor != "" {
			values.Set("cursor", cursor)
		}
		var out slackChannelsResponse
		if err := c.postForm(ctx, "conversations.list", c.botToken, values, &out); err != nil {
			return nil, err
		}
		if !out.OK {
			return nil, slackAPIError{Method: "conversations.list", Code: out.Error}
		}
		all = append(all, out.Channels...)
		cursor = strings.TrimSpace(out.ResponseMetadata.NextCursor)
		if cursor == "" {
			return all, nil
		}
	}
}

func (c *slackAPIClient) postMessage(ctx context.Context, payload map[string]any) (slackPostMessageResponse, error) {
	var out slackPostMessageResponse
	if err := c.postJSON(ctx, "chat.postMessage", c.botToken, payload, &out); err != nil {
		return out, err
	}
	if !out.OK {
		return out, slackAPIError{Method: "chat.postMessage", Code: out.Error}
	}
	return out, nil
}

func (c *slackAPIClient) setAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
	payload := map[string]any{
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"status":     status,
	}
	var out slackOKResponse
	if err := c.postJSON(ctx, "assistant.threads.setStatus", c.botToken, payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return slackAPIError{Method: "assistant.threads.setStatus", Code: out.Error}
	}
	return nil
}

func (c *slackAPIClient) setAssistantTitle(ctx context.Context, channelID, threadTS, title string) error {
	payload := map[string]any{
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"title":      title,
	}
	var out slackOKResponse
	if err := c.postJSON(ctx, "assistant.threads.setTitle", c.botToken, payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return slackAPIError{Method: "assistant.threads.setTitle", Code: out.Error}
	}
	return nil
}

func (c *slackAPIClient) setSuggestedPrompts(ctx context.Context, channelID, threadTS, title string, prompts []slackAssistantPrompt) error {
	payload := map[string]any{
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"prompts":    prompts,
	}
	if strings.TrimSpace(title) != "" {
		payload["title"] = title
	}
	var out slackOKResponse
	if err := c.postJSON(ctx, "assistant.threads.setSuggestedPrompts", c.botToken, payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return slackAPIError{Method: "assistant.threads.setSuggestedPrompts", Code: out.Error}
	}
	return nil
}

func (c *slackAPIClient) startStream(ctx context.Context, payload map[string]any) (slackStreamResponse, error) {
	var out slackStreamResponse
	if err := c.postJSON(ctx, "chat.startStream", c.botToken, payload, &out); err != nil {
		return out, err
	}
	if !out.OK {
		return out, slackAPIError{Method: "chat.startStream", Code: out.Error}
	}
	return out, nil
}

func (c *slackAPIClient) appendStream(ctx context.Context, payload map[string]any) error {
	var out slackOKResponse
	if err := c.postJSON(ctx, "chat.appendStream", c.botToken, payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return slackAPIError{Method: "chat.appendStream", Code: out.Error}
	}
	return nil
}

func (c *slackAPIClient) stopStream(ctx context.Context, payload map[string]any) error {
	var out slackOKResponse
	if err := c.postJSON(ctx, "chat.stopStream", c.botToken, payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return slackAPIError{Method: "chat.stopStream", Code: out.Error}
	}
	return nil
}

func (c *slackAPIClient) publishHomeView(ctx context.Context, userID string, blocks []map[string]any) error {
	payload := map[string]any{
		"user_id": userID,
		"view": map[string]any{
			"type":   "home",
			"blocks": blocks,
		},
	}
	var out slackViewResponse
	if err := c.postJSON(ctx, "views.publish", c.botToken, payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return slackAPIError{Method: "views.publish", Code: out.Error}
	}
	return nil
}

func (c *slackAPIClient) createManifestApp(ctx context.Context, configToken string, manifest any) (slackManifestCreateResponse, error) {
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return slackManifestCreateResponse{}, err
	}
	payload := map[string]any{"manifest": string(manifestBytes)}
	var out slackManifestCreateResponse
	if err := c.postJSON(ctx, "apps.manifest.create", configToken, payload, &out); err != nil {
		return out, err
	}
	if !out.OK {
		return out, slackAPIError{Method: "apps.manifest.create", Code: out.Error}
	}
	return out, nil
}

func (c *slackAPIClient) postForm(ctx context.Context, method, token string, values url.Values, out any) error {
	if strings.TrimSpace(token) == "" {
		return slackAPIError{Method: method, Code: "missing_token"}
	}
	if values == nil {
		values = url.Values{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(method), strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%s: slack rate limited: retry after %s", method, resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: slack returned HTTP %d", method, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *slackAPIClient) postJSON(ctx context.Context, method, token string, payload any, out any) error {
	if strings.TrimSpace(token) == "" {
		return slackAPIError{Method: method, Code: "missing_token"}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(method), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%s: slack rate limited: retry after %s", method, resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: slack returned HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

func (c *slackAPIClient) methodURL(method string) string {
	base := strings.TrimRight(c.baseURL, "/")
	if base == "" {
		base = slackAPIBase
	}
	return base + "/" + strings.TrimLeft(method, "/")
}

func (c *slackAPIClient) httpClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	return slackHTTPClient
}
