package action

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

type Registry struct {
	providers []Provider
}

func NewRegistryFromEnv() *Registry {
	return &Registry{
		providers: []Provider{
			NewComposioFromEnv(),
			NewOneCLIFromEnv(),
		},
	}
}

func (r *Registry) ProviderFor(cap Capability) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("action registry is not configured")
	}
	explicit := strings.TrimSpace(config.ResolveActionProvider())
	if explicit != "" && !strings.EqualFold(explicit, "auto") {
		for _, p := range r.providers {
			if !strings.EqualFold(p.Name(), explicit) {
				continue
			}
			if !p.Supports(cap) {
				return nil, fmt.Errorf("%s does not support %s", p.Name(), cap)
			}
			if !p.Configured() {
				return nil, fmt.Errorf("%s is selected for %s but is not configured", p.Name(), cap)
			}
			return p, nil
		}
		return nil, fmt.Errorf("unknown action provider %q", explicit)
	}

	order := preferredProvidersFor(cap)
	for _, name := range order {
		for _, p := range r.providers {
			if p.Name() == name && p.Supports(cap) && p.Configured() {
				return p, nil
			}
		}
	}

	for _, p := range r.providers {
		if p.Supports(cap) {
			if p.Configured() {
				return p, nil
			}
		}
	}

	var supported []string
	for _, p := range r.providers {
		if p.Supports(cap) {
			supported = append(supported, p.Name())
		}
	}
	if len(supported) == 0 {
		return nil, fmt.Errorf("no provider supports %s", cap)
	}
	return nil, fmt.Errorf("no configured provider available for %s; supported providers: %s", cap, strings.Join(supported, ", "))
}

func (r *Registry) ProviderNamed(name string, cap Capability) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("action registry is not configured")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return r.ProviderFor(cap)
	}
	for _, p := range r.providers {
		if !strings.EqualFold(p.Name(), name) {
			continue
		}
		if !p.Supports(cap) {
			return nil, fmt.Errorf("%s does not support %s", p.Name(), cap)
		}
		if !p.Configured() {
			return nil, fmt.Errorf("%s is selected for %s but is not configured", p.Name(), cap)
		}
		return p, nil
	}
	return nil, fmt.Errorf("unknown action provider %q", name)
}

// preferredProvidersFor returns providers in the order they should be tried
// for a capability. Composio wins by default: it is the canonical integration
// plane the product surfaces (the Integrations app, OAuth, the catalog, and the
// connection resolver all speak Composio). The connection a human establishes in
// the Integrations app is a Composio connection, so execution must route there
// too — trying One first let a Composio-connected action misroute to a provider
// the human never connected. One CLI remains a fallback for the local-first
// power user, and a specific provider can still be pinned via
// `/config set action_provider`.
func preferredProvidersFor(cap Capability) []string {
	switch cap {
	case CapabilityConnections,
		CapabilityActionSearch,
		CapabilityActionKnowledge,
		CapabilityActionExecute,
		CapabilityRelayList,
		CapabilityRelayEventTypes,
		CapabilityRelayCreate,
		CapabilityRelayActivate:
		return []string{"composio", "one"}
	case CapabilityWorkflowCreate,
		CapabilityWorkflowExecute,
		CapabilityWorkflowRuns,
		CapabilityRelayEvents,
		CapabilityRelayEvent:
		return []string{"composio", "one"}
	default:
		return []string{"composio", "one"}
	}
}
