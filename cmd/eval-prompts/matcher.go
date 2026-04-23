package main

import (
	"fmt"
	"strings"
)

// matchResult holds the outcome of asserting a single eval case expected block.
type matchResult struct {
	pass     bool
	failures []string
}

// assertExpected runs all three contract checks against raw and parsed output.
//
// raw      — the unmodified string returned by the LLM (code fences stripped).
// parsed   — nil for synthesis prompts; otherwise the JSON-decoded map.
// expected — the expected block from the eval case JSON.
func assertExpected(raw string, parsed any, expected expectedBlock) matchResult {
	var failures []string

	for _, sub := range expected.MustInclude {
		if !strings.Contains(raw, sub) {
			failures = append(failures, fmt.Sprintf("must_include missing: %q", sub))
		}
	}

	for _, sub := range expected.MustNotInclude {
		if strings.Contains(raw, sub) {
			failures = append(failures, fmt.Sprintf("must_not_include present: %q", sub))
		}
	}

	if expected.Structured != nil && parsed != nil {
		if errs := partialMatch(expected.Structured, parsed, ""); len(errs) > 0 {
			failures = append(failures, errs...)
		}
	}

	return matchResult{
		pass:     len(failures) == 0,
		failures: failures,
	}
}

// partialMatch recursively checks that every key present in want also exists
// in got with an equal value. Keys absent from want are not checked. Arrays
// are matched elementwise by index.
//
// path is used to build human-readable failure messages.
func partialMatch(want, got any, path string) []string {
	var failures []string

	switch wv := want.(type) {
	case map[string]any:
		gm, ok := got.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("structured[%s]: expected object, got %T", path, got)}
		}
		for k, wVal := range wv {
			childPath := joinPath(path, k)
			gVal, exists := gm[k]
			if !exists {
				failures = append(failures, fmt.Sprintf("structured[%s]: key missing", childPath))
				continue
			}
			failures = append(failures, partialMatch(wVal, gVal, childPath)...)
		}

	case []any:
		ga, ok := got.([]any)
		if !ok {
			return []string{fmt.Sprintf("structured[%s]: expected array, got %T", path, got)}
		}
		for i, wItem := range wv {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			if i >= len(ga) {
				failures = append(failures, fmt.Sprintf("structured[%s]: index out of range (got len %d)", childPath, len(ga)))
				continue
			}
			failures = append(failures, partialMatch(wItem, ga[i], childPath)...)
		}

	default:
		// Scalar comparison.
		if !scalarEqual(want, got) {
			failures = append(failures, fmt.Sprintf("structured[%s]: want %v (%T), got %v (%T)", path, want, want, got, got))
		}
	}

	return failures
}

// scalarEqual compares scalars. JSON numbers unmarshal as float64; booleans as
// bool; strings as string. We need to handle float64 vs int comparisons so
// that expected integers in Go compare correctly to JSON-decoded float64 values.
func scalarEqual(want, got any) bool {
	if want == got {
		return true
	}
	// Allow int/float64 cross-comparison for numeric literals.
	wf, wIsFloat := toFloat64(want)
	gf, gIsFloat := toFloat64(got)
	if wIsFloat && gIsFloat {
		return wf == gf
	}
	return false
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
