// Package provider implements LLM backend providers for agents.
package provider

import "github.com/nex-crm/wuphf/internal/agent"

// Provider creates a StreamFn for a given agent slug.
type Provider interface {
	CreateStreamFn(agentSlug string) agent.StreamFn
}
