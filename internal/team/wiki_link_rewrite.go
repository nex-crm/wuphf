package team

// wiki_link_rewrite.go — the pure [[wikilink]] reference-rewriting engine that
// powers page move/rename/reparent (wiki_page_ops.go). It is deliberately
// side-effect free: it takes a snapshot of article bodies, a from→to mapping,
// and a slug resolver, and returns the changed bodies. The HTTP/git layer owns
// the filesystem and commit; this file owns the string surgery.
//
// Why a dedicated engine
// ======================
//
// A naive "find every [[from-basename]] and swap the basename" rewrite is
// wrong: two pages can share a basename (team/people/nazz.md AND
// team/companies/nazz.md), and a bare slug [[nazz]] resolves to exactly ONE of
// them via the fixed candidate-directory order (see candidateRelPaths). When
// people/nazz moves, [[nazz]] must be rewritten; when companies/nazz moves,
// [[nazz]] must NOT be touched because it still resolves to people/nazz. The
// resolver is the single source of truth for that decision — rewrite-time
// resolution is byte-identical to click-time resolution (web candidatePaths),
// so a link we rewrite is exactly the link a reader would have followed.
//
// Slug FORM preservation
// ======================
//
// Authors write links in three forms and we keep the form they chose:
//
//   - full path:  [[team/people/nazz.md]]  → [[team/companies/nazz.md]]
//   - kinded slug: [[people/nazz]]          → [[companies/nazz]]
//   - bare slug:   [[nazz]]                  → [[nazz]] when the bare slug still
//     unambiguously resolves to the NEW path; otherwise it falls back to the
//     new kinded slug (people/foo) so the link keeps pointing at the right page.
//
// Display text ([[slug|Display]]) is always preserved verbatim.

import (
	"path/filepath"
	"regexp"
	"strings"
)

// linkRewriteResolver maps a wikilink slug to the canonical repo-root-relative
// article path it points at (team/<...>.md), given the current set of articles.
// fromArticlePath is the path of the article the link lives in; it is reserved
// for future relative-slug support and is currently unused by callers, which
// resolve slugs globally exactly as the web client does.
//
// A return of "" means the slug does not resolve to any known article (a broken
// link); such links are left untouched by the rewrite.
type linkRewriteResolver func(slug, fromArticlePath string) string

// rewriteWikilinkInner is the single-token regex: it matches the inner text of
// one [[...]] occurrence so we can rewrite the slug while leaving the
// surrounding bytes (including any |Display) under our explicit control.
//
// Group 1: the inner text between [[ and ]]. The character class MUST stay
// byte-identical to the web buildReplacements grammar (web/src/lib/wikilink.ts,
// /\[\[([^\]\n]+)\]\]/g): forbid only ']' and newlines, NOT '['. Rewrite-time
// resolution has to match click-time resolution, so the inner class must accept
// the exact same slugs the web client would parse — including a stray '[' inside
// the link (e.g. [[a[b]]). Display text is split off in code, not in the regex,
// so the form-preservation rules live in one place.
var rewriteWikilinkInner = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)

// rewriteWikilinks rewrites every [[wikilink]] across articles whose slug
// resolves to `from` so it resolves to `to` instead, preserving display text
// and the author's slug form.
//
//   - articles maps repo-root-relative article path → full file content.
//   - from / to are repo-root-relative article paths (team/<...>.md).
//   - resolveFrom resolves a slug against the PRE-move world (where `from`
//     still exists and `to` may not). It is the single source of truth for
//     deciding which links point at the moved page, and therefore for
//     same-basename collision handling: [[nazz]] only rewrites when it
//     resolves to the page that moved, not to a sibling that shares its
//     basename.
//   - resolveTo resolves a slug against the POST-move world (where `from` is
//     gone and `to` exists). It is consulted only to decide whether a bare
//     slug can STAY bare after the move (i.e. the bare basename of `to` still
//     resolves to `to` and is not shadowed by another page).
//
// Returns the subset of articles whose content changed (path → new content) and
// the total number of individual links rewritten. The input map is never
// mutated; changed entries are fresh strings.
//
// The `from` article itself is included in the scan when present in `articles`:
// a self-link or a sibling link inside the moved page is rewritten the same way
// as any other reference, so the moved file stays internally consistent.
func rewriteWikilinks(
	articles map[string]string,
	from, to string,
	resolveFrom, resolveTo linkRewriteResolver,
) (map[string]string, int) {
	return rewriteWikilinksMulti(
		articles,
		[]pageMove{{From: from, To: to}},
		resolveFrom, resolveTo,
	)
}

// pageMove is one from→to page remapping. A single rename/reparent produces
// one pageMove; a directory move produces one per descendant .md page.
type pageMove struct {
	From string
	To   string
}

// rewriteWikilinksMulti applies a batch of from→to page remappings (the
// directory-cascade case) in a single pass. resolveFrom resolves against the
// pre-move world (all `From` paths still exist); resolveTo against the
// post-move world (all `To` paths exist, every `From` gone). A link is
// rewritten when its pre-move resolution matches any move's From; it is then
// rewritten toward that move's To, preserving the author's slug form.
//
// Returns the changed articles (path → new content) and the total link count.
func rewriteWikilinksMulti(
	articles map[string]string,
	moves []pageMove,
	resolveFrom, resolveTo linkRewriteResolver,
) (map[string]string, int) {
	toByFrom := make(map[string]string, len(moves))
	for _, m := range moves {
		from := filepath.ToSlash(strings.TrimSpace(m.From))
		to := filepath.ToSlash(strings.TrimSpace(m.To))
		if from == "" || to == "" || from == to {
			continue
		}
		toByFrom[from] = to
	}
	changed := make(map[string]string)
	total := 0
	if len(toByFrom) == 0 {
		return changed, total
	}
	for path, content := range articles {
		newContent, n := rewriteOneMulti(content, path, toByFrom, resolveFrom, resolveTo)
		if n > 0 {
			changed[path] = newContent
			total += n
		}
	}
	return changed, total
}

// rewriteOneMulti is rewriteOne generalised over a from→to map.
func rewriteOneMulti(
	content, articlePath string,
	toByFrom map[string]string,
	resolveFrom, resolveTo linkRewriteResolver,
) (string, int) {
	count := 0
	out := rewriteWikilinkInner.ReplaceAllStringFunc(content, func(match string) string {
		inner := match[2 : len(match)-2]
		slugPart, displayPart, hasDisplay := splitWikilinkInner(inner)
		slug := strings.TrimSpace(slugPart)
		if slug == "" || strings.Count(inner, "|") > 1 || !linkSlugValid(slug) {
			return match
		}
		from := resolveFrom(slug, articlePath)
		to, ok := toByFrom[from]
		if !ok {
			return match
		}
		newSlug := rewrittenSlugForm(slug, to, resolveTo, articlePath)
		if newSlug == "" || newSlug == slug {
			return match
		}
		count++
		if hasDisplay {
			return "[[" + newSlug + "|" + displayPart + "]]"
		}
		return "[[" + newSlug + "]]"
	})
	return out, count
}

// splitWikilinkInner splits the inner text of a [[...]] token into its slug and
// optional display segments. The boundary is the FIRST pipe; everything after
// it (including further pipes, which callers reject as invalid) is the display.
// hasDisplay is true when a pipe was present, so the caller can reconstruct the
// exact form even when the display equals the slug.
func splitWikilinkInner(inner string) (slug, display string, hasDisplay bool) {
	idx := strings.IndexByte(inner, '|')
	if idx < 0 {
		return inner, "", false
	}
	return inner[:idx], inner[idx+1:], true
}

// rewrittenSlugForm computes the replacement slug, preserving the author's
// chosen FORM:
//
//   - full path form (starts with team/, ends .md): emit the new full path.
//   - kinded slug (contains '/'): emit the new kinded slug (to minus team/ and
//     .md). This is the from-without-team/.md → to-without-team/.md swap.
//   - bare slug (no '/'): keep it bare when the bare basename of `to` resolves
//     unambiguously back to `to`; otherwise fall back to the new kinded slug so
//     the link cannot silently break or point at a colliding page.
//
// resolveTo resolves slugs against the POST-move world. slug is already
// validated and known to resolve to `from` in the pre-move world.
func rewrittenSlugForm(
	slug, to string,
	resolveTo linkRewriteResolver,
	articlePath string,
) string {
	switch {
	case isFullPathSlug(slug):
		// Author wrote the whole path; mirror it with the destination path.
		return to
	case strings.Contains(slug, "/"):
		// Kinded slug like people/nazz → companies/nazz.
		return slugFromRelPath(to)
	default:
		// Bare slug like nazz. Prefer to keep it bare, but only if the bare
		// basename of the destination resolves back to the destination in the
		// post-move world — i.e. no other page shadows it in the candidate
		// order.
		bare := bareSlugFromRelPath(to)
		if bare != "" && resolveTo(bare, articlePath) == to {
			return bare
		}
		// Ambiguous (some other page wins the bare slug) or unresolvable:
		// fall back to the kinded slug so the link still points at `to`.
		return slugFromRelPath(to)
	}
}

// isFullPathSlug reports whether a slug was written as a full article path
// (team/<...>.md). These are passed through candidatePaths unchanged on the web
// side, so we treat them as the explicit full-path form.
func isFullPathSlug(slug string) bool {
	s := filepath.ToSlash(strings.TrimSpace(slug))
	return strings.HasPrefix(s, "team/") && strings.HasSuffix(strings.ToLower(s), ".md")
}

// slugFromRelPath converts a repo-root-relative article path to its kinded slug
// form: team/people/nazz.md → people/nazz. Returns "" when the path is not a
// team/<...>.md article.
func slugFromRelPath(relPath string) string {
	rel := filepath.ToSlash(strings.TrimSpace(relPath))
	if !strings.HasPrefix(rel, "team/") {
		return ""
	}
	rel = strings.TrimPrefix(rel, "team/")
	if !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return ""
	}
	return rel[:len(rel)-len(".md")]
}

// bareSlugFromRelPath returns just the basename (no directory, no .md) of an
// article path: team/companies/nazz.md → nazz. Returns "" for non-articles.
func bareSlugFromRelPath(relPath string) string {
	kinded := slugFromRelPath(relPath)
	if kinded == "" {
		return ""
	}
	if idx := strings.LastIndexByte(kinded, '/'); idx >= 0 {
		return kinded[idx+1:]
	}
	return kinded
}

// linkCandidateDirs is the fixed candidate-directory priority order used to
// resolve a bare wikilink slug to a canonical article path. It MUST stay
// byte-identical to web/src/api/wiki.ts candidatePaths so rewrite-time
// resolution equals click-time resolution. Changing this order silently
// re-points existing bare links — treat it as a wire contract.
var linkCandidateDirs = []string{
	"team/people/",
	"team/companies/",
	"team/playbooks/",
	"team/decisions/",
	"team/projects/",
	"team/",
}

// candidateRelPaths returns the ordered list of repo-root-relative article
// paths a wikilink slug could resolve to, mirroring web candidatePaths exactly:
//
//   - empty after trimming slashes → no candidates.
//   - already team/-prefixed → that single path (with .md appended if absent).
//   - any other slash-containing slug → team/<slug>.md (single candidate).
//   - bare slug → the candidate dirs in priority order.
//
// The caller picks the first candidate that exists; that is the canonical
// target. The returned paths are slash-form and carry the .md suffix.
func candidateRelPaths(slug string) []string {
	trimmed := strings.Trim(filepath.ToSlash(strings.TrimSpace(slug)), "/")
	if trimmed == "" {
		return nil
	}
	withExt := trimmed
	if !strings.HasSuffix(strings.ToLower(withExt), ".md") {
		withExt += ".md"
	}
	if strings.HasPrefix(trimmed, "team/") {
		return []string{withExt}
	}
	if strings.Contains(trimmed, "/") {
		return []string{"team/" + withExt}
	}
	out := make([]string, 0, len(linkCandidateDirs))
	for _, dir := range linkCandidateDirs {
		out = append(out, dir+withExt)
	}
	return out
}

// newLinkResolver builds a linkRewriteResolver over an existence predicate.
// exists reports whether a repo-root-relative article path currently names a
// real article. The resolver returns the FIRST existing candidate path for a
// slug, exactly as the web client's fetchArticle loop does, or "" when none
// exist (a broken link). Invalid slugs (traversal/absolute) never resolve.
func newLinkResolver(exists func(relPath string) bool) linkRewriteResolver {
	return func(slug, _ string) string {
		if !linkSlugValid(strings.TrimSpace(slug)) {
			return ""
		}
		for _, cand := range candidateRelPaths(slug) {
			if exists(cand) {
				return cand
			}
		}
		return ""
	}
}

// linkSlugValid mirrors web/src/lib/wikilink.ts + validSlug: reject empty,
// absolute, and traversal slugs. Such slugs never name a real article so they
// can never be a rewrite target.
func linkSlugValid(slug string) bool {
	if slug == "" {
		return false
	}
	if strings.HasPrefix(slug, "/") {
		return false
	}
	if strings.Contains(slug, "..") {
		return false
	}
	for i := 0; i < len(slug); i++ {
		if slug[i] <= 0x1f {
			return false
		}
	}
	return true
}
