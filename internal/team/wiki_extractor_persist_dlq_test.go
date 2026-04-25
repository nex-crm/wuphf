package team

// wiki_extractor_persist_dlq_test.go — coverage for the fact-log-persist
// DLQ category and its dedicated replay path.
//
// The tests here target PR #254 review blockers:
//   - Critical: ErrQueueSaturated on EnqueueFactLogAppend must NOT silently
//     drop the JSONL line. The failure lands under DLQCategoryFactLogPersist
//     and ReplayDLQ retries the APPEND (not full extraction), so the fact
//     eventually reaches the on-disk substrate.
//   - High: the DLQ entry carries the new category (not provider_timeout),
//     so it lands in the right metrics bucket and backoff curve.
//   - Strengthens §7.4: after replay, a fresh index + ReconcileFromMarkdown
//     finds the fact — the substrate guarantee is closed even under queue
//     saturation.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestExtractAppendSaturationLandsInFactLogPersistDLQ asserts that when
// EnqueueFactLogAppend returns ErrQueueSaturated, the failure lands in the
// DLQ under DLQCategoryFactLogPersist (not provider_timeout) with a payload
// that carries enough state to reconstruct the append. §7.4 requires the
// JSONL line to eventually reach disk.
func TestExtractAppendSaturationLandsInFactLogPersistDLQ(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	// Poison the append seam to simulate queue saturation for exactly one
	// call. Production path uses worker.EnqueueFactLogAppend.
	h.extractor.setAppendFactLog(func(context.Context, string, string, string, string) (string, int, error) {
		return "", 0, ErrQueueSaturated
	})

	ctx := context.Background()
	fact := TypedFact{
		ID:         "sat-fact-id-01",
		EntitySlug: "sarah-chen",
		Kind:       "person",
		Type:       "observation",
		Triplet:    &Triplet{Subject: "sarah-chen", Predicate: "role_at", Object: "acme"},
		Text:       "Synthetic fact for saturation test.",
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  ArchivistAuthor,
	}
	h.extractor.persistFactLogs(ctx, "sat-artifact-sha", []TypedFact{fact})

	// A DLQ entry under DLQCategoryFactLogPersist must exist.
	ready, err := h.dlq.ReadyForReplay(ctx, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	var matching *DLQEntry
	for i := range ready {
		if ready[i].ErrorCategory == DLQCategoryFactLogPersist {
			matching = &ready[i]
			break
		}
	}
	if matching == nil {
		t.Fatalf("expected a DLQCategoryFactLogPersist entry; got %+v", ready)
	}
	if matching.FactLogAppend == nil {
		t.Fatal("DLQCategoryFactLogPersist entry missing FactLogAppend payload")
	}
	if matching.FactLogAppend.Slug != "sarah-chen" {
		t.Errorf("payload slug = %q, want sarah-chen", matching.FactLogAppend.Slug)
	}
	if matching.FactLogAppend.Kind != "person" {
		t.Errorf("payload kind = %q, want person", matching.FactLogAppend.Kind)
	}
	if matching.FactLogAppend.ArtifactSHA != "sat-artifact-sha" {
		t.Errorf("payload artifact_sha = %q, want sat-artifact-sha", matching.FactLogAppend.ArtifactSHA)
	}
	if !strings.Contains(matching.FactLogAppend.JSONLLines, `"id":"sat-fact-id-01"`) {
		t.Errorf("payload missing fact id line: %q", matching.FactLogAppend.JSONLLines)
	}
	// Explicit negative assertion: NOT provider_timeout. That is the old
	// (wrong) category — using it would put these I/O/git/queue failures
	// into the LLM-timeout metrics bucket with the LLM-timeout backoff.
	if matching.ErrorCategory == DLQCategoryProviderTimeout {
		t.Error("fact-log-persist failure must not use DLQCategoryProviderTimeout")
	}
	// The synthetic DLQ key must be the fact-log composite form so multiple
	// per-entity append failures on one artifact do not clobber each other
	// in readLatestStateLocked.
	wantSHA := FactLogAppendSHA("person", "sarah-chen", "sat-artifact-sha")
	if matching.ArtifactSHA != wantSHA {
		t.Errorf("DLQ entry sha key = %q, want %q", matching.ArtifactSHA, wantSHA)
	}
}

// TestReplayDLQRecoversFactLogAppend proves that ReplayDLQ on a
// DLQCategoryFactLogPersist entry actually writes the missing JSONL line to
// disk — closing the §7.4 substrate guarantee under queue saturation.
//
// After replay, a fresh index + ReconcileFromMarkdown finds the fact.
func TestReplayDLQRecoversFactLogAppend(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	ctx := context.Background()

	// 1. First call saturates; subsequent calls go to the real worker.
	var calls int32
	h.extractor.setAppendFactLog(func(ctx context.Context, slug, path, content, commitMsg string) (string, int, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return "", 0, ErrQueueSaturated
		}
		return h.worker.EnqueueFactLogAppend(ctx, slug, path, content, commitMsg)
	})

	fact := TypedFact{
		ID:         "recover-fact-01",
		EntitySlug: "sarah-chen",
		Kind:       "person",
		Type:       "observation",
		Triplet:    &Triplet{Subject: "sarah-chen", Predicate: "works_at", Object: "acme"},
		Text:       "Sarah works at Acme (recovery test).",
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  ArchivistAuthor,
	}
	h.extractor.persistFactLogs(ctx, "recover1", []TypedFact{fact})

	// Fact log must NOT exist on disk yet — append was rejected.
	factLogAbs := filepath.Join(h.repo.Root(),
		filepath.FromSlash(factLogPath("person", "sarah-chen")))
	if _, err := os.Stat(factLogAbs); !os.IsNotExist(err) {
		t.Fatalf("fact log unexpectedly present pre-replay: %v", err)
	}

	// 2. ReplayDLQ must take the append path and write the line. Advance
	// the extractor clock past the default backoff so the entry is eligible.
	future := time.Now().Add(1 * time.Hour)
	h.extractor.SetNow(func() time.Time { return future })

	processed, retired, err := h.extractor.ReplayDLQ(ctx)
	if err != nil {
		t.Fatalf("ReplayDLQ: %v", err)
	}
	if processed == 0 {
		t.Fatal("ReplayDLQ processed 0 entries; expected >=1")
	}
	if retired == 0 {
		t.Fatal("ReplayDLQ retired 0 entries; expected >=1")
	}
	h.worker.WaitForIdle()

	// 3. Fact log MUST now exist with the recovered line.
	data, err := os.ReadFile(factLogAbs)
	if err != nil {
		t.Fatalf("fact log missing after replay: %v", err)
	}
	if !strings.Contains(string(data), `"id":"recover-fact-01"`) {
		t.Fatalf("replayed fact-log does not contain the missing fact:\n%s", data)
	}

	// 4. Rebuild a fresh index from markdown — the fact must be searchable.
	fresh := NewWikiIndex(h.repo.Root())
	defer fresh.Close()
	if err := fresh.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if f, ok, _ := fresh.GetFact(ctx, "recover-fact-01"); !ok {
		t.Fatal("recovered fact not present in rebuilt index (§7.4 violated)")
	} else if f.Text == "" {
		t.Error("recovered fact text missing after reconcile")
	}
}

// TestReplayDLQFactLogAppendIsIdempotent asserts that a fact-log replay
// which finds the line already on disk resolves cleanly without appending
// a duplicate. Covers the case where the original append partially landed
// (or a concurrent reinforcement path ran) before the DLQ entry's backoff
// window expired.
func TestReplayDLQFactLogAppendIsIdempotent(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	ctx := context.Background()

	// First call fails (simulated saturation); subsequent calls pass through.
	var calls int32
	h.extractor.setAppendFactLog(func(ctx context.Context, slug, path, content, commitMsg string) (string, int, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return "", 0, ErrQueueSaturated
		}
		return h.worker.EnqueueFactLogAppend(ctx, slug, path, content, commitMsg)
	})

	fact := TypedFact{
		ID:         "idem-fact-01",
		EntitySlug: "sarah-chen",
		Kind:       "person",
		Type:       "observation",
		Triplet:    &Triplet{Subject: "sarah-chen", Predicate: "prefers", Object: "async"},
		Text:       "Sarah prefers async (idempotency test).",
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  ArchivistAuthor,
	}
	h.extractor.persistFactLogs(ctx, "idem1", []TypedFact{fact})

	// Pre-write the same fact line to simulate "prior append actually landed
	// on disk but the failure signal was captured first" — the DLQ row is
	// queued, so replay must be idempotent.
	line, _ := json.Marshal(fact)
	if _, _, err := h.worker.EnqueueFactLogAppend(
		ctx, ArchivistAuthor,
		factLogPath("person", "sarah-chen"),
		string(line)+"\n",
		"seed: pre-write the fact before replay",
	); err != nil {
		t.Fatalf("seed append: %v", err)
	}
	h.worker.WaitForIdle()

	factLogAbs := filepath.Join(h.repo.Root(),
		filepath.FromSlash(factLogPath("person", "sarah-chen")))
	pre := countNonEmptyLines(t, factLogAbs)

	// Replay. Same fact_id is present — replay should resolve as a no-op.
	future := time.Now().Add(1 * time.Hour)
	h.extractor.SetNow(func() time.Time { return future })
	if _, _, err := h.extractor.ReplayDLQ(ctx); err != nil {
		t.Fatalf("ReplayDLQ: %v", err)
	}
	h.worker.WaitForIdle()

	post := countNonEmptyLines(t, factLogAbs)
	if post != pre {
		t.Errorf("replay must be idempotent: lines pre=%d post=%d", pre, post)
	}
}

// TestExtractAppendSaturationDistinctEntitiesDoNotCollide asserts that the
// DLQ key for fact-log-persist failures includes (kind, slug) so two
// concurrent append failures from the SAME artifact (but different entities)
// both survive readLatestStateLocked's last-write-wins keying.
func TestExtractAppendSaturationDistinctEntitiesDoNotCollide(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	// Every append fails — both entities need their own DLQ row.
	h.extractor.setAppendFactLog(func(context.Context, string, string, string, string) (string, int, error) {
		return "", 0, ErrQueueSaturated
	})

	ctx := context.Background()
	now := time.Now().UTC()
	facts := []TypedFact{
		{
			ID:         "collide-a",
			EntitySlug: "sarah-chen",
			Kind:       "person",
			Type:       "observation",
			Triplet:    &Triplet{Subject: "sarah-chen", Predicate: "role_at", Object: "acme"},
			Text:       "Sarah at Acme.",
			CreatedAt:  now,
			CreatedBy:  ArchivistAuthor,
		},
		{
			ID:         "collide-b",
			EntitySlug: "acme-corp",
			Kind:       "company",
			Type:       "observation",
			Triplet:    &Triplet{Subject: "acme-corp", Predicate: "founded", Object: "2010"},
			Text:       "Acme founded 2010.",
			CreatedAt:  now,
			CreatedBy:  ArchivistAuthor,
		},
	}
	h.extractor.persistFactLogs(ctx, "multi-ent", facts)

	ready, err := h.dlq.ReadyForReplay(ctx, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	count := 0
	for _, e := range ready {
		if e.ErrorCategory == DLQCategoryFactLogPersist {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 fact-log-persist DLQ entries (one per entity); got %d: %+v", count, ready)
	}
}

// TestSerializeFactsAsJSONLAllGoodLines asserts every well-formed fact
// lands as a line. The partial-recovery branch is exercised via log-only
// defensive path (TypedFact has no unmarshalable fields under normal
// operation), so the unit assertion is that well-formed batches are never
// short-circuited.
func TestSerializeFactsAsJSONLAllGoodLines(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	facts := []TypedFact{
		{ID: "a", EntitySlug: "x", Text: "one", CreatedAt: now, CreatedBy: ArchivistAuthor},
		{ID: "b", EntitySlug: "x", Text: "two", CreatedAt: now, CreatedBy: ArchivistAuthor},
		{ID: "c", EntitySlug: "x", Text: "three", CreatedAt: now, CreatedBy: ArchivistAuthor},
	}
	body, err := serializeFactsAsJSONL(facts)
	if err != nil {
		t.Fatalf("serializeFactsAsJSONL: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 lines in batch body; got %d", lines)
	}
	for _, want := range []string{`"id":"a"`, `"id":"b"`, `"id":"c"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s:\n%s", want, body)
		}
	}
}

// TestReconcileFromMarkdownReadsFactLogJSONL proves the §7.4 rebuild path
// literally consumes wiki/facts/**/*.jsonl. Pre-condition: fact log exists
// on disk with a single TypedFact line. Post-condition: a brand-new
// WikiIndex (no in-memory state at all) exposes that fact after
// ReconcileFromMarkdown.
//
// This is the §7.4 contract test the PR reviewer asked for: the reboot
// test is only meaningful if reconcile actually walks the fact log files.
func TestReconcileFromMarkdownReadsFactLogJSONL(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Seed the fact log by hand — do NOT touch any worker / index path. The
	// test has to prove the rebuild reads markdown, not that extraction
	// wrote something into memory.
	fact := TypedFact{
		ID:         "reconcile-proof-1",
		EntitySlug: "sarah-chen",
		Kind:       "person",
		Type:       "observation",
		Triplet:    &Triplet{Subject: "sarah-chen", Predicate: "role_at", Object: "acme"},
		Text:       "Sarah Chen is VP of Sales.",
		Confidence: 0.92,
		CreatedAt:  time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		CreatedBy:  ArchivistAuthor,
	}
	line, err := json.Marshal(fact)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	factLogRel := factLogPath("person", "sarah-chen")
	factLogAbs := filepath.Join(root, filepath.FromSlash(factLogRel))
	if err := os.MkdirAll(filepath.Dir(factLogAbs), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(factLogAbs, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("seed fact log: %v", err)
	}

	// Fresh index, no prior state whatsoever.
	idx := NewWikiIndex(root)
	defer idx.Close()

	// Pre-assert: index is empty.
	if _, ok, _ := idx.GetFact(context.Background(), fact.ID); ok {
		t.Fatal("precondition: fact should not be in a fresh index")
	}

	// Rebuild — this is the §7.4 boot-reconcile step.
	if err := idx.ReconcileFromMarkdown(context.Background()); err != nil {
		t.Fatalf("ReconcileFromMarkdown: %v", err)
	}

	// Post-assert: the fact is findable by ID AND by BM25 search.
	got, ok, _ := idx.GetFact(context.Background(), fact.ID)
	if !ok {
		t.Fatal("fact missing after ReconcileFromMarkdown — §7.4 broken: reconcile does not read the fact log")
	}
	if got.Text != fact.Text {
		t.Errorf("reconciled fact text drift: got %q, want %q", got.Text, fact.Text)
	}

	hits, err := idx.Search(context.Background(), "VP of Sales", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, hit := range hits {
		if hit.FactID == fact.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("BM25 search missed the reconciled fact; got %+v", hits)
	}
}

// TestReconcileFactLogPreservesInMemoryReinforcement is a regression guard
// for the race that caused TestExtractIdempotentOnRepeat to flake on CI:
// after the extractor appends a fact-log line, the worker fires a
// post-commit reconcile on that JSONL path in a side goroutine. A second
// extraction that only bumps in-memory ReinforcedAt (the §7.3 reinforcement
// path) can land BEFORE the reconcile goroutine runs. Reconcile then reads
// the JSONL (which never carries reinforced_at by design — it is excluded
// from serialization so the append-only log stays immutable) and must NOT
// clobber the in-memory ReinforcedAt back to nil.
//
// Breaking this invariant silently un-reinforces every fact any time the
// index reconciles a fact-log path — the SymptomFAIL on CI was
// "expected reinforced_at to be set after second extract" but the real
// failure mode is any JSONL reconcile (boot, DLQ replay, external git sync)
// erasing reinforcement.
func TestReconcileFactLogPreservesInMemoryReinforcement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Seed JSONL on disk WITHOUT reinforced_at — this mirrors how the
	// extractor writes new facts: ReinforcedAt is nil on first emission and
	// subsequent reinforcements are intentionally NOT appended.
	fact := TypedFact{
		ID:         "reinforce-preserve-1",
		EntitySlug: "sarah-chen",
		Kind:       "person",
		Type:       "observation",
		Triplet:    &Triplet{Subject: "sarah-chen", Predicate: "role_at", Object: "acme"},
		Text:       "Sarah Chen is VP of Sales.",
		Confidence: 0.9,
		CreatedAt:  time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		CreatedBy:  ArchivistAuthor,
	}
	line, err := json.Marshal(fact)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	factLogRel := factLogPath("person", "sarah-chen")
	factLogAbs := filepath.Join(root, filepath.FromSlash(factLogRel))
	if err := os.MkdirAll(filepath.Dir(factLogAbs), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(factLogAbs, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("seed fact log: %v", err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()

	ctx := context.Background()

	// Pre-load the fact into memory AS-IF SubmitFacts had just bumped
	// reinforced_at — this is the state the extractor's §7.3 path leaves the
	// store in after a second-run reinforcement.
	reinforcedAt := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	inMem := fact
	inMem.ReinforcedAt = &reinforcedAt
	if err := idx.store.UpsertFact(ctx, inMem); err != nil {
		t.Fatalf("seed in-memory reinforced fact: %v", err)
	}

	// Now trigger the same reconcile path the post-commit side goroutine
	// runs after an IsFactLogAppend commit.
	if err := idx.ReconcilePath(ctx, factLogRel); err != nil {
		t.Fatalf("ReconcilePath: %v", err)
	}

	got, ok, _ := idx.GetFact(ctx, fact.ID)
	if !ok {
		t.Fatal("fact missing after ReconcilePath")
	}
	if got.ReinforcedAt == nil {
		t.Fatal("reconcile clobbered in-memory ReinforcedAt — §7.3 broken (JSONL must not un-reinforce memory)")
	}
	if !got.ReinforcedAt.Equal(reinforcedAt) {
		t.Errorf("ReinforcedAt drift after reconcile: got %v, want %v", got.ReinforcedAt, reinforcedAt)
	}
}
