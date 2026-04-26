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

// TestHandleConfig_ProviderEndpointsRejectsDangerousURLSchemes is the
// security gate from the v6 review (HIGH): a locally-authenticated
// client must NOT be able to persist `file://`, `gopher://`,
// `unix://`, schemeless, or hostless URLs as a provider endpoint.
// Persisting one would let the attacker redirect every subsequent
// agent turn to their own target — exfiltrating the system prompt
// + conversation history. Allowed schemes are http and https only;
// host must be non-empty.
func TestHandleConfig_ProviderEndpointsRejectsDangerousURLSchemes(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	for _, badURL := range []string{
		"file:///etc/passwd",
		"gopher://evil.example.com/",
		"unix:///var/run/mlx.sock",
		"javascript:alert(1)",
		"//no-scheme.example.com/v1",
		"http://", // hostless
		"https://",
		"http://:8080",      // port-only host (Host=":8080", Hostname()="")
		"http://:8080/v1",   // port-only host with path
		"https://:443/path", // port-only HTTPS
		"not a url at all",
	} {
		body := `{"provider_endpoints":{"mlx-lm":{"base_url":"` + badURL + `","model":"x"}}}`
		rec := configRequest(t, b, http.MethodPost, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("base_url=%q: expected 400, got %d %s", badURL, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "base_url") {
			t.Errorf("base_url=%q: 400 body did not name the offending field: %s", badURL, rec.Body.String())
		}
	}
}

// TestHandleConfig_ProviderEndpointsAcceptsLoopbackAndHTTPS is the
// positive case for the security gate above. Loopback HTTP is the
// canonical local-LLM target (mlx_lm.server, ollama, exo); HTTPS to
// a remote host is supported when users tunnel an LLM. Both must
// pass the validator.
func TestHandleConfig_ProviderEndpointsAcceptsLoopbackAndHTTPS(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	for _, goodURL := range []string{
		"http://127.0.0.1:8080/v1",
		"http://localhost:11434/v1",
		"http://[::1]:8080/v1",
		"https://gateway.example.com/v1",
		"https://gateway.example.com/v1?key=abc",
		"http://192.168.1.10:8080/v1", // user explicitly pointed at a LAN box
	} {
		body := `{"provider_endpoints":{"mlx-lm":{"base_url":"` + goodURL + `","model":"x"}}}`
		rec := configRequest(t, b, http.MethodPost, body)
		if rec.Code != http.StatusOK {
			t.Errorf("base_url=%q: expected 200, got %d %s", goodURL, rec.Code, rec.Body.String())
		}
	}
}

// TestValidateProviderEndpointURL exercises the predicate directly
// so a regression localizes here rather than in the broker handler.
func TestValidateProviderEndpointURL(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"http://127.0.0.1:8080/v1", false},
		{"https://api.example.com/v1", false},
		{"HTTP://Example.com/v1", false}, // case-insensitive scheme
		{"file:///etc/passwd", true},
		{"gopher://example.com", true},
		{"unix:///socket", true},
		{"javascript:alert(1)", true},
		{"//host.example.com/path", true}, // schemeless protocol-relative
		{"/relative/path", true},
		{"", true},
		{"   ", true},
		{"http://", true},           // empty host
		{"https://", true},          // empty host
		{"http://:8080", true},      // port-only — Host=":8080" but Hostname()=""
		{"http://:8080/v1", true},   // port-only with path
		{"https://:443/path", true}, // port-only HTTPS
	}
	for _, tc := range cases {
		err := validateProviderEndpointURL(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateProviderEndpointURL(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
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

// TestHandleConfig_LLMProviderRejectsMemberOnlyKinds is the v7 Major
// finding: provider.ValidateKind accepts kinds that are valid for
// per-member bindings (e.g. openclaw) but are NOT registered as
// runnable global LLM providers — config.AllowLLMProviderKind is
// only called from each runtime's init() in internal/provider/*.go,
// and openclaw deliberately omits that registration. Persisting
// llm_provider=openclaw used to silently succeed; the resolver
// would then normalize the value back to "" on load and the user's
// choice would be lost. The broker now rejects member-only kinds at
// the boundary so the failure is loud and immediate.
func TestHandleConfig_LLMProviderRejectsMemberOnlyKinds(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	for _, kind := range []string{"openclaw", "totally-fake"} {
		body := `{"llm_provider":"` + kind + `"}`
		rec := configRequest(t, b, http.MethodPost, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST llm_provider=%q expected 400, got %d %s", kind, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unsupported llm_provider") {
			t.Errorf("POST llm_provider=%q response did not name the field: %s", kind, rec.Body.String())
		}
	}
}

// TestHandleConfig_LLMProviderClearsToDefault is the v7 Major paired
// finding: nil pointer vs empty string must round-trip distinctly.
// The Settings UI clears a saved provider override by posting
// {"llm_provider":""} — that gesture must reach disk + the in-process
// runtimeProvider so /health stops reporting the stale value
// without a broker restart.
func TestHandleConfig_LLMProviderClearsToDefault(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)

	// Seed an override.
	if rec := configRequest(t, b, http.MethodPost, `{"llm_provider":"mlx-lm"}`); rec.Code != http.StatusOK {
		t.Fatalf("seed POST: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ := config.Load()
	if cfg.LLMProvider != "mlx-lm" {
		t.Fatalf("seed didn't persist: cfg.LLMProvider=%q", cfg.LLMProvider)
	}

	// Clear via empty string.
	if rec := configRequest(t, b, http.MethodPost, `{"llm_provider":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear POST: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ = config.Load()
	if cfg.LLMProvider != "" {
		t.Errorf("clear didn't reach disk: cfg.LLMProvider=%q", cfg.LLMProvider)
	}
}

// TestHandleConfig_LLMProviderPriorityRejectsMemberOnlyKinds is the
// counterpart to the singular `llm_provider` test. The priority
// list is the fallback chain for the global LLM provider, so each
// entry has the same runnable-only requirement.
func TestHandleConfig_LLMProviderPriorityRejectsMemberOnlyKinds(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	body := `{"llm_provider_priority":["claude-code","openclaw","mlx-lm"]}`
	rec := configRequest(t, b, http.MethodPost, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for priority with openclaw, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "openclaw") {
		t.Errorf("400 body did not name the offending entry: %s", rec.Body.String())
	}
}

// TestHandleConfig_ProviderEndpointsRejectsMemberOnlyKinds extends
// the provider_endpoints validation to the same restriction — a
// per-kind HTTP endpoint config only makes sense for runtimes the
// resolver can dispatch as a global LLM.
func TestHandleConfig_ProviderEndpointsRejectsMemberOnlyKinds(t *testing.T) {
	withWuphfHomeDir(t)
	b := newTestBroker(t)
	body := `{"provider_endpoints":{"openclaw":{"base_url":"http://127.0.0.1:9000/v1","model":"x"}}}`
	rec := configRequest(t, b, http.MethodPost, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for provider_endpoints[openclaw], got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "openclaw") {
		t.Errorf("400 body did not name the offending kind: %s", rec.Body.String())
	}
}
