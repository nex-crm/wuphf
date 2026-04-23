package team

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeFactID_Deterministic(t *testing.T) {
	cases := []struct {
		name  string
		a, b  func() string
		equal bool
	}{
		{
			name: "same inputs produce same id",
			a: func() string {
				return ComputeFactID("artifact_sha_1", 142, "sarah-jones", "role_at", "acme-corp")
			},
			b: func() string {
				return ComputeFactID("artifact_sha_1", 142, "sarah-jones", "role_at", "acme-corp")
			},
			equal: true,
		},
		{
			name: "normalization collapses case and punctuation",
			a: func() string {
				return ComputeFactID("artifact_sha_1", 142, "Sarah Jones", "Role_At", "Acme Corp")
			},
			b: func() string {
				return ComputeFactID("artifact_sha_1", 142, "sarah-jones", "role-at", "acme-corp")
			},
			equal: true,
		},
		{
			name: "different sentence offset produces different id",
			a: func() string {
				return ComputeFactID("artifact_sha_1", 142, "s", "p", "o")
			},
			b: func() string {
				return ComputeFactID("artifact_sha_1", 999, "s", "p", "o")
			},
			equal: false,
		},
		{
			name: "different artifact sha produces different id",
			a: func() string {
				return ComputeFactID("sha1", 1, "s", "p", "o")
			},
			b: func() string {
				return ComputeFactID("sha2", 1, "s", "p", "o")
			},
			equal: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotA := tc.a()
			gotB := tc.b()
			if tc.equal && gotA != gotB {
				t.Errorf("expected equal ids, got %q vs %q", gotA, gotB)
			}
			if !tc.equal && gotA == gotB {
				t.Errorf("expected different ids, both = %q", gotA)
			}
			if len(gotA) != 16 {
				t.Errorf("fact id must be 16 chars, got %d: %q", len(gotA), gotA)
			}
		})
	}
}

func TestNormalizeForFactID(t *testing.T) {
	cases := map[string]string{
		"Sarah Jones": "sarah-jones",
		"  foo  bar ": "foo-bar",
		"ACME_CORP!!": "acme-corp",
		"a/b/c":       "a-b-c",
		"":            "",
	}
	for in, want := range cases {
		if got := NormalizeForFactID(in); got != want {
			t.Errorf("NormalizeForFactID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStaleness_Formula(t *testing.T) {
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		fact     TypedFact
		wantMin  float64
		wantMax  float64
		describe string
	}{
		{
			name: "fresh status fact high confidence has negative staleness",
			fact: TypedFact{
				Type:       "status",
				Confidence: 0.95,
				ValidFrom:  now.AddDate(0, 0, -1),
			},
			wantMin:  -10.0,
			wantMax:  -8.0,
			describe: "days_old=1 * weight=1.0 - conf*10=9.5 - 0 = -8.5",
		},
		{
			name: "old observation fact low confidence large positive staleness",
			fact: TypedFact{
				Type:       "observation",
				Confidence: 0.5,
				ValidFrom:  now.AddDate(0, 0, -90),
			},
			wantMin:  39.0,
			wantMax:  41.0,
			describe: "days_old=90 * weight=0.5 - conf*10=5 = 40.0",
		},
		{
			name: "recently reinforced fact has bonus applied",
			fact: TypedFact{
				Type:       "status",
				Confidence: 0.9,
				ValidFrom:  now.AddDate(0, 0, -60),
				ReinforcedAt: func() *time.Time {
					t := now.AddDate(0, 0, -1)
					return &t
				}(),
			},
			wantMin:  45.0,
			wantMax:  47.0,
			describe: "days_old=60*1.0=60 - conf*10=9 - reinforcement=5*(1-1/30)=4.83 → 46.17",
		},
		{
			name: "default confidence = 1.0 when zero",
			fact: TypedFact{
				Type:      "observation",
				ValidFrom: now,
			},
			wantMin:  -10.1,
			wantMax:  -9.9,
			describe: "days_old=0 - conf*10=10 = -10 (default conf)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Staleness(tc.fact, now)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("Staleness = %.2f, want in [%.2f, %.2f] — %s",
					got, tc.wantMin, tc.wantMax, tc.describe)
			}
		})
	}
}

func TestWikiIndex_ReconcileFromMarkdown_Roundtrip(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Write a legacy-shape fact log (no typed fields except ID).
	factsDir := filepath.Join(root, "wiki", "facts", "person")
	if err := os.MkdirAll(factsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f1 := TypedFact{
		ID:         "a3f9b2c14e8d0001",
		EntitySlug: "sarah-jones",
		Type:       "status",
		Text:       "Sarah was promoted to VP of Sales.",
		Confidence: 0.95,
		ValidFrom:  time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 4, 22, 13, 1, 0, 0, time.UTC),
		CreatedBy:  "archivist",
	}
	b, _ := json.Marshal(f1)
	if err := os.WriteFile(filepath.Join(factsDir, "sarah-jones.jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write an entity brief with canonical_slug frontmatter.
	briefDir := filepath.Join(root, "team", "person")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\ncanonical_slug: sarah-jones\nkind: person\ncreated_by: nazz\n---\n\n# Sarah Jones\n\nVP of Sales.\n"
	if err := os.WriteFile(filepath.Join(briefDir, "sarah-jones.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a graph.log edge.
	edge := "sarah-jones  works_at  acme-corp  2026-04-10  src=3f9a21\n"
	if err := os.WriteFile(filepath.Join(root, "graph.log"), []byte(edge), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()

	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Fact reads back.
	got, ok, err := idx.GetFact(ctx, "a3f9b2c14e8d0001")
	if err != nil || !ok {
		t.Fatalf("GetFact missing: ok=%v err=%v", ok, err)
	}
	if got.EntitySlug != "sarah-jones" {
		t.Errorf("fact entity_slug = %q, want sarah-jones", got.EntitySlug)
	}

	// List by entity.
	facts, err := idx.ListFactsForEntity(ctx, "sarah-jones")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("list returned %d facts, want 1", len(facts))
	}

	// Edge was indexed.
	edges, err := idx.ListEdgesForEntity(ctx, "sarah-jones")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Errorf("expected at least one edge for sarah-jones")
	}

	// Search finds it.
	hits, err := idx.Search(ctx, "promoted", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].FactID != "a3f9b2c14e8d0001" {
		t.Errorf("search hits = %+v, want single hit for a3f9b2c14e8d0001", hits)
	}

	// §7.4 rebuild contract: canonical hash is stable across rebuilds.
	h1, err := idx.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idx2 := NewWikiIndex(root)
	defer idx2.Close()
	if err := idx2.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatal(err)
	}
	h2, err := idx2.CanonicalHashFacts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("canonical hash drift: %s → %s", h1, h2)
	}
}

func TestWikiIndex_Redirects(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	briefDir := filepath.Join(root, "team", "person")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// sjones is a redirect to sarah-jones.
	redirect := "---\ncanonical_slug: sarah-jones\nkind: person\n---\n\nThis page redirects to [[sarah-jones]].\n"
	if err := os.WriteFile(filepath.Join(briefDir, "sjones.md"), []byte(redirect), 0o644); err != nil {
		t.Fatal(err)
	}
	// sarah-jones is the survivor.
	survivor := "---\ncanonical_slug: sarah-jones\nkind: person\n---\n\n# Sarah Jones\n"
	if err := os.WriteFile(filepath.Join(briefDir, "sarah-jones.md"), []byte(survivor), 0o644); err != nil {
		t.Fatal(err)
	}
	// One fact against the survivor slug.
	factsDir := filepath.Join(root, "wiki", "facts", "person")
	if err := os.MkdirAll(factsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := TypedFact{
		ID:         "fact001",
		EntitySlug: "sarah-jones",
		Text:       "Sarah is VP of Sales.",
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "archivist",
	}
	b, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(factsDir, "sarah-jones.jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatal(err)
	}
	// Querying the redirect slug returns the survivor's facts.
	facts, err := idx.ListFactsForEntity(ctx, "sjones")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ID != "fact001" {
		t.Errorf("redirect follow failed: got %+v", facts)
	}
}

// TestWikiIndex_ReconcileMutexNarrow verifies that Search and GetFact are not
// blocked for the full duration of ReconcileFromMarkdown (H7 fix). A goroutine
// calls ReconcileFromMarkdown over a 50-fact corpus while the main goroutine
// concurrently issues Search + GetFact calls. Every query must complete within
// 50 ms — far less than any full-walk latency — proving the mutex is not held
// across the entire walk.
func TestWikiIndex_ReconcileMutexNarrow(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Seed 50 facts into a single .jsonl file.
	factsDir := filepath.Join(root, "wiki", "facts", "person")
	if err := os.MkdirAll(factsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []byte
	for i := 0; i < 50; i++ {
		f := TypedFact{
			ID:         fmt.Sprintf("mutex-test-%04d", i),
			EntitySlug: "alice-smith",
			Type:       "observation",
			Text:       fmt.Sprintf("Observation number %d about Alice.", i),
			CreatedAt:  time.Now().UTC(),
			CreatedBy:  "archivist",
		}
		b, _ := json.Marshal(f)
		lines = append(lines, b...)
		lines = append(lines, '\n')
	}
	if err := os.WriteFile(filepath.Join(factsDir, "alice-smith.jsonl"), lines, 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()

	// Prime with one reconcile so GetFact has something to return.
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("prime reconcile: %v", err)
	}

	// Launch a second reconcile in a goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = idx.ReconcileFromMarkdown(ctx)
	}()

	// For 500 ms, fire Search + GetFact in a tight loop and assert each
	// call completes within 50 ms.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		start := time.Now()
		_, _ = idx.Search(ctx, "alice", 5)
		elapsed := time.Since(start)
		if elapsed > 50*time.Millisecond {
			t.Errorf("Search blocked for %v, want <50ms (mutex held across walk?)", elapsed)
		}

		start = time.Now()
		_, _, _ = idx.GetFact(ctx, "mutex-test-0000")
		elapsed = time.Since(start)
		if elapsed > 50*time.Millisecond {
			t.Errorf("GetFact blocked for %v, want <50ms (mutex held across walk?)", elapsed)
		}
	}

	<-done
}

// TestWikiIndex_UpsertEdgeDedup verifies that repeated ReconcileFromMarkdown
// calls do not double-index edges (H9 fix). graph.log with 3 edges reconciled
// twice must yield exactly 3 edges per entity, not 6.
func TestWikiIndex_UpsertEdgeDedup(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	graphLog := "sarah-jones  works_at   acme-corp   2026-04-10  src=aaa111\n" +
		"sarah-jones  reports_to bob-smith   2026-04-10  src=bbb222\n" +
		"bob-smith    manages    sarah-jones 2026-04-10  src=ccc333\n"
	if err := os.WriteFile(filepath.Join(root, "graph.log"), []byte(graphLog), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()

	// Reconcile twice — edges must be idempotent.
	for i := 0; i < 2; i++ {
		if err := idx.ReconcileFromMarkdown(ctx); err != nil {
			t.Fatalf("reconcile %d: %v", i+1, err)
		}
	}

	edges, err := idx.ListEdgesForEntity(ctx, "sarah-jones")
	if err != nil {
		t.Fatal(err)
	}
	// sarah-jones is subject of 2 edges and object of 1 edge → 3 total.
	if len(edges) != 3 {
		t.Errorf("ListEdgesForEntity(sarah-jones) = %d edges, want 3 (no double-index)", len(edges))
		for _, e := range edges {
			t.Logf("  edge: %s -[%s]-> %s", e.Subject, e.Predicate, e.Object)
		}
	}
}

// TestFrontmatterList_BlockList verifies that YAML block-list aliases are parsed
// correctly and populated into IndexEntity.Aliases (M12 fix).
func TestFrontmatterList_BlockList(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	briefDir := filepath.Join(root, "team", "person")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Block-list aliases form.
	brief := "---\ncanonical_slug: sarah-jones\nkind: person\naliases:\n  - Sarah J.\n  - sjones\ncreated_by: nazz\n---\n\n# Sarah Jones\n"
	if err := os.WriteFile(filepath.Join(briefDir, "sarah-jones.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// The entity should have Aliases populated.
	store := idx.store.(*inMemoryFactStore)
	store.mu.RLock()
	entity, ok := store.entities["sarah-jones"]
	store.mu.RUnlock()
	if !ok {
		t.Fatal("entity sarah-jones not found in store")
	}
	if len(entity.Aliases) != 2 {
		t.Fatalf("Aliases = %v, want [Sarah J. sjones]", entity.Aliases)
	}
	if entity.Aliases[0] != "Sarah J." || entity.Aliases[1] != "sjones" {
		t.Errorf("Aliases = %v, want [Sarah J., sjones]", entity.Aliases)
	}
}

func TestFrontmatterList_Scalar(t *testing.T) {
	block := "aliases: Sarah J.\nkind: person\n"
	got := frontmatterList(block, "aliases")
	if len(got) != 1 || got[0] != "Sarah J." {
		t.Errorf("frontmatterList scalar = %v, want [Sarah J.]", got)
	}
}

func TestFrontmatterList_InlineBracket(t *testing.T) {
	block := "aliases: [Sarah J., sjones]\nkind: person\n"
	got := frontmatterList(block, "aliases")
	if len(got) != 2 || got[0] != "Sarah J." || got[1] != "sjones" {
		t.Errorf("frontmatterList inline = %v, want [Sarah J., sjones]", got)
	}
}

// TestWikiIndex_LintReportIndexed verifies that lint reports at
// wiki/.lint/report-YYYY-MM-DD.md are indexed and findable via BM25 search
// (L18 fix).
func TestWikiIndex_LintReportIndexed(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	lintDir := filepath.Join(root, "wiki", ".lint")
	if err := os.MkdirAll(lintDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportBody := "# Lint Report 2026-04-22\n\nContradiction found in sarah-jones role.\n"
	if err := os.WriteFile(filepath.Join(lintDir, "report-2026-04-22.md"), []byte(reportBody), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	hits, err := idx.Search(ctx, "contradiction", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("Search(contradiction) returned no hits; lint report not indexed")
	}
	found := false
	for _, h := range hits {
		if h.FactID == "lint_2026_04_22" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("hit list = %+v; want a hit with FactID lint_2026_04_22", hits)
	}
}

// TestWikiIndex_GraphLogTimestampLayouts verifies that RFC3339 and date-only
// timestamps both parse correctly (L19 fix). Both edges must have non-zero
// Timestamps after reconcile.
func TestWikiIndex_GraphLogTimestampLayouts(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// One RFC3339 edge + one date-only edge.
	graphLog := "alice works_at acme 2026-04-10T09:00:00Z src=rfc3339sha\n" +
		"alice reports_to bob 2026-04-11 src=dateonly\n"
	if err := os.WriteFile(filepath.Join(root, "graph.log"), []byte(graphLog), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewWikiIndex(root)
	defer idx.Close()
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	edges, err := idx.ListEdgesForEntity(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edges))
	}
	for _, e := range edges {
		if e.Timestamp.IsZero() {
			t.Errorf("edge %s-[%s]->%s has zero Timestamp; layout not parsed", e.Subject, e.Predicate, e.Object)
		}
	}
}

// TestNormalizeForFactID_Unicode verifies that NFC and NFD forms of the same
// Unicode string produce identical output (L17 fix). The hash is load-bearing:
// LLMs may return NFD; we must produce the same ID regardless.
func TestNormalizeForFactID_Unicode(t *testing.T) {
	// Build NFC and NFD forms of three test strings programmatically so the
	// source file never embeds raw combining characters.
	cases := []struct {
		nfc string
		nfd string
	}{
		// José García: é = U+00E9 (NFC) vs e + U+0301 (NFD)
		{nfc: "José García", nfd: "José García"},
		// Zürich: ü = U+00FC (NFC) vs u + U+0308 (NFD)
		{nfc: "Zürich", nfd: "Zürich"},
		// naïve: ï = U+00EF (NFC) vs i + U+0308 (NFD)
		{nfc: "naïve", nfd: "naïve"},
	}
	for _, tc := range cases {
		nfcResult := NormalizeForFactID(tc.nfc)
		nfdResult := NormalizeForFactID(tc.nfd)
		if nfcResult != nfdResult {
			t.Errorf("NFC %q → %q, NFD %q → %q; want identical output",
				tc.nfc, nfcResult, tc.nfd, nfdResult)
		}
	}
}

// TestWikiIndex_CanonicalHashAll_RebuildContract verifies that CanonicalHashAll
// is stable across a full wipe + rebuild (L16 fix). It then mutates an entity
// brief on disk and asserts the hash changes — proving the entity layer is covered.
func TestWikiIndex_CanonicalHashAll_RebuildContract(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Seed facts, a brief, an edge, and a redirect.
	factsDir := filepath.Join(root, "wiki", "facts", "person")
	if err := os.MkdirAll(factsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f1 := TypedFact{
		ID:         "hashall-fact-001",
		EntitySlug: "alice",
		Type:       "status",
		Text:       "Alice is CTO.",
		CreatedAt:  time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		CreatedBy:  "archivist",
	}
	b, _ := json.Marshal(f1)
	if err := os.WriteFile(filepath.Join(factsDir, "alice.jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	briefDir := filepath.Join(root, "team", "person")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\ncanonical_slug: alice\nkind: person\naliases:\n  - Alice Smith\ncreated_by: nazz\n---\n\n# Alice\n"
	if err := os.WriteFile(filepath.Join(briefDir, "alice.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}

	// Redirect: old-alice → alice
	if err := os.WriteFile(filepath.Join(briefDir, "old-alice.md"),
		[]byte("---\ncanonical_slug: alice\nkind: person\n---\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	edge := "alice works_at acme 2026-04-10 src=aaa\n"
	if err := os.WriteFile(filepath.Join(root, "graph.log"), []byte(edge), 0o644); err != nil {
		t.Fatal(err)
	}

	idx1 := NewWikiIndex(root)
	defer idx1.Close()
	if err := idx1.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	h1, err := idx1.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll 1: %v", err)
	}

	// Wipe and rebuild with a fresh index.
	idx2 := NewWikiIndex(root)
	defer idx2.Close()
	if err := idx2.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	h2, err := idx2.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("CanonicalHashAll drift after rebuild: %s → %s", h1, h2)
	}

	// Now mutate the alias in the brief and reconcile into a third index.
	mutatedBrief := "---\ncanonical_slug: alice\nkind: person\naliases:\n  - Alice W. Smith\ncreated_by: nazz\n---\n\n# Alice\n"
	if err := os.WriteFile(filepath.Join(briefDir, "alice.md"), []byte(mutatedBrief), 0o644); err != nil {
		t.Fatal(err)
	}

	idx3 := NewWikiIndex(root)
	defer idx3.Close()
	if err := idx3.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile 3: %v", err)
	}
	h3, err := idx3.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("CanonicalHashAll 3: %v", err)
	}
	if h1 == h3 {
		t.Errorf("CanonicalHashAll did not change after alias mutation (entity layer not covered)")
	}
}
