package team

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newIntegrationsTestServer(t *testing.T, b *Broker) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/integrations", b.requireAuth(b.handleIntegrations))
	mux.HandleFunc("/integrations/connect", b.requireAuth(b.handleIntegrationConnect))
	mux.HandleFunc("/integrations/connect-status", b.requireAuth(b.handleIntegrationConnectStatus))
	mux.HandleFunc("/integrations/disconnect", b.requireAuth(b.handleIntegrationDisconnect))
	mux.HandleFunc("/integrations/audit", b.requireAuth(b.handleIntegrationAudit))
	return httptest.NewServer(mux)
}

func integrationRequest(t *testing.T, srv *httptest.Server, b *Broker, method, path string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestIntegrationsEndpointReportsUnconfiguredProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	resp := integrationRequest(t, srv, b, http.MethodGet, "/integrations", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /integrations status=%d body=%s", resp.StatusCode, string(raw))
	}
	var body struct {
		Providers []struct {
			Provider   string `json:"provider"`
			Configured bool   `json:"configured"`
		} `json:"providers"`
		Items []struct {
			Provider   string `json:"provider"`
			Platform   string `json:"platform"`
			State      string `json:"state"`
			CanConnect bool   `json:"can_connect"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) == 0 {
		t.Fatalf("expected curated catalog items without config")
	}
	foundGmail := false
	for _, item := range body.Items {
		if item.Provider != "composio" {
			continue
		}
		if item.Platform == "gmail" {
			foundGmail = true
			if item.State != "unconfigured" || item.CanConnect {
				t.Fatalf("unexpected unconfigured gmail item: %+v", item)
			}
		}
	}
	if !foundGmail {
		t.Fatalf("expected gmail in curated catalog: %+v", body.Items)
	}
	if len(body.Providers) != 1 {
		t.Fatalf("expected only composio provider status: %+v", body.Providers)
	}
	foundComposio := false
	for _, provider := range body.Providers {
		if provider.Provider == "one" {
			t.Fatalf("did not expect one provider status: %+v", body.Providers)
		}
		if provider.Provider == "composio" {
			foundComposio = true
			if provider.Configured {
				t.Fatalf("expected composio unconfigured")
			}
		}
	}
	if !foundComposio {
		t.Fatalf("expected composio provider status: %+v", body.Providers)
	}
}

func TestIntegrationConnectStatusDisconnectAndAudit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("WUPHF_RUNTIME_HOME", tmp)
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "cmp_test")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "ceo@example.com")

	var deletedAccount string
	composioMux := http.NewServeMux()
	composioMux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, r *http.Request) {
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
	composioMux.HandleFunc("/toolkits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"slug":        "gmail",
				"name":        "Gmail",
				"description": "Read and send Gmail messages",
			}},
		})
	})
	composioMux.HandleFunc("/auth_configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "auth_123", "toolkit_slug": "gmail", "is_composio_managed": true}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "auth_123"})
	})
	composioMux.HandleFunc("/connected_accounts/link", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "ca_123",
			"redirect_url": "https://connect.composio.dev/abc",
			"status":       "pending",
		})
	})
	composioMux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
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
	composioServer := httptest.NewServer(composioMux)
	defer composioServer.Close()
	t.Setenv("WUPHF_COMPOSIO_BASE_URL", composioServer.URL)

	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	resp := integrationRequest(t, srv, b, http.MethodGet, "/integrations?provider=composio&search=gmail", nil)
	var catalog struct {
		Items []struct {
			Provider      string `json:"provider"`
			Platform      string `json:"platform"`
			ConnectionKey string `json:"connection_key"`
			State         string `json:"state"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(catalog.Items) != 1 || catalog.Items[0].ConnectionKey != "ca_123" {
		t.Fatalf("unexpected catalog status=%d body=%+v", resp.StatusCode, catalog)
	}

	resp = integrationRequest(t, srv, b, http.MethodPost, "/integrations/connect", []byte(`{"provider":"composio","platform":"gmail"}`))
	var connect struct {
		AuthURL   string `json:"auth_url"`
		ConnectID string `json:"connect_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&connect); err != nil {
		t.Fatalf("decode connect: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || connect.AuthURL == "" || connect.ConnectID != "ca_123" {
		t.Fatalf("unexpected connect status=%d body=%+v", resp.StatusCode, connect)
	}

	resp = integrationRequest(t, srv, b, http.MethodGet, "/integrations/connect-status?provider=composio&connect_id=ca_123", nil)
	var status struct {
		Status        string `json:"status"`
		ConnectionKey string `json:"connection_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	resp.Body.Close()
	if status.Status != "connected" || status.ConnectionKey != "ca_123" {
		t.Fatalf("unexpected status: %+v", status)
	}

	if err := b.RecordActionWithMetadata("external_action_executed", "composio", "general", "agent", "Sent email", "GMAIL_SEND_EMAIL", nil, "", map[string]string{
		"provider":       "composio",
		"platform":       "gmail",
		"action_id":      "GMAIL_SEND_EMAIL",
		"connection_key": "ca_123",
		"status":         "executed",
	}); err != nil {
		t.Fatalf("record action: %v", err)
	}
	if err := b.RecordApprovalAudit(ApprovalAuditEntry{
		ApprovalRequestID: "req-1",
		Platform:          "gmail",
		ActionID:          "GMAIL_SEND_EMAIL",
		ConnectionKey:     "ca_123",
		Outcome:           ApprovalOutcomeExecutedOK,
		CreatedAt:         "2026-06-04T12:00:00Z",
	}); err != nil {
		t.Fatalf("record approval audit: %v", err)
	}

	resp = integrationRequest(t, srv, b, http.MethodGet, "/integrations/audit?provider=composio&platform=gmail&connection_key=ca_123", nil)
	var audit struct {
		Events []struct {
			EventType     string            `json:"event_type"`
			Provider      string            `json:"provider"`
			Platform      string            `json:"platform"`
			ConnectionKey string            `json:"connection_key"`
			Summary       string            `json:"summary"`
			Metadata      map[string]string `json:"metadata"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&audit); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	resp.Body.Close()
	if len(audit.Events) == 0 {
		t.Fatalf("expected audit events")
	}
	foundAction := false
	foundApproval := false
	for _, event := range audit.Events {
		if event.EventType == "external_action_executed" {
			foundAction = true
			if event.Provider != "composio" || event.Platform != "gmail" || event.ConnectionKey != "ca_123" || event.Metadata["action_id"] != "GMAIL_SEND_EMAIL" {
				t.Fatalf("unexpected action audit event: %+v", event)
			}
		}
		if event.EventType == "approval_executed_ok" {
			foundApproval = true
			if event.Provider != "approval" || event.Platform != "gmail" || event.ConnectionKey != "ca_123" {
				t.Fatalf("unexpected approval audit event: %+v", event)
			}
		}
	}
	if !foundAction {
		t.Fatalf("expected external action audit event: %+v", audit.Events)
	}
	if !foundApproval {
		t.Fatalf("expected approval audit event: %+v", audit.Events)
	}

	resp = integrationRequest(t, srv, b, http.MethodPost, "/integrations/disconnect", []byte(`{"provider":"composio","platform":"gmail","connection_key":"ca_123"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || deletedAccount != "ca_123" {
		t.Fatalf("unexpected disconnect status=%d deleted=%q", resp.StatusCode, deletedAccount)
	}

	resp = integrationRequest(t, srv, b, http.MethodGet, "/integrations/audit?provider=composio&connection_key=ca_123", nil)
	if err := json.NewDecoder(resp.Body).Decode(&audit); err != nil {
		t.Fatalf("decode post-disconnect audit: %v", err)
	}
	resp.Body.Close()
	foundDisconnect := false
	for _, event := range audit.Events {
		if event.EventType == "integration_disconnected" {
			foundDisconnect = true
			if event.Summary != "Disconnected Gmail via Composio" {
				t.Fatalf("unexpected disconnect summary: %+v", event)
			}
		}
	}
	if !foundDisconnect {
		t.Fatalf("expected disconnect audit event: %+v", audit.Events)
	}
}

func TestIntegrationConnectRejectsOversizedBody(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	oversized := []byte(`{"provider":"composio","platform":"` + strings.Repeat("a", maxIntegrationRequestBytes) + `"}`)
	resp := integrationRequest(t, srv, b, http.MethodPost, "/integrations/connect", oversized)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", resp.StatusCode)
	}
}
