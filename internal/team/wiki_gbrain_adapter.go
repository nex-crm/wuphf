package team

// wiki_gbrain_adapter.go — re-backs the broker's wiki READ/SEARCH/LIST/CATALOG
// surface with gbrain (via the MCP client) while preserving the existing HTTP
// response shapes so the frontend and the agent MCP proxy tools keep working
// unchanged.
//
// Routing contract (see wiki_handlers.go / wiki_lookup.go):
//
//   - When the markdown wiki worker is present (b.WikiWorker() != nil), the
//     handlers serve from the git/Bleve/SQLite backend exactly as before. This
//     is the legacy markdown deployment and every existing test exercises it.
//   - When the worker is absent (a gbrain deployment — ensureWikiWorker only
//     initialises the worker for the "markdown" backend), the handlers route
//     reads through gbrain via wikiReadClient.
//   - When the worker is absent AND gbrain is not reachable (no binary/key, or
//     no broker registered a client), the handlers degrade gracefully: an empty
//     result set plus the X-Wiki-Backend: unavailable signal header (list-shaped
//     endpoints) or a 503 JSON error (single-page read / lookup). They never
//     panic and never hard-depend on gbrain at boot.
//
// The path<->slug mapping is deterministic and round-trips: a team/-relative
// markdown path maps to a gbrain slug by stripping the "team/" prefix and the
// ".md" suffix; the inverse re-adds them. e.g.
//
//   team/concepts/foo.md  <->  concepts/foo
//   team/people/nazz.md   <->  people/nazz
//   team/foo.md           <->  foo          (flat slug)

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// gbrain retrieval limits for the read surface. Generous enough for the wiki at
// v1 scale; bounded so a single call cannot drag the whole corpus.
const (
	wikiGBrainSearchLimit = 50
	wikiGBrainListLimit   = 500
)

// wikiEmptyIndexMarkdown mirrors readIndexAll's empty fallback so the gbrain
// list path returns the same shape agents already expect.
const wikiEmptyIndexMarkdown = "# Team wiki index\n\n_No articles yet._\n"

// wikiBackendHeader is set to wikiBackendUnavailable on degraded responses so a
// caller can distinguish "the knowledge backend is down" from a genuine empty
// result without breaking JSON/markdown parsing of the (empty) body.
const (
	wikiBackendHeader      = "X-Wiki-Backend"
	wikiBackendUnavailable = "unavailable"
)

// wikiReadClient is the narrow slice of *gbrain.Client the wiki READ/SEARCH/
// LIST/CATALOG/LOOKUP handlers depend on. Defining it where it is consumed lets
// tests inject a fake (no live gbrain subprocess); *gbrain.Client satisfies it
// directly because gbrain.Hit is an alias for gbrain.SearchResult.
type wikiReadClient interface {
	Query(ctx context.Context, query string, limit int) ([]gbrain.Hit, error)
	Search(ctx context.Context, query string, limit int) ([]gbrain.Hit, error)
	GetPage(ctx context.Context, slug string) (gbrain.Page, error)
	ListPages(ctx context.Context, opts gbrain.ListOptions) ([]gbrain.PageMeta, error)
}

// wikiReadClientOverride is a test-only injection seam. Production never sets
// it; the accessor falls through to the broker-owned client. Mirrors the
// package-level sharedGBrainClient pattern already used by the memory backend.
var wikiReadClientOverride wikiReadClient

// setWikiReadClientForTest installs (or, with nil, clears) the override the wiki
// read handlers resolve before the broker-owned client. Tests must defer-clear
// it to avoid leaking across cases.
func setWikiReadClientForTest(c wikiReadClient) { wikiReadClientOverride = c }

// wikiReadGBrain resolves the gbrain client the wiki read handlers should use:
// the test override first, then the broker-owned *gbrain.Client. Returns nil
// when neither is available so callers degrade gracefully instead of panicking.
//
// The broker constructs b.gbrainClient lazily on Start (ensureGBrainMemoryClient)
// and it is non-nil in any real broker even when gbrain is not installed — in
// that case the client constructs fine and only errors on the first call, which
// the handlers translate into the graceful-degradation path.
func (b *Broker) wikiReadGBrain() wikiReadClient {
	if ov := wikiReadClientOverride; ov != nil {
		return ov
	}
	b.mu.Lock()
	c := b.gbrainClient
	b.mu.Unlock()
	if c == nil {
		// Returning the typed nil pointer wrapped in the interface would yield a
		// non-nil interface; guard so callers' nil checks behave.
		return nil
	}
	return c
}

// wikiPathToSlug maps a team/-relative markdown path to a gbrain slug by
// stripping the "team/" prefix and the ".md" suffix. Inverse of wikiSlugToPath.
func wikiPathToSlug(path string) string {
	p := strings.TrimSpace(path)
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "team/")
	p = strings.TrimSuffix(p, ".md")
	return p
}

// wikiSlugToPath maps a gbrain slug back to a team/-relative markdown path. It
// tolerates slugs that already carry the prefix/suffix so the mapping is
// idempotent. Inverse of wikiPathToSlug.
func wikiSlugToPath(slug string) string {
	s := strings.TrimSpace(slug)
	s = filepath.ToSlash(s)
	s = strings.TrimSuffix(s, ".md")
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "team/") {
		s = "team/" + s
	}
	return s + ".md"
}

// wikiGroupFromPath derives the catalog "group" nav key from a team/-relative
// path: the first path segment under team/. e.g. team/people/nazz.md -> people.
// Returns "" when there is no intermediate directory.
func wikiGroupFromPath(path string) string {
	p := wikiPathToSlug(path) // strips team/ + .md
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// gbrainHitToSearchHit maps a gbrain retrieval hit onto the literal-search wire
// shape ({path, line, snippet}). gbrain has no line concept, so Line is 0.
func gbrainHitToSearchHit(hit gbrain.Hit) WikiSearchHit {
	return WikiSearchHit{
		Path:    wikiSlugToPath(hit.Slug),
		Line:    0,
		Snippet: strings.TrimSpace(hit.ChunkText),
	}
}

// gbrainHitToSource maps a gbrain retrieval hit onto the cited-answer source
// shape used by the lookup synthesis prompt.
func gbrainHitToSource(hit gbrain.Hit) QuerySource {
	kind := strings.TrimSpace(hit.Type)
	if kind == "" {
		kind = "page"
	}
	excerpt := strings.TrimSpace(hit.ChunkText)
	if len(excerpt) > 300 {
		excerpt = excerpt[:300] + "…"
	}
	sourcePath := strings.TrimSpace(hit.ChunkSource)
	if sourcePath == "" {
		sourcePath = wikiSlugToPath(hit.Slug)
	}
	staleness := 0.0
	if hit.Stale {
		staleness = 1.0
	}
	return QuerySource{
		Kind:       kind,
		SlugOrID:   hit.Slug,
		Title:      strings.TrimSpace(hit.Title),
		Excerpt:    excerpt,
		Staleness:  staleness,
		SourcePath: sourcePath,
	}
}

// gbrainPageMetaToCatalogEntry maps gbrain list_pages metadata onto the UI
// catalog wire shape. Read-tracking + word-count fields are zero (gbrain owns
// the content, not the git read log); Categories is always non-nil so the UI
// can rely on the field.
func gbrainPageMetaToCatalogEntry(pm gbrain.PageMeta) CatalogEntry {
	path := wikiSlugToPath(pm.Slug)
	categories := pm.Tags
	if categories == nil {
		categories = []string{}
	}
	return CatalogEntry{
		Path:         path,
		Title:        strings.TrimSpace(pm.Title),
		AuthorSlug:   "",
		LastEditedTs: strings.TrimSpace(pm.Updated),
		Group:        wikiGroupFromPath(path),
		Categories:   categories,
	}
}

// renderGBrainIndexMarkdown builds the markdown index body agents read from
// /wiki/list out of gbrain page metadata. Mirrors the index/all.md shape
// (heading + bulleted wikilinks) closely enough for downstream agent parsing.
func renderGBrainIndexMarkdown(pages []gbrain.PageMeta) string {
	if len(pages) == 0 {
		return wikiEmptyIndexMarkdown
	}
	var b strings.Builder
	b.WriteString("# Team wiki index\n\n")
	for _, pm := range pages {
		path := wikiSlugToPath(pm.Slug)
		title := strings.TrimSpace(pm.Title)
		if title == "" {
			title = path
		}
		b.WriteString("- [[")
		b.WriteString(path)
		b.WriteString("]] — ")
		b.WriteString(title)
		b.WriteString("\n")
	}
	return b.String()
}

// writeWikiBackendUnavailable emits the structured "knowledge backend
// unavailable" signal for the single-page read / lookup endpoints, which have no
// list shape to empty out.
func writeWikiBackendUnavailable(w http.ResponseWriter, status int) {
	w.Header().Set(wikiBackendHeader, wikiBackendUnavailable)
	writeJSON(w, status, map[string]string{"error": "knowledge backend unavailable"})
}

// isGBrainUnavailable reports whether a gbrain error means the backend itself is
// not reachable (no binary/key) rather than a per-call miss.
func isGBrainUnavailable(err error) bool {
	return errors.Is(err, gbrain.ErrNotInstalled)
}

// serveWikiReadFromGBrain backs GET /wiki/read when no markdown worker is
// present. Preserves the read contract: 200 text/plain (found), 404 JSON (not
// found / bad slug), 400 JSON (bad path / reserved reader), 503 JSON (backend
// unavailable).
func (b *Broker) serveWikiReadFromGBrain(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	client := b.wikiReadGBrain()
	if client == nil {
		writeWikiBackendUnavailable(w, http.StatusServiceUnavailable)
		return
	}
	page, err := client.GetPage(r.Context(), wikiPathToSlug(relPath))
	if err != nil {
		if isGBrainUnavailable(err) {
			writeWikiBackendUnavailable(w, http.StatusServiceUnavailable)
			return
		}
		// Any other gbrain error for a specific slug reads as "not found".
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(page.Content) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
		return
	}
	// Reader tracking, mirroring handleWikiRead. The reserved human reader is
	// rejected; valid agent readers are appended when a read log exists (it does
	// not in a gbrain-only deployment, so the append is nil-guarded).
	if raw := r.URL.Query().Get("reader"); raw == ReaderHuman {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `reader "web" is reserved for human browser access`})
		return
	} else if reader := sanitizeReader(raw); reader != "" {
		if rl := b.WikiReadLog(); rl != nil {
			rl.Append(relPath, reader)
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(page.Content))
}

// serveWikiSearchFromGBrain backs GET /wiki/search when no markdown worker is
// present. Always 200 {"hits":[...]}; an empty list plus the unavailable header
// when gbrain cannot answer.
func (b *Broker) serveWikiSearchFromGBrain(w http.ResponseWriter, r *http.Request) {
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern is required"})
		return
	}
	client := b.wikiReadGBrain()
	if client == nil {
		writeEmptyWikiHits(w)
		return
	}
	hits, err := client.Search(r.Context(), pattern, wikiGBrainSearchLimit)
	if err != nil {
		log.Printf("wiki/search: gbrain search %q: %v", pattern, err)
		writeEmptyWikiHits(w)
		return
	}
	out := make([]WikiSearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, gbrainHitToSearchHit(h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": out})
}

// writeEmptyWikiHits emits the empty search shape plus the unavailable signal.
func writeEmptyWikiHits(w http.ResponseWriter) {
	w.Header().Set(wikiBackendHeader, wikiBackendUnavailable)
	writeJSON(w, http.StatusOK, map[string]any{"hits": []WikiSearchHit{}})
}

// serveWikiListFromGBrain backs GET /wiki/list when no markdown worker is
// present. Returns the markdown index synthesized from gbrain page metadata.
func (b *Broker) serveWikiListFromGBrain(w http.ResponseWriter, r *http.Request) {
	client := b.wikiReadGBrain()
	if client == nil {
		w.Header().Set(wikiBackendHeader, wikiBackendUnavailable)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(wikiEmptyIndexMarkdown))
		return
	}
	pages, err := client.ListPages(r.Context(), gbrain.ListOptions{Limit: wikiGBrainListLimit})
	if err != nil {
		log.Printf("wiki/list: gbrain list_pages: %v", err)
		w.Header().Set(wikiBackendHeader, wikiBackendUnavailable)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(wikiEmptyIndexMarkdown))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(renderGBrainIndexMarkdown(pages)))
}

// serveWikiCatalogFromGBrain backs GET /wiki/catalog when no markdown worker is
// present. Always 200 {"articles":[...]}; an empty list plus the unavailable
// header when gbrain cannot answer. The ?sort param has no effect on this path
// (sorting was read-log derived, which gbrain does not own).
func (b *Broker) serveWikiCatalogFromGBrain(w http.ResponseWriter, r *http.Request) {
	client := b.wikiReadGBrain()
	if client == nil {
		w.Header().Set(wikiBackendHeader, wikiBackendUnavailable)
		writeJSON(w, http.StatusOK, map[string]any{"articles": []CatalogEntry{}})
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	pages, err := client.ListPages(r.Context(), gbrain.ListOptions{
		Limit:          wikiGBrainListLimit,
		IncludeDeleted: includeArchived,
	})
	if err != nil {
		log.Printf("wiki/catalog: gbrain list_pages: %v", err)
		w.Header().Set(wikiBackendHeader, wikiBackendUnavailable)
		writeJSON(w, http.StatusOK, map[string]any{"articles": []CatalogEntry{}})
		return
	}
	entries := make([]CatalogEntry, 0, len(pages))
	for _, pm := range pages {
		entries = append(entries, gbrainPageMetaToCatalogEntry(pm))
	}
	writeJSON(w, http.StatusOK, map[string]any{"articles": entries})
}
