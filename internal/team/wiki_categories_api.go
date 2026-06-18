package team

// wiki_categories_api.go — the read API + live cache for the Wikipedia-style
// category layer (Phase 2; see docs/specs/wiki-wikipedia-ia.md).
//
// Categories are a many-to-many classification authored in each article's
// `categories:` frontmatter and held in the derived article_categories index
// (Phase 1). This file surfaces that index over HTTP, mirroring the
// /wiki/sections cache + SSE shape:
//
//   - GET /wiki/categories        → the cached category list (slug, title, count)
//   - GET /wiki/categories/{slug} → that category's member articles (live query)
//
// The list is served from a debounced in-memory cache fed by wiki:write events,
// and a wiki:categories_updated SSE event fires whenever the set changes — the
// same machinery as wikiSectionsCache, so the frontend nav can hot-swap.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// CategoriesRefreshDebounce bounds how often the category list recomputes under
// a burst of wiki:write events. Matches the sections debounce so the two
// background loops keep the same cadence.
const CategoriesRefreshDebounce = 500 * time.Millisecond

// CategoryRefreshTimeout bounds one category-list recompute (an index query).
const CategoryRefreshTimeout = 10 * time.Second

// wikiCategoriesEventName is the SSE event emitted when the cached category set
// changes shape. Narrower than wiki:write so the nav can subscribe directly.
const wikiCategoriesEventName = "wiki:categories_updated"

// DiscoveredCategory is one category in the nav list: a slug, a display title
// derived from it, how many articles are filed under it, and its parent
// categories (the subcategory tree). Parents is always present (possibly empty).
type DiscoveredCategory struct {
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	ArticleCount int      `json:"article_count"`
	Parents      []string `json:"parents"`
}

// CategoryArticle is one member of a category: its wiki-root-relative path plus
// its display title (first H1, falling back to the filename).
type CategoryArticle struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// CategoryDetail is the GET /wiki/categories/{slug} response: the category and
// its member articles, sorted by path.
type CategoryDetail struct {
	Slug     string            `json:"slug"`
	Title    string            `json:"title"`
	Articles []CategoryArticle `json:"articles"`
}

// WikiCategoriesUpdatedEvent is the SSE payload broadcast when the cached
// category list changes. The full list ships so the UI can hot-swap without a
// follow-up request.
type WikiCategoriesUpdatedEvent struct {
	Categories []DiscoveredCategory `json:"categories"`
	Timestamp  string               `json:"timestamp"`
}

// categoryTitleFromSlug renders a human title for a category slug, reusing the
// same slug→Title casing as wiki sections ("revenue-operations" → "Revenue
// Operations").
func categoryTitleFromSlug(slug string) string {
	return sectionTitleFromSlug(slug)
}

// wikiCategoriesCache is the debounced in-memory cache + SSE fan-out behind
// GET /wiki/categories. One per broker; lives as long as the broker. It reads
// the derived article_categories index (markdown-authoritative) rather than
// walking the filesystem.
type wikiCategoriesCache struct {
	index     func() *WikiIndex
	publisher wikiCategoriesPublisher

	mu         sync.RWMutex
	categories []DiscoveredCategory
	lastSig    string

	stopCh    chan struct{}
	runMu     sync.Mutex
	refreshCh chan struct{}
	stopped   bool
}

// wikiCategoriesPublisher is the subset of Broker the cache needs, kept as an
// interface so the cache is testable without an HTTP server.
type wikiCategoriesPublisher interface {
	PublishWikiCategoriesUpdated(evt WikiCategoriesUpdatedEvent)
}

// newWikiCategoriesCache wires a cache against an index resolver (a closure so
// it picks up the index lazily — the index may be built after the cache).
func newWikiCategoriesCache(index func() *WikiIndex, publisher wikiCategoriesPublisher) *wikiCategoriesCache {
	return &wikiCategoriesCache{
		index:     index,
		publisher: publisher,
		refreshCh: make(chan struct{}, 1),
	}
}

// Start kicks off the debounce loop after one immediate compute so the cache is
// warm before the first request.
func (c *wikiCategoriesCache) Start(ctx context.Context) {
	c.runMu.Lock()
	if c.stopCh != nil {
		c.runMu.Unlock()
		return
	}
	c.stopCh = make(chan struct{})
	c.runMu.Unlock()

	c.refresh(ctx)
	go c.loop(ctx)
}

// Stop signals the debounce loop to exit. Idempotent.
func (c *wikiCategoriesCache) Stop() {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.stopped || c.stopCh == nil {
		return
	}
	c.stopped = true
	close(c.stopCh)
}

// Enqueue is the non-blocking signal poked on every wiki:write. A pending token
// coalesces multiple writes into one refresh.
func (c *wikiCategoriesCache) Enqueue() {
	select {
	case c.refreshCh <- struct{}{}:
	default:
	}
}

// Categories returns a copy of the current cached list.
func (c *wikiCategoriesCache) Categories() []DiscoveredCategory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.categories) == 0 {
		return []DiscoveredCategory{}
	}
	out := make([]DiscoveredCategory, len(c.categories))
	copy(out, c.categories)
	return out
}

// loop is the debounce drain — same shape as wikiSectionsCache.loop.
func (c *wikiCategoriesCache) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-c.refreshCh:
			timer := time.NewTimer(CategoriesRefreshDebounce)
		waitLoop:
			for {
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-c.stopCh:
					timer.Stop()
					return
				case <-c.refreshCh:
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(CategoriesRefreshDebounce)
				case <-timer.C:
					break waitLoop
				}
			}
			c.refresh(ctx)
		}
	}
}

// refresh recomputes the category list from the index and publishes a SSE event
// when the set changed. A nil index (markdown backend off) or query error
// leaves the last-known cache intact.
func (c *wikiCategoriesCache) refresh(ctx context.Context) {
	idx := c.resolveIndex()
	if idx == nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, CategoryRefreshTimeout)
	defer cancel()

	counts, err := idx.ListAllCategories(callCtx)
	if err != nil {
		log.Printf("wiki categories: list failed: %v", err)
		return
	}
	edges, err := idx.ListAllCategoryParents(callCtx)
	if err != nil {
		log.Printf("wiki categories: parents failed: %v", err)
		return
	}

	// The category universe is every slug that has articles OR participates in a
	// parent edge — so parent categories appear in the tree even before any
	// article is filed under them.
	countBySlug := make(map[string]int, len(counts))
	universe := make(map[string]struct{}, len(counts))
	for _, cc := range counts {
		countBySlug[cc.Slug] = cc.Count
		universe[cc.Slug] = struct{}{}
	}
	parentsBySlug := make(map[string][]string)
	for _, e := range edges {
		parentsBySlug[e.Category] = append(parentsBySlug[e.Category], e.Parent)
		universe[e.Category] = struct{}{}
		universe[e.Parent] = struct{}{}
	}
	slugs := make([]string, 0, len(universe))
	for slug := range universe {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	cats := make([]DiscoveredCategory, 0, len(slugs))
	for _, slug := range slugs {
		parents := parentsBySlug[slug] // already parent-sorted (edges come sorted)
		if parents == nil {
			parents = []string{}
		}
		cats = append(cats, DiscoveredCategory{
			Slug:         slug,
			Title:        categoryTitleFromSlug(slug),
			ArticleCount: countBySlug[slug],
			Parents:      parents,
		})
	}

	sig := categoriesSignature(cats)
	c.mu.Lock()
	changed := sig != c.lastSig
	c.categories = cats
	c.lastSig = sig
	c.mu.Unlock()

	if changed && c.publisher != nil {
		c.publisher.PublishWikiCategoriesUpdated(WikiCategoriesUpdatedEvent{
			Categories: cats,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func (c *wikiCategoriesCache) resolveIndex() *WikiIndex {
	if c.index == nil {
		return nil
	}
	return c.index()
}

// categoriesSignature is a compact fingerprint of the category set (slug+count),
// so a write that doesn't change membership doesn't spam the SSE channel.
func categoriesSignature(cats []DiscoveredCategory) string {
	parts := make([]string, 0, len(cats))
	for _, c := range cats {
		parts = append(parts, fmt.Sprintf("%s:%d:%s", c.Slug, c.ArticleCount, strings.Join(c.Parents, ",")))
	}
	return strings.Join(parts, "|")
}

// ── Broker wiring ────────────────────────────────────────────────────

// SubscribeWikiCategoriesUpdated returns a channel of category-updated events
// plus an unsubscribe func. Mirror of SubscribeWikiSectionsUpdated.
func (b *Broker) SubscribeWikiCategoriesUpdated(buffer int) (<-chan WikiCategoriesUpdatedEvent, func()) {
	if buffer <= 0 {
		buffer = 16
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wikiCategoriesSubscribers == nil {
		b.wikiCategoriesSubscribers = make(map[int]chan WikiCategoriesUpdatedEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan WikiCategoriesUpdatedEvent, buffer)
	b.wikiCategoriesSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.wikiCategoriesSubscribers[id]; ok {
			delete(b.wikiCategoriesSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishWikiCategoriesUpdated fans a category-updated event out to every
// current SSE subscriber. Non-blocking per subscriber.
func (b *Broker) PublishWikiCategoriesUpdated(evt WikiCategoriesUpdatedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.wikiCategoriesSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// EnqueueCategoriesRefresh pokes the category cache after a wiki write. No-op
// when the cache is not attached (tests, non-markdown backend).
func (b *Broker) EnqueueCategoriesRefresh() {
	b.mu.Lock()
	cache := b.wikiCategoriesCache
	b.mu.Unlock()
	if cache == nil {
		return
	}
	cache.Enqueue()
}

// WikiCategoriesCache returns the attached cache, or nil when the markdown
// backend is not active. Primarily used by tests.
func (b *Broker) WikiCategoriesCache() *wikiCategoriesCache {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiCategoriesCache
}

// ensureWikiCategoriesCache wires the cache against the broker's wiki index.
// Idempotent. Called alongside ensureWikiSectionsCache.
func (b *Broker) ensureWikiCategoriesCache() {
	b.mu.Lock()
	if b.wikiCategoriesCache != nil {
		b.mu.Unlock()
		return
	}
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}
	cache := newWikiCategoriesCache(b.WikiIndex, b)
	cache.Start(context.Background())

	b.mu.Lock()
	b.wikiCategoriesCache = cache
	b.mu.Unlock()
}

// handleWikiCategories is GET /wiki/categories.
//
//	Response: { "categories": DiscoveredCategory[] }
//
// Served from the in-memory cache. 503 when the wiki backend is not active.
func (b *Broker) handleWikiCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cache := b.WikiCategoriesCache()
	if cache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": cache.Categories()})
}

// handleWikiCategory is GET /wiki/categories/{slug}.
//
//	Response: CategoryDetail
//
// Member articles are queried live from the index and enriched with titles.
// 503 when the wiki backend is not active, 400 for an empty slug.
func (b *Broker) handleWikiCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/wiki/categories/"), "/")
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category slug is required"})
		return
	}
	idx := b.WikiIndex()
	worker := b.WikiWorker()
	if idx == nil || worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	paths, err := idx.ListArticlesInCategory(r.Context(), slug)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, CategoryDetail{
		Slug:     slug,
		Title:    categoryTitleFromSlug(slug),
		Articles: categoryArticles(worker.Repo(), paths),
	})
}

// categoryArticles enriches member paths with their display titles. A missing
// or unreadable article falls back to a filename-derived title rather than
// dropping the row, so a mid-flight delete doesn't blank the listing.
func categoryArticles(repo *Repo, paths []string) []CategoryArticle {
	out := make([]CategoryArticle, 0, len(paths))
	for _, p := range paths {
		title := ""
		if content, err := readArticle(repo, p); err == nil {
			title = extractTitle(content, p)
		} else {
			title = extractTitle(nil, p)
		}
		out = append(out, CategoryArticle{Path: p, Title: title})
	}
	return out
}
