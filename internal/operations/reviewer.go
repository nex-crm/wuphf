package operations

import "strings"

// ResolveReviewer returns the agent slug that should review a promotion
// for the given wiki path. It walks ReviewerPaths in declaration order
// (first match wins), falls through to DefaultReviewer, and finally to
// ReviewerFallback ("ceo") when nothing else is configured.
//
// A return value of ReviewerHumanOnly ("human-only") means agent approval
// is disabled and the promotion must wait for a human click.
func (b *Blueprint) ResolveReviewer(wikiPath string) string {
	path := strings.TrimSpace(wikiPath)
	path = strings.TrimPrefix(path, "/")
	for _, rule := range b.ReviewerPaths {
		pattern := rule.Pattern
		if pattern == "" {
			continue
		}
		if matchGlob(pattern, path) {
			if rule.Reviewer == "" {
				continue
			}
			return rule.Reviewer
		}
	}
	if reviewer := strings.TrimSpace(b.DefaultReviewer); reviewer != "" {
		return reviewer
	}
	return ReviewerFallback
}

// matchGlob reports whether name matches pattern. Supported syntax:
//
//   - "*" matches any run of characters within a single path segment
//     (no "/" crossing).
//   - "**" matches any number of path segments, including zero.
//   - Everything else is literal.
//
// This mirrors the gitignore / .dockerignore mental model so blueprint
// authors can write `team/playbooks/**` and have it behave as expected.
func matchGlob(pattern, name string) bool {
	return globMatch(splitGlob(pattern), splitPath(name))
}

// splitPath segments a forward-slash path, dropping empty segments so
// leading/trailing "/" doesn't break matching.
func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	raw := strings.Split(p, "/")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		if seg == "" {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// splitGlob segments a pattern and flags each segment as "**" or a
// regular glob segment.
func splitGlob(p string) []string {
	return splitPath(p)
}

// globMatch is a recursive segment-by-segment matcher. patterns is the
// segmented glob, parts is the segmented input path.
func globMatch(patterns, parts []string) bool {
	for i := 0; i < len(patterns); i++ {
		seg := patterns[i]
		if seg == "**" {
			rest := patterns[i+1:]
			if len(rest) == 0 {
				return true
			}
			for j := 0; j <= len(parts); j++ {
				if globMatch(rest, parts[j:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		if !segmentMatch(seg, parts[0]) {
			return false
		}
		parts = parts[1:]
	}
	return len(parts) == 0
}

// segmentMatch compares a single glob segment (may contain "*") against
// a single path segment. "*" consumes zero or more characters within the
// segment but never crosses "/".
func segmentMatch(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	if parts[0] != "" {
		if !strings.HasPrefix(name, parts[0]) {
			return false
		}
		name = name[len(parts[0]):]
	}
	for i := 1; i < len(parts)-1; i++ {
		chunk := parts[i]
		if chunk == "" {
			continue
		}
		idx := strings.Index(name, chunk)
		if idx < 0 {
			return false
		}
		name = name[idx+len(chunk):]
	}
	last := parts[len(parts)-1]
	if last == "" {
		return true
	}
	return strings.HasSuffix(name, last)
}
