package action

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// instantlyToolkitDetail is the GET /toolkits/instantly response for an
// API-key toolkit: no Composio-managed OAuth scheme, one API_KEY mode with a
// single required credential field.
func instantlyToolkitDetail() map[string]any {
	return map[string]any{
		"slug":                          "instantly",
		"name":                          "Instantly",
		"composio_managed_auth_schemes": []string{},
		"auth_config_details": []map[string]any{{
			"mode": "API_KEY",
			"fields": map[string]any{
				"connected_account_initiation": map[string]any{
					"required": []map[string]any{{
						"name":        "generic_api_key",
						"displayName": "API Key",
						"type":        "string",
						"required":    true,
					}},
				},
			},
		}},
	}
}

func TestToolkitAuthInfo_DetectsAPIKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/toolkits/instantly", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(instantlyToolkitDetail())
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	info := client.toolkitAuthInfo(context.Background(), "instantly")
	if info.Managed {
		t.Fatalf("API-key toolkit must not be treated as managed: %+v", info)
	}
	if info.Mode != "API_KEY" {
		t.Fatalf("expected Mode=API_KEY, got %q", info.Mode)
	}
	if len(info.Fields) != 1 || info.Fields[0].Name != "generic_api_key" {
		t.Fatalf("expected one generic_api_key field, got %+v", info.Fields)
	}
	if !info.Fields[0].Secret {
		t.Fatalf("an api key field must be marked secret: %+v", info.Fields[0])
	}
}

// toolkitDetailJSON builds a GET /toolkits/{slug} body with the given managed
// schemes and an ordered list of auth_config_details modes (each carrying one
// required generic_api_key field).
func toolkitDetailJSON(managedSchemes []string, modes ...string) map[string]any {
	details := make([]map[string]any, 0, len(modes))
	for _, m := range modes {
		details = append(details, map[string]any{
			"mode": m,
			"fields": map[string]any{
				"connected_account_initiation": map[string]any{
					"required": []map[string]any{{"name": "generic_api_key", "displayName": "API Key", "required": true}},
				},
			},
		})
	}
	return map[string]any{
		"slug":                          "multi",
		"composio_managed_auth_schemes": managedSchemes,
		"auth_config_details":           details,
	}
}

// TestToolkitAuthInfo_MixedModes locks the precedence rules so the connect-path
// choice never becomes payload-order-dependent:
//   - a managed scheme (composio_managed_auth_schemes) always wins;
//   - otherwise the first non-OAuth mode is selected regardless of where OAuth
//     rows sit, and empty-mode rows are skipped;
//   - only-unmanaged-OAuth falls back to managed.
func TestToolkitAuthInfo_MixedModes(t *testing.T) {
	cases := []struct {
		name           string
		managedSchemes []string
		modes          []string
		wantManaged    bool
		wantMode       string
	}{
		{"oauth-before-apikey", nil, []string{"OAUTH2", "API_KEY"}, false, "API_KEY"},
		{"apikey-before-oauth", nil, []string{"API_KEY", "OAUTH2"}, false, "API_KEY"},
		{"empty-mode-row-skipped", nil, []string{"", "API_KEY"}, false, "API_KEY"},
		{"managed-schemes-win", []string{"OAUTH2"}, []string{"API_KEY"}, true, ""},
		{"only-unmanaged-oauth", nil, []string{"OAUTH2"}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/toolkits/multi", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(toolkitDetailJSON(tc.managedSchemes, tc.modes...))
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

			info := client.toolkitAuthInfo(context.Background(), "multi")
			if info.Managed != tc.wantManaged {
				t.Fatalf("Managed=%v want %v (%+v)", info.Managed, tc.wantManaged, info)
			}
			if info.Mode != tc.wantMode {
				t.Fatalf("Mode=%q want %q", info.Mode, tc.wantMode)
			}
		})
	}
}

// A blank required credential must be rejected BEFORE any Composio write, so we
// never leave an orphan auth config behind.
func TestCompleteAPIKeyConnection_RejectsMissingRequired(t *testing.T) {
	authConfigCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/toolkits/instantly", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(instantlyToolkitDetail())
	})
	mux.HandleFunc("/auth_configs", func(w http.ResponseWriter, _ *http.Request) {
		authConfigCalled = true
		_ = json.NewEncoder(w).Encode(map[string]any{"auth_config": map[string]any{"id": "ac_x"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	_, err := client.CompleteAPIKeyConnection(context.Background(), "instantly", map[string]string{"generic_api_key": "   "})
	if err == nil {
		t.Fatalf("expected an error when the required credential is blank")
	}
	if authConfigCalled {
		t.Fatalf("must not create an auth config when a required credential is blank")
	}
}

func TestToolkitAuthInfo_ManagedOAuthFallsBack(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/toolkits/gmail", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"slug":                          "gmail",
			"composio_managed_auth_schemes": []string{"OAUTH2"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	if info := client.toolkitAuthInfo(context.Background(), "gmail"); !info.Managed {
		t.Fatalf("OAuth toolkit must be managed, got %+v", info)
	}
}

// An unintrospectable toolkit (404 / network error) must fall back to managed
// so we never block a toolkit we simply couldn't read.
func TestToolkitAuthInfo_UnknownFallsBackToManaged(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	if info := client.toolkitAuthInfo(context.Background(), "mystery"); !info.Managed {
		t.Fatalf("unknown toolkit must fall back to managed, got %+v", info)
	}
}

// StartIntegrationConnection must short-circuit to needs_fields for an API-key
// toolkit instead of POSTing use_composio_managed_auth (which 400s).
func TestStartIntegrationConnection_NeedsFieldsForAPIKey(t *testing.T) {
	authConfigCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/toolkits/instantly", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(instantlyToolkitDetail())
	})
	mux.HandleFunc("/auth_configs", func(w http.ResponseWriter, _ *http.Request) {
		authConfigCalled = true
		w.WriteHeader(http.StatusBadRequest)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	res, err := client.StartIntegrationConnection(context.Background(), IntegrationConnectRequest{Platform: "Instantly"})
	if err != nil {
		t.Fatalf("connect should not error for an API-key toolkit: %v", err)
	}
	if res.Status != "needs_fields" {
		t.Fatalf("expected status needs_fields, got %q", res.Status)
	}
	if res.AuthMode != "api_key" {
		t.Fatalf("expected auth_mode api_key, got %q", res.AuthMode)
	}
	if len(res.RequiredFields) == 0 {
		t.Fatalf("expected required fields for the key entry form")
	}
	if authConfigCalled {
		t.Fatalf("must NOT attempt the managed /auth_configs path for an API-key toolkit")
	}
}

// CompleteAPIKeyConnection must create a use_custom_auth config then a
// connected account carrying the user's key in the scheme-specific val map.
func TestCompleteAPIKeyConnection_SendsCustomAuthAndKey(t *testing.T) {
	var authBody, accountBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/toolkits/instantly", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(instantlyToolkitDetail())
	})
	mux.HandleFunc("/auth_configs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&authBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"auth_config": map[string]any{"id": "ac_custom"}})
	})
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&accountBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ca_instantly", "status": "ACTIVE"})
	})
	// The credential is verified via the refresh endpoint before we claim
	// "connected"; a live key comes back ACTIVE.
	var refreshBody map[string]any
	mux.HandleFunc("/connected_accounts/ca_instantly/refresh", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&refreshBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ca_instantly", "status": "ACTIVE"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	res, err := client.CompleteAPIKeyConnection(context.Background(), "Instantly", map[string]string{"generic_api_key": "ik_secret"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Status != "connected" {
		t.Fatalf("expected connected, got %q (%+v)", res.Status, res)
	}
	if refreshBody["validate_credentials"] != true {
		t.Fatalf("connect must ask Composio to validate the credential, got refresh body %v", refreshBody)
	}
	if res.ConnectionKey != "ca_instantly" {
		t.Fatalf("expected connection key ca_instantly, got %q", res.ConnectionKey)
	}

	// Auth config request shape.
	ac, _ := authBody["auth_config"].(map[string]any)
	if ac["type"] != "use_custom_auth" {
		t.Fatalf("auth_config.type must be use_custom_auth, got %v", authBody)
	}
	if ac["authScheme"] != "API_KEY" {
		t.Fatalf("auth_config.authScheme must be API_KEY, got %v", authBody)
	}

	// Connected account request shape: the user's key in connection.state.val.
	conn, _ := accountBody["connection"].(map[string]any)
	if conn["user_id"] != "ceo@example.com" {
		t.Fatalf("connected account must carry the user id, got %v", accountBody)
	}
	state, _ := conn["state"].(map[string]any)
	if state["authScheme"] != "API_KEY" {
		t.Fatalf("connection.state.authScheme must be API_KEY, got %v", state)
	}
	val, _ := state["val"].(map[string]any)
	if val["generic_api_key"] != "ik_secret" {
		t.Fatalf("the user's api key must be sent in state.val, got %v", val)
	}
	acRef, _ := accountBody["auth_config"].(map[string]any)
	if acRef["id"] != "ac_custom" {
		t.Fatalf("connected account must reference the created auth config, got %v", accountBody)
	}
}

// apiKeyConnectMux builds the create-path mux (toolkit detail + auth config +
// connected account create) shared by the verification tests. The caller
// registers a /connected_accounts/ca_instantly/refresh handler to drive the
// validation outcome and may inspect *deleted to assert cleanup.
func apiKeyConnectMux(deleted *bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/toolkits/instantly", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(instantlyToolkitDetail())
	})
	mux.HandleFunc("/auth_configs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"auth_config": map[string]any{"id": "ac_custom"}})
	})
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ca_instantly", "status": "ACTIVE"})
	})
	// DELETE /connected_accounts/ca_instantly — orphan cleanup after a rejection.
	mux.HandleFunc("/connected_accounts/ca_instantly", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && deleted != nil {
			*deleted = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	return mux
}

// A credential Composio rejects (refresh reports a non-live status) must NOT be
// reported as connected: the connect call errors and the orphan account is
// deleted so a retry starts clean. This is the core "random key says connected"
// bug fix.
func TestCompleteAPIKeyConnection_RejectsBadKey(t *testing.T) {
	deleted := false
	mux := apiKeyConnectMux(&deleted)
	mux.HandleFunc("/connected_accounts/ca_instantly/refresh", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ca_instantly", "status": "FAILED"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	_, err := client.CompleteAPIKeyConnection(context.Background(), "Instantly", map[string]string{"generic_api_key": "bogus"})
	if err == nil {
		t.Fatalf("a rejected credential must surface an error, not a connection")
	}
	if !deleted {
		t.Fatalf("a rejected credential must delete the orphan connected account")
	}
}

// Composio can signal a rejected credential with a 401/403/422 from the refresh
// endpoint rather than a status body — that path must also fail closed.
func TestCompleteAPIKeyConnection_RejectsBadKeyOnAuthError(t *testing.T) {
	deleted := false
	mux := apiKeyConnectMux(&deleted)
	mux.HandleFunc("/connected_accounts/ca_instantly/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	_, err := client.CompleteAPIKeyConnection(context.Background(), "Instantly", map[string]string{"generic_api_key": "bogus"})
	if err == nil {
		t.Fatalf("a 401 from validation must surface an error, not a connection")
	}
	if !deleted {
		t.Fatalf("a rejected credential must delete the orphan connected account")
	}
}

// When the experimental validation path is unavailable (404 / 5xx / network),
// we must not block the connection: fall back to the create-time status so a
// real key still connects on plans/toolkits without the validate endpoint.
func TestCompleteAPIKeyConnection_UnverifiedFallsBackToConnected(t *testing.T) {
	deleted := false
	mux := apiKeyConnectMux(&deleted)
	mux.HandleFunc("/connected_accounts/ca_instantly/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &ComposioREST{APIKey: "cmp_test", UserID: "ceo@example.com", BaseURL: server.URL, Client: server.Client()}

	res, err := client.CompleteAPIKeyConnection(context.Background(), "Instantly", map[string]string{"generic_api_key": "ik_secret"})
	if err != nil {
		t.Fatalf("an unverifiable connection must not be blocked: %v", err)
	}
	if res.Status != "connected" {
		t.Fatalf("expected connected fallback, got %q", res.Status)
	}
	if deleted {
		t.Fatalf("an unverified (not rejected) credential must NOT be deleted")
	}
}
