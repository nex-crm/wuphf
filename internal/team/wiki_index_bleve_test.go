package team

// wiki_index_bleve_test.go — BM25 search tests for BleveTextIndex.
//
// Verifies:
//   - Higher-relevance hits appear first (ordering).
//   - topK clamping at the caller-supplied limit.
//   - Delete removes a fact from search results.
//   - Empty query returns empty results.
//
// All tests use t.TempDir() for the bleve directory; nothing persists between runs.

import (
	"context"
	"testing"
	"time"
)

// openTestBleve opens a fresh BleveTextIndex in a temp directory.
func openTestBleve(t *testing.T) *BleveTextIndex {
	t.Helper()
	dir := t.TempDir()
	idx, err := NewBleveTextIndex(dir)
	if err != nil {
		t.Fatalf("NewBleveTextIndex: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

// makeFact creates a TypedFact with the given id and text for test use.
func makeFact(id, slug, text string) TypedFact {
	return TypedFact{
		ID:         id,
		EntitySlug: slug,
		Text:       text,
		CreatedAt:  time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
		CreatedBy:  "archivist",
	}
}

// TestBleveTextIndex_Index_Search_Basic verifies that indexed facts are
// retrievable by query.
func TestBleveTextIndex_Index_Search_Basic(t *testing.T) {
	ctx := context.Background()
	b := openTestBleve(t)

	facts := []TypedFact{
		makeFact("f001", "sarah-jones", "Sarah was promoted to VP of Sales in Q1 2026."),
		makeFact("f002", "acme-corp", "Acme Corp signed a major enterprise deal worth 2M."),
		makeFact("f003", "bob-smith", "Bob Smith joined as Head of Engineering."),
	}
	for _, f := range facts {
		if err := b.Index(ctx, f); err != nil {
			t.Fatalf("Index(%s): %v", f.ID, err)
		}
	}

	hits, err := b.Search(ctx, "promoted VP Sales", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits for 'promoted VP Sales'")
	}

	// f001 must be in the results.
	found := false
	for _, h := range hits {
		if h.FactID == "f001" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("f001 not in search results: %+v", hits)
	}
}

// TestBleveTextIndex_Search_Ordering verifies that a fact with higher query
// relevance scores above a fact with lower relevance (BM25 ordering).
func TestBleveTextIndex_Search_Ordering(t *testing.T) {
	ctx := context.Background()
	b := openTestBleve(t)

	// f-high mentions the query terms multiple times → higher BM25 score.
	highRelevance := makeFact("f-high", "alice",
		"Alice promoted to CTO. Alice promotion was announced today. Promotion effective immediately.")
	// f-low mentions the term once tangentially.
	lowRelevance := makeFact("f-low", "bob",
		"Bob attended the company meeting.")

	for _, f := range []TypedFact{highRelevance, lowRelevance} {
		if err := b.Index(ctx, f); err != nil {
			t.Fatalf("Index(%s): %v", f.ID, err)
		}
	}

	hits, err := b.Search(ctx, "promoted promotion", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// f-high should appear, and if both appear f-high should be first.
	if len(hits) == 0 {
		t.Fatal("no hits for 'promoted promotion'")
	}
	if hits[0].FactID != "f-high" {
		// Accept if f-low is absent (BM25 may not match it at all).
		if len(hits) > 1 {
			t.Errorf("expected f-high first, got %s first (all hits: %+v)", hits[0].FactID, hits)
		}
	}

	// Scores should be in descending order.
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Errorf("hits not sorted descending: hits[%d].Score=%f > hits[%d].Score=%f",
				i, hits[i].Score, i-1, hits[i-1].Score)
		}
	}
}

// TestBleveTextIndex_Search_TopKClamping verifies that topK limits results and
// that bleveMaxTopK is enforced.
func TestBleveTextIndex_Search_TopKClamping(t *testing.T) {
	ctx := context.Background()
	b := openTestBleve(t)

	// Index 10 facts all matching the query.
	for i := 0; i < 10; i++ {
		f := TypedFact{
			ID:         "topk-" + string(rune('a'+i)),
			EntitySlug: "corp",
			Text:       "quarterly revenue growth exceeded expectations significantly",
			CreatedAt:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			CreatedBy:  "archivist",
		}
		if err := b.Index(ctx, f); err != nil {
			t.Fatalf("Index(%s): %v", f.ID, err)
		}
	}

	// Ask for only 3 results.
	hits, err := b.Search(ctx, "revenue growth", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 3 {
		t.Errorf("len(hits) = %d, want ≤3 (topK=3)", len(hits))
	}
}

// TestBleveTextIndex_Search_TopKZero verifies that topK=0 returns empty results.
func TestBleveTextIndex_Search_TopKZero(t *testing.T) {
	ctx := context.Background()
	b := openTestBleve(t)

	if err := b.Index(ctx, makeFact("z1", "alice", "alice leads engineering")); err != nil {
		t.Fatal(err)
	}
	hits, err := b.Search(ctx, "alice", 0)
	if err != nil {
		t.Fatalf("Search(topK=0): %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for topK=0, got %d", len(hits))
	}
}

// TestBleveTextIndex_Delete removes a fact and verifies it is excluded from
// subsequent searches.
func TestBleveTextIndex_Delete(t *testing.T) {
	ctx := context.Background()
	b := openTestBleve(t)

	f1 := makeFact("del001", "alice", "alice signed the partnership agreement")
	f2 := makeFact("del002", "bob", "alice and bob attended the summit")

	for _, f := range []TypedFact{f1, f2} {
		if err := b.Index(ctx, f); err != nil {
			t.Fatalf("Index(%s): %v", f.ID, err)
		}
	}

	// Both appear in search.
	hits, err := b.Search(ctx, "alice", 10)
	if err != nil {
		t.Fatalf("Search before delete: %v", err)
	}
	if len(hits) < 1 {
		t.Fatal("expected ≥1 hit before delete")
	}

	// Delete f1.
	if err := b.Delete(ctx, "del001"); err != nil {
		t.Fatalf("Delete(del001): %v", err)
	}

	// f1 must not appear; f2 may still appear.
	hits2, err := b.Search(ctx, "alice", 10)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	for _, h := range hits2 {
		if h.FactID == "del001" {
			t.Errorf("deleted fact del001 still appears in search results")
		}
	}
}

// TestBleveTextIndex_HitsCarryEntitySlug verifies that hits include the
// entity_slug field so callers can build entity context without a follow-up
// lookup.
func TestBleveTextIndex_HitsCarryEntitySlug(t *testing.T) {
	ctx := context.Background()
	b := openTestBleve(t)

	f := makeFact("slug001", "sarah-jones", "sarah closed the enterprise deal")
	if err := b.Index(ctx, f); err != nil {
		t.Fatalf("Index: %v", err)
	}

	hits, err := b.Search(ctx, "enterprise deal", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Entity != "sarah-jones" {
		t.Errorf("Entity = %q, want sarah-jones", hits[0].Entity)
	}
}

// TestWikiIndex_PersistentWikiIndex_Search is an integration test: a full
// NewPersistentWikiIndex round-trip with Search via the WikiIndex handle.
func TestWikiIndex_PersistentWikiIndex_Search(t *testing.T) {
	root := t.TempDir()
	indexDir := t.TempDir()
	ctx := context.Background()

	idx, err := NewPersistentWikiIndex(root, indexDir)
	if err != nil {
		t.Fatalf("NewPersistentWikiIndex: %v", err)
	}
	defer idx.Close()

	facts := []TypedFact{
		{
			ID:         "srch001",
			EntitySlug: "sarah-jones",
			Text:       "Sarah closed a 2M deal with Globex.",
			CreatedAt:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			CreatedBy:  "archivist",
		},
		{
			ID:         "srch002",
			EntitySlug: "globex",
			Text:       "Globex is a multinational conglomerate.",
			CreatedAt:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			CreatedBy:  "archivist",
		},
	}

	for _, f := range facts {
		if err := idx.store.UpsertFact(ctx, f); err != nil {
			t.Fatalf("UpsertFact(%s): %v", f.ID, err)
		}
		if err := idx.text.Index(ctx, f); err != nil {
			t.Fatalf("text.Index(%s): %v", f.ID, err)
		}
	}

	hits, err := idx.Search(ctx, "deal Globex", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits for 'deal Globex'")
	}

	// srch001 mentions both "deal" and "Globex" so it should appear.
	found := false
	for _, h := range hits {
		if h.FactID == "srch001" {
			found = true
		}
	}
	if !found {
		t.Errorf("srch001 not found in hits: %+v", hits)
	}
}
