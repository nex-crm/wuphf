package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/operations"
)

func buildOperationWorkflowDrafts(blueprint operations.Blueprint) []operationWorkflowDraft {
	out := make([]operationWorkflowDraft, 0, len(blueprint.Workflows))
	for _, workflow := range blueprint.Workflows {
		out = append(out, operationWorkflowDraft{
			SkillName:         strings.TrimSpace(workflow.ID),
			Title:             strings.TrimSpace(workflow.Name),
			Trigger:           strings.TrimSpace(workflow.Trigger),
			Description:       strings.TrimSpace(workflow.Description),
			OwnedIntegrations: append([]string(nil), workflow.Integrations...),
			Schedule:          strings.TrimSpace(workflow.Schedule),
			Checklist:         append([]string(nil), workflow.Checklist...),
			Definition:        cloneOperationMap(workflow.Definition),
		})
	}
	return out
}

func buildOperationSmokeTests(blueprint operations.Blueprint) []operationSmokeTest {
	out := make([]operationSmokeTest, 0, len(blueprint.Workflows))
	for _, workflow := range blueprint.Workflows {
		if strings.TrimSpace(workflow.SmokeTest.Name) == "" {
			continue
		}
		out = append(out, operationSmokeTest{
			Name:         strings.TrimSpace(workflow.SmokeTest.Name),
			WorkflowKey:  operationWorkflowKeyFromTemplate(workflow),
			Mode:         operationFirstNonEmpty(strings.TrimSpace(workflow.SmokeTest.Mode), strings.TrimSpace(workflow.Mode)),
			Integrations: append([]string(nil), workflow.Integrations...),
			Proof:        strings.TrimSpace(workflow.SmokeTest.Proof),
			Inputs:       cloneOperationMap(workflow.SmokeTest.Inputs),
		})
	}
	return out
}

func operationWorkflowKey(draft operationWorkflowDraft) string {
	if value, ok := draft.Definition["key"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(draft.SkillName)
}

func operationWorkflowKeyFromTemplate(workflow operations.WorkflowTemplate) string {
	if value, ok := workflow.Definition["key"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(workflow.ID)
}
