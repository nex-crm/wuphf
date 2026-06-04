package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
)

const maxIntegrationRequestBytes = 1 << 20

type integrationProviderStatus struct {
	Provider           string `json:"provider"`
	Label              string `json:"label"`
	Configured         bool   `json:"configured"`
	SupportsConnect    bool   `json:"supports_connect"`
	SupportsDisconnect bool   `json:"supports_disconnect"`
	Detail             string `json:"detail,omitempty"`
}

type integrationsResponse struct {
	Providers  []integrationProviderStatus     `json:"providers"`
	Items      []action.IntegrationCatalogItem `json:"items"`
	NextCursor string                          `json:"next_cursor,omitempty"`
}

type integrationAuditEvent struct {
	ID            string            `json:"id"`
	EventType     string            `json:"event_type"`
	Provider      string            `json:"provider,omitempty"`
	Platform      string            `json:"platform,omitempty"`
	ConnectionKey string            `json:"connection_key,omitempty"`
	ActionID      string            `json:"action_id,omitempty"`
	Status        string            `json:"status,omitempty"`
	Actor         string            `json:"actor,omitempty"`
	Channel       string            `json:"channel,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	RelatedID     string            `json:"related_id,omitempty"`
	CreatedAt     string            `json:"created_at"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

func (b *Broker) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	providerFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	opts := action.IntegrationCatalogOptions{
		Search:    strings.TrimSpace(r.URL.Query().Get("search")),
		Connected: strings.TrimSpace(r.URL.Query().Get("connected")),
		Cursor:    strings.TrimSpace(r.URL.Query().Get("cursor")),
		Limit:     parseIntegrationLimit(r.URL.Query().Get("limit"), 50),
	}

	composio := action.NewComposioFromEnv()
	resp := integrationsResponse{Providers: []integrationProviderStatus{
		{
			Provider:           "composio",
			Label:              "Composio",
			Configured:         composio.Configured(),
			SupportsConnect:    true,
			SupportsDisconnect: true,
			Detail:             providerDetail(composio.Configured(), "COMPOSIO_API_KEY and COMPOSIO_USER_ID are required for OAuth-managed integrations."),
		},
	}}

	if providerFilter == "" || providerFilter == "composio" {
		if composio.Configured() {
			catalog, err := composio.ListIntegrationCatalog(r.Context(), opts)
			if err != nil {
				setIntegrationProviderDetail(resp.Providers, "composio", "Composio unavailable: "+err.Error())
				resp.Items = append(resp.Items, curatedComposioCatalog(opts, true)...)
			} else {
				resp.Items = append(resp.Items, catalog.Items...)
				resp.NextCursor = catalog.NextCursor
			}
		} else {
			resp.Items = append(resp.Items, curatedComposioCatalog(opts, false)...)
		}
	}
	b.decorateIntegrationItems(&resp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (b *Broker) handleIntegrationConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req action.IntegrationConnectRequest
	if !decodeIntegrationRequest(w, r, &req) {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "composio"
	}
	if provider != "composio" {
		http.Error(w, "only composio supports web-managed connect", http.StatusBadRequest)
		return
	}
	composio := action.NewComposioFromEnv()
	result, err := composio.StartIntegrationConnection(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("start composio connection: %v", err), http.StatusBadGateway)
		return
	}
	actor := integrationRequestActor(r)
	_ = b.RecordActionWithMetadata(
		"integration_connect_started",
		"composio",
		"general",
		actor,
		fmt.Sprintf("Started %s connection via Composio", action.DisplayPlatformName(result.Platform)),
		result.Platform,
		nil,
		"",
		map[string]string{
			"provider": "composio",
			"platform": result.Platform,
			"status":   result.Status,
		},
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (b *Broker) handleIntegrationConnectStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		provider = "composio"
	}
	if provider != "composio" {
		http.Error(w, "only composio supports web-managed connect status", http.StatusBadRequest)
		return
	}
	composio := action.NewComposioFromEnv()
	result, err := composio.GetIntegrationConnectionStatus(r.Context(), action.IntegrationStatusRequest{
		Provider:  provider,
		Platform:  strings.TrimSpace(r.URL.Query().Get("platform")),
		ConnectID: strings.TrimSpace(r.URL.Query().Get("connect_id")),
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("check composio connection: %v", err), http.StatusBadGateway)
		return
	}
	if result.Status == "connected" && result.ConnectionKey != "" && !b.hasIntegrationAction("integration_connected", "composio", result.ConnectionKey) {
		_ = b.RecordActionWithMetadata(
			"integration_connected",
			"composio",
			"general",
			integrationRequestActor(r),
			fmt.Sprintf("Connected %s via Composio", action.DisplayPlatformName(result.Platform)),
			result.ConnectionKey,
			nil,
			"",
			map[string]string{
				"provider":       "composio",
				"platform":       result.Platform,
				"connection_key": result.ConnectionKey,
				"status":         "connected",
			},
		)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (b *Broker) handleIntegrationDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req action.IntegrationDisconnectRequest
	if !decodeIntegrationRequest(w, r, &req) {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "composio"
	}
	if provider != "composio" {
		http.Error(w, "only composio supports web-managed disconnect", http.StatusBadRequest)
		return
	}
	composio := action.NewComposioFromEnv()
	result, err := composio.DisconnectIntegration(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("disconnect composio connection: %v", err), http.StatusBadGateway)
		return
	}
	_ = b.RecordActionWithMetadata(
		"integration_disconnected",
		"composio",
		"general",
		integrationRequestActor(r),
		fmt.Sprintf("Disconnected %s via Composio", integrationAuditPlatform(result.Platform)),
		result.ConnectionKey,
		nil,
		"",
		map[string]string{
			"provider":       "composio",
			"platform":       result.Platform,
			"connection_key": result.ConnectionKey,
			"status":         "disconnected",
		},
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (b *Broker) handleIntegrationAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	events := b.integrationAuditEvents(
		strings.TrimSpace(r.URL.Query().Get("provider")),
		strings.TrimSpace(r.URL.Query().Get("platform")),
		strings.TrimSpace(r.URL.Query().Get("connection_key")),
		parseIntegrationLimit(r.URL.Query().Get("limit"), 100),
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"events": events})
}

func decodeIntegrationRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxIntegrationRequestBytes))
	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid json", http.StatusBadRequest)
		}
		return false
	}
	return true
}

func integrationAuditPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return "unknown platform"
	}
	return action.DisplayPlatformName(platform)
}

type curatedComposioToolkit struct {
	platform    string
	name        string
	description string
	category    string
	logoURL     string
}

var curatedComposioToolkits = []curatedComposioToolkit{
	{
		platform:    "gmail",
		name:        "Gmail",
		description: "Read, draft, search, and send Gmail messages after approval.",
		category:    "Communication",
	},
	{
		platform:    "slack",
		name:        "Slack",
		description: "Post channel updates, read threads, and route workspace context.",
		category:    "Communication",
	},
	{
		platform:    "github",
		name:        "GitHub",
		description: "Inspect pull requests, create issues, and update repository work.",
		category:    "Code",
	},
	{
		platform:    "googlecalendar",
		name:        "Google Calendar",
		description: "Read availability, schedule meetings, and manage calendar events.",
		category:    "Productivity",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/googlecalendar.svg",
	},
	{
		platform:    "googledrive",
		name:        "Google Drive",
		description: "Find, read, and organize workspace files with approval.",
		category:    "Documents",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/googledrive.svg",
	},
	{
		platform:    "notion",
		name:        "Notion",
		description: "Search pages, update databases, and keep project notes current.",
		category:    "Knowledge",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/notion.svg",
	},
	{
		platform:    "linear",
		name:        "Linear",
		description: "Create issues, update cycles, and track engineering work.",
		category:    "Project Management",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/linear.svg",
	},
	{
		platform:    "jira",
		name:        "Jira",
		description: "Read tickets, transition issues, and synchronize delivery state.",
		category:    "Project Management",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/jira.svg",
	},
	{
		platform:    "hubspot",
		name:        "HubSpot",
		description: "Update contacts, companies, deals, and revenue workflow records.",
		category:    "Revenue",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/hubspot.svg",
	},
	{
		platform:    "salesforce",
		name:        "Salesforce",
		description: "Read and update account, opportunity, and lead records.",
		category:    "Revenue",
		logoURL:     "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/salesforce.svg",
	},
}

func curatedComposioCatalog(opts action.IntegrationCatalogOptions, configured bool) []action.IntegrationCatalogItem {
	connectedFilter := strings.ToLower(strings.TrimSpace(opts.Connected))
	if connectedFilter == "true" || connectedFilter == "1" || connectedFilter == "connected" {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(opts.Search))
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	state := "unconfigured"
	if configured {
		state = "available"
	}
	out := make([]action.IntegrationCatalogItem, 0, len(curatedComposioToolkits))
	for _, toolkit := range curatedComposioToolkits {
		if query != "" && !strings.Contains(strings.ToLower(toolkit.platform), query) && !strings.Contains(strings.ToLower(toolkit.name), query) && !strings.Contains(strings.ToLower(toolkit.category), query) {
			continue
		}
		out = append(out, action.IntegrationCatalogItem{
			Provider:      "composio",
			Platform:      toolkit.platform,
			Name:          toolkit.name,
			Description:   toolkit.description,
			Category:      toolkit.category,
			LogoURL:       toolkit.logoURL,
			State:         state,
			CanConnect:    configured,
			CanDisconnect: false,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (b *Broker) decorateIntegrationItems(resp *integrationsResponse) {
	events := b.integrationAuditEvents("", "", "", 500)
	latest := make(map[string]integrationAuditEvent)
	for _, event := range events {
		keys := []string{}
		if event.Provider != "" && event.Platform != "" {
			keys = append(keys, event.Provider+"::platform::"+strings.ToLower(event.Platform))
		}
		if event.Provider != "" && event.ConnectionKey != "" {
			keys = append(keys, event.Provider+"::connection::"+strings.ToLower(event.ConnectionKey))
		}
		for _, key := range keys {
			if _, ok := latest[key]; !ok {
				latest[key] = event
			}
		}
	}
	for i := range resp.Items {
		item := &resp.Items[i]
		keys := []string{
			item.Provider + "::platform::" + strings.ToLower(item.Platform),
			item.Provider + "::connection::" + strings.ToLower(item.ConnectionKey),
		}
		for _, key := range keys {
			if event, ok := latest[key]; ok {
				item.LastActionAt = event.CreatedAt
				item.LastActionSummary = event.Summary
				break
			}
		}
	}
}

func (b *Broker) integrationAuditEvents(provider, platform, connectionKey string, limit int) []integrationAuditEvent {
	provider = strings.ToLower(strings.TrimSpace(provider))
	platform = strings.ToLower(strings.TrimSpace(platform))
	connectionKey = strings.ToLower(strings.TrimSpace(connectionKey))
	b.mu.Lock()
	actions := make([]officeActionLog, len(b.actions))
	for i, act := range b.actions {
		actions[i] = sanitizeOfficeActionLog(act)
	}
	approvals := make([]ApprovalAuditEntry, len(b.approvalAudit))
	for i, entry := range b.approvalAudit {
		approvals[i] = sanitizeApprovalAuditEntry(entry)
	}
	b.mu.Unlock()

	var events []integrationAuditEvent
	for _, act := range actions {
		metadata := sanitizeActionMetadata(act.Metadata)
		eventProvider := strings.TrimSpace(metadata["provider"])
		if eventProvider == "" {
			eventProvider = strings.TrimSpace(act.Source)
		}
		eventPlatform := strings.TrimSpace(metadata["platform"])
		eventConnection := strings.TrimSpace(metadata["connection_key"])
		if !isIntegrationActionKind(act.Kind) {
			continue
		}
		if !matchesIntegrationFilter(provider, eventProvider) || !matchesIntegrationFilter(platform, eventPlatform) || !matchesIntegrationFilter(connectionKey, eventConnection) {
			continue
		}
		events = append(events, integrationAuditEvent{
			ID:            act.ID,
			EventType:     act.Kind,
			Provider:      eventProvider,
			Platform:      eventPlatform,
			ConnectionKey: eventConnection,
			ActionID:      strings.TrimSpace(metadata["action_id"]),
			Status:        strings.TrimSpace(metadata["status"]),
			Actor:         act.Actor,
			Channel:       act.Channel,
			Summary:       act.Summary,
			RelatedID:     act.RelatedID,
			CreatedAt:     act.CreatedAt,
			Metadata:      metadata,
		})
	}
	for _, entry := range approvals {
		if !matchesIntegrationFilter(platform, entry.Platform) || !matchesIntegrationFilter(connectionKey, entry.ConnectionKey) {
			continue
		}
		if provider != "" && provider != "approval" && platform == "" && connectionKey == "" {
			continue
		}
		events = append(events, integrationAuditEvent{
			ID:            entry.ApprovalRequestID,
			EventType:     "approval_" + strings.TrimSpace(entry.Outcome),
			Provider:      "approval",
			Platform:      entry.Platform,
			ConnectionKey: entry.ConnectionKey,
			ActionID:      entry.ActionID,
			Status:        entry.Outcome,
			Actor:         entry.Actor,
			Channel:       entry.Channel,
			Summary:       entry.OutcomeSummary,
			RelatedID:     entry.TaskID,
			CreatedAt:     entry.CreatedAt,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt > events[j].CreatedAt
	})
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events
}

func integrationRequestActor(r *http.Request) string {
	if actor, ok := requestActorFromContext(r.Context()); ok && actor.Kind == requestActorKindHuman {
		return humanMessageSender(actor.Slug)
	}
	return "human"
}

func isIntegrationActionKind(kind string) bool {
	kind = strings.TrimSpace(kind)
	return strings.HasPrefix(kind, "integration_") ||
		strings.HasPrefix(kind, "external_action_") ||
		strings.HasPrefix(kind, "external_workflow_") ||
		strings.HasPrefix(kind, "external_trigger_")
}

func (b *Broker) hasIntegrationAction(kind, provider, connectionKey string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	connectionKey = strings.ToLower(strings.TrimSpace(connectionKey))
	if kind == "" || provider == "" || connectionKey == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, act := range b.actions {
		if strings.TrimSpace(act.Kind) != kind {
			continue
		}
		metadata := sanitizeActionMetadata(act.Metadata)
		if strings.EqualFold(metadata["provider"], provider) &&
			strings.EqualFold(metadata["connection_key"], connectionKey) {
			return true
		}
	}
	return false
}

func matchesIntegrationFilter(filter, value string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	return strings.EqualFold(filter, strings.TrimSpace(value))
}

func parseIntegrationLimit(value string, fallback int) int {
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func providerDetail(configured bool, missing string) string {
	if configured {
		return "Configured"
	}
	return missing
}

func setIntegrationProviderDetail(providers []integrationProviderStatus, provider, detail string) {
	for i := range providers {
		if providers[i].Provider == provider {
			providers[i].Detail = detail
			return
		}
	}
}
