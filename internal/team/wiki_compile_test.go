package team

// wiki_compile_test.go covers the deterministic compile engine end-to-end with
// a fake PamRunner. The fake distinguishes extract calls from page calls by
// inspecting the system prompt, returning canned JSON for the former and canned
// markdown for the latter, so every test is fully deterministic.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCompileRunner returns canned extract JSON keyed by source title and
// canned page markdown keyed by concept title. It records every call for
// assertions and is safe for concurrent use.
type fakeCompileRunner struct {
	mu sync.Mutex
	// extractByTitle maps a source Title to the JSON its extract call returns.
	extractByTitle map[string]string
	// pageBody is the markdown returned for every page call when pageByTitle
	// has no entry.
	pageBody string
	// pageByTitle overrides pageBody per concept title.
	pageByTitle map[string]string
	// extractErrTitle, when matched against a source title, makes the extract
	// call fail.
	extractErrTitle string

	extractCalls int
	pageCalls    int
}

func (f *fakeCompileRunner) Run(_ context.Context, system, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(system, "You are a knowledge-extraction engine") {
		f.extractCalls++
		for title, json := range f.extractByTitle {
			if strings.Contains(user, "Source title: "+title) {
				if f.extractErrTitle != "" && title == f.extractErrTitle {
					return "", fmt.Errorf("simulated extract failure for %s", title)
				}
				return json, nil
			}
		}
		return `{"concepts":[]}`, nil
	}
	// Page call.
	f.pageCalls++
	for title, body := range f.pageByTitle {
		if strings.Contains(user, "Concept title: "+title) {
			return body, nil
		}
	}
	return f.pageBody, nil
}

// --- parseExtraction ---------------------------------------------------------

func TestParseExtraction(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantLen   int
		wantFirst *ExtractedConcept
		wantErr   bool
	}{
		{
			name:    "valid",
			raw:     `{"concepts":[{"title":"Brex Pilot","slug":"brex-pilot","kind":"entity","summary":"A pilot.","tags":["sales","pilot"],"confidence":0.8}]}`,
			wantLen: 1,
			wantFirst: &ExtractedConcept{
				Title: "Brex Pilot", Slug: "brex-pilot", Kind: "entity",
				Summary: "A pilot.", Tags: []string{"sales", "pilot"}, Confidence: 0.8,
			},
		},
		{
			name:    "fenced",
			raw:     "```json\n{\"concepts\":[{\"title\":\"RRF\",\"slug\":\"rrf\",\"kind\":\"concept\",\"summary\":\"fusion\",\"confidence\":0.5}]}\n```",
			wantLen: 1,
		},
		{
			name:    "garbage wrapped",
			raw:     "Sure! Here is the JSON you asked for:\n{\"concepts\":[{\"title\":\"X\",\"slug\":\"x\",\"kind\":\"concept\",\"confidence\":0.3}]}\nHope that helps.",
			wantLen: 1,
		},
		{
			name:    "empty concepts",
			raw:     `{"concepts":[]}`,
			wantLen: 0,
		},
		{
			name:    "bad kind defaults to concept",
			raw:     `{"concepts":[{"title":"Y","slug":"y","kind":"banana","confidence":0.4}]}`,
			wantLen: 1,
			wantFirst: &ExtractedConcept{
				Title: "Y", Slug: "y", Kind: "concept", Confidence: 0.4,
			},
		},
		{
			name:    "confidence clamp high and low",
			raw:     `{"concepts":[{"title":"Hi","slug":"hi","kind":"concept","confidence":5.0},{"title":"Lo","slug":"lo","kind":"concept","confidence":-2.0}]}`,
			wantLen: 2,
		},
		{
			name:    "drops empty title and slug",
			raw:     `{"concepts":[{"title":"","slug":"a","kind":"concept"},{"title":"B","slug":"","kind":"concept"},{"title":"Keep","slug":"keep","kind":"concept"}]}`,
			wantLen: 1,
			wantFirst: &ExtractedConcept{
				Title: "Keep", Slug: "keep", Kind: "concept",
			},
		},
		{
			name:    "no json object",
			raw:     "the model refused",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExtraction(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseExtraction: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d (%+v)", len(got), tt.wantLen, got)
			}
			if tt.wantFirst != nil {
				assertConcept(t, got[0], *tt.wantFirst)
			}
			// Confidence clamp invariants on every concept.
			for _, c := range got {
				if c.Confidence < 0 || c.Confidence > 1 {
					t.Fatalf("confidence %v out of [0,1]", c.Confidence)
				}
				if c.Kind != "concept" && c.Kind != "entity" {
					t.Fatalf("kind %q not normalized", c.Kind)
				}
			}
		})
	}
}

func assertConcept(t *testing.T, got, want ExtractedConcept) {
	t.Helper()
	if got.Title != want.Title || got.Slug != want.Slug || got.Kind != want.Kind {
		t.Fatalf("concept = {%q,%q,%q}, want {%q,%q,%q}", got.Title, got.Slug, got.Kind, want.Title, want.Slug, want.Kind)
	}
	if want.Summary != "" && got.Summary != want.Summary {
		t.Fatalf("summary = %q, want %q", got.Summary, want.Summary)
	}
	if want.Tags != nil {
		if strings.Join(got.Tags, ",") != strings.Join(want.Tags, ",") {
			t.Fatalf("tags = %v, want %v", got.Tags, want.Tags)
		}
	}
}

// --- mergeExtractions --------------------------------------------------------

func TestMergeExtractions_SameSlugAcrossTwoSources(t *testing.T) {
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	srcA := mustSource(t, SourceKindNote, "src-a", "Doc A", "body a", base.Add(time.Hour))
	srcB := mustSource(t, SourceKindNote, "src-b", "Doc B", "body b", base)
	sources := []SourceRecord{srcA, srcB} // newest-first, as ListSources returns

	perSource := map[string][]ExtractedConcept{
		srcA.ID: {{Title: "Brex (lower conf)", Slug: "brex", Kind: "entity", Summary: "low", Tags: []string{"a", "shared"}, Confidence: 0.4}},
		srcB.ID: {{Title: "Brex (high conf)", Slug: "brex", Kind: "concept", Summary: "high", Tags: []string{"b", "shared"}, Confidence: 0.9}},
	}

	merged := mergeExtractions(perSource, sources)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged concept, got %d", len(merged))
	}
	mc := merged[0]
	if mc.Slug != "brex" {
		t.Fatalf("slug = %q", mc.Slug)
	}
	if len(mc.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(mc.Sources))
	}
	// Title/Kind/Summary from the highest-confidence extraction (0.9).
	if mc.Title != "Brex (high conf)" || mc.Kind != "concept" || mc.Summary != "high" {
		t.Fatalf("expected high-conf metadata, got {%q,%q,%q}", mc.Title, mc.Kind, mc.Summary)
	}
	// Confidence = min across sources.
	if mc.Confidence != 0.4 {
		t.Fatalf("confidence = %v, want 0.4 (min)", mc.Confidence)
	}
	// Tags = dedup union, first-seen order (source-list order: A then B).
	if strings.Join(mc.Tags, ",") != "a,shared,b" {
		t.Fatalf("tags = %v, want [a shared b]", mc.Tags)
	}
	// Sources in source-list order.
	if mc.Sources[0].ID != srcA.ID || mc.Sources[1].ID != srcB.ID {
		t.Fatalf("source order = [%s %s]", mc.Sources[0].ID, mc.Sources[1].ID)
	}
}

func TestMergeExtractions_DeterministicSlugOrder(t *testing.T) {
	src := mustSource(t, SourceKindNote, "s", "Doc", "body", time.Now().UTC())
	perSource := map[string][]ExtractedConcept{
		src.ID: {
			{Title: "Zed", Slug: "zed", Kind: "concept", Confidence: 0.5},
			{Title: "Alpha", Slug: "alpha", Kind: "concept", Confidence: 0.5},
			{Title: "Mid", Slug: "mid", Kind: "concept", Confidence: 0.5},
		},
	}
	merged := mergeExtractions(perSource, []SourceRecord{src})
	got := make([]string, len(merged))
	for i, m := range merged {
		got[i] = m.Slug
	}
	if strings.Join(got, ",") != "alpha,mid,zed" {
		t.Fatalf("order = %v, want [alpha mid zed]", got)
	}
}

// --- buildPagePrompt ---------------------------------------------------------

func TestBuildPagePrompt_ContainsSourceIDsAndRules(t *testing.T) {
	src1 := mustSource(t, SourceKindTask, "task-42", "Task 42", "We shipped the pilot.", time.Now().UTC())
	src2 := mustSource(t, SourceKindDecision, "decision-7", "Decision 7", "We chose Wails.", time.Now().UTC())
	mc := MergedConcept{
		Slug: "pilot", Title: "Pilot", Kind: "concept",
		Sources: []SourceRecord{src1, src2},
	}
	system, user := buildPagePrompt(mc, "", []string{"Other Page", "Pilot"})

	// System contract carries the load-bearing rules.
	for _, want := range []string{"^[source-id]", "Do NOT write an H1 title", "Do NOT invent facts"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
	// Both source ids must appear so the author can cite them.
	for _, id := range []string{"task-42", "decision-7"} {
		if !strings.Contains(user, "### source: "+id) {
			t.Fatalf("user prompt missing source id %q", id)
		}
	}
	// Related titles exclude the concept's own title.
	if !strings.Contains(user, "Other Page") {
		t.Fatalf("user prompt missing related title")
	}
	if strings.Contains(user, "Related pages") && strings.Contains(user, "[[wikilink]] syntax: Pilot") {
		t.Fatalf("related list should not include the concept's own title")
	}
}

// --- Compiler.Compile end-to-end --------------------------------------------

func TestCompilerCompile_EndToEnd(t *testing.T) {
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	ctx := context.Background()

	// Seed two sources. Source A yields a "concept" page, source B an "entity".
	seedSource(t, worker, SourceKindNote, "", "RRF Method", "Reciprocal rank fusion beats pure semantic search.")
	seedSource(t, worker, SourceKindNote, "", "Brex Company", "Brex is the pilot anchor account.")

	runner := &fakeCompileRunner{
		extractByTitle: map[string]string{
			"RRF Method":   `{"concepts":[{"title":"Reciprocal Rank Fusion","slug":"reciprocal-rank-fusion","kind":"concept","summary":"hybrid search","tags":["search","retrieval"],"confidence":0.9}]}`,
			"Brex Company": `{"concepts":[{"title":"Brex","slug":"brex","kind":"entity","summary":"anchor account","tags":["customer"],"confidence":0.8}]}`,
		},
		pageByTitle: map[string]string{
			"Reciprocal Rank Fusion": "RRF combines rankings. ^[note-rrf-method-...]\n\n## Details\nMore. ^[x]",
			"Brex":                   "Brex is an anchor. ^[y]",
		},
		pageBody: "Default body. ^[z]",
	}

	compiler := NewCompiler(repo, worker, runner)
	compiler.now = func() time.Time { return time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC) }

	result, err := compiler.Compile(ctx)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	worker.WaitForIdle()

	if result.SourcesRead != 2 {
		t.Fatalf("SourcesRead = %d, want 2", result.SourcesRead)
	}
	if result.Concepts != 2 {
		t.Fatalf("Concepts = %d, want 2", result.Concepts)
	}
	if result.PagesWritten != 2 {
		t.Fatalf("PagesWritten = %d, want 2 (errors: %v)", result.PagesWritten, result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if runner.extractCalls != 2 || runner.pageCalls != 2 {
		t.Fatalf("calls: extract=%d page=%d, want 2/2", runner.extractCalls, runner.pageCalls)
	}

	// Concept page on disk with frontmatter + the fake body.
	conceptPath := filepath.Join(repo.Root(), "team", "concepts", "reciprocal-rank-fusion.md")
	assertCompiledArticle(t, conceptPath, "Reciprocal Rank Fusion", "concept", "RRF combines rankings.")

	// Entity page on disk.
	entityPath := filepath.Join(repo.Root(), "team", "entities", "brex.md")
	assertCompiledArticle(t, entityPath, "Brex", "entity", "Brex is an anchor.")
}

func TestCompilerCompile_ExtractFailureIsNonFatal(t *testing.T) {
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()

	seedSource(t, worker, SourceKindNote, "", "Good Source", "Has content.")
	seedSource(t, worker, SourceKindNote, "", "Bad Source", "Will fail extraction.")

	runner := &fakeCompileRunner{
		extractByTitle: map[string]string{
			"Good Source": `{"concepts":[{"title":"Good","slug":"good","kind":"concept","confidence":0.7}]}`,
			"Bad Source":  `ignored`,
		},
		extractErrTitle: "Bad Source",
		pageBody:        "Good page. ^[good]",
	}
	compiler := NewCompiler(repo, worker, runner)

	result, err := compiler.Compile(context.Background())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	worker.WaitForIdle()

	if result.SourcesRead != 2 {
		t.Fatalf("SourcesRead = %d, want 2", result.SourcesRead)
	}
	if result.Concepts != 1 || result.PagesWritten != 1 {
		t.Fatalf("Concepts=%d PagesWritten=%d, want 1/1", result.Concepts, result.PagesWritten)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 recorded error, got %v", result.Errors)
	}
	if !strings.Contains(result.Errors[0], "Bad Source") {
		t.Fatalf("error should name the bad source: %q", result.Errors[0])
	}
}

func TestCompilerCompile_EmptyRepo(t *testing.T) {
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	result, err := NewCompiler(repo, worker, &fakeCompileRunner{}).Compile(context.Background())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if result.SourcesRead != 0 || result.Concepts != 0 || result.PagesWritten != 0 {
		t.Fatalf("expected zero tally on empty repo, got %+v", result)
	}
}

// --- helpers -----------------------------------------------------------------

func newStartedCompileWorker(t *testing.T) (*WikiWorker, *Repo, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return worker, repo, func() {
		cancel()
		<-worker.Done()
	}
}

func seedSource(t *testing.T, worker *WikiWorker, kind SourceKind, origin, title, content string) {
	t.Helper()
	id := DeriveSourceID(kind, origin, title, content)
	rec, err := NewSourceRecord(id, kind, title, origin, content, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewSourceRecord: %v", err)
	}
	if _, _, err := worker.EnqueueSource(context.Background(), rec); err != nil {
		t.Fatalf("EnqueueSource: %v", err)
	}
}

func mustSource(t *testing.T, kind SourceKind, id, title, content string, capturedAt time.Time) SourceRecord {
	t.Helper()
	rec, err := NewSourceRecord(id, kind, title, "", content, capturedAt)
	if err != nil {
		t.Fatalf("NewSourceRecord: %v", err)
	}
	return rec
}

func assertCompiledArticle(t *testing.T, fullPath, wantTitle, wantKind, wantBodyFragment string) {
	t.Helper()
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read %s: %v", fullPath, err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		t.Fatalf("article missing frontmatter:\n%s", content)
	}
	for _, want := range []string{
		"title: " + wantTitle,
		"kind: " + wantKind,
		"compiled: true",
		"updated_at:",
		"sources:",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("frontmatter missing %q in:\n%s", want, content)
		}
	}
	if !strings.Contains(content, wantBodyFragment) {
		t.Fatalf("body missing %q in:\n%s", wantBodyFragment, content)
	}
	// The LLM body omits the H1 (prompt contract), but the renderer prepends
	// a single "# {Title}" after the frontmatter so the wiki catalog shows the
	// real title rather than the kebab slug. The reader UI strips this leading
	// H1. Assert exactly one rendered H1 carrying the concept title.
	if want := "# " + wantTitle + "\n"; !strings.Contains(content, want) {
		t.Fatalf("compiled article missing rendered H1 title %q in:\n%s", want, content)
	}
	h1s := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			h1s++
		}
	}
	if h1s != 1 {
		t.Fatalf("compiled article should have exactly one H1 (the rendered title), found %d:\n%s", h1s, content)
	}
}
