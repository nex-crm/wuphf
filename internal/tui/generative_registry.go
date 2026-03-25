package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ComponentRenderer renders a single A2UI component to a string.
type ComponentRenderer func(comp A2UIComponent, data map[string]any, width int, registry *ComponentRegistry) string

// ComponentSpec defines a registered component type with its renderer and required props.
type ComponentSpec struct {
	Renderer      ComponentRenderer
	RequiredProps []string // prop keys that must be present (empty = none required)
	AllowChildren bool     // whether the component accepts children
}

// ComponentRegistry maps component type names to their specs.
type ComponentRegistry struct {
	specs map[string]ComponentSpec
}

// NewComponentRegistry creates a registry with all built-in A2UI component types.
func NewComponentRegistry() *ComponentRegistry {
	r := &ComponentRegistry{specs: make(map[string]ComponentSpec)}
	r.registerDefaults()
	return r
}

// Register adds a component spec to the registry.
func (r *ComponentRegistry) Register(typeName string, spec ComponentSpec) {
	r.specs[typeName] = spec
}

// Render renders a component by type, dispatching to the registered renderer.
func (r *ComponentRegistry) Render(comp A2UIComponent, data map[string]any, width int) string {
	spec, ok := r.specs[comp.Type]
	if !ok {
		return fmt.Sprintf("[unknown component: %s]", comp.Type)
	}
	return spec.Renderer(comp, data, width, r)
}

// RenderChildren is a helper that renders all children of a component.
func (r *ComponentRegistry) RenderChildren(children []A2UIComponent, data map[string]any, width int) []string {
	parts := make([]string, len(children))
	for i, child := range children {
		parts[i] = r.Render(child, data, width)
	}
	return parts
}

// Validate checks that a component has the required props for its type.
// Returns nil if valid, or an error describing the problem.
func (r *ComponentRegistry) Validate(comp A2UIComponent) error {
	return r.validateRecursive(comp, "")
}

func (r *ComponentRegistry) validateRecursive(comp A2UIComponent, path string) error {
	if path == "" {
		path = comp.Type
	}

	spec, ok := r.specs[comp.Type]
	if !ok {
		return fmt.Errorf("%s: unknown component type %q", path, comp.Type)
	}

	// Check required props
	for _, key := range spec.RequiredProps {
		if comp.Props == nil || comp.Props[key] == nil {
			return fmt.Errorf("%s: missing required prop %q", path, key)
		}
	}

	// Check children on non-container types
	if !spec.AllowChildren && len(comp.Children) > 0 {
		return fmt.Errorf("%s: component type %q does not accept children", path, comp.Type)
	}

	// Validate children recursively
	for i, child := range comp.Children {
		childPath := fmt.Sprintf("%s.children[%d](%s)", path, i, child.Type)
		if err := r.validateRecursive(child, childPath); err != nil {
			return err
		}
	}

	return nil
}

// registerDefaults registers all built-in component types.
func (r *ComponentRegistry) registerDefaults() {
	r.Register("row", ComponentSpec{
		Renderer:      renderRowComponent,
		AllowChildren: true,
	})
	r.Register("column", ComponentSpec{
		Renderer:      renderColumnComponent,
		AllowChildren: true,
	})
	r.Register("card", ComponentSpec{
		Renderer:      renderCardComponent,
		AllowChildren: true,
	})
	r.Register("text", ComponentSpec{
		Renderer: renderTextComponent,
	})
	r.Register("textfield", ComponentSpec{
		Renderer:      renderTextfieldComponent,
		RequiredProps: []string{"label"},
	})
	r.Register("list", ComponentSpec{
		Renderer: renderListComponent,
	})
	r.Register("table", ComponentSpec{
		Renderer: renderTableComponent,
	})
	r.Register("progress", ComponentSpec{
		Renderer: renderProgressComponent,
	})
	r.Register("spacer", ComponentSpec{
		Renderer: renderSpacerComponent,
	})
}

// --- Component renderers ---

func renderRowComponent(comp A2UIComponent, data map[string]any, width int, reg *ComponentRegistry) string {
	if len(comp.Children) == 0 {
		return ""
	}
	childWidth := width / len(comp.Children)
	if childWidth < 1 {
		childWidth = 1
	}
	parts := make([]string, len(comp.Children))
	for i, child := range comp.Children {
		parts[i] = reg.Render(child, data, childWidth)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func renderColumnComponent(comp A2UIComponent, data map[string]any, width int, reg *ComponentRegistry) string {
	parts := reg.RenderChildren(comp.Children, data, width)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func renderCardComponent(comp A2UIComponent, data map[string]any, width int, reg *ComponentRegistry) string {
	title := ""
	if t, ok := comp.Props["title"].(string); ok {
		title = t
	}
	inner := reg.RenderChildren(comp.Children, data, max(1, width-4))
	body := strings.Join(inner, "\n")
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 1).
		Width(max(4, width-2))
	if title != "" {
		titleLine := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(NexPurple)).Render(title)
		return titleLine + "\n" + cardStyle.Render(body)
	}
	return cardStyle.Render(body)
}

func renderTextComponent(comp A2UIComponent, data map[string]any, width int, _ *ComponentRegistry) string {
	val := ""
	if comp.DataRef != "" {
		resolved := resolvePointer(data, comp.DataRef)
		if resolved != nil {
			val = fmt.Sprintf("%v", resolved)
		}
	} else if s, ok := comp.Props["content"].(string); ok {
		val = s
	}
	style := lipgloss.NewStyle()
	if b, ok := comp.Props["bold"].(bool); ok && b {
		style = style.Bold(true)
	}
	if c, ok := comp.Props["color"].(string); ok && c != "" {
		style = style.Foreground(lipgloss.Color(c))
	}
	if d, ok := comp.Props["dimmed"].(bool); ok && d {
		style = style.Foreground(lipgloss.Color(MutedColor))
	}
	return style.Render(val)
}

func renderTextfieldComponent(comp A2UIComponent, data map[string]any, _ int, _ *ComponentRegistry) string {
	label := "input"
	if l, ok := comp.Props["label"].(string); ok && l != "" {
		label = l
	}
	val := ""
	if comp.DataRef != "" {
		if resolved := resolvePointer(data, comp.DataRef); resolved != nil {
			val = fmt.Sprintf("%v", resolved)
		}
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(LabelColor))
	fieldStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 1)
	content := val
	if content == "" {
		content = lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor)).Render(label)
	}
	return labelStyle.Render(label) + "\n" + fieldStyle.Render(content)
}

func renderListComponent(comp A2UIComponent, data map[string]any, _ int, _ *ComponentRegistry) string {
	var items []any
	if comp.DataRef != "" {
		if resolved, ok := resolvePointer(data, comp.DataRef).([]any); ok {
			items = resolved
		}
	} else if rawItems, ok := comp.Props["items"].([]any); ok {
		items = rawItems
	} else if rawStrings, ok := comp.Props["items"].([]string); ok {
		items = make([]any, len(rawStrings))
		for i, item := range rawStrings {
			items[i] = item
		}
	}
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = "• " + fmt.Sprintf("%v", item)
	}
	return strings.Join(lines, "\n")
}

func renderTableComponent(comp A2UIComponent, data map[string]any, _ int, _ *ComponentRegistry) string {
	var rows [][]string
	if comp.DataRef != "" {
		if resolved, ok := resolvePointer(data, comp.DataRef).([]any); ok {
			for _, row := range resolved {
				if r, ok := row.([]any); ok {
					cells := make([]string, len(r))
					for j, cell := range r {
						cells[j] = fmt.Sprintf("%v", cell)
					}
					rows = append(rows, cells)
				}
			}
		}
	} else if rawRows, ok := comp.Props["rows"].([]any); ok {
		for _, row := range rawRows {
			if r, ok := row.([]any); ok {
				cells := make([]string, len(r))
				for j, cell := range r {
					cells[j] = fmt.Sprintf("%v", cell)
				}
				rows = append(rows, cells)
			}
		}
	}
	return renderTable(rows)
}

func renderProgressComponent(comp A2UIComponent, data map[string]any, width int, _ *ComponentRegistry) string {
	var val float64
	if comp.DataRef != "" {
		switch v := resolvePointer(data, comp.DataRef).(type) {
		case float64:
			val = v
		case int:
			val = float64(v)
		}
	} else {
		switch v := comp.Props["value"].(type) {
		case float64:
			val = v
		case int:
			val = float64(v)
		}
	}
	return renderProgress(val, width)
}

func renderSpacerComponent(comp A2UIComponent, _ map[string]any, _ int, _ *ComponentRegistry) string {
	n := 1
	if v, ok := comp.Props["lines"].(float64); ok && v > 0 {
		n = int(v)
	}
	return strings.Repeat("\n", n)
}
