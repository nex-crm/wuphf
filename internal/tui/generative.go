package tui

import (
	"fmt"
	"strings"
)

// A2UIComponent is a generative UI component emitted by agents as JSON schemas.
type A2UIComponent struct {
	Type     string          `json:"type"`
	Children []A2UIComponent `json:"children,omitempty"`
	Props    map[string]any  `json:"props,omitempty"`
	DataRef  string          `json:"dataRef,omitempty"`
	Action   string          `json:"action,omitempty"`
}

// A2UIDataUpdate is a patch operation on the generative model's data store.
type A2UIDataUpdate struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// GenerativeModel holds a schema and its data, rendering inline TUI components.
type GenerativeModel struct {
	schema   *A2UIComponent
	data     map[string]any
	width    int
	registry *ComponentRegistry
}

// NewGenerativeModel creates an empty GenerativeModel with the default component registry.
func NewGenerativeModel() GenerativeModel {
	return GenerativeModel{
		data:     make(map[string]any),
		registry: NewComponentRegistry(),
	}
}

// SetSchema replaces the current component schema.
func (g *GenerativeModel) SetSchema(schema A2UIComponent) {
	g.schema = &schema
}

// SetData replaces the current data store.
func (g *GenerativeModel) SetData(data map[string]any) {
	g.data = data
}

// SetWidth overrides the render width used by View.
func (g *GenerativeModel) SetWidth(width int) {
	g.width = width
}

// SetValue sets a single value at the given JSON Pointer path (RFC 6901).
func (g *GenerativeModel) SetValue(pointer string, value any) {
	setPointer(g.data, pointer, value)
}

// MergeValue merges a map into the node at the given JSON Pointer path.
func (g *GenerativeModel) MergeValue(pointer string, value map[string]any) {
	mergePointer(g.data, pointer, value)
}

// DeleteValue removes the key at the given JSON Pointer path.
func (g *GenerativeModel) DeleteValue(pointer string) {
	deletePointer(g.data, pointer)
}

// ApplyUpdates applies a batch of JSON Pointer patch operations to the data store.
func (g *GenerativeModel) ApplyUpdates(updates []A2UIDataUpdate) {
	for _, u := range updates {
		switch u.Op {
		case "set":
			setPointer(g.data, u.Path, u.Value)
		case "merge":
			if sub, ok := u.Value.(map[string]any); ok {
				mergePointer(g.data, u.Path, sub)
			} else {
				setPointer(g.data, u.Path, u.Value)
			}
		case "delete":
			deletePointer(g.data, u.Path)
		}
	}
}

// Validate checks the current schema against the component registry.
func (g *GenerativeModel) Validate() error {
	if g.schema == nil {
		return fmt.Errorf("no schema set")
	}
	return g.registry.Validate(*g.schema)
}

// View renders the current schema with resolved data via the component registry.
func (g GenerativeModel) View() string {
	if g.schema == nil {
		return ""
	}
	width := g.width
	if width <= 0 {
		width = 80
	}
	return g.registry.Render(*g.schema, g.data, width)
}

// resolvePointer implements RFC 6901 JSON Pointer resolution.
// "/foo/bar/0" -> data["foo"]["bar"][0]
func resolvePointer(data any, pointer string) any {
	if pointer == "" || pointer == "/" {
		return data
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := data
	for _, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		switch v := current.(type) {
		case map[string]any:
			current = v[part]
		case []any:
			var idx int
			fmt.Sscanf(part, "%d", &idx)
			if idx >= 0 && idx < len(v) {
				current = v[idx]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
}

// setPointer sets a value at the given JSON Pointer path, creating intermediate maps as needed.
func setPointer(data map[string]any, pointer string, value any) {
	if pointer == "" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := data
	for i, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		if sub, ok := current[part].(map[string]any); ok {
			current = sub
		} else {
			sub := make(map[string]any)
			current[part] = sub
			current = sub
		}
	}
}

// mergePointer merges a map into the node at the given path.
func mergePointer(data map[string]any, pointer string, value map[string]any) {
	if pointer == "" || pointer == "/" {
		for k, v := range value {
			data[k] = v
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := data
	for i, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		if i == len(parts)-1 {
			if sub, ok := current[part].(map[string]any); ok {
				for k, v := range value {
					sub[k] = v
				}
			} else {
				current[part] = value
			}
			return
		}
		if sub, ok := current[part].(map[string]any); ok {
			current = sub
		} else {
			sub := make(map[string]any)
			current[part] = sub
			current = sub
		}
	}
}

// deletePointer removes the key at the given JSON Pointer path.
func deletePointer(data map[string]any, pointer string) {
	if pointer == "" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := data
	for i, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		if i == len(parts)-1 {
			delete(current, part)
			return
		}
		sub, ok := current[part].(map[string]any)
		if !ok {
			return
		}
		current = sub
	}
}

// renderTable renders a simple text table from row data.
func renderTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	colWidths := make([]int, cols)
	for _, row := range rows {
		for j, cell := range row {
			if len(cell) > colWidths[j] {
				colWidths[j] = len(cell)
			}
		}
	}
	var sb strings.Builder
	for i, row := range rows {
		for j := 0; j < cols; j++ {
			cell := ""
			if j < len(row) {
				cell = row[j]
			}
			pad := colWidths[j] - len(cell) + 2
			sb.WriteString(cell)
			sb.WriteString(strings.Repeat(" ", pad))
		}
		if i < len(rows)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderProgress renders a progress bar with percentage.
func renderProgress(val float64, width int) string {
	if val < 0 {
		val = 0
	}
	if val > 1 {
		val = 1
	}
	barWidth := width - 8
	if barWidth < 4 {
		barWidth = 20
	}
	filled := int(val * float64(barWidth))
	empty := barWidth - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return fmt.Sprintf("%s %d%%", bar, int(val*100))
}
