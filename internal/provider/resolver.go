package provider

import (
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// DefaultStreamFnResolver returns a StreamFnResolver that picks a provider's
// StreamFn factory based on the active LLM provider (config.ResolveLLMProvider).
// Resolution goes through the Registry (registry.go), so any provider that
// registers via init() — including future Ollama/vLLM/exo backends — is
// reachable here without editing this file.
//
// Config is re-read on each call so runtime provider changes (e.g., a /provider
// switch from the TUI) take effect on the next agent turn.
//
// Unknown or unregistered kinds fall back to claude-code, the most capable
// runtime for multi-turn orchestration.
func DefaultStreamFnResolver(client *api.Client) agent.StreamFnResolver {
	return func(agentSlug string) agent.StreamFn {
		kind := config.ResolveLLMProvider("")
		if e := Lookup(kind); e != nil {
			return e.StreamFn(agentSlug)
		}
		return CreateClaudeCodeStreamFn(agentSlug)
	}
}
