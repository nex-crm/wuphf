package team

import (
	"context"
	"testing"
	"time"
)

// newClusterTestStore builds an in-memory fact store seeded with the given
// facts. The helper stamps CreatedAt if the caller left it zero so ordering
// is deterministic, and honors whatever ReinforcedAt the caller set.
func newClusterTestStore(t *testing.T, facts []TypedFact) FactStore {
	t.Helper()
	store := newInMemoryFactStore()
	ctx := context.Background()
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i, f := range facts {
		if f.CreatedAt.IsZero() {
			f.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		}
		if f.ID == "" {
			// Deterministic ID — tests assert on (predicate, object, entity)
			// not on IDs, but UpsertFact requires a non-empty ID to dedupe.
			f.ID = f.EntitySlug + "-" + f.Triplet.Predicate + "-" + f.Triplet.Object
		}
		if err := store.UpsertFact(ctx, f); err != nil {
			t.Fatalf("seed fact %d: %v", i, err)
		}
	}
	return store
}

// reinforcedFact is a compact constructor for table-driven test fixtures.
func reinforcedFact(entity, predicate, object string, reinforced bool) TypedFact {
	f := TypedFact{
		EntitySlug: entity,
		Triplet:    &Triplet{Subject: entity, Predicate: predicate, Object: object},
		Text:       entity + " " + predicate + " " + object,
	}
	if reinforced {
		r := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
		f.ReinforcedAt = &r
	}
	return f
}

func TestClusterReinforcedFacts(t *testing.T) {
	tests := []struct {
		name            string
		facts           []TypedFact
		predicateFilter string
		minEntities     int
		wantClusters    []FactCluster
	}{
		{
			name: "three entities share a reinforced pair → single cluster",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
				reinforcedFact("bob", "champions", "q2-pilot", true),
				reinforcedFact("carol", "champions", "q2-pilot", true),
			},
			minEntities: 3,
			wantClusters: []FactCluster{
				{
					Predicate: "champions",
					Object:    "q2-pilot",
					Entities:  []string{"alice", "bob", "carol"},
					Count:     3,
				},
			},
		},
		{
			name: "below threshold → no clusters",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
				reinforcedFact("bob", "champions", "q2-pilot", true),
			},
			minEntities:  3,
			wantClusters: nil,
		},
		{
			name: "non-reinforced facts are ignored even when shared",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", false),
				reinforcedFact("bob", "champions", "q2-pilot", false),
				reinforcedFact("carol", "champions", "q2-pilot", false),
			},
			minEntities:  3,
			wantClusters: nil,
		},
		{
			name: "mixed reinforced + not; only entities with reinforcement count",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
				reinforcedFact("bob", "champions", "q2-pilot", true),
				reinforcedFact("carol", "champions", "q2-pilot", false),
			},
			minEntities:  3,
			wantClusters: nil, // carol is not reinforced, so only 2 qualify
		},
		{
			name: "same entity reinforcing twice does not inflate count",
			facts: []TypedFact{
				{
					EntitySlug:   "alice",
					Triplet:      &Triplet{Subject: "alice", Predicate: "champions", Object: "q2-pilot"},
					Text:         "alice champions q2-pilot (first)",
					ID:           "alice-champions-q2-pilot-a",
					ReinforcedAt: ptrTime(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)),
				},
				{
					EntitySlug:   "alice",
					Triplet:      &Triplet{Subject: "alice", Predicate: "champions", Object: "q2-pilot"},
					Text:         "alice champions q2-pilot (second)",
					ID:           "alice-champions-q2-pilot-b",
					ReinforcedAt: ptrTime(time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)),
				},
				reinforcedFact("bob", "champions", "q2-pilot", true),
			},
			minEntities: 2,
			wantClusters: []FactCluster{
				{Predicate: "champions", Object: "q2-pilot", Entities: []string{"alice", "bob"}, Count: 2},
			},
		},
		{
			name: "predicate filter drops off-topic pairs",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
				reinforcedFact("bob", "champions", "q2-pilot", true),
				reinforcedFact("alice", "works_at", "acme-corp", true),
				reinforcedFact("bob", "works_at", "acme-corp", true),
			},
			predicateFilter: "champions",
			minEntities:     2,
			wantClusters: []FactCluster{
				{Predicate: "champions", Object: "q2-pilot", Entities: []string{"alice", "bob"}, Count: 2},
			},
		},
		{
			name: "no predicate filter → all qualifying clusters",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
				reinforcedFact("bob", "champions", "q2-pilot", true),
				reinforcedFact("alice", "works_at", "acme-corp", true),
				reinforcedFact("bob", "works_at", "acme-corp", true),
			},
			minEntities: 2,
			wantClusters: []FactCluster{
				// Equal counts → sorted by predicate asc, then object asc.
				{Predicate: "champions", Object: "q2-pilot", Entities: []string{"alice", "bob"}, Count: 2},
				{Predicate: "works_at", Object: "acme-corp", Entities: []string{"alice", "bob"}, Count: 2},
			},
		},
		{
			name: "nil triplet facts are skipped",
			facts: []TypedFact{
				{
					EntitySlug:   "alice",
					Text:         "freeform observation without triplet",
					ID:           "alice-freeform",
					ReinforcedAt: ptrTime(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)),
				},
				reinforcedFact("bob", "champions", "q2-pilot", true),
				reinforcedFact("carol", "champions", "q2-pilot", true),
			},
			minEntities: 2,
			wantClusters: []FactCluster{
				{Predicate: "champions", Object: "q2-pilot", Entities: []string{"bob", "carol"}, Count: 2},
			},
		},
		{
			name: "minEntities < 2 is clamped to 2",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
			},
			minEntities:  1,
			wantClusters: nil, // clamped → 2 required, only 1 entity present
		},
		{
			name: "count desc ordering surfaces strongest cluster first",
			facts: []TypedFact{
				reinforcedFact("alice", "champions", "q2-pilot", true),
				reinforcedFact("bob", "champions", "q2-pilot", true),
				reinforcedFact("carol", "works_at", "acme-corp", true),
				reinforcedFact("dave", "works_at", "acme-corp", true),
				reinforcedFact("eve", "works_at", "acme-corp", true),
			},
			minEntities: 2,
			wantClusters: []FactCluster{
				{Predicate: "works_at", Object: "acme-corp", Entities: []string{"carol", "dave", "eve"}, Count: 3},
				{Predicate: "champions", Object: "q2-pilot", Entities: []string{"alice", "bob"}, Count: 2},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			store := newClusterTestStore(t, tc.facts)
			got, err := clusterReinforcedFacts(context.Background(), store, tc.predicateFilter, tc.minEntities, 0)
			if err != nil {
				t.Fatalf("clusterReinforcedFacts: %v", err)
			}
			if !equalClusters(got, tc.wantClusters) {
				t.Fatalf("cluster mismatch\n got:  %#v\n want: %#v", got, tc.wantClusters)
			}
		})
	}
}

func TestClusterReinforcedFacts_NilStore(t *testing.T) {
	_, err := clusterReinforcedFacts(context.Background(), nil, "", 2, 0)
	if err == nil {
		t.Fatalf("expected error for nil store")
	}
}

func TestClusterReinforcedFacts_SQLiteParity(t *testing.T) {
	// The SQLite and in-memory backends must produce identical clusters from
	// the same seed. This is the read-side complement of the §7.4 rebuild
	// contract — cluster detection is a pure function of the fact store,
	// regardless of backend.
	seed := []TypedFact{
		reinforcedFact("alice", "champions", "q2-pilot", true),
		reinforcedFact("bob", "champions", "q2-pilot", true),
		reinforcedFact("carol", "champions", "q2-pilot", true),
		reinforcedFact("dave", "works_at", "acme-corp", true),
		reinforcedFact("eve", "works_at", "acme-corp", true),
	}

	memStore := newClusterTestStore(t, seed)
	memClusters, err := clusterReinforcedFacts(context.Background(), memStore, "", 2, 0)
	if err != nil {
		t.Fatalf("mem cluster: %v", err)
	}

	sqlitePath := t.TempDir() + "/cluster-parity.sqlite"
	sqliteStore, err := NewSQLiteFactStore(sqlitePath)
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer func() { _ = sqliteStore.Close() }()

	ctx := context.Background()
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i, f := range seed {
		if f.CreatedAt.IsZero() {
			f.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		}
		if f.ID == "" {
			f.ID = f.EntitySlug + "-" + f.Triplet.Predicate + "-" + f.Triplet.Object
		}
		if err := sqliteStore.UpsertFact(ctx, f); err != nil {
			t.Fatalf("sqlite seed %d: %v", i, err)
		}
	}

	sqliteClusters, err := clusterReinforcedFacts(ctx, sqliteStore, "", 2, 0)
	if err != nil {
		t.Fatalf("sqlite cluster: %v", err)
	}

	if !equalClusters(memClusters, sqliteClusters) {
		t.Fatalf("backend parity broken\n mem:    %#v\n sqlite: %#v", memClusters, sqliteClusters)
	}
}

// equalClusters compares two cluster slices element-wise. Entities within a
// cluster are already sorted by clusterReinforcedFacts, so a direct compare
// is safe.
func equalClusters(a, b []FactCluster) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Predicate != b[i].Predicate ||
			a[i].Object != b[i].Object ||
			a[i].Count != b[i].Count ||
			!stringSlicesEqual(a[i].Entities, b[i].Entities) {
			return false
		}
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
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

func ptrTime(t time.Time) *time.Time { return &t }

// TestClusterReinforcedFacts_TopNShortCircuits verifies topN trims the output
// after sorting — strongest-first ordering is preserved and the tail is
// dropped. Callers that want everything pass 0.
func TestClusterReinforcedFacts_TopNShortCircuits(t *testing.T) {
	store := newClusterTestStore(t, []TypedFact{
		// Cluster A: 3 entities
		reinforcedFact("a1", "champions", "pilot-a", true),
		reinforcedFact("a2", "champions", "pilot-a", true),
		reinforcedFact("a3", "champions", "pilot-a", true),
		// Cluster B: 2 entities
		reinforcedFact("b1", "champions", "pilot-b", true),
		reinforcedFact("b2", "champions", "pilot-b", true),
		// Cluster C: 2 entities
		reinforcedFact("c1", "champions", "pilot-c", true),
		reinforcedFact("c2", "champions", "pilot-c", true),
	})

	// Without a cap, we get all 3.
	all, err := clusterReinforcedFacts(context.Background(), store, "", 2, 0)
	if err != nil {
		t.Fatalf("all clusters: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 clusters without cap; got %d", len(all))
	}

	// topN=1 — only the strongest survives.
	top1, err := clusterReinforcedFacts(context.Background(), store, "", 2, 1)
	if err != nil {
		t.Fatalf("top1: %v", err)
	}
	if len(top1) != 1 {
		t.Fatalf("expected 1 cluster with topN=1; got %d", len(top1))
	}
	if top1[0].Object != "pilot-a" {
		t.Errorf("expected strongest cluster first; got %q", top1[0].Object)
	}

	// topN > len(clusters) — no-op, all returned.
	topBig, err := clusterReinforcedFacts(context.Background(), store, "", 2, 999)
	if err != nil {
		t.Fatalf("topBig: %v", err)
	}
	if len(topBig) != 3 {
		t.Fatalf("expected 3 clusters with topN=999; got %d", len(topBig))
	}
}
