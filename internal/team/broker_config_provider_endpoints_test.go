package team

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

// withWuphfHomeDir redirects ~/.wuphf to a temp dir and returns the
// matching ~/.wuphf path so tests can read the file the broker just
// wrote. Mirrors the pattern used in internal/config/config_test.go.
func withWuphfHomeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	return filepath.Join(dir, ".wuphf")
}

// configRequest issues an authenticated /config request through the
// broker's HTTP surface and returns the response.
func configRequest(t *testing.T, b *Broker, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "/config", bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := b.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("/config", b.requireAuth(b.handleConfig))
	mux.ServeHTTP(rec, req)
	return rec
}

// TestHandleConfig_ProviderEndpointsRoundTrip is the load-bearing test
// for the Settings UI: the provider_endpoints map must persist through
// POST → config.json → GET so the Local LLMs section can save and
// reload its inputs.
func TestHandleConfig_ProviderEndpointsRoundTrip(t *testing.T) {
	wuphfDir := withWuphfHomeDir(t)
	b := newTestBroker(t)

	// POST: set mlx-lm and ollama overrides.
	body := `{"provider_endpoints":{
		"mlx-lm": {"base_url":"http://127.0.0.1:9000/v1","model":"my-mlx-model"},
		"ollama": {"base_url":"http://127.0.0.1:11434/v1","model":"qwen2.5-coder:32b-instruct-q4_K_M"}
	}}`
	if rec := configRequest(t, b, http.MethodPost, body); rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Confirm config.json on disk has the merged values — guards against
	// a future refactor that would silently drop the merge.
	raw, err := os.ReadFile(filepath.Join(wuphfDir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var onDisk config.Config
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal config.json: %v", err)
	}
	if onDisk.ProviderEndpoints["mlx-lm"].Model != "my-mlx-model" {
		t.Errorf("mlx-lm model not persisted on disk: %+v", onDisk.ProviderEndpoints)
	}
	if onDisk.ProviderEndpoints["ollama"].BaseURL != "http://127.0.0.1:11434/v1" {
		t.Errorf("ollama base_url not persisted on disk: %+v", onDisk.ProviderEndpoints)
	}

	// GET: same payload comes back.
	rec := configRequest(t, b, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal GET: %v", err)
	}
	endpoints, ok := got["provider_endpoints"].(map[string]any)
	if !ok {
		t.Fatalf("GET response missing provider_endpoints: %s", rec.Body.String())
	}
	mlxlm, _ := endpoints["mlx-lm"].(map[string]any)
	if mlxlm["model"] != "my-mlx-model" {
		t.Errorf("GET mlx-lm.model = %v, want my-mlx-model", mlxlm["model"])
	}
}

// TestHandleConfig_ProviderEndpointsClearsKindOnEmpty exercises the
// "delete an override" path: posting an entry with empty base_url +
// empty model removes the key, falling the kind back to compile-time
// defaults. Without this users would have to hand-edit config.json
// to drop a custom endpoint.
func TestHandleConfig_ProviderEndpointsClearsKindOnEmpty(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)

	// Seed an override.
	if rec := configRequest(t, b, http.MethodPost, `{"provider_endpoints":{"mlx-lm":{"base_url":"http://x/v1","model":"y"}}}`); rec.Code != http.StatusOK {
		t.Fatalf("seed POST: %d %s", rec.Code, rec.Body.String())
	}
	// Clear it via the empty-value gesture.
	if rec := configRequest(t, b, http.MethodPost, `{"provider_endpoints":{"mlx-lm":{"base_url":"","model":""}}}`); rec.Code != http.StatusOK {
		t.Fatalf("clear POST: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ := config.Load()
	if _, ok := cfg.ProviderEndpoints["mlx-lm"]; ok {
		t.Errorf("mlx-lm key still present after empty-value clear: %+v", cfg.ProviderEndpoints)
	}
}

// TestHandleConfig_ProviderEndpointsRejectsUnknownKind keeps the
// validation honest — only kinds the registry knows about can be
// stored, so the doctor panel always has metadata for any key the
// user managed to save.
func TestHandleConfig_ProviderEndpointsRejectsUnknownKind(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	rec := configRequest(t, b, http.MethodPost, `{"provider_endpoints":{"made-up":{"base_url":"http://x","model":"y"}}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown kind, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "made-up") {
		t.Errorf("error did not name the offending kind: %s", rec.Body.String())
	}
}

// TestHandleConfig_LLMProviderAcceptsRegisteredLocalKinds locks in the
// fix that closed the v1 review: the broker's POST validation must
// accept mlx-lm/ollama/exo (not just claude-code/codex/opencode), so
// the Settings "Set as default" button can persist the user's choice.
func TestHandleConfig_LLMProviderAcceptsRegisteredLocalKinds(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	for _, kind := range []string{"mlx-lm", "ollama", "exo"} {
		body := `{"llm_provider":"` + kind + `"}`
		rec := configRequest(t, b, http.MethodPost, body)
		if rec.Code != http.StatusOK {
			t.Errorf("POST llm_provider=%q rejected: %d %s", kind, rec.Code, rec.Body.String())
		}
	}
}
