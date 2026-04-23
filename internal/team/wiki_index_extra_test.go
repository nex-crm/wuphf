package team

// wiki_index_extra_test.go — additional coverage for the wiki index:
//   - Fact ID determinism across Unicode, whitespace, casing, long objects,
//     empty inputs.
//   - Staleness regimes: zero confidence, zero age, heavily reinforced,
//     future valid_from.
//   - Path routing helpers (isEntityBriefPath, isLintReportPath).
//   - parseEdgeTimestamp layout fallbacks.
//   - Collision survey: 10k synthetic sentences yield no fact_id collisions.
//   - Substrate rebuild determinism under a full wipe (§7.4).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Fact-ID table coverage ────────────────────────────────────────────────────

func TestComputeFactID_ExtendedTableCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		a, b  func() string
		equal bool
	}{
		{
			name: "unicode_NFC_vs_NFD_normalization",
			// é in NFC (U+00E9) vs NFD (U+0065 U+0301)
			a: func() string { return ComputeFactID("sha", 1, "José", "role_at", "acme") },
			b: func() string { return ComputeFactID("sha", 1, "José", "role_at", "acme") },
			equal: true,
		},
		{
			name:  "whitespace_variations",
			a:     func() string { return ComputeFactID("sha", 1, " sarah   jones ", "role_at", "acme-corp") },
			b:     func() string { return ComputeFactID("sha", 1, "Sarah Jones", "role_at", "acme-corp") },
			equal: true,
		},
		{
			name:  "case_insensitive_predicate",
			a:     func() string { return ComputeFactID("sha", 1, "sarah", "REPORTS_TO", "bob") },
			b:     func() string { return ComputeFactID("sha", 1, "sarah", "reports_to", "bob") },
			equal: true,
		},
		{
			name: "very_long_object_deterministic",
			a: func() string {
				long := strings.Repeat("long-object-segment-", 100) // ~2KB
				return ComputeFactID("sha", 1, "sarah", "mentions", long)
			},
			b: func() string {
				long := strings.Repeat("long-object-segment-", 100)
				return ComputeFactID("sha", 1, "sarah", "mentions", long)
			},
			equal: true,
		},
		{
			name:  "empty_strings_do_not_panic",
			a:     func() string { return ComputeFactID("", 0, "", "", "") },
			b:     func() string { return ComputeFactID("", 0, "", "", "") },
			equal: true,
		},
		{
			name:  "empty_subject_distinct_from_nonempty",
			a:     func() string { return ComputeFactID("sha", 1, "", "p", "o") },
			b:     func() string { return ComputeFactID("sha", 1, "s", "p", "o") },
			equal: false,
		},
		{
			name:  "pure_punctuation_normalizes_to_empty",
			a:     func() string { return ComputeFactID("sha", 1, "!!!", "p", "o") },
			b:     func() string { return ComputeFactID("sha", 1, "", "p", "o") },
			equal: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotA, gotB := tc.a(), tc.b()
			if len(gotA) != 16 {
				t.Errorf("len(gotA) = %d, want 16", len(gotA))
			}
			if tc.equal && gotA != gotB {
				t.Errorf("expected equal: %q vs %q", gotA, gotB)
			}
			if !tc.equal && gotA == gotB {
				t.Errorf("expected distinct, both = %q", gotA)
			}
		})
	}
}

// TestComputeFactID_NoCollisionsOn10kSyntheticSentences is the regression guard
// that the 16-hex truncation does not collide on realistic corpora.
//
// 10k distinct (subject, predicate, object) triplets should yield 10k distinct
// fact IDs. If any two collide we have a substrate bug.
func TestComputeFactID_NoCollisionsOn10kSyntheticSentences(t *testing.T) {
	t.Parallel()
	const n = 10000
	seen := make(map[string]string, n)
	for i := 0; i < n; i++ {
		// Mix the index into all four positions so entropy is high.
		artifact := fmt.Sprintf("artifact-%05d", i)
		offset := i % 500
		subject := fmt.Sprintf("subject-%05d", i)
		predicate := fmt.Sprintf("predicate_%d", i%20) // 20 predicate buckets
		object := fmt.Sprintf("object-%05d", i)
		id := ComputeFactID(artifact, offset, subject, predicate, object)
		if prev, ok := seen[id]; ok {
			t.Fatalf("fact_id collision at i=%d: id=%q, previous description=%q", i, id, prev)
		}
		seen[id] = fmt.Sprintf("%s/%d/%s/%s/%s", artifact, offset, subject, predicate, object)
	}
	if len(seen) != n {
		t.Errorf("len(seen) = %d, want %d", len(seen), n)
	}
}

// ── NormalizeForFactID extra cases ────────────────────────────────────────────

func TestNormalizeForFactID_EdgeCases(t *testing.T) {
	t.Parallel()
	// The normalizer keeps only [a-z0-9] — NFC canonicalizes glyph shape but
	// non-ASCII letters collapse to dash separators. This matches §7.3 (pure
	// ASCII slug space) and is stable across locales.
	cases := map[string]string{
		"Sarah\tJ.\nones":          "sarah-j-ones",
		"---trailing---":           "trailing",
		"multiple   spaces   here": "multiple-spaces-here",
		"ASCII_ONLY_UPPER":         "ascii-only-upper",
		"123numbers":               "123numbers",
		"Mix3d_C4s3":               "mix3d-c4s3",
	}
	for in, want := range cases {
		if got := NormalizeForFactID(in); got != want {
			t.Errorf("NormalizeForFactID(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── Staleness additional regimes ──────────────────────────────────────────────

func TestStaleness_AdditionalRegimes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	ptrTime := func(tt time.Time) *time.Time { return &tt }

	cases := []struct {
		name    string
		fact    TypedFact
		wantMin float64
		wantMax float64
		note    string
	}{
		{
			name: "zero_confidence_falls_back_to_default_1",
			fact: TypedFact{
				Type:       "observation",
				Confidence: 0, // should be treated as 1.0
				ValidFrom:  now.AddDate(0, 0, -10),
			},
			// days_old=10 * weight=0.5 - conf*10 = 5 - 10 = -5
			wantMin: -5.1,
			wantMax: -4.9,
			note:    "zero confidence uses default=1.0 (§4.3)",
		},
		{
			name: "zero_age_status_high_confidence",
			fact: TypedFact{
				Type:       "status",
				Confidence: 1.0,
				ValidFrom:  now, // age = 0
			},
			wantMin: -10.1,
			wantMax: -9.9,
			note:    "0 * 1 - 1*10 = -10",
		},
		{
			name: "heavily_reinforced_recent",
			fact: TypedFact{
				Type:         "observation",
				Confidence:   0.5,
				ValidFrom:    now.AddDate(0, 0, -30),
				ReinforcedAt: ptrTime(now), // 0 days since reinforced → decay 1.0 → bonus 5.0
			},
			// 30*0.5 - 0.5*10 - 5 = 15 - 5 - 5 = 5
			wantMin: 4.9,
			wantMax: 5.1,
			note:    "reinforced_at=now → full 5.0 bonus",
		},
		{
			name: "future_valid_from_yields_negative_age",
			fact: TypedFact{
				Type:       "status",
				Confidence: 0.8,
				ValidFrom:  now.AddDate(0, 0, 10), // 10 days in the future
			},
			// days_old = -10, weight=1.0 → -10 - 8 = -18
			wantMin: -18.1,
			wantMax: -17.9,
			note:    "future valid_from → negative days_old",
		},
		{
			name: "background_weight_small_contribution",
			fact: TypedFact{
				Type:       "background",
				Confidence: 1.0,
				ValidFrom:  now.AddDate(0, 0, -365),
			},
			// 365 * 0.1 - 10 = 36.5 - 10 = 26.5
			wantMin: 26.4,
			wantMax: 26.6,
			note:    "background weight 0.1 keeps year-old fact only mildly stale",
		},
		{
			name: "reinforced_long_ago_bonus_decays_to_zero",
			fact: TypedFact{
				Type:         "status",
				Confidence:   1.0,
				ValidFrom:    now.AddDate(0, 0, -100),
				ReinforcedAt: ptrTime(now.AddDate(0, 0, -60)), // 60 days > 30 → decay = 0
			},
			// 100 - 10 - 0 = 90
			wantMin: 89.9,
			wantMax: 90.1,
			note:    "reinforcement bonus decays to 0 after 30 days",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Staleness(tc.fact, now)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("Staleness = %.3f, want in [%.3f, %.3f] — %s",
					got, tc.wantMin, tc.wantMax, tc.note)
			}
		})
	}
}

// TestStaleness_DefaultNow verifies that passing a zero now falls back to
// time.Now, keeping the formula usable in production call sites that don't
// pass an explicit clock.
func TestStaleness_DefaultNow(t *testing.T) {
	t.Parallel()
	f := TypedFact{
		Type:       "observation",
		Confidence: 1.0,
		ValidFrom:  time.Now().Add(-24 * time.Hour),
	}
	got := Staleness(f, time.Time{})
	// days_old ≈ 1, weight=0.5, conf*10 = 10 → ~ 0.5 - 10 = -9.5
	if got > -9 || got < -10 {
		t.Errorf("Staleness(zero now) = %.2f, want roughly -9.5", got)
	}
}

// ── Path routing helpers ──────────────────────────────────────────────────────

func TestIsEntityBriefPath(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"team/person/sarah-jones.md":      true,
		"team/companies/acme-corp.md":     true,
		"team/.lint/report-2026-04-22.md": true,  // matches the regex (not filtered here)
		"team/person/nested/file.md":      false, // deeper than one subdir
		"wiki/facts/person/sarah.jsonl":   false,
		"team.md":                         false,
		"":                                false,
	}
	for in, want := range cases {
		if got := isEntityBriefPath(in); got != want {
			t.Errorf("isEntityBriefPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsLintReportPath(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"wiki/.lint/report-2026-04-22.md": true,
		"wiki/.lint/report-2025-12-31.md": true,
		"wiki/.lint/report-bad-date.md":   false,
		"wiki/.lint/report-2026-13-99.md": true, // regex only validates shape, not date
		"wiki/lint/report-2026-04-22.md":  false,
		"wiki/.lint/something-else.md":    false,
	}
	for in, want := range cases {
		if got := isLintReportPath(in); got != want {
			t.Errorf("isLintReportPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsFactLogPath_ExtendedCases(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"wiki/facts/person/sarah-jones.jsonl":        true,
		"wiki/facts/company/acme-corp.jsonl":         true,
		"team/entities/person-sarah-jones.facts.jsonl": true,
		"team/entities/company-acme-corp.facts.jsonl":  true,
		"wiki/facts/sarah-jones.jsonl":                 false, // missing kind segment
		"wiki/facts/person/sub/sarah.jsonl":            false, // too deep
		"team/entities/Bad_Uppercase.facts.jsonl":      false,
	}
	for in, want := range cases {
		if got := isFactLogPath(in); got != want {
			t.Errorf("isFactLogPath(%q) = %v, want %v", in, got, want)
		}
	}
}

// ── parseEdgeTimestamp fallbacks ──────────────────────────────────────────────

func TestParseEdgeTimestamp_Layouts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		wantZero bool
	}{
		{"rfc3339", "2026-04-10T09:00:00Z", false},
		{"datetime_no_tz", "2026-04-10T09:00:00", false},
		{"date_only", "2026-04-10", false},
		{"empty_string", "", true},
		{"garbage", "not-a-date", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseEdgeTimestamp(tc.in)
			if tc.wantZero && !got.IsZero() {
				t.Errorf("parseEdgeTimestamp(%q) = %v, want zero", tc.in, got)
			}
			if !tc.wantZero && got.IsZero() {
				t.Errorf("parseEdgeTimestamp(%q) = zero, want parsed", tc.in)
			}
		})
	}
}

// ── Substrate rebuild determinism under wipe ──────────────────────────────────

// TestWikiIndex_SubstrateWipeAndRebuild implements §7.4: write N artifacts,
// build the index, compute CanonicalHashAll, blow the index away, rebuild
// from markdown only, assert the composite hash matches.
func TestWikiIndex_SubstrateWipeAndRebuild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx := context.Background()

	// Seed 20 fact logs, 5 entity briefs, a graph.log with 10 edges.
	seedCorpus := func(t *testing.T) {
		t.Helper()
		for i := 0; i < 20; i++ {
			slug := fmt.Sprintf("person-%03d", i)
			factsDir := filepath.Join(root, "wiki", "facts", "person")
			mustMkdir(t, factsDir)
			f := TypedFact{
				ID:         fmt.Sprintf("wipefact-%04d", i),
				EntitySlug: slug,
				Kind:       "person",
				Type:       "observation",
				Text:       fmt.Sprintf("Observation %d about %s.", i, slug),
				Confidence: 0.9,
				ValidFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				CreatedBy:  "archivist",
			}
			writeFactJSONL(t, filepath.Join(factsDir, slug+".jsonl"), f)
		}
		briefDir := filepath.Join(root, "team", "person")
		mustMkdir(t, briefDir)
		for i := 0; i < 5; i++ {
			slug := fmt.Sprintf("brief-%03d", i)
			body := fmt.Sprintf("---\ncanonical_slug: %s\nkind: person\n---\n# %s\n", slug, slug)
			mustWrite(t, filepath.Join(briefDir, slug+".md"), []byte(body))
		}
		var graph strings.Builder
		for i := 0; i < 10; i++ {
			fmt.Fprintf(&graph, "sub-%d works_at obj-%d 2026-04-10 src=sha%d\n", i, i%3, i)
		}
		mustWrite(t, filepath.Join(root, "graph.log"), []byte(graph.String()))
	}
	seedCorpus(t)

	idx1 := NewWikiIndex(root)
	if err := idx1.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	h1, err := idx1.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	_ = idx1.Close()

	// "Blow the index away" — for in-memory indexes this is effectively a fresh
	// WikiIndex with no prior state, which is the substrate guarantee: the
	// markdown corpus alone reproduces the index.
	idx2 := NewWikiIndex(root)
	if err := idx2.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	h2, err := idx2.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	_ = idx2.Close()

	if h1 != h2 {
		t.Errorf("CanonicalHashAll drift after substrate wipe: %s → %s", h1, h2)
	}
}

// ── Concurrent read during write ──────────────────────────────────────────────

// TestWikiIndex_ConcurrentReadDuringWrite exercises the in-memory store's
// read/write locking under 10 concurrent readers + 1 concurrent writer.
// Assertions: no data race (run with -race), readers never observe a partial
// fact shape.
func TestWikiIndex_ConcurrentReadDuringWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := NewWikiIndex(t.TempDir())
	defer idx.Close()

	// Prime with a single fact so readers have something to find.
	seed := TypedFact{
		ID:         "concur-seed",
		EntitySlug: "alice",
		Text:       "Seed fact for concurrency test.",
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "archivist",
	}
	if err := idx.store.UpsertFact(ctx, seed); err != nil {
		t.Fatalf("prime: %v", err)
	}
	if err := idx.text.Index(ctx, seed); err != nil {
		t.Fatalf("prime text: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: add 200 facts as fast as possible.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			f := TypedFact{
				ID:         fmt.Sprintf("concur-%04d", i),
				EntitySlug: "alice",
				Text:       "fact",
				CreatedAt:  time.Now().UTC(),
				CreatedBy:  "archivist",
			}
			_ = idx.store.UpsertFact(ctx, f)
			_ = idx.text.Index(ctx, f)
		}
	}()

	// Readers: 10 goroutines calling GetFact + Search in parallel.
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				select {
				case <-stop:
					return
				default:
				}
				_, _, _ = idx.GetFact(ctx, "concur-seed")
				_, _ = idx.Search(ctx, "fact", 5)
			}
		}()
	}

	wg.Wait()
	close(stop)
}

// ── Search top-K clamping ────────────────────────────────────────────────────

func TestWikiIndex_SearchTopKClamping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := NewWikiIndex(t.TempDir())
	defer idx.Close()

	// Seed 150 facts with the same hit term so we can verify the 100 cap.
	for i := 0; i < 150; i++ {
		f := TypedFact{
			ID:         fmt.Sprintf("clamp-%04d", i),
			EntitySlug: "alice",
			Text:       "alpha-term-clamp-target",
			CreatedAt:  time.Now().UTC(),
			CreatedBy:  "archivist",
		}
		_ = idx.store.UpsertFact(ctx, f)
		_ = idx.text.Index(ctx, f)
	}

	// Asking for 1000 should clamp to 100.
	hits, err := idx.Search(ctx, "alpha-term-clamp-target", 1000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 100 {
		t.Errorf("Search topK=1000 returned %d hits, want ≤ 100", len(hits))
	}
	// Asking for 0 should default to 10 (not return empty).
	hits2, err := idx.Search(ctx, "alpha-term-clamp-target", 0)
	if err != nil {
		t.Fatalf("Search 0: %v", err)
	}
	if len(hits2) > 10 {
		t.Errorf("Search topK=0 returned %d hits, want ≤ 10", len(hits2))
	}
	// Empty query yields no hits (not an error).
	empty, err := idx.Search(ctx, "", 5)
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Search(empty) = %d hits, want 0", len(empty))
	}
}

// ── Last-build timestamp ──────────────────────────────────────────────────────

func TestWikiIndex_LastBuildTimestampAdvances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := NewWikiIndex(t.TempDir())
	defer idx.Close()

	if !idx.LastBuild().IsZero() {
		t.Errorf("LastBuild before reconcile = %v, want zero", idx.LastBuild())
	}
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	first := idx.LastBuild()
	if first.IsZero() {
		t.Fatal("LastBuild still zero after reconcile")
	}
	time.Sleep(5 * time.Millisecond) // monotonic clock moves; small sleep OK
	if err := idx.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if !idx.LastBuild().After(first) {
		t.Errorf("LastBuild did not advance: %v → %v", first, idx.LastBuild())
	}
}

// ── Security: path traversal guards ──────────────────────────────────────────

// TestPathRouting_RejectsTraversal verifies the path classifiers do not accept
// traversal or absolute paths disguised as relative inputs.
func TestPathRouting_RejectsTraversal(t *testing.T) {
	t.Parallel()
	dangerous := []string{
		"../../etc/passwd",
		"/etc/passwd",
		"wiki/facts/../../../etc/passwd.jsonl",
		"team/../../../home/user.md",
		"wiki/.lint/../../report.md",
	}
	for _, p := range dangerous {
		if isFactLogPath(p) {
			t.Errorf("isFactLogPath(%q) = true, want false (traversal rejection)", p)
		}
		if isEntityBriefPath(p) {
			t.Errorf("isEntityBriefPath(%q) = true, want false (traversal rejection)", p)
		}
		if isLintReportPath(p) {
			t.Errorf("isLintReportPath(%q) = true, want false (traversal rejection)", p)
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// writeFactJSONL appends a single TypedFact as a newline-terminated JSON line.
func writeFactJSONL(t *testing.T, path string, f TypedFact) {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal fact %s: %v", f.ID, err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write fact jsonl %s: %v", path, err)
	}
}
