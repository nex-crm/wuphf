// Package config handles loading, saving, and resolving WUPHF configuration.
// Resolution chain: CLI flag > environment variable > config file.
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RuntimeHomeDir returns the home directory WUPHF should use for persisted
// runtime state. Inventive runs may override this with WUPHF_RUNTIME_HOME so
// they don't inherit an existing office from the user's global ~/.wuphf.
//
// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — this is the
// definition of RuntimeHomeDir itself; os.UserHomeDir() is the fallback only.
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
	// Composio user-key auth (fallback when the CLI can't mint a project ak_
	// key — the current composio CLI no longer writes one via `dev init`). The
	// SDK accepts EITHER a project `ak_` key (sent as x-api-key) OR the
	// user-scoped `uak_` session key paired with org + project ids (sent as
	// x-user-api-key / x-org-id / x-project-id). These hold the latter;
	// ComposioAPIKey, when set, takes precedence.
	ComposioUserAPIKey string `json:"composio_user_api_key,omitempty"`
	ComposioOrgID      string `json:"composio_org_id,omitempty"`
	ComposioProjectID  string `json:"composio_project_id,omitempty"`
	ActionProvider     string `json:"action_provider,omitempty"`
	Email              string `json:"email,omitempty"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
	WorkspaceSlug      string `json:"workspace_slug,omitempty"`
	LLMProvider        string `json:"llm_provider,omitempty"`
	// LLMProviderPriority is an ordered list of provider identifiers (same
	// vocabulary as LLMProvider — "claude-code", "codex", "opencode", etc.) that agents
	// should try in order when picking a runtime. LLMProvider remains the
	// single-value primary choice; the priority list is consulted by agent
	// creation and fallback flows. An empty slice means "fall back to
	// LLMProvider alone", preserving legacy behavior.
	LLMProviderPriority []string `json:"llm_provider_priority,omitempty"`
	GeminiAPIKey        string   `json:"gemini_api_key,omitempty"`
	AnthropicAPIKey     string   `json:"anthropic_api_key,omitempty"`
	OpenAIAPIKey        string   `json:"openai_api_key,omitempty"`
	// RealtimeModel is the OpenAI Realtime model used by the "Demo workflow to
	// Nex" voice call (speech-to-speech + screen vision). Empty falls back to
	// the compiled default in ResolveRealtimeModel.
	RealtimeModel string `json:"realtime_model,omitempty"`
	MinimaxAPIKey string `json:"minimax_api_key,omitempty"`
	Blueprint     string `json:"blueprint,omitempty"`
	// Pack is retained as a legacy alias for the active operation blueprint/template.
	Pack                string   `json:"pack,omitempty"`
	TeamLeadSlug        string   `json:"team_lead_slug,omitempty"`
	MaxConcurrent       int      `json:"max_concurrent_agents,omitempty"`
	DefaultFormat       string   `json:"default_format,omitempty"`
	DefaultTimeout      int      `json:"default_timeout,omitempty"`
	DevURL              string   `json:"dev_url,omitempty"`
	InsightsPollMinutes int      `json:"insights_poll_minutes,omitempty"`
	TaskFollowUpMinutes int      `json:"task_follow_up_minutes,omitempty"`
	TaskReminderMinutes int      `json:"task_reminder_minutes,omitempty"`
	TaskRecheckMinutes  int      `json:"task_recheck_minutes,omitempty"`
	TelegramBotToken    string   `json:"telegram_bot_token,omitempty"`
	SlackBotToken       string   `json:"slack_bot_token,omitempty"`
	SlackAppToken       string   `json:"slack_app_token,omitempty"`
	CompanyName         string   `json:"company_name,omitempty"`
	CompanyDescription  string   `json:"company_description,omitempty"`
	CompanyGoals        string   `json:"company_goals,omitempty"`
	CompanySize         string   `json:"company_size,omitempty"`
	CompanyPriority     string   `json:"company_priority,omitempty"`
	OwnerName           string   `json:"owner_name,omitempty"`
	OwnerRole           string   `json:"owner_role,omitempty"`
	CompanyWebsite      string   `json:"company_website,omitempty"`
	CompanyFilePaths    []string `json:"company_file_paths,omitempty"`
	PendingCompanySeed  bool     `json:"pending_company_seed,omitempty"`

	// Product analytics consent (PostHog). Two independent channels:
	// anonymous usage events and session recordings (typed text masked). Both
	// default ON when unset (nil) so legacy installs keep the documented
	// default, but the whole analytics layer is dormant unless a PostHog
	// project key is configured (build-time VITE_PUBLIC_POSTHOG_KEY or
	// WUPHF_POSTHOG_KEY env), so an unset flag is moot until analytics is
	// actually wired up. The pointer type distinguishes "never chosen"
	// (default ON) from an explicit opt-out (false), which a plain bool
	// with omitempty could not. See docs/specs/product-analytics.md.
	AnalyticsTelemetryEnabled        *bool `json:"analytics_telemetry_enabled,omitempty"`
	AnalyticsSessionRecordingEnabled *bool `json:"analytics_session_recording_enabled,omitempty"`

	OpenclawBridges    []OpenclawBridgeBinding `json:"openclaw_bridges,omitempty"`
	OpenclawGatewayURL string                  `json:"openclaw_gateway_url,omitempty"`
	OpenclawToken      string                  `json:"openclaw_token,omitempty"`

	// ProviderEndpoints overrides the default base URL / model for OpenAI-
	// compatible local runtimes (mlx-lm, ollama, exo). Keys are provider Kind
	// strings; missing keys fall back to compile-time defaults declared in
	// internal/provider/<kind>.go. Per-kind env vars
	// (WUPHF_MLX_LM_BASE_URL, WUPHF_OLLAMA_MODEL, etc.) take precedence over
	// this map.
	ProviderEndpoints map[string]ProviderEndpoint `json:"provider_endpoints,omitempty"`
	ImageEndpoints    map[string]ImageEndpoint    `json:"image_endpoints,omitempty"`
}

// ProviderEndpoint configures one OpenAI-compatible HTTP backend.
type ProviderEndpoint struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
}

// ImageEndpoint is per-image-gen-provider runtime config (api key, base URL,
// default model). Stored under Config.ImageEndpoints keyed by kind slug
// (nano-banana, higgsfield, gpt-image, seedance, comfyui).
type ImageEndpoint struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
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
//
// Resolution order:
//  1. Explicit --memory-backend flag value.
//  2. WUPHF_MEMORY_BACKEND env var.
//  3. config-file `memory_backend`.
//  4. Default: `gbrain` when gbrain is ready (installed on PATH + a semantic
//     embedding provider available — an OpenAI key or a local Ollama embedding
//     model), otherwise `markdown`. gbrain is the strong-default organizational
//     memory backend, but only when it can do semantic retrieval; a
//     keyword-only gbrain is ~equivalent to the markdown wiki, so the markdown
//     fallback keeps a fresh OSS clone with no embedder booting with a
//     zero-config, git-native wiki at ~/.wuphf/wiki.
//
// After selection, `--no-nex` forces a `nex` selection to `none` (it disables
// the Nex backend itself) but never blocks gbrain or markdown.
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
		if gbrainBackendReady() {
			return MemoryBackendGBrain
		}
		return MemoryBackendMarkdown
	}
	if backend == MemoryBackendNex && ResolveNoNex() {
		return MemoryBackendNone
	}
	return backend
}

// gbrainBackendReady reports whether gbrain can serve as the implicit default
// backend: the gbrain binary is on PATH and a semantic embedding provider is
// available (an OpenAI key or a local Ollama embedding model). A no-embedding /
// keyword-only gbrain is ~equivalent to the markdown wiki, so it does NOT make
// gbrain the default — the markdown fallback stays in that case. Anthropic
// alone does not count: Anthropic has no embeddings API.
//
// This is intentionally implemented here rather than via internal/gbrain to
// avoid an import cycle (internal/gbrain imports this package); the Ollama probe
// mirrors gbrain.OllamaEmbeddingModel's logic. Only consulted for the implicit
// default — an explicit backend selection bypasses it entirely.
func gbrainBackendReady() bool {
	return gbrainBinaryInstalled() && gbrainEmbeddingAvailable()
}

func gbrainBinaryInstalled() bool {
	if cmd := strings.TrimSpace(os.Getenv("WUPHF_GBRAIN_COMMAND")); cmd != "" {
		if _, err := exec.LookPath(cmd); err == nil {
			return true
		}
	}
	if _, err := exec.LookPath("gbrain"); err == nil {
		return true
	}
	return false
}

// gbrainEmbeddingAvailable reports whether gbrain can do semantic retrieval:
// an OpenAI key is configured, or a local Ollama embedding model is pulled.
func gbrainEmbeddingAvailable() bool {
	if strings.TrimSpace(ResolveOpenAIAPIKey()) != "" {
		return true
	}
	return gbrainOllamaEmbedderAvailable()
}

// gbrainOllamaEmbedderAvailable is a seam: defaults to the real probe and is
// overridden in tests so the implicit-default resolution is deterministic
// regardless of whether the host running the suite has ollama installed.
var gbrainOllamaEmbedderAvailable = detectGBrainOllamaEmbedder

// ollamaListTimeout bounds the `ollama list` probe so a wedged ollama never
// stalls backend resolution at launch.
const ollamaListTimeout = 3 * time.Second

// detectGBrainOllamaEmbedder reports whether ollama is on PATH and has at least
// one embedding model pulled. It never pulls a model (no network side effects)
// — it only inspects what is already present. Mirrors
// gbrain.OllamaEmbeddingModel; duplicated here to avoid an import cycle.
func detectGBrainOllamaEmbedder() bool {
	if _, err := exec.LookPath("ollama"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), ollamaListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ollama", "list")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(fields[0]), "embed") {
			return true
		}
	}
	return false
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

// allowedLLMProviderKinds is the set of values normalizeLLMProvider accepts
// for --provider, WUPHF_LLM_PROVIDER, and the config file. claude-code and
// codex are baked in for backward compatibility and standalone config tests
// (which don't import the provider package). Additional kinds are registered
// at init() time by their provider implementation via AllowLLMProviderKind —
// see internal/provider/registry.go.
var (
	allowedLLMProviderKindsMu sync.RWMutex
	allowedLLMProviderKinds   = map[string]struct{}{
		"claude-code": {},
		"codex":       {},
		"opencode":    {},
	}
)

// AllowLLMProviderKind registers name as an acceptable provider value.
// Provider implementations call this from init() so that
// config.ResolveLLMProvider returns the kind for env/config values that match.
// Idempotent: re-registering a known kind is a no-op.
func AllowLLMProviderKind(name string) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return
	}
	allowedLLMProviderKindsMu.Lock()
	defer allowedLLMProviderKindsMu.Unlock()
	allowedLLMProviderKinds[name] = struct{}{}
}

func normalizeLLMProvider(value string) string {
	name := strings.TrimSpace(strings.ToLower(value))
	if name == "" {
		return ""
	}
	allowedLLMProviderKindsMu.RLock()
	defer allowedLLMProviderKindsMu.RUnlock()
	if _, ok := allowedLLMProviderKinds[name]; ok {
		return name
	}
	return ""
}

// IsLLMProviderKindAllowed reports whether name is registered as a runnable
// global LLM provider kind. Use this at API boundaries that persist the
// install-wide `llm_provider` (or members of `llm_provider_priority` /
// `provider_endpoints` map keys) — provider.ValidateKind is broader and
// includes member-only kinds (e.g. openclaw) that the runtime launcher
// cannot dispatch as a global default. The empty string is NOT allowed
// here so callers must handle the "clear back to default" gesture
// explicitly with a nil-vs-empty check on the request body.
func IsLLMProviderKindAllowed(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false
	}
	allowedLLMProviderKindsMu.RLock()
	defer allowedLLMProviderKindsMu.RUnlock()
	_, ok := allowedLLMProviderKinds[name]
	return ok
}

// AllowedLLMProviderKinds returns a sorted snapshot of the registered global
// LLM provider kinds. Useful for error messages that want to list what was
// expected without poking at package internals.
func AllowedLLMProviderKinds() []string {
	allowedLLMProviderKindsMu.RLock()
	out := make([]string, 0, len(allowedLLMProviderKinds))
	for k := range allowedLLMProviderKinds {
		out = append(out, k)
	}
	allowedLLMProviderKindsMu.RUnlock()
	sort.Strings(out)
	return out
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

	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — codex config
	// layering reads from the user's real home, not the WUPHF workspace root.
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

// ResolveOpencodeModel returns the effective Opencode model for the current
// run. Resolution: WUPHF_OPENCODE_MODEL env > OPENCODE_MODEL env > empty (Opencode
// picks its configured default). Unlike Codex, Opencode has no on-disk config
// file layout WUPHF needs to inspect — users configure their Opencode
// ~/.config/opencode settings directly, so there is no cwd-relative search.
func ResolveOpencodeModel() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_OPENCODE_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("OPENCODE_MODEL")); v != "" {
		return v
	}
	return ""
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

// ResolveComposioUserAPIKey resolves the user-scoped Composio session key
// (`uak_…`). Resolution: WUPHF_COMPOSIO_USER_API_KEY env > config file.
func ResolveComposioUserAPIKey() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_COMPOSIO_USER_API_KEY")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.ComposioUserAPIKey)
}

// ResolveComposioOrgID resolves the Composio org id used with the user key.
func ResolveComposioOrgID() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_COMPOSIO_ORG_ID")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.ComposioOrgID)
}

// ResolveComposioProjectID resolves the Composio project id used with the user
// key. Optional: the SDK falls back to the org's default project when absent.
func ResolveComposioProjectID() string {
	if ResolveNoNex() {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_COMPOSIO_PROJECT_ID")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.ComposioProjectID)
}

// IsComposioConfigured reports whether Composio has usable credentials: either
// a project `ak_` key, or the user-key pair (`uak_` + org id). Project id is
// optional. Used to drive the `composio_key_set` flag the onboarding UI gates
// on, so user-key sign-ins flip the office out of the first-run state too.
func IsComposioConfigured() bool {
	if ResolveComposioAPIKey() != "" {
		return true
	}
	return ResolveComposioUserAPIKey() != "" && ResolveComposioOrgID() != ""
}

// IsAnalyticsTelemetryEnabled reports whether anonymous product-analytics
// events may be sent. Default ON when the operator has not explicitly opted
// out (nil flag). The analytics layer is still dormant unless a PostHog key is
// configured, so this only takes effect once analytics is wired up.
func (c Config) IsAnalyticsTelemetryEnabled() bool {
	return c.AnalyticsTelemetryEnabled == nil || *c.AnalyticsTelemetryEnabled
}

// IsAnalyticsSessionRecordingEnabled reports whether session recordings
// (with typed text masked) may be captured. Default ON when not explicitly
// opted out (nil).
func (c Config) IsAnalyticsSessionRecordingEnabled() bool {
	return c.AnalyticsSessionRecordingEnabled == nil || *c.AnalyticsSessionRecordingEnabled
}

// ResolvePostHogKey returns the PostHog project (write-only) API key used for
// product analytics, from env only. Empty means the backend injects no key, in
// which case the frontend falls back to the build-time VITE_PUBLIC_POSTHOG_KEY
// (which is also empty in a stock OSS build, keeping analytics dormant).
// Resolution: WUPHF_POSTHOG_KEY env > POSTHOG_KEY env.
func ResolvePostHogKey() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_POSTHOG_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("POSTHOG_KEY"))
}

// ResolvePostHogHost returns the PostHog ingestion host from env, or empty to
// let the frontend use its build-time default (us.i.posthog.com).
// Resolution: WUPHF_POSTHOG_HOST env > POSTHOG_HOST env.
func ResolvePostHogHost() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_POSTHOG_HOST")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("POSTHOG_HOST"))
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

// ResolveSlackBotToken returns the Slack bot token used for the Web API
// (chat.postMessage, users.info, conversations.members). This is the
// workspace-scoped "xoxb-" token issued when the app is installed.
// Resolution: SLACK_BOT_TOKEN env > config file.
func ResolveSlackBotToken() string {
	if v := strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.SlackBotToken)
}

// ResolveSlackAppToken returns the Slack app-level token used to open a
// Socket Mode connection for inbound events. This is the app-scoped "xapp-"
// token with the connections:write scope; it is distinct from the bot token
// and is required only for the inbound (Socket Mode) half of the bridge.
// Resolution: SLACK_APP_TOKEN env > config file.
func ResolveSlackAppToken() string {
	if v := strings.TrimSpace(os.Getenv("SLACK_APP_TOKEN")); v != "" {
		return v
	}
	cfg, _ := Load()
	return strings.TrimSpace(cfg.SlackAppToken)
}

// SaveSlackTokens persists the Slack bot and app tokens to config.json. Empty
// values are stored as-is so a caller can clear a token by passing "". Returns
// an error rather than silently dropping Load/Save failures: a failed Load
// would otherwise write an EMPTY config back over every other persisted field.
func SaveSlackTokens(botToken, appToken string) error {
	cfg, err := Load()
	if err != nil {
		return fmt.Errorf("save slack tokens: load config: %w", err)
	}
	cfg.SlackBotToken = strings.TrimSpace(botToken)
	cfg.SlackAppToken = strings.TrimSpace(appToken)
	if err := Save(cfg); err != nil {
		return fmt.Errorf("save slack tokens: %w", err)
	}
	return nil
}

// CompanyContextBlock returns a prompt fragment with company context for agent
// system prompts. Returns empty string if no relevant fields are configured.
func CompanyContextBlock() string {
	cfg, _ := Load()
	name := strings.TrimSpace(cfg.CompanyName)
	website := strings.TrimSpace(cfg.CompanyWebsite)
	ownerName := strings.TrimSpace(cfg.OwnerName)
	if name == "" && website == "" && ownerName == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("== COMPANY CONTEXT ==\n")
	if name != "" {
		sb.WriteString(fmt.Sprintf("Company: %s\n", name))
	}
	if desc := strings.TrimSpace(cfg.CompanyDescription); desc != "" {
		sb.WriteString(fmt.Sprintf("What they do: %s\n", desc))
	}
	if goals := strings.TrimSpace(cfg.CompanyGoals); goals != "" {
		sb.WriteString(fmt.Sprintf("Current goals: %s\n", goals))
	}
	if priority := strings.TrimSpace(cfg.CompanyPriority); priority != "" {
		sb.WriteString(fmt.Sprintf("Immediate priority: %s\n", priority))
	}
	if website != "" {
		sb.WriteString(fmt.Sprintf("Website: %s\n", website))
	}
	if ownerName != "" {
		sb.WriteString(fmt.Sprintf("Owner: %s", ownerName))
		if role := strings.TrimSpace(cfg.OwnerRole); role != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", role))
		}
		sb.WriteString("\n")
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

// DefaultRealtimeModel is the OpenAI Realtime model used by the demo call when
// nothing is configured. The exact GA model string can drift, so it stays a
// single override point (env/config) rather than being hardcoded at call sites.
const DefaultRealtimeModel = "gpt-realtime-2"

// ResolveRealtimeModel resolves the OpenAI Realtime model for the demo call.
// Resolution: WUPHF_REALTIME_MODEL env > config file > DefaultRealtimeModel.
func ResolveRealtimeModel() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_REALTIME_MODEL")); v != "" {
		return v
	}
	if cfg, _ := Load(); strings.TrimSpace(cfg.RealtimeModel) != "" {
		return strings.TrimSpace(cfg.RealtimeModel)
	}
	return DefaultRealtimeModel
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
// Resolution: WUPHF_COMPOSIO_USER_ID env > COMPOSIO_USER_ID env > config email >
// workspace identifier > "default".
//
// The user_id only namespaces this office's connected accounts on Composio's
// side — any stable string works. It must never be empty once Composio is
// signed in, or the catalog (which gates on a non-empty identity) stays blank
// even though available integrations don't need a per-user identity. So a
// signed-in office with no recorded email still falls back to a stable id.
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
	for _, candidate := range []string{cfg.Email, cfg.WorkspaceSlug, cfg.WorkspaceID} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return "default"
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
	minutes := 30
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
// WUPHF_OPENCLAW_TOKEN wins for WUPHF-specific setup; OPENCLAW_GATEWAY_TOKEN is
// accepted for compatibility with OpenClaw's own Gateway docs.
func ResolveOpenclawToken() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_OPENCLAW_TOKEN")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN")); v != "" {
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

// ResolveProviderEndpoint resolves the base URL and model for an OpenAI-
// compatible local provider Kind (mlx-lm, ollama, exo). Resolution order:
//
//  1. Per-kind env vars: WUPHF_<KIND>_BASE_URL and WUPHF_<KIND>_MODEL
//     (kind uppercased with '-' → '_'; e.g. mlx-lm → WUPHF_MLX_LM_BASE_URL).
//  2. Config.ProviderEndpoints[kind].
//  3. The compile-time defaults supplied by the caller.
//
// Returned values are always non-empty when defaults are non-empty; this
// helper never returns "" for a registered Kind.
func ResolveProviderEndpoint(kind, defaultBaseURL, defaultModel string) (string, string) {
	envKind := strings.ToUpper(strings.ReplaceAll(kind, "-", "_"))
	baseURL := strings.TrimSpace(os.Getenv("WUPHF_" + envKind + "_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("WUPHF_" + envKind + "_MODEL"))
	if baseURL == "" || model == "" {
		cfg, _ := Load()
		if ep, ok := cfg.ProviderEndpoints[kind]; ok {
			if baseURL == "" {
				baseURL = strings.TrimSpace(ep.BaseURL)
			}
			if model == "" {
				model = strings.TrimSpace(ep.Model)
			}
		}
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if model == "" {
		model = defaultModel
	}
	return baseURL, model
}

// ResolveOpenclawIdentityPath returns where the Ed25519 device identity is
// persisted. OpenClaw's gateway requires device-pair auth — token alone grants
// zero scopes — so this keypair is effectively credentials: write only to a
// user-scoped 0600 file under the WUPHF home.
//
// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — OpenClaw identity
// is device-bound credentials, not workspace state. Per-workspace OpenClaw
// identity is a separate feature decision deferred to post-v1.
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
