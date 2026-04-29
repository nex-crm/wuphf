package team

// mcp_config.go owns the MCP server-config assembly used to wire the
// office broker's MCP endpoint into each agent's claude session
// (PLAN.md §C13). buildMCPServerMap composes the server entry,
// ensureMCPConfig writes the team-wide config file (with optional
// per-agent override via ensureAgentMCPConfig), and agentMCPServers
// maps slugs to allowed-server slugs. codingAgentSlugs is the
// hardcoded "these slugs are coders, give them the broker MCP"
// allowlist.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// codingAgentSlugs lists agents that default to a minimal coding-focused MCP set.
// Task-level local_worktree isolation is driven by execution_mode, not this list.
var codingAgentSlugs = map[string]bool{
	"eng":       true,
	"fe":        true,
	"be":        true,
	"ai":        true,
	"qa":        true,
	"tech-lead": true,
}

// agentMCPServers returns the MCP server keys that a given agent should receive.
func agentMCPServers(slug string) []string {
	channel := strings.TrimSpace(os.Getenv("WUPHF_CHANNEL"))
	// DM mode: only wuphf-office (minimal tool set, no nex overhead)
	if strings.HasPrefix(channel, "dm-") {
		return []string{"wuphf-office"}
	}
	if codingAgentSlugs[slug] {
		return []string{"wuphf-office"}
	}
	return []string{"wuphf-office", "nex"}
}

// buildMCPServerMap constructs the full set of MCP server entries.
// This is the shared helper used by both ensureMCPConfig and ensureAgentMCPConfig.
func (l *Launcher) buildMCPServerMap() (map[string]any, error) {
	apiKey := config.ResolveAPIKey("")
	servers := map[string]any{}
	wuphfBinary, err := os.Executable()
	if err != nil {
		return nil, err
	}

	office := map[string]any{
		"command": wuphfBinary,
		"args":    []string{"mcp-team"},
	}
	servers["wuphf-office"] = office
	if oneSecret := strings.TrimSpace(config.ResolveOneSecret()); oneSecret != "" {
		office["env"] = map[string]string{
			"ONE_SECRET": oneSecret,
		}
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		env["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env["ONE_IDENTITY_TYPE"] = identityType
		}
		office["env"] = env
	}

	switch config.ResolveMemoryBackend("") {
	case config.MemoryBackendNex:
		if apiKey != "" {
			env, _ := office["env"].(map[string]string)
			if env == nil {
				env = map[string]string{}
			}
			env["WUPHF_API_KEY"] = apiKey
			env["NEX_API_KEY"] = apiKey
			office["env"] = env
		}
	case config.MemoryBackendGBrain:
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range gbrainMCPEnv() {
			env[key] = value
		}
		office["env"] = env
	}

	if memoryServer, err := resolvedMemoryMCPServer(); err != nil {
		return nil, err
	} else if memoryServer != nil && len(memoryServer.Env) > 0 {
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range memoryServer.Env {
			env[key] = value
		}
		office["env"] = env
	}

	if !config.ResolveNoNex() && apiKey != "" {
		if nexMCP, err := exec.LookPath("nex-mcp"); err == nil {
			servers["nex"] = map[string]any{
				"command": nexMCP,
				"env": map[string]string{
					"WUPHF_API_KEY": apiKey,
					"NEX_API_KEY":   apiKey,
				},
			}
		}
	}

	return servers, nil
}

func (l *Launcher) ensureMCPConfig() (string, error) {
	servers, err := l.buildMCPServerMap()
	if err != nil {
		return "", err
	}

	cfg := map[string]any{
		"mcpServers": servers,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	path := filepath.Join(os.TempDir(), "wuphf-team-mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ensureAgentMCPConfig writes a per-agent MCP config containing only the servers
// that agent needs. Returns the config file path.
func (l *Launcher) ensureAgentMCPConfig(slug string) (string, error) {
	allServers, err := l.buildMCPServerMap()
	if err != nil {
		return "", err
	}

	allowed := agentMCPServers(slug)
	filtered := make(map[string]any, len(allowed))
	for _, key := range allowed {
		if srv, ok := allServers[key]; ok {
			filtered[key] = srv
		}
	}

	cfg := map[string]any{
		"mcpServers": filtered,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	path := filepath.Join(os.TempDir(), "wuphf-mcp-"+slug+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
