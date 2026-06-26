package team

// wiki_compile_finalize_test.go covers the S4 deterministic-finalize layer:
// idempotent recompiles (the zero-LLM no-op proof), state caching, citation
// validation, interlink wrapping rules, index regeneration, and the append-only
// log. The pure builders are tested directly; the orchestration is tested
// end-to-end against the fake PamRunner from wiki_compile_test.go.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- IDEMPOTENCY: the headline zero-LLM no-op recompile ----------------------

func TestCompilerCompile_NoOpRecompileMakesZeroLLMCalls(t *testing.T) {
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	ctx := context.Background()

	seedSource(t, worker, SourceKindNote, "", "RRF Method", "Reciprocal rank fusion beats pure semantic search.")
	seedSource(t, worker, SourceKindNote, "", "Brex Company", "Brex is the pilot anchor account.")

	runner := &fakeCompileRunner{
		extractByTitle: map[string]string{
			"RRF Method":   `{"concepts":[{"title":"Reciprocal Rank Fusion","slug":"reciprocal-rank-fusion","kind":"concept","summary":"hybrid search","tags":["search"],"confidence":0.9}]}`,
			"Brex Company": `{"concepts":[{"title":"Brex","slug":"brex","kind":"entity","summary":"anchor account","tags":["customer"],"confidence":0.8}]}`,
		},
		pageByTitle: map[string]string{
			"Reciprocal Rank Fusion": "RRF combines rankings. ^[note-rrf-method]\n\n## Details\nMore. ^[note-rrf-method]",
			"Brex":                   "Brex is an anchor. ^[note-brex-company]",
		},
		pageBody: "Default body. ^[unknown]",
	}

	compiler := NewCompiler(repo, worker, runner)
	compiler.now = func() time.Time { return time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC) }

	// First compile: full work.
	first, err := compiler.Compile(ctx)
	if err != nil {
		t.Fatalf("first Compile: %v", err)
	}
	worker.WaitForIdle()
	if first.PagesWritten != 2 || first.PagesSkipped != 0 {
		t.Fatalf("first run: written=%d skipped=%d, want 2/0 (errors: %v)", first.PagesWritten, first.PagesSkipped, first.Errors)
	}
	if runner.extractCalls != 2 || runner.pageCalls != 2 {
		t.Fatalf("first run calls: extract=%d page=%d, want 2/2", runner.extractCalls, runner.pageCalls)
	}

	// Reset the runner's counters; the SECOND compile must touch the LLM zero
	// times because nothing changed.
	runner.mu.Lock()
	runner.extractCalls = 0
	runner.pageCalls = 0
	runner.mu.Unlock()

	second, err := compiler.Compile(ctx)
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}
	worker.WaitForIdle()

	if runner.extractCalls != 0 {
		t.Fatalf("no-op recompile made %d extract calls, want 0", runner.extractCalls)
	}
	if runner.pageCalls != 0 {
		t.Fatalf("no-op recompile made %d page calls, want 0", runner.pageCalls)
	}
	if second.PagesWritten != 0 {
		t.Fatalf("no-op recompile wrote %d pages, want 0", second.PagesWritten)
	}
	if second.PagesSkipped != 2 {
		t.Fatalf("no-op recompile skipped %d pages, want 2", second.PagesSkipped)
	}
	if second.Concepts != 2 {
		t.Fatalf("no-op recompile concepts=%d, want 2", second.Concepts)
	}
}

func TestCompilerCompile_AddedSourceExtractsOnlyNewSource(t *testing.T) {
	// Sources are write-once immutable, so the realistic incremental case is a
	// NEW source appearing between compiles. Only that source is extracted; the
	// existing source reuses its cached extraction and its page is skipped.
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	ctx := context.Background()

	seedSource(t, worker, SourceKindNote, "", "RRF Method", "Reciprocal rank fusion beats pure semantic search.")

	runner := &fakeCompileRunner{
		extractByTitle: map[string]string{
			"RRF Method":   `{"concepts":[{"title":"Reciprocal Rank Fusion","slug":"reciprocal-rank-fusion","kind":"concept","summary":"hybrid search","confidence":0.9}]}`,
			"Brex Company": `{"concepts":[{"title":"Brex","slug":"brex","kind":"entity","summary":"anchor account","confidence":0.8}]}`,
		},
		pageBody: "Body. ^[x]",
	}
	compiler := NewCompiler(repo, worker, runner)
	compiler.now = func() time.Time { return time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC) }

	if _, err := compiler.Compile(ctx); err != nil {
		t.Fatalf("first Compile: %v", err)
	}
	worker.WaitForIdle()

	// Add a brand-new source. The existing RRF source is unchanged.
	seedSource(t, worker, SourceKindNote, "", "Brex Company", "Brex is the pilot anchor account.")
	worker.WaitForIdle()

	runner.mu.Lock()
	runner.extractCalls = 0
	runner.pageCalls = 0
	runner.mu.Unlock()

	second, err := compiler.Compile(ctx)
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}
	worker.WaitForIdle()

	if runner.extractCalls != 1 {
		t.Fatalf("incremental recompile made %d extract calls, want 1 (only the new source)", runner.extractCalls)
	}
	if runner.pageCalls != 1 {
		t.Fatalf("incremental recompile made %d page calls, want 1 (only the new page)", runner.pageCalls)
	}
	if second.PagesWritten != 1 || second.PagesSkipped != 1 {
		t.Fatalf("incremental recompile written=%d skipped=%d, want 1/1", second.PagesWritten, second.PagesSkipped)
	}
}

// --- STATE -------------------------------------------------------------------

func TestCompileState_LoadMissingIsEmptyNotError(t *testing.T) {
	_, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	st, err := LoadCompileState(repo)
	if err != nil {
		t.Fatalf("LoadCompileState on missing file: %v", err)
	}
	if len(st.Sources) != 0 || len(st.Pages) != 0 {
		t.Fatalf("expected empty state, got %+v", st)
	}
}

func TestCompileState_SaveThenLoadRoundTrips(t *testing.T) {
	_, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	st := newCompileState()
	st.Sources["note-a"] = CachedExtraction{
		ContentHash: "hash-a",
		Concepts:    []ExtractedConcept{{Title: "A", Slug: "a", Kind: "concept", Confidence: 0.5}},
	}
	st.Pages["a"] = CompiledPageState{InputHash: "ih-a", Kind: "concept"}

	if err := SaveCompileState(repo, st); err != nil {
		t.Fatalf("SaveCompileState: %v", err)
	}
	got, err := LoadCompileState(repo)
	if err != nil {
		t.Fatalf("LoadCompileState: %v", err)
	}
	if got.Sources["note-a"].ContentHash != "hash-a" || len(got.Sources["note-a"].Concepts) != 1 {
		t.Fatalf("source cache did not round-trip: %+v", got.Sources)
	}
	if got.Pages["a"].InputHash != "ih-a" {
		t.Fatalf("page state did not round-trip: %+v", got.Pages)
	}
}

func TestCompileState_CorruptFileTreatedAsEmpty(t *testing.T) {
	_, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	dir := filepath.Join(repo.Root(), compileStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, compileStateFile), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	st, err := LoadCompileState(repo)
	if err != nil {
		t.Fatalf("LoadCompileState on corrupt file should not error: %v", err)
	}
	if len(st.Sources) != 0 || len(st.Pages) != 0 {
		t.Fatalf("corrupt file should yield empty state, got %+v", st)
	}
}

func TestPageInputHash_ChangesWithInputs(t *testing.T) {
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	srcA := mustSource(t, SourceKindNote, "src-a", "Doc A", "body a", base)
	mc := MergedConcept{Slug: "x", Title: "X", Kind: "concept", Summary: "s", Sources: []SourceRecord{srcA}}

	h1 := pageInputHash(mc)
	if h1 == "" {
		t.Fatal("empty hash")
	}
	// Same inputs => same hash.
	if pageInputHash(mc) != h1 {
		t.Fatal("hash not stable across calls")
	}
	// Title change => different hash.
	mc2 := mc
	mc2.Title = "Y"
	if pageInputHash(mc2) == h1 {
		t.Fatal("title change did not change hash")
	}
	// Source content change => different hash.
	srcB := mustSource(t, SourceKindNote, "src-a", "Doc A", "different body", base)
	mc3 := mc
	mc3.Sources = []SourceRecord{srcB}
	if pageInputHash(mc3) == h1 {
		t.Fatal("source content change did not change hash")
	}
}

// --- CITATION VALIDATION -----------------------------------------------------

func TestValidateCitations(t *testing.T) {
	valid := []string{"task-42", "decision-7"}

	t.Run("known ids pass", func(t *testing.T) {
		body := "Fact one. ^[task-42]\n\nFact two. ^[decision-7]"
		if w := validateCitations("pilot", body, valid); len(w) != 0 {
			t.Fatalf("expected no warnings, got %v", w)
		}
	})

	t.Run("hallucinated id flagged", func(t *testing.T) {
		body := "Fact. ^[task-42]\n\nMade up. ^[task-999]"
		w := validateCitations("pilot", body, valid)
		if len(w) != 1 || !strings.Contains(w[0], "task-999") {
			t.Fatalf("expected one unknown-id warning naming task-999, got %v", w)
		}
	})

	t.Run("uncited page flagged", func(t *testing.T) {
		body := "A page with no citation markers at all."
		w := validateCitations("pilot", body, valid)
		if len(w) != 1 || !strings.Contains(w[0], "no citations") {
			t.Fatalf("expected one no-citations warning, got %v", w)
		}
	})

	t.Run("multiple unknown ids deduped and sorted", func(t *testing.T) {
		body := "^[zeta] ^[alpha] ^[zeta] ^[task-42]"
		w := validateCitations("p", body, valid)
		if len(w) != 2 {
			t.Fatalf("expected 2 deduped warnings, got %v", w)
		}
		if !strings.Contains(w[0], "alpha") || !strings.Contains(w[1], "zeta") {
			t.Fatalf("warnings not sorted alpha<zeta: %v", w)
		}
	})
}

// --- LOG ---------------------------------------------------------------------

func TestBuildLogLine_Deterministic(t *testing.T) {
	now := time.Date(2026, 6, 26, 9, 30, 0, 0, time.UTC)
	got := buildLogLine(now, 5, 3, 2, 1)
	want := "## [2026-06-26T09:30:00Z] compile | 5 sources -> 3 pages (2 updated, 1 skipped)"
	if got != want {
		t.Fatalf("buildLogLine =\n%q\nwant\n%q", got, want)
	}
}

func TestAppendLogEntry_PreservesPriorEntries(t *testing.T) {
	first := appendLogEntry("", "## [t1] compile | 1 sources -> 1 pages (1 updated, 0 skipped)")
	if first != "## [t1] compile | 1 sources -> 1 pages (1 updated, 0 skipped)\n" {
		t.Fatalf("first entry = %q", first)
	}
	second := appendLogEntry(first, "## [t2] compile | 2 sources -> 2 pages (1 updated, 1 skipped)")
	if !strings.Contains(second, "[t1]") || !strings.Contains(second, "[t2]") {
		t.Fatalf("second journal lost an entry: %q", second)
	}
	// t1 must come before t2 (append-only, never rewritten).
	if strings.Index(second, "[t1]") > strings.Index(second, "[t2]") {
		t.Fatalf("entries out of order: %q", second)
	}
}

// --- INDEX -------------------------------------------------------------------

func TestBuildIndexMarkdown_GroupsAndSorts(t *testing.T) {
	pages := []compiledPageRef{
		{Slug: "zebra", Kind: "concept", Title: "Zebra", Summary: "z animal"},
		{Slug: "apple", Kind: "concept", Title: "Apple", Summary: "a fruit"},
		{Slug: "brex", Kind: "entity", Title: "Brex", Summary: "anchor"},
	}
	md := buildIndexMarkdown(pages)

	if !strings.Contains(md, compiledIndexMarker) {
		t.Fatalf("index missing generated marker:\n%s", md)
	}
	// Concepts section, sorted by slug (apple before zebra).
	if strings.Index(md, "[[apple|Apple]]") > strings.Index(md, "[[zebra|Zebra]]") {
		t.Fatalf("concepts not sorted by slug:\n%s", md)
	}
	if !strings.Contains(md, "- [[apple|Apple]] — a fruit") {
		t.Fatalf("index missing apple entry with summary:\n%s", md)
	}
	// Entity in its own group.
	conceptsAt := strings.Index(md, "## Concepts")
	entitiesAt := strings.Index(md, "## Entities")
	brexAt := strings.Index(md, "[[brex|Brex]]")
	if !(conceptsAt < entitiesAt && entitiesAt < brexAt) {
		t.Fatalf("Brex should appear under Entities:\n%s", md)
	}
}

func TestBuildIndexMarkdown_EmptyGroupsRenderPlaceholder(t *testing.T) {
	md := buildIndexMarkdown(nil)
	if strings.Count(md, "_None yet._") != 2 {
		t.Fatalf("expected both groups to render placeholders:\n%s", md)
	}
}

// --- END-TO-END: index.md + log.md land on disk ------------------------------

func TestCompilerCompile_WritesIndexAndLog(t *testing.T) {
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	ctx := context.Background()

	seedSource(t, worker, SourceKindNote, "", "RRF Method", "Reciprocal rank fusion beats pure semantic search.")

	runner := &fakeCompileRunner{
		extractByTitle: map[string]string{
			"RRF Method": `{"concepts":[{"title":"Reciprocal Rank Fusion","slug":"reciprocal-rank-fusion","kind":"concept","summary":"hybrid search","confidence":0.9}]}`,
		},
		pageBody: "RRF combines rankings. ^[note-rrf-method]",
	}
	compiler := NewCompiler(repo, worker, runner)
	compiler.now = func() time.Time { return time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC) }

	if _, err := compiler.Compile(ctx); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	worker.WaitForIdle()

	indexBody, err := os.ReadFile(filepath.Join(repo.Root(), "team", "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if !strings.Contains(string(indexBody), "[[reciprocal-rank-fusion|Reciprocal Rank Fusion]]") {
		t.Fatalf("index.md missing the compiled page link:\n%s", indexBody)
	}

	logBody, err := os.ReadFile(filepath.Join(repo.Root(), "team", "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	if !strings.Contains(string(logBody), "[2026-06-26T00:00:00Z] compile | 1 sources -> 1 pages (1 updated, 0 skipped)") {
		t.Fatalf("log.md missing the run line:\n%s", logBody)
	}

	// A second compile appends a NEW log line and preserves the first.
	if _, err := compiler.Compile(ctx); err != nil {
		t.Fatalf("second Compile: %v", err)
	}
	worker.WaitForIdle()
	logBody2, err := os.ReadFile(filepath.Join(repo.Root(), "team", "log.md"))
	if err != nil {
		t.Fatalf("read log.md again: %v", err)
	}
	if n := strings.Count(string(logBody2), "] compile |"); n != 2 {
		t.Fatalf("expected 2 journal lines after two compiles, got %d:\n%s", n, logBody2)
	}
	if !strings.Contains(string(logBody2), "(0 updated, 1 skipped)") {
		t.Fatalf("second run line should record a skip:\n%s", logBody2)
	}
}
