package workflow

import (
	"encoding/json"
	"testing"
)

// emailTriageSpec is the email triage workflow with branching transitions.
// Flow: list emails → select → approve (confirm send) OR reject (back to list) OR dismiss (submit, back to list).
const emailTriageSpec = `{
  "id": "email-triage",
  "title": "Email Triage",
  "steps": [
    {
      "id": "list",
      "type": "select",
      "prompt": "Select emails to triage:",
      "dataRef": "/emails",
      "display": {"component": "card", "props": {"showPriority": true}},
      "actions": [
        {"key": "a", "label": "Approve", "transition": "triage"},
        {"key": "r", "label": "Reject", "transition": "list"},
        {"key": "d", "label": "Dismiss", "transition": "dismiss"}
      ],
      "allowLoop": true
    },
    {
      "id": "triage",
      "type": "run",
      "prompt": "Triaging email...",
      "agent": "email-triage",
      "agentPrompt": "Triage this email and draft a reply.",
      "outputRef": "/triageResult",
      "actions": [
        {"key": "Enter", "label": "Continue", "transition": "confirm-send"}
      ]
    },
    {
      "id": "confirm-send",
      "type": "confirm",
      "prompt": "Send this reply?",
      "allowLoop": true,
      "display": {"component": "text", "dataRef": "/triageResult/draftReply"},
      "actions": [
        {"key": "y", "label": "Yes, send", "execute": {"provider": "composio", "action": "GMAIL_SEND_EMAIL", "data": {"ref": "/triageResult/draftReply"}}, "transition": "list"},
        {"key": "e", "label": "Edit first", "transition": "edit-reply"},
        {"key": "n", "label": "Cancel", "transition": "list"}
      ]
    },
    {
      "id": "edit-reply",
      "type": "edit",
      "prompt": "Edit the reply:",
      "dataRef": "/triageResult/draftReply",
      "display": {"component": "textfield", "props": {"label": "Reply", "multiline": true}},
      "actions": [
        {"key": "Enter", "label": "Done", "transition": "confirm-send"}
      ]
    },
    {
      "id": "dismiss",
      "type": "submit",
      "execute": {"provider": "broker", "method": "AddEmailDecision", "data": {"decision": "dismiss"}},
      "transition": "list"
    }
  ],
  "dataSources": [
    {"id": "emails", "provider": "composio", "action": "GMAIL_LIST_MESSAGES"}
  ]
}`

// deployCheckSpec is a linear deploy-check workflow.
// Flow: fetch status → show dashboard → confirm deploy → execute → done.
const deployCheckSpec = `{
  "id": "deploy-check",
  "title": "Deploy Check",
  "description": "Pre-deploy verification workflow",
  "steps": [
    {
      "id": "fetch-status",
      "type": "run",
      "prompt": "Checking deploy readiness...",
      "agent": "deploy-checker",
      "agentPrompt": "Check CI status, test results, and open PRs. Return a readiness report.",
      "outputRef": "/deployStatus",
      "actions": [
        {"key": "Enter", "label": "Continue", "transition": "review"}
      ]
    },
    {
      "id": "review",
      "type": "confirm",
      "prompt": "Deploy readiness report:",
      "display": {"component": "card", "props": {"title": "Deploy Status"}, "dataRef": "/deployStatus"},
      "actions": [
        {"key": "d", "label": "Deploy", "transition": "notify-team"},
        {"key": "a", "label": "Abort", "transition": "done"}
      ]
    },
    {
      "id": "notify-team",
      "type": "submit",
      "prompt": "Notifying team...",
      "execute": {"provider": "composio", "action": "SLACK_SEND_MESSAGE", "data": {"channel": "#deploys", "text": "Deploying to production..."}},
      "transition": "execute-deploy"
    },
    {
      "id": "execute-deploy",
      "type": "run",
      "prompt": "Deploying...",
      "agent": "deploy-runner",
      "agentPrompt": "Run the deploy pipeline. Report success or failure.",
      "outputRef": "/deployResult",
      "actions": [
        {"key": "Enter", "label": "Done", "transition": "done"}
      ]
    }
  ],
  "dataSources": []
}`

func TestParseEmailTriageSpec(t *testing.T) {
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(emailTriageSpec), &spec); err != nil {
		t.Fatalf("failed to parse email-triage spec: %v", err)
	}
	if spec.ID != "email-triage" {
		t.Errorf("expected id email-triage, got %s", spec.ID)
	}
	if len(spec.Steps) != 5 {
		t.Errorf("expected 5 steps, got %d", len(spec.Steps))
	}
	if len(spec.DataSources) != 1 {
		t.Errorf("expected 1 data source, got %d", len(spec.DataSources))
	}

	// Verify step types.
	expectedTypes := map[string]string{
		"list":         StepSelect,
		"triage":       StepRun,
		"confirm-send": StepConfirm,
		"edit-reply":   StepEdit,
		"dismiss":      StepSubmit,
	}
	for _, step := range spec.Steps {
		expected, ok := expectedTypes[step.ID]
		if !ok {
			t.Errorf("unexpected step id: %s", step.ID)
			continue
		}
		if step.Type != expected {
			t.Errorf("step %s: expected type %s, got %s", step.ID, expected, step.Type)
		}
	}

	// Verify branching: list → triage OR list (reject) OR dismiss.
	listStep := spec.Steps[0]
	if len(listStep.Actions) != 3 {
		t.Fatalf("list step expected 3 actions, got %d", len(listStep.Actions))
	}
	transitions := map[string]string{}
	for _, a := range listStep.Actions {
		transitions[a.Key] = a.Transition
	}
	if transitions["a"] != "triage" {
		t.Errorf("approve should transition to triage, got %s", transitions["a"])
	}
	if transitions["r"] != "list" {
		t.Errorf("reject should transition to list, got %s", transitions["r"])
	}
	if transitions["d"] != "dismiss" {
		t.Errorf("dismiss should transition to dismiss, got %s", transitions["d"])
	}
}

func TestParseDeployCheckSpec(t *testing.T) {
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(deployCheckSpec), &spec); err != nil {
		t.Fatalf("failed to parse deploy-check spec: %v", err)
	}
	if spec.ID != "deploy-check" {
		t.Errorf("expected id deploy-check, got %s", spec.ID)
	}
	if len(spec.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(spec.Steps))
	}

	// Verify linear flow: fetch-status → review → notify-team → execute-deploy → done.
	// Transitions can be in actions OR in step-level transition field.
	expectedTransitions := []struct {
		stepID string
		target string
	}{
		{"fetch-status", "review"},
		{"review", "notify-team"},
		{"notify-team", "execute-deploy"},
		{"execute-deploy", "done"},
	}
	for _, et := range expectedTransitions {
		var found bool
		for _, step := range spec.Steps {
			if step.ID != et.stepID {
				continue
			}
			// Check step-level transition.
			if step.Transition == et.target {
				found = true
				break
			}
			// Check action-level transitions.
			for _, a := range step.Actions {
				if a.Transition == et.target {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("step %s should have transition to %s", et.stepID, et.target)
		}
	}
}

func TestValidateEmailTriageSpec(t *testing.T) {
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(emailTriageSpec), &spec); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("validate email-triage spec: %v", err)
	}
}

func TestValidateDeployCheckSpec(t *testing.T) {
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(deployCheckSpec), &spec); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("validate deploy-check spec: %v", err)
	}
}

func TestValidateRejectsEmptySpec(t *testing.T) {
	if err := ValidateSpec(WorkflowSpec{}); err == nil {
		t.Fatal("expected error for empty spec")
	}
}

func TestValidateRejectsMissingStepID(t *testing.T) {
	spec := WorkflowSpec{
		ID:    "test",
		Steps: []StepSpec{{Type: StepSelect}},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for missing step id")
	}
}

func TestValidateRejectsDuplicateStepID(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepSelect},
			{ID: "step1", Type: StepConfirm},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for duplicate step id")
	}
}

func TestValidateRejectsInvalidStepType(t *testing.T) {
	spec := WorkflowSpec{
		ID:    "test",
		Steps: []StepSpec{{ID: "step1", Type: "unknown"}},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for invalid step type")
	}
}

func TestValidateRejectsInvalidTransitionTarget(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepSelect, Actions: []ActionSpec{
				{Key: "a", Label: "Go", Transition: "nonexistent"},
			}},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for invalid transition target")
	}
}

func TestValidateRejectsCircularTransition(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "a", Type: StepSelect, Actions: []ActionSpec{
				{Key: "x", Label: "Go", Transition: "b"},
			}},
			{ID: "b", Type: StepSelect, Actions: []ActionSpec{
				{Key: "x", Label: "Back", Transition: "a"},
			}},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for circular transition without allowLoop")
	}
}

func TestValidateAllowsLoopWithFlag(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "a", Type: StepSelect, AllowLoop: true, Actions: []ActionSpec{
				{Key: "x", Label: "Go", Transition: "b"},
			}},
			{ID: "b", Type: StepSelect, Actions: []ActionSpec{
				{Key: "x", Label: "Back", Transition: "a"},
			}},
		},
	}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("expected loop to be allowed: %v", err)
	}
}

func TestValidateRejectsBadDataRef(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepSelect, DataRef: "no-slash"},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for dataRef without leading /")
	}
}

func TestValidateRejectsRunWithoutTarget(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepRun},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for run step without agent/workflow/execute")
	}
}

func TestValidateRejectsDuplicateActionKey(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepSelect, Actions: []ActionSpec{
				{Key: "a", Label: "One"},
				{Key: "a", Label: "Two"},
			}},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for duplicate action key")
	}
}

func TestValidateRejectsUnknownProvider(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepSubmit, Execute: &ExecuteSpec{
				Provider: "unknown",
			}},
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestValidateTransitionToDoneIsValid(t *testing.T) {
	spec := WorkflowSpec{
		ID: "test",
		Steps: []StepSpec{
			{ID: "step1", Type: StepConfirm, Actions: []ActionSpec{
				{Key: "y", Label: "Yes", Transition: "done"},
			}},
		},
	}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("transition to 'done' should be valid: %v", err)
	}
}
