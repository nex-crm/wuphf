package action

import "testing"

// TestPreferredProvidersComposioFirst pins the routing fix: Composio must be
// tried before One for every capability. The human connects integrations in the
// Composio-backed Integrations app, so execution has to route to Composio too —
// the old One-first order let a Composio-connected action misroute to a provider
// the human never connected.
func TestPreferredProvidersComposioFirst(t *testing.T) {
	caps := []Capability{
		CapabilityConnections,
		CapabilityActionExecute,
		CapabilityActionSearch,
		CapabilityWorkflowCreate,
		CapabilityWorkflowExecute,
		CapabilityRelayList,
		Capability("some-unmapped-capability"),
	}
	for _, c := range caps {
		order := preferredProvidersFor(c)
		if len(order) == 0 || order[0] != "composio" {
			t.Errorf("cap %q: preferred order %v; composio must be first", c, order)
		}
	}
}
