package team

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func decodeResolve(t *testing.T, resp *http.Response) integrationResolveResponse {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("resolve status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out integrationResolveResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	return out
}

// Unconfigured Composio routes a mutating action to connect (so the connect
// decision can guide setup), never proceeds blind.
func TestResolveUnconfiguredRoutesToConnect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	body, _ := json.Marshal(integrationResolveRequest{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		Data:     map[string]any{"to": "x@y.com"},
	})
	got := decodeResolve(t, integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", body))
	if got.Decision != "connect" {
		t.Fatalf("unconfigured mutating action: decision=%q want connect (%+v)", got.Decision, got)
	}
	if got.ReadOnly {
		t.Fatalf("GMAIL_SEND_EMAIL classified read-only")
	}
}

// Read-only action against a connected platform proceeds with no human; a
// mutating action against the same connection raises approve with a preview
// raw envelope whose secrets are masked. Also asserts the registry persisted.
func TestResolveConnectedApproveAndReadOnlyProceed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("WUPHF_RUNTIME_HOME", tmp)
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "cmp_test")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "ceo@example.com")

	composioMux := http.NewServeMux()
	composioMux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id":         "ca_123",
				"status":     "ACTIVE",
				"toolkit":    map[string]any{"slug": "gmail", "name": "Gmail"},
				"connection": map[string]any{"name": "Founder Gmail"},
			}},
		})
	})
	composioMux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "ca_123", "status": "ACTIVE", "toolkit": map[string]any{"slug": "gmail"},
		})
	})
	composioServer := httptest.NewServer(composioMux)
	defer composioServer.Close()
	t.Setenv("WUPHF_COMPOSIO_BASE_URL", composioServer.URL)

	statePath := filepath.Join(t.TempDir(), "state.json")
	b := NewBrokerAt(statePath)
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	// Mutating action -> approve with a masked raw envelope.
	body, _ := json.Marshal(integrationResolveRequest{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		Data: map[string]any{
			"to":      "lead@acme.com",
			"subject": "Hi",
			"token":   "super-secret-value",
		},
	})
	got := decodeResolve(t, integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", body))
	if got.Decision != "approve" {
		t.Fatalf("connected mutating action: decision=%q want approve (%+v)", got.Decision, got)
	}
	if got.State != "connected" {
		t.Fatalf("expected effective state connected, got %q", got.State)
	}
	if got.RawEnvelope == nil {
		t.Fatalf("expected a preview raw envelope for an approve decision")
	}
	if got.Account == nil || got.Account.Key != "ca_123" {
		t.Fatalf("expected account key ca_123, got %+v", got.Account)
	}
	// The secret in the payload must be masked in the raw envelope.
	args, _ := got.RawEnvelope.Data["arguments"].(map[string]any)
	if args != nil {
		if v, ok := args["token"]; ok && v != "***" {
			t.Fatalf("token not masked in raw envelope: %v", v)
		}
	}

	// Read-only action against the same connection proceeds with no human.
	roBody, _ := json.Marshal(integrationResolveRequest{Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"})
	ro := decodeResolve(t, integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", roBody))
	if ro.Decision != "proceed" {
		t.Fatalf("connected read-only action: decision=%q want proceed (%+v)", ro.Decision, ro)
	}

	// The probe populated and persisted the registry.
	if entry, ok := b.lookupConnectionRegistry("gmail"); !ok || entry.State != "connected" || entry.ConnectionKey != "ca_123" {
		t.Fatalf("registry not updated from probe: ok=%v entry=%+v", ok, entry)
	}
	b2 := NewBrokerAt(statePath)
	if err := b2.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if entry, ok := b2.lookupConnectionRegistry("gmail"); !ok || entry.State != "connected" {
		t.Fatalf("registry did not persist across reload: ok=%v entry=%+v", ok, entry)
	}
}

func TestResolveRejectsMissingFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	body, _ := json.Marshal(integrationResolveRequest{Platform: "gmail"}) // no action_id
	resp := integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing action_id, got %d", resp.StatusCode)
	}
}
