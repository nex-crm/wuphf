package team

// playbook_clusters.go implements Slice 2 Thread C of the wiki intelligence
// port: surface "patterns across entities" in playbook synthesis by grouping
// reinforced facts that share the same (predicate, object) pair across a
// threshold-minimum number of distinct entities.
//
// Read-only consumer of the fact log (§7.4 rebuild contract). Never mutates
// facts — enforcement of the single-writer invariant lives in WikiWorker.
//
// The live signal for "reinforced" is TypedFact.ReinforcedAt != nil; when the
// same content-hashed fact is re-extracted, the indexing path in
// wiki_extractor.go advances ReinforcedAt on the in-memory row (no new JSONL
// line is appended). That is the only reinforcement counter v1.2 exposes, so
// the cluster predicate is simply "at least one reinforced fact per entity".
//
// Slice 3 Thread A change:
//   - cluster detection now uses ListReinforcedFactsByPredicate, which is
//     index-backed on the SQLite store (idx_facts_reinforced — partial index
//     keyed on triplet_predicate where reinforced_at IS NOT NULL). Cost
//     scales with matching rows, not corpus size, so the previous
//     count > threshold "consider paging" warning is no longer needed and
//     has been retired along with its test hook.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FactCluster is one reinforced (predicate, object) pair observed across
// multiple distinct entities. Emitted by clusterReinforcedFacts as input to
// the v2 playbook synthesis prompt (§Thread C, WIKI-SLICE2-PLAN.md).
//
// Entities are the distinct entity slugs whose fact logs contain a reinforced
// fact matching (Predicate, Object). Count is len(Entities), surfaced as a
// separate field so the prompt template can print it without a template
// function.
//
// Count reflects distinct entities where the fact was confirmed via
// re-extraction (ReinforcedAt != nil), not all entities where the fact was
// observed. Facts seen only once never enter a cluster.
type FactCluster struct {
	Predicate string   `json:"predicate"`
	Object    string   `json:"object"`
	Entities  []string `json:"entities"`
	Count     int      `json:"count"`
}

// clusterReinforcedFacts scans the fact store and returns clusters of
// reinforced facts that share a (predicate, object) pair across at least
// minDistinctEntities distinct entities.
//
// Parameters:
//   - store: any FactStore. The SQLite and in-memory backends both satisfy
//     the contract. Read-only — this function never calls Upsert*.
//   - predicateFilter: when non-empty, only facts with Triplet.Predicate
//     equal to this value contribute to clusters. Empty string means
//     "consider every predicate". The synthesizer uses the empty filter by
//     default; tests pin it to single predicates for readable assertions.
//   - minDistinctEntities: minimum count of distinct entity slugs required
//     for a (predicate, object) pair to be emitted as a cluster. Values < 2
//     are clamped to 2 — a single-entity "cluster" is not a pattern.
//   - topN: cap on the number of clusters returned. Clusters are sorted
//     strongest-first, so the head slice is the most informative window.
//     Values ≤ 0 mean "return every qualifying cluster" (unbounded).
//
// Implementation: we ask the store for the predicate-narrowed reinforced
// slice up front (ListReinforcedFactsByPredicate). On SQLite this hits the
// idx_facts_reinforced partial index; on the in-memory backend it filters
// linearly. Either way, the caller no longer scans the full corpus, and
// the (ReinforcedAt != nil) + predicate guards live in the store layer.
//
// Facts with nil Triplet are skipped. Clusters are returned sorted by
// (Count desc, Predicate asc, Object asc) so prompt output is stable
// across runs.
func clusterReinforcedFacts(
	ctx context.Context,
	store FactStore,
	predicateFilter string,
	minDistinctEntities int,
	topN int,
) ([]FactCluster, error) {
	if store == nil {
		return nil, fmt.Errorf("playbook_clusters: nil fact store")
	}
	if minDistinctEntities < 2 {
		minDistinctEntities = 2
	}

	// Pull only the slice we actually cluster against. The store-layer
	// predicate + reinforced filter means we never materialise the full fact
	// corpus into a single Go slice. ListReinforcedFactsByPredicate accepts
	// an empty predicate to mean "every predicate".
	facts, err := store.ListReinforcedFactsByPredicate(ctx, predicateFilter)
	if err != nil {
		return nil, fmt.Errorf("playbook_clusters: list reinforced facts: %w", err)
	}

	// Bucket reinforced facts by (predicate, object). Use a set of entity
	// slugs per bucket so a single entity reinforcing the same pair many
	// times does not inflate the count.
	type key struct{ predicate, object string }
	buckets := map[key]map[string]struct{}{}

	for _, f := range facts {
		if f.Triplet == nil {
			continue
		}
		predicate := strings.TrimSpace(f.Triplet.Predicate)
		object := strings.TrimSpace(f.Triplet.Object)
		if predicate == "" || object == "" {
			continue
		}
		entity := strings.TrimSpace(f.EntitySlug)
		if entity == "" {
			continue
		}
		k := key{predicate: predicate, object: object}
		set, ok := buckets[k]
		if !ok {
			set = map[string]struct{}{}
			buckets[k] = set
		}
		set[entity] = struct{}{}
	}

	var out []FactCluster
	for k, set := range buckets {
		if len(set) < minDistinctEntities {
			continue
		}
		entities := make([]string, 0, len(set))
		for slug := range set {
			entities = append(entities, slug)
		}
		sort.Strings(entities)
		out = append(out, FactCluster{
			Predicate: k.predicate,
			Object:    k.object,
			Entities:  entities,
			Count:     len(entities),
		})
	}

	// Stable ordering: strongest clusters first, then lexical tiebreakers.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Predicate != out[j].Predicate {
			return out[i].Predicate < out[j].Predicate
		}
		return out[i].Object < out[j].Object
	})
	// Head-slice at topN if set. Saves the downstream allocation of the
	// tail clusters the caller would throw away anyway. topN ≤ 0 means
	// "unbounded" — callers that want every cluster pass 0.
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out, nil
}
