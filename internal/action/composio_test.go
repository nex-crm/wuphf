package action

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComposioRESTActionHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	var executeUserID string
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id":      "ca_123",
				"status":  "ACTIVE",
				"user_id": "cmp_user_123",
				"toolkit": map[string]any{
					"slug": "gmail",
					"name": "Gmail",
				},
				"connection": map[string]any{
					"name": "Founder Gmail",
				},
			}},
		})
	})
	mux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "ca_123",
			"user_id": "cmp_user_123",
			"status":  "ACTIVE",
		})
	})
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"slug":        "GMAIL_SEND_EMAIL",
				"name":        "Send email",
				"description": "Send an email from Gmail",
				"toolkit": map[string]any{
					"slug": "gmail",
				},
			}},
		})
	})
	mux.HandleFunc("/tools/GMAIL_SEND_EMAIL", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"slug":        "GMAIL_SEND_EMAIL",
			"name":        "Send email",
			"description": "Send an email from Gmail",
			"toolkit": map[string]any{
				"slug": "gmail",
			},
			"input_parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to": map[string]any{"type": "string"},
				},
			},
		})
	})
	mux.HandleFunc("/tools/execute/GMAIL_SEND_EMAIL", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if userID, _ := body["user_id"].(string); userID != "" {
			executeUserID = userID
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"successful": true,
			"data": map[string]any{
				"id": "msg-123",
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "ceo@example.com",
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	connections, err := client.ListConnections(context.Background(), ListConnectionsOptions{})
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(connections.Connections) != 1 || connections.Connections[0].Key != "ca_123" {
		t.Fatalf("unexpected connections %+v", connections)
	}

	search, err := client.SearchActions(context.Background(), "gmail", "send email", "execute")
	if err != nil {
		t.Fatalf("search actions: %v", err)
	}
	if len(search.Actions) != 1 || search.Actions[0].ActionID != "GMAIL_SEND_EMAIL" {
		t.Fatalf("unexpected search %+v", search)
	}

	knowledge, err := client.ActionKnowledge(context.Background(), "gmail", "GMAIL_SEND_EMAIL")
	if err != nil {
		t.Fatalf("knowledge: %v", err)
	}
	if knowledge.Platform != "gmail" || knowledge.ActionID != "GMAIL_SEND_EMAIL" {
		t.Fatalf("unexpected knowledge %+v", knowledge)
	}

	dryRun, err := client.ExecuteAction(context.Background(), ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "GMAIL_SEND_EMAIL",
		ConnectionKey: "ca_123",
		Data:          map[string]any{"to": "ceo@example.com"},
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dryRun.DryRun || dryRun.Request.URL == "" {
		t.Fatalf("unexpected dry run %+v", dryRun)
	}

	result, err := client.ExecuteAction(context.Background(), ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "GMAIL_SEND_EMAIL",
		ConnectionKey: "ca_123",
		Data:          map[string]any{"to": "ceo@example.com"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.DryRun || len(result.Response) == 0 {
		t.Fatalf("unexpected execute %+v", result)
	}
	if executeUserID != "cmp_user_123" {
		t.Fatalf("expected resolved composio user id cmp_user_123, got %q", executeUserID)
	}
}

func TestComposioRESTIntegrationLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	var createdAuthConfig bool
	var connectBody map[string]any
	var deletedAccount string
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id":     "ca_123",
				"status": "ACTIVE",
				"toolkit": map[string]any{
					"slug": "gmail",
					"name": "Gmail",
				},
				"connection": map[string]any{"name": "Founder Gmail"},
			}},
		})
	})
	mux.HandleFunc("/toolkits", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("search"); got != "gmail" {
			t.Fatalf("expected toolkit search=gmail, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"slug": "gmail",
				"name": "Gmail",
				"meta": map[string]any{
					"description": "Read and send Gmail messages",
					"logo":        "https://example.com/gmail.png",
					"categories": []map[string]string{{
						"id":   "communication",
						"name": "Communication",
					}},
				},
			}},
		})
	})
	mux.HandleFunc("/auth_configs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
		case http.MethodPost:
			createdAuthConfig = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "auth_123"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/connected_accounts/link", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&connectBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                   "link_123",
			"connected_account_id": "ca_123",
			"redirect_url":         "https://connect.composio.dev/abc",
			"status":               "INITIATED",
			"expires_at":           "2026-06-04T12:00:00Z",
		})
	})
	mux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletedAccount = "ca_123"
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "ca_123",
			"status": "ACTIVE",
			"toolkit": map[string]any{
				"slug": "gmail",
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "ceo@example.com",
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	catalog, err := client.ListIntegrationCatalog(context.Background(), IntegrationCatalogOptions{Search: "gmail", Limit: 10})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if len(catalog.Items) != 1 || catalog.Items[0].State != "connected" || catalog.Items[0].ConnectionKey != "ca_123" {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	if got := catalog.Items[0].LogoURL; got != "https://example.com/gmail.png" {
		t.Fatalf("expected toolkit logo from meta.logo, got %q", got)
	}
	if got := catalog.Items[0].Category; got != "Communication" {
		t.Fatalf("expected toolkit category from meta.categories, got %q", got)
	}

	started, err := client.StartIntegrationConnection(context.Background(), IntegrationConnectRequest{Platform: "gmail"})
	if err != nil {
		t.Fatalf("start connection: %v", err)
	}
	if !createdAuthConfig {
		t.Fatalf("expected auth config creation")
	}
	if got := connectBody["auth_config_id"]; got != "auth_123" {
		t.Fatalf("expected auth_config_id auth_123, got %#v", got)
	}
	if got := connectBody["user_id"]; got != "ceo@example.com" {
		t.Fatalf("expected user_id ceo@example.com, got %#v", got)
	}
	if started.AuthURL == "" || started.ConnectID != "ca_123" || started.Status != "initiated" {
		t.Fatalf("unexpected start result: %+v", started)
	}

	status, err := client.GetIntegrationConnectionStatus(context.Background(), IntegrationStatusRequest{ConnectID: "ca_123"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Status != "connected" || status.ConnectionKey != "ca_123" || status.Platform != "gmail" {
		t.Fatalf("unexpected status: %+v", status)
	}

	disconnected, err := client.DisconnectIntegration(context.Background(), IntegrationDisconnectRequest{Platform: "gmail", ConnectionKey: "ca_123"})
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if !disconnected.OK || disconnected.Platform != "gmail" || deletedAccount != "ca_123" {
		t.Fatalf("unexpected disconnect: %+v deleted=%q", disconnected, deletedAccount)
	}
}

func TestComposioRESTWorkflowDigestHappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "nex-test-key")

	mux := http.NewServeMux()
	var sentBody string
	mux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "ca_123",
			"user_id": "cmp_user_123",
			"status":  "ACTIVE",
		})
	})
	mux.HandleFunc("/tools/execute/GMAIL_FETCH_EMAILS", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"messages": []map[string]any{
					{
						"messageId":        "msg-1",
						"threadId":         "thread-1",
						"messageTimestamp": "2026-03-31T07:30:00Z",
						"subject":          "Customer escalation on Acme rollout",
						"sender":           "support@acme.com",
						"to":               "najmuzzaman@nex.ai",
						"messageText":      "Customer reported rollout issue.",
						"preview": map[string]any{
							"body": "Customer reported rollout issue.",
						},
						"labelIds": []string{"INBOX"},
					},
				},
				"resultSizeEstimate": 1,
			},
		})
	})
	mux.HandleFunc("/tools/execute/GMAIL_SEND_EMAIL", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		args, _ := body["arguments"].(map[string]any)
		sentBody, _ = args["body"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id": "msg-sent-1",
			},
		})
	})
	mux.HandleFunc("/api/developers/v1/context/ask", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answer": "Executive Summary\n- Acme escalation needs immediate follow-up.\n\nWhy This Matters\n- It affects rollout trust.\n\nWhat To Do Next\n- Have PM coordinate a response today.\n\nEmail Highlights\n- support@acme.com | Customer escalation on Acme rollout\n\nRelevant Nex Insights\n- Recent insight confirms rollout risk.",
		})
	})
	mux.HandleFunc("/api/developers/v1/insights", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"insights": []map[string]any{{
				"id":      "ins-1",
				"type":    "risk",
				"content": "Acme rollout risk increased after support issues.",
			}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("WUPHF_DEV_URL", server.URL)

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "najmuzzaman@nex.ai",
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"inputs": map[string]any{
			"connection_key":  "ca_123",
			"recipient_email": "najmuzzaman@nex.ai",
			"subject":         "Daily Digest",
			"window_hours":    24,
			"insight_limit":   5,
			"max_results":     10,
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
					"max_results": "{{ .inputs.max_results }}",
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
				"template": "Email highlights:\n{{- range $m := .steps.fetch_emails.result.data.messages }}\n- {{ $m.sender }} | {{ $m.subject }} | {{ $m.preview.body }}\n{{- end }}",
			},
			{
				"id":             "compose_digest",
				"type":           "nex_ask",
				"query_template": "Create a plain-text daily digest with sections Executive Summary, Why This Matters, What To Do Next, Email Highlights, and Relevant Nex Insights.\n\n{{ .steps.email_summary.result }}\n\nInsights:\n{{ .steps.recent_insights.result }}",
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
	})

	created, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{
		Key:        "daily-digest",
		Definition: definition,
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if !created.Created || created.Key != "daily-digest" {
		t.Fatalf("unexpected create result %+v", created)
	}

	result, err := client.ExecuteWorkflow(context.Background(), WorkflowExecuteRequest{
		KeyOrPath: "daily-digest",
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("unexpected execute result %+v", result)
	}
	if !strings.Contains(sentBody, "Why This Matters") {
		t.Fatalf("expected hydrated digest body, got %q", sentBody)
	}

	runs, err := client.ListWorkflowRuns(context.Background(), "daily-digest")
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs.Runs) != 1 {
		t.Fatalf("expected 1 run, got %+v", runs.Runs)
	}
}

func TestComposioRESTWorkflowNormalizesProviderStepAliases(t *testing.T) {
	client := &ComposioREST{}
	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"steps": []map[string]any{
			{
				"id":             "send_email",
				"type":           "composio",
				"platform":       "gmail",
				"action_id":      "GMAIL_SEND_EMAIL",
				"connection_key": "ca_123",
				"data": map[string]any{
					"recipient_email": "najmuzzaman@nex.ai",
					"subject":         "Hi",
					"body":            "Body",
				},
			},
		},
	})

	spec, err := client.decodeWorkflowDefinition(definition)
	if err != nil {
		t.Fatalf("decode workflow definition: %v", err)
	}
	if len(spec.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(spec.Steps))
	}
	if spec.Steps[0].Type != "action" {
		t.Fatalf("expected normalized step type action, got %q", spec.Steps[0].Type)
	}
}

func TestComposioRESTWorkflowNormalizesAgentShorthandSyntax(t *testing.T) {
	client := &ComposioREST{}
	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"inputs": map[string]any{
			"recipient": map[string]any{
				"type":        "string",
				"default":     "najmuzzaman@nex.ai",
				"description": "Email recipient",
			},
			"gmail_connection_key": map[string]any{
				"type":    "string",
				"default": "ca_123",
			},
		},
		"steps": []map[string]any{
			{
				"id":             "fetch_emails",
				"type":           "action",
				"platform":       "gmail",
				"action_id":      "GMAIL_FETCH_EMAILS",
				"connection_key": "{{inputs.gmail_connection_key}}",
				"params": map[string]any{
					"query": "newer_than:1d",
				},
			},
			{
				"id":       "compose",
				"type":     "template",
				"template": "Recent emails: {{steps.fetch_emails.result}}",
			},
			{
				"id":             "send_digest",
				"type":           "action",
				"platform":       "gmail",
				"action_id":      "GMAIL_SEND_EMAIL",
				"connection_key": "{{inputs.gmail_connection_key}}",
				"params": map[string]any{
					"recipient_email": "{{inputs.recipient}}",
					"subject":         "Daily Digest — {{today_date}}",
					"body":            "{{steps.compose.result}}",
				},
			},
		},
	})

	spec, err := client.decodeWorkflowDefinition(definition)
	if err != nil {
		t.Fatalf("decode workflow definition: %v", err)
	}
	if got := spec.Inputs["recipient"]; got != "najmuzzaman@nex.ai" {
		t.Fatalf("expected input default to normalize, got %#v", got)
	}
	if got := spec.Steps[0].ConnectionKey; got != "{{ .inputs.gmail_connection_key}}" {
		t.Fatalf("expected normalized connection_key, got %#v", got)
	}
	if got := spec.Steps[0].Data["query"]; got != "newer_than:1d" {
		t.Fatalf("expected params to normalize into data, got %#v", got)
	}
	if got := spec.Steps[1].Template; got != "Recent emails: {{ .steps.fetch_emails.result}}" {
		t.Fatalf("expected normalized template, got %q", got)
	}
	if got := spec.Steps[2].Data["subject"]; got != "Daily Digest — {{ .now.date }}" && got != "Daily Digest — {{ .now.date}}" {
		t.Fatalf("expected normalized today_date template, got %#v", got)
	}
}

func TestComposioRESTWorkflowNormalizesHandlebarsEachSyntax(t *testing.T) {
	client := &ComposioREST{}
	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"steps": []map[string]any{
			{
				"id":       "email_summary",
				"type":     "template",
				"template": "Recent emails:\n{{#each steps.fetch_emails.result.data.messages}}\n- {{this.sender}} | {{this.subject}}\n{{/each}}",
			},
		},
	})

	spec, err := client.decodeWorkflowDefinition(definition)
	if err != nil {
		t.Fatalf("decode workflow definition: %v", err)
	}
	want := "Recent emails:\n{{- range $item := .steps.fetch_emails.result.data.messages }}\n- {{ $item.sender }} | {{ $item.subject }}\n{{- end }}"
	if got := spec.Steps[0].Template; got != want {
		t.Fatalf("expected normalized handlebars loop, got %q", got)
	}
}

func TestComposioRESTWorkflowAutoResolvesSingleConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "nex-test-key")

	mux := http.NewServeMux()
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id":      "ca_123",
				"status":  "ACTIVE",
				"user_id": "cmp_user_123",
				"toolkit": map[string]any{
					"slug": "gmail",
					"name": "Gmail",
				},
				"connection": map[string]any{
					"name": "Founder Gmail",
				},
			}},
		})
	})
	mux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "ca_123",
			"user_id": "cmp_user_123",
			"status":  "ACTIVE",
		})
	})
	mux.HandleFunc("/tools/execute/GMAIL_SEND_EMAIL", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"successful": true,
			"echo":       body,
			"data": map[string]any{
				"id": "msg-123",
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "najmuzzaman@nex.ai",
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"steps": []map[string]any{
			{
				"id":        "send_digest",
				"type":      "action",
				"platform":  "gmail",
				"action_id": "GMAIL_SEND_EMAIL",
				"data": map[string]any{
					"recipient_email": "najmuzzaman@nex.ai",
					"subject":         "Daily Digest — {{ .meta.date }}",
					"body":            "Hello",
				},
			},
		},
	})

	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{
		Key:        "auto-resolve-connection",
		Definition: definition,
	}); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	result, err := client.ExecuteWorkflow(context.Background(), WorkflowExecuteRequest{
		KeyOrPath: "auto-resolve-connection",
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	var step map[string]any
	if err := json.Unmarshal(result.Steps["send_digest"], &step); err != nil {
		t.Fatalf("decode action step: %v", err)
	}
	if got := step["connection_key"]; got != "ca_123" {
		t.Fatalf("expected auto-resolved connection_key ca_123, got %#v", got)
	}
	rendered, err := renderWorkflowTemplate("Daily Digest — {{ .meta.date }}", workflowScope("auto-resolve-connection", map[string]any{}, map[string]any{}))
	if err != nil {
		t.Fatalf("render meta date template: %v", err)
	}
	if !strings.Contains(rendered, "Daily Digest — ") {
		t.Fatalf("expected rendered meta date subject, got %q", rendered)
	}
}

func TestDecodeJSONObjectHandlesJSONStringPayload(t *testing.T) {
	raw := json.RawMessage(`"{\"data\":{\"messages\":[{\"subject\":\"hello\",\"sender\":\"a@example.com\"}]}}"`)
	decoded := decodeJSONObject(raw)
	asMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", decoded)
	}
	data, ok := asMap["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %#v", asMap["data"])
	}
	msgs, ok := data["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected decoded messages, got %#v", data["messages"])
	}
}

func TestWorkflowStepsExposeGenericResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "nex-test-key")

	mux := http.NewServeMux()
	mux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "ca_123",
			"user_id": "cmp_user_123",
			"status":  "ACTIVE",
		})
	})
	mux.HandleFunc("/tools/execute/GMAIL_FETCH_EMAILS", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"messages": []map[string]any{{
					"subject": "hello",
					"sender":  "a@example.com",
				}},
			},
		})
	})
	mux.HandleFunc("/api/developers/v1/context/ask", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answer": "Digest body",
		})
	})
	mux.HandleFunc("/api/developers/v1/insights", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"insights": []map[string]any{{
				"id":      "ins-1",
				"type":    "risk",
				"content": "Something changed.",
			}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("WUPHF_DEV_URL", server.URL)

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "najmuzzaman@nex.ai",
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	definition, _ := json.Marshal(map[string]any{
		"version": composioWorkflowVersion,
		"inputs": map[string]any{
			"connection_key": "ca_123",
		},
		"steps": []map[string]any{
			{
				"id":             "fetch_emails",
				"type":           "action",
				"platform":       "gmail",
				"action_id":      "GMAIL_FETCH_EMAILS",
				"connection_key": "{{ .inputs.connection_key }}",
			},
			{
				"id":       "email_summary",
				"type":     "template",
				"template": "{{ range .steps.fetch_emails.result.data.messages }}{{ .subject }}{{ end }}",
			},
			{
				"id":             "recent_insights",
				"type":           "nex_insights",
				"lookback_hours": 24,
				"insight_limit":  5,
			},
			{
				"id":             "compose_digest",
				"type":           "nex_ask",
				"query_template": "{{ .steps.email_summary.result }} :: {{ toPrettyJSON .steps.recent_insights.result }}",
			},
		},
	})

	if _, err := client.CreateWorkflow(context.Background(), WorkflowCreateRequest{
		Key:        "result-aliases",
		Definition: definition,
	}); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	result, err := client.ExecuteWorkflow(context.Background(), WorkflowExecuteRequest{
		KeyOrPath: "result-aliases",
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}

	var compose map[string]any
	if err := json.Unmarshal(result.Steps["compose_digest"], &compose); err != nil {
		t.Fatalf("decode compose step: %v", err)
	}
	if compose["result"] != "Digest body" {
		t.Fatalf("expected compose result alias, got %#v", compose["result"])
	}

	var summary map[string]any
	if err := json.Unmarshal(result.Steps["email_summary"], &summary); err != nil {
		t.Fatalf("decode summary step: %v", err)
	}
	if summary["result"] != "hello" {
		t.Fatalf("expected template result alias, got %#v", summary["result"])
	}

	var recentInsights map[string]any
	if err := json.Unmarshal(result.Steps["recent_insights"], &recentInsights); err != nil {
		t.Fatalf("decode recent insights step: %v", err)
	}
	insightSummary, _ := recentInsights["result"].(string)
	if !strings.Contains(insightSummary, "Something changed.") {
		t.Fatalf("expected compact insight summary, got %#v", recentInsights["result"])
	}
}

func TestComposioApplyAuthHeaders(t *testing.T) {
	t.Run("project key wins and suppresses user-key headers", func(t *testing.T) {
		c := &ComposioREST{APIKey: "ak_proj", UserAPIKey: "uak_x", OrgID: "ok_1", ProjectID: "pr_1"}
		h := http.Header{}
		c.applyAuthHeaders(h)
		if got := h.Get("x-api-key"); got != "ak_proj" {
			t.Fatalf("x-api-key = %q, want ak_proj", got)
		}
		if h.Get("x-user-api-key") != "" || h.Get("x-org-id") != "" || h.Get("x-project-id") != "" {
			t.Fatalf("user-key headers must not be set in ak_ mode: %v", h)
		}
	})

	t.Run("user-key mode sets the trio", func(t *testing.T) {
		c := &ComposioREST{UserAPIKey: "uak_x", OrgID: "ok_1", ProjectID: "pr_1"}
		h := http.Header{}
		c.applyAuthHeaders(h)
		if h.Get("x-api-key") != "" {
			t.Fatalf("x-api-key must be empty in user-key mode, got %q", h.Get("x-api-key"))
		}
		if h.Get("x-user-api-key") != "uak_x" || h.Get("x-org-id") != "ok_1" || h.Get("x-project-id") != "pr_1" {
			t.Fatalf("unexpected user-key headers: %v", h)
		}
	})

	t.Run("user-key without project omits x-project-id", func(t *testing.T) {
		c := &ComposioREST{UserAPIKey: "uak_x", OrgID: "ok_1"}
		h := http.Header{}
		c.applyAuthHeaders(h)
		if _, ok := h["X-Project-Id"]; ok {
			t.Fatalf("x-project-id must be absent when project id is empty: %v", h)
		}
		if h.Get("x-user-api-key") != "uak_x" || h.Get("x-org-id") != "ok_1" {
			t.Fatalf("unexpected headers: %v", h)
		}
	})
}

func TestComposioHasAuth(t *testing.T) {
	cases := []struct {
		name string
		c    ComposioREST
		want bool
	}{
		{"project key", ComposioREST{APIKey: "ak_x"}, true},
		{"user key + org", ComposioREST{UserAPIKey: "uak_x", OrgID: "ok_1"}, true},
		{"user key without org", ComposioREST{UserAPIKey: "uak_x"}, false},
		{"org without user key", ComposioREST{OrgID: "ok_1"}, false},
		{"nothing", ComposioREST{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.hasAuth(); got != tc.want {
				t.Fatalf("hasAuth() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestComposioRESTCatalogHidesComposioToolkits(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
	})
	mux.HandleFunc("/toolkits", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"slug": "composio", "name": "Composio"},
				{"slug": "composio_search", "name": "Composio Search"},
				{"slug": "gmail", "name": "Gmail"},
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "ceo@example.com",
		BaseURL: server.URL,
		Client:  server.Client(),
	}
	catalog, err := client.ListIntegrationCatalog(context.Background(), IntegrationCatalogOptions{Limit: 50})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	// White-labeled: Composio's own toolkits must never reach the catalog.
	for _, item := range catalog.Items {
		if strings.HasPrefix(strings.ToLower(item.Name), "composio") ||
			strings.HasPrefix(strings.ToLower(item.Platform), "composio") {
			t.Fatalf("composio toolkit leaked into the catalog: %+v", item)
		}
	}
	if len(catalog.Items) != 1 || catalog.Items[0].Name != "Gmail" {
		t.Fatalf("expected only Gmail to remain, got %+v", catalog.Items)
	}
}
