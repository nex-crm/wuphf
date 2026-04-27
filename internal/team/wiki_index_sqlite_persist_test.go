package team

// wiki_index_sqlite_persist_test.go — disk-persistence + paginated-walk
// coverage for SQLiteFactStore. Closes the DB and reopens at the same path so
// a regression in the underlying sqlite driver's WAL flush, on-disk format, or
// iterator state would surface here. The other tests in this package use a
// single in-process handle and would not catch any of those.

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

// TestSQLiteFactStore_PersistAcrossClose writes facts + entity + edge +
// redirect, closes the store, reopens at the same path, and verifies that
// every read path observes the data identically. CanonicalHashAll must be
// stable across close/reopen, ListAllFactsPaged must walk every row in
// id-sorted order when the page limit is smaller than the row count, and
// IterateEntities must stream every row through its callback.
func TestSQLiteFactStore_PersistAcrossClose(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.sqlite")

	// --- session 1: write all four row types, then close -------------------

	s1, err := NewSQLiteFactStore(path)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}

	const factCount = 5
	for i := 0; i < factCount; i++ {
		f := sampleFact(fmt.Sprintf("p-%02d", i), fmt.Sprintf("slug-%02d", i))
		if err := s1.UpsertFact(ctx, f); err != nil {
			t.Fatalf("UpsertFact %d: %v", i, err)
		}
	}
	// Two entities so IterateEntities crosses a row boundary AND we can
	// assert the slug-ASC ordering the contract guarantees.
	entities := []IndexEntity{
		{
			Slug:          "slug-00",
			CanonicalSlug: "slug-00",
			Kind:          "person",
			Aliases:       []string{"alias-a", "alias-b"},
		},
		{
			Slug:          "slug-01",
			CanonicalSlug: "slug-01",
			Kind:          "company",
		},
	}
	for _, e := range entities {
		if err := s1.UpsertEntity(ctx, e); err != nil {
			t.Fatalf("UpsertEntity %s: %v", e.Slug, err)
		}
	}
	if err := s1.UpsertEdge(ctx, IndexEdge{
		Subject:   "slug-00",
		Predicate: "knows",
		Object:    "slug-01",
		SourceSHA: "abc123",
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	if err := s1.UpsertRedirect(ctx, Redirect{
		From:      "old-slug-00",
		To:        "slug-00",
		MergedBy:  "tester",
		CommitSHA: "def456",
	}); err != nil {
		t.Fatalf("UpsertRedirect: %v", err)
	}

	hashBefore, err := s1.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll before close: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	// --- session 2: reopen at the same path, verify reads ------------------

	s2, err := NewSQLiteFactStore(path)
	if err != nil {
		t.Fatalf("open 2: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	gotCount, err := s2.CountFacts(ctx)
	if err != nil {
		t.Fatalf("CountFacts: %v", err)
	}
	if gotCount != factCount {
		t.Errorf("CountFacts after reopen = %d, want %d", gotCount, factCount)
	}

	// Walk every fact via ListAllFactsPaged with a limit smaller than the
	// total row count, exercising the keyset pagination path end to end.
	//
	// Three independent assertions on `seen`:
	//   1) every row appears (set equality)
	//   2) rows are id-ASC sorted (a driver collation regression would surface
	//      as out-of-order pages but the keyset cursor advances by the page's
	//      *last* element — silently skipping IDs and false-passing a naive
	//      length check)
	//   3) row count matches expected
	var seen []string
	after := ""
	for {
		page, err := s2.ListAllFactsPaged(ctx, after, 2)
		if err != nil {
			t.Fatalf("ListAllFactsPaged after=%q: %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		for _, f := range page {
			seen = append(seen, f.ID)
		}
		after = page[len(page)-1].ID
	}
	if len(seen) != factCount {
		t.Fatalf("ListAllFactsPaged walked %d rows, want %d (seen=%v)", len(seen), factCount, seen)
	}
	if !sort.StringsAreSorted(seen) {
		t.Errorf("ListAllFactsPaged not id-ASC sorted: %v", seen)
	}
	gotSet := make(map[string]struct{}, len(seen))
	for _, id := range seen {
		gotSet[id] = struct{}{}
	}
	for i := 0; i < factCount; i++ {
		want := fmt.Sprintf("p-%02d", i)
		if _, ok := gotSet[want]; !ok {
			t.Errorf("ListAllFactsPaged missing id %q (seen=%v)", want, seen)
		}
	}

	// IterateEntities streams the entities table through a callback. Failure
	// modes here (mid-iteration cursor invalidation, premature rows.Err(),
	// reordered streaming) are exactly the kind a sqlite driver bump can
	// regress without surfacing in the bulk-result query paths above.
	var iteratedSlugs []string
	var iteratedKinds []string
	var iteratedAliases [][]string
	if err := s2.IterateEntities(ctx, func(e IndexEntity) error {
		iteratedSlugs = append(iteratedSlugs, e.Slug)
		iteratedKinds = append(iteratedKinds, e.Kind)
		iteratedAliases = append(iteratedAliases, e.Aliases)
		return nil
	}); err != nil {
		t.Fatalf("IterateEntities: %v", err)
	}
	wantSlugs := []string{"slug-00", "slug-01"}
	if !sort.StringsAreSorted(iteratedSlugs) {
		t.Errorf("IterateEntities not slug-ASC sorted: %v", iteratedSlugs)
	}
	if len(iteratedSlugs) != len(wantSlugs) {
		t.Fatalf("IterateEntities yielded %d entities, want %d (got=%v)", len(iteratedSlugs), len(wantSlugs), iteratedSlugs)
	}
	for i, want := range wantSlugs {
		if iteratedSlugs[i] != want {
			t.Errorf("IterateEntities[%d].Slug = %q, want %q", i, iteratedSlugs[i], want)
		}
	}
	if iteratedKinds[0] != "person" || iteratedKinds[1] != "company" {
		t.Errorf("IterateEntities Kind = [%q %q], want [person company]", iteratedKinds[0], iteratedKinds[1])
	}
	if len(iteratedAliases[0]) != 2 || iteratedAliases[0][0] != "alias-a" || iteratedAliases[0][1] != "alias-b" {
		t.Errorf("IterateEntities Aliases[0] = %v, want [alias-a alias-b]", iteratedAliases[0])
	}

	// Redirect must survive close/reopen.
	to, ok, err := s2.ResolveRedirect(ctx, "old-slug-00")
	if err != nil {
		t.Fatalf("ResolveRedirect: %v", err)
	}
	if !ok || to != "slug-00" {
		t.Errorf("ResolveRedirect after reopen = (%q, %v), want (slug-00, true)", to, ok)
	}

	// CanonicalHashAll must be byte-identical across close/reopen.
	hashAfter, err := s2.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll after reopen: %v", err)
	}
	if hashBefore != hashAfter {
		t.Errorf("CanonicalHashAll drift across close/reopen:\n  before = %s\n  after  = %s",
			hashBefore, hashAfter)
	}
}
