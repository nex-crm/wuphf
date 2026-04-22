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
		"Sarah Jones":  "sarah-jones",
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
