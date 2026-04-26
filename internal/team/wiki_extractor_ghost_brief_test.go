package team

// wiki_extractor_ghost_brief_test.go — Slice 3 Thread B coverage. Ghost
// entities minted by the extractor MUST also land as markdown briefs on
// disk so the §7.4 substrate-rebuild round-trip closes. Tests drive the
// real extractor harness; the LLM stub emits kind="people" so the
// CommitGhostBrief path's enumerated-kind regex accepts the call.

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

// ghostBriefResponse returns an extraction payload that mints exactly one
// ghost entity (kind="people") so the brief commit's strict regex
// accepts it. Different SHA flips fact_id without affecting the entity
// row, matching production extractor behaviour.
func ghostBriefResponse(sha string) string {
	payload := extractionOutput{
		ArtifactSHA: sha,
		Entities: []extractedEntity{
			{
				Kind:         "people",
				ProposedSlug: "jane-doe",
				Signals: extractedSignal{
					PersonName: "Jane Doe",
					JobTitle:   "VP Engineering",
					Email:      "jane@example.com",
					Domain:     "example.com",
				},
				Aliases:    []string{"JD", "Jane"},
				Confidence: 0.95,
				Ghost:      true,
			},
		},
		Facts: []extractedFact{
			{
				EntitySlug: "jane-doe",
				Type:       "status",
				Triplet: &Triplet{
					Subject:   "jane-doe",
					Predicate: "role_at",
					Object:    "company:example",
				},
				Text:            "Jane Doe is VP Engineering at Example.",
				Confidence:      0.92,
				ValidFrom:       "2026-04-10",
				SourceType:      "chat",
				SourcePath:      "wiki/artifacts/chat/" + sha + ".md",
				SentenceOffset:  0,
				ArtifactExcerpt: "Jane Doe is VP Engineering at Example.",
			},
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// TestExtractorWritesGhostEntityBrief drives one extraction that mints a
// ghost entity and asserts the brief landed on disk with byte-identical
// content to what MinimalBrief would emit for the in-memory IndexEntity.
func TestExtractorWritesGhostEntityBrief(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "ghost001"
	h.provider.response = ghostBriefResponse(sha)
	path := h.writeArtifact(sha, "chat", "Jane Doe is VP Engineering at Example.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	briefRel := "team/people/jane-doe.md"
	briefAbs := filepath.Join(h.repo.Root(), filepath.FromSlash(briefRel))
	got, err := os.ReadFile(briefAbs)
	if err != nil {
		t.Fatalf("ghost brief missing on disk at %s: %v", briefRel, err)
	}

	// Reconstruct the IndexEntity the extractor synthesised so we can
	// compare byte-for-byte against MinimalBrief.
	mem := h.index.store.(*inMemoryFactStore)
	mem.mu.RLock()
	ent, ok := mem.entities["jane-doe"]
	mem.mu.RUnlock()
	if !ok {
		t.Fatal("expected ghost entity jane-doe in index")
	}
	want := MinimalBrief(ent)
	if string(got) != want {
		t.Errorf("brief on disk differs from MinimalBrief(ent)\n--- want ---\n%s\n--- got ---\n%s", want, string(got))
	}

	// Verify the file is committed (not just written): git log shows one
	// commit touching the brief path.
	commits := commitCountForPath(t, h.repo.Root(), briefRel)
	if commits < 1 {
		t.Errorf("expected at least 1 commit touching %s; got %d", briefRel, commits)
	}
}

// TestSubstrateRebuildRoundTripsGhostBriefs is the §7.4 round-trip
// contract for the entity layer. Extract → reconcile-from-disk → record
// hash → fresh index from same disk → reconcile → record hash → assert
// equality. Both indices are built by reading the on-disk substrate, so
// they must converge on the same CanonicalHashAll.
func TestSubstrateRebuildRoundTripsGhostBriefs(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "rt0wholy"
	h.provider.response = ghostBriefResponse(sha)
	path := h.writeArtifact(sha, "chat", "Jane Doe is VP Engineering at Example.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	// Sanity: ghost brief landed on disk.
	briefAbs := filepath.Join(h.repo.Root(), "team", "people", "jane-doe.md")
	if _, err := os.Stat(briefAbs); err != nil {
		t.Fatalf("ghost brief missing on disk: %v", err)
	}

	// Normalise the in-memory index to the on-disk substrate so the hash
	// reflects what reconcile would produce. Without this, runtime-only
	// fields (Signals, CreatedAt) are present in-memory but not encoded
	// in the brief frontmatter that ReconcileFromMarkdown reads —
	// natural drift, not a substrate violation.
	if err := h.index.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("normalise current index: %v", err)
	}
	hashAfterReconcile, err := h.index.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll on current: %v", err)
	}

	// Build a fresh index against the same root and reconcile from
	// disk. This is the wipe-and-rebuild path described in §7.4.
	fresh := NewWikiIndex(h.repo.Root())
	defer fresh.Close()
	if err := fresh.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("rebuild from markdown: %v", err)
	}
	hashFresh, err := fresh.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll on fresh: %v", err)
	}

	if hashAfterReconcile != hashFresh {
		t.Errorf("§7.4 substrate-rebuild drift\n  current=%s\n  fresh  =%s", hashAfterReconcile, hashFresh)
	}

	// Belt-and-braces: rebuilding the fresh index again must converge to
	// the same hash (idempotency of reconcile).
	again := NewWikiIndex(h.repo.Root())
	defer again.Close()
	if err := again.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("rebuild again: %v", err)
	}
	hashAgain, err := again.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll on again: %v", err)
	}
	if hashFresh != hashAgain {
		t.Errorf("rebuild not idempotent\n  fresh=%s\n  again=%s", hashFresh, hashAgain)
	}
}

// TestGhostBriefCommitIdempotent asserts re-extracting the same artifact
// does not create orphan brief commits. The first run mints the brief;
// the second is a no-op at the brief layer because the on-disk content
// is byte-identical.
func TestGhostBriefCommitIdempotent(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "idem0001"
	h.provider.response = ghostBriefResponse(sha)
	path := h.writeArtifact(sha, "chat", "Jane Doe is VP Engineering at Example.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	h.worker.WaitForIdle()

	briefRel := "team/people/jane-doe.md"
	commitsAfterFirst := commitCountForPath(t, h.repo.Root(), briefRel)
	if commitsAfterFirst < 1 {
		t.Fatalf("expected ≥ 1 commit on first extract; got %d", commitsAfterFirst)
	}

	headBefore := headSHA(t, h.repo.Root())

	// Second extraction with the same response — entity row is already in
	// the index (matches by name), so no fresh ghost row is added. Even if
	// the resolver somehow produced a row, the brief content is
	// byte-identical so CommitGhostBrief must early-return without
	// committing.
	h.provider.mu.Lock()
	h.provider.response = ghostBriefResponse(sha)
	h.provider.mu.Unlock()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	h.worker.WaitForIdle()

	commitsAfterSecond := commitCountForPath(t, h.repo.Root(), briefRel)
	if commitsAfterSecond != commitsAfterFirst {
		t.Errorf("re-extraction created orphan brief commit: before=%d after=%d",
			commitsAfterFirst, commitsAfterSecond)
	}

	headAfter := headSHA(t, h.repo.Root())
	if headBefore != headAfter {
		// HEAD may legitimately advance because of fact-log re-append on
		// reinforcement (already covered by closure tests); but the brief
		// path must not contribute to that advance. The commitCountForPath
		// check above is the load-bearing assertion.
		t.Logf("HEAD advanced from %s to %s (acceptable when reinforcement appends elsewhere)", headBefore, headAfter)
	}
}

// TestGhostBriefCommitWritesAcrossKindRegimes asserts the brief lands on
// disk for whatever kind the extractor mints today — the wiki has both
// singular ("person") and plural ("people") regimes coexisting and the
// brief regex must accept both so substrate-rebuild round-trip works
// regardless of which path the resolver came through. Tightening the
// regex to a single regime would silently disable Thread B in production
// where the LLM prompt emits singular kinds today.
func TestGhostBriefCommitWritesAcrossKindRegimes(t *testing.T) {
	h := newExtractHarness(t)
	defer h.teardown()

	sha := "regime01"
	payload := extractionOutput{
		ArtifactSHA: sha,
		Entities: []extractedEntity{
			{
				Kind:         "person",
				ProposedSlug: "regime-singular",
				Signals:      extractedSignal{PersonName: "Regime Singular"},
				Confidence:   0.9,
				Ghost:        true,
			},
		},
	}
	b, _ := json.Marshal(payload)
	h.provider.response = string(b)
	path := h.writeArtifact(sha, "chat", "An entity with the production singular kind.\n")

	ctx := context.Background()
	if err := h.extractor.ExtractFromArtifact(ctx, path); err != nil {
		t.Fatalf("extract: %v", err)
	}
	h.worker.WaitForIdle()

	mem := h.index.store.(*inMemoryFactStore)
	mem.mu.RLock()
	_, ok := mem.entities["regime-singular"]
	mem.mu.RUnlock()
	if !ok {
		t.Fatal("expected entity row in index")
	}

	briefAbs := filepath.Join(h.repo.Root(), "team", "person", "regime-singular.md")
	if _, err := os.Stat(briefAbs); err != nil {
		t.Fatalf("expected brief at %s; stat err = %v", briefAbs, err)
	}
}

// TestGhostBriefCommitRejectsMalformedKind asserts the regex still
// refuses kinds that violate the slug character class (uppercase,
// underscores, dot-segments) — those would let an LLM hallucination
// land arbitrary paths inside team/. The regime-permissiveness above
// only widens the kind enumeration; the character class stays tight.
func TestGhostBriefCommitRejectsMalformedKind(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo init: %v", err)
	}

	cases := []string{"PEOPLE", "people_org", "people/sub", ".dot"}
	for _, kind := range cases {
		_, _, err := repo.CommitGhostBrief(context.Background(), kind, "slug", "body", "msg")
		if err == nil {
			t.Errorf("kind %q: expected error from path regex, got nil", kind)
		}
	}
}

// TestCommitGhostBriefByteIdempotent exercises the primitive-level
// byte-equality idempotency: calling CommitGhostBrief twice with the
// exact same content must return the same SHA without a second commit.
// Distinct from TestGhostBriefCommitIdempotent which exercises the full
// extractor flow (where the resolver de-dupes the entity row earlier).
func TestCommitGhostBriefByteIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	ent := IndexEntity{
		Slug:          "byte-equal",
		CanonicalSlug: "byte-equal",
		Kind:          "people",
		Signals:       Signals{PersonName: "Byte Equal"},
		CreatedAt:     time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC),
		CreatedBy:     ArchivistAuthor,
	}
	brief := MinimalBrief(ent)

	sha1, n1, err := repo.CommitGhostBrief(ctx, ent.Kind, ent.Slug, brief, "first")
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if n1 != len(brief) {
		t.Errorf("first commit reported bytes=%d, want %d", n1, len(brief))
	}

	sha2, n2, err := repo.CommitGhostBrief(ctx, ent.Kind, ent.Slug, brief, "second")
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if sha1 != sha2 {
		t.Errorf("byte-identical re-commit advanced SHA: %s → %s", sha1, sha2)
	}
	if n2 != len(brief) {
		t.Errorf("second commit reported bytes=%d, want %d", n2, len(brief))
	}

	// Path validation: rejected slugs must surface a clear error.
	if _, _, err := repo.CommitGhostBrief(ctx, "people", "Bad-Slug", brief, "bad"); err == nil {
		t.Error("expected rejection for uppercase slug; got nil err")
	}
	if _, _, err := repo.CommitGhostBrief(ctx, "people", "-leading-dash", brief, "bad"); err == nil {
		t.Error("expected rejection for leading-dash slug; got nil err")
	}
	if _, _, err := repo.CommitGhostBrief(ctx, "people", "ok", "", "bad"); err == nil {
		t.Error("expected rejection for empty content; got nil err")
	}
}

// headSHA reads the current short HEAD via gitexec.Run, scrubbing
// inherited GIT_DIR so a host-side pre-push hook cannot hijack the
// lookup. Mirrors commitCountForPath in the closure tests.
func headSHA(t *testing.T, repoRoot string) string {
	t.Helper()
	out, err := gitexec.Run(t.Context(), repoRoot, "rev-parse", "--short", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(out)
}
