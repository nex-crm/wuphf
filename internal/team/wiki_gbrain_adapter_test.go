package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// fakeWikiReadClient is an in-memory wikiReadClient for handler tests. It
// records the slug/query/pattern it was asked for so the path<->slug mapping can
// be asserted, and can simulate an unreachable gbrain via the unavailable flag.
type fakeWikiReadClient struct {
	pages map[string]gbrain.Page // slug -> page
	hits  []gbrain.Hit
	metas []gbrain.PageMeta

	unavailable bool // when true every method returns ErrNotInstalled-wrapped

	gotSlug    string
	gotQuery   string
	gotPattern string
	gotLimit   int
}

func (f *fakeWikiReadClient) errUnavailable() error {
	return fmt.Errorf("fake gbrain down: %w", gbrain.ErrNotInstalled)
}

func (f *fakeWikiReadClient) Query(_ context.Context, query string, limit int) ([]gbrain.Hit, error) {
	f.gotQuery = query
	f.gotLimit = limit
	if f.unavailable {
		return nil, f.errUnavailable()
	}
	return f.hits, nil
}

func (f *fakeWikiReadClient) Search(_ context.Context, query string, limit int) ([]gbrain.Hit, error) {
	f.gotPattern = query
	f.gotLimit = limit
	if f.unavailable {
		return nil, f.errUnavailable()
	}
	return f.hits, nil
}

func (f *fakeWikiReadClient) GetPage(_ context.Context, slug string) (gbrain.Page, error) {
	f.gotSlug = slug
	if f.unavailable {
		return gbrain.Page{}, f.errUnavailable()
	}
	page, ok := f.pages[slug]
	if !ok {
		return gbrain.Page{}, fmt.Errorf("page %q not found", slug)
	}
	return page, nil
}

func (f *fakeWikiReadClient) ListPages(_ context.Context, opts gbrain.ListOptions) ([]gbrain.PageMeta, error) {
	f.gotLimit = opts.Limit
	if f.unavailable {
		return nil, f.errUnavailable()
	}
	return f.metas, nil
}

// gbrainWikiTestServer mounts the four read handlers on a broker with no
// markdown worker, so every request takes the gbrain path. The supplied client
// is installed via the test override and cleared on cleanup.
func gbrainWikiTestServer(t *testing.T, client wikiReadClient) string {
	t.Helper()
	setWikiReadClientForTest(client)
	t.Cleanup(func() { setWikiReadClientForTest(nil) })

	b := &Broker{}
	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/read", b.handleWikiRead)
	mux.HandleFunc("/wiki/search", b.handleWikiSearch)
	mux.HandleFunc("/wiki/list", b.handleWikiList)
	mux.HandleFunc("/wiki/catalog", b.handleWikiCatalog)
	mux.HandleFunc("/wiki/lookup", b.handleWikiLookup)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// ── path <-> slug mapping ────────────────────────────────────────────────────

func TestWikiPathSlugRoundTrip(t *testing.T) {
	cases := []struct {
		path string
		slug string
	}{
		{"team/concepts/foo.md", "concepts/foo"},
		{"team/people/nazz.md", "people/nazz"},
		{"team/foo.md", "foo"},
		{"team/a/b/c.md", "a/b/c"},
	}
	for _, c := range cases {
		if got := wikiPathToSlug(c.path); got != c.slug {
			t.Errorf("wikiPathToSlug(%q) = %q, want %q", c.path, got, c.slug)
		}
		if got := wikiSlugToPath(c.slug); got != c.path {
			t.Errorf("wikiSlugToPath(%q) = %q, want %q", c.slug, got, c.path)
		}
		// Round-trip: path -> slug -> path is stable.
		if got := wikiSlugToPath(wikiPathToSlug(c.path)); got != c.path {
			t.Errorf("round-trip path %q -> %q", c.path, got)
		}
	}
}

func TestWikiGroupFromPath(t *testing.T) {
	if got := wikiGroupFromPath("team/people/nazz.md"); got != "people" {
		t.Errorf("group = %q, want people", got)
	}
	if got := wikiGroupFromPath("team/foo.md"); got != "" {
		t.Errorf("flat group = %q, want empty", got)
	}
}

// ── read ─────────────────────────────────────────────────────────────────────

func TestWikiReadFromGBrain(t *testing.T) {
	fake := &fakeWikiReadClient{pages: map[string]gbrain.Page{
		"people/nazz": {Slug: "people/nazz", Title: "Nazz", Content: "# Nazz\n\nFounder.\n"},
	}}
	base := gbrainWikiTestServer(t, fake)

	resp, err := http.Get(base + "/wiki/read?path=team/people/nazz.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Founder") {
		t.Errorf("body = %q, want to contain Founder", body)
	}
	// path -> slug mapping reached gbrain as the bare slug.
	if fake.gotSlug != "people/nazz" {
		t.Errorf("gbrain got slug %q, want people/nazz", fake.gotSlug)
	}
}

func TestWikiReadFromGBrainNotFound(t *testing.T) {
	fake := &fakeWikiReadClient{pages: map[string]gbrain.Page{}}
	base := gbrainWikiTestServer(t, fake)

	resp, err := http.Get(base + "/wiki/read?path=team/people/missing.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestWikiReadFromGBrainUnavailable(t *testing.T) {
	// nil client (no override, no broker client) → 503 + unavailable header.
	base := gbrainWikiTestServer(t, nil)

	resp, err := http.Get(base + "/wiki/read?path=team/people/nazz.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if h := resp.Header.Get(wikiBackendHeader); h != wikiBackendUnavailable {
		t.Errorf("%s = %q, want %q", wikiBackendHeader, h, wikiBackendUnavailable)
	}
}

// ── search ───────────────────────────────────────────────────────────────────

func TestWikiSearchFromGBrain(t *testing.T) {
	fake := &fakeWikiReadClient{hits: []gbrain.Hit{
		{Slug: "people/nazz", Title: "Nazz", ChunkText: "Founder of the company"},
		{Slug: "accounts/corti", Title: "Corti", ChunkText: "Account brief"},
	}}
	base := gbrainWikiTestServer(t, fake)

	resp, err := http.Get(base + "/wiki/search?pattern=Founder")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Hits []WikiSearchHit `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(out.Hits))
	}
	if out.Hits[0].Path != "team/people/nazz.md" {
		t.Errorf("hit[0].path = %q, want team/people/nazz.md", out.Hits[0].Path)
	}
	if out.Hits[0].Snippet != "Founder of the company" {
		t.Errorf("hit[0].snippet = %q", out.Hits[0].Snippet)
	}
	if fake.gotPattern != "Founder" {
		t.Errorf("gbrain got pattern %q, want Founder", fake.gotPattern)
	}
}

func TestWikiSearchFromGBrainUnavailableEmpty(t *testing.T) {
	fake := &fakeWikiReadClient{unavailable: true}
	base := gbrainWikiTestServer(t, fake)

	resp, err := http.Get(base + "/wiki/search?pattern=Founder")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if h := resp.Header.Get(wikiBackendHeader); h != wikiBackendUnavailable {
		t.Errorf("%s = %q, want %q", wikiBackendHeader, h, wikiBackendUnavailable)
	}
	var out struct {
		Hits []WikiSearchHit `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Hits) != 0 {
		t.Errorf("hits = %d, want 0 (empty shape preserved)", len(out.Hits))
	}
}

// ── list ─────────────────────────────────────────────────────────────────────

func TestWikiListFromGBrain(t *testing.T) {
	fake := &fakeWikiReadClient{metas: []gbrain.PageMeta{
		{Slug: "people/nazz", Title: "Nazz"},
		{Slug: "accounts/corti", Title: "Corti"},
	}}
	base := gbrainWikiTestServer(t, fake)

	resp, err := http.Get(base + "/wiki/list")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Team wiki index") {
		t.Errorf("body missing heading: %q", body)
	}
	if !strings.Contains(body, "[[team/people/nazz.md]]") {
		t.Errorf("body missing nazz link: %q", body)
	}
}

// ── catalog ──────────────────────────────────────────────────────────────────

func TestWikiCatalogFromGBrain(t *testing.T) {
	fake := &fakeWikiReadClient{metas: []gbrain.PageMeta{
		{Slug: "people/nazz", Title: "Nazz", Updated: "2026-06-01T00:00:00Z", Tags: []string{"founders"}},
		{Slug: "accounts/corti", Title: "Corti"},
	}}
	base := gbrainWikiTestServer(t, fake)

	resp, err := http.Get(base + "/wiki/catalog")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Articles []CatalogEntry `json:"articles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Articles) != 2 {
		t.Fatalf("articles = %d, want 2", len(out.Articles))
	}
	a := out.Articles[0]
	if a.Path != "team/people/nazz.md" {
		t.Errorf("path = %q, want team/people/nazz.md", a.Path)
	}
	if a.Title != "Nazz" {
		t.Errorf("title = %q, want Nazz", a.Title)
	}
	if a.Group != "people" {
		t.Errorf("group = %q, want people", a.Group)
	}
	if a.LastEditedTs != "2026-06-01T00:00:00Z" {
		t.Errorf("last_edited_ts = %q", a.LastEditedTs)
	}
	if len(a.Categories) != 1 || a.Categories[0] != "founders" {
		t.Errorf("categories = %v, want [founders]", a.Categories)
	}
	// Categories must always be non-nil even when gbrain returns no tags.
	if out.Articles[1].Categories == nil {
		t.Error("articles[1].Categories is nil; must be empty slice")
	}
}

func TestWikiCatalogFromGBrainUnavailableEmpty(t *testing.T) {
	base := gbrainWikiTestServer(t, nil) // nil client

	resp, err := http.Get(base + "/wiki/catalog")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if h := resp.Header.Get(wikiBackendHeader); h != wikiBackendUnavailable {
		t.Errorf("%s = %q, want %q", wikiBackendHeader, h, wikiBackendUnavailable)
	}
	var out struct {
		Articles []CatalogEntry `json:"articles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Articles == nil {
		t.Error("articles is nil; must be empty array")
	}
}

// ── lookup ───────────────────────────────────────────────────────────────────

func TestWikiLookupFromGBrainUnavailable(t *testing.T) {
	// nil client → lookup degrades to 503 + unavailable header before any
	// provider call.
	base := gbrainWikiTestServer(t, nil)

	resp, err := http.Get(base + "/wiki/lookup?q=who+is+nazz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if h := resp.Header.Get(wikiBackendHeader); h != wikiBackendUnavailable {
		t.Errorf("%s = %q, want %q", wikiBackendHeader, h, wikiBackendUnavailable)
	}
}

// cannedAnswerProvider returns a canned cited-answer JSON so AnswerWithSources can
// be exercised without a real LLM.
type cannedAnswerProvider struct{ gotPrompt string }

func (f *cannedAnswerProvider) RunPrompt(_ context.Context, _, userPrompt string) (string, error) {
	f.gotPrompt = userPrompt
	return `{"query_class":"entity_lookup","answer_markdown":"Nazz is the founder.","sources_cited":[1],"confidence":0.9,"coverage":"complete"}`, nil
}

func TestAnswerWithSourcesSynthesisFromGBrainHits(t *testing.T) {
	prov := &cannedAnswerProvider{}
	h := NewQueryHandler(nil, prov) // nil index: synthesis must not touch it

	sources := []QuerySource{
		gbrainHitToSource(gbrain.Hit{Slug: "people/nazz", Title: "Nazz", ChunkText: "Founder of the company", ChunkSource: "team/people/nazz.md"}),
	}
	ans, err := h.AnswerWithSources(context.Background(), QueryRequest{Query: "who is nazz"}, sources)
	if err != nil {
		t.Fatalf("AnswerWithSources: %v", err)
	}
	if ans.AnswerMarkdown != "Nazz is the founder." {
		t.Errorf("answer = %q", ans.AnswerMarkdown)
	}
	if len(ans.SourcesCited) != 1 || ans.SourcesCited[0] != 1 {
		t.Errorf("sources_cited = %v, want [1]", ans.SourcesCited)
	}
	if len(ans.Sources) != 1 || ans.Sources[0].SlugOrID != "people/nazz" {
		t.Errorf("sources = %+v", ans.Sources)
	}
	if prov.gotPrompt == "" {
		t.Error("provider prompt was empty")
	}
}
