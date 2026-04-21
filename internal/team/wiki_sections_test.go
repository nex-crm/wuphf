package team

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/operations"
)

// sectionsTestRepo spins up a fresh Repo backed by t.TempDir(). Returns a
// worker so we can drive writes through it. The commit path is exactly
// the one production uses — we want git history so DiscoverSections can
// resolve first-seen / last-update timestamps.
func sectionsTestRepo(t *testing.T) (*Repo, *WikiWorker) {
	t.Helper()
	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	worker.Start(context.Background())
	t.Cleanup(worker.Stop)
	return repo, worker
}

// writeArticle shells the write through the real worker so the commit
// ends up in git history where materializeSection can find it.
func writeArticle(t *testing.T, worker *WikiWorker, slug, path, content string) {
	t.Helper()
	_, _, err := worker.Enqueue(context.Background(), slug, path, content, "create", "test: seed "+path)
	if err != nil {
		t.Fatalf("Enqueue %s: %v", path, err)
	}
}

func TestDiscoverSectionsBlueprintOnly(t *testing.T) {
	repo, _ := sectionsTestRepo(t)
	bp := &operations.Blueprint{
		WikiSchema: &operations.BlueprintWikiSchema{
			Dirs: []string{"team/people/", "team/playbooks/", "team/decisions/"},
		},
	}
	sections, err := DiscoverSections(context.Background(), repo, bp)
	if err != nil {
		t.Fatalf("DiscoverSections: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("len=%d want 3 (%+v)", len(sections), sections)
	}
	wantOrder := []string{"people", "playbooks", "decisions"}
	for i, s := range sections {
		if s.Slug != wantOrder[i] {
			t.Errorf("sections[%d].Slug=%q want %q", i, s.Slug, wantOrder[i])
		}
		if !s.FromSchema {
			t.Errorf("sections[%d].FromSchema=false, want true", i)
		}
		if s.ArticleCount != 0 {
			t.Errorf("sections[%d].ArticleCount=%d, want 0 (empty)", i, s.ArticleCount)
		}
	}
}

func TestDiscoverSectionsBlueprintPlusDiscovered(t *testing.T) {
	repo, worker := sectionsTestRepo(t)
	// Blueprint declares people + playbooks. Agents later write
	// articles under retrospectives/ and templates/.
	writeArticle(t, worker, "ceo", "team/people/nazz.md", "# Nazz\n")
	writeArticle(t, worker, "pm", "team/playbooks/onboarding.md", "# Onboarding\n")
	writeArticle(t, worker, "reviewer", "team/retrospectives/q1.md", "# Q1\n")
	writeArticle(t, worker, "designer", "team/templates/brief.md", "# Brief\n")

	bp := &operations.Blueprint{
		WikiSchema: &operations.BlueprintWikiSchema{
			Dirs: []string{"team/people/", "team/playbooks/"},
		},
	}
	sections, err := DiscoverSections(context.Background(), repo, bp)
	if err != nil {
		t.Fatalf("DiscoverSections: %v", err)
	}
	// Expected order: blueprint-declared first (people, playbooks),
	// then discovered-only alphabetical (retrospectives, templates).
	wantOrder := []string{"people", "playbooks", "retrospectives", "templates"}
	if len(sections) != len(wantOrder) {
		t.Fatalf("len=%d want %d: %+v", len(sections), len(wantOrder), sections)
	}
	for i, s := range sections {
		if s.Slug != wantOrder[i] {
			t.Errorf("sections[%d].Slug=%q want %q", i, s.Slug, wantOrder[i])
		}
	}
	// Blueprint sections must have FromSchema=true; discovered-only false.
	if !sections[0].FromSchema || !sections[1].FromSchema {
		t.Errorf("blueprint sections FromSchema=false, want true")
	}
	if sections[2].FromSchema || sections[3].FromSchema {
		t.Errorf("discovered sections FromSchema=true, want false")
	}
	// Each section has exactly one article.
	for _, s := range sections {
		if s.ArticleCount != 1 {
			t.Errorf("section %q count=%d want 1", s.Slug, s.ArticleCount)
		}
		if len(s.ArticlePaths) != 1 {
			t.Errorf("section %q paths=%v want len 1", s.Slug, s.ArticlePaths)
		}
		// FirstSeenTs should be non-zero for a committed article.
		if s.FirstSeenTs.IsZero() {
			t.Errorf("section %q FirstSeenTs is zero (no commit history resolved)", s.Slug)
		}
		if s.LastUpdateTs.IsZero() {
			t.Errorf("section %q LastUpdateTs is zero", s.Slug)
		}
	}
}

func TestDiscoverSectionsEmptyWiki(t *testing.T) {
	repo, _ := sectionsTestRepo(t)
	sections, err := DiscoverSections(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("DiscoverSections: %v", err)
	}
	if len(sections) != 0 {
		t.Errorf("len=%d want 0 (empty wiki, nil blueprint)", len(sections))
	}
}

func TestDiscoverSectionsDiscoveredOnlyAlphabetical(t *testing.T) {
	repo, worker := sectionsTestRepo(t)
	writeArticle(t, worker, "a", "team/zeta/one.md", "# one\n")
	writeArticle(t, worker, "a", "team/alpha/one.md", "# one\n")
	writeArticle(t, worker, "a", "team/mu/one.md", "# one\n")

	sections, err := DiscoverSections(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("DiscoverSections: %v", err)
	}
	slugs := make([]string, 0, len(sections))
	for _, s := range sections {
		slugs = append(slugs, s.Slug)
	}
	want := []string{"alpha", "mu", "zeta"}
	if len(slugs) != len(want) {
		t.Fatalf("slugs=%v want %v", slugs, want)
	}
	for i, s := range slugs {
		if s != want[i] {
			t.Errorf("slugs[%d]=%q want %q", i, s, want[i])
		}
	}
}

func TestBlueprintDirToSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"team/people/", "people"},
		{"team/playbooks/", "playbooks"},
		{"team/retrospectives", "retrospectives"},
		{"team/people/nested/", "people"},
		{"", ""},
		{"team/", ""},
		{"team", ""},
		{"./team/customers/", "customers"},
	}
	for _, c := range cases {
		got := blueprintDirToSlug(c.in)
		if got != c.want {
			t.Errorf("blueprintDirToSlug(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSectionTitleFromSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"people", "People"},
		{"ops-reviews", "Ops Reviews"},
		{"retrospectives", "Retrospectives"},
		{"", ""},
		{"a", "A"},
	}
	for _, c := range cases {
		got := sectionTitleFromSlug(c.in)
		if got != c.want {
			t.Errorf("sectionTitleFromSlug(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSectionsSignatureStability(t *testing.T) {
	a := []DiscoveredSection{
		{Slug: "people", ArticleCount: 3, FromSchema: true},
		{Slug: "playbooks", ArticleCount: 1, FromSchema: true},
	}
	b := []DiscoveredSection{
		{Slug: "people", ArticleCount: 3, FromSchema: true, LastUpdateTs: time.Now()},
		{Slug: "playbooks", ArticleCount: 1, FromSchema: true, LastUpdateTs: time.Now()},
	}
	if sectionsSignature(a) != sectionsSignature(b) {
		t.Error("signature changes on timestamp-only diff; should be stable")
	}

	c := []DiscoveredSection{
		{Slug: "people", ArticleCount: 3, FromSchema: true},
		{Slug: "playbooks", ArticleCount: 2, FromSchema: true},
	}
	if sectionsSignature(a) == sectionsSignature(c) {
		t.Error("signature same across different article counts; should differ")
	}
}

// recordingSectionsPublisher captures every PublishWikiSectionsUpdated
// for the integration test below.
type recordingSectionsPublisher struct {
	mu     sync.Mutex
	events []WikiSectionsUpdatedEvent
}

func (r *recordingSectionsPublisher) PublishWikiSectionsUpdated(evt WikiSectionsUpdatedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
}

func (r *recordingSectionsPublisher) snapshot() []WikiSectionsUpdatedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]WikiSectionsUpdatedEvent, len(r.events))
	copy(out, r.events)
	return out
}

func TestWikiSectionsCacheEmitsEventOnNewSection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Build a worker whose publisher ALREADY knows about the cache.
	// sectionsTestRepo starts the worker with a noopPublisher and the
	// data race detector trips if we swap it out after Start() — so we
	// wire the publisher up front and start the worker ourselves here.
	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	bp := &operations.Blueprint{
		WikiSchema: &operations.BlueprintWikiSchema{
			Dirs: []string{"team/people/"},
		},
	}
	recorder := &recordingSectionsPublisher{}

	// Two-step bootstrap: (a) build the cache pointing at a to-be-
	// constructed worker via a pointer indirection, (b) build the
	// worker with the cache-aware publisher, (c) start both.
	var worker *WikiWorker
	cache := newWikiSectionsCache(nil, func() *operations.Blueprint { return bp }, recorder)
	pub := &capturePublisherWithSections{inner: noopPublisher{}, cache: cache}
	worker = NewWikiWorker(repo, pub)
	cache.worker = worker
	worker.Start(context.Background())
	t.Cleanup(worker.Stop)

	// Seed one article in the "people" dir so the initial compute is
	// stable before we add a new section.
	writeArticle(t, worker, "ceo", "team/people/seed.md", "# seed\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache.Start(ctx)
	defer cache.Stop()

	// Write an article in a brand-new section — the debounce loop
	// should recompute and emit an event.
	writeArticle(t, worker, "pm", "team/retrospectives/q1.md", "# Q1\n")

	// Wait up to 2s (4x debounce) for the event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events := recorder.snapshot()
		for _, evt := range events {
			if sectionPresent(evt.Sections, "retrospectives") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for sections_updated event mentioning 'retrospectives'. events=%+v", recorder.snapshot())
}

// capturePublisherWithSections wraps a base publisher + cache notifier.
// Used in the integration test to route wiki writes into the cache.
type capturePublisherWithSections struct {
	inner wikiEventPublisher
	cache *wikiSectionsCache
}

func (p *capturePublisherWithSections) PublishWikiEvent(evt wikiWriteEvent) {
	p.inner.PublishWikiEvent(evt)
}

func (p *capturePublisherWithSections) EnqueueSectionsRefresh() {
	p.cache.Enqueue()
}

func sectionPresent(sections []DiscoveredSection, slug string) bool {
	for _, s := range sections {
		if s.Slug == slug {
			return true
		}
	}
	return false
}

func TestWikiSectionsCacheStartSynchronousCompute(t *testing.T) {
	_, worker := sectionsTestRepo(t)
	writeArticle(t, worker, "ceo", "team/people/nazz.md", "# Nazz\n")
	writeArticle(t, worker, "ceo", "team/templates/brief.md", "# Brief\n")

	bp := &operations.Blueprint{
		WikiSchema: &operations.BlueprintWikiSchema{
			Dirs: []string{"team/people/"},
		},
	}
	cache := newWikiSectionsCache(worker, func() *operations.Blueprint { return bp }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache.Start(ctx)
	defer cache.Stop()

	sections := cache.Sections()
	if len(sections) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(sections), sections)
	}
	slugs := make([]string, 0, len(sections))
	for _, s := range sections {
		slugs = append(slugs, s.Slug)
	}
	sort.Strings(slugs)
	if slugs[0] != "people" || slugs[1] != "templates" {
		t.Errorf("slugs=%v want [people templates]", slugs)
	}
}

// TestDiscoverSectionsStableFilesystemFallback guarantees that a section
// created outside the worker (e.g. bootstrap materializer laid down a
// dir before the worker turned on) still resolves a non-zero timestamp
// from filesystem mtime. Catches a regression where we silently drop
// the section because git log returned empty.
func TestDiscoverSectionsStableFilesystemFallback(t *testing.T) {
	repo, _ := sectionsTestRepo(t)
	dir := filepath.Join(repo.Root(), "team", "bootstrap")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.md"), []byte("# seed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sections, err := DiscoverSections(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("DiscoverSections: %v", err)
	}
	if len(sections) != 1 || sections[0].Slug != "bootstrap" {
		t.Fatalf("sections=%+v want one 'bootstrap'", sections)
	}
	if sections[0].FirstSeenTs.IsZero() {
		t.Error("FirstSeenTs is zero — expected mtime fallback")
	}
}
