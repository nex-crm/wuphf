package render

import (
	"strings"
	"testing"
)

func TestRenderTableBasic(t *testing.T) {
	headers := []string{"Name", "Status"}
	rows := [][]string{
		{"Alice", "active"},
		{"Bob", "inactive"},
		{"Carol", "active"},
	}

	out := RenderTable(headers, rows, 80)

	if !strings.Contains(out, "Name") {
		t.Error("expected header 'Name' in output")
	}
	if !strings.Contains(out, "Status") {
		t.Error("expected header 'Status' in output")
	}
	if !strings.Contains(out, "(3 rows)") {
		t.Errorf("expected '(3 rows)' footer, got:\n%s", out)
	}
	// Verify all data rows present.
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected row containing %q", name)
		}
	}
}

func TestRenderTableEmpty(t *testing.T) {
	out := RenderTable([]string{"Name"}, nil, 80)
	if !strings.Contains(out, "(no results)") {
		t.Errorf("expected '(no results)' for empty table, got:\n%s", out)
	}
}

func TestRenderTableTruncation(t *testing.T) {
	headers := []string{"Name", "Description"}
	rows := [][]string{
		{"A", "This is a very long description that should get truncated"},
	}

	out := RenderTable(headers, rows, 30)

	if !strings.Contains(out, "...") {
		t.Errorf("expected truncation '...' with narrow maxWidth, got:\n%s", out)
	}
}
