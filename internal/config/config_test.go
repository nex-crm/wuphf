package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
			APIKey:        "test-key",
			Email:         "user@example.com",
			WorkspaceID:   "ws-123",
			WorkspaceSlug: "my-ws",
			LLMProvider:   "gemini",
			GeminiAPIKey:  "gemini-key",
			DefaultFormat: "json",
			DefaultTimeout: 30_000,
			DevURL:        "http://localhost:3000",
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

func TestRegisterURL(t *testing.T) {
	t.Setenv("WUPHF_DEV_URL", "")
	withTempConfig(t, func(_ string) {
		want := "https://app.nex.ai/api/v1/agents/register"
		if got := RegisterURL(); got != want {
			t.Fatalf("RegisterURL: got %q, want %q", got, want)
		}
	})
}
