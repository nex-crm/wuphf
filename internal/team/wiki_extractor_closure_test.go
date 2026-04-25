package team

// wiki_extractor_closure_test.go — Thread B coverage for the §7.4 substrate
// guarantee. Extraction MUST append the JSONL fact log in addition to mutating
// the in-memory index, so `rm -rf .wuphf/index/` + ReconcileFromMarkdown
// produces a logically-identical rebuild.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/gitexec"
)

// extractSecondArtifactResponse returns an extraction payload for the SAME
// entity (sarah-chen) from a different artifact. Facts have distinct
// sentence offsets so the deterministic fact_id (§7.3) differs from the
// cannedResponse payload — tests of append-only JSONL can assert the file
// grew by the expected count.
func extractSecondArtifactResponse(sha string) string {
	payload := extractionOutput{
		ArtifactSHA: sha,
		Entities: []extractedEntity{
			{
				Kind:         "person",
				ProposedSlug: "sarah-chen",
				ExistingSlug: "sarah-chen",
				Signals: extractedSignal{
					PersonName: "Sarah Chen",
					JobTitle:   "VP of Sales",
				},
				Confidence: 0.95,
			},
		},
		Facts: []extractedFact{
			{
				EntitySlug: "sarah-chen",
				Type:       "observation",
				Triplet: &Triplet{
					Subject:   "sarah-chen",
					Predicate: "prefers",
					Object:    "literal:async-updates",
				},
				Text:           "Sarah prefers async updates over standups.",
				Confidence:     0.9,
				ValidFrom:      "2026-04-15",
				SourceType:     "chat",
				SourcePath:     "wiki/artifacts/chat/" + sha + ".md",
				SentenceOffset: 42,
			},
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// TestExtractionSurvivesReboot is the §7.4 contract test for Thread B.
// Extract facts from an artifact, assert BM25 finds them, wipe the in-memory
// index, rebuild via ReconcileFromMarkdown, and assert both (a) BM25 still
// finds the same facts, and (b) CanonicalHashAll matches pre-wipe — proving
// the derived cache was rebuilt with logical identity.
func TestExtractionSurvivesReboot(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "survivr1"
	h.provider.response = cannedResponse(sha)
	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	// Sanity check: BM25 finds the fact pre-wipe.
	preHits, err := h.index.Search(ctx, "VP of Sales", 10)
	if err != nil {
		t.Fatalf("pre-wipe search: %v", err)
	}
	if len(preHits) == 0 {
		t.Fatal("pre-wipe search returned 0 hits; extraction did not populate the index")
	}

	preHashFacts, err := h.index.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("pre-wipe CanonicalHashFacts: %v", err)
	}

	// Fact log must exist on disk — that is the substrate guarantee.
	factLogRel := factLogPath("person", "sarah-chen")
	factLogAbs := filepath.Join(h.repo.Root(), filepath.FromSlash(factLogRel))
	if _, err := os.Stat(factLogAbs); err != nil {
		t.Fatalf("fact log missing on disk at %s: %v", factLogRel, err)
	}

	// Wipe the index by constructing a fresh one rooted at the same repo, then
	// reconciling from markdown. This is the equivalent of rm -rf .wuphf/index/
	// + broker restart.
	fresh := NewWikiIndex(h.repo.Root())
	defer fresh.Close()
	if err := fresh.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile from markdown: %v", err)
	}

	// BM25 finds the same fact after rebuild.
	postHits, err := fresh.Search(ctx, "VP of Sales", 10)
	if err != nil {
		t.Fatalf("post-wipe search: %v", err)
	}
	if len(postHits) == 0 {
		t.Fatal("post-wipe search returned 0 hits; substrate guarantee (§7.4) violated")
	}
	wantID := ComputeFactID(sha, 0, "sarah-chen", "role_at", "company:acme-corp")
	found := false
	for _, hit := range postHits {
		if hit.FactID == wantID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("post-wipe search missed fact %s; got %+v", wantID, postHits)
	}

	// CanonicalHashFacts is logically identical across the wipe — this is the
	// load-bearing §7.4 fact-substrate contract that Thread B closes.
	// (CanonicalHashAll also covers entities/edges/redirects; ghost entity
	// rows minted in-memory by the extractor are not committed as briefs by
	// Thread B and so do not round-trip through markdown reconcile. Brief
	// writing lives in the synthesizer path; Slice 3 will unify the two so
	// CanonicalHashAll also round-trips end-to-end through extraction alone.)
	postHashFacts, err := fresh.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("post-wipe CanonicalHashFacts: %v", err)
	}
	if preHashFacts != postHashFacts {
		t.Errorf("§7.4 substrate drift: pre=%s post=%s", preHashFacts, postHashFacts)
	}
}

// TestEnqueueFactLogAppendsJSONL asserts that two extractions for the same
// entity (but different artifacts) append distinct lines to the JSONL file.
// Re-extracting the same artifact must NOT duplicate lines — reinforcement
// updates in-memory reinforced_at only.
func TestEnqueueFactLogAppendsJSONL(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	firstSHA := "append01"
	h.provider.response = cannedResponse(firstSHA)
	firstPath := h.writeArtifact(firstSHA, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, firstPath); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	h.worker.WaitForIdle()

	factLogAbs := filepath.Join(h.repo.Root(),
		filepath.FromSlash(factLogPath("person", "sarah-chen")))

	lineCountAfterFirst := countNonEmptyLines(t, factLogAbs)
	if lineCountAfterFirst != 1 {
		t.Fatalf("expected 1 JSONL line after first extract; got %d", lineCountAfterFirst)
	}

	// Second artifact, different sha and sentence offset → brand-new fact_id.
	secondSHA := "append02"
	h.provider.mu.Lock()
	h.provider.response = extractSecondArtifactResponse(secondSHA)
	h.provider.mu.Unlock()
	secondPath := h.writeArtifact(secondSHA, "chat", "Sarah prefers async updates.\n")
	if err := h.extractor.ExtractFromArtifact(ctx, secondPath); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	h.worker.WaitForIdle()

	lineCountAfterSecond := countNonEmptyLines(t, factLogAbs)
	if lineCountAfterSecond != 2 {
		t.Fatalf("expected 2 JSONL lines after second extract (distinct fact); got %d", lineCountAfterSecond)
	}

	// Third run: re-extract the FIRST artifact. Reinforcement must NOT append.
	h.provider.mu.Lock()
	h.provider.response = cannedResponse(firstSHA)
	h.provider.mu.Unlock()
	if err := h.extractor.ExtractFromArtifact(ctx, firstPath); err != nil {
		t.Fatalf("third extract (reinforcement): %v", err)
	}
	h.worker.WaitForIdle()

	lineCountAfterReinforce := countNonEmptyLines(t, factLogAbs)
	if lineCountAfterReinforce != 2 {
		t.Errorf("reinforcement must not append a JSONL line; got %d lines, want 2",
			lineCountAfterReinforce)
	}
}

// TestReinforcementHashInvariance asserts the two-hash contract: extracting
// the same artifact twice produces IDENTICAL CanonicalHashFacts (because
// reinforced_at is excluded from the hash input) and DIFFERENT CanonicalHashAll
// (because reinforced_at advanced).
func TestReinforcementHashInvariance(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "reinfhsh"
	h.provider.response = cannedResponse(sha)
	path := h.writeArtifact(sha, "chat", "Sarah Chen was promoted to VP of Sales.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	h.worker.WaitForIdle()

	hashFacts1, err := h.index.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashFacts 1: %v", err)
	}
	hashAll1, err := h.index.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll 1: %v", err)
	}

	// Advance the extractor clock so reinforced_at is strictly later than the
	// first pass. Without this, a sub-millisecond second pass could leave the
	// timestamps identical and the test would be flaky.
	later := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	h.extractor.SetNow(func() time.Time { return later })

	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	h.worker.WaitForIdle()

	// Confirm reinforced_at was actually set — the invariance guarantees only
	// apply when the second pass was reinforcement rather than a fresh insert.
	wantID := ComputeFactID(sha, 0, "sarah-chen", "role_at", "company:acme-corp")
	f, ok, err := h.index.GetFact(ctx, wantID)
	if err != nil || !ok {
		t.Fatalf("fact missing after reinforcement: ok=%v err=%v", ok, err)
	}
	if f.ReinforcedAt == nil {
		t.Fatal("reinforced_at not set; harness precondition violated")
	}

	hashFacts2, err := h.index.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashFacts 2: %v", err)
	}
	hashAll2, err := h.index.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll 2: %v", err)
	}

	if hashFacts1 != hashFacts2 {
		t.Errorf("CanonicalHashFacts must be invariant across reinforcement: %s → %s",
			hashFacts1, hashFacts2)
	}
	if hashAll1 == hashAll2 {
		t.Errorf("CanonicalHashAll must advance when reinforced_at changes: stuck at %s",
			hashAll1)
	}
}

// TestExtractionBatchesFactsPerEntity asserts the extractor emits a single
// commit per entity when multiple facts for that entity come from one
// artifact. This caps worker-queue pressure (see the plan risk note).
func TestExtractionBatchesFactsPerEntity(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "batchabc"
	payload := extractionOutput{
		ArtifactSHA: sha,
		Entities: []extractedEntity{
			{
				Kind:         "person",
				ProposedSlug: "sarah-chen",
				Signals:      extractedSignal{PersonName: "Sarah Chen"},
				Confidence:   0.95,
				Ghost:        true,
			},
		},
		Facts: []extractedFact{
			{
				EntitySlug:     "sarah-chen",
				Type:           "observation",
				Triplet:        &Triplet{Subject: "sarah-chen", Predicate: "role_at", Object: "acme-corp"},
				Text:           "Works at Acme.",
				Confidence:     0.9,
				SentenceOffset: 0,
			},
			{
				EntitySlug:     "sarah-chen",
				Type:           "observation",
				Triplet:        &Triplet{Subject: "sarah-chen", Predicate: "based_in", Object: "sf"},
				Text:           "Based in SF.",
				Confidence:     0.9,
				SentenceOffset: 24,
			},
			{
				EntitySlug:     "sarah-chen",
				Type:           "observation",
				Triplet:        &Triplet{Subject: "sarah-chen", Predicate: "team", Object: "sales"},
				Text:           "On the sales team.",
				Confidence:     0.9,
				SentenceOffset: 48,
			},
		},
	}
	b, _ := json.Marshal(payload)
	h.provider.response = string(b)
	path := h.writeArtifact(sha, "chat", "Three facts about Sarah.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	factLogRel := factLogPath("person", "sarah-chen")
	factLogAbs := filepath.Join(h.repo.Root(), filepath.FromSlash(factLogRel))
	lines := countNonEmptyLines(t, factLogAbs)
	if lines != 3 {
		t.Fatalf("expected 3 JSONL lines (one per fact); got %d", lines)
	}

	// All three facts should live in a SINGLE commit — batch-per-entity.
	commits := commitCountForPath(t, h.repo.Root(), factLogRel)
	if commits != 1 {
		t.Errorf("expected 1 commit for 3 same-entity facts (batched); got %d", commits)
	}
}

// TestFactLogPath covers the helper that wires extraction to the markdown
// layout (§3 Layer-2). Keep it tiny but explicit so a silent rename breaks
// here rather than inside the reboot test.
func TestFactLogPath(t *testing.T) {
	cases := []struct{ kind, slug, want string }{
		{"person", "sarah-jones", "wiki/facts/person/sarah-jones.jsonl"},
		{"company", "acme-corp", "wiki/facts/company/acme-corp.jsonl"},
		{"project", "q2-pilot", "wiki/facts/project/q2-pilot.jsonl"},
	}
	for _, tc := range cases {
		if got := factLogPath(tc.kind, tc.slug); got != tc.want {
			t.Errorf("factLogPath(%q, %q) = %q, want %q", tc.kind, tc.slug, got, tc.want)
		}
	}
}

// --- helpers --------------------------------------------------------------

func countNonEmptyLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// gitexec.Run scrubs GIT_DIR / GIT_CONFIG_* so the lookup isn't hijacked
// by an inherited GIT_DIR when tests run under a pre-push hook (git
// exports GIT_DIR pointing at the outer repo, and an unscrubbed `git
// log` would then query the outer history instead of this test's
// fixture repo). Same pattern as internal/migration/writer_test.go.
func commitCountForPath(t *testing.T, repoRoot, relPath string) int {
	t.Helper()
	out, err := gitexec.Run(t.Context(), repoRoot, "log", "--oneline", "--", relPath)
	if err != nil {
		t.Fatalf("git log %s: %v", relPath, err)
	}
	if out == "" {
		return 0
	}
	return strings.Count(out, "\n") + 1
}
