package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempConfig redirects ConfigPath to a temp dir for the duration of f.
func withTempConfig(t *testing.T, f func(dir string)) {
	t.Helper()
	dir := t.TempDir()
	// Override UserHomeDir by pointing ConfigPath indirectly via HOME env var.
	orig := os.Getenv("HOME")
	t.Setenv("HOME", dir)
	defer os.Setenv("HOME", orig)
	f(dir)
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	withTempConfig(t, func(_ string) {
		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected no error for missing file, got: %v", err)
		}
		if cfg.APIKey != "" || cfg.Email != "" {
			t.Fatalf("expected empty config, got: %+v", cfg)
		}
	})
}

func TestRoundtrip(t *testing.T) {
	withTempConfig(t, func(_ string) {
		in := Config{
			APIKey:             "test-key",
			Email:              "user@example.com",
			WorkspaceID:        "ws-123",
			WorkspaceSlug:      "my-ws",
			LLMProvider:        "gemini",
			GeminiAPIKey:       "gemini-key",
			AnthropicAPIKey:    "anthropic-key",
			OpenAIAPIKey:       "openai-key",
			MinimaxAPIKey:      "minimax-key",
			DefaultFormat:      "json",
			DefaultTimeout:     30_000,
			DevURL:             "http://localhost:3000",
			CompanyName:        "Acme Corp",
			CompanyDescription: "AI-powered analytics",
			CompanyGoals:       "Ship MVP, get 10 customers",
			CompanySize:        "2-5",
			CompanyPriority:    "Launch landing page",
		}
		if err := Save(in); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
		out, err := Load()
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if out != in {
			t.Fatalf("roundtrip mismatch:\n  got:  %+v\n  want: %+v", out, in)
		}
	})
}

func TestSaveCreatesParentDirs(t *testing.T) {
	withTempConfig(t, func(dir string) {
		if err := Save(Config{APIKey: "k"}); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
		path := filepath.Join(dir, ".wuphf", "config.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected config file at %s: %v", path, err)
		}
	})
}

func TestSaveWritesValidJSON(t *testing.T) {
	withTempConfig(t, func(dir string) {
		if err := Save(Config{APIKey: "k", Email: "e@e.com"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, ".wuphf", "config.json"))
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, raw)
		}
		if m["api_key"] != "k" {
			t.Fatalf("unexpected api_key: %v", m["api_key"])
		}
	})
}

func TestResolveAPIKeyFlag(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_API_KEY", "env-key")
		if got := ResolveAPIKey("flag-key"); got != "flag-key" {
			t.Fatalf("flag should win, got: %s", got)
		}
	})
}

func TestResolveAPIKeyEnv(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_API_KEY", "env-key")
		if got := ResolveAPIKey(""); got != "env-key" {
			t.Fatalf("env should win over config, got: %s", got)
		}
	})
}

func TestResolveAPIKeyConfigFile(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_API_KEY", "")
		_ = Save(Config{APIKey: "file-key"})
		if got := ResolveAPIKey(""); got != "file-key" {
			t.Fatalf("config file fallback failed, got: %s", got)
		}
	})
}

func TestResolveOneSecretDisabledWhenNoNex(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_NO_NEX", "1")
		t.Setenv("WUPHF_ONE_SECRET", "env-secret")
		_ = Save(Config{OneAPIKey: "file-secret"})
		if got := ResolveOneSecret(); got != "" {
			t.Fatalf("expected no One secret when Nex is disabled, got %q", got)
		}
	})
}

func TestResolveOneIdentityFallsBackToEmail(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{Email: "founder@example.com"})
		if got := ResolveOneIdentity(); got != "founder@example.com" {
			t.Fatalf("expected config email identity, got %q", got)
		}
		if got := ResolveOneIdentityType(); got != "user" {
			t.Fatalf("expected default identity type user, got %q", got)
		}
	})
}

func TestOneSetupSummaryManagedPending(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{Email: "ops@example.com"})
		got := OneSetupSummary()
		if got != "managed by Nex via One (ops@example.com), provisioning pending" {
			t.Fatalf("unexpected setup summary %q", got)
		}
	})
}

func TestResolveComposioAPIKeyFallsBackToConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{ComposioAPIKey: "cmp-key"})
		if got := ResolveComposioAPIKey(); got != "cmp-key" {
			t.Fatalf("expected composio key from config, got %q", got)
		}
	})
}

func TestResolveGeminiAPIKeyEnvOverride(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_GEMINI_API_KEY", "wuphf-gemini")
		_ = Save(Config{GeminiAPIKey: "file-gemini"})
		if got := ResolveGeminiAPIKey(); got != "wuphf-gemini" {
			t.Fatalf("expected WUPHF env override, got %q", got)
		}
	})
}

func TestResolveGeminiAPIKeyFallbackEnv(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("GEMINI_API_KEY", "generic-gemini")
		if got := ResolveGeminiAPIKey(); got != "generic-gemini" {
			t.Fatalf("expected GEMINI_API_KEY fallback, got %q", got)
		}
	})
}

func TestResolveGeminiAPIKeyConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{GeminiAPIKey: "cfg-gemini"})
		if got := ResolveGeminiAPIKey(); got != "cfg-gemini" {
			t.Fatalf("expected config fallback, got %q", got)
		}
	})
}

func TestResolveAnthropicAPIKeyEnvOverride(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_ANTHROPIC_API_KEY", "wuphf-anthropic")
		_ = Save(Config{AnthropicAPIKey: "file-anthropic"})
		if got := ResolveAnthropicAPIKey(); got != "wuphf-anthropic" {
			t.Fatalf("expected WUPHF env override, got %q", got)
		}
	})
}

func TestResolveAnthropicAPIKeyFallbackEnv(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("ANTHROPIC_API_KEY", "generic-anthropic")
		if got := ResolveAnthropicAPIKey(); got != "generic-anthropic" {
			t.Fatalf("expected ANTHROPIC_API_KEY fallback, got %q", got)
		}
	})
}

func TestResolveAnthropicAPIKeyConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{AnthropicAPIKey: "cfg-anthropic"})
		if got := ResolveAnthropicAPIKey(); got != "cfg-anthropic" {
			t.Fatalf("expected config fallback, got %q", got)
		}
	})
}

func TestResolveOpenAIAPIKeyEnvOverride(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_OPENAI_API_KEY", "wuphf-openai")
		_ = Save(Config{OpenAIAPIKey: "file-openai"})
		if got := ResolveOpenAIAPIKey(); got != "wuphf-openai" {
			t.Fatalf("expected WUPHF env override, got %q", got)
		}
	})
}

func TestResolveOpenAIAPIKeyFallbackEnv(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("OPENAI_API_KEY", "generic-openai")
		if got := ResolveOpenAIAPIKey(); got != "generic-openai" {
			t.Fatalf("expected OPENAI_API_KEY fallback, got %q", got)
		}
	})
}

func TestResolveOpenAIAPIKeyConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{OpenAIAPIKey: "cfg-openai"})
		if got := ResolveOpenAIAPIKey(); got != "cfg-openai" {
			t.Fatalf("expected config fallback, got %q", got)
		}
	})
}

func TestResolveMinimaxAPIKeyEnvOverride(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_MINIMAX_API_KEY", "wuphf-minimax")
		_ = Save(Config{MinimaxAPIKey: "file-minimax"})
		if got := ResolveMinimaxAPIKey(); got != "wuphf-minimax" {
			t.Fatalf("expected WUPHF env override, got %q", got)
		}
	})
}

func TestResolveMinimaxAPIKeyFallbackEnv(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("MINIMAX_API_KEY", "generic-minimax")
		if got := ResolveMinimaxAPIKey(); got != "generic-minimax" {
			t.Fatalf("expected MINIMAX_API_KEY fallback, got %q", got)
		}
	})
}

func TestResolveMinimaxAPIKeyConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{MinimaxAPIKey: "cfg-minimax"})
		if got := ResolveMinimaxAPIKey(); got != "cfg-minimax" {
			t.Fatalf("expected config fallback, got %q", got)
		}
	})
}

func TestCompanyContextBlockFull(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{
			CompanyName:        "Acme Corp",
			CompanyDescription: "AI analytics for e-commerce",
			CompanyGoals:       "Ship MVP, get 10 customers",
			CompanyPriority:    "Launch landing page",
		})
		block := CompanyContextBlock()
		if block == "" {
			t.Fatal("expected non-empty company context block")
		}
		for _, want := range []string{"Acme Corp", "AI analytics", "Ship MVP", "Launch landing page"} {
			if !strings.Contains(block, want) {
				t.Errorf("expected block to contain %q, got:\n%s", want, block)
			}
		}
	})
}

func TestCompanyContextBlockEmpty(t *testing.T) {
	withTempConfig(t, func(_ string) {
		block := CompanyContextBlock()
		if block != "" {
			t.Fatalf("expected empty block when no company name, got: %q", block)
		}
	})
}

func TestCompanyContextBlockNameOnly(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{CompanyName: "Solo Inc"})
		block := CompanyContextBlock()
		if block == "" {
			t.Fatal("expected non-empty block with name only")
		}
		if !strings.Contains(block, "Solo Inc") {
			t.Errorf("expected block to contain company name")
		}
		if strings.Contains(block, "Current goals") {
			t.Errorf("should not contain goals when empty")
		}
	})
}

func TestResolveActionProviderDefaultsToAuto(t *testing.T) {
	withTempConfig(t, func(_ string) {
		if got := ResolveActionProvider(); got != "auto" {
			t.Fatalf("expected auto provider, got %q", got)
		}
	})
}

func TestResolveActionProviderUsesConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{ActionProvider: "composio"})
		if got := ResolveActionProvider(); got != "composio" {
			t.Fatalf("expected composio provider, got %q", got)
		}
	})
}

func TestResolveLLMProviderDefaultsToClaude(t *testing.T) {
	withTempConfig(t, func(_ string) {
		if got := ResolveLLMProvider(""); got != "claude-code" {
			t.Fatalf("expected claude-code default, got %q", got)
		}
	})
}

func TestResolveLLMProviderUsesEnvOverride(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_LLM_PROVIDER", "codex")
		if got := ResolveLLMProvider(""); got != "codex" {
			t.Fatalf("expected codex env override, got %q", got)
		}
	})
}

func TestResolveLLMProviderNormalizesUnsupportedConfig(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{LLMProvider: "gemini"})
		if got := ResolveLLMProvider(""); got != "claude-code" {
			t.Fatalf("expected unsupported provider to normalize to claude-code, got %q", got)
		}
	})
}

func TestResolveCodexModelUsesEnvOverride(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_CODEX_MODEL", "gpt-5.4")
		if got := ResolveCodexModel(""); got != "gpt-5.4" {
			t.Fatalf("expected env codex model, got %q", got)
		}
	})
}

func TestResolveCodexModelPrefersNearestProjectConfig(t *testing.T) {
	withTempConfig(t, func(dir string) {
		homeConfigDir := filepath.Join(dir, ".codex")
		if err := os.MkdirAll(homeConfigDir, 0o755); err != nil {
			t.Fatalf("mkdir home codex dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(homeConfigDir, "config.toml"), []byte("model = \"gpt-5.4\"\n"), 0o644); err != nil {
			t.Fatalf("write home config: %v", err)
		}

		projectRoot := filepath.Join(dir, "repo")
		projectConfigDir := filepath.Join(projectRoot, ".codex")
		if err := os.MkdirAll(projectConfigDir, 0o755); err != nil {
			t.Fatalf("mkdir project codex dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(projectConfigDir, "config.toml"), []byte("model = \"gpt-5.4-mini\"\n"), 0o644); err != nil {
			t.Fatalf("write project config: %v", err)
		}

		nested := filepath.Join(projectRoot, "nested", "deeper")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested dir: %v", err)
		}

		if got := ResolveCodexModel(nested); got != "gpt-5.4-mini" {
			t.Fatalf("expected nearest project codex model, got %q", got)
		}
	})
}

func TestResolveFormatFlag(t *testing.T) {
	withTempConfig(t, func(_ string) {
		if got := ResolveFormat("json"); got != "json" {
			t.Fatalf("expected json, got: %s", got)
		}
	})
}

func TestResolveFormatConfigDefault(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{DefaultFormat: "json"})
		if got := ResolveFormat(""); got != "json" {
			t.Fatalf("expected json from config, got: %s", got)
		}
	})
}

func TestResolveFormatFallback(t *testing.T) {
	withTempConfig(t, func(_ string) {
		if got := ResolveFormat(""); got != "text" {
			t.Fatalf("expected text default, got: %s", got)
		}
	})
}

func TestResolveTimeoutFlag(t *testing.T) {
	withTempConfig(t, func(_ string) {
		if got := ResolveTimeout("5000"); got != 5000 {
			t.Fatalf("expected 5000, got: %d", got)
		}
	})
}

func TestResolveTimeoutConfigDefault(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{DefaultTimeout: 60_000})
		if got := ResolveTimeout(""); got != 60_000 {
			t.Fatalf("expected 60000, got: %d", got)
		}
	})
}

func TestResolveTimeoutFallback(t *testing.T) {
	withTempConfig(t, func(_ string) {
		if got := ResolveTimeout(""); got != 120_000 {
			t.Fatalf("expected 120000, got: %d", got)
		}
	})
}

func TestPersistRegistration(t *testing.T) {
	withTempConfig(t, func(_ string) {
		data := map[string]interface{}{
			"api_key":        "reg-key",
			"email":          "reg@example.com",
			"workspace_id":   "ws-456",
			"workspace_slug": "reg-ws",
		}
		if err := PersistRegistration(data); err != nil {
			t.Fatalf("PersistRegistration: %v", err)
		}
		cfg, _ := Load()
		if cfg.APIKey != "reg-key" {
			t.Errorf("api_key: got %q, want %q", cfg.APIKey, "reg-key")
		}
		if cfg.Email != "reg@example.com" {
			t.Errorf("email: got %q", cfg.Email)
		}
		if cfg.WorkspaceID != "ws-456" {
			t.Errorf("workspace_id: got %q", cfg.WorkspaceID)
		}
		if cfg.WorkspaceSlug != "reg-ws" {
			t.Errorf("workspace_slug: got %q", cfg.WorkspaceSlug)
		}
	})
}

func TestPersistRegistrationNumericWorkspaceID(t *testing.T) {
	withTempConfig(t, func(_ string) {
		data := map[string]interface{}{
			"workspace_id": float64(12345),
		}
		if err := PersistRegistration(data); err != nil {
			t.Fatalf("PersistRegistration: %v", err)
		}
		cfg, _ := Load()
		if cfg.WorkspaceID != "12345" {
			t.Errorf("numeric workspace_id: got %q, want %q", cfg.WorkspaceID, "12345")
		}
	})
}

func TestPersistRegistrationMerges(t *testing.T) {
	withTempConfig(t, func(_ string) {
		_ = Save(Config{APIKey: "existing-key", DefaultFormat: "json"})
		if err := PersistRegistration(map[string]interface{}{"email": "new@example.com"}); err != nil {
			t.Fatalf("PersistRegistration: %v", err)
		}
		cfg, _ := Load()
		if cfg.APIKey != "existing-key" {
			t.Errorf("existing api_key should be preserved, got %q", cfg.APIKey)
		}
		if cfg.DefaultFormat != "json" {
			t.Errorf("existing default_format should be preserved, got %q", cfg.DefaultFormat)
		}
		if cfg.Email != "new@example.com" {
			t.Errorf("email should be set, got %q", cfg.Email)
		}
	})
}

func TestBaseURLDevURLEnv(t *testing.T) {
	t.Setenv("WUPHF_DEV_URL", "http://localhost:4000")
	if got := BaseURL(); got != "http://localhost:4000" {
		t.Fatalf("expected localhost, got: %s", got)
	}
}

func TestBaseURLDefault(t *testing.T) {
	t.Setenv("WUPHF_DEV_URL", "")
	withTempConfig(t, func(_ string) {
		if got := BaseURL(); got != "https://app.nex.ai" {
			t.Fatalf("expected production URL, got: %s", got)
		}
	})
}

func TestAPIBase(t *testing.T) {
	t.Setenv("WUPHF_DEV_URL", "")
	withTempConfig(t, func(_ string) {
		want := "https://app.nex.ai/api/developers"
		if got := APIBase(); got != want {
			t.Fatalf("APIBase: got %q, want %q", got, want)
		}
	})
}

// RegisterURL used to point at the legacy HTTP registration endpoint.
// Registration now shells out via internal/nex.Register (nex-cli), so the
// URL builder is gone. The test is removed along with it.
