package team

// wiki_compile_interlink_test.go exhaustively covers the deterministic
// wrapping rules: whole-word case-insensitive first-occurrence linking, no
// double-wrap, and skipping citations, headings, and code fences.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkTargetsFrom builds the compiled target set from (slug,title) pairs for
// direct linkifyBody tests.
func linkTargetsFrom(pairs ...[2]string) []linkTarget {
	refs := make([]compiledPageRef, 0, len(pairs))
	for _, p := range pairs {
		refs = append(refs, compiledPageRef{Slug: p[0], Title: p[1]})
	}
	return buildLinkTargets(refs)
}

func TestLinkifyBody_WrapsFirstOccurrenceOnly(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"}, [2]string{"self", "Self"})
	body := "Brex is great. We love Brex. Brex again."
	got := linkifyBody(body, targets, "self")
	want := "[[brex|Brex]] is great. We love Brex. Brex again."
	if got != want {
		t.Fatalf("first-occurrence linking failed:\n got: %q\nwant: %q", got, want)
	}
	// Exactly one wikilink emitted.
	if strings.Count(got, "[[brex|") != 1 {
		t.Fatalf("expected exactly one link, got %q", got)
	}
}

func TestLinkifyBody_SkipsSelf(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"})
	body := "Brex talks about Brex."
	if got := linkifyBody(body, targets, "brex"); got != body {
		t.Fatalf("self-link should be skipped, got %q", got)
	}
}

func TestLinkifyBody_CaseInsensitiveWholeWord(t *testing.T) {
	targets := linkTargetsFrom([2]string{"rrf", "RRF"}, [2]string{"x", "X"})
	// "rrf" lowercase matches case-insensitively; "RRFusion" must NOT match
	// (not a whole word).
	body := "We use rrf here. RRFusion is unrelated."
	got := linkifyBody(body, targets, "x")
	if !strings.Contains(got, "[[rrf|RRF]] here") {
		t.Fatalf("case-insensitive whole-word match failed: %q", got)
	}
	if strings.Contains(got, "[[rrf|RRF]]usion") {
		t.Fatalf("matched inside a larger word: %q", got)
	}
}

func TestLinkifyBody_NeverDoubleWrap(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"}, [2]string{"x", "X"})
	// Already-linked Brex must not be wrapped again; the bare "Brex" later
	// must not be touched because the existing link counts as the page's
	// reference... actually it sits inside [[...]] so it is protected, and the
	// FIRST unprotected occurrence is the later one. Assert no nesting occurs.
	body := "See [[brex|Brex]] and Brex."
	got := linkifyBody(body, targets, "x")
	// No nested brackets like [[brex|[[...
	if strings.Contains(got, "[[[[") || strings.Contains(got, "|[[") {
		t.Fatalf("double-wrap produced nested links: %q", got)
	}
	// The pre-existing link is intact.
	if !strings.Contains(got, "[[brex|Brex]]") {
		t.Fatalf("pre-existing link mangled: %q", got)
	}
}

func TestLinkifyBody_SkipsCitationMarkers(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"}, [2]string{"x", "X"})
	// "Brex" appears only inside a citation marker — must not be linked.
	body := "Some claim. ^[Brex]"
	if got := linkifyBody(body, targets, "x"); got != body {
		t.Fatalf("citation marker should be protected, got %q", got)
	}
}

func TestLinkifyBody_SkipsHeadings(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"}, [2]string{"x", "X"})
	body := "## Brex overview\n\nBrex is the anchor."
	got := linkifyBody(body, targets, "x")
	// Heading line untouched.
	if !strings.HasPrefix(got, "## Brex overview\n") {
		t.Fatalf("heading should be untouched: %q", got)
	}
	// Body occurrence linked.
	if !strings.Contains(got, "[[brex|Brex]] is the anchor.") {
		t.Fatalf("body occurrence should be linked: %q", got)
	}
}

func TestLinkifyBody_SkipsCodeFences(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"}, [2]string{"x", "X"})
	body := "```\nBrex code sample\n```\n\nBrex prose."
	got := linkifyBody(body, targets, "x")
	if !strings.Contains(got, "```\nBrex code sample\n```") {
		t.Fatalf("code fence content should be untouched: %q", got)
	}
	if !strings.Contains(got, "[[brex|Brex]] prose.") {
		t.Fatalf("prose after fence should be linked: %q", got)
	}
}

func TestLinkifyBody_LongerTitleWins(t *testing.T) {
	// "Reciprocal Rank Fusion" must win over "Rank Fusion" when both are
	// candidates against the same text.
	targets := linkTargetsFrom(
		[2]string{"reciprocal-rank-fusion", "Reciprocal Rank Fusion"},
		[2]string{"rank-fusion", "Rank Fusion"},
		[2]string{"x", "X"},
	)
	body := "We adopted Reciprocal Rank Fusion last quarter."
	got := linkifyBody(body, targets, "x")
	if !strings.Contains(got, "[[reciprocal-rank-fusion|Reciprocal Rank Fusion]]") {
		t.Fatalf("longer title should win: %q", got)
	}
	if strings.Contains(got, "[[rank-fusion|") {
		t.Fatalf("shorter contained title should not also match: %q", got)
	}
}

func TestLinkifyBody_Idempotent(t *testing.T) {
	targets := linkTargetsFrom([2]string{"brex", "Brex"}, [2]string{"x", "X"})
	body := "Brex is the anchor account."
	once := linkifyBody(body, targets, "x")
	twice := linkifyBody(once, targets, "x")
	if once != twice {
		t.Fatalf("linkify not idempotent:\n once: %q\ntwice: %q", once, twice)
	}
}

// --- end-to-end interlink through the worker ---------------------------------

func TestInterlinkPages_RewritesAndIsIdempotent(t *testing.T) {
	worker, repo, teardown := newStartedCompileWorker(t)
	defer teardown()
	ctx := context.Background()

	// Seed two compiled pages directly so interlink has files to read.
	rrfPath := "team/concepts/reciprocal-rank-fusion.md"
	brexPath := "team/entities/brex.md"
	mustWritePage(t, worker, rrfPath, "# Reciprocal Rank Fusion\n\nRRF is used by Brex. ^[s1]")
	mustWritePage(t, worker, brexPath, "# Brex\n\nBrex evaluated Reciprocal Rank Fusion. ^[s2]")

	pages := []compiledPageRef{
		{Slug: "reciprocal-rank-fusion", Kind: "concept", Title: "Reciprocal Rank Fusion", RelPath: rrfPath},
		{Slug: "brex", Kind: "entity", Title: "Brex", RelPath: brexPath},
	}

	linked, errs := interlinkPages(ctx, worker, pages)
	worker.WaitForIdle()
	if len(errs) != 0 {
		t.Fatalf("interlink errors: %v", errs)
	}
	if linked != 2 {
		t.Fatalf("expected both pages rewritten, got linked=%d", linked)
	}

	rrf := mustReadFile(t, filepath.Join(repo.Root(), filepath.FromSlash(rrfPath)))
	if !strings.Contains(rrf, "[[brex|Brex]]") {
		t.Fatalf("RRF page should link to Brex:\n%s", rrf)
	}
	// The H1 heading "# Reciprocal Rank Fusion" must remain unlinked.
	if !strings.Contains(rrf, "# Reciprocal Rank Fusion\n") {
		t.Fatalf("RRF heading should be untouched:\n%s", rrf)
	}

	brex := mustReadFile(t, filepath.Join(repo.Root(), filepath.FromSlash(brexPath)))
	if !strings.Contains(brex, "[[reciprocal-rank-fusion|Reciprocal Rank Fusion]]") {
		t.Fatalf("Brex page should link to RRF:\n%s", brex)
	}

	// Second pass: idempotent — nothing rewritten.
	linked2, errs2 := interlinkPages(ctx, worker, pages)
	worker.WaitForIdle()
	if len(errs2) != 0 {
		t.Fatalf("second interlink errors: %v", errs2)
	}
	if linked2 != 0 {
		t.Fatalf("second interlink should rewrite nothing, got linked=%d", linked2)
	}
}

func mustWritePage(t *testing.T, worker *WikiWorker, relPath, body string) {
	t.Helper()
	if _, _, err := worker.Enqueue(context.Background(), ArchivistAuthor, relPath, body, "replace", "seed "+relPath); err != nil {
		t.Fatalf("seed page %s: %v", relPath, err)
	}
}

func mustReadFile(t *testing.T, fullPath string) string {
	t.Helper()
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read %s: %v", fullPath, err)
	}
	return string(data)
}
