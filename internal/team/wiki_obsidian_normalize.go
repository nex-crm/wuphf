package team

// wiki_obsidian_normalize.go rewrites loose Obsidian-typed wikilinks into the
// canonical kinded form per WIKI-OBSIDIAN-COMPATIBILITY §5. The watcher
// commit pipeline runs this against brief bodies under team/{kind}/{slug}.md
// before handing the contents to Repo.Commit.

import (
	"regexp"
	"strings"
)

// looseWikilinkPattern matches `[[target]]` or `[[target|display]]` where
// target does NOT contain a slash (kinded forms are handled by
// kindedWikilinkPattern in entity_graph.go and left untouched here). The
// pipe and display segment are optional; an `!` immediately preceding the
// `[[` makes this an image embed (`![[...]]`) which we skip.
var looseWikilinkPattern = regexp.MustCompile(`(^|[^!])\[\[([^\]|/]+)(?:\|([^\]]*))?\]\]`)

// briefPathPattern restricts normalization (and embed ingestion) to the
// seven entity-kind subtrees defined in WIKI-OBSIDIAN-COMPATIBILITY §3.
var briefPathPattern = regexp.MustCompile(`^team/(people|companies|customers|projects|learnings|decisions|playbooks)/[a-z0-9][a-z0-9-]*\.md$`)

// NormalizeLooseWikilinks rewrites every loose `[[target]]` whose target
// resolves through `resolve` to `[[kind/slug|target]]`. The display text is
// preserved exactly as the user typed it; the link target lands on the
// canonical brief path so Obsidian (and our entity graph parser) can resolve
// it deterministically.
//
// resolve(displayOrSlug) returns the kind and slug to point at and ok=false
// when the display string is ambiguous or unknown. Bare-slug ambiguity is
// the explicit guard called out in §5 of the spec — we never guess.
//
// Returns the rewritten body and a bool flag indicating whether anything
// changed; callers can skip a second commit when nothing did.
func NormalizeLooseWikilinks(body string, resolve func(displayOrSlug string) (kind EntityKind, slug string, ok bool)) (string, bool) {
	if resolve == nil {
		return body, false
	}
	changed := false
	out := looseWikilinkPattern.ReplaceAllStringFunc(body, func(match string) string {
		m := looseWikilinkPattern.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		prefix, target, display := m[1], m[2], m[3]
		trimmed := strings.TrimSpace(target)
		if trimmed == "" {
			return match
		}
		kind, slug, ok := resolve(trimmed)
		if !ok || kind == "" || slug == "" {
			return match
		}
		displayText := display
		if displayText == "" {
			displayText = target
		}
		canonical := string(kind) + "/" + slug
		var rewritten string
		if strings.TrimSpace(displayText) == canonical {
			rewritten = "[[" + canonical + "]]"
		} else {
			rewritten = "[[" + canonical + "|" + displayText + "]]"
		}
		changed = true
		return prefix + rewritten
	})
	return out, changed
}

// isBriefPath returns true when rel is a normalizable entity brief per §3.
func isBriefPath(rel string) bool {
	return briefPathPattern.MatchString(rel)
}
