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
		cfg, _ := config.Load()
		if config.ResolveNoNex() && cfg.LLMProvider == "nex-ask" {
			cfg.LLMProvider = "claude-code"
		}
		switch cfg.LLMProvider {
		case "codex":
			return CreateCodexCLIStreamFn(agentSlug)
		case "gemini":
			return CreateGeminiStreamFn(cfg.GeminiAPIKey)
		case "nex-ask":
			return CreateNexAskStreamFn(client)
		case "claude-code", "":
			// Default to Claude Code — most capable for multi-turn orchestration
			return CreateClaudeCodeStreamFn(agentSlug)
		default:
			return CreateClaudeCodeStreamFn(agentSlug)
		}
	}
}
