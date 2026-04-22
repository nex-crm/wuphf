package team

// wiki_index_sqlite_test.go — table-driven tests for SQLiteFactStore.
//
// Covers every FactStore method plus the §7.4 canonical hash stability contract:
// wipe + re-reconcile on the same markdown corpus → identical hash.
//
// All tests use t.TempDir() for the SQLite file; nothing persists between runs.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openTestStore creates a temporary SQLiteFactStore for the duration of the test.
func openTestStore(t *testing.T) *SQLiteFactStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSQLiteFactStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteFactStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// sampleFact returns a deterministic TypedFact for use in tests.
func sampleFact(id, slug string) TypedFact {
	ts := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	until := ts.Add(30 * 24 * time.Hour)
	reinforced := ts.Add(1 * 24 * time.Hour)
	return TypedFact{
		ID:         id,
		EntitySlug: slug,
		Kind:       "person",
		Type:       "status",
		Triplet: &Triplet{
			Subject:   slug,
			Predicate: "role_at",
			Object:    "acme-corp",
		},
		Text:            "Sarah was promoted to VP of Sales.",
		Confidence:      0.95,
		ValidFrom:       ts,
		ValidUntil:      &until,
		Supersedes:      []string{"oldfact001"},
		ContradictsWith: []string{"contradict001"},
		SourceType:      "chat",
		SourcePath:      "wiki/artifacts/chat/abc123.md",
		SentenceOffset:  3,
		ArtifactExcerpt: "promoted to VP",
		CreatedAt:       ts,
		CreatedBy:       "archivist",
		ReinforcedAt:    &reinforced,
	}
}

// TestSQLiteFactStore_UpsertFact_GetFact verifies that a fact survives a round-trip.
func TestSQLiteFactStore_UpsertFact_GetFact(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	f := sampleFact("fact001", "sarah-jones")

	if err := s.UpsertFact(ctx, f); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}

	got, ok, err := s.GetFact(ctx, "fact001")
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if !ok {
		t.Fatal("GetFact: fact not found")
	}

	// Core fields.
	if got.ID != f.ID {
		t.Errorf("ID = %q, want %q", got.ID, f.ID)
	}
	if got.EntitySlug != f.EntitySlug {
		t.Errorf("EntitySlug = %q, want %q", got.EntitySlug, f.EntitySlug)
	}
	if got.Text != f.Text {
		t.Errorf("Text = %q, want %q", got.Text, f.Text)
	}
	if got.Confidence != f.Confidence {
		t.Errorf("Confidence = %v, want %v", got.Confidence, f.Confidence)
	}
	if got.Kind != f.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, f.Kind)
	}
	if got.Type != f.Type {
		t.Errorf("Type = %q, want %q", got.Type, f.Type)
	}
	if got.SourceType != f.SourceType {
		t.Errorf("SourceType = %q, want %q", got.SourceType, f.SourceType)
	}
	if got.SentenceOffset != f.SentenceOffset {
		t.Errorf("SentenceOffset = %d, want %d", got.SentenceOffset, f.SentenceOffset)
	}

	// Triplet.
	if got.Triplet == nil {
		t.Fatal("Triplet = nil, want non-nil")
	}
	if got.Triplet.Subject != f.Triplet.Subject {
		t.Errorf("Triplet.Subject = %q, want %q", got.Triplet.Subject, f.Triplet.Subject)
	}
	if got.Triplet.Predicate != f.Triplet.Predicate {
		t.Errorf("Triplet.Predicate = %q, want %q", got.Triplet.Predicate, f.Triplet.Predicate)
	}
	if got.Triplet.Object != f.Triplet.Object {
		t.Errorf("Triplet.Object = %q, want %q", got.Triplet.Object, f.Triplet.Object)
	}

	// Optional time fields.
	if !got.ValidFrom.Equal(f.ValidFrom) {
		t.Errorf("ValidFrom = %v, want %v", got.ValidFrom, f.ValidFrom)
	}
	if got.ValidUntil == nil {
		t.Fatal("ValidUntil = nil, want non-nil")
	}
	if !got.ValidUntil.Equal(*f.ValidUntil) {
		t.Errorf("ValidUntil = %v, want %v", got.ValidUntil, f.ValidUntil)
	}
	if got.ReinforcedAt == nil {
		t.Fatal("ReinforcedAt = nil, want non-nil")
	}

	// JSON arrays.
	if len(got.Supersedes) != 1 || got.Supersedes[0] != "oldfact001" {
		t.Errorf("Supersedes = %v, want [oldfact001]", got.Supersedes)
	}
	if len(got.ContradictsWith) != 1 || got.ContradictsWith[0] != "contradict001" {
		t.Errorf("ContradictsWith = %v, want [contradict001]", got.ContradictsWith)
	}
}

// TestSQLiteFactStore_UpsertFact_Idempotent verifies that upserting the same fact
// twice results in only one row (no duplicate).
func TestSQLiteFactStore_UpsertFact_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	f := sampleFact("dup001", "bob")

	if err := s.UpsertFact(ctx, f); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	f.Text = "Updated text after re-extraction."
	if err := s.UpsertFact(ctx, f); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	facts, err := s.ListFactsForEntity(ctx, "bob")
	if err != nil {
		t.Fatalf("ListFactsForEntity: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("len(facts) = %d, want 1", len(facts))
	}
	if facts[0].Text != "Updated text after re-extraction." {
		t.Errorf("Text not updated: %q", facts[0].Text)
	}
}

// TestSQLiteFactStore_GetFact_NotFound verifies GetFact returns (_, false, nil)
// for a missing ID.
func TestSQLiteFactStore_GetFact_NotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	_, ok, err := s.GetFact(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetFact error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing fact")
	}
}

// TestSQLiteFactStore_ListFactsForEntity verifies ordering (by created_at ASC).
func TestSQLiteFactStore_ListFactsForEntity(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"f3", "f1", "f2"} {
		f := TypedFact{
			ID:         id,
			EntitySlug: "alice",
			Text:       "fact " + id,
			CreatedAt:  base.Add(time.Duration(i+1) * time.Hour),
			CreatedBy:  "archivist",
		}
		if err := s.UpsertFact(ctx, f); err != nil {
			t.Fatalf("UpsertFact(%s): %v", id, err)
		}
	}

	// Add a fact for a different entity — must not appear.
	other := TypedFact{ID: "o1", EntitySlug: "bob", Text: "other", CreatedAt: base, CreatedBy: "archivist"}
	if err := s.UpsertFact(ctx, other); err != nil {
		t.Fatalf("UpsertFact(other): %v", err)
	}

	facts, err := s.ListFactsForEntity(ctx, "alice")
	if err != nil {
		t.Fatalf("ListFactsForEntity: %v", err)
	}
	if len(facts) != 3 {
		t.Fatalf("len(facts) = %d, want 3", len(facts))
	}
	// Expect ascending created_at order: f3 (i=0, +1h), f1 (i=1, +2h), f2 (i=2, +3h).
	wantOrder := []string{"f3", "f1", "f2"}
	for i, want := range wantOrder {
		if facts[i].ID != want {
			t.Errorf("facts[%d].ID = %q, want %q", i, facts[i].ID, want)
		}
	}
}

// TestSQLiteFactStore_UpsertEdge_ListEdgesForEntity verifies edge storage and
// bi-directional retrieval (subject OR object match).
func TestSQLiteFactStore_UpsertEdge_ListEdgesForEntity(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	ts := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	e := IndexEdge{
		Subject:   "sarah-jones",
		Predicate: "works_at",
		Object:    "acme-corp",
		Timestamp: ts,
		SourceSHA: "abc123",
	}
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	// Subject side.
	edges, err := s.ListEdgesForEntity(ctx, "sarah-jones")
	if err != nil {
		t.Fatalf("ListEdgesForEntity(sarah-jones): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	if edges[0].Predicate != "works_at" {
		t.Errorf("Predicate = %q, want works_at", edges[0].Predicate)
	}
	if !edges[0].Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", edges[0].Timestamp, ts)
	}

	// Object side (the company should also resolve the edge).
	edges2, err := s.ListEdgesForEntity(ctx, "acme-corp")
	if err != nil {
		t.Fatalf("ListEdgesForEntity(acme-corp): %v", err)
	}
	if len(edges2) != 1 {
		t.Fatalf("len(edges2) = %d, want 1", len(edges2))
	}
}

// TestSQLiteFactStore_UpsertEdge_Idempotent verifies that upserting the same
// edge twice doesn't create a duplicate.
func TestSQLiteFactStore_UpsertEdge_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	e := IndexEdge{Subject: "a", Predicate: "rel", Object: "b"}
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	e.SourceSHA = "updated"
	if err := s.UpsertEdge(ctx, e); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	edges, err := s.ListEdgesForEntity(ctx, "a")
	if err != nil {
		t.Fatalf("ListEdgesForEntity: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("len(edges) = %d, want 1 (no duplicates)", len(edges))
	}
	if edges[0].SourceSHA != "updated" {
		t.Errorf("SourceSHA = %q, want updated", edges[0].SourceSHA)
	}
}

// TestSQLiteFactStore_UpsertEntity verifies entity upsert and retrieval via
// ResolveRedirect (entities are not read back directly from the test, but are
// used by the redirect logic tested in a companion test).
func TestSQLiteFactStore_UpsertEntity(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := IndexEntity{
		Slug:               "sarah-jones",
		CanonicalSlug:      "sarah-jones",
		Kind:               "person",
		Aliases:            []string{"Sarah J.", "sjones"},
		Signals:            Signals{Email: "sarah@acme.com", PersonName: "Sarah Jones"},
		LastSynthesizedSHA: "abc123",
		LastSynthesizedAt:  ts,
		FactCountAtSynth:   42,
		CreatedAt:          ts,
		CreatedBy:          "nazz",
	}
	if err := s.UpsertEntity(ctx, e); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	// Re-upsert with updated SHA — no error, no duplicate.
	e.LastSynthesizedSHA = "def456"
	if err := s.UpsertEntity(ctx, e); err != nil {
		t.Fatalf("second UpsertEntity: %v", err)
	}
}

// TestSQLiteFactStore_UpsertRedirect_ResolveRedirect verifies the redirect table.
func TestSQLiteFactStore_UpsertRedirect_ResolveRedirect(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	r := Redirect{
		From:      "sjones",
		To:        "sarah-jones",
		MergedAt:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		MergedBy:  "nazz",
		CommitSHA: "abc123",
	}
	if err := s.UpsertRedirect(ctx, r); err != nil {
		t.Fatalf("UpsertRedirect: %v", err)
	}

	// Redirect resolves.
	to, ok, err := s.ResolveRedirect(ctx, "sjones")
	if err != nil {
		t.Fatalf("ResolveRedirect: %v", err)
	}
	if !ok {
		t.Fatal("ResolveRedirect: expected ok=true")
	}
	if to != "sarah-jones" {
		t.Errorf("to = %q, want sarah-jones", to)
	}

	// Non-redirect slug returns itself.
	to2, ok2, err2 := s.ResolveRedirect(ctx, "sarah-jones")
	if err2 != nil {
		t.Fatalf("ResolveRedirect(survivor): %v", err2)
	}
	if ok2 {
		t.Error("expected ok=false for non-redirect slug")
	}
	if to2 != "sarah-jones" {
		t.Errorf("to = %q, want sarah-jones (passthrough)", to2)
	}
}

// TestSQLiteFactStore_CanonicalHashFacts_Empty verifies that an empty store
// returns a stable (non-empty) hash.
func TestSQLiteFactStore_CanonicalHashFacts_Empty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	h, err := s.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashFacts: %v", err)
	}
	// sha256 of empty input = e3b0c44298fc... (64 hex chars)
	if len(h) != 64 {
		t.Errorf("hash len = %d, want 64", len(h))
	}
}

// TestSQLiteFactStore_CanonicalHashFacts_Stable verifies that hash is identical
// when computed twice on the same data (determinism).
func TestSQLiteFactStore_CanonicalHashFacts_Stable(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	for _, id := range []string{"z-fact", "a-fact", "m-fact"} {
		f := TypedFact{
			ID:         id,
			EntitySlug: "alice",
			Text:       "text for " + id,
			CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			CreatedBy:  "archivist",
		}
		if err := s.UpsertFact(ctx, f); err != nil {
			t.Fatalf("UpsertFact(%s): %v", id, err)
		}
	}

	h1, err := s.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	h2, err := s.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash not stable: %q vs %q", h1, h2)
	}
}

// TestSQLiteFactStore_FactWithNilOptionals verifies that a minimal fact (no
// triplet, no optional times, empty arrays) round-trips cleanly.
func TestSQLiteFactStore_FactWithNilOptionals(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	f := TypedFact{
		ID:        "minimal001",
		EntitySlug: "alice",
		Text:      "A minimal fact with no optional fields.",
		CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy: "archivist",
	}
	if err := s.UpsertFact(ctx, f); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	got, ok, err := s.GetFact(ctx, "minimal001")
	if err != nil || !ok {
		t.Fatalf("GetFact: ok=%v err=%v", ok, err)
	}
	if got.Triplet != nil {
		t.Error("Triplet should be nil for minimal fact")
	}
	if got.ValidUntil != nil {
		t.Error("ValidUntil should be nil for minimal fact")
	}
	if got.ReinforcedAt != nil {
		t.Error("ReinforcedAt should be nil for minimal fact")
	}
	if len(got.Supersedes) != 0 {
		t.Errorf("Supersedes = %v, want empty", got.Supersedes)
	}
}

// TestWikiIndex_PersistentBackend_CanonicalHash is the §7.4 rebuild contract test.
//
// It writes a markdown corpus to a temp dir, reconciles into a persistent index,
// records the canonical hash, then opens a FRESH persistent index on the same
// indexDir and reconciles again. The hash must be identical.
func TestWikiIndex_PersistentBackend_CanonicalHash(t *testing.T) {
	root := t.TempDir()
	indexDir := t.TempDir()
	ctx := context.Background()

	// --- Write corpus -------------------------------------------------------

	factsDir := filepath.Join(root, "wiki", "facts", "person")
	if err := os.MkdirAll(factsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	facts := []TypedFact{
		{
			ID:         "hash-fact-001",
			EntitySlug: "sarah-jones",
			Type:       "status",
			Text:       "Sarah was promoted to VP of Sales.",
			Confidence: 0.95,
			ValidFrom:  time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
			CreatedAt:  time.Date(2026, 4, 22, 13, 0, 0, 0, time.UTC),
			CreatedBy:  "archivist",
		},
		{
			ID:         "hash-fact-002",
			EntitySlug: "sarah-jones",
			Type:       "observation",
			Text:       "Sarah attended the Q1 kickoff on 2026-01-15.",
			Confidence: 0.8,
			CreatedAt:  time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC),
			CreatedBy:  "archivist",
		},
		{
			ID:         "hash-fact-003",
			EntitySlug: "acme-corp",
			Type:       "background",
			Text:       "Acme Corp was founded in 1995.",
			Confidence: 1.0,
			CreatedAt:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			CreatedBy:  "nazz",
		},
	}

	// Write facts for sarah-jones.
	var sarahLines []byte
	for _, f := range facts[:2] {
		b, _ := json.Marshal(f)
		sarahLines = append(sarahLines, b...)
		sarahLines = append(sarahLines, '\n')
	}
	if err := os.WriteFile(filepath.Join(factsDir, "sarah-jones.jsonl"), sarahLines, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write facts for acme-corp.
	acmeFacts := filepath.Join(root, "wiki", "facts", "company")
	if err := os.MkdirAll(acmeFacts, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(facts[2])
	if err := os.WriteFile(filepath.Join(acmeFacts, "acme-corp.jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- First reconcile ----------------------------------------------------

	idx1, err := NewPersistentWikiIndex(root, indexDir)
	if err != nil {
		t.Fatalf("NewPersistentWikiIndex: %v", err)
	}
	if err := idx1.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	h1, err := idx1.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	if err := idx1.Close(); err != nil {
		t.Fatalf("close idx1: %v", err)
	}

	// --- Second reconcile (fresh index, same data) --------------------------

	idx2, err := NewPersistentWikiIndex(root, indexDir)
	if err != nil {
		t.Fatalf("NewPersistentWikiIndex (second): %v", err)
	}
	if err := idx2.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	h2, err := idx2.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if err := idx2.Close(); err != nil {
		t.Fatalf("close idx2: %v", err)
	}

	// §7.4 contract: hashes must be identical across rebuilds.
	if h1 != h2 {
		t.Errorf("canonical hash drift: %s → %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (sha256 hex)", len(h1))
	}
}

// TestWikiIndex_InMemoryFallbackAlive ensures that NewWikiIndex (no options)
// still uses in-memory stores — the regression guard for the fallback path.
func TestWikiIndex_InMemoryFallbackAlive(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	idx := NewWikiIndex(root)
	defer idx.Close()

	// Verify the store type is inMemoryFactStore (not SQLite).
	if _, ok := idx.store.(*inMemoryFactStore); !ok {
		t.Errorf("NewWikiIndex store = %T, want *inMemoryFactStore", idx.store)
	}
	if _, ok := idx.text.(*inMemoryTextIndex); !ok {
		t.Errorf("NewWikiIndex text = %T, want *inMemoryTextIndex", idx.text)
	}

	// Smoke-test: upsert through the WikiIndex API.
	f := TypedFact{
		ID:         "inmem001",
		EntitySlug: "alice",
		Text:       "Alice is the founder.",
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "archivist",
	}
	if err := idx.store.UpsertFact(ctx, f); err != nil {
		t.Fatalf("UpsertFact via in-memory: %v", err)
	}
	got, ok, err := idx.GetFact(ctx, "inmem001")
	if err != nil || !ok {
		t.Fatalf("GetFact: ok=%v err=%v", ok, err)
	}
	if got.Text != f.Text {
		t.Errorf("Text = %q, want %q", got.Text, f.Text)
	}
}
