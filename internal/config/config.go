// Package config handles loading, saving, and resolving WUPHF configuration.
// Resolution chain: CLI flag > environment variable > config file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// RuntimeHomeDir returns the home directory WUPHF should use for persisted
// runtime state. Inventive runs may override this with WUPHF_RUNTIME_HOME so
// they don't inherit an existing office from the user's global ~/.wuphf.
func RuntimeHomeDir() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_RUNTIME_HOME")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// Config mirrors ~/.wuphf/config.json.
type Config struct {
	APIKey         string `json:"api_key,omitempty"`
	MemoryBackend  string `json:"memory_backend,omitempty"`
	OneAPIKey      string `json:"one_api_key,omitempty"`
	ComposioAPIKey string `json:"composio_api_key,omitempty"`
	ActionProvider string `json:"action_provider,omitempty"`
	Email          string `json:"email,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	WorkspaceSlug  string `json:"workspace_slug,omitempty"`
	LLMProvider    string `json:"llm_provider,omitempty"`
	// LLMProviderPriority is an ordered list of provider identifiers (same
	// vocabulary as LLMProvider — "claude-code", "codex", etc.) that agents
	// should try in order when picking a runtime. LLMProvider remains the
	// single-value primary choice; the priority list is consulted by agent
	// creation and fallback flows. An empty slice means "fall back to
	// LLMProvider alone", preserving legacy behavior.
	LLMProviderPriority []string `json:"llm_provider_priority,omitempty"`
	GeminiAPIKey        string   `json:"gemini_api_key,omitempty"`
	AnthropicAPIKey     string   `json:"anthropic_api_key,omitempty"`
	OpenAIAPIKey        string   `json:"openai_api_key,omitempty"`
	MinimaxAPIKey       string   `json:"minimax_api_key,omitempty"`
	Blueprint           string   `json:"blueprint,omitempty"`
	// Pack is retained as a legacy alias for the active operation blueprint/template.
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
	TelegramBotToken    string `json:"telegram_bot_token,omitempty"`
	CompanyName         string `json:"company_name,omitempty"`
	CompanyDescription  string `json:"company_description,omitempty"`
	CompanyGoals        string `json:"company_goals,omitempty"`
	CompanySize         string `json:"company_size,omitempty"`
	CompanyPriority     string `json:"company_priority,omitempty"`

	OpenclawBridges    []OpenclawBridgeBinding `json:"openclaw_bridges,omitempty"`
	OpenclawGatewayURL string                  `json:"openclaw_gateway_url,omitempty"`
	OpenclawToken      string                  `json:"openclaw_token,omitempty"`
}

const (
	MemoryBackendNone     = "none"
	MemoryBackendNex      = "nex"
	MemoryBackendGBrain   = "gbrain"
	MemoryBackendMarkdown = "markdown"
)

// OpenclawBridgeBinding binds a WUPHF agent session to an OpenClaw bridge slug.
type OpenclawBridgeBinding struct {
	SessionKey  string `json:"session_key"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name,omitempty"`
}

// ActiveBlueprint returns the preferred operation blueprint/template id.
// Blueprint is the primary field; Pack remains as a compatibility alias.
func (c Config) ActiveBlueprint() string {
	if v := strings.TrimSpace(c.Blueprint); v != "" {
		return v
	}
	return strings.TrimSpace(c.Pack)
}

// SetActiveBlueprint stores the selected operation blueprint/template id in
// the preferred field. The legacy Pack alias is retained for reads only.
func (c *Config) SetActiveBlueprint(id string) {
	id = strings.TrimSpace(id)
	c.Blueprint = id
}

// ConfigPath returns the absolute path to ~/.wuphf/config.json, with a legacy
// fallback to ~/.nex/config.json when the old file already exists.
func ConfigPath() string {
	// Env override for test harnesses that need to isolate config state from
	// the user's real ~/.wuphf/config.json without remapping HOME (which
	// breaks macOS keychain-backed CLI auth).
	if p := strings.TrimSpace(os.Getenv("WUPHF_CONFIG_PATH")); p != "" {
		return p
	}
	home := RuntimeHomeDir()
	if home == "" {
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
//
// Note: as of the nex-cli migration, BaseURL is only used by the legacy
// developer API client surface (api.Client) which still backs the workflow
// engine's /v1/insights and /v1/context/ask calls. New Nex integrations
// should shell out via the internal/nex package instead.
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

// NormalizeMemoryBackend returns a supported memory backend or the empty string.
func NormalizeMemoryBackend(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case MemoryBackendNone:
		return MemoryBackendNone
	case MemoryBackendNex:
		return MemoryBackendNex
	case MemoryBackendGBrain:
		return MemoryBackendGBrain
	case MemoryBackendMarkdown:
		return MemoryBackendMarkdown
	default:
		return ""
	}
}

// ResolveMemoryBackend resolves the active organizational memory backend.
// Resolution: flag/env override > config file > default.
//
// Defaults:
//   - `nex` when Nex is allowed for the run
//   - `none` when --no-nex is set and no alternate backend was selected
//
// `--no-nex` always disables the Nex backend itself, but does not prevent an
// alternate backend like GBrain from being selected.
func ResolveMemoryBackend(flagValue string) string {
	backend := NormalizeMemoryBackend(flagValue)
	if backend == "" {
		backend = NormalizeMemoryBackend(os.Getenv("WUPHF_MEMORY_BACKEND"))
	}
	if backend == "" {
		cfg, _ := Load()
		backend = NormalizeMemoryBackend(cfg.MemoryBackend)
	}
	if backend == "" {
		if ResolveNoNex() {
			return MemoryBackendNone
		}
		return MemoryBackendNex
	}
	if backend == MemoryBackendNex && ResolveNoNex() {
		return MemoryBackendNone
	}
	return backend
}

// MemoryBackendLabel returns a short user-facing label for the backend.
func MemoryBackendLabel(backend string) string {
	switch NormalizeMemoryBackend(backend) {
	case MemoryBackendNex:
		return "Nex"
	case MemoryBackendGBrain:
		return "GBrain"
	case MemoryBackendMarkdown:
		return "Markdown wiki"
	default:
		return "Local-only"
	}
}

// ResolveLLMProvider resolves the active LLM provider for this run.
// Resolution: flag/env override > config file > default claude-code.
// Only supported interactive providers are returned.
func ResolveLLMProvider(flagValue string) string {
	if v := normalizeLLMProvider(flagValue); v != "" {
		return v
	}
	if v := normalizeLLMProvider(os.Getenv("WUPHF_LLM_PROVIDER")); v != "" {
		return v
	}
	cfg, _ := Load()
	if v := normalizeLLMProvider(cfg.LLMProvider); v != "" {
		return v
	}
	return "claude-code"
}

func normalizeLLMProvider(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "claude-code":
		return "claude-code"
	case "codex":
		return "codex"
	default:
		return ""
	}
}

var codexModelLinePattern = regexp.MustCompile(`(?m)^\s*model\s*=\s*("([^"\\]|\\.)*"|'[^']*')`)

// ResolveCodexModel returns the effective Codex model for the current working
// directory, following the documented Codex config layering:
// WUPHF_CODEX_MODEL/CODEX_MODEL env > nearest .codex/config.toml > ~/.codex/config.toml.
func ResolveCodexModel(cwd string) string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_CODEX_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_MODEL")); v != "" {
		return v
	}
	for _, path := range codexConfigSearchPaths(cwd) {
		if model := codexModelFromFile(path); model != "" {
			return model
		}
	}
	return ""
}

func codexConfigSearchPaths(cwd string) []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, 8)
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	if absCwd, err := filepath.Abs(strings.TrimSpace(cwd)); err == nil && absCwd != "" {
		for dir := absCwd; ; dir = filepath.Dir(dir) {
			add(filepath.Join(dir, ".codex", "config.toml"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".codex", "config.toml"))
	}
	return paths
}

func codexModelFromFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	match := codexModelLinePattern.FindSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	value := strings.TrimSpace(string(match[1]))
	if len(value) >= 2 {
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			if unquoted, err := strconv.Unquote(value); err == nil {
				return strings.TrimSpace(unquoted)
			}
		}
		if strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`) {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return strings.TrimSpace(value)
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

// ResolveOneSecret resolves the Nex-managed One secret.
// One is disabled entirely when Nex is disabled for the session.
// Resolution: WUPHF_ONE_SECRET env > ONE_SECRET env > config file.
func ResolveOneSecret() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_ONE_SECRET")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ONE_SECRET")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.OneAPIKey)
}

// ResolveOneIdentity resolves the identity scope WUPHF should use with One.
// Resolution: WUPHF_ONE_IDENTITY env > ONE_IDENTITY env > config email.
func ResolveOneIdentity() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_ONE_IDENTITY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ONE_IDENTITY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.Email)
}

// ResolveOneIdentityType resolves the One identity type.
// Resolution: WUPHF_ONE_IDENTITY_TYPE env > ONE_IDENTITY_TYPE env > "user".
func ResolveOneIdentityType() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_ONE_IDENTITY_TYPE")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ONE_IDENTITY_TYPE")); v != "" {
		return v
	}
	if ResolveOneIdentity() == "" {
		return ""
	}
	return "user"
}

// OneSetupSummary explains how integrations are handled for the current setup.
func OneSetupSummary() string {
	if ResolveNoNex() {
		return "disabled with Nex (--no-nex)"
	}
	email := ResolveOneIdentity()
	secret := ResolveOneSecret()
	switch {
	case email != "" && secret != "":
		return fmt.Sprintf("managed by Nex via One (%s)", email)
	case email != "":
		return fmt.Sprintf("managed by Nex via One (%s), provisioning pending", email)
	case secret != "":
		return "managed by Nex via One"
	default:
		return "managed by Nex via One after Nex setup"
	}
}

// OneSetupBlurb is the user-facing copy for setup and config surfaces.
func OneSetupBlurb() string {
	if ResolveNoNex() {
		return "Nex is disabled for this session, so WUPHF-managed integrations are disabled too."
	}
	email := ResolveOneIdentity()
	if email != "" {
		return fmt.Sprintf("WUPHF uses One for integrations and manages it automatically with your Nex email (%s).", email)
	}
	return "WUPHF uses One for integrations and will manage it automatically once Nex setup is complete."
}

// ResolveComposioAPIKey resolves the Composio API key.
// Resolution: WUPHF_COMPOSIO_API_KEY env > COMPOSIO_API_KEY env > config file.
func ResolveComposioAPIKey() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_COMPOSIO_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("COMPOSIO_API_KEY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.ComposioAPIKey)
}

// ResolveTelegramBotToken returns the stored Telegram bot token from config.
func ResolveTelegramBotToken() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_TELEGRAM_BOT_TOKEN")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.TelegramBotToken)
}

// SaveTelegramBotToken persists the bot token to config.json.
func SaveTelegramBotToken(token string) {
	cfg, _ := Load()
	cfg.TelegramBotToken = strings.TrimSpace(token)
	_ = Save(cfg)
}

// CompanyContextBlock returns a prompt fragment with company context for agent
// system prompts. Returns empty string if no company name is configured.
func CompanyContextBlock() string {
	cfg, _ := Load()
	name := strings.TrimSpace(cfg.CompanyName)
	if name == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("== COMPANY CONTEXT ==\n")
	sb.WriteString(fmt.Sprintf("Company: %s\n", name))
	if desc := strings.TrimSpace(cfg.CompanyDescription); desc != "" {
		sb.WriteString(fmt.Sprintf("What they do: %s\n", desc))
	}
	if goals := strings.TrimSpace(cfg.CompanyGoals); goals != "" {
		sb.WriteString(fmt.Sprintf("Current goals: %s\n", goals))
	}
	if priority := strings.TrimSpace(cfg.CompanyPriority); priority != "" {
		sb.WriteString(fmt.Sprintf("Immediate priority: %s\n", priority))
	}
	sb.WriteString("\n")
	return sb.String()
}

// ResolveGeminiAPIKey resolves the Gemini API key.
// Resolution: WUPHF_GEMINI_API_KEY env > GEMINI_API_KEY env > config file.
func ResolveGeminiAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.GeminiAPIKey)
}

// ResolveAnthropicAPIKey resolves the Anthropic API key.
// Resolution: WUPHF_ANTHROPIC_API_KEY env > ANTHROPIC_API_KEY env > config file.
func ResolveAnthropicAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_ANTHROPIC_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.AnthropicAPIKey)
}

// ResolveOpenAIAPIKey resolves the OpenAI API key.
// Resolution: WUPHF_OPENAI_API_KEY env > OPENAI_API_KEY env > config file.
func ResolveOpenAIAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_OPENAI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.OpenAIAPIKey)
}

// ResolveMinimaxAPIKey resolves the Minimax API key.
// Resolution: WUPHF_MINIMAX_API_KEY env > MINIMAX_API_KEY env > config file.
func ResolveMinimaxAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_MINIMAX_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("MINIMAX_API_KEY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.MinimaxAPIKey)
}

// ResolveComposioUserID resolves the Composio user identity WUPHF should use.
// Resolution: WUPHF_COMPOSIO_USER_ID env > COMPOSIO_USER_ID env > config email.
func ResolveComposioUserID() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_COMPOSIO_USER_ID")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("COMPOSIO_USER_ID")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.Email)
}

// ResolveActionProvider resolves the preferred external action provider.
// Resolution: WUPHF_ACTION_PROVIDER env > ACTION_PROVIDER env > config file > auto.
func ResolveActionProvider() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_ACTION_PROVIDER")); v != "" {
		return strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("ACTION_PROVIDER")); v != "" {
		return strings.ToLower(v)
	}
	cfg, _ := Load()
	if v := strings.TrimSpace(cfg.ActionProvider); v != "" {
		return strings.ToLower(v)
	}
	return "auto"
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

// ResolveOpenclawToken returns the OpenClaw gateway auth token from env > config.
func ResolveOpenclawToken() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_OPENCLAW_TOKEN")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.OpenclawToken)
}

// ResolveOpenclawGatewayURL returns the OpenClaw gateway URL from env > config > default loopback.
func ResolveOpenclawGatewayURL() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_OPENCLAW_GATEWAY_URL")); v != "" {
		return v
	}
	cfg, _ := Load()
	if v := strings.TrimSpace(cfg.OpenclawGatewayURL); v != "" {
		return v
	}
	return "ws://127.0.0.1:18789"
}

// ResolveOpenclawIdentityPath returns where the Ed25519 device identity is
// persisted. OpenClaw's gateway requires device-pair auth — token alone grants
// zero scopes — so this keypair is effectively credentials: write only to a
// user-scoped 0600 file under the WUPHF home.
func ResolveOpenclawIdentityPath() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_OPENCLAW_IDENTITY_PATH")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "openclaw", "identity.json")
	}
	return filepath.Join(home, ".wuphf", "openclaw", "identity.json")
}
