package team

// entity_synthesizer_sentinel.go owns the Obsidian-roundtrip helpers the
// synthesizer uses to avoid stomping user edits. The Obsidian watcher stamps
// `last_human_edit_ts` on every external edit; this file reads that key,
// projects derived tags into frontmatter, and renders the append-mode
// "What we've learned" section that lands new synthesis content without
// overwriting a user-edited body.
//
// See WIKI-OBSIDIAN-COMPATIBILITY.md §6.3 (sentinel) and §7.3 (tags).

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// whatWeLearnedHeading is the canonical heading the synthesizer uses to
// append synthesis content when the user has edited the body. The block is
// wrapped in sentinels so successive synthesis runs replace the same block
// without accumulating duplicates.
const whatWeLearnedHeading = "## What we've learned"

const (
	learnedSentinelStart = "<!-- wuphf:learned:start -->"
	learnedSentinelEnd   = "<!-- wuphf:learned:end -->"
)

// maxDerivedTags caps the number of WUPHF-derived tags appended to the
// frontmatter `tags` field, per WIKI-OBSIDIAN-COMPATIBILITY §7.3.
const maxDerivedTags = 8

// parseLastHumanEditTS extracts `last_human_edit_ts` from the brief's
// frontmatter. Missing key or missing frontmatter yields the zero time.
func parseLastHumanEditTS(brief string) time.Time {
	if !strings.HasPrefix(brief, "---\n") {
		return time.Time{}
	}
	rest := brief[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return time.Time{}
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] != lastHumanEditKey {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(m[2])); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// humanEditedSince reports whether the brief has a `last_human_edit_ts`
// strictly greater than `lastSynthesizedTS`. Used to switch from rewrite
// mode to append-section mode without stomping user edits.
func humanEditedSince(brief string, lastSynthesizedTS time.Time) bool {
	hts := parseLastHumanEditTS(brief)
	if hts.IsZero() {
		return false
	}
	return hts.After(lastSynthesizedTS)
}

// applyLearnedSection writes (or rewrites) a sentinel-wrapped
// `## What we've learned` block at the end of body. The block contains the
// trimmed synthesis content. Existing user-authored body is preserved
// verbatim.
func applyLearnedSection(body, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return body
	}

	stripped := stripLearnedSection(body)
	stripped = strings.TrimRight(stripped, "\n")

	var b strings.Builder
	b.WriteString(stripped)
	if stripped != "" {
		b.WriteString("\n\n")
	}
	b.WriteString(learnedSentinelStart)
	b.WriteString("\n")
	b.WriteString(whatWeLearnedHeading)
	b.WriteString("\n\n")
	b.WriteString(content)
	b.WriteString("\n")
	b.WriteString(learnedSentinelEnd)
	b.WriteString("\n")
	return b.String()
}

// stripLearnedSection removes a previously-written sentinel-wrapped learned
// block. Returns body unchanged when no sentinels are present.
func stripLearnedSection(body string) string {
	start := strings.Index(body, learnedSentinelStart)
	if start < 0 {
		return body
	}
	after := body[start+len(learnedSentinelStart):]
	end := strings.Index(after, learnedSentinelEnd)
	if end < 0 {
		return body
	}
	tail := after[end+len(learnedSentinelEnd):]
	return strings.TrimRight(body[:start], "\n") + tail
}

// deriveTagsFromBrief reads `kind`, the indented `signals` map, and (for
// playbooks) `author` from the brief's frontmatter and returns a normalised,
// deduplicated, capped tag set. The order is deterministic: kind first,
// then signals in fixed order, then author. Empty values are skipped.
func deriveTagsFromBrief(brief string) []string {
	if !strings.HasPrefix(brief, "---\n") {
		return nil
	}
	rest := brief[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil
	}
	block := rest[:end]

	var (
		kind, jobTitle, domain, author string
	)
	inSignals := false
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if !inSignals {
				continue
			}
			trimmed := strings.TrimSpace(line)
			m := frontmatterKeyLine.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			switch m[1] {
			case "job_title":
				jobTitle = strings.TrimSpace(m[2])
			case "domain":
				domain = strings.TrimSpace(m[2])
			}
			continue
		}
		inSignals = false
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		val := strings.TrimSpace(m[2])
		switch key {
		case "kind":
			kind = val
		case "author":
			author = val
		case "signals":
			if val == "" {
				inSignals = true
			}
		}
	}

	out := make([]string, 0, 4)
	if t := normalizeTag(kind); t != "" {
		out = append(out, t)
	}
	if t := normalizeTag(jobTitle); t != "" {
		out = append(out, t)
	}
	if t := normalizeTag(domainHost(domain)); t != "" {
		out = append(out, t)
	}
	if t := normalizeTag(author); t != "" {
		out = append(out, t)
	}
	out = dedupePreserveOrder(out)
	if len(out) > maxDerivedTags {
		out = out[:maxDerivedTags]
	}
	return out
}

// domainHost reduces a domain or URL string to its bare host. Returns "" for
// inputs that don't have a recoverable host. Strips `www.` for tidiness.
func domainHost(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	if strings.Contains(in, "://") {
		if u, err := url.Parse(in); err == nil && u.Host != "" {
			return strings.TrimPrefix(u.Host, "www.")
		}
		return ""
	}
	// Bare host or host/path. Take the first path segment off.
	if i := strings.IndexAny(in, "/?#"); i >= 0 {
		in = in[:i]
	}
	return strings.TrimPrefix(in, "www.")
}

var tagAllowedRune = regexp.MustCompile(`[^a-z0-9._/-]+`)

// normalizeTag lowercases, replaces whitespace with hyphens, drops
// disallowed characters (keeping `.`, `_`, `/`, `-` — Obsidian-compatible
// tag punctuation), and trims leading/trailing punctuation.
func normalizeTag(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), "-")
	s = tagAllowedRune.ReplaceAllString(s, "")
	s = strings.Trim(s, "-_./")
	return s
}

// dedupePreserveOrder drops duplicates while keeping first-seen order.
func dedupePreserveOrder(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// parseTagsFromFrontmatter reads any existing `tags` entries (flow `[a, b]`
// or block sequence form) and returns the normalized list. Returns nil when
// the field is absent.
func parseTagsFromFrontmatter(brief string) []string {
	if !strings.HasPrefix(brief, "---\n") {
		return nil
	}
	rest := brief[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil
	}
	block := rest[:end]
	lines := strings.Split(block, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil || m[1] != "tags" {
			continue
		}
		val := strings.TrimSpace(m[2])
		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			inner := strings.TrimSuffix(strings.TrimPrefix(val, "["), "]")
			for _, p := range strings.Split(inner, ",") {
				if t := strings.Trim(strings.TrimSpace(p), `"'`); t != "" {
					out = append(out, t)
				}
			}
			break
		}
		if val == "" {
			for j := i + 1; j < len(lines); j++ {
				next := lines[j]
				trimmed := strings.TrimLeft(next, " \t")
				if !strings.HasPrefix(trimmed, "- ") {
					break
				}
				if t := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), `"'`); t != "" {
					out = append(out, t)
				}
			}
		}
		break
	}
	return out
}

// applyTagsFrontmatter writes a `tags` block into body's frontmatter,
// merging derived tags with any pre-existing user tags. User tags are
// preserved verbatim; derived tags are normalized and capped before merge.
// Output uses YAML flow sequence form for compact, diff-friendly rendering.
//
// body MUST already have a frontmatter block (the synthesizer pipeline
// guarantees this — applySynthesisFrontmatter always emits one).
func applyTagsFrontmatter(body string, derived []string) string {
	existing := parseTagsFromFrontmatter(body)
	derivedNorm := make([]string, 0, len(derived))
	for _, t := range derived {
		if n := normalizeTag(t); n != "" {
			derivedNorm = append(derivedNorm, n)
		}
	}
	if len(derivedNorm) > maxDerivedTags {
		derivedNorm = derivedNorm[:maxDerivedTags]
	}

	merged := dedupePreserveOrder(append(append([]string{}, existing...), derivedNorm...))
	if len(merged) == 0 {
		return body
	}

	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return body
	}
	block := rest[:end]
	tail := rest[end+len("\n---"):]

	lines := strings.Split(block, "\n")
	rendered := renderTagsLine(merged)

	rewrote := false
	out := make([]string, 0, len(lines)+1)
	skipBlockSeq := false
	for _, line := range lines {
		if skipBlockSeq {
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, "- ") {
				continue
			}
			skipBlockSeq = false
		}
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m != nil && m[1] == "tags" {
			out = append(out, rendered)
			rewrote = true
			if strings.TrimSpace(m[2]) == "" {
				skipBlockSeq = true
			}
			continue
		}
		out = append(out, line)
	}
	if !rewrote {
		out = append(out, rendered)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.Join(out, "\n"))
	b.WriteString("\n---")
	b.WriteString(tail)
	return b.String()
}

// renderTagsLine returns a flow-sequence YAML line for the tag set, with a
// stable sort so re-running synthesis with the same inputs is byte-stable.
func renderTagsLine(tags []string) string {
	sorted := append([]string{}, tags...)
	sort.Strings(sorted)
	return "tags: [" + strings.Join(sorted, ", ") + "]"
}
