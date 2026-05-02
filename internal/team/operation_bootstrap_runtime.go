package team

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/operations"
)

func loadOperationRuntimeConnections(ctx context.Context) ([]action.Connection, string) {
	registry := action.NewRegistryFromEnv()
	provider, err := registry.ProviderFor(action.CapabilityConnections)
	if err != nil {
		return nil, ""
	}
	result, err := provider.ListConnections(ctx, action.ListConnectionsOptions{Limit: 200})
	if err != nil {
		return nil, provider.Name()
	}
	return result.Connections, provider.Name()
}

func operationRuntimeIntegrationsFromConnections(runtimeConnections []action.Connection) []operations.RuntimeIntegration {
	integrations := make([]operations.RuntimeIntegration, 0, len(runtimeConnections))
	seen := make(map[string]struct{}, len(runtimeConnections))
	for _, conn := range runtimeConnections {
		integration := strings.TrimSpace(conn.Platform)
		if integration == "" {
			continue
		}
		key := normalizeOperationIntegrationKey(integration)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		displayName := operationFirstNonEmpty(strings.TrimSpace(conn.Name), titleCaser.String(integration))
		integrations = append(integrations, operations.RuntimeIntegration{
			Name:        displayName,
			Provider:    integration,
			Status:      strings.TrimSpace(conn.State),
			Purpose:     fmt.Sprintf("Connected %s account available for workflow planning.", integration),
			Description: fmt.Sprintf("Connected account %q.", displayName),
			Connected:   isOperationConnectionConnected(conn),
		})
	}
	sort.SliceStable(integrations, func(i, j int) bool { return integrations[i].Provider < integrations[j].Provider })
	return integrations
}

func operationRuntimeCapabilitiesFromConnections(runtimeConnections []action.Connection, providerName string) []operations.RuntimeCapability {
	capabilities := []operations.RuntimeCapability{
		{Key: "bootstrap", Name: "Bootstrap synthesis", Category: "planner", Lifecycle: "active", Detail: "Turn a blank directive into an operation blueprint."},
		{Key: "approval", Name: "Human approval gate", Category: "policy", Lifecycle: "active", Detail: "Block high-risk actions until a human approves them."},
	}
	if providerName != "" {
		capabilities = append(capabilities, operations.RuntimeCapability{
			Key:       operationSlug(providerName + "-connections"),
			Name:      titleCaser.String(strings.TrimSpace(providerName)) + " connections",
			Category:  "integration",
			Lifecycle: "active",
			Detail:    "Discover connected accounts and map them into workflows.",
		})
	}
	type platformCapability struct {
		name  string
		state string
	}
	platforms := make(map[string]platformCapability, len(runtimeConnections))
	for _, conn := range runtimeConnections {
		integration := strings.TrimSpace(conn.Platform)
		if integration == "" {
			continue
		}
		key := normalizeOperationIntegrationKey(integration)
		if _, ok := platforms[key]; ok {
			continue
		}
		platforms[key] = platformCapability{
			name:  integration,
			state: strings.TrimSpace(conn.State),
		}
	}
	keys := make([]string, 0, len(platforms))
	for key := range platforms {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		platform := platforms[key]
		capabilities = append(capabilities, operations.RuntimeCapability{
			Key:       operationSlug(platform.name),
			Name:      titleCaser.String(platform.name),
			Category:  "integration",
			Lifecycle: platform.state,
			Detail:    fmt.Sprintf("Use the connected %s account when the workflow needs it.", platform.name),
		})
	}
	return capabilities
}

func isOperationConnectionConnected(conn action.Connection) bool {
	switch strings.ToLower(strings.TrimSpace(conn.State)) {
	case "connected", "active", "operational", "ready", "authorized":
		return true
	default:
		return false
	}
}
