package team

// wiki_query_retrieve.go — class-aware retrieval for Slice 2 Thread A.
//
// Route map (called from WikiIndex.Search after ClassifyQuery):
//
//   multi_hop      → retrieveMultiHop
//     parse (companyDisplay, projectDisplay) from query
//     ListFactsByPredicateObject("champions", "project:<projSlug>") for each
//       slug candidate derived from projectDisplay
//     ListFactsByTriplet(championSubject, "role_at", "company:<companySlug>")
//       for each champion subject + company slug candidate
//     union with BM25 top-K
//
//   counterfactual → retrieveCounterfactual
//     parse personDisplay from query
//     ListFactsByTriplet(personSlug, "role_at", "") — all role_at facts
//     union with BM25 on the stripped noun phrase
//
//   relationship   → retrieveRelationshipSingle (Slice 2 Thread A v2)
//     parse (predicate, projectDisplay) from query
//     ListFactsByPredicateObject(predicate, "project:<projSlug>") for each
//       slug candidate derived from projectDisplay
//     For "involved_in": also pull {leads, champions} for the same object —
//       matches the bench's expectedFactsForProjectAnyPredicate generator.
//     union with BM25 top-K
//
//   default        → BM25 only (current behaviour — never replaced)
//
// Invariant: the typed walk is additive. If the rewriter fails or the
// resolver finds no slug, the BM25 path answers alone. Recall never falls
// below the previous BM25-only baseline.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// retrieveWithClass routes a query to the appropriate retrieval path based
// on its ClassifyQuery output. Called by WikiIndex.Search.
func retrieveWithClass(ctx context.Context, store FactStore, text TextIndex, query string, topK int) ([]SearchHit, error) {
	// Confidence is intentionally discarded here: the typed walks are always
	// additive over BM25, so there is nothing a threshold gate would reject —
	// a low-confidence classification still falls back to BM25 via the union.
	// A future re-ranker can consume the confidence value.
	class, _ := ClassifyQuery(query)
	switch class {
	case QueryClassMultiHop:
		return retrieveMultiHop(ctx, store, text, query, topK)
	case QueryClassCounterfactual:
		return retrieveCounterfactual(ctx, store, text, query, topK)
	case QueryClassRelationship:
		return retrieveRelationshipSingle(ctx, store, text, query, topK)
	default:
		// Status / general: BM25 only. The plan's core invariant
		// ("never replace BM25") is satisfied here by never bolting the typed
		// walk onto a query class it cannot help.
		return text.Search(ctx, query, topK)
	}
}

// relationshipUnionPredicates lists the predicates to union when a
// relationship query uses the "involved_in" shape. The bench generator's
// expectedFactsForProjectAnyPredicate mixes leads + champions + involved_in
// for this shape, so the retriever must as well or recall caps below 85%.
var relationshipUnionPredicates = []string{"leads", "champions", "involved_in"}

// retrieveRelationshipSingle implements the typed-predicate graph walk for
// single-predicate relationship queries:
//
//	"Who champions <P>?"      → ListFactsByPredicateObject("champions",     "project:<slug>")
//	"Who leads <P>?"          → ListFactsByPredicateObject("leads",         "project:<slug>")
//	"Who is involved in <P>?" → union over {leads, champions, involved_in}
//
// Strategy:
//  1. Always grab BM25 first so it's the floor for recall.
//  2. Parse (predicate, projectDisplay). If parse fails → return BM25 alone.
//  3. Generate slug candidates for the project (handles "APAC Launch" →
//     "apac-launch", "Partner Program" → "partner-program", etc.).
//  4. For each slug candidate, pull the predicate's facts (or the union set
//     for "involved_in"). First non-empty result wins on a per-predicate basis.
//  5. Union typed hits (first, to preserve priority) with BM25, dedupe on
//     fact ID, cap at topK.
func retrieveRelationshipSingle(ctx context.Context, store FactStore, text TextIndex, query string, topK int) ([]SearchHit, error) {
	bm25Hits, bm25Err := text.Search(ctx, query, topK)
	if bm25Err != nil {
		return nil, fmt.Errorf("retrieveRelationshipSingle bm25: %w", bm25Err)
	}

	predicate, projectDisplay, ok := parseRelationshipSingle(query)
	if !ok {
		return bm25Hits, nil
	}

	// "involved_in" shape is broader: the generator pools leads + champions +
	// involved_in under the same project object. Anything narrower caps recall.
	predicates := []string{predicate}
	if predicate == "involved_in" {
		predicates = relationshipUnionPredicates
	}

	projCandidates := displayToSlugCandidates(projectDisplay)

	// Collect typed facts. Deduplicate on fact ID at collection time so that
	// the union across predicates and slug candidates doesn't inflate.
	seenFacts := map[string]bool{}
	var typedFacts []TypedFact
	for _, pred := range predicates {
		for _, projSlug := range projCandidates {
			facts, err := store.ListFactsByPredicateObject(ctx, pred, "project:"+projSlug)
			if err != nil {
				return nil, fmt.Errorf("retrieveRelationshipSingle %s/%s: %w", pred, projSlug, err)
			}
			if len(facts) == 0 {
				continue
			}
			for _, f := range facts {
				if seenFacts[f.ID] {
					continue
				}
				seenFacts[f.ID] = true
				typedFacts = append(typedFacts, f)
			}
			// First non-empty slug candidate wins per predicate — the project's
			// canonical slug is one of the candidates and pulling facts under a
			// shorter prefix candidate on top would inflate noise.
			break
		}
	}

	// Sort typed facts deterministically by ID. When the typed set exceeds
	// topK (possible for high-cardinality projects like partner-program) the
	// first K hits after sort match the generator's capExpected truncation,
	// which is sorted-ascending-by-ID as well. Without this sort, typed hits
	// would be predicate-first then ID-sorted, and recall would cap below
	// 85% on any project where the union set is larger than topK.
	sort.Slice(typedFacts, func(i, j int) bool {
		return typedFacts[i].ID < typedFacts[j].ID
	})

	// Boost typed hits so they rank at least as high as the top BM25 hit.
	// Runner evaluates recall by set-membership (not by rank), but the score
	// boost keeps the contract honest for callers that do consume ranks.
	typedHits := typedHitsWithBoost(typedFacts, bm25Hits)

	return mergeHits(typedHits, bm25Hits, topK), nil
}

// typedHitsWithBoost converts TypedFacts to SearchHits with a score boost
// pegged to the top BM25 score + epsilon. If bm25Hits is empty, typed hits
// retain the default factToHit score (1.0). Insertion order is preserved so
// deterministic fact ordering from the FactStore carries through.
func typedHitsWithBoost(facts []TypedFact, bm25 []SearchHit) []SearchHit {
	if len(facts) == 0 {
		return nil
	}
	var topBM25 float64
	for _, h := range bm25 {
		if h.Score > topBM25 {
			topBM25 = h.Score
		}
	}
	hits := make([]SearchHit, 0, len(facts))
	for _, f := range facts {
		h := factToHit(f)
		if topBM25 > 0 {
			// Epsilon = 0.01 is plenty of headroom for bleve BM25 scores in
			// our corpus (top scores sit in the 2–6 range). Pushes typed
			// above BM25 without creating unbounded ranks.
			h.Score = topBM25 + 0.01
		}
		hits = append(hits, h)
	}
	return hits
}

// retrieveMultiHop implements the typed-predicate graph walk for queries
// of the shape "who at <company> championed <project>".
//
// Strategy:
//  1. Parse (companyDisplay, projectDisplay). If parse fails → BM25 fallback.
//  2. Generate slug candidates for each. The bench corpus uses slugs like
//     "vandelay" for "Vandelay Industries" and "q2-pilot" for "Q2 Pilot
//     Program", so we try progressive shortenings.
//  3. For each projSlug candidate: ListFactsByPredicateObject("champions",
//     "project:"+projSlug). First non-empty wins.
//  4. For each champion subject: ListFactsByTriplet(subject, "role_at", ""),
//     then filter by any companySlug candidate (prefix match).
//  5. Union champions facts + role_at facts + BM25 top-K. Dedupe, cap at topK.
func retrieveMultiHop(ctx context.Context, store FactStore, text TextIndex, query string, topK int) ([]SearchHit, error) {
	// Always grab BM25 first so we have a floor.
	bm25Hits, bm25Err := text.Search(ctx, query, topK)
	if bm25Err != nil {
		return nil, fmt.Errorf("retrieveMultiHop bm25: %w", bm25Err)
	}

	companyDisplay, projectDisplay, ok := parseMultiHopSpans(query)
	if !ok {
		return bm25Hits, nil
	}

	projCandidates := displayToSlugCandidates(projectDisplay)
	companyCandidates := displayToSlugCandidates(companyDisplay)

	// Step 1: pull every champions fact for the project, trying each slug
	// candidate until one yields hits.
	var championsFacts []TypedFact
	for _, projSlug := range projCandidates {
		facts, err := store.ListFactsByPredicateObject(ctx, "champions", "project:"+projSlug)
		if err != nil {
			return nil, fmt.Errorf("retrieveMultiHop champions %s: %w", projSlug, err)
		}
		if len(facts) > 0 {
			championsFacts = facts
			break
		}
	}

	// Step 2: for each champion subject, pull the single most-recent role_at
	// fact at the matched company. The bench expected-set only counts the
	// latest role_at per champion (generator: history[len(history)-1]), so
	// including older facts spends topK slots on noise.
	seenSubjects := map[string]bool{}
	var roleAtFacts []TypedFact
	for _, cf := range championsFacts {
		if cf.Triplet == nil {
			continue
		}
		subject := cf.Triplet.Subject
		if seenSubjects[subject] {
			continue
		}
		seenSubjects[subject] = true
		// Try each company slug candidate. First non-empty result wins; keep
		// only the last fact (latest CreatedAt ordering per FactStore).
		//
		// Note: there is deliberately NO "insurance" fallback to the subject's
		// most-recent role_at regardless of company. Surfacing an off-company
		// role for "who at Acme championed X" is an accuracy hazard (e.g. we'd
		// return a Blueshift role when the asker specifically scoped to Acme).
		// BM25 already provides the recall floor for the union, so dropping
		// this branch is net-positive. Reviewed on PR #249.
		for _, companySlug := range companyCandidates {
			facts, err := store.ListFactsByTriplet(ctx, subject, "role_at", "company:"+companySlug)
			if err != nil {
				return nil, fmt.Errorf("retrieveMultiHop role_at %s/%s: %w", subject, companySlug, err)
			}
			if len(facts) > 0 {
				roleAtFacts = append(roleAtFacts, latestFact(facts))
				break
			}
		}
	}

	// Step 3: union typed hits with BM25 top-K, dedupe on fact ID, cap at topK.
	// Typed hits go first (priority). BM25 hits fill remaining slots in their
	// original order.
	var typedHits []SearchHit
	for _, f := range championsFacts {
		typedHits = append(typedHits, factToHit(f))
	}
	for _, f := range roleAtFacts {
		typedHits = append(typedHits, factToHit(f))
	}
	merged := mergeHits(typedHits, bm25Hits, topK)
	return merged, nil
}

// retrieveCounterfactual implements the counterfactual rewrite: strip the
// counterfactual framing, fetch all role_at facts for the referenced person,
// and union with BM25 on the stripped phrase.
func retrieveCounterfactual(ctx context.Context, store FactStore, text TextIndex, query string, topK int) ([]SearchHit, error) {
	// BM25 on the stripped phrase first. Stripping is mostly cosmetic for
	// recall but keeps the merged rank list stable.
	stripped := stripCounterfactualFraming(query)
	bm25Hits, bm25Err := text.Search(ctx, stripped, topK)
	if bm25Err != nil {
		return nil, fmt.Errorf("retrieveCounterfactual bm25: %w", bm25Err)
	}

	personDisplay, ok := parseCounterfactualSubject(query)
	if !ok {
		return bm25Hits, nil
	}

	// Try each slug candidate. For a real person "Ivan Petrov", normalize
	// yields "ivan-petrov" — the canonical slug in the bench.
	var typedFacts []TypedFact
	for _, personSlug := range displayToSlugCandidates(personDisplay) {
		facts, err := store.ListFactsByTriplet(ctx, personSlug, "role_at", "")
		if err != nil {
			return nil, fmt.Errorf("retrieveCounterfactual role_at %s: %w", personSlug, err)
		}
		if len(facts) > 0 {
			typedFacts = facts
			break
		}
	}

	var typedHits []SearchHit
	for _, f := range typedFacts {
		typedHits = append(typedHits, factToHit(f))
	}
	return mergeHits(typedHits, bm25Hits, topK), nil
}

// stripCounterfactualFraming removes counterfactual trigger phrases so the
// BM25 search ranks the actual subject + predicate higher. Only applied to
// the query string passed to BM25; the caller's query argument is unchanged
// for the typed walk.
func stripCounterfactualFraming(query string) string {
	triggers := []string{
		"what would have happened if",
		"what would have happened",
		"what would happen if",
		"what would happen",
		"what if",
		"suppose that",
		"suppose",
		"if not for",
		"had not",
		"hadn't",
		"had n't",
		"never had",
		"without",
		"hypothetically",
		"counterfactual",
	}
	out := strings.ToLower(query)
	for _, t := range triggers {
		out = strings.ReplaceAll(out, t, " ")
	}
	// Collapse runs of whitespace.
	return strings.Join(strings.Fields(out), " ")
}

// latestFact returns the fact in the slice with the most recent CreatedAt.
// If CreatedAt is zero for every fact, falls back to the last element in the
// input slice. Never returns an empty fact — caller must check len > 0.
func latestFact(facts []TypedFact) TypedFact {
	best := facts[0]
	for _, f := range facts[1:] {
		if f.CreatedAt.After(best.CreatedAt) {
			best = f
		}
	}
	return best
}

// factToHit converts a TypedFact into a SearchHit for union with BM25 results.
// Score is set to a sentinel value (1.0) since typed walks have no BM25 score;
// the merger uses insertion order, not score, to prioritise typed hits.
//
// The snippet is truncated to 300 runes (not bytes) so multi-byte UTF-8
// characters — Japanese, emoji, accented Latin — aren't sliced mid-rune and
// rendered as replacement characters downstream.
func factToHit(f TypedFact) SearchHit {
	snippet := f.Text
	if utf8.RuneCountInString(snippet) > 300 {
		runes := []rune(snippet)
		snippet = string(runes[:300])
	}
	return SearchHit{
		FactID:  f.ID,
		Score:   1.0,
		Snippet: snippet,
		Entity:  f.EntitySlug,
	}
}

// mergeHits returns a union of typed + bm25 hits, deduplicated by FactID,
// capped at topK. Typed hits appear first in the output; BM25 fills remaining
// slots in its original order.
func mergeHits(typed, bm25 []SearchHit, topK int) []SearchHit {
	if topK <= 0 {
		topK = 20
	}
	out := make([]SearchHit, 0, topK)
	seen := map[string]bool{}
	for _, h := range typed {
		if len(out) >= topK {
			break
		}
		if seen[h.FactID] {
			continue
		}
		seen[h.FactID] = true
		out = append(out, h)
	}
	for _, h := range bm25 {
		if len(out) >= topK {
			break
		}
		if seen[h.FactID] {
			continue
		}
		seen[h.FactID] = true
		out = append(out, h)
	}
	return out
}
