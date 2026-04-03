package workflow

import (
	"strings"
	"testing"
)

func TestGenerationPrompt_ContainsSchema(t *testing.T) {
	prompt := GenerationPrompt()

	schemaElements := []string{
		`"id"`,
		`"title"`,
		`"steps"`,
		`"type"`,
		`"actions"`,
		`"transition"`,
		`"execute"`,
		`"provider"`,
		`"dataRef"`,
		`"allowLoop"`,
		`StepSpec`,
		`ActionSpec`,
		`ExecuteSpec`,
		`DisplaySpec`,
		`DataSource`,
	}
	for _, elem := range schemaElements {
		if !strings.Contains(prompt, elem) {
			t.Errorf("prompt missing schema element %q", elem)
		}
	}
}

func TestGenerationPrompt_ContainsExamples(t *testing.T) {
	prompt := GenerationPrompt()

	// Should contain both example workflow IDs.
	if !strings.Contains(prompt, "email-triage") {
		t.Error("prompt missing email-triage example")
	}
	if !strings.Contains(prompt, "deploy-check") {
		t.Error("prompt missing deploy-check example")
	}

	// Should contain the 5 interaction type names.
	types := []string{"select", "confirm", "edit", "submit", "run"}
	for _, typ := range types {
		if !strings.Contains(prompt, `"`+typ+`"`) {
			t.Errorf("prompt missing interaction type %q", typ)
		}
	}

	// Should contain key agent instructions.
	if !strings.Contains(prompt, "Output ONLY valid JSON") {
		t.Error("prompt missing output instruction")
	}
	if !strings.Contains(prompt, "No markdown fencing") {
		t.Error("prompt missing markdown fencing instruction")
	}
}

func TestValidateAndFix_ValidSpec(t *testing.T) {
	validJSON := `{
		"id": "test-wf",
		"title": "Test Workflow",
		"steps": [
			{
				"id": "step1",
				"type": "confirm",
				"prompt": "Continue?",
				"actions": [
					{"key": "y", "label": "Yes", "transition": "done"},
					{"key": "n", "label": "No", "transition": "done"}
				]
			}
		]
	}`

	spec, err := ValidateAndFix(validJSON)
	if err != nil {
		t.Fatalf("expected valid spec to pass: %v", err)
	}
	if spec == nil {
		t.Fatal("expected non-nil spec")
	}
	if spec.ID != "test-wf" {
		t.Errorf("expected id 'test-wf', got %q", spec.ID)
	}
	if len(spec.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(spec.Steps))
	}
}

func TestValidateAndFix_InvalidJSON(t *testing.T) {
	_, err := ValidateAndFix(`{not valid json}`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "JSON parse error") {
		t.Errorf("expected JSON parse error, got: %v", err)
	}
}

func TestValidateAndFix_InvalidSpec(t *testing.T) {
	// Valid JSON but invalid workflow spec (missing id).
	invalidSpec := `{
		"id": "",
		"title": "Bad",
		"steps": [{"id": "s1", "type": "select"}]
	}`

	_, err := ValidateAndFix(invalidSpec)
	if err == nil {
		t.Fatal("expected error for invalid spec")
	}
	if !strings.Contains(err.Error(), "validation error") {
		t.Errorf("expected validation error, got: %v", err)
	}
}
