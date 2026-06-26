package team

// wiki_obsidian_frontmatter.go owns the `last_human_edit_ts` sentinel the
// Obsidian watcher stamps before committing an external edit. The
// EntitySynthesizer reads this key and switches to append-mode when it is
// newer than `last_synthesized_ts`, per WIKI-OBSIDIAN-COMPATIBILITY §6.3.

import (
	"strings"
	"time"
)

const lastHumanEditKey = "last_human_edit_ts"

// applyHumanEditSentinel sets `last_human_edit_ts: <ISO-8601 UTC>` on the
// brief's existing YAML frontmatter. Files without a frontmatter block are
// returned unchanged (artifacts, freshly-typed Obsidian notes that have not
// yet been promoted to briefs — synthesizer never reads them).
//
// Field ordering is preserved: an existing key is rewritten in place; a
// missing key is appended at the end of the frontmatter block so reviewers
// see the synthesizer-owned keys at the top stay where they are.
func applyHumanEditSentinel(content string, ts time.Time) (string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return content, nil
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return content, nil
	}
	block := rest[:end]
	tail := rest[end+len("\n---"):]

	tsStr := ts.UTC().Format(time.RFC3339)
	lines := strings.Split(block, "\n")
	rewrote := false
	for i, line := range lines {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] == lastHumanEditKey {
			lines[i] = lastHumanEditKey + ": " + tsStr
			rewrote = true
			break
		}
	}
	if !rewrote {
		lines = append(lines, lastHumanEditKey+": "+tsStr)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n---")
	b.WriteString(tail)
	return b.String(), nil
}
