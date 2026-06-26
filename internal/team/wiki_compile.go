package team

// wiki_compile.go — the compile-engine orchestrator. It glues the two narrow
// LLM seams (extract, page) to the deterministic Go middle (merge, interlink,
// index, log) and the single-writer WikiWorker:
//
//	LoadCompileState (.compile/state.json)
//	   │  Phase 1: extract concepts — ONLY for new/changed sources; unchanged
//	   ▼  sources reuse their cached extraction (no LLM call)
//	mergeExtractions  (group by slug, deterministic, over ALL sources)
//	   │  Phase 2: author one cited article per merged concept whose input
//	   ▼  hash changed; unchanged pages with a file on disk are skipped
//	worker.Enqueue → team/{kind}s/{slug}.md   (ArchivistAuthor, mode "replace")
//	   │  deterministic finalize (no LLM):
//	   ▼  interlink → team/index.md → append team/log.md → SaveCompileState
//
// The key S4 property: a no-op recompile (no source changes) makes ZERO
// PamRunner calls — every extraction is reused from cache and every page input
// hash matches, so nothing is re-authored.
//
// Concurrency is bounded by a semaphore so a large source set never fans out
// into an unbounded goroutine storm. A single source or page failure is
// recorded in CompileResult.Errors, never fatal.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// defaultCompileConcurrency bounds in-flight LLM calls per phase.
const defaultCompileConcurrency = 4

// Compiler turns the immutable source layer into compiled wiki articles.
type Compiler struct {
	repo        *Repo
	worker      *WikiWorker
	runner      PamRunner
	concurrency int
	// now is the compile clock, injected so tests are deterministic. Defaults
	// to time.Now.
	now func() time.Time
}

// NewCompiler builds a Compiler over the given repo + worker. A nil runner
// defaults to HeadlessPamRunner (the production one-shot seam); tests inject a
// fake. concurrency <= 0 falls back to defaultCompileConcurrency.
func NewCompiler(repo *Repo, worker *WikiWorker, runner PamRunner) *Compiler {
	if runner == nil {
		runner = HeadlessPamRunner{}
	}
	return &Compiler{
		repo:        repo,
		worker:      worker,
		runner:      runner,
		concurrency: defaultCompileConcurrency,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// Compile runs the full pipeline over every captured source and returns a
// tally. The only hard errors are a failure to load the cache or list sources;
// everything else is best-effort and surfaced via CompileResult.Errors.
func (c *Compiler) Compile(ctx context.Context) (CompileResult, error) {
	var result CompileResult
	if c.repo == nil || c.worker == nil {
		return result, fmt.Errorf("compile: repo and worker are required")
	}

	state, err := LoadCompileState(c.repo)
	if err != nil {
		return result, fmt.Errorf("compile: load state: %w", err)
	}

	sources, err := ListSources(c.repo)
	if err != nil {
		return result, fmt.Errorf("compile: list sources: %w", err)
	}
	result.SourcesRead = len(sources)
	if len(sources) == 0 {
		return result, nil
	}

	// Phase 1: extract only changed sources; reuse the cached extraction for
	// the rest. freshCache holds exactly the current sources, so deleted
	// sources drop out of the persisted cache.
	perSource, freshCache, extractErrs := c.extractChanged(ctx, sources, state)
	result.Errors = append(result.Errors, extractErrs...)
	state.Sources = freshCache

	merged := mergeExtractions(perSource, sources)
	result.Concepts = len(merged)

	// Phase 2: author only pages whose input changed; skip the rest.
	pr := c.compileChanged(ctx, merged, state)
	result.PagesWritten = pr.written
	result.PagesSkipped = pr.skipped
	result.Errors = append(result.Errors, pr.errs...)
	result.CitationWarnings = pr.citationWarnings
	state.Pages = pr.pages

	// Deterministic finalize passes — each best-effort, never fatal.
	linked, linkErrs := interlinkPages(ctx, c.worker, pr.live)
	result.PagesLinked = linked
	result.Errors = append(result.Errors, linkErrs...)

	if err := writeCompiledIndex(ctx, c.worker, pr.live); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("index: %v", err))
	}

	line := buildLogLine(c.now(), result.SourcesRead, pr.written+pr.skipped, pr.written, pr.skipped)
	if err := appendCompileLog(ctx, c.worker, line); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("log: %v", err))
	}

	if err := SaveCompileState(c.repo, state); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("state: %v", err))
	}
	return result, nil
}

// extractChanged runs Phase 1 only for sources whose content hash differs from
// the cached extraction. Unchanged sources reuse their cached concepts with no
// LLM call. Returns the per-source concept map (cached + fresh), the rebuilt
// cache covering exactly the current sources, and per-source error strings.
func (c *Compiler) extractChanged(ctx context.Context, sources []SourceRecord, state CompileState) (map[string][]ExtractedConcept, map[string]CachedExtraction, []string) {
	perSource := make(map[string][]ExtractedConcept, len(sources))
	freshCache := make(map[string]CachedExtraction, len(sources))

	var toExtract []SourceRecord
	for _, src := range sources {
		cached, ok := state.Sources[src.ID]
		if ok && cached.ContentHash == src.ContentHash {
			freshCache[src.ID] = cached
			if len(cached.Concepts) > 0 {
				perSource[src.ID] = cached.Concepts
			}
			continue
		}
		toExtract = append(toExtract, src)
	}

	var (
		mu   sync.Mutex
		errs []string
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, c.boundedConcurrency())
	for _, src := range toExtract {
		src := src
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			concepts, err := extractConcepts(ctx, c.runner, src)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// Do NOT cache a failed source so it is retried next run.
				errs = append(errs, err.Error())
				return
			}
			freshCache[src.ID] = CachedExtraction{ContentHash: src.ContentHash, Concepts: concepts}
			if len(concepts) > 0 {
				perSource[src.ID] = concepts
			}
		}()
	}
	wg.Wait()
	sort.Strings(errs)
	return perSource, freshCache, errs
}

// pageResult bundles the outcome of the Phase-2 page loop so Compile can update
// CompileResult and the persisted state in one place.
type pageResult struct {
	live             []compiledPageRef
	pages            map[string]CompiledPageState
	written          int
	skipped          int
	errs             []string
	citationWarnings []string
}

// compileChanged runs Phase 2 over the merged concepts with bounded
// concurrency. A concept whose input hash matches the cached one AND whose page
// file still exists is skipped (no LLM call); every other concept is authored
// and written. Returns the live page set (for the finalize passes), the rebuilt
// page-state map, counts, and per-page errors + citation warnings.
func (c *Compiler) compileChanged(ctx context.Context, merged []MergedConcept, state CompileState) pageResult {
	relatedTitles := mergedConceptTitles(merged)
	now := c.now()

	var (
		mu  sync.Mutex
		res = pageResult{pages: make(map[string]CompiledPageState, len(merged))}
		wg  sync.WaitGroup
	)
	sem := make(chan struct{}, c.boundedConcurrency())

	for _, mc := range merged {
		mc := mc
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			relPath := compiledArticlePath(mc.Kind, mc.Slug)
			inputHash := pageInputHash(mc)
			ref := compiledPageRef{
				Slug:    mc.Slug,
				Kind:    conceptKind(mc.Kind),
				Title:   mc.Title,
				Summary: mc.Summary,
				RelPath: relPath,
			}

			prev, hasPrev := state.Pages[mc.Slug]
			if hasPrev && prev.InputHash == inputHash && c.pageFileExists(relPath) {
				mu.Lock()
				res.skipped++
				res.live = append(res.live, ref)
				res.pages[mc.Slug] = prev
				mu.Unlock()
				return
			}

			warnings, err := c.authorPage(ctx, mc, relatedTitles, now)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.errs = append(res.errs, err.Error())
				// Keep an already-on-disk page live so finalize still includes
				// it; carry its prior input hash so it is retried next run.
				if c.pageFileExists(relPath) {
					res.live = append(res.live, ref)
					if hasPrev {
						res.pages[mc.Slug] = prev
					}
				}
				return
			}
			res.written++
			res.live = append(res.live, ref)
			res.pages[mc.Slug] = CompiledPageState{InputHash: inputHash, Kind: ref.Kind}
			res.citationWarnings = append(res.citationWarnings, warnings...)
		}()
	}
	wg.Wait()

	sort.Strings(res.errs)
	sort.Strings(res.citationWarnings)
	return res
}

// authorPage reads the existing page (if any), authors the article via the
// Phase-2 LLM seam, validates its citations, and writes it under the
// ArchivistAuthor identity in "replace" mode. Citation warnings are returned
// separately so they never count as errors.
func (c *Compiler) authorPage(ctx context.Context, mc MergedConcept, relatedTitles []string, now time.Time) ([]string, error) {
	relPath := compiledArticlePath(mc.Kind, mc.Slug)

	existing, err := c.readExisting(relPath)
	if err != nil {
		return nil, fmt.Errorf("compile %s: read existing: %w", mc.Slug, err)
	}

	article, err := compilePage(ctx, c.runner, mc, existing, relatedTitles, now)
	if err != nil {
		return nil, err
	}

	warnings := validateCitations(mc.Slug, article, sourceIDs(mc.Sources))

	commitMsg := "compile: " + mc.Slug
	if _, _, err := c.worker.Enqueue(ctx, ArchivistAuthor, relPath, article, "replace", commitMsg); err != nil {
		return nil, fmt.Errorf("compile %s: write %s: %w", mc.Slug, relPath, err)
	}
	return warnings, nil
}

// pageFileExists reports whether the compiled article already exists on disk.
func (c *Compiler) pageFileExists(relPath string) bool {
	_, err := os.Stat(filepath.Join(c.repo.Root(), filepath.FromSlash(relPath)))
	return err == nil
}

// readExisting returns the current article body, or "" when the page does not
// exist yet (the common first-compile case). A genuine read error propagates.
func (c *Compiler) readExisting(relPath string) (string, error) {
	body, err := c.worker.ReadArticle(relPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(body), nil
}

// boundedConcurrency returns a sane positive concurrency limit.
func (c *Compiler) boundedConcurrency() int {
	if c.concurrency <= 0 {
		return defaultCompileConcurrency
	}
	return c.concurrency
}

// compiledArticlePath maps a concept kind + slug to its on-disk article path:
// team/concepts/{slug}.md or team/entities/{slug}.md.
func compiledArticlePath(kind, slug string) string {
	return "team/" + compiledKindDir(kind) + "/" + slug + ".md"
}

// compiledKindDir returns the article-store subdirectory for a concept kind:
// "entities" for entity, "concepts" for everything else.
func compiledKindDir(kind string) string {
	if conceptKind(kind) == "entity" {
		return "entities"
	}
	return "concepts"
}

// mergedConceptTitles returns the titles of every merged concept, for
// [[wikilink]] context. Order follows the (slug-sorted) merged slice.
func mergedConceptTitles(merged []MergedConcept) []string {
	out := make([]string, 0, len(merged))
	for _, mc := range merged {
		if t := mc.Title; t != "" {
			out = append(out, t)
		}
	}
	return out
}
