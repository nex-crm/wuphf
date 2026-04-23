package team

// wiki_query_retrieve_test.go — covers the class-aware WikiIndex.Search path.
//
// The bench (cmd/bench-slice-1) exercises the happy-path already; these
// tests lock in the invariants:
//   - multi_hop retrieval unions the typed walk with BM25 and caps at topK.
//   - when the company/project display doesn't slug-match anything in the
//     store, the BM25 fallback still returns hits (no empty result).
//   - counterfactual retrieval surfaces the subject's latest role_at fact
//     even when BM25 would out-rank it with trigger-word noise.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// newBenchLikeIndex returns a WikiIndex backed by SQLiteFactStore + the real
// BleveTextIndex so tests get tokenised BM25 matching (not the substring
// fallback in newInMemoryTextIndex).
func newBenchLikeIndex(t *testing.T) *WikiIndex {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSQLiteFactStore(filepath.Join(dir, "wiki.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteFactStore: %v", err)
	}
	text, err := NewBleveTextIndex(filepath.Join(dir, "bleve"))
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewBleveTextIndex: %v", err)
	}
	idx := NewWikiIndex(dir, WithFactStore(store), WithTextIndex(text))
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// seedRetrieveFact stores + indexes a pre-built TypedFact into the bench-like
// WikiIndex and fails the test on any error. Previously the tests used
// `_ = ...` which silently hid setup failures and produced misleading
// assertion errors downstream. (Named distinctly from the seedFact and
// seedTypedFact helpers in this package to avoid a redeclaration clash.)
func seedRetrieveFact(t *testing.T, idx *WikiIndex, f TypedFact) {
	t.Helper()
	ctx := context.Background()
	if err := idx.store.UpsertFact(ctx, f); err != nil {
		t.Fatalf("seed UpsertFact %s: %v", f.ID, err)
	}
	if err := idx.text.Index(ctx, f); err != nil {
		t.Fatalf("seed text.Index %s: %v", f.ID, err)
	}
}

// TestRetrieveMultiHopFallsBackOnFuzzyResolution exercises the "slug resolver
// found nothing" branch. The BM25 index still has hits for the query, so the
// returned SearchHit list is non-empty — recall never falls below the BM25
// baseline even when the rewriter is wrong.
func TestRetrieveMultiHopFallsBackOnFuzzyResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := newBenchLikeIndex(t)

	// Seed a champions fact under a project slug that does NOT match any
	// candidate from displayToSlugCandidates("Orion Launch"). The BM25
	// index will still find it by text.
	f := TypedFact{
		ID:         "seed-bm25",
		EntitySlug: "alice",
		Kind:       "person",
		Type:       "relationship",
		Triplet:    &Triplet{Subject: "alice", Predicate: "champions", Object: "project:completely-unrelated-slug"},
		Text:       "Alice championed the Orion Launch initiative at some company called FakeCorp.",
		CreatedAt:  time.Now(),
		CreatedBy:  "test",
	}
	seedRetrieveFact(t, idx, f)

	// Query that looks multi_hop but whose slug candidates won't match the
	// store's actual slug for the project.
	hits, err := idx.Search(ctx, "Who at FakeCorp championed the Orion Launch project?", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("BM25 fallback produced zero hits — invariant violated")
	}
	// The seeded fact must be surfaced by BM25.
	found := false
	for _, h := range hits {
		if h.FactID == "seed-bm25" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected BM25 to surface seed-bm25; got %+v", hits)
	}
}

// TestRetrieveMultiHop_TypedWalkUnionsWithBM25 verifies that the typed walk
// pulls in the role_at fact BM25 would miss.
func TestRetrieveMultiHop_TypedWalkUnionsWithBM25(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := newBenchLikeIndex(t)

	// Champions fact — BM25 will match this on "q2 pilot" + "championed".
	champFact := TypedFact{
		ID:         "fact-champ",
		EntitySlug: "bob",
		Kind:       "person",
		Type:       "relationship",
		Triplet:    &Triplet{Subject: "bob", Predicate: "champions", Object: "project:q2-pilot"},
		Text:       "Bob championed the Q2 Pilot Program end-to-end.",
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:  "test",
	}
	// Role_at fact — BM25 will NOT match this on the multi_hop query because
	// the text doesn't mention Q2 Pilot.
	roleFact := TypedFact{
		ID:         "fact-role",
		EntitySlug: "bob",
		Kind:       "person",
		Type:       "status",
		Triplet:    &Triplet{Subject: "bob", Predicate: "role_at", Object: "company:blueshift"},
		Text:       "Bob is now Director of Product at Blueshift.",
		CreatedAt:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:  "test",
	}
	for _, f := range []TypedFact{champFact, roleFact} {
		seedRetrieveFact(t, idx, f)
	}

	hits, err := idx.Search(ctx, "Who at Blueshift championed the Q2 Pilot Program project?", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sawChamp, sawRole bool
	for _, h := range hits {
		if h.FactID == "fact-champ" {
			sawChamp = true
		}
		if h.FactID == "fact-role" {
			sawRole = true
		}
	}
	if !sawChamp {
		t.Error("champions fact missing from results — typed walk or BM25 regression")
	}
	if !sawRole {
		t.Error("role_at fact missing from results — typed walk did not union")
	}
}

// TestRetrieveCounterfactual_LatestRoleAtSurfaces verifies that the
// counterfactual path surfaces the subject's latest role_at fact even when
// BM25 would rank noise above it.
func TestRetrieveCounterfactual_LatestRoleAtSurfaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := newBenchLikeIndex(t)

	// Role_at for ivan-petrov. Text deliberately doesn't contain the
	// counterfactual trigger words so BM25 won't out-rank it.
	roleFact := TypedFact{
		ID:         "ivan-role",
		EntitySlug: "ivan-petrov",
		Kind:       "person",
		Type:       "status",
		Triplet:    &Triplet{Subject: "ivan-petrov", Predicate: "role_at", Object: "company:blueshift"},
		Text:       "Ivan Petrov leads Growth at Blueshift.",
		CreatedAt:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:  "test",
	}
	seedRetrieveFact(t, idx, roleFact)

	// Noise fact that BM25 will score above the role fact because it
	// contains the query verbatim.
	noise := TypedFact{
		ID:         "noise",
		EntitySlug: "other",
		Kind:       "person",
		Type:       "observation",
		Text:       "What would have happened if we had not shipped the role feature on time?",
		CreatedAt:  time.Now(),
		CreatedBy:  "test",
	}
	seedRetrieveFact(t, idx, noise)

	hits, err := idx.Search(ctx, "What would have happened if Ivan Petrov had not taken her current role?", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sawRole bool
	for _, h := range hits {
		if h.FactID == "ivan-role" {
			sawRole = true
			break
		}
	}
	if !sawRole {
		t.Errorf("counterfactual retrieval missed ivan-role; hits=%+v", hits)
	}
}

// TestFactToHit_TruncatesOnRuneBoundary verifies the snippet truncation slices
// on rune boundaries, not byte boundaries. Byte-slicing a multi-byte UTF-8
// character produces a replacement character (U+FFFD) downstream, which leaks
// all the way to UI/API consumers. Regression guard for the 300-char cap.
func TestFactToHit_TruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()

	// Japanese character "あ" is 3 bytes, so 400 of them is 1200 bytes — well
	// past the 300-rune cap. If the truncation sliced on bytes, it would land
	// mid-rune and produce U+FFFD.
	text := strings.Repeat("あ", 400)
	f := TypedFact{
		ID:         "utf8-trunc",
		EntitySlug: "test",
		Text:       text,
		CreatedAt:  time.Now(),
		CreatedBy:  "test",
	}
	hit := factToHit(f)

	if !utf8.ValidString(hit.Snippet) {
		t.Fatal("snippet is not valid UTF-8 — truncation sliced mid-rune")
	}
	if got := utf8.RuneCountInString(hit.Snippet); got != 300 {
		t.Errorf("snippet rune count = %d, want 300", got)
	}
	if strings.ContainsRune(hit.Snippet, '�') {
		t.Error("snippet contains U+FFFD replacement character")
	}

	// Sanity: shorter strings are untouched.
	short := TypedFact{ID: "short", Text: "hello", CreatedAt: time.Now()}
	if got := factToHit(short).Snippet; got != "hello" {
		t.Errorf("short snippet mutated: %q", got)
	}
}

// TestRetrieveStatusStillUsesBM25 confirms that non-multi_hop, non-
// counterfactual queries don't accidentally engage the typed walk.
// Regression guard for the "never replace BM25" invariant.
func TestRetrieveStatusStillUsesBM25(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := newBenchLikeIndex(t)

	roleFact := TypedFact{
		ID:         "sarah-role",
		EntitySlug: "sarah-jones",
		Kind:       "person",
		Type:       "status",
		Triplet:    &Triplet{Subject: "sarah-jones", Predicate: "role_at", Object: "company:acme-corp"},
		Text:       "Sarah Jones is VP of Sales at Acme Corp.",
		CreatedAt:  time.Now(),
		CreatedBy:  "test",
	}
	seedRetrieveFact(t, idx, roleFact)

	hits, err := idx.Search(ctx, "What does Sarah Jones do?", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("status query returned zero hits — BM25 path broken")
	}
	if hits[0].FactID != "sarah-role" {
		t.Errorf("expected sarah-role first, got %+v", hits)
	}
}
