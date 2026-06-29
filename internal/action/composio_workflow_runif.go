package action

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Deterministic conditional gate for workflow steps. A step's `run_if` is a
// single comparison evaluated over the workflow scope (inputs + prior step
// outputs); when it is false the step is skipped, with no AI in the loop. This
// is the deterministic half of "deterministic unless AI needs to decide": a
// threshold like `steps.score.result.fit >= 80` stays a plain number compare,
// while genuine judgment lives in nex_ask steps.
//
// The grammar is intentionally tiny and closed — `<operand> <op> <operand>`,
// where an operand is a scope path, a number, a quoted string, or a boolean —
// because this gate decides whether external mutations run. No arbitrary
// expressions, no function calls, no code. Surrounding `{{ }}` is tolerated so
// the planner can emit either `steps.x >= 80` or `{{ steps.x >= 80 }}`.

// Two-char operators are matched before single-char so ">=" never reads as ">".
var runIfOperators = []string{">=", "<=", "==", "!=", ">", "<"}

type runIfOperand struct {
	// path is a dotted scope path (e.g. "steps.score.result.fit") when non-empty;
	// otherwise literal holds a number (float64), string, or bool.
	path    string
	literal any
}

type runIfCondition struct {
	lhs runIfOperand
	op  string
	rhs runIfOperand
}

// parseRunIf compiles a run_if expression. It is called at definition decode
// time so a malformed gate is rejected before the workflow ever runs.
func parseRunIf(expr string) (runIfCondition, error) {
	text := strings.TrimSpace(expr)
	if strings.HasPrefix(text, "{{") && strings.HasSuffix(text, "}}") {
		text = strings.TrimSpace(text[2 : len(text)-2])
	}
	if text == "" {
		return runIfCondition{}, fmt.Errorf("run_if is empty")
	}

	op, idx := findRunIfOperator(text)
	if op == "" {
		return runIfCondition{}, fmt.Errorf("run_if %q has no comparison operator (use one of >= <= == != > <)", expr)
	}
	lhs, err := parseRunIfOperand(text[:idx])
	if err != nil {
		return runIfCondition{}, fmt.Errorf("run_if left side: %w", err)
	}
	rhs, err := parseRunIfOperand(text[idx+len(op):])
	if err != nil {
		return runIfCondition{}, fmt.Errorf("run_if right side: %w", err)
	}
	return runIfCondition{lhs: lhs, op: op, rhs: rhs}, nil
}

// findRunIfOperator returns the first operator in the string and its index,
// preferring two-char operators. Operators inside a quoted literal are ignored.
func findRunIfOperator(text string) (string, int) {
	var quote rune
	for i := 0; i < len(text); i++ {
		ch := rune(text[i])
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		for _, op := range runIfOperators {
			if strings.HasPrefix(text[i:], op) {
				return op, i
			}
		}
	}
	return "", -1
}

func parseRunIfOperand(tok string) (runIfOperand, error) {
	t := strings.TrimSpace(tok)
	if t == "" {
		return runIfOperand{}, fmt.Errorf("missing operand")
	}
	// Quoted string literal.
	if len(t) >= 2 && (t[0] == '"' || t[0] == '\'') && t[len(t)-1] == t[0] {
		return runIfOperand{literal: t[1 : len(t)-1]}, nil
	}
	// Boolean literal.
	switch strings.ToLower(t) {
	case "true":
		return runIfOperand{literal: true}, nil
	case "false":
		return runIfOperand{literal: false}, nil
	}
	// Number literal.
	if n, err := strconv.ParseFloat(t, 64); err == nil {
		return runIfOperand{literal: n}, nil
	}
	// Otherwise a scope path. Keep it conservative: identifiers and dots only.
	for _, ch := range t {
		isLetter := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		isDigit := ch >= '0' && ch <= '9'
		if !isLetter && !isDigit && ch != '.' && ch != '_' {
			return runIfOperand{}, fmt.Errorf("operand %q is not a number, quoted string, boolean, or scope path", t)
		}
	}
	return runIfOperand{path: t}, nil
}

// evaluateRunIf resolves the condition against the scope and returns whether the
// step should run. An unresolved path or a type-incompatible comparison is an
// error (surfaced as a failed run) rather than a silent skip, so planner bugs do
// not quietly drop a step.
func evaluateRunIf(expr string, scope map[string]any) (bool, error) {
	cond, err := parseRunIf(expr)
	if err != nil {
		return false, err
	}
	left, err := resolveRunIfOperand(cond.lhs, scope)
	if err != nil {
		return false, err
	}
	right, err := resolveRunIfOperand(cond.rhs, scope)
	if err != nil {
		return false, err
	}
	return compareRunIf(left, cond.op, right)
}

func resolveRunIfOperand(op runIfOperand, scope map[string]any) (any, error) {
	if op.path == "" {
		return op.literal, nil
	}
	return lookupScopePath(scope, op.path)
}

// lookupScopePath walks a dotted path through the scope maps.
func lookupScopePath(scope map[string]any, path string) (any, error) {
	var current any = scope
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("run_if path %q: %q is not an object", path, strings.Join(segments[:i], "."))
		}
		next, ok := asMap[segment]
		if !ok {
			return nil, fmt.Errorf("run_if path %q: %q not found", path, strings.Join(segments[:i+1], "."))
		}
		current = next
	}
	return current, nil
}

func compareRunIf(left any, op string, right any) (bool, error) {
	lf, lok := runIfFloat(left)
	rf, rok := runIfFloat(right)
	if lok && rok {
		switch op {
		case ">":
			return lf > rf, nil
		case ">=":
			return lf >= rf, nil
		case "<":
			return lf < rf, nil
		case "<=":
			return lf <= rf, nil
		case "==":
			return lf == rf, nil
		case "!=":
			return lf != rf, nil
		}
	}
	// Non-numeric: only equality is well-defined.
	switch op {
	case "==":
		return runIfString(left) == runIfString(right), nil
	case "!=":
		return runIfString(left) != runIfString(right), nil
	default:
		return false, fmt.Errorf("run_if operator %q needs numbers, got %v and %v", op, left, right)
	}
}

func runIfFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func runIfString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
