package provider

import (
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// DefaultStreamFnResolver returns a StreamFnResolver that picks the right provider
// based on the user's config (llm_provider, gemini_api_key).
// Config is re-read on each call so runtime provider changes take effect.
func DefaultStreamFnResolver(client *api.Client) agent.StreamFnResolver {
	return func(agentSlug string) agent.StreamFn {
		switch config.ResolveLLMProvider("") {
		case "codex":
			return CreateCodexCLIStreamFn(agentSlug)
		case "opencode":
			return CreateOpencodeCLIStreamFn(agentSlug)
		case "claude-code", "":
			// Default to Claude Code — most capable for multi-turn orchestration
			return CreateClaudeCodeStreamFn(agentSlug)
		default:
			return CreateClaudeCodeStreamFn(agentSlug)
		}
	}
}
