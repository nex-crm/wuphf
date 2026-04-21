package team

// entity_frontmatter.go owns the YAML frontmatter helpers used by the
// synthesizer to stamp brief metadata (last synthesized sha, timestamp,
// fact count) without disturbing other keys the authoring agents or
// maintainers may have added.

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Frontmatter keys owned by the synthesizer. Preserved in order so
// successive commits don't churn on reordering-only diffs.
var (
	lastSHAKey = "last_synthesized_sha"
	lastTSKey  = "last_synthesized_ts"
	factCntKey = "fact_count_at_synthesis"
)

var frontmatterKeyLine = regexp.MustCompile(`(?m)^([a-zA-Z0-9_]+):\s*(.*)$`)

// parseSynthesisFrontmatter extracts the three synthesis keys from the
// existing brief. Missing keys yield zero values. Other keys are
// ignored — preservedFrontmatterKeys handles non-synthesis keys.
func parseSynthesisFrontmatter(brief string) (sha string, ts time.Time, factCount int) {
	if !strings.HasPrefix(brief, "---\n") {
		return "", time.Time{}, 0
	}
	rest := brief[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", time.Time{}, 0
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		val := strings.TrimSpace(m[2])
		switch key {
		case lastSHAKey:
			sha = val
		case lastTSKey:
			if parsed, err := time.Parse(time.RFC3339, val); err == nil {
				ts = parsed
			}
		case factCntKey:
			factCount = parseInt(val)
		}
	}
	return sha, ts, factCount
}

// applySynthesisFrontmatter merges the three synthesis keys onto the LLM
// output. When the output already has a frontmatter block we update those
// keys in place; otherwise we prepend a fresh block that also preserves
// any non-synthesis keys from the prior brief.
func applySynthesisFrontmatter(body, headSHA string, ts time.Time, factCount int, prior string) string {
	tsStr := ts.UTC().Format(time.RFC3339)
	ours := map[string]string{
		lastSHAKey: headSHA,
		lastTSKey:  tsStr,
		factCntKey: fmt.Sprintf("%d", factCount),
	}

	preserved := preservedFrontmatterKeys(prior)

	if !strings.HasPrefix(body, "---\n") {
		return buildFrontmatterPrepend(body, ours, preserved)
	}

	// Body HAS frontmatter. Rewrite only our keys + append missing ones.
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		// Malformed — fall back to prepend path.
		return applySynthesisFrontmatter(stripFrontmatter(body), headSHA, ts, factCount, prior)
	}
	block := rest[:end]
	tail := rest[end+len("\n---"):]
	lines := strings.Split(block, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		if v, ok := ours[key]; ok {
			lines[i] = key + ": " + v
			seen[key] = true
		}
	}
	for _, key := range orderedFrontmatterKeys() {
		if !seen[key] {
			lines = append(lines, key+": "+ours[key])
		}
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n---")
	b.WriteString(tail)
	return b.String()
}

// buildFrontmatterPrepend writes a fresh frontmatter block in front of body.
func buildFrontmatterPrepend(body string, ours map[string]string, preserved []frontmatterKV) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, key := range orderedFrontmatterKeys() {
		if v, ok := ours[key]; ok {
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	for _, kv := range preserved {
		if _, mine := ours[kv.key]; mine {
			continue
		}
		b.WriteString(kv.key)
		b.WriteString(": ")
		b.WriteString(kv.val)
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	return b.String()
}

// orderedFrontmatterKeys returns the synthesis keys in a deterministic
// order so commits don't churn on reorderings.
func orderedFrontmatterKeys() []string {
	return []string{lastSHAKey, lastTSKey, factCntKey}
}

type frontmatterKV struct {
	key string
	val string
}

// preservedFrontmatterKeys lifts non-synthesis keys from a prior brief so
// we don't lose custom frontmatter when we rewrite from scratch.
func preservedFrontmatterKeys(prior string) []frontmatterKV {
	if !strings.HasPrefix(prior, "---\n") {
		return nil
	}
	rest := prior[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil
	}
	block := rest[:end]
	var out []frontmatterKV
	for _, line := range strings.Split(block, "\n") {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		switch key {
		case lastSHAKey, lastTSKey, factCntKey:
			continue
		}
		out = append(out, frontmatterKV{key: key, val: strings.TrimSpace(m[2])})
	}
	return out
}

func parseInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
