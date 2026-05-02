package team

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/operations"
)

func buildOperationAutomation(blueprint operations.Blueprint, providerName string) []operationAutomationModule {
	connectionMode := "Stub first"
	connectionStatus := "stub"
	connectionFooter := "Waiting on connected external systems and human approvals."
	if providerName != "" {
		connectionMode = "Live-capable"
		connectionStatus = "build_now"
		connectionFooter = fmt.Sprintf("Connected systems are inspected via %s; keep mutating actions behind approval.", titleCaser.String(providerName))
	}
	replacements := map[string]string{
		"connection_mode":     connectionMode,
		"connection_status":   connectionStatus,
		"connection_footer":   connectionFooter,
		"approval_boundaries": operationAutomationApprovalSummary(blueprint.ApprovalRules),
	}
	out := make([]operationAutomationModule, 0, len(blueprint.Automation))
	for _, module := range blueprint.Automation {
		out = append(out, operationAutomationModule{
			ID:     operationRenderTemplateString(module.ID, replacements),
			Kicker: operationRenderTemplateString(module.Kicker, replacements),
			Title:  operationRenderTemplateString(module.Title, replacements),
			Copy:   operationRenderTemplateString(module.Copy, replacements),
			Status: operationRenderTemplateString(module.Status, replacements),
			Footer: operationRenderTemplateString(module.Footer, replacements),
		})
	}
	return out
}

func buildOperationIntegrationStubs(cards []operationConnectionCard) []operationIntegrationStub {
	out := make([]operationIntegrationStub, 0, len(cards))
	for _, card := range cards {
		status := "stub"
		switch card.State {
		case "connected", "smoke_tested":
			status = "connected"
		case "ready_for_auth":
			status = "ready_for_auth"
		}
		out = append(out, operationIntegrationStub{
			Name:   card.Name,
			Status: status,
			Detail: operationFirstNonEmpty(card.Purpose, card.Blocker),
		})
	}
	return out
}

type operationIntegrationBlueprint struct {
	Name        string
	Integration string
	Owner       string
	Priority    string
	Purpose     string
	SmokeTest   string
	Blocker     string
}

func buildOperationConnectionCards(blueprint operations.Blueprint, runtimeConnections []action.Connection, providerName string) []operationConnectionCard {
	blueprints := make([]operationIntegrationBlueprint, 0, len(blueprint.Connections))
	for _, item := range blueprint.Connections {
		blueprints = append(blueprints, operationIntegrationBlueprint{
			Name:        item.Name,
			Integration: item.Integration,
			Owner:       item.Owner,
			Priority:    item.Priority,
			Purpose:     item.Purpose,
			SmokeTest:   item.SmokeTest,
			Blocker:     item.Blocker,
		})
	}
	sort.SliceStable(blueprints, func(i, j int) bool {
		return blueprints[i].Priority < blueprints[j].Priority
	})

	connectionMap := make(map[string]action.Connection, len(runtimeConnections))
	for _, connection := range runtimeConnections {
		key := normalizeOperationIntegrationKey(connection.Platform)
		if key == "" {
			continue
		}
		if existing, ok := connectionMap[key]; ok && !operationConnectionIsBetter(connection, existing) {
			continue
		}
		connectionMap[key] = connection
	}

	out := make([]operationConnectionCard, 0, len(blueprints))
	for _, blueprint := range blueprints {
		card := operationConnectionCard{
			Name:        blueprint.Name,
			Integration: blueprint.Integration,
			Owner:       blueprint.Owner,
			Priority:    blueprint.Priority,
			Mode:        "approval_required",
			State:       "stubbed",
			Purpose:     blueprint.Purpose,
			SmokeTest:   blueprint.SmokeTest,
			Blocker:     blueprint.Blocker,
		}
		if providerName != "" {
			card.State = "ready_for_auth"
		}
		if live, ok := connectionMap[normalizeOperationIntegrationKey(blueprint.Integration)]; ok {
			card.State = "connected"
			card.Mode = "live_capable"
			card.Blocker = fmt.Sprintf("Connection %q is available via %s. Live mutations still require human approval.", live.Key, providerName)
		}
		out = append(out, card)
	}
	return out
}

func operationConnectionIsBetter(next, current action.Connection) bool {
	return operationConnectionStateRank(next.State) > operationConnectionStateRank(current.State)
}

func operationConnectionStateRank(state string) int {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "operational", "active", "connected":
		return 3
	case "ready", "authorized":
		return 2
	default:
		return 1
	}
}
