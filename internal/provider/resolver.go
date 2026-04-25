package provider

import (
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// ProviderKindResolver maps an agent slug to its registered provider Kind.
// Implementations consult per-agent state (e.g., broker.MemberProviderKind)
// and return "" when the agent has no explicit binding so the resolver can
// fall back to the install-wide default.
type ProviderKindResolver func(agentSlug string) string

// DefaultStreamFnResolver returns a StreamFnResolver that picks a provider's
// StreamFn factory by Kind. Resolution order:
//
//  1. Per-agent kind from kindResolver (if non-nil and returns non-empty)
//  2. Install-wide kind from config.ResolveLLMProvider
//  3. Claude Code (default fallback for unknown / unregistered Kinds)
//
// kindResolver is what makes per-agent ProviderBindings (an Ollama agent
// alongside Claude agents in the same team) actually take effect on the
// streaming dispatch path. Pass nil to use only the install-wide default.
//
// Config is re-read on each call so runtime provider changes (e.g., a /provider
// switch from the TUI) take effect on the next agent turn without restart.
func DefaultStreamFnResolver(client *api.Client, kindResolver ProviderKindResolver) agent.StreamFnResolver {
	// TODO: thread client into provider-specific StreamFn factories (see issue #186).
	return func(agentSlug string) agent.StreamFn {
		var kind string
		if kindResolver != nil {
			kind = kindResolver(agentSlug)
		}
		if kind == "" {
			kind = config.ResolveLLMProvider("")
		}
		if e := Lookup(kind); e != nil {
			return e.StreamFn(agentSlug)
		}
		return CreateClaudeCodeStreamFn(agentSlug)
	}
}
