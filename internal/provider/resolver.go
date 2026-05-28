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
//  1. If the global runtime is unlocked (Config.LLMProviderUnlocked) AND a
//     non-empty global provider is configured, the install-wide kind wins
//     over every per-agent binding. This is the explicit "stamp this runtime
//     onto every agent" gesture from the Settings panel.
//  2. Otherwise, per-agent kind from kindResolver (if non-nil and non-empty).
//  3. Otherwise, install-wide kind from config.ResolveLLMProvider (the
//     default-for-new-agents fallback when an agent has no binding).
//  4. Claude Code (default fallback for unknown / unregistered Kinds).
//
// kindResolver is what makes per-agent ProviderBindings (an Ollama agent
// alongside Claude agents in the same team) actually take effect on the
// streaming dispatch path. Pass nil to use only the install-wide default.
//
// Config is re-read on each call so runtime provider changes (e.g., a /provider
// switch from the TUI, an unlock/lock toggle in Settings) take effect on the
// next agent turn without restart.
//
// Gateway-controlled bindings (OpenClaw, Hermes) are not affected by step (1):
// kindResolver still wins for those agents because the gateway needs the
// matching transport to talk to the imported runtime. The override only
// stamps onto agents whose per-agent kind is itself non-gateway (or empty).
func DefaultStreamFnResolver(client *api.Client, kindResolver ProviderKindResolver) agent.StreamFnResolver {
	// TODO: thread client into provider-specific StreamFn factories (see issue #186).
	return func(agentSlug string) agent.StreamFn {
		globalKind, override := config.ResolveLLMProviderOverride()
		var perAgentKind string
		if kindResolver != nil {
			perAgentKind = kindResolver(agentSlug)
		}
		// Step 1: unlocked global override wins, but never displaces a
		// gateway binding (the gateway transport is load-bearing for
		// agents imported through OpenClaw / Hermes).
		if override && !IsGatewayKind(perAgentKind) {
			if e := Lookup(globalKind); e != nil {
				return e.StreamFn(agentSlug)
			}
		}
		// Steps 2-3: per-agent first, then global default.
		kind := perAgentKind
		if kind == "" {
			kind = globalKind
		}
		if e := Lookup(kind); e != nil {
			return e.StreamFn(agentSlug)
		}
		return CreateClaudeCodeStreamFn(agentSlug)
	}
}
