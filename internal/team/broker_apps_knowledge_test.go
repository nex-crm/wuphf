package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

func TestKnowledgePageGBrainRoundTrip(t *testing.T) {
	page := appKnowledgePage{
		ID:       "world-weather",
		Title:    "World Weather",
		Category: "Weather app",
		Summary:  "Weather for five cities.",
		Infobox:  []appKnowledgeInfoRow{{Label: "Unit", Value: "Celsius"}},
		Lead:     "Shows temperatures in Celsius.[[1]]",
		Sections: []appKnowledgeSection{
			{Heading: "What it shows", Paras: []string{"Temp in degrees C.[[1]]"}},
		},
		References: []appKnowledgeRef{
			{N: 1, Title: "App brief", Detail: "spec", Kind: "document", Snippet: "…", Why: "states the unit"},
		},
		Categories: []string{"Weather"},
		SeeAlso:    []string{"other"},
	}
	content, err := renderKnowledgePageForGBrain(page, []string{appKnowledgeScopeTag("app_d50e34194a87a5ed")})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Frontmatter carries the app-scope tag for ListPages filtering.
	if !strings.Contains(content, "wuphf-app-d50e34194a87a5ed") {
		t.Fatalf("missing app-scope tag in frontmatter:\n%s", content)
	}
	// The readable body strips citation markers (the exact form is in base64).
	body := content[strings.LastIndex(content, "---\n\n")+len("---\n\n"):]
	readable := body[:strings.Index(body, "<!--")]
	if strings.Contains(readable, "[[1]]") {
		t.Fatalf("readable body should strip [[n]] citations:\n%s", readable)
	}
	if !strings.Contains(readable, "World Weather") {
		t.Fatalf("readable body should contain the title")
	}
	// The structured page round-trips exactly.
	got, ok := decodeKnowledgePageFromBody(content)
	if !ok {
		t.Fatalf("decode failed")
	}
	a, _ := json.Marshal(page)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("round-trip mismatch:\n want %s\n got  %s", a, b)
	}
}

func TestAppKnowledgeSlugAndTag(t *testing.T) {
	if got := appKnowledgeScopeTag("app_abc123"); got != "wuphf-app-abc123" {
		t.Fatalf("scope tag = %q", got)
	}
	if got := appKnowledgeSlug("app_abc123", "world-weather"); got != "k-abc123-world-weather" {
		t.Fatalf("slug = %q", got)
	}
}

func TestDecodeKnowledgePageRejectsGarbage(t *testing.T) {
	if _, ok := decodeKnowledgePageFromBody("no marker here"); ok {
		t.Fatalf("should reject body with no marker")
	}
	if _, ok := decodeKnowledgePageFromBody("<!--wuphf-knowledge-b64:%%%notb64%%%-->"); ok {
		t.Fatalf("should reject invalid base64")
	}
}

func testSources() []knowledgeSource {
	return []knowledgeSource{
		{N: 1, Kind: "document", Title: "App: X", Detail: "spec", Snippet: "does a thing"},
		{N: 2, Kind: "roster", Title: "Office roster", Detail: "team", Snippet: "Maya — RevOps"},
	}
}

func TestSanitizeKnowledgePagesDropsUngroundedRefsAndPages(t *testing.T) {
	pages := []appKnowledgePage{
		{
			Title: "Good Page",
			Lead:  "A fact.[[1]]",
			References: []appKnowledgeRef{
				{N: 1, Title: "App: X", Kind: "document", Snippet: "does a thing", Why: "it says so"},
				{N: 9, Title: "Made up", Kind: "document", Snippet: "hallucinated"}, // no such source → dropped
			},
			SeeAlso:    []string{"ghost-page"}, // no such page → dropped
			Categories: []string{"Ops", "  "},
		},
		{
			// No grounded references at all → whole page dropped.
			Title:      "Empty Refs",
			Lead:       "Unsupported claim.",
			References: []appKnowledgeRef{{N: 42, Title: "nope"}},
		},
		{
			// A REAL source listed in references but never cited as [[1]] in the
			// prose → the ref is decoration, not grounding: ref and page dropped.
			Title:      "Uncited Ref",
			Lead:       "A claim with no citation marker.",
			References: []appKnowledgeRef{{N: 1, Title: "App: X", Kind: "document"}},
		},
	}
	out := sanitizeKnowledgePages(pages, testSources())
	if len(out) != 1 {
		t.Fatalf("want 1 page (ungrounded + uncited dropped), got %d", len(out))
	}
	p := out[0]
	if p.ID != "good-page" {
		t.Fatalf("id should slugify from title, got %q", p.ID)
	}
	if len(p.References) != 1 || p.References[0].N != 1 {
		t.Fatalf("want only the grounded ref [1], got %+v", p.References)
	}
	if len(p.SeeAlso) != 0 {
		t.Fatalf("seeAlso to a non-existent page should be dropped, got %v", p.SeeAlso)
	}
	if len(p.Categories) != 1 || p.Categories[0] != "Ops" {
		t.Fatalf("blank categories should be trimmed, got %v", p.Categories)
	}
	if p.UpdatedAt == "" {
		t.Fatalf("updatedAt should be stamped server-side")
	}
}

func TestSanitizeRefsNormalizesKindAndFillsFromSource(t *testing.T) {
	refs := []appKnowledgeRef{
		{N: 1, Kind: "bogus"},           // unknown kind → source kind (document)
		{N: 2, Title: "", Kind: "chat"}, // title filled from source
		{N: 1, Kind: "document"},        // duplicate n → dropped
	}
	byN := map[int]knowledgeSource{}
	for _, s := range testSources() {
		byN[s.N] = s
	}
	cited := map[int]bool{1: true, 2: true}
	out := sanitizeRefs(refs, byN, cited)
	if len(out) != 2 {
		t.Fatalf("want 2 refs (dup dropped), got %d", len(out))
	}
	if out[0].Kind != "document" {
		t.Fatalf("unknown kind should normalize to source kind, got %q", out[0].Kind)
	}
	if out[1].Title != "Office roster" {
		t.Fatalf("empty title should fill from source, got %q", out[1].Title)
	}
	// A ref with an empty snippet gets one from the source.
	if out[0].Snippet == "" {
		t.Fatalf("empty snippet should fall back to the source excerpt")
	}
}

// TestSanitizeRefsCanonicalizesMismatchedMetadata locks that model-provided
// title/detail/kind/snippet are NEVER trusted: a ref whose n maps to a real
// source is overwritten with that source's metadata, so a page cannot
// misattribute a claim to a source it did not come from. Why stays the model's.
func TestSanitizeRefsCanonicalizesMismatchedMetadata(t *testing.T) {
	refs := []appKnowledgeRef{{
		N:       1,
		Title:   "Totally Different Doc", // mismatched, non-empty → overwritten
		Detail:  "made-up detail",
		Kind:    "crm", // valid kind, but not the source's → overwritten
		Snippet: "a snippet the source never contained",
		Why:     "explains the claim",
	}}
	byN := map[int]knowledgeSource{}
	for _, s := range testSources() {
		byN[s.N] = s
	}
	out := sanitizeRefs(refs, byN, map[int]bool{1: true})
	if len(out) != 1 {
		t.Fatalf("want 1 ref, got %d", len(out))
	}
	got := out[0]
	if got.Title != "App: X" || got.Detail != "spec" || got.Kind != "document" || got.Snippet != "does a thing" {
		t.Fatalf("ref metadata must canonicalize from the source, got %+v", got)
	}
	if got.Why != "explains the claim" {
		t.Fatalf("why is the model's explanation and must be preserved, got %q", got.Why)
	}
}

// TestSanitizeRefsDropsUncitedRefs locks that a reference whose [[n]] never
// appears in the prose is dropped even when it maps to a real source.
func TestSanitizeRefsDropsUncitedRefs(t *testing.T) {
	byN := map[int]knowledgeSource{}
	for _, s := range testSources() {
		byN[s.N] = s
	}
	out := sanitizeRefs([]appKnowledgeRef{{N: 1, Kind: "document"}, {N: 2, Kind: "roster"}}, byN, map[int]bool{2: true})
	if len(out) != 1 || out[0].N != 2 {
		t.Fatalf("want only the cited ref [2], got %+v", out)
	}
}

func TestSanitizeKnowledgePagesCapsAtThree(t *testing.T) {
	mk := func(id string) appKnowledgePage {
		return appKnowledgePage{
			Title:      id,
			Lead:       "About " + id + ".[[1]]",
			References: []appKnowledgeRef{{N: 1, Kind: "document"}},
		}
	}
	pages := []appKnowledgePage{mk("a"), mk("b"), mk("c"), mk("d")}
	out := sanitizeKnowledgePages(pages, testSources())
	if len(out) != 3 {
		t.Fatalf("want at most 3 pages, got %d", len(out))
	}
}

// TestAppDataHeldSummaryFlattensStringArrays locks that string[] cells surface
// their ELEMENTS as individual values (deduped across rows), not one formatted
// "[a b]" blob per row.
func TestAppDataHeldSummaryFlattensStringArrays(t *testing.T) {
	tables := []AppDBTable{{
		Name: "Emails",
		Columns: []AppDBColumn{
			{Name: "tags", Type: "string[]"},
			{Name: "city", Type: "string"},
		},
		Rows: []map[string]any{
			{"tags": []any{"sales", "ops"}, "city": "Austin"},
			{"tags": []string{"ops", "finance"}, "city": "Boston"},
			{"tags": nil, "city": ""},
		},
	}}
	got := appDataHeldSummary(tables)
	if !strings.Contains(got, "tags: sales, ops, finance") {
		t.Fatalf("string[] elements should flatten + dedupe, got %q", got)
	}
	if strings.Contains(got, "[") {
		t.Fatalf("no whole-array formatting should leak into the summary, got %q", got)
	}
	if !strings.Contains(got, "city: Austin, Boston") {
		t.Fatalf("plain string column should still summarize, got %q", got)
	}
}

func TestAppKnowledgeScopeTagRoundTrip(t *testing.T) {
	tag := appKnowledgeScopeTag("app_d50e34194a87a5ed")
	if tag != "wuphf-app-d50e34194a87a5ed" {
		t.Fatalf("scope tag = %q", tag)
	}
	id, ok := appIDFromScopeTag(tag)
	if !ok || id != "app_d50e34194a87a5ed" {
		t.Fatalf("reverse = %q,%v", id, ok)
	}
	// The shared knowledge tag is NOT an app-scope tag.
	if _, ok := appIDFromScopeTag(appKnowledgeTag); ok {
		t.Fatalf("wuphf-app-knowledge must not resolve to an app id")
	}
}

func TestAppScopeTagsHelpers(t *testing.T) {
	tags := []string{
		"wuphf-app-knowledge",
		"wuphf-app-d50e34194a87a5ed",
		"wuphf-app-aaaa1111bbbb2222",
		"office",
	}
	scoped := appScopeTagsOf(tags)
	if len(scoped) != 2 {
		t.Fatalf("want 2 app-scope tags, got %v", scoped)
	}
	// mergeScopeTags unions + dedups, keeping only real app-scope tags.
	merged := mergeScopeTags(
		[]string{"wuphf-app-d50e34194a87a5ed"},
		[]string{"wuphf-app-d50e34194a87a5ed", "wuphf-app-cccc3333dddd4444", "office"},
	)
	if len(merged) != 2 {
		t.Fatalf("want 2 merged scope tags (dedup + drop non-app), got %v", merged)
	}
}

// ── fake brain: an in-memory knowledgeBrain for the gbrain-backed paths ───────

// Valid custom-app ids (app_ + 16 hex) — appIDFromScopeTag validates the id
// shape, so scope tags built from short fake ids would be silently dropped.
const (
	testKnowledgeAppX = "app_00000000000000aa"
	testKnowledgeAppY = "app_00000000000000bb"
)

// fakeBrain implements knowledgeBrain in memory. PutPage parses title/tags from
// the rendered frontmatter (the same shape renderKnowledgePageForGBrain emits),
// so ListPages-by-tag and GetPage behave like the real brain.
type fakeBrain struct {
	mu      sync.Mutex
	pages   map[string]gbrain.Page
	links   map[string][]gbrain.Link
	putErr  error // injected PutPage failure
	listErr error // injected ListPages failure (unreachable brain)
}

func newFakeBrain() *fakeBrain {
	return &fakeBrain{pages: map[string]gbrain.Page{}, links: map[string][]gbrain.Link{}}
}

func (f *fakeBrain) put(slug, content string) error {
	var fm struct {
		Title string   `yaml:"title"`
		Tags  []string `yaml:"tags"`
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) == 3 {
		if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
			return err
		}
	}
	f.pages[slug] = gbrain.Page{Slug: slug, Title: fm.Title, Content: content, Tags: fm.Tags}
	return nil
}

func (f *fakeBrain) GetPage(_ context.Context, slug string) (gbrain.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pages[slug]
	if !ok {
		return gbrain.Page{}, fmt.Errorf("page %q not found", slug)
	}
	return p, nil
}

func (f *fakeBrain) ListPages(_ context.Context, opts gbrain.ListOptions) ([]gbrain.PageMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []gbrain.PageMeta
	for _, p := range f.pages {
		if opts.Tag != "" && !slices.Contains(p.Tags, opts.Tag) {
			continue
		}
		out = append(out, gbrain.PageMeta{Slug: p.Slug, Title: p.Title, Tags: p.Tags})
	}
	return out, nil
}

func (f *fakeBrain) PutPage(_ context.Context, content string, opts gbrain.PutOptions) (gbrain.PutResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return gbrain.PutResult{}, f.putErr
	}
	if err := f.put(opts.Slug, content); err != nil {
		return gbrain.PutResult{}, err
	}
	return gbrain.PutResult{Slug: opts.Slug, Status: "ok"}, nil
}

func (f *fakeBrain) AddLink(_ context.Context, from, to, linkType, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[from] = append(f.links[from], gbrain.Link{From: from, To: to, Type: linkType})
	return nil
}

func (f *fakeBrain) GetLinks(_ context.Context, slug string) ([]gbrain.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.links[slug], nil
}

// knowledgeTestPage builds a minimal page that survives sanitize/decode.
func knowledgeTestPage(id, title string, seeAlso ...string) appKnowledgePage {
	return appKnowledgePage{
		ID:      id,
		Title:   title,
		Lead:    "A lead about " + title + ".",
		SeeAlso: seeAlso,
	}
}

// TestAppKnowledgeEmptySynthesisCachedWhenGBrainBacked locks the cache contract
// for a genuinely-empty result: gbrain holds nothing to read back, so without
// the file-cache marker every GET would re-run the LLM synthesis. The completer
// must run exactly ONCE across two GETs.
func TestAppKnowledgeEmptySynthesisCachedWhenGBrainBacked(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	b.knowledgeBrainOverride = newFakeBrain()
	var synthCalls atomic.Int32
	withFakeAppsLLM(t, func(context.Context, string, string) (string, error) {
		synthCalls.Add(1)
		return `{"pages": []}`, nil
	})
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	regBody, _ := json.Marshal(map[string]any{
		"name": "Empty App", "description": "An app with nothing worth writing down.",
		"html": validAppHTML,
	})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, regBody)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id: %v", created)
	}

	for i := 1; i <= 2; i++ {
		status, out := getAppsJSON(t, base+"/apps/"+id+"/knowledge", b.Token())
		if status != http.StatusOK {
			t.Fatalf("GET knowledge #%d: %d", i, status)
		}
		if pages, _ := out["pages"].([]any); len(pages) != 0 {
			t.Fatalf("GET #%d pages = %v, want empty", i, out["pages"])
		}
	}
	if got := synthCalls.Load(); got != 1 {
		t.Fatalf("synthesis ran %d times across two GETs, want 1 (empty result must be cached)", got)
	}
}

// TestKnowledgeSeeAlsoFilteredToServedSet locks that a shared page's inherited
// SeeAlso ids (synthesized for its OWNER app) are dropped when they are not in
// the set served to the current app — otherwise the FE renders a dead link.
func TestKnowledgeSeeAlsoFilteredToServedSet(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	brain := newFakeBrain()
	ctx := context.Background()

	pages := []appKnowledgePage{
		knowledgeTestPage("pa", "Alpha", "pb", "ghost-not-served"),
		knowledgeTestPage("pb", "Beta"),
	}
	if err := b.writeAppKnowledgeToGBrain(ctx, brain, testKnowledgeAppX, pages); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := b.readAppKnowledgeFromGBrain(ctx, brain, testKnowledgeAppX)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var alpha *appKnowledgePage
	for i := range got {
		if got[i].Title == "Alpha" {
			alpha = &got[i]
		}
	}
	if alpha == nil {
		t.Fatalf("Alpha not served: %v", got)
	}
	if len(alpha.SeeAlso) != 1 || alpha.SeeAlso[0] != "pb" {
		t.Fatalf("Alpha.SeeAlso = %v, want just [pb] (ghost id filtered)", alpha.SeeAlso)
	}
}

// TestKnowledgeSharedPageRetagFailureSurfaces locks that a failed re-tag of a
// shared page returns an error instead of silently linking the app to a page
// whose scope tag never persisted.
func TestKnowledgeSharedPageRetagFailureSurfaces(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	brain := newFakeBrain()
	ctx := context.Background()

	// Seed a same-title page owned by ANOTHER app (bypassing putErr).
	owner := knowledgeTestPage("py", "Shared Topic")
	content, err := renderKnowledgePageForGBrain(owner, []string{appKnowledgeScopeTag(testKnowledgeAppY)})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	brain.mu.Lock()
	if err := brain.put(appKnowledgeSlug(testKnowledgeAppY, owner.Title), content); err != nil {
		brain.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	brain.mu.Unlock()

	// Now writes fail: sharing must SURFACE the re-tag failure.
	brain.putErr = fmt.Errorf("brain down")
	err = b.writeAppKnowledgeToGBrain(ctx, brain, testKnowledgeAppX, []appKnowledgePage{knowledgeTestPage("px", "Shared Topic")})
	if err == nil {
		t.Fatal("re-tag failure was swallowed, want error")
	}
}

// TestAppKnowledgeGBrainReadFailureServesCache locks the fallback contract for
// an UNREACHABLE brain (dead serve, PGLite lock contention, missing binary): a
// prior synthesis whose gbrain write failed lives on in the per-app file cache
// (stamped by the marker write), and the handler must serve it rather than tell
// the user "no knowledge yet" and burn a fresh LLM pass whose result could not
// persist anyway.
func TestAppKnowledgeGBrainReadFailureServesCache(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	brain := newFakeBrain()
	b.knowledgeBrainOverride = brain
	var synthCalls atomic.Int32
	withFakeAppsLLM(t, func(context.Context, string, string) (string, error) {
		synthCalls.Add(1)
		return `{"pages": []}`, nil
	})
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	regBody, _ := json.Marshal(map[string]any{
		"name": "Cached App", "description": "An app with a cached operating guide.",
		"html": validAppHTML,
	})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, regBody)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id: %v", created)
	}
	if err := b.appStore().WriteAppKnowledge(id, []appKnowledgePage{knowledgeTestPage("p1", "Operating Guide")}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	brain.mu.Lock()
	brain.listErr = fmt.Errorf("connect gbrain mcp: context deadline exceeded")
	brain.mu.Unlock()

	status, out := getAppsJSON(t, base+"/apps/"+id+"/knowledge", b.Token())
	if status != http.StatusOK {
		t.Fatalf("GET knowledge: %d", status)
	}
	pages, _ := out["pages"].([]any)
	if len(pages) != 1 {
		t.Fatalf("pages = %v, want the one cached page", out["pages"])
	}
	first, _ := pages[0].(map[string]any)
	if got, _ := first["title"].(string); got != "Operating Guide" {
		t.Fatalf("title = %q, want cached \"Operating Guide\"", got)
	}
	if errStr, _ := out["error"].(string); errStr != "" {
		t.Fatalf("error = %q, want none (a cache serve is a good response)", errStr)
	}
	if got := synthCalls.Load(); got != 0 {
		t.Fatalf("synthesis ran %d times, want 0 (the cache must satisfy the read)", got)
	}

	// The empty MARKER also survives an unreachable brain: an app whose last
	// synthesis was genuinely empty serves empty without a fresh LLM pass.
	if err := b.appStore().WriteAppKnowledge(id, nil); err != nil {
		t.Fatalf("seed empty marker: %v", err)
	}
	status, out = getAppsJSON(t, base+"/apps/"+id+"/knowledge", b.Token())
	if status != http.StatusOK {
		t.Fatalf("GET knowledge (empty marker): %d", status)
	}
	if pages, _ := out["pages"].([]any); len(pages) != 0 {
		t.Fatalf("pages = %v, want empty from the marker", out["pages"])
	}
	if got := synthCalls.Load(); got != 0 {
		t.Fatalf("synthesis ran %d times, want 0 (the empty marker must satisfy the read)", got)
	}
}

// TestAppKnowledgeGBrainEmptyStaysAuthoritative locks the OTHER side of the
// fallback boundary: when the brain is REACHABLE and simply holds no pages, a
// stale non-empty file cache must NOT be served — pages deleted from the brain
// stay deleted, and the handler re-synthesizes instead.
func TestAppKnowledgeGBrainEmptyStaysAuthoritative(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	b.knowledgeBrainOverride = newFakeBrain()
	var synthCalls atomic.Int32
	withFakeAppsLLM(t, func(context.Context, string, string) (string, error) {
		synthCalls.Add(1)
		return `{"pages": []}`, nil
	})
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	regBody, _ := json.Marshal(map[string]any{
		"name": "Stale Cache App", "description": "An app whose brain pages were deleted.",
		"html": validAppHTML,
	})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, regBody)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id: %v", created)
	}
	if err := b.appStore().WriteAppKnowledge(id, []appKnowledgePage{knowledgeTestPage("p1", "Stale Page")}); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}

	status, out := getAppsJSON(t, base+"/apps/"+id+"/knowledge", b.Token())
	if status != http.StatusOK {
		t.Fatalf("GET knowledge: %d", status)
	}
	if pages, _ := out["pages"].([]any); len(pages) != 0 {
		t.Fatalf("pages = %v, want empty (stale cache must not mask a reachable brain)", out["pages"])
	}
	if got := synthCalls.Load(); got != 1 {
		t.Fatalf("synthesis ran %d times, want 1 (reachable-but-empty brain re-synthesizes)", got)
	}
}
