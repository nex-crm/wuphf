package tui

import (
	"strings"
	"testing"
)

func TestResolvePointer(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{
			"name":   "Alice",
			"scores": []any{10, 20, 30},
		},
	}

	// Scalar / index lookups
	scalars := []struct {
		pointer string
		want    any
	}{
		{"/user/name", "Alice"},
		{"/user/scores/1", 20},
		{"/user/missing", nil},
	}
	for _, tt := range scalars {
		got := resolvePointer(data, tt.pointer)
		if got != tt.want {
			t.Errorf("resolvePointer(%q) = %v, want %v", tt.pointer, got, tt.want)
		}
	}

	// Empty pointer returns the root; just verify it is non-nil
	if got := resolvePointer(data, ""); got == nil {
		t.Error("resolvePointer('') should return the root data, got nil")
	}
}

func TestSetPointer(t *testing.T) {
	data := map[string]any{}
	setPointer(data, "/a/b/c", "hello")
	got := resolvePointer(data, "/a/b/c")
	if got != "hello" {
		t.Errorf("expected 'hello', got %v", got)
	}
}

func TestApplyUpdates(t *testing.T) {
	g := NewGenerativeModel()
	g.SetData(map[string]any{"name": "old"})

	g.ApplyUpdates([]A2UIDataUpdate{
		{Op: "set", Path: "/name", Value: "new"},
		{Op: "set", Path: "/count", Value: float64(42)},
	})

	if got := resolvePointer(g.data, "/name"); got != "new" {
		t.Errorf("expected 'new', got %v", got)
	}
	if got := resolvePointer(g.data, "/count"); got != float64(42) {
		t.Errorf("expected 42, got %v", got)
	}

	g.ApplyUpdates([]A2UIDataUpdate{{Op: "delete", Path: "/count"}})
	if got := resolvePointer(g.data, "/count"); got != nil {
		t.Errorf("expected nil after delete, got %v", got)
	}
}

func TestSetValueMergeValueDeleteValue(t *testing.T) {
	g := NewGenerativeModel()

	// SetValue
	g.SetValue("/status", "active")
	if got := resolvePointer(g.data, "/status"); got != "active" {
		t.Errorf("SetValue: expected 'active', got %v", got)
	}

	// SetValue nested
	g.SetValue("/user/name", "Bob")
	if got := resolvePointer(g.data, "/user/name"); got != "Bob" {
		t.Errorf("SetValue nested: expected 'Bob', got %v", got)
	}

	// MergeValue
	g.MergeValue("/user", map[string]any{"email": "bob@test.com", "role": "admin"})
	if got := resolvePointer(g.data, "/user/email"); got != "bob@test.com" {
		t.Errorf("MergeValue: expected 'bob@test.com', got %v", got)
	}
	// Original key should still exist
	if got := resolvePointer(g.data, "/user/name"); got != "Bob" {
		t.Errorf("MergeValue should preserve existing keys, got %v", got)
	}

	// DeleteValue
	g.DeleteValue("/user/role")
	if got := resolvePointer(g.data, "/user/role"); got != nil {
		t.Errorf("DeleteValue: expected nil, got %v", got)
	}
}

func TestRenderCard(t *testing.T) {
	g := NewGenerativeModel()
	g.width = 40
	g.SetSchema(A2UIComponent{
		Type:  "card",
		Props: map[string]any{"title": "Test Card"},
		Children: []A2UIComponent{
			{
				Type:  "text",
				Props: map[string]any{"content": "Hello World"},
			},
		},
	})
	view := g.View()
	if !strings.Contains(view, "Test Card") {
		t.Errorf("card view missing title; got: %s", view)
	}
	if !strings.Contains(view, "Hello World") {
		t.Errorf("card view missing content; got: %s", view)
	}
}

func TestRenderList(t *testing.T) {
	g := NewGenerativeModel()
	g.width = 40
	g.SetData(map[string]any{
		"items": []any{"alpha", "beta", "gamma"},
	})
	g.SetSchema(A2UIComponent{
		Type:    "list",
		DataRef: "/items",
	})
	view := g.View()
	for _, item := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(view, item) {
			t.Errorf("list view missing %q; got: %s", item, view)
		}
	}
}

func TestRenderProgress(t *testing.T) {
	out := renderProgress(0.65, 40)
	if !strings.Contains(out, "65%") {
		t.Errorf("progress missing '65%%'; got: %s", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("progress missing filled blocks; got: %s", out)
	}
}

func TestRenderTable(t *testing.T) {
	rows := [][]string{
		{"Name", "Score"},
		{"Alice", "100"},
	}
	out := renderTable(rows)
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "Score") {
		t.Errorf("table missing expected content; got: %s", out)
	}
}

func TestResolvePointerEscape(t *testing.T) {
	data := map[string]any{
		"a/b": "slash",
		"a~b": "tilde",
	}
	// RFC 6901: "~1" -> "/" and "~0" -> "~"
	if got := resolvePointer(data, "/a~1b"); got != "slash" {
		t.Errorf("tilde-escape ~1: expected 'slash', got %v", got)
	}
	if got := resolvePointer(data, "/a~0b"); got != "tilde" {
		t.Errorf("tilde-escape ~0: expected 'tilde', got %v", got)
	}
}

// --- New component registry tests ---

func TestRegistryRenderText(t *testing.T) {
	reg := NewComponentRegistry()
	data := map[string]any{"greeting": "Hello"}

	// Static text
	out := reg.Render(A2UIComponent{
		Type:  "text",
		Props: map[string]any{"content": "Static"},
	}, data, 40)
	if !strings.Contains(out, "Static") {
		t.Errorf("text renderer should show static content, got: %s", out)
	}

	// Data-bound text
	out = reg.Render(A2UIComponent{
		Type:    "text",
		DataRef: "/greeting",
	}, data, 40)
	if !strings.Contains(out, "Hello") {
		t.Errorf("text renderer should resolve dataRef, got: %s", out)
	}
}

func TestRegistryRenderRow(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{
		Type: "row",
		Children: []A2UIComponent{
			{Type: "text", Props: map[string]any{"content": "Left"}},
			{Type: "text", Props: map[string]any{"content": "Right"}},
		},
	}, nil, 40)
	if !strings.Contains(out, "Left") || !strings.Contains(out, "Right") {
		t.Errorf("row should render both children, got: %s", out)
	}
}

func TestRegistryRenderColumn(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{
		Type: "column",
		Children: []A2UIComponent{
			{Type: "text", Props: map[string]any{"content": "Top"}},
			{Type: "text", Props: map[string]any{"content": "Bottom"}},
		},
	}, nil, 40)
	if !strings.Contains(out, "Top") || !strings.Contains(out, "Bottom") {
		t.Errorf("column should render both children, got: %s", out)
	}
}

func TestRegistryRenderTextfield(t *testing.T) {
	reg := NewComponentRegistry()
	data := map[string]any{"name": "Alice"}
	out := reg.Render(A2UIComponent{
		Type:    "textfield",
		Props:   map[string]any{"label": "Name"},
		DataRef: "/name",
	}, data, 40)
	if !strings.Contains(out, "Name") {
		t.Errorf("textfield should show label, got: %s", out)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("textfield should show data-bound value, got: %s", out)
	}
}

func TestRegistryRenderProgressComponent(t *testing.T) {
	reg := NewComponentRegistry()
	data := map[string]any{"pct": 0.75}
	out := reg.Render(A2UIComponent{
		Type:    "progress",
		DataRef: "/pct",
	}, data, 40)
	if !strings.Contains(out, "75%") {
		t.Errorf("progress should show 75%%, got: %s", out)
	}
}

func TestRegistryRenderSpacer(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{
		Type:  "spacer",
		Props: map[string]any{"lines": float64(3)},
	}, nil, 40)
	if out != "\n\n\n" {
		t.Errorf("spacer with lines=3 should produce 3 newlines, got: %q", out)
	}
}

func TestRegistryRenderTableComponent(t *testing.T) {
	reg := NewComponentRegistry()
	data := map[string]any{
		"rows": []any{
			[]any{"Name", "Score"},
			[]any{"Bob", "95"},
		},
	}
	out := reg.Render(A2UIComponent{
		Type:    "table",
		DataRef: "/rows",
	}, data, 40)
	if !strings.Contains(out, "Bob") || !strings.Contains(out, "95") {
		t.Errorf("table should render data rows, got: %s", out)
	}
}

func TestRegistryRenderListFromProps(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{
		Type: "list",
		Props: map[string]any{
			"items": []any{"alpha", "beta"},
		},
	}, nil, 40)
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("list should render direct prop items, got: %s", out)
	}
}

func TestRegistryRenderTableFromProps(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{
		Type: "table",
		Props: map[string]any{
			"rows": []any{
				[]any{"Name", "Score"},
				[]any{"Alice", "100"},
			},
		},
	}, nil, 40)
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "100") {
		t.Errorf("table should render direct prop rows, got: %s", out)
	}
}

func TestRegistryRenderProgressFromProps(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{
		Type:  "progress",
		Props: map[string]any{"value": 0.4},
	}, nil, 40)
	if !strings.Contains(out, "40%") {
		t.Errorf("progress should render direct prop value, got: %s", out)
	}
}

func TestRegistryRenderUnknownType(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{Type: "bogus"}, nil, 40)
	if !strings.Contains(out, "unknown component") {
		t.Errorf("unknown type should show error, got: %s", out)
	}
}

func TestRegistryValidate(t *testing.T) {
	reg := NewComponentRegistry()

	// Valid schema
	err := reg.Validate(A2UIComponent{
		Type: "column",
		Children: []A2UIComponent{
			{Type: "text", Props: map[string]any{"content": "ok"}},
		},
	})
	if err != nil {
		t.Errorf("valid schema should not error, got: %v", err)
	}

	// Unknown type
	err = reg.Validate(A2UIComponent{Type: "foobar"})
	if err == nil || !strings.Contains(err.Error(), "unknown component type") {
		t.Errorf("unknown type should error, got: %v", err)
	}

	// Missing required prop
	err = reg.Validate(A2UIComponent{Type: "textfield"})
	if err == nil || !strings.Contains(err.Error(), "missing required prop") {
		t.Errorf("missing prop should error, got: %v", err)
	}

	// Children on non-container
	err = reg.Validate(A2UIComponent{
		Type: "text",
		Children: []A2UIComponent{
			{Type: "text"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not accept children") {
		t.Errorf("children on leaf should error, got: %v", err)
	}

	// Nested validation
	err = reg.Validate(A2UIComponent{
		Type: "card",
		Children: []A2UIComponent{
			{Type: "invalid_type"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown component type") {
		t.Errorf("nested unknown type should error, got: %v", err)
	}
}

func TestGenerativeModelValidate(t *testing.T) {
	g := NewGenerativeModel()

	// No schema
	if err := g.Validate(); err == nil {
		t.Error("Validate with no schema should error")
	}

	// Valid schema
	g.SetSchema(A2UIComponent{
		Type:  "text",
		Props: map[string]any{"content": "hello"},
	})
	if err := g.Validate(); err != nil {
		t.Errorf("valid schema should pass, got: %v", err)
	}
}

func TestRenderNestedLayout(t *testing.T) {
	g := NewGenerativeModel()
	g.width = 60
	g.SetData(map[string]any{
		"title":    "Dashboard",
		"progress": 0.5,
		"items":    []any{"Task A", "Task B"},
	})
	g.SetSchema(A2UIComponent{
		Type: "column",
		Children: []A2UIComponent{
			{
				Type:  "card",
				Props: map[string]any{"title": "Overview"},
				Children: []A2UIComponent{
					{Type: "text", DataRef: "/title"},
					{Type: "progress", DataRef: "/progress"},
				},
			},
			{Type: "spacer"},
			{Type: "list", DataRef: "/items"},
		},
	})
	view := g.View()
	if !strings.Contains(view, "Dashboard") {
		t.Errorf("nested layout missing data-bound title, got: %s", view)
	}
	if !strings.Contains(view, "50%") {
		t.Errorf("nested layout missing progress, got: %s", view)
	}
	if !strings.Contains(view, "Task A") || !strings.Contains(view, "Task B") {
		t.Errorf("nested layout missing list items, got: %s", view)
	}
}

func TestRenderEmptyRow(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{Type: "row"}, nil, 40)
	if out != "" {
		t.Errorf("empty row should return empty string, got: %q", out)
	}
}

func TestRenderEmptyList(t *testing.T) {
	reg := NewComponentRegistry()
	out := reg.Render(A2UIComponent{Type: "list", DataRef: "/missing"}, map[string]any{}, 40)
	if out != "" {
		t.Errorf("empty list should return empty string, got: %q", out)
	}
}

func TestMergeUpdate(t *testing.T) {
	g := NewGenerativeModel()
	g.SetData(map[string]any{
		"user": map[string]any{"name": "Alice", "age": 30},
	})
	g.ApplyUpdates([]A2UIDataUpdate{
		{Op: "merge", Path: "/user", Value: map[string]any{"email": "alice@test.com"}},
	})
	if got := resolvePointer(g.data, "/user/email"); got != "alice@test.com" {
		t.Errorf("merge should add email, got %v", got)
	}
	if got := resolvePointer(g.data, "/user/name"); got != "Alice" {
		t.Errorf("merge should preserve name, got %v", got)
	}
}
