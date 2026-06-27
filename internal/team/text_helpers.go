package team

import "strings"

// text_helpers.go holds small, dependency-free markdown/string helpers that
// are shared across the wiki, entity, playbook, and scheduler surfaces. They
// were previously colocated with the (now removed) notebook/promotion files;
// they live here so the kept callers keep compiling.

// stripFrontmatter returns the body with a leading YAML frontmatter block
// removed. Used when building wiki articles so the copy doesn't inherit the
// source's metadata keys.
func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	rest := body[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return body
	}
	return strings.TrimLeft(rest[idx+len("\n---\n"):], "\n")
}

// headerLineFrom returns the first markdown H1 line from a body, or "" when
// none is found. Used for nicer commit messages / UI; not load-bearing.
func headerLineFrom(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}

// firstLine returns the first non-empty line of s, or "(no rationale)".
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return "(no rationale)"
}

// markdownTitle extracts the first H1 from a body (ignoring frontmatter),
// falling back to a humanized slug or "Review" when no heading exists.
func markdownTitle(body, fallbackSlug string) string {
	for _, line := range strings.Split(stripFrontmatter(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	if fallbackSlug == "" {
		return "Review"
	}
	return titleFromSlug(fallbackSlug)
}

// titleFromSlug humanizes a kebab/snake slug into a Title Cased string.
func titleFromSlug(slug string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// disabledOrSleeping renders a scheduler heartbeat status string.
func disabledOrSleeping(enabled bool) string {
	if enabled {
		return "sleeping"
	}
	return "disabled"
}
