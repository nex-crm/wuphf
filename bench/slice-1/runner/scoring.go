package runner

import "math"

// scoring.go — rank-sensitive retrieval metrics for the Slice 1 observability
// layer. recall@20 / pass-rate are saturated on this corpus (every in-scope
// query returns its full expected set inside top-20), so they cannot show
// whether a fusion or rerank change actually reordered the union toward the
// top. These metrics can: they reward putting relevant facts at low ranks.
//
// All metrics use binary relevance (a fact is relevant iff its ID is in the
// query's expected set) and operate on the ORDERED `got` list as returned by
// WikiIndex.Search. No LLM, no external credential, fully deterministic.

// scoreRecallAtK computes recall@k = |expected ∩ got[:k]| / |expected|: the
// fraction of relevant facts that appear within the top-k retrieved results.
//
// Returns 0 for out-of-scope queries (empty expected set) — those carry no
// relevant docs to rank and are excluded from the macro-averages by the
// caller. Note recall@1 is bounded above by 1/|expected| on multi-fact
// queries, so on this corpus nDCG@10 and MRR are the sharper discriminators;
// recall@1/@3 still move when a rerank lifts a single relevant fact to the top.
func scoreRecallAtK(expected, got []string, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	if k > len(got) {
		k = len(got)
	}
	gset := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		gset[e] = struct{}{}
	}
	hit := 0
	for i := 0; i < k; i++ {
		if _, ok := gset[got[i]]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(expected))
}

// scoreNDCG computes the normalised discounted cumulative gain at rank k with
// binary relevance:
//
//	DCG@k  = Σ_{i=1..k} rel_i / log2(i+1)
//	IDCG@k = Σ_{i=1..min(k,|expected|)} 1 / log2(i+1)   (ideal ordering)
//	nDCG@k = DCG@k / IDCG@k
//
// rel_i is 1 when got[i-1] is in the expected set, else 0. Returns 0 for
// out-of-scope queries (no ideal gain exists to normalise against).
func scoreNDCG(expected, got []string, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	gset := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		gset[e] = struct{}{}
	}
	limit := k
	if limit > len(got) {
		limit = len(got)
	}
	var dcg float64
	for i := 0; i < limit; i++ {
		if _, ok := gset[got[i]]; ok {
			// rank position is i+1; discount uses log2(position+1).
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	// Ideal DCG: as many relevant docs as possible packed at the top, capped
	// by both k and the number of relevant docs available.
	ideal := len(expected)
	if ideal > k {
		ideal = k
	}
	var idcg float64
	for i := 0; i < ideal; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// scoreMRR computes the reciprocal rank of the FIRST relevant fact in the
// ordered got list (1/rank, 1-indexed). Returns 0 when no relevant fact is
// retrieved or the query is out of scope. MRR is the cleanest single-number
// signal for "did the retriever put a right answer near the top".
func scoreMRR(expected, got []string) float64 {
	if len(expected) == 0 {
		return 0
	}
	gset := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		gset[e] = struct{}{}
	}
	for i, g := range got {
		if _, ok := gset[g]; ok {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}
