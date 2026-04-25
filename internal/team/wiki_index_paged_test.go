package team

// wiki_index_paged_test.go — coverage for the Slice 3 Thread A FactStore
// extensions: ListReinforcedFactsByPredicate (predicate-narrowed reinforced
// scan, index-backed on SQLite) and ListAllFactsPaged (keyset pagination).
//
// Each new method gets a parity test against both backends so the SQLite
// implementation can never silently diverge from the in-memory reference.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// reinforcedSeed builds a deterministic seed of 5 facts across mixed predicates
// and reinforcement states. Returned in seed order so callers can assert on a
// known shape regardless of backend storage order.
func reinforcedSeed() []TypedFact {
	base := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	at := func(d int) *time.Time { t := base.AddDate(0, 0, d); return &t }
	return []TypedFact{
		{
			ID: "fact-001", EntitySlug: "alice",
			Triplet:   &Triplet{Subject: "alice", Predicate: "champions", Object: "q2-pilot"},
			Text:      "alice champions q2-pilot",
			CreatedAt: base, CreatedBy: "test", ReinforcedAt: at(1),
		},
		{
			ID: "fact-002", EntitySlug: "bob",
			Triplet:   &Triplet{Subject: "bob", Predicate: "champions", Object: "q2-pilot"},
			Text:      "bob champions q2-pilot",
			CreatedAt: base, CreatedBy: "test", ReinforcedAt: at(2),
		},
		{
			ID: "fact-003", EntitySlug: "carol",
			Triplet:   &Triplet{Subject: "carol", Predicate: "works_at", Object: "acme-corp"},
			Text:      "carol works_at acme-corp",
			CreatedAt: base, CreatedBy: "test",
			// NOT reinforced — should be excluded.
		},
		{
			ID: "fact-004", EntitySlug: "dave",
			Triplet:   &Triplet{Subject: "dave", Predicate: "works_at", Object: "acme-corp"},
			Text:      "dave works_at acme-corp",
			CreatedAt: base, CreatedBy: "test", ReinforcedAt: at(3),
		},
		{
			ID: "fact-005", EntitySlug: "eve",
			Triplet:   &Triplet{Subject: "eve", Predicate: "champions", Object: "another-pilot"},
			Text:      "eve champions another-pilot",
			CreatedAt: base, CreatedBy: "test", ReinforcedAt: at(4),
		},
	}
}

func seedStore(t *testing.T, store FactStore, facts []TypedFact) {
	t.Helper()
	ctx := context.Background()
	for i, f := range facts {
		if err := store.UpsertFact(ctx, f); err != nil {
			t.Fatalf("seed fact %d: %v", i, err)
		}
	}
}

func factIDs(facts []TypedFact) []string {
	ids := make([]string, 0, len(facts))
	for _, f := range facts {
		ids = append(ids, f.ID)
	}
	return ids
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestListReinforcedFactsByPredicate_InMemory pins the filter contract for the
// in-memory backend: empty predicate returns every reinforced fact; specific
// predicate returns only the matching reinforced rows; unreinforced rows are
// always excluded; ordering is by ID ASC.
func TestListReinforcedFactsByPredicate_InMemory(t *testing.T) {
	store := newInMemoryFactStore()
	seedStore(t, store, reinforcedSeed())

	ctx := context.Background()

	all, err := store.ListReinforcedFactsByPredicate(ctx, "")
	if err != nil {
		t.Fatalf("empty predicate: %v", err)
	}
	wantAll := []string{"fact-001", "fact-002", "fact-004", "fact-005"}
	if !sliceEqual(factIDs(all), wantAll) {
		t.Fatalf("empty predicate ids:\n got:  %v\n want: %v", factIDs(all), wantAll)
	}

	champ, err := store.ListReinforcedFactsByPredicate(ctx, "champions")
	if err != nil {
		t.Fatalf("champions predicate: %v", err)
	}
	wantChamp := []string{"fact-001", "fact-002", "fact-005"}
	if !sliceEqual(factIDs(champ), wantChamp) {
		t.Fatalf("champions ids:\n got:  %v\n want: %v", factIDs(champ), wantChamp)
	}

	works, err := store.ListReinforcedFactsByPredicate(ctx, "works_at")
	if err != nil {
		t.Fatalf("works_at predicate: %v", err)
	}
	// fact-003 is unreinforced, fact-004 is reinforced.
	wantWorks := []string{"fact-004"}
	if !sliceEqual(factIDs(works), wantWorks) {
		t.Fatalf("works_at ids:\n got:  %v\n want: %v", factIDs(works), wantWorks)
	}

	// Unknown predicate → empty slice, not error.
	none, err := store.ListReinforcedFactsByPredicate(ctx, "no-such-predicate")
	if err != nil {
		t.Fatalf("unknown predicate: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unknown predicate: expected 0 facts; got %d", len(none))
	}
}

// TestListReinforcedFactsByPredicate_SQLite pins the same contract on the
// SQLite backend. The partial index idx_facts_reinforced is the load-bearing
// piece — if it is missing the query still works but linearly scans the
// table; the parity assertion catches any logic divergence.
func TestListReinforcedFactsByPredicate_SQLite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteFactStore(filepath.Join(dir, "reinforced.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteFactStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedStore(t, store, reinforcedSeed())

	ctx := context.Background()

	all, err := store.ListReinforcedFactsByPredicate(ctx, "")
	if err != nil {
		t.Fatalf("empty predicate: %v", err)
	}
	wantAll := []string{"fact-001", "fact-002", "fact-004", "fact-005"}
	if !sliceEqual(factIDs(all), wantAll) {
		t.Fatalf("empty predicate ids:\n got:  %v\n want: %v", factIDs(all), wantAll)
	}

	champ, err := store.ListReinforcedFactsByPredicate(ctx, "champions")
	if err != nil {
		t.Fatalf("champions predicate: %v", err)
	}
	wantChamp := []string{"fact-001", "fact-002", "fact-005"}
	if !sliceEqual(factIDs(champ), wantChamp) {
		t.Fatalf("champions ids:\n got:  %v\n want: %v", factIDs(champ), wantChamp)
	}

	works, err := store.ListReinforcedFactsByPredicate(ctx, "works_at")
	if err != nil {
		t.Fatalf("works_at predicate: %v", err)
	}
	wantWorks := []string{"fact-004"}
	if !sliceEqual(factIDs(works), wantWorks) {
		t.Fatalf("works_at ids:\n got:  %v\n want: %v", factIDs(works), wantWorks)
	}

	none, err := store.ListReinforcedFactsByPredicate(ctx, "no-such-predicate")
	if err != nil {
		t.Fatalf("unknown predicate: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unknown predicate: expected 0 facts; got %d", len(none))
	}
}

// pagedSeed builds 50 facts with deterministic, sortable IDs. Pagination
// tests walk this seed in slices and assert each ID appears exactly once
// in the natural sort order.
func pagedSeed() []TypedFact {
	base := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	out := make([]TypedFact, 50)
	for i := 0; i < 50; i++ {
		// Zero-padded so lexicographic order matches numeric order.
		id := fmt.Sprintf("paged-%03d", i)
		out[i] = TypedFact{
			ID:         id,
			EntitySlug: fmt.Sprintf("entity-%03d", i),
			Text:       fmt.Sprintf("paged fact %d", i),
			CreatedAt:  base,
			CreatedBy:  "test",
		}
	}
	return out
}

// walkPaged drains the store via ListAllFactsPaged with the given page size and
// returns the IDs in the order they were observed.
func walkPaged(t *testing.T, store FactStore, pageSize int) []string {
	t.Helper()
	ctx := context.Background()
	var (
		seen    []string
		afterID string
	)
	// Hard upper bound on iterations so a buggy implementation never spins.
	for iter := 0; iter < 1000; iter++ {
		page, err := store.ListAllFactsPaged(ctx, afterID, pageSize)
		if err != nil {
			t.Fatalf("ListAllFactsPaged after=%q: %v", afterID, err)
		}
		if len(page) == 0 {
			return seen
		}
		for _, f := range page {
			seen = append(seen, f.ID)
		}
		afterID = page[len(page)-1].ID
	}
	t.Fatalf("walkPaged: too many iterations; afterID stuck at %q", afterID)
	return nil
}

// TestListAllFactsPaged_InMemory walks 50 seeded facts in pages of 10 and
// asserts every fact is observed once in ID-ascending order.
func TestListAllFactsPaged_InMemory(t *testing.T) {
	store := newInMemoryFactStore()
	seedStore(t, store, pagedSeed())

	got := walkPaged(t, store, 10)
	if len(got) != 50 {
		t.Fatalf("expected 50 facts; got %d", len(got))
	}
	for i, id := range got {
		want := fmt.Sprintf("paged-%03d", i)
		if id != want {
			t.Fatalf("fact at index %d: got %q want %q", i, id, want)
		}
	}

	// Default limit (limit <= 0) should also produce a sane page — we have
	// 50 facts, default cap is 1000, so a single call returns everything.
	ctx := context.Background()
	full, err := store.ListAllFactsPaged(ctx, "", 0)
	if err != nil {
		t.Fatalf("default limit: %v", err)
	}
	if len(full) != 50 {
		t.Fatalf("default limit: expected 50; got %d", len(full))
	}

	// Past the end → empty page, no error.
	tail, err := store.ListAllFactsPaged(ctx, "paged-049", 10)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("tail: expected 0; got %d", len(tail))
	}
}

// TestListAllFactsPaged_SQLite mirrors the in-memory pagination assertions
// against the SQLite backend so the keyset query (id > ? ORDER BY id LIMIT ?)
// stays in lockstep with the reference implementation.
func TestListAllFactsPaged_SQLite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteFactStore(filepath.Join(dir, "paged.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteFactStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedStore(t, store, pagedSeed())

	got := walkPaged(t, store, 10)
	if len(got) != 50 {
		t.Fatalf("expected 50 facts; got %d", len(got))
	}
	for i, id := range got {
		want := fmt.Sprintf("paged-%03d", i)
		if id != want {
			t.Fatalf("fact at index %d: got %q want %q", i, id, want)
		}
	}

	ctx := context.Background()
	full, err := store.ListAllFactsPaged(ctx, "", 0)
	if err != nil {
		t.Fatalf("default limit: %v", err)
	}
	if len(full) != 50 {
		t.Fatalf("default limit: expected 50; got %d", len(full))
	}

	tail, err := store.ListAllFactsPaged(ctx, "paged-049", 10)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("tail: expected 0; got %d", len(tail))
	}
}

// TestFindFactClusters_UsesIndexedPath is the regression guard for the
// rewrite: the cluster result with a predicate filter must equal the result
// of bucketing the full ListAllFacts manually. If a future refactor
// reintroduces post-filter logic that diverges from the predicate path,
// this test catches it.
func TestFindFactClusters_UsesIndexedPath(t *testing.T) {
	store := newClusterTestStore(t, []TypedFact{
		reinforcedFact("alice", "champions", "q2-pilot", true),
		reinforcedFact("bob", "champions", "q2-pilot", true),
		reinforcedFact("carol", "champions", "q2-pilot", true),
		// Different predicate — must NOT contribute under predicate=champions.
		reinforcedFact("alice", "works_at", "acme-corp", true),
		reinforcedFact("bob", "works_at", "acme-corp", true),
		// Reinforced but different object — separate cluster.
		reinforcedFact("dave", "champions", "q3-pilot", true),
		reinforcedFact("eve", "champions", "q3-pilot", true),
		// Unreinforced — must be ignored.
		reinforcedFact("frank", "champions", "q2-pilot", false),
	})

	ctx := context.Background()
	got, err := clusterReinforcedFacts(ctx, store, "champions", 2, 0)
	if err != nil {
		t.Fatalf("clusterReinforcedFacts: %v", err)
	}

	// Manually bucket via ListAllFacts to derive the expected result. This
	// is the reference implementation we replaced — running it inline keeps
	// the parity check tied to a property of the data, not a hard-coded
	// expectation.
	allFacts, err := store.ListAllFacts(ctx)
	if err != nil {
		t.Fatalf("ListAllFacts: %v", err)
	}
	type pair struct{ p, o string }
	bucket := map[pair]map[string]struct{}{}
	for _, f := range allFacts {
		if f.Triplet == nil || f.ReinforcedAt == nil {
			continue
		}
		if f.Triplet.Predicate != "champions" {
			continue
		}
		k := pair{f.Triplet.Predicate, f.Triplet.Object}
		set, ok := bucket[k]
		if !ok {
			set = map[string]struct{}{}
			bucket[k] = set
		}
		set[f.EntitySlug] = struct{}{}
	}

	// Build the same FactCluster shape from the manual bucket.
	wantCount := 0
	for _, set := range bucket {
		if len(set) >= 2 {
			wantCount++
		}
	}
	if len(got) != wantCount {
		t.Fatalf("cluster count: got %d want %d (clusters=%v)", len(got), wantCount, got)
	}

	// Spot-check: every emitted cluster must match the manual bucket exactly.
	for _, c := range got {
		set := bucket[pair{c.Predicate, c.Object}]
		if c.Count != len(set) {
			t.Errorf("cluster %s/%s: got count %d want %d",
				c.Predicate, c.Object, c.Count, len(set))
		}
		for _, e := range c.Entities {
			if _, ok := set[e]; !ok {
				t.Errorf("cluster %s/%s: emitted entity %q not in manual bucket",
					c.Predicate, c.Object, e)
			}
		}
	}
}
