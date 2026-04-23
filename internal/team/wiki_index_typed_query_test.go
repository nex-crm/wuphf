package team

// wiki_index_typed_query_test.go — tests for the Slice 2 Thread A typed-
// predicate read paths (ListFactsByPredicateObject, ListFactsByTriplet).
//
// Covers both the default in-memory FactStore and the SQLite backend so the
// typed-graph walk in wiki_query.go can rely on identical semantics across
// test and production boots.

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// factStoreCase bundles a FactStore constructor + teardown so tests can run
// the same corpus against both backends.
type factStoreCase struct {
	name string
	open func(t *testing.T) FactStore
}

func factStoreCases() []factStoreCase {
	return []factStoreCase{
		{
			name: "in-memory",
			open: func(t *testing.T) FactStore {
				return newInMemoryFactStore()
			},
		},
		{
			name: "sqlite",
			open: func(t *testing.T) FactStore {
				t.Helper()
				dir := t.TempDir()
				s, err := NewSQLiteFactStore(filepath.Join(dir, "typed.sqlite"))
				if err != nil {
					t.Fatalf("NewSQLiteFactStore: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
	}
}

// seedTriplet creates a TypedFact with the given (subject, predicate, object)
// triplet plus deterministic non-triplet fields.
func seedTriplet(id, subject, predicate, object string) TypedFact {
	ts := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	return TypedFact{
		ID:         id,
		EntitySlug: subject,
		Kind:       "person",
		Type:       "status",
		Triplet:    &Triplet{Subject: subject, Predicate: predicate, Object: object},
		Text:       id + " text",
		Confidence: 0.9,
		ValidFrom:  ts,
		CreatedAt:  ts,
		CreatedBy:  "test",
	}
}

// collectIDs returns sorted fact IDs from a fact slice. Handy for set equality.
func collectIDs(fs []TypedFact) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.ID)
	}
	sort.Strings(out)
	return out
}

// TestListFactsByPredicateObject — covers exact predicate+object match across
// both backends. Includes negative cases (wrong object, wrong predicate,
// triplet-less fact) so the SQL LIKE trap can't sneak in.
func TestListFactsByPredicateObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range factStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.open(t)

			// Seed: two facts match (champions, project:q2-pilot); one matches
			// predicate only; one matches object only; one has no triplet.
			seed := []TypedFact{
				seedTriplet("hit-1", "alice", "champions", "project:q2-pilot"),
				seedTriplet("hit-2", "bob", "champions", "project:q2-pilot"),
				seedTriplet("miss-pred", "carol", "leads", "project:q2-pilot"),
				seedTriplet("miss-obj", "dan", "champions", "project:other"),
				{
					ID:         "no-triplet",
					EntitySlug: "eve",
					Text:       "note without triplet",
					CreatedAt:  time.Now(),
					CreatedBy:  "test",
				},
			}
			for _, f := range seed {
				if err := s.UpsertFact(ctx, f); err != nil {
					t.Fatalf("UpsertFact %s: %v", f.ID, err)
				}
			}

			got, err := s.ListFactsByPredicateObject(ctx, "champions", "project:q2-pilot")
			if err != nil {
				t.Fatalf("ListFactsByPredicateObject: %v", err)
			}
			gotIDs := collectIDs(got)
			wantIDs := []string{"hit-1", "hit-2"}
			if !equalStringSlices(gotIDs, wantIDs) {
				t.Errorf("got ids %v, want %v", gotIDs, wantIDs)
			}

			// Empty result set when predicate/object don't exist.
			empty, err := s.ListFactsByPredicateObject(ctx, "champions", "project:does-not-exist")
			if err != nil {
				t.Fatalf("ListFactsByPredicateObject empty: %v", err)
			}
			if len(empty) != 0 {
				t.Errorf("want empty result, got %v", collectIDs(empty))
			}
		})
	}
}

// TestListFactsByTriplet — covers (subject, predicate) lookup plus objectPrefix
// behaviour. The prefix mode underpins the multi_hop typed walk (role_at facts
// for a specific company) and the counterfactual rewrite (any role_at for
// a person).
func TestListFactsByTriplet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range factStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.open(t)

			// Seed: alice has two role_at facts (acme, blueshift) and one
			// involved_in; bob has one role_at at acme.
			seed := []TypedFact{
				seedTriplet("alice-role-acme", "alice", "role_at", "company:acme-corp"),
				seedTriplet("alice-role-bs", "alice", "role_at", "company:blueshift"),
				seedTriplet("alice-involved", "alice", "involved_in", "project:q2-pilot"),
				seedTriplet("bob-role-acme", "bob", "role_at", "company:acme-corp"),
			}
			for _, f := range seed {
				if err := s.UpsertFact(ctx, f); err != nil {
					t.Fatalf("UpsertFact %s: %v", f.ID, err)
				}
			}

			// Exact prefix match: alice's role_at at acme.
			got, err := s.ListFactsByTriplet(ctx, "alice", "role_at", "company:acme-corp")
			if err != nil {
				t.Fatalf("ListFactsByTriplet exact: %v", err)
			}
			if ids := collectIDs(got); !equalStringSlices(ids, []string{"alice-role-acme"}) {
				t.Errorf("exact prefix: got %v, want [alice-role-acme]", ids)
			}

			// Broader prefix: alice's role_at at any company:* → both facts.
			got, err = s.ListFactsByTriplet(ctx, "alice", "role_at", "company:")
			if err != nil {
				t.Fatalf("ListFactsByTriplet broad: %v", err)
			}
			if ids := collectIDs(got); !equalStringSlices(ids, []string{"alice-role-acme", "alice-role-bs"}) {
				t.Errorf("broad prefix: got %v, want alice-role-acme + alice-role-bs", ids)
			}

			// Empty prefix: alice's role_at at anything.
			got, err = s.ListFactsByTriplet(ctx, "alice", "role_at", "")
			if err != nil {
				t.Fatalf("ListFactsByTriplet empty prefix: %v", err)
			}
			if ids := collectIDs(got); !equalStringSlices(ids, []string{"alice-role-acme", "alice-role-bs"}) {
				t.Errorf("empty prefix: got %v, want both alice role_at facts", ids)
			}

			// Subject mismatch: bob has no role_at at blueshift.
			got, err = s.ListFactsByTriplet(ctx, "bob", "role_at", "company:blueshift")
			if err != nil {
				t.Fatalf("ListFactsByTriplet miss: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("subject miss: got %v, want empty", collectIDs(got))
			}

			// Predicate mismatch: alice has no leads fact at all.
			got, err = s.ListFactsByTriplet(ctx, "alice", "leads", "")
			if err != nil {
				t.Fatalf("ListFactsByTriplet pred miss: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("pred miss: got %v, want empty", collectIDs(got))
			}
		})
	}
}

// TestListFactsByTriplet_EscapesLikeWildcards guards against a future caller
// passing raw bytes (not just NormalizeForFactID output) as the object prefix.
// A literal "%" or "_" must match only itself, not act as a SQL LIKE wildcard
// that causes over-matching. The in-memory backend uses strings.HasPrefix and
// is inherently safe; the SQLite backend explicitly escapes metacharacters.
func TestListFactsByTriplet_EscapesLikeWildcards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range factStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.open(t)

			// Two facts: one with a "%" in the object (exact prefix "foo%")
			// and a sibling that would false-match if "%" were treated as a
			// wildcard ("foobar").
			seed := []TypedFact{
				seedTriplet("exact-pct", "alice", "custom_pred", "foo%bar"),
				seedTriplet("wildcard-trap", "alice", "custom_pred", "foobar"),
				seedTriplet("exact-under", "alice", "custom_pred", "foo_baz"),
				seedTriplet("under-trap", "alice", "custom_pred", "fooXbaz"),
			}
			for _, f := range seed {
				if err := s.UpsertFact(ctx, f); err != nil {
					t.Fatalf("UpsertFact %s: %v", f.ID, err)
				}
			}

			// "foo%" prefix must match only "foo%bar", never "foobar".
			got, err := s.ListFactsByTriplet(ctx, "alice", "custom_pred", "foo%")
			if err != nil {
				t.Fatalf("ListFactsByTriplet %%: %v", err)
			}
			if ids := collectIDs(got); !equalStringSlices(ids, []string{"exact-pct"}) {
				t.Errorf("%% escape: got %v, want [exact-pct]", ids)
			}

			// "foo_" prefix must match only "foo_baz", never "fooXbaz".
			got, err = s.ListFactsByTriplet(ctx, "alice", "custom_pred", "foo_")
			if err != nil {
				t.Fatalf("ListFactsByTriplet _: %v", err)
			}
			if ids := collectIDs(got); !equalStringSlices(ids, []string{"exact-under"}) {
				t.Errorf("_ escape: got %v, want [exact-under]", ids)
			}
		})
	}
}

// equalStringSlices compares two string slices assuming both are already
// sorted. Keeps test intent explicit without pulling reflect.DeepEqual into
// the hot path.
func equalStringSlices(a, b []string) bool {
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
