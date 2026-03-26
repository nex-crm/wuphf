// Package config handles loading, saving, and resolving WUPHF configuration.
// Resolution chain: CLI flag > environment variable > config file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config mirrors ~/.wuphf/config.json.
type Config struct {
	APIKey              string `json:"api_key,omitempty"`
	Email               string `json:"email,omitempty"`
	WorkspaceID         string `json:"workspace_id,omitempty"`
	WorkspaceSlug       string `json:"workspace_slug,omitempty"`
	LLMProvider         string `json:"llm_provider,omitempty"`
	GeminiAPIKey        string `json:"gemini_api_key,omitempty"`
	Pack                string `json:"pack,omitempty"`
	TeamLeadSlug        string `json:"team_lead_slug,omitempty"`
	MaxConcurrent       int    `json:"max_concurrent_agents,omitempty"`
	DefaultFormat       string `json:"default_format,omitempty"`
	DefaultTimeout      int    `json:"default_timeout,omitempty"`
	DevURL              string `json:"dev_url,omitempty"`
	InsightsPollMinutes int    `json:"insights_poll_minutes,omitempty"`
	TaskFollowUpMinutes int    `json:"task_follow_up_minutes,omitempty"`
	TaskReminderMinutes int    `json:"task_reminder_minutes,omitempty"`
	TaskRecheckMinutes  int    `json:"task_recheck_minutes,omitempty"`
}

// ConfigPath returns the absolute path to ~/.wuphf/config.json, with a legacy
// fallback to ~/.nex/config.json when the old file already exists.
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "config.json")
	}
	newPath := filepath.Join(home, ".wuphf", "config.json")
	legacyPath := filepath.Join(home, ".nex", "config.json")
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}
	return newPath
}

// BaseURL returns the resolved base URL.
// Priority: WUPHF_DEV_URL env > NEX_DEV_URL env > config dev_url > production default.
func BaseURL() string {
	if v := os.Getenv("WUPHF_DEV_URL"); v != "" {
		return v
	}
	if v := os.Getenv("NEX_DEV_URL"); v != "" {
		return v
	}
	if cfg, err := load(ConfigPath()); err == nil && cfg.DevURL != "" {
		return cfg.DevURL
	}
	return "https://app.nex.ai"
}

// APIBase returns the developer API base URL.
func APIBase() string {
	return fmt.Sprintf("%s/api/developers", BaseURL())
}

// RegisterURL returns the agent registration URL.
func RegisterURL() string {
	return fmt.Sprintf("%s/api/v1/agents/register", BaseURL())
}

// Load reads the config file. Returns an empty config if the file is missing or unreadable.
func Load() (Config, error) {
	return load(ConfigPath())
}

func load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes cfg to the config file, creating parent directories as needed.
func Save(cfg Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// ResolveNoNex reports whether Nex-backed tools are disabled for this run.
func ResolveNoNex() bool {
	v := strings.TrimSpace(os.Getenv("WUPHF_NO_NEX"))
	if v == "" {
		return false
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// ResolveAPIKey resolves the API key via: flag > WUPHF_API_KEY env > NEX_API_KEY env > config file.
func ResolveAPIKey(flagValue string) string {
	if ResolveNoNex() {
		return ""
	}
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("WUPHF_API_KEY"); v != "" {
		return v
	}
	if v := os.Getenv("NEX_API_KEY"); v != "" {
		return v
	}
	cfg, _ := Load()
	return cfg.APIKey
}

// ResolveFormat resolves the output format via: flag > config file > "text".
func ResolveFormat(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	cfg, _ := Load()
	if cfg.DefaultFormat != "" {
		return cfg.DefaultFormat
	}
	return "text"
}

// ResolveTimeout resolves the timeout (ms) via: flag > config file > 120000.
func ResolveTimeout(flagValue string) int {
	if flagValue != "" {
		if n, err := strconv.Atoi(flagValue); err == nil {
			return n
		}
	}
	cfg, _ := Load()
	if cfg.DefaultTimeout > 0 {
		return cfg.DefaultTimeout
	}
	return 120_000
}

// PersistRegistration merges registration data into the config file.
func PersistRegistration(data map[string]interface{}) error {
	cfg, _ := Load()
	if v, ok := data["api_key"].(string); ok && v != "" {
		cfg.APIKey = v
	}
	if v, ok := data["email"].(string); ok && v != "" {
		cfg.Email = v
	}
	if v, ok := data["workspace_id"].(string); ok && v != "" {
		cfg.WorkspaceID = v
	} else if v, ok := data["workspace_id"].(float64); ok {
		cfg.WorkspaceID = strconv.FormatFloat(v, 'f', -1, 64)
	}
	if v, ok := data["workspace_slug"].(string); ok && v != "" {
		cfg.WorkspaceSlug = v
	}
	return Save(cfg)
}

func ResolveInsightsPollInterval() int {
	minutes := 15
	if raw := os.Getenv("WUPHF_INSIGHTS_INTERVAL_MINUTES"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			minutes = n
		}
	} else if raw := os.Getenv("NEX_INSIGHTS_INTERVAL_MINUTES"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			minutes = n
		}
	} else if cfg, err := Load(); err == nil && cfg.InsightsPollMinutes > 0 {
		minutes = cfg.InsightsPollMinutes
	}
	if minutes < 2 {
		minutes = 2
	}
	return minutes
}

func ResolveTaskFollowUpInterval() int {
	return resolveTaskInterval(
		"WUPHF_TASK_FOLLOWUP_MINUTES",
		"NEX_TASK_FOLLOWUP_MINUTES",
		func(cfg Config) int { return cfg.TaskFollowUpMinutes },
		60,
	)
}

func ResolveTaskReminderInterval() int {
	return resolveTaskInterval(
		"WUPHF_TASK_REMINDER_MINUTES",
		"NEX_TASK_REMINDER_MINUTES",
		func(cfg Config) int { return cfg.TaskReminderMinutes },
		30,
	)
}

func ResolveTaskRecheckInterval() int {
	return resolveTaskInterval(
		"WUPHF_TASK_RECHECK_MINUTES",
		"NEX_TASK_RECHECK_MINUTES",
		func(cfg Config) int { return cfg.TaskRecheckMinutes },
		15,
	)
}

func resolveTaskInterval(envKey, legacyEnvKey string, fromConfig func(Config) int, defaultMinutes int) int {
	minutes := defaultMinutes
	if raw := os.Getenv(envKey); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			minutes = n
		}
	} else if raw := os.Getenv(legacyEnvKey); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			minutes = n
		}
	} else if cfg, err := Load(); err == nil && fromConfig(cfg) > 0 {
		minutes = fromConfig(cfg)
	}
	if minutes < 2 {
		minutes = 2
	}
	return minutes
}
