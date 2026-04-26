package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveProviderEndpoint_DefaultsWhenNothingConfigured exercises the
// fallback path: no env, no config file → caller's defaults are returned
// verbatim.
func TestResolveProviderEndpoint_DefaultsWhenNothingConfigured(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_MLX_LM_BASE_URL", "")
		t.Setenv("WUPHF_MLX_LM_MODEL", "")

		baseURL, model := ResolveProviderEndpoint("mlx-lm",
			"http://default/v1", "default-model")
		if baseURL != "http://default/v1" {
			t.Errorf("baseURL = %q, want default", baseURL)
		}
		if model != "default-model" {
			t.Errorf("model = %q, want default", model)
		}
	})
}

// TestResolveProviderEndpoint_ConfigFileOverridesDefault exercises the
// middle layer: a config file with provider_endpoints overrides the
// caller-supplied defaults.
func TestResolveProviderEndpoint_ConfigFileOverridesDefault(t *testing.T) {
	withTempConfig(t, func(dir string) {
		t.Setenv("WUPHF_MLX_LM_BASE_URL", "")
		t.Setenv("WUPHF_MLX_LM_MODEL", "")

		cfg := Config{
			ProviderEndpoints: map[string]ProviderEndpoint{
				"mlx-lm": {BaseURL: "http://configured:9000/v1", Model: "configured-model"},
			},
		}
		writeTestConfig(t, dir, cfg)

		baseURL, model := ResolveProviderEndpoint("mlx-lm",
			"http://default/v1", "default-model")
		if baseURL != "http://configured:9000/v1" {
			t.Errorf("baseURL = %q, want configured override", baseURL)
		}
		if model != "configured-model" {
			t.Errorf("model = %q, want configured override", model)
		}
	})
}

// TestResolveProviderEndpoint_EnvOverridesConfig exercises the top of the
// resolution order: env > config file > default.
func TestResolveProviderEndpoint_EnvOverridesConfig(t *testing.T) {
	withTempConfig(t, func(dir string) {
		cfg := Config{
			ProviderEndpoints: map[string]ProviderEndpoint{
				"ollama": {BaseURL: "http://configured/v1", Model: "configured-model"},
			},
		}
		writeTestConfig(t, dir, cfg)

		t.Setenv("WUPHF_OLLAMA_BASE_URL", "http://env/v1")
		t.Setenv("WUPHF_OLLAMA_MODEL", "env-model")

		baseURL, model := ResolveProviderEndpoint("ollama",
			"http://default/v1", "default-model")
		if baseURL != "http://env/v1" {
			t.Errorf("baseURL = %q, want env override", baseURL)
		}
		if model != "env-model" {
			t.Errorf("model = %q, want env override", model)
		}
	})
}

// TestResolveProviderEndpoint_PartialOverrides verifies a partially-set
// config (only base_url) doesn't blank out the model — each field falls
// through independently.
func TestResolveProviderEndpoint_PartialOverrides(t *testing.T) {
	withTempConfig(t, func(dir string) {
		cfg := Config{
			ProviderEndpoints: map[string]ProviderEndpoint{
				"exo": {BaseURL: "http://configured/v1"}, // model intentionally empty
			},
		}
		writeTestConfig(t, dir, cfg)
		t.Setenv("WUPHF_EXO_BASE_URL", "")
		t.Setenv("WUPHF_EXO_MODEL", "")

		baseURL, model := ResolveProviderEndpoint("exo",
			"http://default/v1", "default-model")
		if baseURL != "http://configured/v1" {
			t.Errorf("baseURL = %q, want configured", baseURL)
		}
		if model != "default-model" {
			t.Errorf("model = %q, want compile-time default (config left blank)", model)
		}
	})
}

// TestResolveProviderEndpoint_KindWithDashesMapsToEnvUnderscore confirms
// that mlx-lm → WUPHF_MLX_LM_BASE_URL (not WUPHF_MLX-LM_BASE_URL, which
// most shells refuse to set).
func TestResolveProviderEndpoint_KindWithDashesMapsToEnvUnderscore(t *testing.T) {
	withTempConfig(t, func(_ string) {
		t.Setenv("WUPHF_MLX_LM_BASE_URL", "http://expected/v1")
		t.Setenv("WUPHF_MLX_LM_MODEL", "expected-model")
		baseURL, model := ResolveProviderEndpoint("mlx-lm",
			"http://default/v1", "default-model")
		if baseURL != "http://expected/v1" || model != "expected-model" {
			t.Errorf("env-via-underscore not honoured: baseURL=%q model=%q", baseURL, model)
		}
	})
}

func writeTestConfig(t *testing.T, dir string, cfg Config) {
	t.Helper()
	path := filepath.Join(dir, ".wuphf", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
