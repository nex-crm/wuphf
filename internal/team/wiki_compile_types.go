package team

// wiki_compile_types.go defines the value types for the deterministic compile
// engine (S3) that turns the immutable source layer (wiki_source.go) into
// Wikipedia-shaped, cited wiki articles.
//
// The engine is the atomicstrata "llm-wiki-compiler" kernel adapted to WUPHF:
// the LLM is confined to two narrow calls (extract concepts from one source;
// write one article from N sources). Everything between — grouping, merge,
// write routing — is plain, deterministic Go living in the other
// wiki_compile_*.go files.
//
// Pipeline:
//
//	ListSources ──► Phase 1 extract (LLM, per source) ──► []ExtractedConcept
//	                                                          │
//	                                       mergeExtractions (deterministic Go)
//	                                                          ▼
//	                                                  []MergedConcept
//	                                                          │
//	                            Phase 2 compile (LLM, per concept) ──► article md
//	                                                          ▼
//	                          worker.Enqueue → team/{kind}s/{slug}.md

// ExtractedConcept is one durable, encyclopedic concept the Phase-1 extractor
// pulled out of a single source document. Kind ∈ {"concept","entity"}.
type ExtractedConcept struct {
	Title      string   `json:"title"`
	Slug       string   `json:"slug"`
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// MergedConcept is one concept after grouping every ExtractedConcept that
// shares a slug across all sources. It carries the full set of source records
// the Phase-2 author cites from. Kind ∈ {"concept","entity"}.
type MergedConcept struct {
	Slug       string
	Title      string
	Kind       string
	Summary    string
	Tags       []string
	Sources    []SourceRecord
	Confidence float64
}

// CompileResult is the tally returned by Compiler.Compile. Errors collects
// non-fatal per-source / per-page failures so a single bad source or page
// never aborts the whole run.
//
// S4 finalize fields:
//   - PagesSkipped counts pages whose Phase-2 input was unchanged AND whose
//     file still existed, so no author call was made (the idempotency win).
//   - PagesLinked counts pages the deterministic interlink pass rewrote.
//   - CitationWarnings surfaces non-fatal citation problems (unknown ids,
//     uncited pages) found after authoring; they never fail the run.
type CompileResult struct {
	PagesWritten     int      `json:"pages_written"`
	PagesSkipped     int      `json:"pages_skipped"`
	PagesLinked      int      `json:"pages_linked"`
	Concepts         int      `json:"concepts"`
	SourcesRead      int      `json:"sources_read"`
	Errors           []string `json:"errors,omitempty"`
	CitationWarnings []string `json:"citation_warnings,omitempty"`
}

// conceptKind normalizes a free-form kind string to one of the two valid
// values. Anything that is not exactly "entity" (case-insensitive) defaults
// to "concept", matching the extractor contract.
func conceptKind(raw string) string {
	if normalizeKind(raw) == "entity" {
		return "entity"
	}
	return "concept"
}
