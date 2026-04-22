// Package team — wiki article metadata.
//
// BuildArticle computes the rich view a UI needs for an article page:
// content + extracted title + backlinks + revision history + word count.
// It is read-only from the caller's perspective; the scan walks team/ once
// per request. At v1 corpus sizes (≤ ~500 articles) the walk runs in tens
// of milliseconds.
//
// Data flow:
//
//   BuildArticle(relPath)
//     │
//     ├── read article bytes        (O(article size))
//     ├── extract title (first H1)  (O(article size))
//     ├── Log(relPath)              (O(revisions))   → revisions, contributors, last edit
//     ├── walk team/*.md            (O(all articles))
//     │     └── parseWikilinkTargets
//     │           └── collect links pointing at relPath
//     └── return ArticleMeta
//
// Wikilink grammar (must match the shared fixture in web/tests/fixtures/wikilinks.json
// and the TypeScript parser in web/src/lib/wikilink.ts):
//
//     [[slug]]         → {slug: "slug",         display: "slug"}
//     [[slug|Display]] → {slug: "slug",         display: "Display"}
//     [[ ]]            → invalid, skip
//     [[a|b|c]]        → invalid (extra pipe), skip
//     [[../...]]       → invalid (path traversal), skip
//     [[/absolute]]    → invalid (absolute), skip
//
// Slug → article relpath mapping: "people/nazz" → "team/people/nazz.md".
// Article relpath → slug: strip "team/" prefix + ".md" suffix.

package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ArticleMeta is the rich view sent to the UI for an article.
// The JSON shape matches web/src/api/wiki.ts WikiArticle.
type ArticleMeta struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	LastEditedBy string `json:"last_edited_by"`
	LastEditedTs string `json:"last_edited_ts"`
	// CommitSHA is the short SHA of the most recent commit touching this
	// article. The editor sends it back as expected_sha on save so the
	// broker can detect conflicting writes that landed after the editor
	// opened. Empty when the article has no commit history yet.
	CommitSHA    string     `json:"commit_sha"`
	Revisions    int        `json:"revisions"`
	Contributors []string   `json:"contributors"`
	Backlinks    []Backlink `json:"backlinks"`
	WordCount    int        `json:"word_count"`
	Categories   []string   `json:"categories"`
}

// Backlink represents another article that wikilinks to this article.
// The JSON shape matches web/src/api/wiki.ts WikiArticle.backlinks[].
type Backlink struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	AuthorSlug string `json:"author_slug"`
}

// CatalogEntry is a single article in the /wiki/catalog response.
// The JSON shape matches web/src/api/wiki.ts WikiCatalogEntry.
type CatalogEntry struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	AuthorSlug   string `json:"author_slug"`
	LastEditedTs string `json:"last_edited_ts"`
	Group        string `json:"group"`
}

// BuildCatalog walks team/ and returns every .md article with title + author +
// last-edit metadata grouped by top-level thematic dir.
//
// Shape matches web/src/api/wiki.ts WikiCatalogEntry. Sorted by path for
// reproducibility; the UI re-sorts by recency within each group.
func (r *Repo) BuildCatalog(ctx context.Context) ([]CatalogEntry, error) {
	teamDir := r.TeamDir()
	var entries []CatalogEntry

	walkErr := filepath.WalkDir(teamDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// team/inbox/ holds raw ingested source material (scanner dumps),
			// not curated wiki content. Excluding it here keeps the catalog
			// and UI focused on synthesized briefs/playbooks/decisions; the
			// raw files are still reachable by direct path via /wiki/read
			// for agents that want to cite them.
			rel, relErr := filepath.Rel(r.Root(), path)
			if relErr == nil && filepath.ToSlash(rel) == "team/inbox" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(r.Root(), path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		entry := CatalogEntry{
			Path:  rel,
			Title: extractTitle(content, rel),
			Group: groupFromPath(rel),
		}
		if refs, err := r.Log(ctx, rel); err == nil && len(refs) > 0 {
			entry.AuthorSlug = refs[0].Author
			entry.LastEditedTs = refs[0].Timestamp.Format("2006-01-02T15:04:05Z07:00")
		}
		entries = append(entries, entry)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("wiki: walk team/: %w", walkErr)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// groupFromPath returns the first subdir under team/ (e.g. "team/people/x.md"
// → "people"). Used to group catalog entries in the UI.
func groupFromPath(relPath string) string {
	rel := filepath.ToSlash(relPath)
	rel = strings.TrimPrefix(rel, "team/")
	idx := strings.Index(rel, "/")
	if idx <= 0 {
		return "root"
	}
	return rel[:idx]
}

// wikilinkPattern captures `[[slug|Display]]` and `[[slug]]`.
// Group 1: slug. Group 2: optional `|Display` (including the pipe).
// Group 3: display text without the pipe.
// Invalid forms (empty, extra pipes, path traversal) are filtered post-match.
var wikilinkPattern = regexp.MustCompile(`\[\[([^\[\]|]+)(\|([^\[\]|]+))?\]\]`)

// BuildArticle reads an article and computes its metadata + backlinks.
// Returns os.ErrNotExist wrapped if the article is missing.
func (r *Repo) BuildArticle(ctx context.Context, relPath string) (ArticleMeta, error) {
	if err := validateArticlePath(relPath); err != nil {
		return ArticleMeta{}, err
	}

	content, err := readArticle(r, relPath)
	if err != nil {
		return ArticleMeta{}, err
	}

	meta := ArticleMeta{
		Path:         relPath,
		Content:      string(content),
		Title:        extractTitle(content, relPath),
		WordCount:    countWords(content),
		Contributors: []string{},
		Backlinks:    []Backlink{},
		Categories:   []string{},
	}

	// Revision history and last-edit info (via git log).
	if refs, err := r.Log(ctx, relPath); err == nil && len(refs) > 0 {
		meta.Revisions = len(refs)
		meta.LastEditedBy = refs[0].Author
		meta.LastEditedTs = refs[0].Timestamp.Format("2006-01-02T15:04:05Z07:00")
		meta.CommitSHA = refs[0].SHA
		meta.Contributors = uniqueAuthors(refs)
	}

	// Backlinks: walk team/ and collect articles that reference this one.
	backs, err := r.backlinksFor(ctx, relPath)
	if err != nil {
		// Non-fatal: surface the article without backlinks rather than 500.
		// The UI degrades gracefully.
		return meta, nil
	}
	meta.Backlinks = backs

	return meta, nil
}

// backlinksFor walks team/ and returns all articles that wikilink to target.
// Does NOT hold r.mu: read-only filesystem access, safe to race with writes
// (the worker's commit serialization means mid-walk state is at worst one
// article stale — acceptable for a reverse index).
func (r *Repo) backlinksFor(_ context.Context, target string) ([]Backlink, error) {
	targetSlug := relPathToSlug(target)
	if targetSlug == "" {
		return nil, fmt.Errorf("wiki: target has no slug mapping: %q", target)
	}
	teamDir := r.TeamDir()

	type hit struct {
		relPath string
		title   string
	}
	var hits []hit

	walkErr := filepath.WalkDir(teamDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than abort
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(r.Root(), path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == target {
			return nil // skip self-references
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		targets := parseWikilinkTargets(content)
		for _, t := range targets {
			if t == targetSlug {
				hits = append(hits, hit{
					relPath: rel,
					title:   extractTitle(content, rel),
				})
				break
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("wiki: walk team/: %w", walkErr)
	}

	// Sort stably for reproducible output.
	sort.Slice(hits, func(i, j int) bool { return hits[i].relPath < hits[j].relPath })

	// Fill author_slug per article from git log. Best-effort: if log fails,
	// leave author_slug empty rather than abort.
	backs := make([]Backlink, 0, len(hits))
	for _, h := range hits {
		author := ""
		if refs, err := r.Log(context.Background(), h.relPath); err == nil && len(refs) > 0 {
			author = refs[0].Author
		}
		backs = append(backs, Backlink{
			Path:       h.relPath,
			Title:      h.title,
			AuthorSlug: author,
		})
	}
	return backs, nil
}

// parseWikilinkTargets extracts the canonical slugs (no display text)
// referenced by `[[slug]]` or `[[slug|Display]]` markers in content.
// Invalid forms (empty, path traversal, absolute paths, extra pipes) are filtered.
func parseWikilinkTargets(content []byte) []string {
	matches := wikilinkPattern.FindAllSubmatch(content, -1)
	targets := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		slug := strings.TrimSpace(string(m[1]))
		if !validSlug(slug) {
			continue
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		targets = append(targets, slug)
	}
	return targets
}

// validSlug rejects path-traversal, absolute, and empty forms.
// Mirrors web/src/lib/wikilink.ts rejection rules.
func validSlug(slug string) bool {
	if slug == "" {
		return false
	}
	if strings.HasPrefix(slug, "/") {
		return false
	}
	if strings.Contains(slug, "..") {
		return false
	}
	return true
}

// relPathToSlug converts "team/people/nazz.md" → "people/nazz".
// Returns "" if the path isn't under team/ or doesn't end in .md.
func relPathToSlug(relPath string) string {
	rel := filepath.ToSlash(relPath)
	if !strings.HasPrefix(rel, "team/") {
		return ""
	}
	rel = strings.TrimPrefix(rel, "team/")
	if !strings.HasSuffix(rel, ".md") {
		return ""
	}
	return strings.TrimSuffix(rel, ".md")
}

// extractTitle returns the first H1 heading in content, or a filename-derived
// fallback. Used by BuildArticle and backlink rendering.
func extractTitle(content []byte, relPath string) string {
	// First `# ` line wins.
	for _, line := range strings.Split(string(content), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trim, "# "))
		}
	}
	// Fallback: last path segment minus .md, with dashes → spaces, Title Case.
	base := filepath.Base(relPath)
	base = strings.TrimSuffix(base, ".md")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return base
}

// countWords returns a whitespace-delimited word count.
// Rough but sufficient for the UI's "N words" stat.
func countWords(content []byte) int {
	return len(strings.Fields(string(content)))
}

// uniqueAuthors returns distinct authors in first-seen order.
func uniqueAuthors(refs []CommitRef) []string {
	seen := make(map[string]bool, len(refs))
	authors := make([]string, 0, len(refs))
	for _, ref := range refs {
		if seen[ref.Author] {
			continue
		}
		seen[ref.Author] = true
		authors = append(authors, ref.Author)
	}
	return authors
}
