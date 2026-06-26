package team

// wiki_compile_merge.go — the deterministic middle of the compile engine.
// Phase 1 produced, per source, a slice of ExtractedConcept. Merge groups
// those by slug across ALL sources so a concept mentioned in N sources becomes
// ONE MergedConcept whose Sources are exactly those N records.
//
// This is plain Go — no LLM. The aggregation rules mirror the atomicstrata
// kernel:
//
//   - Title / Kind / Summary come from the highest-confidence extraction.
//   - Tags are the deduped union across every extraction of the slug.
//   - Confidence is the MIN across sources (pessimistic: a concept is only as
//     trustworthy as its weakest mention).
//   - Sources are the distinct source records, in source-list order.
//
// Output is sorted by slug so a recompile of unchanged inputs is byte-stable.

import "sort"

// conceptAccumulator gathers the running merge state for one slug.
type conceptAccumulator struct {
	slug      string
	title     string
	kind      string
	summary   string
	bestConf  float64
	haveBest  bool
	minConf   float64
	haveMin   bool
	tags      []string
	tagSeen   map[string]struct{}
	sources   []SourceRecord
	sourceIDs map[string]struct{}
}

// mergeExtractions groups the per-source extractions by slug. perSource maps a
// source ID to the concepts extracted from that source; sources is the full
// record list (used to resolve IDs to records and to fix iteration order).
// Sources missing from perSource (extraction failed/skipped) simply contribute
// nothing. The result is sorted by slug for deterministic output.
func mergeExtractions(perSource map[string][]ExtractedConcept, sources []SourceRecord) []MergedConcept {
	accums := make(map[string]*conceptAccumulator)
	var order []string

	// Iterate sources in their given order (not the map) so "highest
	// confidence" tie-breaks and source ordering are deterministic.
	for _, src := range sources {
		for _, ec := range perSource[src.ID] {
			a := accums[ec.Slug]
			if a == nil {
				a = &conceptAccumulator{
					slug:      ec.Slug,
					tagSeen:   make(map[string]struct{}),
					sourceIDs: make(map[string]struct{}),
				}
				accums[ec.Slug] = a
				order = append(order, ec.Slug)
			}
			a.observe(ec, src)
		}
	}

	out := make([]MergedConcept, 0, len(order))
	for _, slug := range order {
		out = append(out, accums[slug].finish())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// observe folds one extraction (from src) into the accumulator.
func (a *conceptAccumulator) observe(ec ExtractedConcept, src SourceRecord) {
	// Highest-confidence extraction wins Title/Kind/Summary. Strict ">" keeps
	// the first-seen (deterministic source order) value on ties.
	if !a.haveBest || ec.Confidence > a.bestConf {
		a.bestConf = ec.Confidence
		a.title = ec.Title
		a.kind = ec.Kind
		a.summary = ec.Summary
		a.haveBest = true
	}
	// Pessimistic confidence: min across all mentions.
	if !a.haveMin || ec.Confidence < a.minConf {
		a.minConf = ec.Confidence
		a.haveMin = true
	}
	// Dedup-union tags, first-seen order.
	for _, t := range ec.Tags {
		if _, ok := a.tagSeen[t]; ok {
			continue
		}
		a.tagSeen[t] = struct{}{}
		a.tags = append(a.tags, t)
	}
	// Distinct source records, in source-list order.
	if _, ok := a.sourceIDs[src.ID]; !ok {
		a.sourceIDs[src.ID] = struct{}{}
		a.sources = append(a.sources, src)
	}
}

// finish materializes the accumulator into a MergedConcept.
func (a *conceptAccumulator) finish() MergedConcept {
	return MergedConcept{
		Slug:       a.slug,
		Title:      a.title,
		Kind:       a.kind,
		Summary:    a.summary,
		Tags:       a.tags,
		Sources:    a.sources,
		Confidence: a.minConf,
	}
}
