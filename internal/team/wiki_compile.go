package team

// wiki_compile.go — the compile-engine orchestrator. It glues the two narrow
// LLM seams (extract, page) to the deterministic Go middle (merge) and the
// single-writer WikiWorker:
//
//	ListSources
//	   │  Phase 1: extract concepts from every source, concurrently
//	   ▼
//	mergeExtractions  (group by slug, deterministic)
//	   │  Phase 2: author one cited article per merged concept, concurrently
//	   ▼
//	worker.Enqueue → team/{kind}s/{slug}.md   (ArchivistAuthor, mode "replace")
//
// Concurrency is bounded by a semaphore so a large source set never fans out
// into an unbounded goroutine storm. A single source or page failure is
// recorded in CompileResult.Errors, never fatal. S3 does a full recompile
// every run; idempotency via content hashes, interlinking, and index/log.md
// land in S4.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
// tally. The only hard error is a failure to list sources; everything else is
// best-effort and surfaced via CompileResult.Errors.
func (c *Compiler) Compile(ctx context.Context) (CompileResult, error) {
	var result CompileResult
	if c.repo == nil || c.worker == nil {
		return result, fmt.Errorf("compile: repo and worker are required")
	}

	sources, err := ListSources(c.repo)
	if err != nil {
		return result, fmt.Errorf("compile: list sources: %w", err)
	}
	result.SourcesRead = len(sources)
	if len(sources) == 0 {
		return result, nil
	}

	perSource, extractErrs := c.extractAll(ctx, sources)
	result.Errors = append(result.Errors, extractErrs...)

	merged := mergeExtractions(perSource, sources)
	result.Concepts = len(merged)
	if len(merged) == 0 {
		return result, nil
	}

	written, pageErrs := c.compileAll(ctx, merged)
	result.PagesWritten = written
	result.Errors = append(result.Errors, pageErrs...)
	return result, nil
}

// extractAll runs Phase 1 over every source with bounded concurrency. Returns
// a map keyed by source ID plus a slice of per-source error strings.
func (c *Compiler) extractAll(ctx context.Context, sources []SourceRecord) (map[string][]ExtractedConcept, []string) {
	var (
		mu        sync.Mutex
		perSource = make(map[string][]ExtractedConcept, len(sources))
		errs      []string
		wg        sync.WaitGroup
	)
	sem := make(chan struct{}, c.boundedConcurrency())

	for _, src := range sources {
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
				errs = append(errs, err.Error())
				return
			}
			if len(concepts) > 0 {
				perSource[src.ID] = concepts
			}
		}()
	}
	wg.Wait()
	sort.Strings(errs)
	return perSource, errs
}

// compileAll runs Phase 2 over every merged concept with bounded concurrency,
// writing each article through the single-writer worker. Returns the count of
// pages written plus per-page error strings.
func (c *Compiler) compileAll(ctx context.Context, merged []MergedConcept) (int, []string) {
	relatedTitles := mergedConceptTitles(merged)
	now := c.now()

	var (
		mu      sync.Mutex
		written int
		errs    []string
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, c.boundedConcurrency())

	for _, mc := range merged {
		mc := mc
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := c.compileOne(ctx, mc, relatedTitles, now); err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
				return
			}
			mu.Lock()
			written++
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(errs)
	return written, errs
}

// compileOne reads the existing page (if any), authors the article, and writes
// it under the ArchivistAuthor identity in "replace" mode.
func (c *Compiler) compileOne(ctx context.Context, mc MergedConcept, relatedTitles []string, now time.Time) error {
	relPath := compiledArticlePath(mc.Kind, mc.Slug)

	existing, err := c.readExisting(relPath)
	if err != nil {
		return fmt.Errorf("compile %s: read existing: %w", mc.Slug, err)
	}

	article, err := compilePage(ctx, c.runner, mc, existing, relatedTitles, now)
	if err != nil {
		return err
	}

	commitMsg := "compile: " + mc.Slug
	if _, _, err := c.worker.Enqueue(ctx, ArchivistAuthor, relPath, article, "replace", commitMsg); err != nil {
		return fmt.Errorf("compile %s: write %s: %w", mc.Slug, relPath, err)
	}
	return nil
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
