package main

import (
	"strings"
	"testing"
)

func TestDetectWorkflowBlock(t *testing.T) {
	content := "Here's a workflow:\n\n```workflow\n" +
		`{"id":"deploy-check","title":"Deploy Check","steps":[{"id":"s1","title":"Verify"}]}` +
		"\n```\n\nRun it when ready."

	blocks, remaining := detectWorkflowBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 workflow block, got %d", len(blocks))
	}
	if blocks[0].Kind != "workflow" {
		t.Fatalf("expected kind=workflow, got %q", blocks[0].Kind)
	}

	spec, err := parseWorkflowSpec(blocks[0].JSON)
	if err != nil {
		t.Fatalf("parseWorkflowSpec: %v", err)
	}
	if spec.ID != "deploy-check" {
		t.Fatalf("expected id=deploy-check, got %q", spec.ID)
	}
	if spec.Title != "Deploy Check" {
		t.Fatalf("expected title=Deploy Check, got %q", spec.Title)
	}
	if len(spec.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(spec.Steps))
	}

	if !strings.Contains(remaining, "Here's a workflow") {
		t.Fatalf("expected remaining text to contain prose, got %q", remaining)
	}
	if !strings.Contains(remaining, "Run it when ready") {
		t.Fatalf("expected remaining text to contain trailing prose, got %q", remaining)
	}
	if strings.Contains(remaining, "```workflow") {
		t.Fatalf("expected workflow fence to be stripped, got %q", remaining)
	}
}

func TestDetectA2UIBlock(t *testing.T) {
	content := "Check this out:\n\n```a2ui\n" +
		`{"type":"card","props":{"title":"Status"},"children":[{"type":"text","props":{"content":"All good"}}]}` +
		"\n```"

	blocks, remaining := detectA2UIBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 a2ui block, got %d", len(blocks))
	}
	if blocks[0].Kind != "a2ui" {
		t.Fatalf("expected kind=a2ui, got %q", blocks[0].Kind)
	}

	comp, err := parseA2UIComponent(blocks[0].JSON)
	if err != nil {
		t.Fatalf("parseA2UIComponent: %v", err)
	}
	if comp.Type != "card" {
		t.Fatalf("expected type=card, got %q", comp.Type)
	}

	if !strings.Contains(remaining, "Check this out") {
		t.Fatalf("expected remaining text, got %q", remaining)
	}
	if strings.Contains(remaining, "```a2ui") {
		t.Fatalf("expected a2ui fence to be stripped, got %q", remaining)
	}
}

func TestDetectMultipleBlocks(t *testing.T) {
	content := "Intro\n\n```a2ui\n" +
		`{"type":"text","props":{"content":"A"}}` +
		"\n```\n\nMiddle\n\n```workflow\n" +
		`{"id":"w1","title":"W1","steps":[]}` +
		"\n```\n\nEnd"

	a2uiBlocks, afterA2UI := detectA2UIBlocks(content)
	if len(a2uiBlocks) != 1 {
		t.Fatalf("expected 1 a2ui block, got %d", len(a2uiBlocks))
	}

	wfBlocks, afterWF := detectWorkflowBlocks(afterA2UI)
	if len(wfBlocks) != 1 {
		t.Fatalf("expected 1 workflow block, got %d", len(wfBlocks))
	}

	if strings.Contains(afterWF, "```") {
		t.Fatalf("expected no fences remaining, got %q", afterWF)
	}
}

func TestRenderEmbeddedWorkflowCard(t *testing.T) {
	spec := &WorkflowSpec{
		ID:    "deploy-check",
		Title: "Deploy Check",
		Steps: []WorkflowStep{
			{ID: "s1", Title: "Build"},
			{ID: "s2", Title: "Test"},
			{ID: "s3", Title: "Deploy"},
		},
	}

	rendered := renderEmbeddedWorkflowCard(spec, 60, false)
	plain := stripANSI(rendered)

	if !strings.Contains(plain, "Deploy Check") {
		t.Fatalf("expected workflow title in rendered card, got %q", plain)
	}
	if !strings.Contains(plain, "3 steps") {
		t.Fatalf("expected step count in rendered card, got %q", plain)
	}
	if !strings.Contains(plain, "[Enter] Run") {
		t.Fatalf("expected action hint in rendered card, got %q", plain)
	}
	if !strings.Contains(plain, "workflow") {
		t.Fatalf("expected 'workflow' label, got %q", plain)
	}
}

func TestRenderEmbeddedWorkflowCardSingleStep(t *testing.T) {
	spec := &WorkflowSpec{
		ID:    "single",
		Title: "Quick Task",
		Steps: []WorkflowStep{{ID: "s1", Title: "Do it"}},
	}

	rendered := renderEmbeddedWorkflowCard(spec, 40, false)
	plain := stripANSI(rendered)

	if !strings.Contains(plain, "1 step") {
		t.Fatalf("expected singular 'step', got %q", plain)
	}
	if strings.Contains(plain, "1 steps") {
		t.Fatalf("expected singular not plural, got %q", plain)
	}
}

func TestRenderEmbeddedWorkflowCardFocused(t *testing.T) {
	spec := &WorkflowSpec{
		ID:    "focused",
		Title: "Focused Workflow",
		Steps: []WorkflowStep{{ID: "s1", Title: "Step"}},
	}

	// Both focused and unfocused should render without error and contain the title
	unfocused := renderEmbeddedWorkflowCard(spec, 50, false)
	focused := renderEmbeddedWorkflowCard(spec, 50, true)

	plain := stripANSI(unfocused)
	if !strings.Contains(plain, "Focused Workflow") {
		t.Fatalf("unfocused card missing title, got %q", plain)
	}
	plain = stripANSI(focused)
	if !strings.Contains(plain, "Focused Workflow") {
		t.Fatalf("focused card missing title, got %q", plain)
	}
}

func TestRenderA2UITable(t *testing.T) {
	content := "```a2ui\n" +
		`{"type":"table","props":{"rows":[["Name","Status"],["API","healthy"],["DB","degraded"]]}}` +
		"\n```"

	blocks, _ := detectA2UIBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	comp, err := parseA2UIComponent(blocks[0].JSON)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rendered := renderA2UIComponentLabeled(comp, 60)
	plain := stripANSI(rendered)

	if !strings.Contains(plain, "table") {
		t.Fatalf("expected type label 'table', got %q", plain)
	}
	if !strings.Contains(plain, "Name") || !strings.Contains(plain, "Status") {
		t.Fatalf("expected table headers, got %q", plain)
	}
	if !strings.Contains(plain, "API") || !strings.Contains(plain, "healthy") {
		t.Fatalf("expected table row data, got %q", plain)
	}
	if !strings.Contains(plain, "DB") || !strings.Contains(plain, "degraded") {
		t.Fatalf("expected table row data, got %q", plain)
	}

	// Verify alignment: all rows should have consistent column spacing
	lines := strings.Split(plain, "\n")
	var dataLines []string
	for _, line := range lines {
		if strings.Contains(line, "Name") || strings.Contains(line, "API") || strings.Contains(line, "DB") {
			dataLines = append(dataLines, line)
		}
	}
	if len(dataLines) < 2 {
		t.Fatalf("expected at least 2 data lines for alignment check, got %d", len(dataLines))
	}
}

func TestRenderA2UICard(t *testing.T) {
	content := "```a2ui\n" +
		`{"type":"card","props":{"title":"Deploy Status"},"children":[{"type":"text","props":{"content":"All services running"}}]}` +
		"\n```"

	blocks, _ := detectA2UIBlocks(content)
	comp, err := parseA2UIComponent(blocks[0].JSON)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rendered := renderA2UIComponentLabeled(comp, 50)
	plain := stripANSI(rendered)

	if !strings.Contains(plain, "card") {
		t.Fatalf("expected type label 'card', got %q", plain)
	}
	if !strings.Contains(plain, "Deploy Status") {
		t.Fatalf("expected card title, got %q", plain)
	}
	if !strings.Contains(plain, "All services running") {
		t.Fatalf("expected card body content, got %q", plain)
	}
}

func TestRenderA2UICardWithNestedList(t *testing.T) {
	content := "```a2ui\n" +
		`{"type":"card","props":{"title":"Tasks"},"children":[{"type":"list","props":{"items":["Design","Build","Ship"]}}]}` +
		"\n```"

	blocks, _ := detectA2UIBlocks(content)
	comp, err := parseA2UIComponent(blocks[0].JSON)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rendered := renderA2UIComponentLabeled(comp, 50)
	plain := stripANSI(rendered)

	if !strings.Contains(plain, "Tasks") {
		t.Fatalf("expected card title, got %q", plain)
	}
	for _, item := range []string{"Design", "Build", "Ship"} {
		if !strings.Contains(plain, item) {
			t.Fatalf("expected list item %q, got %q", item, plain)
		}
	}
}

func TestProcessMessageA2UIMixedContent(t *testing.T) {
	content := "Let me show you the plan:\n\n```workflow\n" +
		`{"id":"launch","title":"Launch Plan","steps":[{"id":"s1","title":"Build"},{"id":"s2","title":"Ship"}]}` +
		"\n```\n\nAnd here's the current status:\n\n```a2ui\n" +
		`{"type":"card","props":{"title":"Status"},"children":[{"type":"text","props":{"content":"In progress"}}]}` +
		"\n```"

	result := processMessageA2UI(content, 60)

	if !result.HasInteractive {
		t.Fatal("expected HasInteractive=true for message with workflow")
	}
	if len(result.WorkflowSpecs) != 1 {
		t.Fatalf("expected 1 workflow spec, got %d", len(result.WorkflowSpecs))
	}
	if result.WorkflowSpecs[0].Title != "Launch Plan" {
		t.Fatalf("expected workflow title, got %q", result.WorkflowSpecs[0].Title)
	}
	if len(result.RenderedBlocks) != 2 {
		t.Fatalf("expected 2 rendered blocks, got %d", len(result.RenderedBlocks))
	}
	if !strings.Contains(result.TextPart, "Let me show you the plan") {
		t.Fatalf("expected prose text, got %q", result.TextPart)
	}
}

func TestProcessMessageA2UIPlainMessage(t *testing.T) {
	content := "Just a regular message with no blocks."
	result := processMessageA2UI(content, 60)

	if result.HasInteractive {
		t.Fatal("expected HasInteractive=false for plain message")
	}
	if len(result.RenderedBlocks) != 0 {
		t.Fatalf("expected no rendered blocks, got %d", len(result.RenderedBlocks))
	}
	if result.TextPart != content {
		t.Fatalf("expected original text, got %q", result.TextPart)
	}
}

func TestMessageA2UIStateFocusNavigation(t *testing.T) {
	state := newMessageA2UIState()
	state.setInteractiveMessages([]int{2, 5, 8})

	if state.focusedIndex() != -1 {
		t.Fatalf("expected no focus initially, got %d", state.focusedIndex())
	}

	state.focusNext()
	if state.focusedIndex() != 2 {
		t.Fatalf("expected focus on index 2, got %d", state.focusedIndex())
	}

	state.focusNext()
	if state.focusedIndex() != 5 {
		t.Fatalf("expected focus on index 5, got %d", state.focusedIndex())
	}

	state.focusNext()
	if state.focusedIndex() != 8 {
		t.Fatalf("expected focus on index 8, got %d", state.focusedIndex())
	}

	// Wrap around
	state.focusNext()
	if state.focusedIndex() != 2 {
		t.Fatalf("expected wrap to index 2, got %d", state.focusedIndex())
	}

	state.focusPrev()
	if state.focusedIndex() != 8 {
		t.Fatalf("expected wrap back to index 8, got %d", state.focusedIndex())
	}
}

func TestMessageA2UIStateEmptySet(t *testing.T) {
	state := newMessageA2UIState()
	state.setInteractiveMessages(nil)

	state.focusNext() // should not panic
	state.focusPrev() // should not panic

	if state.focusedIndex() != -1 {
		t.Fatalf("expected -1 for empty set, got %d", state.focusedIndex())
	}
}

func TestParseWorkflowSpecValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid",
			input:   `{"id":"test","title":"Test","steps":[]}`,
			wantErr: false,
		},
		{
			name:    "missing id",
			input:   `{"title":"Test","steps":[]}`,
			wantErr: true,
		},
		{
			name:    "missing title",
			input:   `{"id":"test","steps":[]}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{broken`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseWorkflowSpec(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseA2UIComponentRejectsUnknownTypes(t *testing.T) {
	_, err := parseA2UIComponent(`{"type":"nonexistent","props":{}}`)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestDetectWorkflowBlockNoMatch(t *testing.T) {
	content := "No workflow here, just text with ```code block```"
	blocks, remaining := detectWorkflowBlocks(content)
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
	if remaining != content {
		t.Fatalf("expected unchanged content, got %q", remaining)
	}
}
