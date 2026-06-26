package team

// wiki_compile_citation.go — deterministic, post-author citation validation.
// The Phase-2 contract requires every factual sentence to end with a
// ^[source-id] marker naming a REAL source feeding the page. This pass scans a
// generated article for those markers and flags two classes of problem:
//
//   - an unknown id: a ^[id] that names no source actually feeding the page
//     (a hallucinated citation), and
//   - an uncited page: an article body with zero citation markers at all.
//
// Violations are collected as warnings and surfaced via
// CompileResult.CitationWarnings; they NEVER hard-fail the compile. The wiki is
// best-effort knowledge, not a gate.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// citationMarkerRe matches a ^[source-id] citation marker. The id is any run of
// characters up to the closing bracket; it is trimmed before validation.
var citationMarkerRe = regexp.MustCompile(`\^\[([^\]]+)\]`)

// validateCitations checks the markers in body against the set of source ids
// genuinely feeding the page and returns a sorted, deduped slice of warnings.
// slug names the page in each warning so the run log is actionable. An empty
// return means the page's citations are clean.
func validateCitations(slug, body string, validIDs []string) []string {
	valid := make(map[string]struct{}, len(validIDs))
	for _, id := range validIDs {
		valid[strings.TrimSpace(id)] = struct{}{}
	}

	matches := citationMarkerRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return []string{fmt.Sprintf("page %q has no citations", slug)}
	}

	unknown := make(map[string]struct{})
	for _, m := range matches {
		id := strings.TrimSpace(m[1])
		if id == "" {
			continue
		}
		if _, ok := valid[id]; !ok {
			unknown[id] = struct{}{}
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	out := make([]string, 0, len(unknown))
	for id := range unknown {
		out = append(out, fmt.Sprintf("page %q cites unknown source %q", slug, id))
	}
	sort.Strings(out)
	return out
}
