package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
	"github.com/nex-crm/wuphf/internal/provider"
)

func (b *Broker) handleCompany(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.Load()
		if err != nil {
			http.Error(w, "config load failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":        cfg.CompanyName,
			"description": cfg.CompanyDescription,
			"goals":       cfg.CompanyGoals,
			"size":        cfg.CompanySize,
			"priority":    cfg.CompanyPriority,
		})
	case http.MethodPost:
		// /company and /config write the same file; share the same lock so
		// a concurrent /config POST cannot interleave a partial read here
		// with a Save and lose fields.
		b.configMu.Lock()
		defer b.configMu.Unlock()
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Goals       string `json:"goals"`
			Size        string `json:"size"`
			Priority    string `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cfg, err := config.Load()
		if err != nil {
			// Refuse to proceed: writing back a zero-value cfg with our
			// fields layered on would clobber whatever else lived in the
			// file under a transient read failure.
			http.Error(w, "config load failed", http.StatusInternalServerError)
			return
		}
		if body.Name != "" {
			cfg.CompanyName = strings.TrimSpace(body.Name)
		}
		if body.Description != "" {
			cfg.CompanyDescription = strings.TrimSpace(body.Description)
		}
		if body.Goals != "" {
			cfg.CompanyGoals = strings.TrimSpace(body.Goals)
		}
		if body.Size != "" {
			cfg.CompanySize = strings.TrimSpace(body.Size)
		}
		if body.Priority != "" {
			cfg.CompanyPriority = strings.TrimSpace(body.Priority)
		}
		if err := config.Save(cfg); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// validateProviderEndpointURL gates user-supplied base URLs persisted
// to ~/.wuphf/config.json so a locally-authenticated client can't
// pivot future agent turns to attacker-controlled targets via
// schemes our HTTP client doesn't service (or persist URLs that
// would surprise users on next launch). Allowed: http://… and
// https://… with a non-empty host. Rejected: file://, gopher://,
// unix://, schemeless paths, hostless URLs, raw IPs without scheme,
// userinfo-only URLs, etc.
//
// Loopback hosts are allowed — wuphf's whole point is local-LLM
// pointing at 127.0.0.1, and the runtime probe code already gates
// reachability scans on isLoopbackBaseURL elsewhere. The threat we
// care about here is "URL the agent runner will later POST a
// system prompt + conversation to," which is governed by scheme +
// host, not by loopback-vs-public.
func validateProviderEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		// ok
	case "":
		return fmt.Errorf("missing scheme (must be http or https)")
	default:
		return fmt.Errorf("unsupported scheme %q (must be http or https)", u.Scheme)
	}
	// Use Hostname() not Host: url.Parse("http://:8080") yields
	// Host=":8080" but Hostname()="", so checking Host would let a
	// port-only URL through and persist a hostless endpoint that
	// fails later at request time.
	if strings.TrimSpace(u.Hostname()) == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

// handleConfig exposes GET/POST over ~/.wuphf/config.json for the web UI
// settings page and onboarding wizard. All POST fields are optional; clients
// can update one without touching the others. Secret fields (API keys, tokens)
// are returned as boolean flags on GET and accepted as plain values on POST.
//
// TODO(broker-split): this 400-line handler is ripe for a parser/applier
// split — see the broker.go decomposition plan. Currently a faithful
// monolithic relocation; the validation, secret-mask, and persistence
// concerns should be isolated in a follow-up pass once the slice series
// has soaked.
func (b *Broker) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.Load()
		if err != nil {
			http.Error(w, "failed to read config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			// Runtime
			"llm_provider":          config.ResolveLLMProvider(""),
			"llm_provider_priority": cfg.LLMProviderPriority,
			"provider_endpoints":    cfg.ProviderEndpoints,
			"memory_backend":        config.ResolveMemoryBackend(""),
			"action_provider":       config.ResolveActionProvider(),
			"team_lead_slug":        cfg.TeamLeadSlug,
			"max_concurrent_agents": cfg.MaxConcurrent,
			"default_format":        config.ResolveFormat(""),
			"default_timeout":       config.ResolveTimeout(""),
			"blueprint":             cfg.ActiveBlueprint(),
			// Workspace
			"email":          cfg.Email,
			"workspace_id":   cfg.WorkspaceID,
			"workspace_slug": cfg.WorkspaceSlug,
			"dev_url":        cfg.DevURL,
			// Company
			"company_name":        cfg.CompanyName,
			"company_description": cfg.CompanyDescription,
			"company_goals":       cfg.CompanyGoals,
			"company_size":        cfg.CompanySize,
			"company_priority":    cfg.CompanyPriority,
			// Polling intervals
			"insights_poll_minutes":  config.ResolveInsightsPollInterval(),
			"task_follow_up_minutes": config.ResolveTaskFollowUpInterval(),
			"task_reminder_minutes":  config.ResolveTaskReminderInterval(),
			"task_recheck_minutes":   config.ResolveTaskRecheckInterval(),
			// Integrations — secret fields as booleans
			"api_key_set":          config.ResolveAPIKey("") != "",
			"openai_key_set":       config.ResolveOpenAIAPIKey() != "",
			"anthropic_key_set":    config.ResolveAnthropicAPIKey() != "",
			"gemini_key_set":       config.ResolveGeminiAPIKey() != "",
			"minimax_key_set":      config.ResolveMinimaxAPIKey() != "",
			"one_key_set":          config.ResolveOneSecret() != "",
			"composio_key_set":     config.ResolveComposioAPIKey() != "",
			"telegram_token_set":   config.ResolveTelegramBotToken() != "",
			"openclaw_token_set":   config.ResolveOpenclawToken() != "",
			"openclaw_gateway_url": config.ResolveOpenclawGatewayURL(),
			// Config file path (informational)
			"config_path": config.ConfigPath(),
		})
	case http.MethodPost:
		// Serialize POST reads/writes; config.Save is not atomic against
		// concurrent writers and two parallel calls can corrupt the file.
		b.configMu.Lock()
		defer b.configMu.Unlock()
		var body struct {
			LLMProvider         *string   `json:"llm_provider,omitempty"`
			LLMProviderPriority *[]string `json:"llm_provider_priority,omitempty"`
			MemoryBackend       *string   `json:"memory_backend,omitempty"`
			ActionProvider      *string   `json:"action_provider,omitempty"`
			TeamLeadSlug        *string   `json:"team_lead_slug,omitempty"`
			MaxConcurrent       *int      `json:"max_concurrent_agents,omitempty"`
			DefaultFormat       *string   `json:"default_format,omitempty"`
			DefaultTimeout      *int      `json:"default_timeout,omitempty"`
			Blueprint           *string   `json:"blueprint,omitempty"`
			Email               *string   `json:"email,omitempty"`
			DevURL              *string   `json:"dev_url,omitempty"`
			CompanyName         *string   `json:"company_name,omitempty"`
			CompanyDesc         *string   `json:"company_description,omitempty"`
			CompanyGoals        *string   `json:"company_goals,omitempty"`
			CompanySize         *string   `json:"company_size,omitempty"`
			CompanyPriority     *string   `json:"company_priority,omitempty"`
			InsightsPoll        *int      `json:"insights_poll_minutes,omitempty"`
			TaskFollowUp        *int      `json:"task_follow_up_minutes,omitempty"`
			TaskReminder        *int      `json:"task_reminder_minutes,omitempty"`
			TaskRecheck         *int      `json:"task_recheck_minutes,omitempty"`
			// Secret fields
			APIKey          *string `json:"api_key,omitempty"`
			OpenAIAPIKey    *string `json:"openai_api_key,omitempty"`
			AnthropicAPIKey *string `json:"anthropic_api_key,omitempty"`
			GeminiAPIKey    *string `json:"gemini_api_key,omitempty"`
			MinimaxAPIKey   *string `json:"minimax_api_key,omitempty"`
			OneAPIKey       *string `json:"one_api_key,omitempty"`
			ComposioAPIKey  *string `json:"composio_api_key,omitempty"`
			TelegramToken   *string `json:"telegram_bot_token,omitempty"`
			OpenclawToken   *string `json:"openclaw_token,omitempty"`
			OpenclawGateway *string `json:"openclaw_gateway_url,omitempty"`
			// ProviderEndpoints is a partial-update map: keys present in
			// the payload replace the corresponding entry; absent keys are
			// preserved. Pass an empty value (`{"base_url":"","model":""}`)
			// to clear a kind back to its compile-time defaults.
			ProviderEndpoints *map[string]config.ProviderEndpoint `json:"provider_endpoints,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Validate enum fields before touching config. The "global LLM
		// provider" surface (llm_provider, llm_provider_priority, and
		// the provider_endpoints map keys) must use
		// config.IsLLMProviderKindAllowed — provider.ValidateKind is
		// broader and accepts member-only kinds like openclaw that the
		// runtime launcher can't dispatch as a global default. Per-
		// member binding kinds keep ValidateKind below.
		//
		// Nil pointer vs empty string: a nil body field means "the
		// client didn't send it, leave the saved value alone"; an
		// explicit empty string means "clear my override and fall back
		// to the install default". Both must round-trip.
		var (
			llmProvider    string
			llmProviderSet bool
		)
		if body.LLMProvider != nil {
			llmProviderSet = true
			llmProvider = strings.TrimSpace(strings.ToLower(*body.LLMProvider))
			if llmProvider != "" && !config.IsLLMProviderKindAllowed(llmProvider) {
				http.Error(w, "unsupported llm_provider "+strconv.Quote(llmProvider)+
					" (allowed: "+strings.Join(config.AllowedLLMProviderKinds(), ", ")+")",
					http.StatusBadRequest)
				return
			}
		}
		var providerPriority []string
		if body.LLMProviderPriority != nil {
			// Normalize + validate each entry. Unknown entries are rejected so
			// the stored list only contains provider ids the resolver knows how
			// to dispatch. Empty list is accepted (means "clear").
			seen := make(map[string]bool, len(*body.LLMProviderPriority))
			for _, raw := range *body.LLMProviderPriority {
				id := strings.TrimSpace(strings.ToLower(raw))
				if id == "" {
					continue
				}
				if !config.IsLLMProviderKindAllowed(id) {
					http.Error(w, "unsupported entry in llm_provider_priority: "+strconv.Quote(id)+
						" (allowed: "+strings.Join(config.AllowedLLMProviderKinds(), ", ")+")",
						http.StatusBadRequest)
					return
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				providerPriority = append(providerPriority, id)
			}
		}
		var memory string
		if body.MemoryBackend != nil {
			memory = config.NormalizeMemoryBackend(*body.MemoryBackend)
			if memory == "" {
				http.Error(w, "unsupported memory_backend", http.StatusBadRequest)
				return
			}
		}

		cfg, err := config.Load()
		if err != nil {
			// A transient read failure must not turn into a destructive
			// writeback of a zero-value config plus whichever fields the
			// client supplied — that would silently clobber any field the
			// client didn't send (provider keys, custom endpoints, etc.).
			http.Error(w, "config load failed", http.StatusInternalServerError)
			return
		}
		changed := false

		// Enum/string fields. `llmProviderSet` distinguishes "client
		// sent the field with a value" (use it) and "client sent the
		// field with empty string" (clear back to install default)
		// from "client didn't send the field" (leave alone). Without
		// this distinction the Settings UI couldn't drop a saved
		// provider override.
		if llmProviderSet {
			cfg.LLMProvider = llmProvider
			changed = true
		}
		if body.LLMProviderPriority != nil {
			// Explicit pointer set means the client wanted to write this field,
			// even if the final list is empty (which clears the stored order).
			cfg.LLMProviderPriority = providerPriority
			changed = true
		}
		if memory != "" {
			cfg.MemoryBackend = memory
			changed = true
		}
		if body.ActionProvider != nil {
			ap := strings.TrimSpace(strings.ToLower(*body.ActionProvider))
			switch ap {
			case "auto", "one", "composio", "":
				cfg.ActionProvider = ap
				changed = true
			default:
				http.Error(w, "unsupported action_provider", http.StatusBadRequest)
				return
			}
		}
		if body.TeamLeadSlug != nil {
			cfg.TeamLeadSlug = strings.TrimSpace(*body.TeamLeadSlug)
			changed = true
		}
		if body.MaxConcurrent != nil {
			cfg.MaxConcurrent = *body.MaxConcurrent
			changed = true
		}
		if body.DefaultFormat != nil {
			cfg.DefaultFormat = strings.TrimSpace(*body.DefaultFormat)
			changed = true
		}
		if body.DefaultTimeout != nil {
			cfg.DefaultTimeout = *body.DefaultTimeout
			changed = true
		}
		if body.Blueprint != nil {
			cfg.SetActiveBlueprint(*body.Blueprint)
			changed = true
		}
		if body.Email != nil {
			cfg.Email = strings.TrimSpace(*body.Email)
			changed = true
		}
		if body.DevURL != nil {
			cfg.DevURL = strings.TrimSpace(*body.DevURL)
			changed = true
		}
		// Company
		if body.CompanyName != nil {
			cfg.CompanyName = strings.TrimSpace(*body.CompanyName)
			changed = true
		}
		if body.CompanyDesc != nil {
			cfg.CompanyDescription = strings.TrimSpace(*body.CompanyDesc)
			changed = true
		}
		if body.CompanyGoals != nil {
			cfg.CompanyGoals = strings.TrimSpace(*body.CompanyGoals)
			changed = true
		}
		if body.CompanySize != nil {
			cfg.CompanySize = strings.TrimSpace(*body.CompanySize)
			changed = true
		}
		if body.CompanyPriority != nil {
			cfg.CompanyPriority = strings.TrimSpace(*body.CompanyPriority)
			changed = true
		}
		// Polling intervals (minimum 2 minutes, matching resolve functions)
		if body.InsightsPoll != nil {
			if *body.InsightsPoll < 2 {
				http.Error(w, "insights_poll_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.InsightsPollMinutes = *body.InsightsPoll
			changed = true
		}
		if body.TaskFollowUp != nil {
			if *body.TaskFollowUp < 2 {
				http.Error(w, "task_follow_up_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.TaskFollowUpMinutes = *body.TaskFollowUp
			changed = true
		}
		if body.TaskReminder != nil {
			if *body.TaskReminder < 2 {
				http.Error(w, "task_reminder_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.TaskReminderMinutes = *body.TaskReminder
			changed = true
		}
		if body.TaskRecheck != nil {
			if *body.TaskRecheck < 2 {
				http.Error(w, "task_recheck_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.TaskRecheckMinutes = *body.TaskRecheck
			changed = true
		}
		// Secret fields
		if body.APIKey != nil {
			cfg.APIKey = strings.TrimSpace(*body.APIKey)
			changed = true
		}
		if body.OpenAIAPIKey != nil {
			cfg.OpenAIAPIKey = strings.TrimSpace(*body.OpenAIAPIKey)
			changed = true
		}
		if body.AnthropicAPIKey != nil {
			cfg.AnthropicAPIKey = strings.TrimSpace(*body.AnthropicAPIKey)
			changed = true
		}
		if body.GeminiAPIKey != nil {
			cfg.GeminiAPIKey = strings.TrimSpace(*body.GeminiAPIKey)
			changed = true
		}
		if body.MinimaxAPIKey != nil {
			cfg.MinimaxAPIKey = strings.TrimSpace(*body.MinimaxAPIKey)
			changed = true
		}
		if body.OneAPIKey != nil {
			cfg.OneAPIKey = strings.TrimSpace(*body.OneAPIKey)
			changed = true
		}
		if body.ComposioAPIKey != nil {
			cfg.ComposioAPIKey = strings.TrimSpace(*body.ComposioAPIKey)
			changed = true
		}
		if body.TelegramToken != nil {
			cfg.TelegramBotToken = strings.TrimSpace(*body.TelegramToken)
			changed = true
		}
		if body.OpenclawToken != nil {
			cfg.OpenclawToken = strings.TrimSpace(*body.OpenclawToken)
			changed = true
		}
		if body.OpenclawGateway != nil {
			cfg.OpenclawGatewayURL = strings.TrimSpace(*body.OpenclawGateway)
			changed = true
		}
		if body.ProviderEndpoints != nil {
			// Partial merge: only kinds present in the payload are touched,
			// so the Settings UI can update one runtime's endpoint without
			// shipping the whole map. Validate each key against the
			// registry — same source of truth as llm_provider. `changed`
			// flips ONLY when at least one entry actually mutates state,
			// so an empty-map payload (or one whose entries are all
			// empty-key skips) doesn't force a config.Save round-trip.
			if cfg.ProviderEndpoints == nil {
				cfg.ProviderEndpoints = map[string]config.ProviderEndpoint{}
			}
			for kind, ep := range *body.ProviderEndpoints {
				k := strings.TrimSpace(strings.ToLower(kind))
				if k == "" {
					continue
				}
				// provider_endpoints keys must be runnable global LLM
				// kinds (mlx-lm/ollama/exo/claude-code/codex/...) —
				// openclaw, while a valid per-member binding, has no
				// HTTP base_url + model concept and must not get a row
				// in this map.
				if !config.IsLLMProviderKindAllowed(k) {
					http.Error(w, "unsupported provider_endpoints kind: "+strconv.Quote(k)+
						" (allowed: "+strings.Join(config.AllowedLLMProviderKinds(), ", ")+")",
						http.StatusBadRequest)
					return
				}
				ep.BaseURL = strings.TrimSpace(ep.BaseURL)
				ep.Model = strings.TrimSpace(ep.Model)
				// Security gate: a malicious authenticated client (or
				// anyone with write access to ~/.wuphf/config.json) must
				// not be able to redirect future agent turns to file://,
				// gopher://, unix://, or schemeless URLs. Allow only the
				// two URL families our HTTP client actually services
				// (http, https) and require a non-empty host so a
				// `http://` no-op can't slip through.
				if ep.BaseURL != "" {
					if err := validateProviderEndpointURL(ep.BaseURL); err != nil {
						http.Error(w, "invalid provider_endpoints["+k+"].base_url: "+err.Error(), http.StatusBadRequest)
						return
					}
				}
				if ep.BaseURL == "" && ep.Model == "" {
					// Treat the empty-empty case as a clear so the user can
					// drop their override and fall back to compile-time
					// defaults without hand-editing config.json.
					if _, existed := cfg.ProviderEndpoints[k]; existed {
						delete(cfg.ProviderEndpoints, k)
						changed = true
					}
				} else {
					prev, existed := cfg.ProviderEndpoints[k]
					if !existed || prev != ep {
						cfg.ProviderEndpoints[k] = ep
						changed = true
					}
				}
			}
		}

		if !changed {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		if err := config.Save(cfg); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		// Keep /health in sync for this process so the wizard choice
		// (or a clear back to default) is reflected immediately
		// without requiring a broker restart. Use `llmProviderSet`
		// for the same reason described at the write-back above —
		// nil-vs-empty must round-trip, otherwise /health keeps
		// reporting the stale provider after a clear.
		if llmProviderSet {
			b.mu.Lock()
			providerChanged := b.runtimeProvider != llmProvider
			b.runtimeProvider = llmProvider
			if providerChanged {
				b.publishOfficeChangeLocked(officeChangeEvent{Kind: "office_reseeded"})
			}
			b.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNexRegister wraps `nex-cli --cmd "setup <email>"` so the onboarding
// wizard can register a Nex identity without the user dropping to the terminal.
// Body: {"email": "..."}. Returns whatever the CLI prints on success, or the
// CLI's stderr on failure. Requires nex-cli to be installed and on PATH.
func (b *Broker) handleNexRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	output, err := nex.Register(r.Context(), email)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"email":  email,
		"output": output,
	})
}

// TODO(broker-split): this 380-line handler mixes parsing, validation,
// channel-membership maintenance, and persistence. Faithful monolithic
// relocation for now — split into parser/applier in a follow-up pass.
func (b *Broker) handleOfficeMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		type officeMemberResponse struct {
			officeMember
			Status       string `json:"status,omitempty"`
			Activity     string `json:"activity,omitempty"`
			Detail       string `json:"detail,omitempty"`
			Task         string `json:"task,omitempty"`
			LiveActivity string `json:"liveActivity,omitempty"`
			LastTime     string `json:"lastTime,omitempty"`
		}
		now := time.Now()
		members := make([]officeMemberResponse, 0, len(b.members))
		for _, member := range b.members {
			entry := officeMemberResponse{officeMember: member}
			if snapshot, ok := b.activity[member.Slug]; ok {
				entry.Status = snapshot.Status
				entry.Activity = snapshot.Activity
				entry.Detail = snapshot.Detail
				entry.LiveActivity = snapshot.Detail
				entry.Task = snapshot.Detail
				entry.LastTime = snapshot.LastTime
			}
			if entry.Status == "" && b.lastTaggedAt != nil {
				if taggedAt, ok := b.lastTaggedAt[member.Slug]; ok && now.Sub(taggedAt) < 60*time.Second {
					entry.Status = "active"
					entry.Activity = "queued"
					entry.Detail = "active"
					entry.LiveActivity = "active"
					entry.Task = "active"
					entry.LastTime = taggedAt.UTC().Format(time.RFC3339)
				}
			}
			if entry.Status == "" {
				entry.Status = "idle"
			}
			if entry.Activity == "" {
				entry.Activity = "idle"
			}
			members = append(members, entry)
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"members": members})
	case http.MethodPost:
		var body struct {
			Action         string                    `json:"action"`
			Slug           string                    `json:"slug"`
			Name           string                    `json:"name"`
			Role           string                    `json:"role"`
			Expertise      []string                  `json:"expertise"`
			Personality    string                    `json:"personality"`
			PermissionMode string                    `json:"permission_mode"`
			AllowedTools   []string                  `json:"allowed_tools"`
			CreatedBy      string                    `json:"created_by"`
			Provider       *provider.ProviderBinding `json:"provider,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(body.Action)
		slug := normalizeChannelSlug(body.Slug)
		if slug == "" {
			http.Error(w, "slug required", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)

		b.mu.Lock()
		ensureNotebookDirsAfterUnlock := false
		defer func() {
			if ensureNotebookDirsAfterUnlock {
				b.ensureNotebookDirsForRoster()
			}
		}()
		defer b.mu.Unlock()
		switch action {
		case "create":
			if b.findMemberLocked(slug) != nil {
				http.Error(w, "member already exists", http.StatusConflict)
				return
			}
			if body.Provider != nil {
				if err := provider.ValidateKind(body.Provider.Kind); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			member := officeMember{
				Slug:           slug,
				Name:           strings.TrimSpace(body.Name),
				Role:           strings.TrimSpace(body.Role),
				Expertise:      normalizeStringList(body.Expertise),
				Personality:    strings.TrimSpace(body.Personality),
				PermissionMode: strings.TrimSpace(body.PermissionMode),
				AllowedTools:   normalizeStringList(body.AllowedTools),
				CreatedBy:      strings.TrimSpace(body.CreatedBy),
				CreatedAt:      now,
			}
			if body.Provider != nil {
				member.Provider = *body.Provider
			}
			applyOfficeMemberDefaults(&member)

			// For openclaw agents, reach the gateway BEFORE we persist: if the
			// caller didn't supply a session key, auto-create one; either way,
			// attach the bridge subscription. If the gateway is unreachable we
			// fail the whole create so we don't persist a half-configured
			// member that can't actually talk.
			if member.Provider.Kind == provider.KindOpenclaw {
				if member.Provider.Openclaw == nil {
					member.Provider.Openclaw = &provider.OpenclawProviderBinding{}
				}
				bridge := b.openclawBridgeLocked()
				if bridge == nil {
					http.Error(w, "openclaw bridge not active; cannot create openclaw member", http.StatusServiceUnavailable)
					return
				}
				if member.Provider.Openclaw.SessionKey == "" {
					agentID := member.Provider.Openclaw.AgentID
					if agentID == "" {
						agentID = "main"
					}
					label := fmt.Sprintf("wuphf-%s-%d", slug, time.Now().UnixNano())
					key, err := bridge.CreateSession(r.Context(), agentID, label)
					if err != nil {
						http.Error(w, fmt.Sprintf("openclaw sessions.create: %v", err), http.StatusBadGateway)
						return
					}
					member.Provider.Openclaw.SessionKey = key
				}
				if err := bridge.AttachSlug(r.Context(), slug, member.Provider.Openclaw.SessionKey); err != nil {
					http.Error(w, fmt.Sprintf("openclaw subscribe: %v", err), http.StatusBadGateway)
					return
				}
			}

			b.members = append(b.members, member)
			b.memberIndex[member.Slug] = len(b.members) - 1
			// Add the new hire to every non-DM channel's Members list so they
			// can actually POST replies. canAccessChannelLocked enforces
			// ch.Members for every non-CEO agent sender; without this, a
			// wizard-hired specialist can be tagged and dispatched but its
			// reply is 403'd with "channel access denied" and the user sees
			// nothing. DM channels are intentionally skipped — DMs encode
			// the target agent in the slug and go through a different
			// membership gate.
			//
			// Policy note: this is broader than normalizeLoadedStateLocked's
			// seed (which only fills #general). A wizard hire joins every
			// topical channel by default; admins can narrow via
			// /channel-members action=remove afterwards. The rationale is
			// that an office member who can't post to any non-default
			// channel without a second configuration step violates the
			// principle of least surprise — the hire UI does not surface a
			// channel-scope picker, so the implicit default has to be
			// "office-wide."
			//
			// We also clear any stale Disabled entry for this slug. A fresh
			// hire shouldn't inherit a mute left over from a prior lifecycle.
			updatedChannels := make([]string, 0, len(b.channels))
			for i := range b.channels {
				if b.channels[i].isDM() {
					continue
				}
				mutated := false
				if !containsString(b.channels[i].Members, slug) {
					b.channels[i].Members = uniqueSlugs(append(b.channels[i].Members, slug))
					mutated = true
				}
				if containsString(b.channels[i].Disabled, slug) {
					// Allocate a fresh slice instead of reusing the
					// backing array via [:0]+append. The in-place form
					// is safe but reads as if it could clobber the
					// range — readability over one extra alloc on a
					// rare re-hire path.
					next := make([]string, 0, len(b.channels[i].Disabled))
					for _, d := range b.channels[i].Disabled {
						if d != slug {
							next = append(next, d)
						}
					}
					b.channels[i].Disabled = next
					mutated = true
				}
				if mutated {
					b.channels[i].UpdatedAt = now
					updatedChannels = append(updatedChannels, b.channels[i].Slug)
				}
			}
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			ensureNotebookDirsAfterUnlock = true
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_created", Slug: slug})
			// Notify SSE subscribers that these channels' rosters changed so
			// the UI sidebar refreshes without requiring a separate trigger.
			for _, chSlug := range updatedChannels {
				b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"member": member})
		case "update":
			member := b.findMemberLocked(slug)
			if member == nil {
				http.Error(w, "member not found", http.StatusNotFound)
				return
			}
			if body.Name != "" {
				member.Name = strings.TrimSpace(body.Name)
			}
			if body.Role != "" {
				member.Role = strings.TrimSpace(body.Role)
			}
			if body.Expertise != nil {
				member.Expertise = normalizeStringList(body.Expertise)
			}
			if body.Personality != "" {
				member.Personality = strings.TrimSpace(body.Personality)
			}
			if body.PermissionMode != "" {
				member.PermissionMode = strings.TrimSpace(body.PermissionMode)
			}
			if body.AllowedTools != nil {
				member.AllowedTools = normalizeStringList(body.AllowedTools)
			}
			if body.Provider != nil {
				if err := provider.ValidateKind(body.Provider.Kind); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				oldBinding := member.Provider
				newBinding := *body.Provider

				// Provider switch: reconcile the bridge state best-effort. We
				// don't block the update on gateway failures — the persisted
				// binding is the new truth, and a leaked old session is
				// recoverable via `openclaw sessions list` out-of-band.
				bridge := b.openclawBridgeLocked()

				fromOpenclaw := oldBinding.Kind == provider.KindOpenclaw
				toOpenclaw := newBinding.Kind == provider.KindOpenclaw

				if toOpenclaw {
					if bridge == nil {
						http.Error(w, "openclaw bridge not active; cannot switch agent to openclaw", http.StatusServiceUnavailable)
						return
					}
					if newBinding.Openclaw == nil {
						newBinding.Openclaw = &provider.OpenclawProviderBinding{}
					}
					if newBinding.Openclaw.SessionKey == "" {
						agentID := newBinding.Openclaw.AgentID
						if agentID == "" {
							agentID = "main"
						}
						label := fmt.Sprintf("wuphf-%s-%d", member.Slug, time.Now().UnixNano())
						key, err := bridge.CreateSession(r.Context(), agentID, label)
						if err != nil {
							http.Error(w, fmt.Sprintf("openclaw sessions.create: %v", err), http.StatusBadGateway)
							return
						}
						newBinding.Openclaw.SessionKey = key
					}
				}

				// Attach BEFORE detaching the old session so an attach failure
				// preserves the previous subscription rather than leaving the
				// agent silently disconnected. Order matters for openclaw→
				// openclaw swaps in particular: detach-first plus a failed
				// attach would return 502 with member.Provider still pointing
				// at the old binding but no live subscription on the gateway.
				if toOpenclaw {
					if err := bridge.AttachSlug(r.Context(), member.Slug, newBinding.Openclaw.SessionKey); err != nil {
						http.Error(w, fmt.Sprintf("openclaw subscribe: %v", err), http.StatusBadGateway)
						return
					}
				}

				if fromOpenclaw && bridge != nil {
					// Detach old session from subscriptions. Best-effort; log via
					// the bridge's own system-message channel on failure. The
					// daemon-side session lingers (no sessions.end method); user
					// can prune via the OpenClaw CLI if they care.
					if err := bridge.DetachSlug(r.Context(), member.Slug); err != nil {
						go bridge.postSystemMessage(fmt.Sprintf("agent %q provider-switch: detach warning: %v", member.Slug, err))
					}
				}

				member.Provider = newBinding
			}
			applyOfficeMemberDefaults(member)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			// Match the create/remove paths so SSE subscribers learn about
			// updated member metadata (provider switch, name changes,
			// channel reassignment) instead of waiting for a full reload.
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_updated", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"member": member})
		case "remove":
			member := b.findMemberLocked(slug)
			if member == nil {
				http.Error(w, "member not found", http.StatusNotFound)
				return
			}
			if member.BuiltIn || slug == "ceo" {
				http.Error(w, "cannot remove built-in member", http.StatusBadRequest)
				return
			}
			// If the member was bridged to OpenClaw, unsubscribe from the
			// gateway. Best-effort: member removal must succeed even when
			// the gateway is unreachable. We do NOT call sessions.end because
			// the current daemon doesn't expose that method — the session
			// lingers daemon-side and the user can clean it up via the
			// OpenClaw CLI if they want to reclaim the slot.
			if member.Provider.Kind == provider.KindOpenclaw {
				if bridge := b.openclawBridgeLocked(); bridge != nil {
					if err := bridge.DetachSlug(r.Context(), member.Slug); err != nil {
						go bridge.postSystemMessage(fmt.Sprintf("agent %q removed: detach warning: %v", member.Slug, err))
					}
				}
			}
			filteredMembers := b.members[:0]
			for _, existing := range b.members {
				if existing.Slug != slug {
					filteredMembers = append(filteredMembers, existing)
				}
			}
			b.members = filteredMembers
			b.rebuildMemberIndexLocked()
			// Symmetry with action:create — skip DM channels (they encode
			// their target in the slug and go through a different
			// membership gate) and emit a channel_updated event per
			// actually-mutated channel so SSE subscribers refresh the
			// roster. Without this, the UI sidebar gets a half-signal
			// lifecycle (create emits channel_updated, remove does not).
			removedChannels := make([]string, 0, len(b.channels))
			for i := range b.channels {
				if b.channels[i].isDM() {
					continue
				}
				mutated := false
				if containsString(b.channels[i].Members, slug) {
					next := make([]string, 0, len(b.channels[i].Members))
					for _, existing := range b.channels[i].Members {
						if existing != slug {
							next = append(next, existing)
						}
					}
					b.channels[i].Members = next
					mutated = true
				}
				if containsString(b.channels[i].Disabled, slug) {
					next := make([]string, 0, len(b.channels[i].Disabled))
					for _, existing := range b.channels[i].Disabled {
						if existing != slug {
							next = append(next, existing)
						}
					}
					b.channels[i].Disabled = next
					mutated = true
				}
				if mutated {
					b.channels[i].UpdatedAt = now
					removedChannels = append(removedChannels, b.channels[i].Slug)
				}
			}
			for i := range b.tasks {
				if b.tasks[i].Owner == slug {
					b.tasks[i].Owner = ""
					b.tasks[i].Status = "open"
					b.tasks[i].UpdatedAt = now
				}
			}
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_removed", Slug: slug})
			for _, chSlug := range removedChannels {
				b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGenerateMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.generateMemberFn == nil {
		http.Error(w, "generate not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	tmpl, err := b.generateMemberFn(prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tmpl)
}

func (b *Broker) handleGenerateChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.generateChannelFn == nil {
		http.Error(w, "generate not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	tmpl, err := b.generateChannelFn(prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tmpl)
}

func (b *Broker) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		typeFilter := r.URL.Query().Get("type") // "dm" to see DMs, default excludes them
		b.mu.Lock()
		channels := make([]teamChannel, 0, len(b.channels))
		for _, ch := range b.channels {
			if typeFilter == "dm" {
				if ch.isDM() {
					channels = append(channels, ch)
				}
			} else {
				// Default: only return real channels, never DMs
				if !ch.isDM() {
					channels = append(channels, ch)
				}
			}
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"channels": channels})
	case http.MethodPost:
		var body struct {
			Action      string          `json:"action"`
			Slug        string          `json:"slug"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Members     []string        `json:"members"`
			CreatedBy   string          `json:"created_by"`
			Surface     *channelSurface `json:"surface,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(body.Action)
		slug := normalizeChannelSlug(body.Slug)
		now := time.Now().UTC().Format(time.RFC3339)
		b.mu.Lock()
		defer b.mu.Unlock()
		validateMembers := func(members []string) ([]string, string) {
			members = uniqueSlugs(members)
			if len(members) == 0 {
				return nil, ""
			}
			validated := make([]string, 0, len(members))
			var missing []string
			for _, member := range members {
				if b.findMemberLocked(member) == nil {
					missing = append(missing, member)
					continue
				}
				validated = append(validated, member)
			}
			return validated, strings.Join(missing, ", ")
		}
		switch action {
		case "create":
			if slug == "" {
				http.Error(w, "slug required", http.StatusBadRequest)
				return
			}
			if reservedChannelSlugs[slug] {
				// Reject slugs that canAccessChannelLocked treats as universally
				// trusted senders. Without this gate, a user-created channel
				// named e.g. "system" would let every trusted-sender slug read
				// + post in it without an explicit Members entry — defeating
				// the membership check the rest of the auth path relies on.
				http.Error(w, "slug is reserved", http.StatusBadRequest)
				return
			}
			if b.findChannelLocked(slug) != nil {
				http.Error(w, "channel already exists", http.StatusConflict)
				return
			}
			members, missing := validateMembers(body.Members)
			if missing != "" {
				http.Error(w, "unknown members: "+missing, http.StatusNotFound)
				return
			}
			members = append([]string{"ceo"}, members...)
			if creator := normalizeChannelSlug(body.CreatedBy); creator != "" && creator != "ceo" && b.findMemberLocked(creator) != nil {
				members = append(members, creator)
			}
			ch := teamChannel{
				Slug:        slug,
				Name:        strings.TrimSpace(body.Name),
				Description: strings.TrimSpace(body.Description),
				Members:     uniqueSlugs(members),
				Surface:     body.Surface,
				CreatedBy:   strings.TrimSpace(body.CreatedBy),
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if ch.Name == "" {
				ch.Name = slug
			}
			if ch.Description == "" {
				ch.Description = defaultTeamChannelDescription(ch.Slug, ch.Name)
			}
			b.channels = append(b.channels, ch)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_created", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"channel": ch})
		case "update":
			if slug == "" {
				http.Error(w, "slug required", http.StatusBadRequest)
				return
			}
			ch := b.findChannelLocked(slug)
			if ch == nil {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
			if name := strings.TrimSpace(body.Name); name != "" {
				ch.Name = name
			}
			if description := strings.TrimSpace(body.Description); description != "" {
				ch.Description = description
			}
			if body.Surface != nil {
				ch.Surface = body.Surface
			}
			if body.Members != nil {
				members, missing := validateMembers(body.Members)
				if missing != "" {
					http.Error(w, "unknown members: "+missing, http.StatusNotFound)
					return
				}
				ch.Members = uniqueSlugs(append([]string{"ceo"}, members...))
				if len(ch.Disabled) > 0 {
					// Drop any disabled entry whose slug is in the updated
					// roster. The semantic pinned by
					// TestChannelUpdateMutatesDescriptionAndMembers is
					// "re-adding a slug to Members clears the per-channel
					// disabled flag" — so the filter keeps only entries
					// that are NOT in the new member list.
					filtered := make([]string, 0, len(ch.Disabled))
					for _, disabled := range ch.Disabled {
						if !containsString(ch.Members, disabled) {
							filtered = append(filtered, disabled)
						}
					}
					ch.Disabled = filtered
				}
			}
			ch.UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"channel": ch})
		case "remove":
			if slug == "" || slug == "general" {
				http.Error(w, "cannot remove channel", http.StatusBadRequest)
				return
			}
			idx := -1
			for i := range b.channels {
				if b.channels[i].Slug == slug {
					idx = i
					break
				}
			}
			if idx == -1 {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
			b.channels = append(b.channels[:idx], b.channels[idx+1:]...)
			filteredMessages := b.messages[:0]
			for _, msg := range b.messages {
				if normalizeChannelSlug(msg.Channel) != slug {
					filteredMessages = append(filteredMessages, msg)
				}
			}
			b.messages = filteredMessages
			filteredTasks := b.tasks[:0]
			for _, task := range b.tasks {
				if normalizeChannelSlug(task.Channel) != slug {
					filteredTasks = append(filteredTasks, task)
				}
			}
			b.tasks = filteredTasks
			filteredRequests := b.requests[:0]
			for _, req := range b.requests {
				if normalizeChannelSlug(req.Channel) != slug {
					filteredRequests = append(filteredRequests, req)
				}
			}
			b.requests = filteredRequests
			b.pendingInterview = firstBlockingRequest(b.requests)
			b.pruneAgentIssuesByChannelLocked(slug)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_removed", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateDM creates or returns an existing DM channel.
// POST /channels/dm — body: {members: ["human", "engineering"], type: "direct"|"group"}
func (b *Broker) handleCreateDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Members []string `json:"members"`
		Type    string   `json:"type"` // "direct" or "group"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(body.Members) < 2 {
		http.Error(w, "at least 2 members required", http.StatusBadRequest)
		return
	}
	// Validate: at least one member must be "human" (no agent-to-agent DMs).
	hasHuman := false
	for _, m := range body.Members {
		if m == "human" || m == "you" {
			hasHuman = true
			break
		}
	}
	if !hasHuman {
		http.Error(w, "DM must include a human member; agent-to-agent DMs are not allowed", http.StatusBadRequest)
		return
	}

	if b.channelStore == nil {
		http.Error(w, "channel store not initialized", http.StatusInternalServerError)
		return
	}

	var (
		ch      *channel.Channel
		err     error
		created bool
	)
	dmType := strings.TrimSpace(strings.ToLower(body.Type))
	// For group DMs, infer "created" from the group slug — the previous
	// FindDirectByMembers lookup checked for a 1:1 channel between the
	// first two members, which has no semantic relationship to whether
	// the group already existed.
	groupAlreadyExists := func(members []string) bool {
		slug := channel.GroupSlug(members)
		if slug == "" {
			return false
		}
		_, exists := b.channelStore.GetBySlug(slug)
		return exists
	}
	if dmType == "group" && len(body.Members) > 2 {
		created = !groupAlreadyExists(body.Members)
		ch, err = b.channelStore.GetOrCreateGroup(body.Members, "human")
	} else {
		// Default: direct (1:1). For >2 members use group.
		if len(body.Members) > 2 {
			created = !groupAlreadyExists(body.Members)
			ch, err = b.channelStore.GetOrCreateGroup(body.Members, "human")
		} else {
			// Normalize: find the non-human member for the slug.
			agentSlug := ""
			for _, m := range body.Members {
				if m != "human" && m != "you" {
					agentSlug = m
					break
				}
			}
			if agentSlug == "" {
				http.Error(w, "could not determine agent member", http.StatusBadRequest)
				return
			}
			_, exists := b.channelStore.FindDirectByMembers("human", agentSlug)
			created = !exists
			ch, err = b.channelStore.GetOrCreateDirect("human", agentSlug)
		}
	}
	if err != nil {
		http.Error(w, "failed to create DM: "+err.Error(), http.StatusInternalServerError)
		return
	}

	b.mu.Lock()
	if b.findChannelLocked(ch.Slug) == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		target := DMTargetAgent(ch.Slug)
		description := "Group direct messages"
		memberSlugs := append([]string(nil), body.Members...)
		if target != "" {
			description = "Direct messages with " + target
			memberSlugs = []string{"human", target}
		}
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			name = ch.Slug
		}
		b.channels = append(b.channels, teamChannel{
			Slug:        ch.Slug,
			Name:        name,
			Type:        "dm",
			Description: description,
			Members:     uniqueSlugs(memberSlugs),
			CreatedBy:   "human",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist DM channel", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      ch.ID,
		"slug":    ch.Slug,
		"type":    ch.Type,
		"name":    ch.Name,
		"created": created,
	})
}

func (b *Broker) handleChannelMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
		b.mu.Lock()
		ch := b.findChannelLocked(channel)
		if ch == nil {
			b.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": []map[string]any{}})
			return
		}
		memberInfo := make([]map[string]any, 0, len(ch.Members))
		for _, member := range ch.Members {
			memberInfo = append(memberInfo, map[string]any{
				"slug":     member,
				"disabled": !b.channelMemberEnabledLocked(channel, member),
			})
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": memberInfo})
	case http.MethodPost:
		var body struct {
			Channel string `json:"channel"`
			Action  string `json:"action"`
			Slug    string `json:"slug"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		channel := normalizeChannelSlug(body.Channel)
		member := normalizeChannelSlug(body.Slug)
		action := strings.TrimSpace(body.Action)
		if member == "" {
			http.Error(w, "slug required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		ch := b.findChannelLocked(channel)
		if ch == nil {
			b.mu.Unlock()
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		memberRecord := b.findMemberLocked(member)
		if memberRecord == nil {
			b.mu.Unlock()
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}
		// Lead agents (BuiltIn) cannot be disabled or removed from any
		// channel. The blueprint's lead is the tag target for the onboarding
		// kickoff and the default owner for channel membership; the UI locks
		// these interactions too. Keeps the "ceo" literal as a legacy guard
		// for team states that predate the BuiltIn field.
		if (memberRecord.BuiltIn || member == "ceo") && (action == "remove" || action == "disable") {
			b.mu.Unlock()
			http.Error(w, "cannot remove or disable lead agent", http.StatusBadRequest)
			return
		}
		switch action {
		case "add":
			ch.Members = uniqueSlugs(append(ch.Members, member))
		case "remove":
			filtered := ch.Members[:0]
			for _, existing := range ch.Members {
				if existing != member {
					filtered = append(filtered, existing)
				}
			}
			ch.Members = filtered
			disabled := ch.Disabled[:0]
			for _, existing := range ch.Disabled {
				if existing != member {
					disabled = append(disabled, existing)
				}
			}
			ch.Disabled = disabled
		case "disable":
			if !b.channelHasMemberLocked(channel, member) {
				ch.Members = uniqueSlugs(append(ch.Members, member))
			}
			ch.Disabled = uniqueSlugs(append(ch.Disabled, member))
		case "enable":
			filtered := ch.Disabled[:0]
			for _, existing := range ch.Disabled {
				if existing != member {
					filtered = append(filtered, existing)
				}
			}
			ch.Disabled = filtered
		default:
			b.mu.Unlock()
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		ch.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		// Match the channel-create/update/remove paths: notify SSE
		// subscribers that the roster changed. Without this, sidebars
		// and member dialogs keep stale member lists until a full
		// reload.
		b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: ch.Slug})
		state := map[string]any{
			"channel":  ch.Slug,
			"members":  ch.Members,
			"disabled": ch.Disabled,
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleMembers(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "general"
	}
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	if !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	type memberView struct {
		name        string
		role        string
		lastMessage string
		lastTime    string
		disabled    bool
	}
	members := make(map[string]memberView)
	if ch := b.findChannelLocked(channel); ch != nil {
		for _, member := range ch.Members {
			if b.sessionMode == SessionModeOneOnOne && member != b.oneOnOneAgent {
				continue
			}
			info := memberView{disabled: containsString(ch.Disabled, member)}
			if office := b.findMemberLocked(member); office != nil {
				info.name = office.Name
				info.role = office.Role
			}
			members[member] = info
		}
	}
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			continue
		}
		if b.sessionMode == SessionModeOneOnOne && msg.From != b.oneOnOneAgent {
			continue
		}
		if msg.Kind == "automation" || msg.From == "nex" {
			continue
		}
		content := msg.Content
		if len(content) > 80 {
			content = content[:80]
		}
		info := members[msg.From]
		info.lastMessage = content
		info.lastTime = msg.Timestamp
		if info.name == "" {
			if office := b.findMemberLocked(msg.From); office != nil {
				info.name = office.Name
				info.role = office.Role
			}
		}
		members[msg.From] = info
	}
	isOneOnOne := b.sessionMode == SessionModeOneOnOne
	oneOnOneSlug := b.oneOnOneAgent
	taggedAt := make(map[string]time.Time, len(b.lastTaggedAt))
	for slug, ts := range b.lastTaggedAt {
		taggedAt[slug] = ts
	}
	activity := make(map[string]agentActivitySnapshot, len(b.activity))
	for slug, snapshot := range b.activity {
		activity[slug] = snapshot
	}
	b.mu.Unlock()

	type memberEntry struct {
		Slug         string `json:"slug"`
		Name         string `json:"name,omitempty"`
		Role         string `json:"role,omitempty"`
		Disabled     bool   `json:"disabled,omitempty"`
		LastMessage  string `json:"lastMessage"`
		LastTime     string `json:"lastTime"`
		LiveActivity string `json:"liveActivity,omitempty"`
		Status       string `json:"status,omitempty"`
		Activity     string `json:"activity,omitempty"`
		Detail       string `json:"detail,omitempty"`
		TotalMs      int64  `json:"totalMs,omitempty"`
		FirstEventMs int64  `json:"firstEventMs,omitempty"`
		FirstTextMs  int64  `json:"firstTextMs,omitempty"`
		FirstToolMs  int64  `json:"firstToolMs,omitempty"`
	}

	// Capture pane activity via diff detection.
	// If content changed since last poll, agent is active — return last 5 lines.
	var paneActivity map[string]string
	if isOneOnOne && oneOnOneSlug != "" {
		paneActivity = b.capturePaneActivity(oneOnOneSlug)
	} else {
		paneActivity = b.capturePaneActivity("")
	}

	var list []memberEntry
	for slug, info := range members {
		entry := memberEntry{
			Slug:        slug,
			Name:        info.name,
			Role:        info.role,
			Disabled:    info.disabled,
			LastMessage: info.lastMessage,
			LastTime:    info.lastTime,
		}
		if snapshot, ok := activity[slug]; ok {
			entry.Status = snapshot.Status
			entry.Activity = snapshot.Activity
			entry.Detail = snapshot.Detail
			entry.TotalMs = snapshot.TotalMs
			entry.FirstEventMs = snapshot.FirstEventMs
			entry.FirstTextMs = snapshot.FirstTextMs
			entry.FirstToolMs = snapshot.FirstToolMs
			if snapshot.LastTime != "" {
				entry.LastTime = snapshot.LastTime
			}
			if snapshot.Detail != "" {
				entry.LiveActivity = snapshot.Detail
			}
		}
		if live, ok := paneActivity[slug]; ok {
			entry.Status = "active"
			if entry.Activity == "" {
				entry.Activity = "text"
			}
			entry.LiveActivity = live
			entry.Detail = live
			if entry.LastTime == "" {
				entry.LastTime = time.Now().UTC().Format(time.RFC3339)
			}
		}
		// Also mark as active if tagged recently and hasn't replied yet
		if entry.LiveActivity == "" && taggedAt != nil {
			if t, ok := taggedAt[slug]; ok && time.Since(t) < 60*time.Second {
				entry.Status = "active"
				if entry.Activity == "" {
					entry.Activity = "queued"
				}
				entry.LiveActivity = "active"
			}
		}
		if entry.Status == "" {
			entry.Status = "idle"
		}
		if entry.Activity == "" {
			entry.Activity = "idle"
		}
		list = append(list, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": list})
}
