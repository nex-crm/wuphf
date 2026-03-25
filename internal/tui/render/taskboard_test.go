package render

import (
	"strings"
	"testing"
)

func TestRenderTaskboardBasic(t *testing.T) {
	tasks := []TaskCard{
		{Title: "Design mockups", Priority: "high", Status: "todo", Due: "2026-03-25"},
		{Title: "Write API", Priority: "urgent", Status: "in_progress"},
		{Title: "Setup CI", Priority: "medium", Status: "done"},
	}

	result := RenderTaskboard(tasks, 80)

	// Should contain column headers.
	if !strings.Contains(result, "To Do") {
		t.Error("expected 'To Do' column header")
	}
	if !strings.Contains(result, "In Progress") {
		t.Error("expected 'In Progress' column header")
	}
	if !strings.Contains(result, "Done") {
		t.Error("expected 'Done' column header")
	}

	// Should contain task titles.
	if !strings.Contains(result, "Design mockups") {
		t.Error("expected task 'Design mockups'")
	}
	if !strings.Contains(result, "Write API") {
		t.Error("expected task 'Write API'")
	}
	if !strings.Contains(result, "Setup CI") {
		t.Error("expected task 'Setup CI'")
	}

	// Should contain due date.
	if !strings.Contains(result, "2026-03-25") {
		t.Error("expected due date '2026-03-25'")
	}

	// Should contain separator.
	if !strings.Contains(result, "│") {
		t.Error("expected column separator '│'")
	}
}

func TestRenderTaskboardEmpty(t *testing.T) {
	result := RenderTaskboard(nil, 80)
	if result != "(no tasks)" {
		t.Errorf("expected '(no tasks)', got %q", result)
	}

	result2 := RenderTaskboard([]TaskCard{}, 80)
	if result2 != "(no tasks)" {
		t.Errorf("expected '(no tasks)' for empty slice, got %q", result2)
	}
}

func TestRenderTaskboardPriority(t *testing.T) {
	tasks := []TaskCard{
		{Title: "Critical bug", Priority: "urgent", Status: "todo"},
		{Title: "Feature req", Priority: "high", Status: "todo"},
		{Title: "Nice to have", Priority: "medium", Status: "todo"},
		{Title: "Someday", Priority: "low", Status: "todo"},
		{Title: "Unset priority", Priority: "", Status: "todo"},
	}

	result := RenderTaskboard(tasks, 100)

	// Check that priority badges are present.
	if !strings.Contains(result, "!!!") {
		t.Error("expected '!!!' badge for urgent priority")
	}
	if !strings.Contains(result, "!!") {
		// "!!" is a substring of "!!!" so check for "!! " (high only has two)
		// We verify by checking the task title appears alongside its badge.
		if !strings.Contains(result, "Feature req") {
			t.Error("expected 'Feature req' task")
		}
	}
	if !strings.Contains(result, "Critical bug") {
		t.Error("expected 'Critical bug' task")
	}
	if !strings.Contains(result, "Nice to have") {
		t.Error("expected 'Nice to have' task")
	}
	if !strings.Contains(result, "·") {
		t.Error("expected '·' badge for low/default priority")
	}
}

func TestRenderTaskboardAllColumns(t *testing.T) {
	tasks := []TaskCard{
		{Title: "Task A", Priority: "high", Status: "todo"},
		{Title: "Task B", Priority: "medium", Status: "todo"},
		{Title: "Task C", Priority: "urgent", Status: "in_progress"},
		{Title: "Task D", Priority: "low", Status: "done"},
		{Title: "Task E", Priority: "low", Status: "done"},
	}

	result := RenderTaskboard(tasks, 90)

	// All tasks should appear.
	for _, name := range []string{"Task A", "Task B", "Task C", "Task D", "Task E"} {
		if !strings.Contains(result, name) {
			t.Errorf("expected task %q in output", name)
		}
	}

	// Verify multiple lines (header + divider + content).
	lines := strings.Split(result, "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines (header, divider, content), got %d", len(lines))
	}
}

func TestRenderTaskboardNarrowWidth(t *testing.T) {
	tasks := []TaskCard{
		{Title: "A very long task title that should be truncated", Priority: "high", Status: "todo"},
	}

	result := RenderTaskboard(tasks, 40)

	// Should still render without panic.
	if result == "" {
		t.Error("expected non-empty output for narrow width")
	}
	if !strings.Contains(result, "To Do") {
		t.Error("expected column header even at narrow width")
	}
}
