package workflow

import (
	"fmt"
	"strings"
)

// Guards are intentionally a tiny, deterministic expression language — not a
// general scripting engine. A workflow contract must be auditable, so the only
// forms allowed are:
//
//	""                      always true
//	"<field> exists"        data[field] present and non-empty
//	"<field> == <value>"    string(data[field]) == value
//	"<field> != <value>"    string(data[field]) != value
//
// Anything else is rejected at Validate time, so a guard can never silently
// pass because it failed to parse.

func validateGuard(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	if _, _, ok := parseGuard(expr); !ok {
		return fmt.Errorf("must be empty, \"<field> exists\", \"<field> == <value>\", or \"<field> != <value>\"")
	}
	return nil
}

// parseGuard returns (op, operandField/value bundle, ok). Kept private; callers
// use evalGuard.
func parseGuard(expr string) (op string, parts []string, ok bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil, true
	}
	if strings.HasSuffix(expr, " exists") {
		field := strings.TrimSpace(strings.TrimSuffix(expr, " exists"))
		if field == "" {
			return "", nil, false
		}
		return "exists", []string{field}, true
	}
	for _, cmp := range []string{"==", "!="} {
		if i := strings.Index(expr, cmp); i >= 0 {
			field := strings.TrimSpace(expr[:i])
			value := strings.TrimSpace(expr[i+len(cmp):])
			if field == "" || value == "" {
				return "", nil, false
			}
			return cmp, []string{field, value}, true
		}
	}
	return "", nil, false
}

// evalGuard evaluates a (pre-validated) guard against event data.
// Deterministic: same expr + same data always yields the same result.
func evalGuard(expr string, data map[string]any) bool {
	op, parts, ok := parseGuard(expr)
	if !ok {
		return false
	}
	switch op {
	case "":
		return true
	case "exists":
		v, present := data[parts[0]]
		return present && strings.TrimSpace(fmt.Sprint(v)) != ""
	case "==":
		return fmt.Sprint(data[parts[0]]) == parts[1]
	case "!=":
		return fmt.Sprint(data[parts[0]]) != parts[1]
	}
	return false
}
