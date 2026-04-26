package team

// entity_minimal_brief.go — deterministic placeholder brief for ghost
// entities the extractor mints. Closes the §7.4 substrate-rebuild gap:
// every ghost-entity row that lands in the in-memory index also lands as
// markdown on disk, so a wipe + ReconcileFromMarkdown produces a
// logically-identical state.
//
// The brief is intentionally minimal — no synthesized prose, no related-
// entity bullets, no timestamps in the body. The only time field is
// frontmatter `created_at`, which the extractor pins to a deterministic
// `e.now()` at the call site so byte-identical output requires only
// byte-identical input.

import (
	"sort"
	"strings"
	"time"
)

// MinimalBrief returns the canonical placeholder brief content for a
// freshly-minted (ghost) entity that has no synthesized facts yet. The
// output is deterministic for a given IndexEntity — same input always
// produces byte-identical output, so substrate-rebuild round-trips
// (§7.4) hold.
//
// Fields included: frontmatter (slug, canonical_slug, kind, aliases
// sorted ascending, signals normalized), a single H1, a Signals stub,
// and an archivist byline. No timestamps in the body — the
// frontmatter's created_at is the only time field, and it pins to the
// IndexEntity's CreatedAt (already deterministic at the call site).
func MinimalBrief(ent IndexEntity) string {
	var b strings.Builder

	// Frontmatter — fixed key order so two runs over the same input
	// always emit byte-identical bytes.
	b.WriteString("---\n")
	b.WriteString("slug: ")
	b.WriteString(ent.Slug)
	b.WriteString("\n")

	canonical := ent.CanonicalSlug
	if canonical == "" {
		canonical = ent.Slug
	}
	b.WriteString("canonical_slug: ")
	b.WriteString(canonical)
	b.WriteString("\n")

	b.WriteString("kind: ")
	b.WriteString(ent.Kind)
	b.WriteString("\n")

	aliases := sortedAliasesAsc(ent.Aliases)
	if len(aliases) > 0 {
		b.WriteString("aliases:\n")
		for _, a := range aliases {
			b.WriteString("  - ")
			b.WriteString(a)
			b.WriteString("\n")
		}
	}

	createdAt := ent.CreatedAt
	if createdAt.IsZero() {
		// Defensive: an unset CreatedAt would still serialize as "0001-01-01..."
		// which is at least deterministic, but call sites are expected to pass
		// a non-zero value (extractor uses e.now()). The UTC normalisation
		// makes the output time-zone-independent for §7.4.
		createdAt = time.Time{}
	}
	b.WriteString("created_at: ")
	b.WriteString(createdAt.UTC().Format(time.RFC3339))
	b.WriteString("\n")

	createdBy := ent.CreatedBy
	if createdBy == "" {
		createdBy = ArchivistAuthor
	}
	b.WriteString("created_by: ")
	b.WriteString(createdBy)
	b.WriteString("\n")
	b.WriteString("---\n\n")

	// H1 — Signals.PersonName preferred, else humanised canonical slug.
	title := strings.TrimSpace(ent.Signals.PersonName)
	if title == "" {
		title = humanizeSlugForBrief(canonical)
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Signals stub — fixed field order, skip empty fields.
	b.WriteString("## Signals\n\n")
	bullets := signalBullets(ent.Signals)
	if len(bullets) == 0 {
		b.WriteString("- (none)\n")
	} else {
		for _, line := range bullets {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Disclaimer line — italicised so the next synthesis can replace
	// the body without leaving conflicting prose. Fixed wording so
	// substrate-rebuild round-trips hold.
	b.WriteString("_This page was auto-created when the team encountered a new entity. Facts will be synthesized here as they accumulate._\n")

	return b.String()
}

// sortedAliasesAsc returns a copy of in sorted case-insensitively. Empty
// strings are dropped. Returns nil for empty input so callers can rely on
// `len(...) == 0` for the "no aliases" branch.
func sortedAliasesAsc(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, a := range in {
		if strings.TrimSpace(a) == "" {
			continue
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		li := strings.ToLower(out[i])
		lj := strings.ToLower(out[j])
		if li != lj {
			return li < lj
		}
		// Stable secondary sort on raw bytes so a case-only tie (e.g.
		// "Alice" vs "alice") still produces a deterministic order.
		return out[i] < out[j]
	})
	return out
}

// signalBullets returns the non-empty Signals fields as `- key: value`
// lines, in fixed order. Empty fields are skipped entirely (no orphan
// `- email:` lines).
func signalBullets(s Signals) []string {
	var out []string
	if v := strings.TrimSpace(s.Email); v != "" {
		out = append(out, "- email: "+v)
	}
	if v := strings.TrimSpace(s.Domain); v != "" {
		out = append(out, "- domain: "+v)
	}
	if v := strings.TrimSpace(s.PersonName); v != "" {
		out = append(out, "- person_name: "+v)
	}
	if v := strings.TrimSpace(s.JobTitle); v != "" {
		out = append(out, "- job_title: "+v)
	}
	return out
}

// humanizeSlugForBrief is the same semantics as the broker's humanizeSlug
// (broker.go) but local to the brief writer to avoid pulling broker code
// into the brief render path. Slugs that flow here are ASCII per
// ghostBriefSlugPattern, so the byte-slice title-case is safe.
func humanizeSlugForBrief(slug string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
