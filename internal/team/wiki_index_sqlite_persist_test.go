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
	"time"
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
	// Register cleanup before any t.Fatalf path could leak the handle.
	// Idempotent: explicit s1.Close() below short-circuits this on the
	// happy path; on early-fatal it actually fires.
	s1Closed := false
	t.Cleanup(func() {
		if !s1Closed {
			_ = s1.Close()
		}
	})

	const factCount = 5
	for i := 0; i < factCount; i++ {
		f := sampleFact(fmt.Sprintf("p-%02d", i), fmt.Sprintf("slug-%02d", i))
		if err := s1.UpsertFact(ctx, f); err != nil {
			t.Fatalf("UpsertFact %d: %v", i, err)
		}
	}
	// Two entities so IterateEntities crosses a row boundary AND we can
	// assert the slug-ASC ordering the contract guarantees.
	//
	// slug-00 carries non-zero CreatedAt + LastSynthesizedAt so the
	// time.Parse(RFC3339) branches in IterateEntities and CanonicalHashAll
	// are exercised on reopen — those would otherwise be dead code in this
	// test, and a TZ/precision regression in modernc.org/sqlite's TEXT
	// column round-trip would silently false-pass.
	createdAt := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	lastSynthAt := time.Date(2026, 4, 23, 9, 30, 0, 0, time.UTC)
	mergedAt := time.Date(2026, 4, 24, 15, 45, 0, 0, time.UTC)
	entities := []IndexEntity{
		{
			Slug:              "slug-00",
			CanonicalSlug:     "slug-00",
			Kind:              "person",
			Aliases:           []string{"alias-a", "alias-b"},
			CreatedAt:         createdAt,
			LastSynthesizedAt: lastSynthAt,
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
	// Non-zero MergedAt so the redirect's time.Parse branch in
	// CanonicalHashAll is exercised on reopen — same dead-code-coverage
	// motivation as the entity timestamps above.
	if err := s1.UpsertRedirect(ctx, Redirect{
		From:      "old-slug-00",
		To:        "slug-00",
		MergedAt:  mergedAt,
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
	s1Closed = true

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
	// Independent assertions on `seen`:
	//   1) every row appears (set equality)
	//   2) rows are id-ASC sorted (a driver collation regression would surface
	//      as out-of-order pages but the keyset cursor advances by the page's
	//      *last* element — silently skipping IDs and false-passing a naive
	//      length check)
	//   3) row count matches expected
	//   4) the walk crossed at least one page boundary (factCount > pageLimit
	//      so a LIMIT regression that returned every row in one page would
	//      false-pass the pagination objective of this test)
	//   5) the cursor advances every iteration (a regression where
	//      ListAllFactsPaged kept returning the same non-empty tail page would
	//      hang the test instead of failing fast)
	const pageLimit = 2
	var seen []string
	after := ""
	pagesSeen := 0
	for {
		page, err := s2.ListAllFactsPaged(ctx, after, pageLimit)
		if err != nil {
			t.Fatalf("ListAllFactsPaged after=%q: %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		pagesSeen++
		lastID := page[len(page)-1].ID
		if after != "" && lastID == after {
			t.Fatalf("ListAllFactsPaged cursor did not advance past %q", after)
		}
		for _, f := range page {
			seen = append(seen, f.ID)
		}
		after = lastID
	}
	wantPages := (factCount + pageLimit - 1) / pageLimit
	if pagesSeen < wantPages {
		t.Errorf("ListAllFactsPaged spanned %d pages, want >= %d (pageLimit=%d, factCount=%d)",
			pagesSeen, wantPages, pageLimit, factCount)
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
	var iteratedCreatedAt []time.Time
	var iteratedLastSynth []time.Time
	if err := s2.IterateEntities(ctx, func(e IndexEntity) error {
		iteratedSlugs = append(iteratedSlugs, e.Slug)
		iteratedKinds = append(iteratedKinds, e.Kind)
		iteratedAliases = append(iteratedAliases, e.Aliases)
		iteratedCreatedAt = append(iteratedCreatedAt, e.CreatedAt)
		iteratedLastSynth = append(iteratedLastSynth, e.LastSynthesizedAt)
		return nil
	}); err != nil {
		t.Fatalf("IterateEntities: %v", err)
	}
	wantSlugs := []string{"slug-00", "slug-01"}
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
	// slug-01 had no aliases — verify the iterator did not leak slug-00's
	// aliases into the next row's IndexEntity.
	if len(iteratedAliases[1]) != 0 {
		t.Errorf("IterateEntities Aliases[1] = %v, want empty (alias leak across iteration)", iteratedAliases[1])
	}
	// Time fields on slug-00 must round-trip through the TEXT/RFC3339 path.
	if !iteratedCreatedAt[0].Equal(createdAt) {
		t.Errorf("IterateEntities CreatedAt[0] = %v, want %v", iteratedCreatedAt[0], createdAt)
	}
	if !iteratedLastSynth[0].Equal(lastSynthAt) {
		t.Errorf("IterateEntities LastSynthesizedAt[0] = %v, want %v", iteratedLastSynth[0], lastSynthAt)
	}

	// Edge round-trip across reopen. Sibling tests cover ListEdgesForEntity
	// in-process; this is the only after-reopen check for that path.
	edges, err := s2.ListEdgesForEntity(ctx, "slug-00")
	if err != nil {
		t.Fatalf("ListEdgesForEntity: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("ListEdgesForEntity len = %d, want 1 (got=%+v)", len(edges), edges)
	}
	if edges[0].Subject != "slug-00" || edges[0].Predicate != "knows" || edges[0].Object != "slug-01" {
		t.Errorf("ListEdgesForEntity edge = %+v, want {slug-00 knows slug-01}", edges[0])
	}
	if edges[0].SourceSHA != "abc123" {
		t.Errorf("ListEdgesForEntity SourceSHA = %q, want abc123", edges[0].SourceSHA)
	}

	// Redirect must survive close/reopen.
	to, ok, err := s2.ResolveRedirect(ctx, "old-slug-00")
	if err != nil {
		t.Fatalf("ResolveRedirect: %v", err)
	}
	if !ok || to != "slug-00" {
		t.Errorf("ResolveRedirect after reopen = (%q, %v), want (slug-00, true)", to, ok)
	}

	// CanonicalHashAll must be byte-identical across close/reopen. With
	// non-zero MergedAt / CreatedAt / LastSynthesizedAt above, this also
	// catches drift in the redirect + entity time-decode paths (zero values
	// would round-trip as NULL → skipped time.Parse on both sides, leaving
	// the assertion meaningful only for the fact columns sampleFact populates).
	hashAfter, err := s2.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll after reopen: %v", err)
	}
	if hashBefore != hashAfter {
		t.Errorf("CanonicalHashAll drift across close/reopen:\n  before = %s\n  after  = %s",
			hashBefore, hashAfter)
	}
}
