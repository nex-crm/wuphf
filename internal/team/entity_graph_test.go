package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newGraphFixture spins up a wiki repo + worker + fact log + graph isolated
// to t.TempDir(). Returns helpers so tests can assert on repo bytes.
func newGraphFixture(t *testing.T) (*FactLog, *EntityGraph, *WikiWorker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return NewFactLog(worker), NewEntityGraph(worker), worker, func() {
		cancel()
		worker.Stop()
	}
}

func TestExtractRefs_KindedWikilinks(t *testing.T) {
	refs := ExtractRefs(EntityKindPeople, "sarah", "Sarah works at [[companies/acme]].", nil)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1: %#v", len(refs), refs)
	}
	if refs[0].Kind != EntityKindCompanies || refs[0].Slug != "acme" {
		t.Fatalf("wrong ref: %#v", refs[0])
	}
}

func TestExtractRefs_SelfReferenceElided(t *testing.T) {
	refs := ExtractRefs(EntityKindPeople, "sarah", "Sarah is [[people/sarah]]; also [[companies/acme]].", nil)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1: %#v", len(refs), refs)
	}
	if refs[0].Kind != EntityKindCompanies || refs[0].Slug != "acme" {
		t.Fatalf("wrong ref: %#v", refs[0])
	}
}

func TestExtractRefs_Dedupe(t *testing.T) {
	refs := ExtractRefs(EntityKindPeople, "sarah", "[[companies/acme]] then [[companies/acme]] again.", nil)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1 (dedup): %#v", len(refs), refs)
	}
}

func TestExtractRefs_BareSlugResolved(t *testing.T) {
	known := func(slug string) (EntityKind, bool) {
		if slug == "acme" {
			return EntityKindCompanies, true
		}
		return "", false
	}
	refs := ExtractRefs(EntityKindPeople, "sarah", "Sarah works at [[acme]].", known)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1: %#v", len(refs), refs)
	}
	if refs[0].Kind != EntityKindCompanies || refs[0].Slug != "acme" {
		t.Fatalf("wrong ref: %#v", refs[0])
	}
}

func TestExtractRefs_BareSlugAmbiguousSkipped(t *testing.T) {
	known := func(slug string) (EntityKind, bool) {
		// Always return false → ambiguous/unknown.
		return "", false
	}
	refs := ExtractRefs(EntityKindPeople, "sarah", "See [[unknown]].", known)
	if len(refs) != 0 {
		t.Fatalf("expected no refs; got %#v", refs)
	}
}

func TestExtractRefs_KindedAndBareNoDoubleCount(t *testing.T) {
	known := func(slug string) (EntityKind, bool) {
		if slug == "acme" {
			return EntityKindCompanies, true
		}
		return "", false
	}
	// Both the kinded wikilink and a bare wikilink point at acme — should
	// not produce two edges.
	refs := ExtractRefs(EntityKindPeople, "sarah", "[[companies/acme]] and also [[acme]].", known)
	if len(refs) != 1 {
		t.Fatalf("expected single ref, got %d: %#v", len(refs), refs)
	}
}

func TestRecordFactRefs_WritesEdges(t *testing.T) {
	factLog, graph, worker, teardown := newGraphFixture(t)
	defer teardown()
	ctx := context.Background()

	fact, err := factLog.Append(ctx, EntityKindPeople, "sarah", "Works at [[companies/acme]]", "", "pm")
	if err != nil {
		t.Fatalf("append fact: %v", err)
	}
	refs, err := graph.RecordFactRefs(ctx, fact)
	if err != nil {
		t.Fatalf("record refs: %v", err)
	}
	if len(refs) != 1 || refs[0].Slug != "acme" {
		t.Fatalf("refs: %#v", refs)
	}

	full := filepath.Join(worker.Repo().Root(), filepath.FromSlash(EntityGraphPath))
	bytes, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	if len(bytes) == 0 {
		t.Fatal("graph file is empty")
	}

	edges, err := graph.Coalesce()
	if err != nil {
		t.Fatalf("coalesce: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 coalesced edge, got %d", len(edges))
	}
	e := edges[0]
	if e.FromKind != EntityKindPeople || e.FromSlug != "sarah" {
		t.Errorf("from mismatch: %+v", e)
	}
	if e.ToKind != EntityKindCompanies || e.ToSlug != "acme" {
		t.Errorf("to mismatch: %+v", e)
	}
	if e.FirstSeenFactID != fact.ID {
		t.Errorf("first_seen_fact_id=%q, want %q", e.FirstSeenFactID, fact.ID)
	}
}

func TestRecordFactRefs_NoRefsNoWrite(t *testing.T) {
	factLog, graph, worker, teardown := newGraphFixture(t)
	defer teardown()
	ctx := context.Background()

	fact, err := factLog.Append(ctx, EntityKindPeople, "sarah", "No wikilinks here.", "", "pm")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	refs, err := graph.RecordFactRefs(ctx, fact)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs, got %d", len(refs))
	}

	full := filepath.Join(worker.Repo().Root(), filepath.FromSlash(EntityGraphPath))
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("graph file should not exist when no refs; err=%v", err)
	}
}

func TestEntityGraph_QueryDirections(t *testing.T) {
	factLog, graph, _, teardown := newGraphFixture(t)
	defer teardown()
	ctx := context.Background()

	fact1, err := factLog.Append(ctx, EntityKindPeople, "sarah", "At [[companies/acme]]", "", "pm")
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := graph.RecordFactRefs(ctx, fact1); err != nil {
		t.Fatalf("refs 1: %v", err)
	}

	// second fact with a different target, to make sure Query narrows by entity.
	fact2, err := factLog.Append(ctx, EntityKindCompanies, "acme", "Employs [[people/sarah]]", "", "pm")
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if _, err := graph.RecordFactRefs(ctx, fact2); err != nil {
		t.Fatalf("refs 2: %v", err)
	}

	// Out from sarah → acme.
	out, err := graph.Query(EntityKindPeople, "sarah", DirectionOut)
	if err != nil {
		t.Fatalf("query out: %v", err)
	}
	if len(out) != 1 || out[0].ToSlug != "acme" {
		t.Fatalf("unexpected out edges: %#v", out)
	}

	// In to sarah ← acme.
	in, err := graph.Query(EntityKindPeople, "sarah", DirectionIn)
	if err != nil {
		t.Fatalf("query in: %v", err)
	}
	if len(in) != 1 || in[0].FromSlug != "acme" {
		t.Fatalf("unexpected in edges: %#v", in)
	}

	// Both from sarah's vantage point.
	both, err := graph.Query(EntityKindPeople, "sarah", DirectionBoth)
	if err != nil {
		t.Fatalf("query both: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("expected 2 edges both ways, got %d: %#v", len(both), both)
	}
}

func TestEntityGraph_CoalesceDedupesMultipleFacts(t *testing.T) {
	factLog, graph, _, teardown := newGraphFixture(t)
	defer teardown()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		fact, err := factLog.Append(ctx, EntityKindPeople, "sarah", "At [[companies/acme]]", "", "pm")
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if _, err := graph.RecordFactRefs(ctx, fact); err != nil {
			t.Fatalf("refs %d: %v", i, err)
		}
		// Tiny separation so LastSeenTS comparisons can differ.
		time.Sleep(2 * time.Millisecond)
	}

	edges, err := graph.Coalesce()
	if err != nil {
		t.Fatalf("coalesce: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 coalesced edge, got %d", len(edges))
	}
	if edges[0].OccurrenceCount != 3 {
		t.Fatalf("occurrence_count=%d, want 3", edges[0].OccurrenceCount)
	}
}

func TestStripRelatedSection(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no section", "# Title\n\nBody.\n", "# Title\n\nBody.\n"},
		{
			"with section",
			"# Title\n\nBody.\n\n## Related\n\n- [[companies/acme]]\n",
			"# Title\n\nBody.",
		},
		{
			"case-insensitive",
			"# X\n\nBody\n\n## related\n- a\n",
			"# X\n\nBody",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := stripRelatedSection(tc.in)
			if got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestRenderRelatedSection_AppendsFromGraph(t *testing.T) {
	factLog, graph, worker, teardown := newGraphFixture(t)
	defer teardown()
	ctx := context.Background()

	fact, err := factLog.Append(ctx, EntityKindPeople, "sarah", "At [[companies/acme]]", "", "pm")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := graph.RecordFactRefs(ctx, fact); err != nil {
		t.Fatalf("record: %v", err)
	}

	synth := NewEntitySynthesizer(worker, factLog, nil, SynthesizerConfig{
		Threshold: 1,
		Timeout:   2 * time.Second,
		Graph:     graph,
		LLMCall: func(ctx context.Context, sys, user string) (string, error) {
			return "# Sarah\n\nBody.\n", nil
		},
	})
	out := synth.renderRelatedSection(EntityKindPeople, "sarah")
	if out == "" {
		t.Fatal("expected Related section, got empty")
	}
	if want := "## Related\n\n- [[companies/acme]]\n"; out != want {
		t.Errorf("got %q; want %q", out, want)
	}
}
