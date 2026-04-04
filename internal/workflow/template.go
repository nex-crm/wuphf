package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// templateShorthandPatterns normalize common shorthand expressions
// like {{inputs.x}} into proper Go template syntax {{.inputs.x}}.
var templateShorthandPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\{\{\s*inputs\.`), "{{ .inputs."},
	{regexp.MustCompile(`\{\{\s*steps\.`), "{{ .steps."},
	{regexp.MustCompile(`\{\{\s*workflow\.`), "{{ .workflow."},
	{regexp.MustCompile(`\{\{\s*now\.`), "{{ .now."},
	{regexp.MustCompile(`\{\{\s*today_date\s*\}\}`), "{{ .now.date }}"},
	{regexp.MustCompile(`\{\{\s*today_rfc3339\s*\}\}`), "{{ .now.rfc3339 }}"},
}

var (
	handlebarsEachOpenRe = regexp.MustCompile(`\{\{\s*#each\s+([^}]+?)\s*\}\}`)
	handlebarsEachClose  = regexp.MustCompile(`\{\{\s*/each\s*\}\}`)
	handlebarsThisRe     = regexp.MustCompile(`\{\{\s*this\.([^}]+?)\s*\}\}`)
)

// NormalizeTemplateString converts shorthand and Handlebars-style
// template expressions into valid Go text/template syntax.
func NormalizeTemplateString(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return raw
	}
	for _, pattern := range templateShorthandPatterns {
		text = pattern.re.ReplaceAllString(text, pattern.repl)
	}
	text = handlebarsEachOpenRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := handlebarsEachOpenRe.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		expr := strings.TrimSpace(parts[1])
		if strings.HasPrefix(expr, "steps.") || strings.HasPrefix(expr, "inputs.") || strings.HasPrefix(expr, "workflow.") || strings.HasPrefix(expr, "now.") {
			expr = "." + expr
		}
		return "{{- range $item := " + expr + " }}"
	})
	text = handlebarsEachClose.ReplaceAllString(text, "{{- end }}")
	text = handlebarsThisRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := handlebarsThisRe.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		return "{{ $item." + strings.TrimSpace(parts[1]) + " }}"
	})
	return text
}

// RenderTemplate executes a Go text/template string against a scope map.
// Returns the raw string unchanged if it contains no template directives.
func RenderTemplate(tpl string, scope map[string]any) (string, error) {
	if !strings.Contains(tpl, "{{") {
		return tpl, nil
	}
	t, err := template.New("workflow").Option("missingkey=error").Funcs(template.FuncMap{
		"toJSON": func(v any) string {
			if s, ok := v.(string); ok {
				return s
			}
			raw, _ := json.Marshal(v)
			return string(raw)
		},
		"toPrettyJSON": func(v any) string {
			if s, ok := v.(string); ok {
				return s
			}
			raw, _ := json.MarshalIndent(v, "", "  ")
			return string(raw)
		},
		"trim":  strings.TrimSpace,
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
	}).Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, scope); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderValue recursively renders template strings within any value
// (string, map, slice) against the given scope.
func RenderValue(value any, scope map[string]any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return RenderTemplate(typed, scope)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			rendered, err := RenderValue(item, scope)
			if err != nil {
				return nil, err
			}
			out = append(out, rendered)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			rendered, err := RenderValue(item, scope)
			if err != nil {
				return nil, err
			}
			out[key] = rendered
		}
		return out, nil
	default:
		return value, nil
	}
}

// RenderMap renders all values in a map through templates.
func RenderMap(in map[string]any, scope map[string]any) (map[string]any, error) {
	if len(in) == 0 {
		return nil, nil
	}
	rendered, err := RenderValue(in, scope)
	if err != nil {
		return nil, err
	}
	out, _ := rendered.(map[string]any)
	return out, nil
}

// RenderString renders a single value as a template string.
func RenderString(value any, scope map[string]any) (string, error) {
	if value == nil {
		return "", nil
	}
	rendered, err := RenderValue(value, scope)
	if err != nil {
		return "", err
	}
	return StringValue(rendered), nil
}

// RenderInt renders a value as an integer, with a fallback default.
func RenderInt(value any, scope map[string]any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	rendered, err := RenderValue(value, scope)
	if err != nil {
		return 0, err
	}
	if n := IntValue(rendered); n > 0 {
		return n, nil
	}
	return fallback, nil
}

// BuildScope constructs the template scope for workflow rendering.
// The provider parameter identifies the workflow engine ("composio", "interactive", etc.).
func BuildScope(key, provider string, inputs map[string]any, steps map[string]any) map[string]any {
	now := time.Now().UTC()
	return map[string]any{
		"workflow": map[string]any{
			"key":      key,
			"provider": provider,
		},
		"inputs": NormalizeScopeValue(inputs),
		"steps":  NormalizeScopeValue(steps),
		"now": map[string]any{
			"rfc3339": now.Format(time.RFC3339),
			"date":    now.Format("2006-01-02"),
		},
		"meta": map[string]any{
			"rfc3339": now.Format(time.RFC3339),
			"date":    now.Format("2006-01-02"),
		},
	}
}

// NormalizeScopeValue round-trips a value through JSON to ensure
// consistent types (e.g., all numbers become float64).
func NormalizeScopeValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return value
	}
	return decoded
}

// NormalizeValueSyntax recursively normalizes template shorthand
// expressions within any value (string, map, slice).
func NormalizeValueSyntax(value any) any {
	switch typed := value.(type) {
	case string:
		return NormalizeTemplateString(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, NormalizeValueSyntax(item))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = NormalizeValueSyntax(item)
		}
		return out
	default:
		return value
	}
}

// NormalizeMapSyntax normalizes all values in a map through NormalizeValueSyntax.
func NormalizeMapSyntax(in map[string]any) map[string]any {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = NormalizeValueSyntax(value)
	}
	return out
}

// StringValue converts any value to its string representation.
func StringValue(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprintf("%v", v)
	}
}

// IntValue extracts an integer from a value that may be int, float64,
// json.Number, or string.
func IntValue(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	default:
		return 0
	}
}

// MergeInputs merges override inputs on top of defaults.
func MergeInputs(defaults, overrides map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range defaults {
		out[key] = value
	}
	for key, value := range overrides {
		out[key] = value
	}
	return out
}
