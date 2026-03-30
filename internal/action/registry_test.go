package action

import (
	"context"
	"testing"
)

type registryStubProvider struct {
	name       string
	configured bool
	supports   map[Capability]bool
}

func (p registryStubProvider) Name() string                 { return p.name }
func (p registryStubProvider) Configured() bool             { return p.configured }
func (p registryStubProvider) Supports(cap Capability) bool { return p.supports[cap] }
func (p registryStubProvider) Guide(context.Context, string) (GuideResult, error) {
	return GuideResult{}, nil
}
func (p registryStubProvider) ListConnections(context.Context, ListConnectionsOptions) (ConnectionsResult, error) {
	return ConnectionsResult{}, nil
}
func (p registryStubProvider) SearchActions(context.Context, string, string, string) (ActionSearchResult, error) {
	return ActionSearchResult{}, nil
}
func (p registryStubProvider) ActionKnowledge(context.Context, string, string) (KnowledgeResult, error) {
	return KnowledgeResult{}, nil
}
func (p registryStubProvider) ExecuteAction(context.Context, ExecuteRequest) (ExecuteResult, error) {
	return ExecuteResult{}, nil
}
func (p registryStubProvider) CreateWorkflow(context.Context, WorkflowCreateRequest) (WorkflowCreateResult, error) {
	return WorkflowCreateResult{}, nil
}
func (p registryStubProvider) ExecuteWorkflow(context.Context, WorkflowExecuteRequest) (WorkflowExecuteResult, error) {
	return WorkflowExecuteResult{}, nil
}
func (p registryStubProvider) ListWorkflowRuns(context.Context, string) (WorkflowRunsResult, error) {
	return WorkflowRunsResult{}, nil
}
func (p registryStubProvider) ListRelays(context.Context, ListRelaysOptions) (RelayListResult, error) {
	return RelayListResult{}, nil
}
func (p registryStubProvider) RelayEventTypes(context.Context, string) (RelayEventTypesResult, error) {
	return RelayEventTypesResult{}, nil
}
func (p registryStubProvider) CreateRelay(context.Context, RelayCreateRequest) (RelayResult, error) {
	return RelayResult{}, nil
}
func (p registryStubProvider) ActivateRelay(context.Context, RelayActivateRequest) (RelayResult, error) {
	return RelayResult{}, nil
}
func (p registryStubProvider) ListRelayEvents(context.Context, RelayEventsOptions) (RelayEventsResult, error) {
	return RelayEventsResult{}, nil
}
func (p registryStubProvider) GetRelayEvent(context.Context, string) (RelayEventDetail, error) {
	return RelayEventDetail{}, nil
}

func TestRegistryPrefersComposioForActionsInAutoMode(t *testing.T) {
	t.Setenv("WUPHF_ACTION_PROVIDER", "auto")
	registry := &Registry{
		providers: []Provider{
			registryStubProvider{
				name:       "composio",
				configured: true,
				supports: map[Capability]bool{
					CapabilityActionExecute: true,
				},
			},
			registryStubProvider{
				name:       "one",
				configured: true,
				supports: map[Capability]bool{
					CapabilityActionExecute: true,
				},
			},
		},
	}
	provider, err := registry.ProviderFor(CapabilityActionExecute)
	if err != nil {
		t.Fatalf("provider for action execute: %v", err)
	}
	if provider.Name() != "composio" {
		t.Fatalf("expected composio, got %s", provider.Name())
	}
}

func TestRegistryPrefersOneForWorkflowsInAutoMode(t *testing.T) {
	t.Setenv("WUPHF_ACTION_PROVIDER", "auto")
	registry := &Registry{
		providers: []Provider{
			registryStubProvider{
				name:       "composio",
				configured: true,
				supports: map[Capability]bool{
					CapabilityActionExecute: true,
				},
			},
			registryStubProvider{
				name:       "one",
				configured: true,
				supports: map[Capability]bool{
					CapabilityWorkflowExecute: true,
				},
			},
		},
	}
	provider, err := registry.ProviderFor(CapabilityWorkflowExecute)
	if err != nil {
		t.Fatalf("provider for workflow execute: %v", err)
	}
	if provider.Name() != "one" {
		t.Fatalf("expected one, got %s", provider.Name())
	}
}
