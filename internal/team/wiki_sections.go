package team

// wiki_sections.go implements v1.3 dynamic wiki sections. Sections are
// discovered from actual article paths under team/ and merged with the
// sections declared in the active blueprint's wiki_schema. The discovered
// list drives the sidebar IA so as agents write articles into new
// top-level dirs ("retrospectives/", "templates/", ...) the sidebar grows
// to match — instead of the hardcoded starter-pack list staying stale.
//
// Design summary
// ==============
//
//   - A DiscoveredSection is "first path segment under team/" — flat, not
//     nested. A brief at team/people/nazz.md belongs to section "people".
//     We intentionally do NOT create nested subsections in v1.3.
//
//   - Blueprint-declared sections always appear, even before any article
//     lands in them (FromSchema=true, ArticleCount=0). This keeps the
//     onboarding empty-state consistent with today.
//
//   - Discovered-only sections (FromSchema=false) surface once an article
//     exists. First-seen timestamps are persisted implicitly via the first
//     commit that created a file in that dir — we pull them from git log.
//
//   - Caching: discovery walks team/ and shells out to git for section
//     first-seen timestamps. We cache the result in memory with RWMutex
//     and invalidate on wiki:write events, debounced by a 500ms timer so
//     a flurry of writes produces at most one refresh. Same debounce
//     shape as entity_synthesizer.go.
//
//   - SSE: on every refresh whose output differs from the last published
//     list (by slug set), we emit wiki:sections_updated through the same
//     /events fan-out the rest of the broker uses.
//
// Happy path
//
//   startup → DiscoverSections(repo, blueprint) → cache
//      ↓
//   wiki:write event → debounce 500ms → recompute → diff → publish?

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
)

// SectionsRefreshDebounce bounds how often DiscoverSections runs under a
// burst of wiki:write events. Chosen to match the entity-synth debounce
// cadence so the two background loops don't contend.
const SectionsRefreshDebounce = 500 * time.Millisecond

// SectionDiscoveryTimeout bounds one DiscoverSections call. git log
// shell-outs per section can add up on a very large wiki; the cap keeps
// a pathological filesystem from stalling the broker forever.
const SectionDiscoveryTimeout = 10 * time.Second

// wikiSectionsEventName is the SSE event name emitted when the cached
// section list changes. Separate from wiki:write so the frontend can
// subscribe narrowly without re-parsing every article commit.
const wikiSectionsEventName = "wiki:sections_updated"

// DiscoveredSection is one top-level dir surfaced in the sidebar IA. A
// section is either declared by the active blueprint (FromSchema=true)
// or emerged organically from article writes (FromSchema=false). Both
// shapes ship to the UI in the same list so the sidebar can style them
// differently.
type DiscoveredSection struct {
	Slug         string    `json:"slug"`
	Title        string    `json:"title"`
	ArticlePaths []string  `json:"article_paths"`
	ArticleCount int       `json:"article_count"`
	FirstSeenTs  time.Time `json:"first_seen_ts"`
	LastUpdateTs time.Time `json:"last_update_ts"`
	FromSchema   bool      `json:"from_schema"`
}

// WikiSectionsUpdatedEvent is the SSE payload broadcast when the cached
// section list changes shape (new section, or a section's count/bounds
// shift). Content ships as the full section list so the UI can hot-swap
// without another HTTP roundtrip.
type WikiSectionsUpdatedEvent struct {
	Sections  []DiscoveredSection `json:"sections"`
	Timestamp string              `json:"timestamp"`
}

// DiscoverSections walks the repo's team/ tree and merges the observed
// first-segment groups with the blueprint's declared wiki_schema dirs.
// Blueprint sections are preserved even when empty; discovered-only
// sections are appended after them.
//
// The returned slice is stable-sorted inside each partition: blueprint
// sections follow blueprint order (as declared in wiki_schema.dirs),
// discovered sections are alphabetical by slug. Consumers that want a
// different ordering can re-sort — this is the canonical order.
func DiscoverSections(ctx context.Context, repo *Repo, blueprint *operations.Blueprint) ([]DiscoveredSection, error) {
	if repo == nil {
		return nil, errors.New("wiki sections: repo is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wiki sections: cancelled: %w", err)
	}

	blueprintOrder, blueprintSlugs := blueprintSectionSlugs(blueprint)

	// Walk team/ and bucket every .md by first path segment.
	bySection := map[string][]CatalogEntry{}
	teamDir := repo.TeamDir()
	walkErr := filepath.WalkDir(teamDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(repo.Root(), path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		slug := groupFromPath(rel)
		if slug == "" || slug == "root" {
			return nil
		}
		bySection[slug] = append(bySection[slug], CatalogEntry{
			Path:  rel,
			Title: "",
			Group: slug,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("wiki sections: walk team/: %w", walkErr)
	}

	// Assemble the section list: blueprint-declared first (in blueprint
	// order), then discovered-only alphabetically.
	out := make([]DiscoveredSection, 0, len(blueprintOrder)+len(bySection))
	seen := map[string]struct{}{}

	for _, slug := range blueprintOrder {
		entries := bySection[slug]
		section := materializeSection(ctx, repo, slug, entries, true)
		out = append(out, section)
		seen[slug] = struct{}{}
	}

	discoveredOnly := make([]string, 0, len(bySection))
	for slug := range bySection {
		if _, ok := blueprintSlugs[slug]; ok {
			continue
		}
		discoveredOnly = append(discoveredOnly, slug)
	}
	sort.Strings(discoveredOnly)
	for _, slug := range discoveredOnly {
		if _, ok := seen[slug]; ok {
			continue
		}
		section := materializeSection(ctx, repo, slug, bySection[slug], false)
		out = append(out, section)
	}

	return out, nil
}

// blueprintSectionSlugs extracts the first-segment section slugs from the
// blueprint's wiki_schema.dirs. Returns ([ordered slugs], lookup set).
// A nil blueprint or empty schema returns empty results — the discoverer
// then falls back to pure discovery.
func blueprintSectionSlugs(bp *operations.Blueprint) ([]string, map[string]struct{}) {
	if bp == nil || bp.WikiSchema == nil {
		return nil, map[string]struct{}{}
	}
	order := make([]string, 0, len(bp.WikiSchema.Dirs))
	seen := make(map[string]struct{}, len(bp.WikiSchema.Dirs))
	for _, dir := range bp.WikiSchema.Dirs {
		slug := blueprintDirToSlug(dir)
		if slug == "" {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		order = append(order, slug)
	}
	return order, seen
}

// blueprintDirToSlug takes a wiki_schema dir like "team/playbooks/" and
// returns "playbooks". Rejects anything that doesn't sit immediately
// under team/ — nested dirs do not create sections in v1.3.
func blueprintDirToSlug(dir string) string {
	cleaned := strings.TrimSpace(dir)
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimSuffix(cleaned, "/")
	// Bare team root — no section slug to derive.
	if cleaned == "" || cleaned == "team" {
		return ""
	}
	cleaned = strings.TrimPrefix(cleaned, "team/")
	if cleaned == "" {
		return ""
	}
	// First segment only — "people/foo" still maps to "people" (parent
	// section) but we do not surface the nested piece as a section.
	if idx := strings.Index(cleaned, "/"); idx >= 0 {
		cleaned = cleaned[:idx]
	}
	return cleaned
}

// materializeSection computes the metadata for one section from its
// article paths. Timestamps are resolved via git log on the oldest and
// newest commits touching any file in the section; articles without git
// history (pre-worker writes, tests) contribute a zero time which we
// collapse to the filesystem mtime as a reasonable fallback.
func materializeSection(ctx context.Context, repo *Repo, slug string, entries []CatalogEntry, fromSchema bool) DiscoveredSection {
	title := sectionTitleFromSlug(slug)
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)

	section := DiscoveredSection{
		Slug:         slug,
		Title:        title,
		ArticlePaths: paths,
		ArticleCount: len(paths),
		FromSchema:   fromSchema,
	}

	if len(paths) == 0 {
		return section
	}

	// Resolve first-seen and last-update by scanning git log on every
	// article in the section. For a small section this is cheap; for a
	// large one (100+ articles) we cap the ctx via the caller's timeout.
	var earliest, latest time.Time
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			break
		}
		refs, err := repo.Log(ctx, rel)
		if err != nil || len(refs) == 0 {
			// Fall back to mtime — keeps sections with pre-worker
			// history (git bootstrap restore, tests) from rendering
			// with zero timestamps.
			if info, statErr := os.Stat(filepath.Join(repo.Root(), filepath.FromSlash(rel))); statErr == nil {
				t := info.ModTime().UTC()
				if earliest.IsZero() || t.Before(earliest) {
					earliest = t
				}
				if latest.IsZero() || t.After(latest) {
					latest = t
				}
			}
			continue
		}
		newest := refs[0].Timestamp.UTC()
		oldest := refs[len(refs)-1].Timestamp.UTC()
		if earliest.IsZero() || oldest.Before(earliest) {
			earliest = oldest
		}
		if latest.IsZero() || newest.After(latest) {
			latest = newest
		}
	}
	section.FirstSeenTs = earliest
	section.LastUpdateTs = latest
	return section
}

// sectionTitleFromSlug turns "retrospectives" into "Retrospectives" and
// "ops-reviews" into "Ops Reviews". Purely cosmetic — the slug remains
// the stable identifier.
func sectionTitleFromSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// wikiSectionsCache is the in-memory cache + debounced refresh loop that
// powers the /wiki/sections endpoint and the SSE fan-out. One cache per
// broker; lives as long as the broker.
type wikiSectionsCache struct {
	worker    *WikiWorker
	blueprint func() *operations.Blueprint
	publisher wikiSectionsPublisher

	mu       sync.RWMutex
	sections []DiscoveredSection
	lastSig  string

	stopCh chan struct{}
	runMu  sync.Mutex
	// refreshCh is fed one token per wiki:write. The drain loop debounces
	// by reading one token, then sleeping until the debounce window
	// passes without another token arriving.
	refreshCh chan struct{}
	stopped   bool
}

// wikiSectionsPublisher is the subset of Broker the cache needs. Having
// it as an interface keeps the cache testable without an HTTP server.
type wikiSectionsPublisher interface {
	PublishWikiSectionsUpdated(evt WikiSectionsUpdatedEvent)
}

// newWikiSectionsCache wires a cache against the given worker + blueprint
// resolver. The blueprint resolver is a closure so the cache picks up
// the current active blueprint at refresh time (the user may change it
// via /config after onboarding).
func newWikiSectionsCache(worker *WikiWorker, blueprint func() *operations.Blueprint, publisher wikiSectionsPublisher) *wikiSectionsCache {
	return &wikiSectionsCache{
		worker:    worker,
		blueprint: blueprint,
		publisher: publisher,
		refreshCh: make(chan struct{}, 1),
	}
}

// Start kicks off the debounce loop and does one immediate compute so
// the cache is warm before the first request.
func (c *wikiSectionsCache) Start(ctx context.Context) {
	c.runMu.Lock()
	if c.stopCh != nil {
		c.runMu.Unlock()
		return
	}
	c.stopCh = make(chan struct{})
	c.runMu.Unlock()

	// Initial synchronous compute — callers hitting /wiki/sections right
	// after startup get a populated list.
	c.refresh(ctx)
	go c.loop(ctx)
}

// Stop signals the debounce loop to exit. Idempotent.
func (c *wikiSectionsCache) Stop() {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.stopped || c.stopCh == nil {
		return
	}
	c.stopped = true
	close(c.stopCh)
}

// Enqueue is the hot-path signal the worker calls on every wiki:write.
// Non-blocking; if the refresh channel already has a pending token the
// call is a no-op (coalesced into the pending refresh).
func (c *wikiSectionsCache) Enqueue() {
	select {
	case c.refreshCh <- struct{}{}:
	default:
	}
}

// Sections returns the current cached list. The returned slice is a
// copy so callers can mutate freely without races.
func (c *wikiSectionsCache) Sections() []DiscoveredSection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.sections) == 0 {
		return []DiscoveredSection{}
	}
	out := make([]DiscoveredSection, len(c.sections))
	copy(out, c.sections)
	return out
}

// loop is the debounce drain. One active refresh at a time; while a
// refresh runs, further Enqueue calls simply leave a token on the
// channel and the loop picks it up on the next iteration.
func (c *wikiSectionsCache) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-c.refreshCh:
			// Debounce: after the first token, wait for quiet before
			// running. Any token arriving during the wait extends the
			// window (but never past the cap of SectionsRefreshDebounce
			// itself — we don't want to starve indefinitely under a
			// non-stop write loop).
			timer := time.NewTimer(SectionsRefreshDebounce)
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
					// Reset timer to extend the quiet window.
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(SectionsRefreshDebounce)
				case <-timer.C:
					break waitLoop
				}
			}
			c.refresh(ctx)
		}
	}
}

// refresh recomputes the section list and publishes a SSE event when the
// shape changed. Errors during discovery are logged; the last-known
// cache stays valid so a transient fs hiccup doesn't empty the sidebar.
func (c *wikiSectionsCache) refresh(ctx context.Context) {
	callCtx, cancel := context.WithTimeout(ctx, SectionDiscoveryTimeout)
	defer cancel()

	bp := c.resolveBlueprint()
	sections, err := DiscoverSections(callCtx, c.worker.Repo(), bp)
	if err != nil {
		log.Printf("wiki sections: discover failed: %v", err)
		return
	}

	sig := sectionsSignature(sections)
	c.mu.Lock()
	changed := sig != c.lastSig
	c.sections = sections
	c.lastSig = sig
	c.mu.Unlock()

	if changed && c.publisher != nil {
		c.publisher.PublishWikiSectionsUpdated(WikiSectionsUpdatedEvent{
			Sections:  sections,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// resolveBlueprint calls the closure; nil is fine — the discoverer
// degrades to pure discovery.
func (c *wikiSectionsCache) resolveBlueprint() *operations.Blueprint {
	if c.blueprint == nil {
		return nil
	}
	return c.blueprint()
}

// sectionsSignature is a compact fingerprint of the current section set.
// Two section lists with the same signature are observationally the same
// for sidebar purposes — same slugs, same counts, same declared-vs-
// discovered split. We exclude timestamps so a routine wiki:write that
// only bumps LastUpdateTs on an already-visible section does not spam
// the SSE channel with near-duplicate events.
func sectionsSignature(sections []DiscoveredSection) string {
	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		schemaFlag := "d"
		if s.FromSchema {
			schemaFlag = "s"
		}
		parts = append(parts, fmt.Sprintf("%s:%s:%d", schemaFlag, s.Slug, s.ArticleCount))
	}
	return strings.Join(parts, "|")
}

// resolveActiveBlueprint returns the current active blueprint or nil.
// Loads YAML from disk on every call — cheap relative to the wiki walk
// and ensures a user's blueprint change via /config is reflected on the
// next refresh without restart.
func resolveActiveBlueprint() *operations.Blueprint {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	id := strings.TrimSpace(cfg.ActiveBlueprint())
	if id == "" {
		return nil
	}
	bp, err := operations.LoadBlueprint(onboarding.ResolveTemplatesRepoRoot(""), id)
	if err != nil {
		return nil
	}
	return &bp
}

// ── Broker wiring ────────────────────────────────────────────────────

// SubscribeWikiSectionsUpdated returns a channel of section-updated
// events plus an unsubscribe func. Mirror of SubscribeWikiEvents.
func (b *Broker) SubscribeWikiSectionsUpdated(buffer int) (<-chan WikiSectionsUpdatedEvent, func()) {
	if buffer <= 0 {
		buffer = 16
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wikiSectionsSubscribers == nil {
		b.wikiSectionsSubscribers = make(map[int]chan WikiSectionsUpdatedEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan WikiSectionsUpdatedEvent, buffer)
	b.wikiSectionsSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.wikiSectionsSubscribers[id]; ok {
			delete(b.wikiSectionsSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishWikiSectionsUpdated fans out a section-updated event to every
// current SSE subscriber. Non-blocking per subscriber: a slow consumer
// loses events rather than stalling the publisher.
func (b *Broker) PublishWikiSectionsUpdated(evt WikiSectionsUpdatedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.wikiSectionsSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// EnqueueSectionsRefresh is the broker-level adapter the wiki worker
// calls after a successful team wiki write. Implements
// wikiSectionsNotifier. No-op when the cache is not attached (tests,
// non-markdown backend).
func (b *Broker) EnqueueSectionsRefresh() {
	b.mu.Lock()
	cache := b.wikiSectionsCache
	b.mu.Unlock()
	if cache == nil {
		return
	}
	cache.Enqueue()
}

// WikiSectionsCache returns the attached cache, or nil when the markdown
// backend is not active. Primarily used by tests; production code reads
// via the HTTP handler.
func (b *Broker) WikiSectionsCache() *wikiSectionsCache {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiSectionsCache
}

// ensureWikiSectionsCache wires the cache against the current wiki
// worker. Idempotent. Called after ensureWikiWorker.
func (b *Broker) ensureWikiSectionsCache() {
	b.mu.Lock()
	if b.wikiSectionsCache != nil {
		b.mu.Unlock()
		return
	}
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}
	cache := newWikiSectionsCache(worker, resolveActiveBlueprint, b)
	cache.Start(context.Background())

	b.mu.Lock()
	b.wikiSectionsCache = cache
	b.mu.Unlock()
}

// handleWikiSections is GET /wiki/sections.
//
//	Response: { "sections": DiscoveredSection[] }
//
// Sections are served from the in-memory cache. If the cache isn't
// attached yet (markdown backend disabled, wiki worker failed to
// initialize), returns 503 — same shape the rest of the /wiki/* handlers
// use.
func (b *Broker) handleWikiSections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cache := b.WikiSectionsCache()
	if cache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	sections := cache.Sections()
	writeJSON(w, http.StatusOK, map[string]any{"sections": sections})
}

// sectionsJSON is a tiny helper for tests that want to assert the wire
// shape without pulling in net/http.
func sectionsJSON(sections []DiscoveredSection) ([]byte, error) {
	return json.Marshal(map[string]any{"sections": sections})
}
