package team

// wiki_categories_api_test.go — Phase 2 coverage for the category read API:
// the debounced cache (list + SSE emit), the signature fingerprint, and the
// two HTTP handlers (list + detail) including their 503/400 edges.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCategoriesSignature(t *testing.T) {
	a := []DiscoveredCategory{{Slug: "ai-agents", ArticleCount: 2}, {Slug: "sales", ArticleCount: 1}}
	b := []DiscoveredCategory{{Slug: "ai-agents", ArticleCount: 2}, {Slug: "sales", ArticleCount: 1}}
	if categoriesSignature(a) != categoriesSignature(b) {
		t.Error("identical category sets produced different signatures")
	}
	// A count change must move the signature (drives the SSE emit).
	c := []DiscoveredCategory{{Slug: "ai-agents", ArticleCount: 3}, {Slug: "sales", ArticleCount: 1}}
	if categoriesSignature(a) == categoriesSignature(c) {
		t.Error("count change did not move the signature")
	}
}

type recordingCategoriesPublisher struct {
	mu     sync.Mutex
	events []WikiCategoriesUpdatedEvent
}

func (r *recordingCategoriesPublisher) PublishWikiCategoriesUpdated(evt WikiCategoriesUpdatedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
}

func (r *recordingCategoriesPublisher) snapshot() []WikiCategoriesUpdatedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]WikiCategoriesUpdatedEvent, len(r.events))
	copy(out, r.events)
	return out
}

func TestWikiCategoriesCacheRefresh(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBrief(t, root, "team/companies/acme.md",
		"---\nkind: company\ncategories: [revenue-operations, ai-agents]\n---\n# Acme\n")
	writeBrief(t, root, "team/concepts/mql.md",
		"---\ntype: concept\ncategories: [ai-agents]\n---\n# MQL\n")

	idx := NewWikiIndex(root)
	defer idx.Close()
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	recorder := &recordingCategoriesPublisher{}
	cache := newWikiCategoriesCache(func() *WikiIndex { return idx }, recorder)
	cache.Start(ctx) // synchronous initial compute
	defer cache.Stop()

	got := cache.Categories()
	want := []DiscoveredCategory{
		{Slug: "ai-agents", Title: "Ai Agents", ArticleCount: 2},
		{Slug: "revenue-operations", Title: "Revenue Operations", ArticleCount: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("Categories() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Categories()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The empty→non-empty transition must have emitted exactly one event.
	if evts := recorder.snapshot(); len(evts) == 0 {
		t.Error("expected a categories_updated event on first populate, got none")
	} else if len(evts[len(evts)-1].Categories) != 2 {
		t.Errorf("last event carried %d categories, want 2", len(evts[len(evts)-1].Categories))
	}
}

// brokerWithCategories builds a minimal in-package Broker wired with a populated
// category cache + index + worker, enough to exercise the handlers directly.
func brokerWithCategories(t *testing.T) (*Broker, *WikiIndex) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	writeBrief(t, root, "team/companies/acme.md",
		"---\nkind: company\ncategories: [ai-agents]\n---\n# Acme Corp\n\nBody.\n")
	writeBrief(t, root, "team/concepts/mql.md",
		"---\ntype: concept\ncategories: [ai-agents]\n---\n# Marketing Qualified Lead\n")

	idx := NewWikiIndex(root)
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	b := &Broker{}
	b.wikiIndex = idx
	b.wikiWorker = NewWikiWorker(NewRepoAt(root, t.TempDir()), noopPublisher{})
	cache := newWikiCategoriesCache(func() *WikiIndex { return idx }, nil)
	cache.Start(ctx)
	t.Cleanup(cache.Stop)
	b.wikiCategoriesCache = cache
	return b, idx
}

func TestHandleWikiCategories(t *testing.T) {
	b, _ := brokerWithCategories(t)

	rr := httptest.NewRecorder()
	b.handleWikiCategories(rr, httptest.NewRequest(http.MethodGet, "/wiki/categories", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Categories []DiscoveredCategory `json:"categories"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Categories) != 1 || resp.Categories[0].Slug != "ai-agents" || resp.Categories[0].ArticleCount != 2 {
		t.Fatalf("categories = %+v, want one ai-agents(count 2)", resp.Categories)
	}

	// No cache attached → 503.
	empty := &Broker{}
	rr2 := httptest.NewRecorder()
	empty.handleWikiCategories(rr2, httptest.NewRequest(http.MethodGet, "/wiki/categories", nil))
	if rr2.Code != http.StatusServiceUnavailable {
		t.Errorf("nil-cache status = %d, want 503", rr2.Code)
	}
}

func TestHandleWikiCategory(t *testing.T) {
	b, _ := brokerWithCategories(t)

	rr := httptest.NewRecorder()
	b.handleWikiCategory(rr, httptest.NewRequest(http.MethodGet, "/wiki/categories/ai-agents", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var detail CategoryDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Slug != "ai-agents" || detail.Title != "Ai Agents" {
		t.Errorf("detail slug/title = %q/%q", detail.Slug, detail.Title)
	}
	if len(detail.Articles) != 2 {
		t.Fatalf("articles = %+v, want 2", detail.Articles)
	}
	// Titles come from each article's H1, sorted by path (companies before concepts).
	if detail.Articles[0].Path != "team/companies/acme.md" || detail.Articles[0].Title != "Acme Corp" {
		t.Errorf("article[0] = %+v, want acme/Acme Corp", detail.Articles[0])
	}
	if detail.Articles[1].Path != "team/concepts/mql.md" || detail.Articles[1].Title != "Marketing Qualified Lead" {
		t.Errorf("article[1] = %+v, want mql/Marketing Qualified Lead", detail.Articles[1])
	}

	// Empty slug → 400.
	rr2 := httptest.NewRecorder()
	b.handleWikiCategory(rr2, httptest.NewRequest(http.MethodGet, "/wiki/categories/", nil))
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("empty-slug status = %d, want 400", rr2.Code)
	}

	// Unknown category → 200 with an empty article list (not an error).
	rr3 := httptest.NewRecorder()
	b.handleWikiCategory(rr3, httptest.NewRequest(http.MethodGet, "/wiki/categories/nope", nil))
	if rr3.Code != http.StatusOK {
		t.Fatalf("unknown-category status = %d, want 200", rr3.Code)
	}
	var empty CategoryDetail
	if err := json.Unmarshal(rr3.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(empty.Articles) != 0 {
		t.Errorf("unknown category articles = %+v, want empty", empty.Articles)
	}
}
