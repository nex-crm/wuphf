package team

import (
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/operations"
)

func buildOperationBootstrapPackage(selected operationPackFile, blueprint operations.Blueprint, backlog operationBacklogDoc, monetization operationMonetizationDoc, runtimeConnections []action.Connection, providerName string, profile operationCompanyProfile) operationBootstrapPackage {
	pack := selected.Doc
	drafts := buildOperationWorkflowDrafts(blueprint)
	smokeTests := buildOperationSmokeTests(blueprint)
	connections := buildOperationConnectionCards(blueprint, runtimeConnections, providerName)
	valueCapturePlan := buildOperationValueCapturePlan(blueprint, pack)
	workstreamSeed := buildOperationWorkstreamSeed(blueprint, pack, backlog)
	sourcePath := filepath.ToSlash(selected.Path)
	if strings.TrimSpace(sourcePath) == "" {
		sourcePath = "synthesized"
	}
	blueprintID := operationFirstNonEmpty(blueprint.ID, profile.BlueprintID, pack.Metadata.ID, operationSlug(operationFirstNonEmpty(profile.Name, blueprint.Name, "synthesized-operation")))
	blueprintLabel := operationFirstNonEmpty(profile.Name, blueprint.Name, pack.Channel.BrandName, blueprint.Description, "Synthesized operation")
	return operationBootstrapPackage{
		BlueprintID:        blueprintID,
		BlueprintLabel:     blueprintLabel,
		PackID:             blueprintID,
		PackLabel:          blueprintLabel,
		SourcePath:         sourcePath,
		ConnectionProvider: providerName,
		Blueprint:          blueprint,
		BootstrapConfig:    buildOperationBootstrapConfig(blueprint, pack, profile),
		Starter:            buildOperationStarterTemplate(blueprint, pack, backlog, profile),
		Automation:         buildOperationAutomation(blueprint, providerName),
		Integrations:       buildOperationIntegrationStubs(connections),
		Connections:        connections,
		SmokeTests:         smokeTests,
		WorkflowDrafts:     drafts,
		ValueCapturePlan:   valueCapturePlan,
		MonetizationLadder: valueCapturePlan,
		WorkstreamSeed:     workstreamSeed,
		QueueSeed:          workstreamSeed,
		Offers:             buildOperationOffers(blueprint, pack, monetization, profile),
	}
}

func buildOperationSynthesizedBootstrapPackage(profile operationCompanyProfile, runtimeConnections []action.Connection, providerName string) operationBootstrapPackage {
	normalizedProvider := strings.TrimSpace(providerName)
	blueprint := operations.SynthesizeBlueprint(operations.SynthesisInput{
		Directive: operationFirstNonEmpty(profile.Goals, profile.Description, profile.Name, "stand up a new operation"),
		Profile: operations.CompanyProfile{
			Name:        strings.TrimSpace(profile.Name),
			Industry:    strings.TrimSpace(profile.Priority),
			Description: strings.TrimSpace(profile.Description),
			Audience:    strings.TrimSpace(profile.Size),
			Offer:       strings.TrimSpace(profile.Goals),
			Notes:       []string{strings.TrimSpace(profile.BlueprintID)},
		},
		Integrations: operationRuntimeIntegrationsFromConnections(runtimeConnections),
		Capabilities: operationRuntimeCapabilitiesFromConnections(runtimeConnections, normalizedProvider),
	})
	return buildOperationBootstrapPackage(operationPackFile{}, blueprint, operationBacklogDoc{}, operationMonetizationDoc{}, runtimeConnections, normalizedProvider, profile)
}
