package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

const defaultComposioBaseURL = "https://backend.composio.dev/api/v3"

type ComposioREST struct {
	APIKey  string
	UserID  string
	BaseURL string
	Client  *http.Client
}

func NewComposioFromEnv() *ComposioREST {
	baseURL := strings.TrimSpace(strings.TrimRight(configResolveComposioBaseURL(), "/"))
	if baseURL == "" {
		baseURL = defaultComposioBaseURL
	}
	return &ComposioREST{
		APIKey:  strings.TrimSpace(config.ResolveComposioAPIKey()),
		UserID:  strings.TrimSpace(config.ResolveComposioUserID()),
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ComposioREST) Name() string { return "composio" }

func (c *ComposioREST) Configured() bool {
	return !config.ResolveNoNex() && strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.UserID) != ""
}

func (c *ComposioREST) Supports(cap Capability) bool {
	switch cap {
	case CapabilityGuide,
		CapabilityConnections,
		CapabilityActionSearch,
		CapabilityActionKnowledge,
		CapabilityActionExecute,
		CapabilityWorkflowCreate,
		CapabilityWorkflowExecute,
		CapabilityWorkflowRuns,
		CapabilityRelayList,
		CapabilityRelayEventTypes,
		CapabilityRelayCreate,
		CapabilityRelayActivate:
		return true
	default:
		return false
	}
}

func (c *ComposioREST) Guide(_ context.Context, topic string) (GuideResult, error) {
	if strings.TrimSpace(topic) == "" {
		topic = "all"
	}
	raw, _ := json.Marshal(map[string]any{
		"provider": "composio",
		"topic":    topic,
		"notes": []string{
			"Use search -> knowledge -> dry-run -> execute for external actions.",
			"Use connected account IDs returned by team_action_connections as the connection_key. If a workflow omits connection_key and there is exactly one active connection for that platform, WUPHF auto-resolves it.",
			"Trigger registration is supported through the existing relay compatibility tools with one event filter per trigger.",
			"Workflow creation and execution are WUPHF-native: save a workflow definition in WUPHF, then WUPHF executes external steps through Composio.",
			`Supported WUPHF workflow step types: "action", "template", "nex_ask", and "nex_insights".`,
			"Every workflow step also exposes a generic .result value: action=result response object, template=result text, nex_ask=result answer text, nex_insights=result compact insight summary text.",
			"Use a template step to compress large action output into concise text before handing it to nex_ask or another action.",
			"Keep workflow compose prompts compact. For digest/report flows, default to about 10 recent emails and 5 recent insights unless the human explicitly asks for more.",
			"Do not dump raw JSON from .response or .insights into nex_ask when a compact .result summary will do.",
		},
		"workflow_examples": []map[string]any{{
			"version": composioWorkflowVersion,
			"inputs": map[string]any{
				"connection_key":  "ca_...",
				"recipient_email": config.ResolveComposioUserID(),
				"subject":         "Daily digest",
				"window_hours":    24,
				"insight_limit":   5,
			},
			"steps": []map[string]any{
				{
					"id":             "fetch_emails",
					"type":           "action",
					"platform":       "gmail",
					"action_id":      "GMAIL_FETCH_EMAILS",
					"connection_key": "{{ .inputs.connection_key }}",
					"data": map[string]any{
						"query":       "newer_than:1d",
						"max_results": 10,
					},
				},
				{
					"id":             "recent_insights",
					"type":           "nex_insights",
					"lookback_hours": "{{ .inputs.window_hours }}",
					"insight_limit":  "{{ .inputs.insight_limit }}",
				},
				{
					"id":       "email_summary",
					"type":     "template",
					"template": "Email highlights from the last 24 hours:\n{{- range $m := .steps.fetch_emails.result.data.messages }}\n- {{ $m.sender }} | {{ $m.subject }} | {{ $m.preview.body }}\n{{- end }}",
				},
				{
					"id":             "compose_digest",
					"type":           "nex_ask",
					"query_template": "Draft a digest with Why This Matters and What To Do Next sections.\n\n{{ .steps.email_summary.result }}\n\n{{ .steps.recent_insights.result }}",
				},
				{
					"id":             "send_email",
					"type":           "action",
					"platform":       "gmail",
					"action_id":      "GMAIL_SEND_EMAIL",
					"connection_key": "{{ .inputs.connection_key }}",
					"data": map[string]any{
						"recipient_email": "{{ .inputs.recipient_email }}",
						"subject":         "{{ .inputs.subject }}",
						"body":            "{{ .steps.compose_digest.result }}",
					},
				},
			},
		}},
	})
	return GuideResult{Topic: topic, Raw: raw}, nil
}

func (c *ComposioREST) ListConnections(ctx context.Context, opts ListConnectionsOptions) (ConnectionsResult, error) {
	query := url.Values{}
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	raw, err := c.get(ctx, "/connected_accounts", query)
	if err != nil {
		return ConnectionsResult{}, err
	}
	var result struct {
		Items []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Toolkit struct {
				Slug string `json:"slug"`
				Name string `json:"name"`
			} `json:"toolkit"`
			Connection struct {
				Name string `json:"name"`
			} `json:"connection"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ConnectionsResult{}, fmt.Errorf("parse composio connections: %w", err)
	}
	out := ConnectionsResult{Total: len(result.Items), Showing: len(result.Items), Search: opts.Search}
	search := strings.ToLower(strings.TrimSpace(opts.Search))
	for _, item := range result.Items {
		platform := strings.TrimSpace(item.Toolkit.Slug)
		name := strings.TrimSpace(item.Connection.Name)
		if name == "" {
			name = strings.TrimSpace(item.ID)
		}
		if search != "" && !strings.Contains(strings.ToLower(platform), search) && !strings.Contains(strings.ToLower(name), search) {
			continue
		}
		out.Connections = append(out.Connections, Connection{
			Platform: platform,
			State:    strings.ToLower(strings.TrimSpace(item.Status)),
			Key:      strings.TrimSpace(item.ID),
			Name:     name,
		})
	}
	out.Total = len(out.Connections)
	out.Showing = len(out.Connections)
	return out, nil
}

func (c *ComposioREST) SearchActions(ctx context.Context, platform, queryText, mode string) (ActionSearchResult, error) {
	query := url.Values{}
	if p := normalizeComposioPlatform(platform); p != "" {
		query.Set("toolkit_slug", p)
	}
	if q := strings.TrimSpace(queryText); q != "" {
		query.Set("query", q)
	}
	query.Set("limit", "10")
	raw, err := c.get(ctx, "/tools", query)
	if err != nil {
		return ActionSearchResult{}, err
	}
	var result struct {
		Items []struct {
			Slug        string `json:"slug"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Toolkit     struct {
				Slug string `json:"slug"`
			} `json:"toolkit"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ActionSearchResult{}, fmt.Errorf("parse composio tools: %w", err)
	}
	out := ActionSearchResult{Platform: platform, Query: queryText, Mode: mode}
	for _, item := range result.Items {
		title := strings.TrimSpace(item.Name)
		if title == "" {
			title = strings.TrimSpace(item.Description)
		}
		out.Actions = append(out.Actions, Action{
			ActionID: strings.TrimSpace(item.Slug),
			Title:    title,
			Path:     strings.TrimSpace(item.Toolkit.Slug),
		})
	}
	return out, nil
}

func (c *ComposioREST) ActionKnowledge(ctx context.Context, _ string, actionID string) (KnowledgeResult, error) {
	raw, err := c.get(ctx, "/tools/"+url.PathEscape(strings.TrimSpace(actionID)), url.Values{"toolkit_versions": []string{"latest"}})
	if err != nil {
		return KnowledgeResult{}, err
	}
	var result struct {
		Slug             string          `json:"slug"`
		Name             string          `json:"name"`
		Description      string          `json:"description"`
		InputParameters  json.RawMessage `json:"input_parameters"`
		OutputParameters json.RawMessage `json:"output_parameters"`
		Toolkit          struct {
			Slug string `json:"slug"`
		} `json:"toolkit"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return KnowledgeResult{}, fmt.Errorf("parse composio tool detail: %w", err)
	}
	knowledge, _ := json.MarshalIndent(map[string]any{
		"name":              result.Name,
		"description":       result.Description,
		"toolkit":           result.Toolkit.Slug,
		"input_parameters":  json.RawMessage(result.InputParameters),
		"output_parameters": json.RawMessage(result.OutputParameters),
	}, "", "  ")
	return KnowledgeResult{
		Platform:  strings.TrimSpace(result.Toolkit.Slug),
		ActionID:  strings.TrimSpace(result.Slug),
		Knowledge: string(knowledge),
	}, nil
}

func (c *ComposioREST) ExecuteAction(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	requestPayload := map[string]any{
		"user_id": c.UserID,
	}
	if key := strings.TrimSpace(req.ConnectionKey); key != "" {
		if meta, err := c.connectedAccount(ctx, key); err == nil {
			if userID := strings.TrimSpace(meta.UserID); userID != "" {
				requestPayload["user_id"] = userID
			}
		}
		requestPayload["connected_account_id"] = key
	}
	if len(req.Data) > 0 {
		requestPayload["arguments"] = req.Data
	}
	envelope := ExecuteEnvelope{
		Method: "POST",
		URL:    c.BaseURL + "/tools/execute/" + url.PathEscape(strings.TrimSpace(req.ActionID)),
		Data:   requestPayload,
	}
	if req.DryRun {
		return ExecuteResult{DryRun: true, Request: envelope}, nil
	}
	raw, err := c.post(ctx, "/tools/execute/"+url.PathEscape(strings.TrimSpace(req.ActionID)), requestPayload)
	if err != nil {
		return ExecuteResult{}, err
	}
	return ExecuteResult{
		DryRun:   false,
		Request:  envelope,
		Response: raw,
	}, nil
}

func (c *ComposioREST) ListRelays(ctx context.Context, opts ListRelaysOptions) (RelayListResult, error) {
	query := url.Values{}
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	query.Set("show_disabled", "true")
	raw, err := c.get(ctx, "/trigger_instances/active", query)
	if err != nil {
		return RelayListResult{}, err
	}
	var result struct {
		Items []struct {
			ID                 string `json:"id"`
			TriggerName        string `json:"trigger_name"`
			ConnectedAccountID string `json:"connected_account_id"`
			UpdatedAt          string `json:"updated_at"`
			DisabledAt         string `json:"disabled_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return RelayListResult{}, fmt.Errorf("parse composio triggers: %w", err)
	}
	out := RelayListResult{Total: len(result.Items), Showing: len(result.Items)}
	for _, item := range result.Items {
		active := strings.TrimSpace(item.DisabledAt) == ""
		out.Endpoints = append(out.Endpoints, Relay{
			ID:           strings.TrimSpace(item.ID),
			Active:       active,
			Description:  strings.TrimSpace(item.TriggerName),
			EventFilters: composioCompactStrings([]string{strings.TrimSpace(item.TriggerName)}),
			CreatedAt:    strings.TrimSpace(item.UpdatedAt),
		})
	}
	return out, nil
}

func (c *ComposioREST) RelayEventTypes(ctx context.Context, platform string) (RelayEventTypesResult, error) {
	query := url.Values{}
	if p := normalizeComposioPlatform(platform); p != "" {
		query.Add("toolkit_slugs", p)
	}
	query.Set("limit", "100")
	raw, err := c.get(ctx, "/triggers_types", query)
	if err != nil {
		return RelayEventTypesResult{}, err
	}
	var result struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return RelayEventTypesResult{}, fmt.Errorf("parse composio trigger types: %w", err)
	}
	out := RelayEventTypesResult{Platform: platform}
	for _, item := range result.Items {
		out.EventTypes = append(out.EventTypes, strings.TrimSpace(item.Slug))
	}
	return out, nil
}

func (c *ComposioREST) CreateRelay(ctx context.Context, req RelayCreateRequest) (RelayResult, error) {
	if len(req.EventFilters) != 1 {
		return RelayResult{}, fmt.Errorf("composio trigger registration currently requires exactly one event filter")
	}
	triggerSlug := strings.TrimSpace(req.EventFilters[0])
	raw, err := c.post(ctx, "/trigger_instances/"+url.PathEscape(triggerSlug)+"/upsert", map[string]any{
		"connected_account_id": strings.TrimSpace(req.ConnectionKey),
		"trigger_config":       map[string]any{},
	})
	if err != nil {
		return RelayResult{}, err
	}
	var result struct {
		TriggerID string `json:"trigger_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return RelayResult{}, fmt.Errorf("parse composio trigger create: %w", err)
	}
	return RelayResult{
		ID:           strings.TrimSpace(result.TriggerID),
		Active:       true,
		Description:  strings.TrimSpace(req.Description),
		EventFilters: composioCompactStrings(req.EventFilters),
	}, nil
}

func (c *ComposioREST) ActivateRelay(ctx context.Context, req RelayActivateRequest) (RelayResult, error) {
	_, err := c.patch(ctx, "/trigger_instances/manage/"+url.PathEscape(strings.TrimSpace(req.ID)), map[string]any{
		"status": "enable",
	})
	if err != nil {
		return RelayResult{}, err
	}
	return RelayResult{
		ID:     strings.TrimSpace(req.ID),
		Active: true,
	}, nil
}

func (c *ComposioREST) ListRelayEvents(context.Context, RelayEventsOptions) (RelayEventsResult, error) {
	return RelayEventsResult{}, fmt.Errorf("composio trigger event polling is not wired into WUPHF yet")
}

func (c *ComposioREST) GetRelayEvent(context.Context, string) (RelayEventDetail, error) {
	return RelayEventDetail{}, fmt.Errorf("composio trigger event fetch is not wired into WUPHF yet")
}

func (c *ComposioREST) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, query, nil)
}

func (c *ComposioREST) post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, nil, body)
}

func (c *ComposioREST) patch(ctx context.Context, path string, body any) ([]byte, error) {
	return c.do(ctx, http.MethodPatch, path, nil, body)
}

func (c *ComposioREST) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("composio is not configured; set COMPOSIO_API_KEY and a user identity")
	}
	u := strings.TrimRight(c.BaseURL, "/") + path
	if encoded := query.Encode(); encoded != "" {
		u += "?" + encoded
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("composio API failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

type composioConnectedAccount struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Status string `json:"status"`
}

func (c *ComposioREST) connectedAccount(ctx context.Context, id string) (composioConnectedAccount, error) {
	raw, err := c.get(ctx, "/connected_accounts/"+url.PathEscape(strings.TrimSpace(id)), nil)
	if err != nil {
		return composioConnectedAccount{}, err
	}
	var result composioConnectedAccount
	if err := json.Unmarshal(raw, &result); err != nil {
		return composioConnectedAccount{}, fmt.Errorf("parse composio connected account: %w", err)
	}
	return result, nil
}

func normalizeComposioPlatform(platform string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	p = strings.ReplaceAll(p, " ", "")
	p = strings.ReplaceAll(p, "_", "")
	switch p {
	case "googlecalendar":
		return "googlecalendar"
	case "hubspot":
		return "hubspot"
	case "salesforce":
		return "salesforce"
	case "gmail":
		return "gmail"
	case "slack":
		return "slack"
	default:
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(platform)), "_", "")
	}
}

func composioCompactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func configResolveComposioBaseURL() string {
	if v := strings.TrimSpace(strings.TrimRight(os.Getenv("WUPHF_COMPOSIO_BASE_URL"), "/")); v != "" {
		return v
	}
	if v := strings.TrimSpace(strings.TrimRight(os.Getenv("COMPOSIO_BASE_URL"), "/")); v != "" {
		return v
	}
	return defaultComposioBaseURL
}

func compactStrings(ss []string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
