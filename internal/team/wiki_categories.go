package team

// wiki_categories.go — the article→category derived index (Phase 1 of the
// Wikipedia-style wiki IA reorg; see docs/specs/wiki-wikipedia-ia.md and
// WIKI-SCHEMA.md §4.1).
//
// Categories are a many-to-many classification layer authored in each article's
// `categories:` frontmatter. Markdown is the source of truth; the
// article_categories rows the FactStore holds are a rebuildable cache. Like
// every other index layer (facts/entities/edges/redirects) this folds into
// CanonicalHashAll and is reproduced identically by a full
// ReconcileFromMarkdown after `rm -rf index/` (the §7.4 substrate guarantee).
//
// Scope note: this slice indexes article→category memberships only. The
// category→parent (subcategory tree) edges land with the category pages that
// are their sole data source (a later phase); an empty parent table now would
// be speculative plumbing, and CREATE TABLE IF NOT EXISTS keeps that addition
// non-breaking.

import "sort"

// ArticleCategory is one (article, category) membership row in the derived
// article_categories index. ArticlePath is the wiki-root-relative markdown path
// (e.g. "team/companies/acme.md"); Category is a normalized category slug.
type ArticleCategory struct {
	ArticlePath string `json:"article_path"`
	Category    string `json:"category"`
}

// CategoryCount is a category slug with the number of articles filed under it.
// Returned by ListAllCategories; backs the category nav/API (Phase 2+).
type CategoryCount struct {
	Slug  string `json:"slug"`
	Count int    `json:"count"`
}

// parseCategoriesFrontmatter extracts the `categories:` list from an article's
// YAML frontmatter and normalizes it to stable, deduped, sorted category slugs.
// Returns nil when the article has no frontmatter or no categories. Callers
// treat nil as "clear this article's category rows" so removing a category from
// frontmatter is reflected on the next reconcile.
func parseCategoriesFrontmatter(body string) []string {
	return categoriesFromFrontmatter(extractFrontmatter(body))
}

// categoriesFromFrontmatter is the same as parseCategoriesFrontmatter but takes
// an already-extracted frontmatter block, so the reconcile path that already
// parsed the frontmatter for entity fields does not re-scan the body.
func categoriesFromFrontmatter(fm string) []string {
	if fm == "" {
		return nil
	}
	return normalizeCategories(frontmatterList(fm, "categories"))
}

// normalizeCategories slugifies each raw category value, drops blanks, dedupes,
// and sorts ascending so the derived index is deterministic regardless of
// authoring order or casing ("Revenue Operations" and "revenue-operations"
// collapse to one slug). Returns nil for an empty input.
func normalizeCategories(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		slug := categorySlug(c)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// categorySlug normalizes a single category label to a slug. It reuses the
// package's shared slugify rules (lowercase, unicode letters/digits kept, runs
// of separators collapsed to one dash) so a category slug is a valid
// `_category/<slug>` nav path segment and matches entity/concept slug grammar.
func categorySlug(s string) string {
	return slugify(s)
}
