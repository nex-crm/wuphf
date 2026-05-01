package team

// headless_opencode_mcp.go owns the MCP config-file shape used by the
// opencode CLI: writeHeadlessOpencodeMCPConfig writes the per-agent
// JSON the CLI reads on startup, buildHeadlessOpencodeMCPEntry composes
// the wuphf-office server entry, and the small slug + path helpers
// keep file naming consistent. Mirrors headless_openai_compat_mcp.go's
// shape so the two MCP-config flows are easy to compare.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// writeHeadlessOpencodeMCPConfig merges WUPHF's MCP server definition into an
// agent-scoped Opencode config derived from the user's normal
// $HOME/.config/opencode/opencode.json. The caller passes the returned path via
// OPENCODE_CONFIG, so concurrent agents do not race to rewrite a shared config
// with different WUPHF_AGENT_SLUG values. Preserves other top-level user keys
// (theme, provider preferences, user-configured MCP servers) and only touches
// the wuphf-office entry under `mcp`. Secrets live in the MCP subprocess's
// `environment` block so they never reach the model backend opencode routes to.
func (l *Launcher) writeHeadlessOpencodeMCPConfig(slug string) (string, error) {
	wuphfBinary, err := headlessOpencodeExecutablePath()
	if err != nil {
		return "", fmt.Errorf("resolve wuphf binary: %w", err)
	}
	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — the base opencode
	// config (~/.config/opencode/opencode.json) is a user-global read; the
	// per-agent write path uses runtimeHome below.
	//
	// os.UserHomeDir failure is non-fatal for the base config read: if HOME is
	// unset the base path is simply skipped and the agent config gets a minimal
	// overlay. Only the write path (runtimeHome) must be non-empty.
	var baseConfigPath string
	if userHome, herr := os.UserHomeDir(); herr == nil && strings.TrimSpace(userHome) != "" {
		baseConfigPath = filepath.Join(userHome, ".config", "opencode", "opencode.json")
	}
	runtimeHome := config.RuntimeHomeDir()
	if runtimeHome == "" {
		if userHome, herr := os.UserHomeDir(); herr == nil && strings.TrimSpace(userHome) != "" {
			runtimeHome = userHome
		}
	}
	if runtimeHome == "" {
		return "", fmt.Errorf("resolve runtime home for opencode config: WUPHF_RUNTIME_HOME unset and os.UserHomeDir failed")
	}
	configPath := headlessOpencodeAgentConfigPath(runtimeHome, slug)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir opencode config dir: %w", err)
	}

	merged := map[string]any{}
	if raw, err := os.ReadFile(baseConfigPath); err == nil && len(raw) > 0 {
		// Best-effort: if the existing file isn't valid JSON, fall back to
		// writing a minimal overlay so wuphf keeps booting — but surface the
		// parse error in the agent log so the operator can see they have a
		// malformed base config silently dropping their `model`/`provider`
		// blocks from every per-agent merge. (#313 bonus #1)
		if uerr := json.Unmarshal(raw, &merged); uerr != nil {
			merged = map[string]any{}
			appendHeadlessCodexLog(slug, fmt.Sprintf("opencode_base-config-parse-failed: %s: %s — per-agent config will not inherit user model/provider/MCP keys until this is fixed", baseConfigPath, uerr.Error()))
		}
	}

	mcp, _ := merged["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	mcp["wuphf-office"] = l.buildHeadlessOpencodeMCPEntry(wuphfBinary, slug)
	merged["mcp"] = mcp
	if _, ok := merged["$schema"]; !ok {
		merged["$schema"] = "https://opencode.ai/config.json"
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal opencode config: %w", err)
	}
	// os.Rename on the same filesystem is atomic (POSIX), so readers always see
	// either the old complete file or the new complete file — never a half-write.
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".opencode-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp opencode config: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("chmod temp opencode config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write temp opencode config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp opencode config: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("install opencode config: %w", err)
	}
	return configPath, nil
}

func headlessOpencodeAgentConfigPath(home string, slug string) string {
	return filepath.Join(home, ".config", "opencode", "opencode."+safeHeadlessOpencodeConfigSlug(slug)+".json")
}

func safeHeadlessOpencodeConfigSlug(slug string) string {
	slug = normalizeActorSlug(slug)
	if slug == "" {
		slug = "agent"
	}
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "agent"
	}
	return b.String()
}

// buildHeadlessOpencodeMCPEntry constructs the `mcp.wuphf-office` block for
// opencode.json. The WUPHF-managed secrets (broker token, identity, Nex API
// key) live inside the MCP `environment` map — opencode forwards these only
// to the MCP subprocess, not to the model backend. This scoping is the
// security boundary that makes it safe to add a third-party provider like
// opencode, which can route to arbitrary user-configured endpoints.
func (l *Launcher) buildHeadlessOpencodeMCPEntry(wuphfBinary string, slug string) map[string]any {
	entry := map[string]any{
		"type":    "local",
		"command": []string{wuphfBinary, "mcp-team"},
		"enabled": true,
	}
	envMap := map[string]string{
		"WUPHF_AGENT_SLUG":      slug,
		"WUPHF_BROKER_BASE_URL": l.BrokerBaseURL(),
	}
	if l != nil && l.broker != nil {
		envMap["WUPHF_BROKER_TOKEN"] = l.broker.Token()
	}
	if config.ResolveNoNex() {
		envMap["WUPHF_NO_NEX"] = "1"
	}
	if l != nil && l.isOneOnOne() {
		envMap["WUPHF_ONE_ON_ONE"] = "1"
		if v := strings.TrimSpace(l.oneOnOneAgent()); v != "" {
			envMap["WUPHF_ONE_ON_ONE_AGENT"] = v
		}
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		envMap["ONE_SECRET"] = secret
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		envMap["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			envMap["ONE_IDENTITY_TYPE"] = identityType
		}
	}
	if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
		envMap["WUPHF_API_KEY"] = apiKey
		envMap["NEX_API_KEY"] = apiKey
	}
	entry["environment"] = envMap
	return entry
}
